//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package accounting_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
)

func TestTaxFactOwnerScopeCanonicalWriterAndSameByteBlobCorruption(t *testing.T) {
	f, posted, sourceHash, ruleHash := newTaxFactOwnerScopePosting(t, 18101)
	assertTaxFactOwnerScopeWriterRows(t, f, posted.factID)
	stateBefore := journalReportingPreflightState(t, f)

	probe := &taxFactOwnerScopeProbe{Transaction: f.tx}
	repository, err := accounting.NewJournalRepository(probe)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := repository.SummarizeTaxFactBASKeys(f.ctx, accountTestOrganisationID)
	if err != nil || summary == nil || summary.ScalarKeyCount != 1 || summary.ScalarBytes <= 0 ||
		probe.summaryReads != 1 || probe.keyReads != 0 || probe.bodyHydrationQueries != 0 {
		t.Fatalf("canonical scalar summary = %#v, %v; summary/key/body queries %d/%d/%d",
			summary, err, probe.summaryReads, probe.keyReads, probe.bodyHydrationQueries)
	}
	keys, err := repository.ReadTaxFactBASKeys(
		f.ctx, accountTestOrganisationID, *summary, summary.ScalarKeyCount, summary.ScalarBytes)
	if err != nil || len(keys) != 1 || keys[0].ID != posted.factID ||
		keys[0].OrganisationID != accountTestOrganisationID || probe.keyReads != 1 ||
		probe.summaryReads != 2 || probe.bodyHydrationQueries != 0 {
		t.Fatalf("canonical scalar keys = %#v, %v; summary/key/body queries %d/%d/%d",
			keys, err, probe.summaryReads, probe.keyReads, probe.bodyHydrationQueries)
	}
	probe.bodyHydrationQueries = 0
	preflight, err := repository.PreflightTaxFactsForReportingWithPayloadBudget(
		f.ctx, accountTestOrganisationID, int64(journalReportingCaptureByteLimit))
	if err != nil || preflight == nil || preflight.FactCount != 1 ||
		preflight.JournalCount != 1 || probe.bodyHydrationQueries != 0 {
		t.Fatalf("canonical reporting preflight = %#v, %v with %d body queries",
			preflight, err, probe.bodyHydrationQueries)
	}
	probe.bodyHydrationQueries = 0
	capture, err := repository.CaptureTaxFactsForReportingWithPayloadBudget(
		f.ctx, accountTestOrganisationID, preflight.PayloadBytes)
	if err != nil || capture == nil || capture.FactCount != 1 || len(capture.Facts) != 1 ||
		capture.Facts[0].ID != posted.factID || capture.PayloadBytes != preflight.PayloadBytes ||
		probe.bodyHydrationQueries == 0 {
		t.Fatalf("canonical reporting capture = %#v, %v with %d body queries",
			capture, err, probe.bodyHydrationQueries)
	}
	assertTaxFactReportingPosting(t, f.repository, capture.Facts[0], posted, sourceHash[:], ruleHash[:], f.now)
	assertTaxFactOwnerScopeUnchanged(t, f, stateBefore, "canonical owner reads")

	// Each static corruption check runs independently. A scalar failure cannot
	// prevent the reporting preflight and capture cases from running.
	for _, testName := range []string{"scalar summary", "scalar key read", "reporting preflight", "reporting capture"} {
		testName := testName
		t.Run(testName, func(t *testing.T) {
			f, posted, _, _ := newTaxFactOwnerScopePosting(t, 18201)
			probe := &taxFactOwnerScopeProbe{Transaction: f.tx}
			repository, err := accounting.NewJournalRepository(probe)
			if err != nil {
				t.Fatal(err)
			}
			var reservation *accounting.TaxFactBASScalarSummary
			if testName == "scalar key read" {
				reservation, err = repository.SummarizeTaxFactBASKeys(f.ctx, accountTestOrganisationID)
				if err != nil || reservation == nil || reservation.ScalarKeyCount != 1 {
					t.Fatalf("canonical reservation before corruption = %#v, %v", reservation, err)
				}
			}
			owner, err := storeTaxFactOwnerScopeBlob(f.ctx, f.tx, posted.factID, accountTestOrganisationID)
			if err != nil {
				t.Fatalf("store same-byte owner as BLOB: %v", err)
			}
			if !bytes.Equal(owner, []byte(accountTestOrganisationID)) {
				t.Fatalf("stored owner bytes = %x; want exact UUID bytes %x", owner, []byte(accountTestOrganisationID))
			}
			assertTaxFactOwnerScopeJournalOwner(t, f, posted.factID, accountTestOrganisationID)
			stateAfterMutation := journalReportingPreflightState(t, f)

			var gotErr error
			switch testName {
			case "scalar summary":
				summary, readErr := repository.SummarizeTaxFactBASKeys(f.ctx, accountTestOrganisationID)
				gotErr = readErr
				if summary != nil || !errors.Is(readErr, accounting.ErrJournalRepository) ||
					probe.summaryReads != 1 || probe.keyReads != 0 || probe.bodyHydrationQueries != 0 {
					t.Fatalf("same-byte BLOB scalar summary = %#v, %v; summary/key/body queries %d/%d/%d",
						summary, readErr, probe.summaryReads, probe.keyReads, probe.bodyHydrationQueries)
				}
			case "scalar key read":
				probe.summaryReads, probe.keyReads, probe.bodyHydrationQueries = 0, 0, 0
				keys, readErr := repository.ReadTaxFactBASKeys(
					f.ctx, accountTestOrganisationID, *reservation, 1, reservation.ScalarBytes)
				gotErr = readErr
				if keys != nil || !errors.Is(readErr, accounting.ErrJournalRepository) ||
					probe.summaryReads != 1 || probe.keyReads != 0 || probe.bodyHydrationQueries != 0 {
					t.Fatalf("same-byte BLOB scalar read = %#v, %v; summary/key/body queries %d/%d/%d",
						keys, readErr, probe.summaryReads, probe.keyReads, probe.bodyHydrationQueries)
				}
			case "reporting preflight":
				preflight, readErr := repository.PreflightTaxFactsForReportingWithPayloadBudget(
					f.ctx, accountTestOrganisationID, int64(journalReportingCaptureByteLimit))
				gotErr = readErr
				if preflight != nil || !errors.Is(readErr, accounting.ErrJournalRepository) ||
					probe.bodyHydrationQueries != 0 {
					t.Fatalf("same-byte BLOB reporting preflight = %#v, %v with %d body queries; want rejection before hydration",
						preflight, readErr, probe.bodyHydrationQueries)
				}
			case "reporting capture":
				capture, readErr := repository.CaptureTaxFactsForReportingWithPayloadBudget(
					f.ctx, accountTestOrganisationID, int64(journalReportingCaptureByteLimit))
				gotErr = readErr
				if capture != nil || !errors.Is(readErr, accounting.ErrJournalRepository) ||
					probe.bodyHydrationQueries != 0 {
					t.Fatalf("same-byte BLOB reporting capture = %#v, %v with %d body queries; want rejection before hydration",
						capture, readErr, probe.bodyHydrationQueries)
				}
			}
			if gotErr == nil {
				t.Fatal("corrupt same-byte BLOB owner unexpectedly succeeded")
			}
			assertTaxFactOwnerScopeUnchanged(t, f, stateAfterMutation, "same-byte BLOB owner read")
		})
	}
}

