//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package accounting_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"hash"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/proto"
)

const (
	journalBodyMaskHeaderMarker = "/* tax fact reporting journal header body */"
	journalBodyMaskLinesMarker  = "/* tax fact reporting journal lines body */"
	journalBodyMaskCopyCeiling  = uint64(5 * 1024 * 1024)
)

func TestJournalRepositoryReportingBodiesMaskLateParentAndLineDrift(t *testing.T) {
	const largeBytes = 9 * 1024 * 1024
	for _, tc := range []struct {
		name, mutationStage, table, update string
		arguments                          func(journalID, factLineID, foreignOrgID string) []any
		foreignOwner                       bool
	}{
		{
			name:          "cross-row header owner",
			mutationStage: "header", table: "journals",
			update: `UPDATE journals SET organisation_id=? WHERE id=?`, foreignOwner: true,
			arguments: func(journalID, _, foreignOrgID string) []any {
				return []any{foreignOrgID, journalID}
			},
		},
		{
			name:          "missing selected line",
			mutationStage: "header", table: "journal_lines",
			update: `DELETE FROM journal_lines WHERE id=?`,
			arguments: func(journalID, _, _ string) []any {
				return []any{journalID + "-balancing-line"}
			},
		},
		{
			name:          "nine MiB header growth",
			mutationStage: "header", table: "journals",
			update:    `UPDATE journals SET description=CAST(zeroblob(?) AS TEXT) WHERE id=?`,
			arguments: func(journalID, _, _ string) []any { return []any{largeBytes, journalID} },
		},
		{
			name:          "nine MiB numeric line storage before header",
			mutationStage: "header", table: "journal_lines",
			update:    `UPDATE journal_lines SET debit_minor=zeroblob(?) WHERE id=?`,
			arguments: func(_, factLineID, _ string) []any { return []any{largeBytes, factLineID} },
		},
		{
			name:          "nine MiB numeric line storage after header",
			mutationStage: "lines", table: "journal_lines",
			update:    `UPDATE journal_lines SET debit_minor=zeroblob(?) WHERE id=?`,
			arguments: func(_, factLineID, _ string) []any { return []any{largeBytes, factLineID} },
		},
		{
			name:          "nine MiB line memo growth after header",
			mutationStage: "lines",
			table:         "journal_lines",
			update:        `UPDATE journal_lines SET memo=CAST(zeroblob(?) AS TEXT) WHERE id=?`,
			arguments: func(_, factLineID, _ string) []any {
				return []any{largeBytes, factLineID}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newTaxFactReportingFixture(t)
			journalID, factLineID := taxFactReportingID(19101), taxFactReportingID(19102)
			seedTaxFactReportingDraft(t, fixture.tx, fixture.accounts, fixture.now,
				journalID, "SYSTEM", 0, "late-body-mask-source", factLineID,
				taxFactReportingID(19103), "late-body-mask-fact", "ADJUSTMENT", nil)

			foreignOrgID := taxFactReportingID(19199)
			if tc.foreignOwner {
				foreignOrgID = addBASLegacyCashOtherOwner(t, fixture)
			}

			probe := &journalReportingLateBodyMutationProbe{
				Transaction: fixture.tx, mutationStage: tc.mutationStage,
				table: tc.table, update: tc.update,
				arguments: tc.arguments(journalID, factLineID, foreignOrgID),
			}
			repository, err := accounting.NewJournalRepository(probe)
			if err != nil {
				t.Fatalf("NewJournalRepository(probe) = %v", err)
			}

			runtime.GC()
			var before runtime.MemStats
			runtime.ReadMemStats(&before)
			record, captureErr := repository.GetForReportingWithPayloadBudget(
				fixture.ctx, accountTestOrganisationID, journalID, int64(journalReportingCaptureByteLimit))
			var after runtime.MemStats
			runtime.ReadMemStats(&after)

			if record != nil || !errors.Is(captureErr, accounting.ErrJournalRepository) || !probe.mutated {
				t.Fatalf("late body mutation returned record=%t err=%v mutated=%t", record != nil, captureErr, probe.mutated)
			}
			if probe.headerBodyQueries != 1 {
				t.Fatalf("header body queries=%d; want one guarded header statement", probe.headerBodyQueries)
			}
			if tc.mutationStage == "header" && probe.lineBodyQueries != 0 {
				t.Fatalf("header guard failed after %d line-body queries; want rejection before line hydration", probe.lineBodyQueries)
			}
			if tc.mutationStage == "lines" && probe.lineBodyQueries != 1 {
				t.Fatalf("late line mutation body queries=%d; want one guarded line statement", probe.lineBodyQueries)
			}
			if allocated := after.TotalAlloc - before.TotalAlloc; allocated > journalBodyMaskCopyCeiling {
				t.Fatalf("masked late body mutation allocated %d Go bytes; want less than %d (the changed cell is %d bytes)",
					allocated, journalBodyMaskCopyCeiling, largeBytes)
			}
			if !fixture.tx.Authenticated() {
				t.Fatal("reporting rejection closed or invalidated the caller-owned SQLCipher transaction")
			}
		})
	}
}