func TestTaxFactBASKeysRejectSameByteBlobOwnerDriftAfterEmptyReservation(t *testing.T) {
	f, posted, _, _ := newTaxFactOwnerScopePosting(t, 18301)
	foreignOwner := taxFactReportingID(18398)

	// Corruption-only scope control: the fact owner changes while its real
	// writer journal remains target-owned. This is not valid foreign history.
	if err := storeTaxFactOwnerScopeText(f.ctx, f.tx, posted.factID, foreignOwner); err != nil {
		t.Fatalf("move writer fact to unrelated TEXT owner: %v", err)
	}
	probe := &taxFactOwnerScopeProbe{Transaction: f.tx}
	repository, err := accounting.NewJournalRepository(probe)
	if err != nil {
		t.Fatal(err)
	}
	empty, err := repository.SummarizeTaxFactBASKeys(f.ctx, accountTestOrganisationID)
	if err != nil || empty == nil || empty.ScalarKeyCount != 0 || empty.ScalarBytes != 0 ||
		probe.summaryReads != 1 || probe.keyReads != 0 || probe.bodyHydrationQueries != 0 {
		t.Fatalf("fresh empty target reservation = %#v, %v; summary/key/body queries %d/%d/%d",
			empty, err, probe.summaryReads, probe.keyReads, probe.bodyHydrationQueries)
	}
	var stateAfterDrift []int64
	var storedOwner []byte
	probe.beforeKeyRead = func() error {
		var mutationErr error
		storedOwner, mutationErr = storeTaxFactOwnerScopeBlob(
			f.ctx, f.tx, posted.factID, accountTestOrganisationID)
		if mutationErr != nil {
			return mutationErr
		}
		stateAfterDrift = journalReportingPreflightState(t, f)
		return nil
	}
	keys, err := repository.ReadTaxFactBASKeys(f.ctx, accountTestOrganisationID, *empty, 0, 0)
	var bound *accounting.TaxFactReportingBoundError
	if keys != nil || !errors.Is(err, accounting.ErrTaxFactReportingBound) ||
		!errors.As(err, &bound) || bound.Resource != "BAS scalar keys" || bound.Limit != 0 {
		t.Fatalf("post-summary same-byte BLOB drift = %#v, %v; want the exact zero-key reservation sentinel", keys, err)
	}
	if !bytes.Equal(storedOwner, []byte(accountTestOrganisationID)) {
		t.Fatalf("drift owner bytes = %x; want exact UUID bytes %x", storedOwner, []byte(accountTestOrganisationID))
	}
	if probe.summaryReads != 2 || probe.keyReads != 1 || probe.bodyHydrationQueries != 0 {
		t.Fatalf("post-summary drift ran %d summary queries, %d guarded key queries, and %d body queries; want 2/1/0",
			probe.summaryReads, probe.keyReads, probe.bodyHydrationQueries)
	}
	if stateAfterDrift == nil || !f.tx.Authenticated() ||
		!equalJournalReportingPreflightState(stateAfterDrift, journalReportingPreflightState(t, f)) {
		t.Fatal("drift rejection changed state after the injected owner mutation or altered caller transaction")
	}
}

func TestTaxFactOwnerScopeLeavesUnrelatedBlobUUIDOutOfScope(t *testing.T) {
	f, posted, _, _ := newTaxFactOwnerScopePosting(t, 18401)
	unrelatedOwner := taxFactReportingID(18498)
	if _, err := storeTaxFactOwnerScopeBlob(f.ctx, f.tx, posted.factID, unrelatedOwner); err != nil {
		t.Fatalf("store unrelated UUID owner as BLOB: %v", err)
	}
	if bytes.Equal([]byte(unrelatedOwner), []byte(accountTestOrganisationID)) {
		t.Fatal("fixture unrelated owner unexpectedly equals requested organisation")
	}
	stateAfterMutation := journalReportingPreflightState(t, f)

	// This corrupt owner/parent mismatch only checks that an unrelated owner
	// remains outside the requested scope; it is not valid accounting history.
	probe := &taxFactOwnerScopeProbe{Transaction: f.tx}
	repository, err := accounting.NewJournalRepository(probe)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := repository.SummarizeTaxFactBASKeys(f.ctx, accountTestOrganisationID)
	if err != nil || summary == nil || summary.ScalarKeyCount != 0 || summary.ScalarBytes != 0 {
		t.Fatalf("unrelated BLOB scalar summary = %#v, %v; want no target-owned keys", summary, err)
	}
	keys, err := repository.ReadTaxFactBASKeys(f.ctx, accountTestOrganisationID, *summary, 0, 0)
	if err != nil || len(keys) != 0 {
		t.Fatalf("unrelated BLOB scalar keys = %#v, %v; want no target-owned keys", keys, err)
	}
	preflight, err := repository.PreflightTaxFactsForReportingWithPayloadBudget(
		f.ctx, accountTestOrganisationID, int64(journalReportingCaptureByteLimit))
	if err != nil || preflight == nil || preflight.FactCount != 0 || preflight.BasePayloadBytes != 0 {
		t.Fatalf("unrelated BLOB reporting preflight = %#v, %v; want no target-owned facts", preflight, err)
	}
	capture, err := repository.CaptureTaxFactsForReportingWithPayloadBudget(
		f.ctx, accountTestOrganisationID, int64(journalReportingCaptureByteLimit))
	if err != nil || capture == nil || capture.FactCount != 0 || len(capture.Facts) != 0 {
		t.Fatalf("unrelated BLOB reporting capture = %#v, %v; want an empty target capture", capture, err)
	}
	assertTaxFactOwnerScopeUnchanged(t, f, stateAfterMutation, "unrelated BLOB scope reads")
}