type journalReportingLateBodyMutationProbe struct {
	*sqlcipher.Transaction
	mutationStage     string
	table             string
	update            string
	arguments         []any
	mutated           bool
	headerBodyQueries int
	lineBodyQueries   int
}

func (probe *journalReportingLateBodyMutationProbe) QueryContext(
	ctx context.Context, query string, arguments ...any,
) (*sql.Rows, error) {
	compact := strings.ToLower(strings.Join(strings.Fields(query), ""))
	headerBody := strings.Contains(query, journalBodyMaskHeaderMarker) ||
		strings.HasPrefix(compact, "selectid,organisation_id,version,source_type,source_kind,source_id,source_revision,")
	lineBody := strings.Contains(query, journalBodyMaskLinesMarker) ||
		(strings.HasPrefix(compact, "selectid,journal_id,line_number,account_id,debit_minor,credit_minor,") && strings.Contains(compact, "limit?"))
	if headerBody {
		probe.headerBodyQueries++
	}
	if lineBody {
		probe.lineBodyQueries++
	}
	shouldMutate := (probe.mutationStage == "header" && headerBody) || (probe.mutationStage == "lines" && lineBody)
	if shouldMutate && !probe.mutated {
		if _, err := probe.Transaction.ExecContext(ctx, "PRAGMA ignore_check_constraints=ON"); err != nil {
			return nil, err
		}
		if err := taxFactSharedBudgetMutateWithTriggersDisabled(
			probe.Transaction, probe.table, probe.update, probe.arguments...,
		); err != nil {
			return nil, err
		}
		if _, err := probe.Transaction.ExecContext(ctx, "PRAGMA ignore_check_constraints=OFF"); err != nil {
			return nil, err
		}
		probe.mutated = true
	}
	return probe.Transaction.QueryContext(ctx, query, arguments...)
}