func TestTaxFactReportingCaptureAtomicallyRejectsOwnerDriftBeforeHydration(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		missingLineID string
	}{
		{name: "present parent"},
		{name: "missing journal-line parent", missingLineID: taxFactReportingID(18590)},
	} {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			f, posted, sourceHash, ruleHash := newTaxFactOwnerScopePosting(t, 18501)
			canonicalPreflight, err := f.repository.PreflightTaxFactsForReportingWithPayloadBudget(
				f.ctx, accountTestOrganisationID, int64(journalReportingCaptureByteLimit))
			if err != nil || canonicalPreflight == nil || canonicalPreflight.FactCount != 1 ||
				canonicalPreflight.JournalCount != 1 {
				t.Fatalf("canonical preflight = %#v, %v", canonicalPreflight, err)
			}
			canonicalCapture, err := f.repository.CaptureTaxFactsForReportingWithPayloadBudget(
				f.ctx, accountTestOrganisationID, canonicalPreflight.PayloadBytes)
			if err != nil || canonicalCapture == nil || canonicalCapture.FactCount != 1 ||
				len(canonicalCapture.Facts) != 1 || canonicalCapture.Facts[0].ID != posted.factID ||
				canonicalCapture.PayloadBytes != canonicalPreflight.PayloadBytes {
				t.Fatalf("canonical capture = %#v, %v; want exact preflight payload and the real writer fact",
					canonicalCapture, err)
			}
			assertTaxFactReportingPosting(t, f.repository, canonicalCapture.Facts[0], posted,
				sourceHash[:], ruleHash[:], f.now)

			// Corruption-only foreign TEXT scope control: the real parent journal
			// stays target-owned, so this is not valid foreign accounting history.
			foreignOwner := taxFactReportingID(18598)
			if err := storeTaxFactOwnerScopeText(f.ctx, f.tx, posted.factID, foreignOwner); err != nil {
				t.Fatalf("set corruption-only foreign TEXT fact owner control: %v", err)
			}
			foreignPreflight, err := f.repository.PreflightTaxFactsForReportingWithPayloadBudget(
				f.ctx, accountTestOrganisationID, int64(journalReportingCaptureByteLimit))
			if err != nil || foreignPreflight == nil || foreignPreflight.FactCount != 0 ||
				foreignPreflight.JournalCount != 0 || foreignPreflight.BasePayloadBytes != 0 {
				t.Fatalf("foreign TEXT preflight = %#v, %v; want empty target scope", foreignPreflight, err)
			}
			foreignCapture, err := f.repository.CaptureTaxFactsForReportingWithPayloadBudget(
				f.ctx, accountTestOrganisationID, int64(journalReportingCaptureByteLimit))
			if err != nil || foreignCapture == nil || foreignCapture.FactCount != 0 ||
				len(foreignCapture.Facts) != 0 {
				t.Fatalf("foreign TEXT capture = %#v, %v; want empty target scope", foreignCapture, err)
			}

			probe := &taxFactOwnerAtomicCaptureProbe{Transaction: f.tx}
			repository, err := accounting.NewJournalRepository(probe)
			if err != nil {
				t.Fatal(err)
			}
			var before runtime.MemStats
			var stateAfterMutation []int64
			probe.beforeCandidateHydration = func() error {
				if probe.preflightQueries != 1 {
					return fmt.Errorf("capture reached candidate query after %d preflight queries; want its own empty preflight",
						probe.preflightQueries)
				}
				if _, err := f.tx.ExecContext(f.ctx, "PRAGMA defer_foreign_keys=ON"); err != nil {
					return err
				}
				update := `UPDATE tax_facts SET organisation_id=CAST(? AS BLOB),
					source_id=zeroblob(9437184) WHERE id=?`
				arguments := []any{accountTestOrganisationID, posted.factID}
				if testCase.missingLineID != "" {
					update = `UPDATE tax_facts SET organisation_id=CAST(? AS BLOB),
						source_id=zeroblob(9437184), journal_line_id=? WHERE id=?`
					arguments = []any{accountTestOrganisationID, testCase.missingLineID, posted.factID}
				}
				if err := taxFactSharedBudgetMutateWithTriggersDisabled(
					f.tx, "tax_facts", update, arguments...); err != nil {
					return fmt.Errorf("install same-byte owner and oversized-body drift: %w", err)
				}

				var ownerType, sourceType, storedLineID string
				var ownerBytes []byte
				var ownerLength, sourceLength int64
				if err := f.tx.QueryRowContext(f.ctx, `
					SELECT typeof(fact.organisation_id), CAST(fact.organisation_id AS BLOB),
					       length(CAST(fact.organisation_id AS BLOB)),
					       typeof(fact.source_id), length(CAST(fact.source_id AS BLOB)),
					       fact.journal_line_id
					FROM tax_facts AS fact WHERE fact.id=?`, posted.factID,
				).Scan(&ownerType, &ownerBytes, &ownerLength, &sourceType, &sourceLength, &storedLineID); err != nil {
					return err
				}
				if ownerType != "blob" || !bytes.Equal(ownerBytes, []byte(accountTestOrganisationID)) ||
					ownerLength != 36 || sourceType != "blob" || sourceLength != 9437184 {
					return fmt.Errorf("drift fixture owner=%q/%x/%d source=%q/%d; want exact 36-byte owner BLOB and 9 MiB source BLOB",
						ownerType, ownerBytes, ownerLength, sourceType, sourceLength)
				}
				var parentCount int
				if err := f.tx.QueryRowContext(f.ctx, `
					SELECT count(line.id)
					FROM tax_facts AS fact
					LEFT JOIN journal_lines AS line ON line.id=fact.journal_line_id
					WHERE fact.id=?`, posted.factID).Scan(&parentCount); err != nil {
					return err
				}
				if testCase.missingLineID == "" {
					if storedLineID != posted.journal.Lines[1].Id || parentCount != 1 {
						return fmt.Errorf("present-parent drift line=%q count=%d; want writer line %q",
							storedLineID, parentCount, posted.journal.Lines[1].Id)
					}
				} else if storedLineID != testCase.missingLineID || parentCount != 0 {
					return fmt.Errorf("missing-parent drift line=%q count=%d; want absent canonical line %q",
						storedLineID, parentCount, testCase.missingLineID)
				}

				stateAfterMutation = journalReportingPreflightState(t, f)
				runtime.ReadMemStats(&before)
				probe.mutatedBeforeCandidateHydration = true
				return nil
			}
			capture, err := repository.CaptureTaxFactsForReportingWithPayloadBudget(
				f.ctx, accountTestOrganisationID, int64(journalReportingCaptureByteLimit))
			var after runtime.MemStats
			runtime.ReadMemStats(&after)
			allocated := after.TotalAlloc - before.TotalAlloc
			if capture != nil || !errors.Is(err, accounting.ErrJournalRepository) ||
				!probe.mutatedBeforeCandidateHydration || probe.preflightQueries != 1 ||
				probe.candidateQueries != 1 {
				t.Fatalf("owner drift capture=%#v err=%v preflight queries=%d candidate queries=%d hook=%t",
					capture, err, probe.preflightQueries, probe.candidateQueries,
					probe.mutatedBeforeCandidateHydration)
			}
			if allocated >= 2*1024*1024 {
				t.Fatalf("candidate rejection copied the 9 MiB fact body: allocated=%d bytes", allocated)
			}
			if len(err.Error()) > 1024 {
				t.Fatalf("candidate rejection expanded the 9 MiB cell into a %d-byte error", len(err.Error()))
			}
			if probe.journalBodyQueries != 0 || probe.lineBodyQueries != 0 {
				t.Fatalf("rejected candidate proceeded to %d journal and %d line body queries",
					probe.journalBodyQueries, probe.lineBodyQueries)
			}
			if stateAfterMutation == nil || !f.tx.Authenticated() ||
				!equalJournalReportingPreflightState(stateAfterMutation, journalReportingPreflightState(t, f)) {
				t.Fatal("same-byte BLOB candidate rejection changed stored state or caller transaction")
			}
		})
	}
}