func TestJournalRepositoryReportingBodyMaskPreservesEmptyLineLegacyHash(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	const (
		journalID = "legacy-empty-lines"
		sourceID  = "legacy-empty-lines-source"
		memo      = "empty raw projection fixture"
		date      = "2026-08-10"
	)
	createdAt := fixture.now.Format(time.RFC3339Nano)
	if _, err := fixture.tx.ExecContext(fixture.ctx, `
        INSERT INTO journals(id,organisation_id,version,source_type,source_kind,source_id,
            source_revision,state,journal_date,description,total_debits_minor,total_credits_minor,
            currency_code,financial_revision,created_at)
        VALUES(?,?,1,'SYSTEM',0,?,1,'DRAFT',?,?,1,1,'AUD',1,?)`,
		journalID, accountTestOrganisationID, sourceID, date, memo, createdAt); err != nil {
		t.Fatalf("insert legacy empty-line journal: %v", err)
	}

	record, err := fixture.repository.GetForReportingWithPayloadBudget(
		fixture.ctx, accountTestOrganisationID, journalID, int64(journalReportingCaptureByteLimit))
	if err != nil || record == nil {
		t.Fatalf("empty-line reporting read = %#v, %v", record, err)
	}
	expectedJournal := &tammyv1.Journal{
		Id: journalID, OrganisationId: accountTestOrganisationID, Version: 1,
		State:             tammyv1.JournalState_JOURNAL_STATE_UNSPECIFIED,
		Source:            tammyv1.JournalSource_JOURNAL_SOURCE_UNSPECIFIED,
		PostingDate:       &tammyv1.CivilDate{Year: 2026, Month: 8, Day: 10},
		Memo:              memo,
		TotalDebits:       &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 1},
		TotalCredits:      &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 1},
		FinancialRevision: 1,
	}
	if !proto.Equal(record.Journal, expectedJournal) || len(record.Journal.Lines) != 0 {
		t.Fatalf("empty-line projection = %#v; want the historical header and no lines", record.Journal)
	}
	wantBytes := int64(512 + len(journalID) + len(accountTestOrganisationID) + len("SYSTEM") +
		len(sourceID) + len("DRAFT") + len(date) + len(memo) + len("AUD") + len(createdAt))
	if record.PayloadBytes != wantBytes {
		t.Fatalf("empty-line payload bytes = %d; want header-only retained bytes %d", record.PayloadBytes, wantBytes)
	}
	wantHash, err := bodyMaskExpectedEmptyJournalHash(
		expectedJournal, accountTestOrganisationID, journalID, sourceID, memo, date, createdAt)
	if err != nil || !bytes.Equal(record.ContentHash, wantHash) {
		t.Fatalf("empty-line canonical content hash = %x, want %x (marshal error %v)",
			record.ContentHash, wantHash, err)
	}
	repeated, err := fixture.repository.GetForReportingWithPayloadBudget(
		fixture.ctx, accountTestOrganisationID, journalID, int64(journalReportingCaptureByteLimit))
	if err != nil || repeated == nil || !bytes.Equal(repeated.ContentHash, wantHash) {
		t.Fatalf("repeated empty-line reporting hash = %#v, %v; want %x", repeated, err, wantHash)
	}
}

func bodyMaskExpectedEmptyJournalHash(
	journal *tammyv1.Journal,
	organisationID, journalID, sourceID, memo, date, createdAt string,
) ([]byte, error) {
	journalProto, err := (proto.MarshalOptions{Deterministic: true}).Marshal(journal)
	if err != nil {
		return nil, err
	}
	raw := &bytes.Buffer{}
	bodyMaskAppendRawFrame(raw, []byte("journal-row/v1"))
	bodyMaskAppendRawFrame(raw, []byte(journalID))
	bodyMaskAppendRawFrame(raw, []byte(organisationID))
	bodyMaskAppendRawInt64(raw, 1) // version
	bodyMaskAppendRawFrame(raw, []byte("SYSTEM"))
	bodyMaskAppendRawInt64(raw, 0) // source_kind
	bodyMaskAppendRawFrame(raw, []byte(sourceID))
	bodyMaskAppendRawInt64(raw, 1) // source_revision
	bodyMaskAppendRawFrame(raw, []byte("DRAFT"))
	bodyMaskAppendRawFrame(raw, []byte(date))
	bodyMaskAppendRawFrame(raw, []byte(memo))
	bodyMaskAppendRawNullableString(raw, nil) // reversal_of_journal_id
	bodyMaskAppendRawNullableString(raw, nil) // reversed_by_journal_id
	bodyMaskAppendRawInt64(raw, 1)            // total_debits_minor
	bodyMaskAppendRawInt64(raw, 1)            // total_credits_minor
	bodyMaskAppendRawFrame(raw, []byte("AUD"))
	bodyMaskAppendRawInt64(raw, 1)            // financial_revision
	bodyMaskAppendRawNullableString(raw, nil) // posted_at
	bodyMaskAppendRawFrame(raw, []byte(createdAt))
	emptyLines := &bytes.Buffer{}
	bodyMaskAppendRawInt64(emptyLines, 0) // canonical line count, with no line frames
	bodyMaskAppendRawFrame(raw, emptyLines.Bytes())

	digest := sha256.New()
	bodyMaskAppendHashFrame(digest, []byte("tammy/accounting/journal-reporting/v1"))
	bodyMaskAppendHashFrame(digest, []byte(organisationID))
	bodyMaskAppendHashFrame(digest, []byte(journalID))
	bodyMaskAppendHashFrame(digest, journalProto)
	bodyMaskAppendHashFrame(digest, raw.Bytes())
	return digest.Sum(nil), nil
}