type taxFactOwnerScopeProbe struct {
	*sqlcipher.Transaction
	summaryReads         int
	keyReads             int
	bodyHydrationQueries int
	beforeKeyRead        func() error
}

func (probe *taxFactOwnerScopeProbe) QueryContext(
	ctx context.Context, query string, arguments ...any,
) (*sql.Rows, error) {
	if strings.Contains(query, "/* BAS tax fact scalar summary */") {
		probe.summaryReads++
	}
	if strings.Contains(query, "/* BAS tax fact scalar keys */") {
		probe.keyReads++
		if probe.beforeKeyRead != nil {
			change := probe.beforeKeyRead
			probe.beforeKeyRead = nil
			if err := change(); err != nil {
				return nil, err
			}
		}
	}
	if strings.Contains(query, "SELECT fact.id, fact.organisation_id, fact.journal_line_id, fact.tax_code") ||
		strings.Contains(query, journalBodyMaskHeaderMarker) ||
		strings.Contains(query, journalBodyMaskLinesMarker) ||
		strings.Contains(query, "SELECT id, organisation_id, version, source_type, source_kind") ||
		strings.Contains(query, "SELECT id, journal_id, line_number, account_id") {
		probe.bodyHydrationQueries++
	}
	return probe.Transaction.QueryContext(ctx, query, arguments...)
}