func bodyMaskAppendRawFrame(buffer *bytes.Buffer, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = buffer.Write(length[:])
	_, _ = buffer.Write(value)
}

func bodyMaskAppendRawInt64(buffer *bytes.Buffer, value int64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(value))
	_, _ = buffer.Write(encoded[:])
}

func bodyMaskAppendRawNullableString(buffer *bytes.Buffer, value *string) {
	if value == nil {
		_, _ = buffer.Write([]byte{0})
		return
	}
	_, _ = buffer.Write([]byte{1})
	bodyMaskAppendRawFrame(buffer, []byte(*value))
}

func bodyMaskAppendHashFrame(digest hash.Hash, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = digest.Write(length[:])
	_, _ = digest.Write(value)
}

func TestJournalRepositoryCaptureMasksLateJournalGrowthAgainstReservedParent(t *testing.T) {
	const largeBytes = 9 * 1024 * 1024
	fixture := newTaxFactReportingFixture(t)
	journalID, factLineID := taxFactReportingID(19201), taxFactReportingID(19202)
	seedTaxFactReportingDraft(t, fixture.tx, fixture.accounts, fixture.now,
		journalID, "SYSTEM", 0, "capture-mask-source", factLineID,
		taxFactReportingID(19203), "capture-mask-fact", "ADJUSTMENT", nil)

	probe := &journalReportingCaptureBodyMaskProbe{Transaction: fixture.tx,
		journalID: journalID, largeBytes: largeBytes}
	repository, err := accounting.NewJournalRepository(probe)
	if err != nil {
		t.Fatalf("NewJournalRepository(probe) = %v", err)
	}
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	capture, err := repository.CaptureTaxFactsForReportingWithPayloadBudget(
		fixture.ctx, accountTestOrganisationID, int64(journalReportingCaptureByteLimit))
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	if capture != nil || !errors.Is(err, accounting.ErrJournalRepository) || !probe.mutated {
		t.Fatalf("late capture mutation returned capture=%t err=%v mutated=%t", capture != nil, err, probe.mutated)
	}
	if probe.headerBodyQueries != 1 || probe.lineBodyQueries != 0 {
		t.Fatalf("capture body queries = header:%d lines:%d; want one guarded header and no line hydration",
			probe.headerBodyQueries, probe.lineBodyQueries)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > journalBodyMaskCopyCeiling {
		t.Fatalf("late capture mutation allocated %d Go bytes; want less than %d (changed cell %d bytes)",
			allocated, journalBodyMaskCopyCeiling, largeBytes)
	}
	if !fixture.tx.Authenticated() {
		t.Fatal("capture rejection closed or invalidated the caller-owned SQLCipher transaction")
	}
}

type journalReportingCaptureBodyMaskProbe struct {
	*sqlcipher.Transaction
	journalID         string
	largeBytes        int
	mutated           bool
	headerBodyQueries int
	lineBodyQueries   int
}

func (probe *journalReportingCaptureBodyMaskProbe) QueryContext(
	ctx context.Context, query string, arguments ...any,
) (*sql.Rows, error) {
	headerBody := strings.Contains(query, journalBodyMaskHeaderMarker) ||
		strings.Contains(query, "SELECT id, organisation_id, version, source_type, source_kind")
	lineBody := strings.Contains(query, journalBodyMaskLinesMarker) ||
		strings.Contains(query, "SELECT id, journal_id, line_number, account_id")
	if headerBody {
		probe.headerBodyQueries++
	}
	if lineBody {
		probe.lineBodyQueries++
	}
	if headerBody && !probe.mutated {
		if _, err := probe.Transaction.ExecContext(ctx, "PRAGMA ignore_check_constraints=ON"); err != nil {
			return nil, err
		}
		if err := taxFactSharedBudgetMutateWithTriggersDisabled(probe.Transaction, "journals",
			`UPDATE journals SET description=CAST(zeroblob(?) AS TEXT) WHERE id=?`,
			probe.largeBytes, probe.journalID); err != nil {
			_, _ = probe.Transaction.ExecContext(ctx, "PRAGMA ignore_check_constraints=OFF")
			return nil, err
		}
		if _, err := probe.Transaction.ExecContext(ctx, "PRAGMA ignore_check_constraints=OFF"); err != nil {
			return nil, err
		}
		probe.mutated = true
	}
	return probe.Transaction.QueryContext(ctx, query, arguments...)
}

func TestJournalRepositoryCaptureHonorsCancellationBeforeHeaderScan(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	journalID, factLineID := taxFactReportingID(19301), taxFactReportingID(19302)
	seedTaxFactReportingDraft(t, fixture.tx, fixture.accounts, fixture.now,
		journalID, "SYSTEM", 0, "cancel-mask-source", factLineID,
		taxFactReportingID(19303), "cancel-mask-fact", "ADJUSTMENT", nil)
	stateBefore := journalReportingPreflightState(t, fixture)

	ctx, cancel := context.WithCancel(fixture.ctx)
	defer cancel()
	probe := &journalReportingCaptureCancelProbe{Transaction: fixture.tx, cancel: cancel}
	repository, err := accounting.NewJournalRepository(probe)
	if err != nil {
		t.Fatalf("NewJournalRepository(probe) = %v", err)
	}
	capture, err := repository.CaptureTaxFactsForReportingWithPayloadBudget(
		ctx, accountTestOrganisationID, int64(journalReportingCaptureByteLimit))
	if capture != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("late-canceled capture = %#v, %v; want nil and context.Canceled", capture, err)
	}
	if probe.headerBodyQueries != 1 || probe.lineBodyQueries != 0 {
		t.Fatalf("canceled capture body queries = header:%d lines:%d; want cancellation before line hydration",
			probe.headerBodyQueries, probe.lineBodyQueries)
	}
	if got := journalReportingPreflightState(t, fixture); !equalJournalReportingPreflightState(got, stateBefore) {
		t.Fatalf("canceled body read changed revisions or SQLite write count: before=%v after=%v", stateBefore, got)
	}
	if !fixture.tx.Authenticated() {
		t.Fatal("canceled body read closed or invalidated the caller-owned SQLCipher transaction")
	}
}

type journalReportingCaptureCancelProbe struct {
	*sqlcipher.Transaction
	cancel            context.CancelFunc
	headerBodyQueries int
	lineBodyQueries   int
}

func (probe *journalReportingCaptureCancelProbe) QueryContext(
	ctx context.Context, query string, arguments ...any,
) (*sql.Rows, error) {
	headerBody := strings.Contains(query, journalBodyMaskHeaderMarker) ||
		strings.Contains(query, "SELECT id, organisation_id, version, source_type, source_kind")
	lineBody := strings.Contains(query, journalBodyMaskLinesMarker) ||
		strings.Contains(query, "SELECT id, journal_id, line_number, account_id")
	if headerBody {
		probe.headerBodyQueries++
	}
	if lineBody {
		probe.lineBodyQueries++
	}
	rows, err := probe.Transaction.QueryContext(ctx, query, arguments...)
	if headerBody && err == nil && probe.cancel != nil {
		cancel := probe.cancel
		probe.cancel = nil
		cancel() // cancel after QueryContext returns, before the production caller scans rows
	}
	return rows, err
}