type taxFactOwnerAtomicCaptureProbe struct {
	*sqlcipher.Transaction
	preflightQueries                int
	candidateQueries                int
	journalBodyQueries              int
	lineBodyQueries                 int
	mutatedBeforeCandidateHydration bool
	beforeCandidateHydration        func() error
}

func (probe *taxFactOwnerAtomicCaptureProbe) QueryContext(
	ctx context.Context, query string, arguments ...any,
) (*sql.Rows, error) {
	if strings.Contains(query, "SELECT count(*), total(payload_bytes)") {
		probe.preflightQueries++
	}
	candidateHydration := strings.Contains(
		query, "SELECT fact.id, fact.organisation_id, fact.journal_line_id, fact.tax_code")
	if candidateHydration {
		probe.candidateQueries++
		if probe.beforeCandidateHydration != nil {
			change := probe.beforeCandidateHydration
			probe.beforeCandidateHydration = nil
			if err := change(); err != nil {
				return nil, err
			}
		}
	}
	if strings.Contains(query, journalBodyMaskHeaderMarker) ||
		strings.Contains(query, "SELECT id, organisation_id, version, source_type, source_kind") {
		probe.journalBodyQueries++
	}
	if strings.Contains(query, journalBodyMaskLinesMarker) ||
		strings.Contains(query, "SELECT id, journal_id, line_number, account_id") {
		probe.lineBodyQueries++
	}
	return probe.Transaction.QueryContext(ctx, query, arguments...)
}

func newTaxFactOwnerScopePosting(
	t *testing.T, seed int,
) (*taxFactReportingFixture, suppliedHashPosting, [sha256.Size]byte, [sha256.Size]byte) {
	t.Helper()
	f := newTaxFactReportingFixture(t)
	sourceHash := sha256.Sum256([]byte(fmt.Sprintf("owner-scope source %d", seed)))
	ruleHash := sha256.Sum256([]byte(fmt.Sprintf("owner-scope rule %d", seed)))
	posted := f.post(t, seed, taxFactReportingID(seed+90), sourceHash[:], ruleHash[:])
	return f, posted, sourceHash, ruleHash
}

func assertTaxFactOwnerScopeWriterRows(
	t *testing.T, f *taxFactReportingFixture, factID string,
) {
	t.Helper()
	var factType, journalType string
	var factOwner, journalOwner string
	err := f.tx.QueryRowContext(f.ctx, `
		SELECT typeof(fact.organisation_id), fact.organisation_id,
		       typeof(journal.organisation_id), journal.organisation_id
		FROM tax_facts AS fact
		JOIN journal_lines AS line ON line.id=fact.journal_line_id
		JOIN journals AS journal ON journal.id=line.journal_id
		WHERE fact.id=?`, factID).Scan(&factType, &factOwner, &journalType, &journalOwner)
	if err != nil || factType != "text" || factOwner != accountTestOrganisationID ||
		journalType != "text" || journalOwner != accountTestOrganisationID {
		t.Fatalf("real writer fact/journal owner = %q:%q / %q:%q, %v; want canonical TEXT owners",
			factType, factOwner, journalType, journalOwner, err)
	}
}

func assertTaxFactOwnerScopeJournalOwner(
	t *testing.T, f *taxFactReportingFixture, factID, expectedOwner string,
) {
	t.Helper()
	var journalType, journalOwner string
	err := f.tx.QueryRowContext(f.ctx, `
		SELECT typeof(journal.organisation_id), journal.organisation_id
		FROM tax_facts AS fact
		JOIN journal_lines AS line ON line.id=fact.journal_line_id
		JOIN journals AS journal ON journal.id=line.journal_id
		WHERE fact.id=?`, factID).Scan(&journalType, &journalOwner)
	if err != nil || journalType != "text" || journalOwner != expectedOwner {
		t.Fatalf("fact's journal owner = %q:%q, %v; want unchanged canonical TEXT owner %q",
			journalType, journalOwner, err, expectedOwner)
	}
}

func assertTaxFactOwnerScopeUnchanged(
	t *testing.T, f *taxFactReportingFixture, expected []int64, operation string,
) {
	t.Helper()
	if !f.tx.Authenticated() ||
		!equalJournalReportingPreflightState(expected, journalReportingPreflightState(t, f)) {
		t.Fatalf("%s changed stored state after the owner mutation or altered caller transaction", operation)
	}
}

func storeTaxFactOwnerScopeBlob(
	ctx context.Context, tx *sqlcipher.Transaction, factID, owner string,
) ([]byte, error) {
	if _, err := tx.ExecContext(ctx, "PRAGMA defer_foreign_keys=ON"); err != nil {
		return nil, err
	}
	if err := taxFactSharedBudgetMutateWithTriggersDisabled(tx, "tax_facts",
		`UPDATE tax_facts SET organisation_id=CAST(? AS BLOB) WHERE id=?`, owner, factID); err != nil {
		return nil, err
	}
	var storageType string
	var actual []byte
	if err := tx.QueryRowContext(ctx,
		"SELECT typeof(organisation_id), CAST(organisation_id AS BLOB) FROM tax_facts WHERE id=?",
		factID).Scan(&storageType, &actual); err != nil {
		return nil, err
	}
	if storageType != "blob" || len(actual) != 36 || !bytes.Equal(actual, []byte(owner)) {
		return nil, fmt.Errorf("stored owner type=%q bytes=%x; want exact 36-byte BLOB UUID %x",
			storageType, actual, []byte(owner))
	}
	return actual, nil
}

func storeTaxFactOwnerScopeText(
	ctx context.Context, tx *sqlcipher.Transaction, factID, owner string,
) error {
	if _, err := tx.ExecContext(ctx, "PRAGMA defer_foreign_keys=ON"); err != nil {
		return err
	}
	if err := taxFactSharedBudgetMutateWithTriggersDisabled(tx, "tax_facts",
		"UPDATE tax_facts SET organisation_id=? WHERE id=?", owner, factID); err != nil {
		return err
	}
	var storageType, storedOwner string
	if err := tx.QueryRowContext(ctx,
		"SELECT typeof(organisation_id), organisation_id FROM tax_facts WHERE id=?",
		factID).Scan(&storageType, &storedOwner); err != nil {
		return err
	}
	if storageType != "text" || storedOwner != owner {
		return fmt.Errorf("stored owner type=%q value=%q; want TEXT owner %q", storageType, storedOwner, owner)
	}
	return nil
}
