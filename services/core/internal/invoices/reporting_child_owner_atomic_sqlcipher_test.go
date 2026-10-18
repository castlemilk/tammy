//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package invoices

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"runtime"
	"strings"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/proto"
)

const invoiceChildOwnerAtomicLargeCellBytes = 9 * 1024 * 1024

const (
	invoiceChildOwnerAtomicRootBodyMarker       = "select id,organisation_id,version,state,number"
	invoiceChildOwnerAtomicPostingBodyMarker    = "select posting.invoice_id,posting.organisation_id,posting.invoice_version"
	invoiceChildOwnerAtomicIncomeLineBodyMarker = "select income_line.invoice_id,income_line.sequence"
	invoiceChildOwnerAtomicReceiptBodyMarker    = "select receipt.id,receipt.organisation_id,receipt.invoice_id,receipt.payment_date"
	invoiceChildOwnerAtomicAllocationBodyMarker = "select allocation.payment_id,allocation.sequence"
	invoiceChildOwnerAtomicPostingIDsMarker     = "select posting.invoice_id,posting.organisation_id,posting.journal_id"
	invoiceChildOwnerAtomicReceiptIDsMarker     = "select receipt.id,receipt.organisation_id,receipt.invoice_id,receipt.journal_id"
)

// These late body drifts exercise the complete child census after its own
// fresh preflight. The foreign-parent and missing-parent graphs are corruption
// fixtures; the child-owner text mismatch keeps a canonical target parent.
func TestInvoiceChildOwnerLateBodyDriftIsRejectedBeforeLargeCellsReachGo(t *testing.T) {
	families := []struct {
		name, bodyMarker, table, ownerColumn, bodyColumn, keyColumn string
		wantBodyPrefixes                                            []string
	}{
		{
			name: "posting", bodyMarker: invoiceChildOwnerAtomicPostingBodyMarker,
			table: "invoice_income_postings_v1", ownerColumn: "organisation_id",
			bodyColumn: "created_at", keyColumn: "invoice_id",
			wantBodyPrefixes: []string{invoiceChildOwnerAtomicRootBodyMarker, invoiceChildOwnerAtomicPostingBodyMarker},
		},
		{
			name: "receipt", bodyMarker: invoiceChildOwnerAtomicReceiptBodyMarker,
			table: "invoice_payment_v1", ownerColumn: "organisation_id",
			bodyColumn: "payment_proto", keyColumn: "id",
			wantBodyPrefixes: []string{
				invoiceChildOwnerAtomicRootBodyMarker,
				invoiceChildOwnerAtomicPostingBodyMarker,
				invoiceChildOwnerAtomicIncomeLineBodyMarker,
				invoiceChildOwnerAtomicReceiptBodyMarker,
			},
		},
	}
	parentCases := []struct {
		name           string
		moveReferences bool
		foreignText    bool
	}{
		{name: "foreign-parent"},
		{name: "missing-parent", moveReferences: true},
		{name: "foreign-child-text-under-canonical-parent", foreignText: true},
	}

	for familyIndex, family := range families {
		family := family
		for parentIndex, parentCase := range parentCases {
			parentCase := parentCase
			t.Run(family.name+"/"+parentCase.name, func(t *testing.T) {
				fixture := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_CASH)
				invoiceID := invoiceReportingID(uint64(981000 + familyIndex*10 + parentIndex))
				invoice, payment := invoiceChildOwnerAtomicWriteGraph(t, fixture, invoiceID, true, 1000)
				port := invoiceChildOwnerAtomicAssertCanonicalWriterControl(t, fixture, invoice, payment, true, 2)

				foreignOwner := invoiceReportingID(982000 + uint64(familyIndex*10+parentIndex))
				missingInvoiceID := invoiceReportingID(983000 + uint64(familyIndex*10+parentIndex))
				childInvoiceID := invoice.Id
				baselineState := invoiceReportingBudgetState(t, fixture)
				if !parentCase.foreignText {
					childInvoiceID = invoiceChildOwnerAtomicMoveGraphOutOfTarget(
						t, fixture, invoice.Id, payment.Id, foreignOwner, missingInvoiceID, parentCase.moveReferences,
					)
					invoiceChildOwnerAtomicAssertReferenceParent(t, fixture, childInvoiceID, foreignOwner,
						!parentCase.moveReferences, family.name == "receipt")
					afterCorruption := invoiceReportingBudgetState(t, fixture)
					invoiceChildOwnerAtomicAssertEmptyBaseline(t, port, fixture)
					if afterBaseline := invoiceReportingBudgetState(t, fixture); afterBaseline != afterCorruption {
						t.Fatal("read-only empty baseline changed retained state")
					}
				} else if !fixture.tx.Authenticated() || invoiceReportingBudgetState(t, fixture) != baselineState {
					t.Fatal("canonical writer control changed retained state")
				}

				probe := &invoiceChildOwnerAtomicProbe{
					Transaction:  fixture.tx,
					targetMarker: invoiceChildOwnerAtomicCompact(family.bodyMarker),
				}
				probe.beforeTargetQuery = func() {
					if _, err := fixture.tx.ExecContext(fixture.ctx, "PRAGMA defer_foreign_keys=ON"); err != nil {
						t.Fatalf("defer late child-owner foreign key: %v", err)
					}
					ownerExpression := "CAST(? AS BLOB)"
					ownerArgument := any(orgID)
					if parentCase.foreignText {
						ownerExpression = "?"
						ownerArgument = foreignOwner
					}
					if family.name == "receipt" {
						if _, err := fixture.tx.ExecContext(fixture.ctx, "PRAGMA ignore_check_constraints=ON"); err != nil {
							t.Fatalf("temporarily disable receipt payload check: %v", err)
						}
					}
					mutationErr := invoiceReportingMutateWithTriggersRestored(fixture, family.table,
						"UPDATE "+family.table+" SET "+family.ownerColumn+"="+ownerExpression+","+family.bodyColumn+"=zeroblob(?) WHERE "+family.keyColumn+"=?",
						ownerArgument, invoiceChildOwnerAtomicLargeCellBytes, invoiceChildOwnerAtomicFamilyKey(family.name, childInvoiceID, payment.Id))
					var restoreChecksErr error
					if family.name == "receipt" {
						_, restoreChecksErr = fixture.tx.ExecContext(fixture.ctx, "PRAGMA ignore_check_constraints=OFF")
					}
					if mutationErr != nil || restoreChecksErr != nil {
						t.Fatalf("install late child owner/body corruption: %v; restore checks: %v", mutationErr, restoreChecksErr)
					}
					invoiceChildOwnerAtomicAssertLargeBodyCell(t, fixture, family.name, childInvoiceID, payment.Id,
						parentCase.foreignText, foreignOwner)
					parentOwner := foreignOwner
					if parentCase.foreignText {
						parentOwner = orgID
					}
					invoiceChildOwnerAtomicAssertReferenceParent(t, fixture, childInvoiceID, parentOwner,
						!parentCase.moveReferences || parentCase.foreignText, family.name == "receipt")
					probe.snapshotTargetCounters()
					probe.stateAfterMutation = invoiceReportingBudgetState(t, fixture)
					probe.hasStateAfterMutation = true
					runtime.ReadMemStats(&probe.memStatsAfterMutation)
					probe.hasMemStatsAfterMutation = true
				}

				capture, captureErr := NewReportingPort(probe)
				if captureErr != nil {
					t.Fatal(captureErr)
				}
				beforeCallState := invoiceReportingBudgetState(t, fixture)
				result, err := capture.CaptureBASWithPayloadBudget(
					fixture.ctx, orgID, invoiceReportingPeriod(), int64(maxBASReportingBytes))
				var after runtime.MemStats
				runtime.ReadMemStats(&after)

				if result != nil || !errors.Is(err, ErrRepository) || len(err.Error()) >= 512 ||
					!probe.targetHookFired || !probe.hasMemStatsAfterMutation {
					t.Fatalf("late %s owner/body drift capture=%#v err=%.256v hooked=%t; want nil result and short ErrRepository",
						family.name, result, err, probe.targetHookFired)
				}
				if allocated := after.TotalAlloc - probe.memStatsAfterMutation.TotalAlloc; allocated >= 2*1024*1024 {
					t.Fatalf("late %s owner/body drift copied its rejected 9 MiB cell: allocated=%d", family.name, allocated)
				}
				if len(probe.bodyPrefixes) != len(family.wantBodyPrefixes) ||
					!invoiceChildOwnerAtomicEqualStrings(probe.bodyPrefixes, family.wantBodyPrefixes) {
					t.Fatalf("late %s owner/body drift body prefixes=%v; want exact pre-failure sequence %v",
						family.name, probe.bodyPrefixes, family.wantBodyPrefixes)
				}
				if probe.journalIDsAtTarget != len(probe.journalIDPrefixes) ||
					probe.journalPayloadPreflightsAtTarget != probe.journalPayloadPreflights ||
					probe.journalBodyQueriesAtTarget != probe.journalBodyQueries {
					t.Fatalf("late %s owner/body drift ran a journal/line query after rejection: IDs %d/%d payload %d/%d bodies %d/%d",
						family.name, probe.journalIDsAtTarget, len(probe.journalIDPrefixes),
						probe.journalPayloadPreflightsAtTarget, probe.journalPayloadPreflights,
						probe.journalBodyQueriesAtTarget, probe.journalBodyQueries)
				}
				invoiceChildOwnerAtomicAssertMutationState(t, fixture, beforeCallState, probe)
			})
		}
	}
}

func TestInvoiceChildOwnerJournalIdentityDriftIsRejectedBeforeParentPreflight(t *testing.T) {
	families := []struct {
		name, idMarker, table, keyColumn string
	}{
		{name: "posting", idMarker: invoiceChildOwnerAtomicPostingIDsMarker,
			table: "invoice_income_postings_v1", keyColumn: "invoice_id"},
		{name: "receipt", idMarker: invoiceChildOwnerAtomicReceiptIDsMarker,
			table: "invoice_payment_v1", keyColumn: "id"},
	}
	for index, family := range families {
		family := family
		t.Run(family.name, func(t *testing.T) {
			fixture := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_CASH)
			invoice, payment := invoiceChildOwnerAtomicWriteGraph(t, fixture,
				invoiceReportingID(uint64(984000+index)), family.name == "receipt", 1000)
			invoiceChildOwnerAtomicAssertCanonicalWriterControl(t, fixture, invoice, payment,
				family.name == "receipt", 1+invoiceChildOwnerAtomicBoolInt(family.name == "receipt"))

			stateBefore := invoiceReportingBudgetState(t, fixture)
			probe := &invoiceChildOwnerAtomicProbe{
				Transaction:  fixture.tx,
				targetMarker: invoiceChildOwnerAtomicCompact(family.idMarker),
			}
			probe.beforeTargetQuery = func() {
				if _, err := fixture.tx.ExecContext(fixture.ctx, "PRAGMA defer_foreign_keys=ON"); err != nil {
					t.Fatalf("defer fixture journal foreign key: %v", err)
				}
				if err := invoiceReportingMutateWithTriggersRestored(fixture, family.table,
					"UPDATE "+family.table+" SET organisation_id=CAST(? AS BLOB),journal_id=zeroblob(?) WHERE "+family.keyColumn+"=?",
					orgID, invoiceChildOwnerAtomicLargeCellBytes, invoiceChildOwnerAtomicFamilyKey(family.name, invoice.Id, invoiceChildOwnerAtomicPaymentID(payment))); err != nil {
					t.Fatalf("install late journal identity corruption: %v", err)
				}
				invoiceChildOwnerAtomicAssertLargeJournalID(t, fixture, family.name, invoice.Id, payment)
				probe.snapshotTargetCounters()
				probe.stateAfterMutation = invoiceReportingBudgetState(t, fixture)
				probe.hasStateAfterMutation = true
				runtime.ReadMemStats(&probe.memStatsAfterMutation)
				probe.hasMemStatsAfterMutation = true
			}

			guardedPort, err := NewReportingPort(probe)
			if err != nil {
				t.Fatal(err)
			}
			result, err := guardedPort.CaptureBASWithPayloadBudget(
				fixture.ctx, orgID, invoiceReportingPeriod(), int64(maxBASReportingBytes))
			var after runtime.MemStats
			runtime.ReadMemStats(&after)
			if result != nil || !errors.Is(err, ErrRepository) || len(err.Error()) >= 512 ||
				!probe.targetHookFired || !probe.hasMemStatsAfterMutation {
				t.Fatalf("late %s journal-ID drift capture=%#v err=%.256v hooked=%t; want nil and short ErrRepository",
					family.name, result, err, probe.targetHookFired)
			}
			if allocated := after.TotalAlloc - probe.memStatsAfterMutation.TotalAlloc; allocated >= 2*1024*1024 {
				t.Fatalf("late %s journal-ID drift copied its rejected 9 MiB ID: allocated=%d", family.name, allocated)
			}
			wantIdentityPrefixes := []string{invoiceChildOwnerAtomicPostingIDsMarker}
			if family.name == "receipt" {
				wantIdentityPrefixes = append(wantIdentityPrefixes, invoiceChildOwnerAtomicReceiptIDsMarker)
			}
			if !invoiceChildOwnerAtomicEqualStrings(probe.journalIDPrefixes, wantIdentityPrefixes) {
				t.Fatalf("late %s journal-ID query prefixes=%v; want %v", family.name, probe.journalIDPrefixes, wantIdentityPrefixes)
			}
			if len(probe.bodyPrefixes) != 0 || probe.journalPayloadPreflights != 0 || probe.journalBodyQueries != 0 {
				t.Fatalf("late %s journal-ID drift reached body/parent payload reads: child=%v parent-preflights=%d journal-bodies=%d",
					family.name, probe.bodyPrefixes, probe.journalPayloadPreflights, probe.journalBodyQueries)
			}
			if probe.journalIDsAtTarget != len(probe.journalIDPrefixes) ||
				probe.journalPayloadPreflightsAtTarget != probe.journalPayloadPreflights ||
				probe.journalBodyQueriesAtTarget != probe.journalBodyQueries {
				t.Fatal("journal identity rejection allowed a later identity, journal payload, line, or body query")
			}
			invoiceChildOwnerAtomicAssertMutationState(t, fixture, stateBefore, probe)
		})
	}
}

func TestInvoiceChildOwnerJournalIdentityRejectsBlobOwnerWithNullJournal(t *testing.T) {
	fixture := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	invoice, _ := invoiceChildOwnerAtomicWriteGraph(t, fixture, invoiceReportingID(986001), false, 0)
	invoiceChildOwnerAtomicAssertCanonicalWriterControl(t, fixture, invoice, nil, false, 0)
	probe := &invoiceChildOwnerAtomicProbe{
		Transaction:  fixture.tx,
		targetMarker: invoiceChildOwnerAtomicCompact(invoiceChildOwnerAtomicPostingIDsMarker),
	}
	probe.beforeTargetQuery = func() {
		if _, err := fixture.tx.ExecContext(fixture.ctx, "PRAGMA defer_foreign_keys=ON"); err != nil {
			t.Fatalf("defer fixture owner foreign key: %v", err)
		}
		if err := invoiceReportingMutateWithTriggersRestored(fixture, "invoice_income_postings_v1",
			"UPDATE invoice_income_postings_v1 SET organisation_id=CAST(? AS BLOB) WHERE invoice_id=?", orgID, invoice.Id); err != nil {
			t.Fatalf("install NULL-journal owner corruption: %v", err)
		}
		var ownerType, journalType string
		var owner []byte
		var journal sql.NullString
		if err := fixture.tx.QueryRowContext(fixture.ctx,
			"SELECT typeof(organisation_id),organisation_id,typeof(journal_id),journal_id FROM invoice_income_postings_v1 WHERE invoice_id=?",
			invoice.Id).Scan(&ownerType, &owner, &journalType, &journal); err != nil || ownerType != "blob" ||
			!bytes.Equal(owner, []byte(orgID)) || journalType != "null" || journal.Valid {
			t.Fatalf("zero-total owner/journal type=%s/%s owner=%x journal=%#v err=%v; want exact target BLOB and NULL journal",
				ownerType, journalType, owner, journal, err)
		}
		probe.snapshotTargetCounters()
		probe.stateAfterMutation = invoiceReportingBudgetState(t, fixture)
		probe.hasStateAfterMutation = true
		runtime.ReadMemStats(&probe.memStatsAfterMutation)
		probe.hasMemStatsAfterMutation = true
	}
	guardedPort, err := NewReportingPort(probe)
	if err != nil {
		t.Fatal(err)
	}
	beforeCallState := invoiceReportingBudgetState(t, fixture)
	result, err := guardedPort.CaptureBASWithPayloadBudget(
		fixture.ctx, orgID, invoiceReportingPeriod(), int64(maxBASReportingBytes))
	if result != nil || !errors.Is(err, ErrRepository) || len(err.Error()) >= 512 || !probe.targetHookFired {
		t.Fatalf("NULL-journal BLOB owner drift capture=%#v err=%.256v hook=%t; want nil and short ErrRepository",
			result, err, probe.targetHookFired)
	}
	if len(probe.journalIDPrefixes) != 1 || probe.journalIDPrefixes[0] != invoiceChildOwnerAtomicPostingIDsMarker ||
		len(probe.bodyPrefixes) != 0 || probe.journalPayloadPreflights != 0 || probe.journalBodyQueries != 0 {
		t.Fatalf("NULL-journal invalid owner was omitted or reached payload reads: IDs=%v bodies=%v parent-preflights=%d journal-bodies=%d",
			probe.journalIDPrefixes, probe.bodyPrefixes, probe.journalPayloadPreflights, probe.journalBodyQueries)
	}
	if probe.journalIDsAtTarget != len(probe.journalIDPrefixes) ||
		probe.journalPayloadPreflightsAtTarget != probe.journalPayloadPreflights ||
		probe.journalBodyQueriesAtTarget != probe.journalBodyQueries {
		t.Fatal("NULL-journal owner rejection allowed later identity, parent payload, line, or body queries")
	}
	invoiceChildOwnerAtomicAssertMutationState(t, fixture, beforeCallState, probe)
}

func invoiceChildOwnerAtomicWriteGraph(
	t *testing.T,
	fixture *invoiceReportingFixture,
	invoiceID string,
	withReceipt bool,
	unitPrice int64,
) (*tammyv1.Invoice, *tammyv1.InvoicePayment) {
	t.Helper()
	invoice := fixture.saveDraft(t, invoiceID, "CHILD-OWNER-ATOMIC", "2026-10-12", unitPrice,
		tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
	fixture.issue(t, invoice)
	fixture.post(t, invoice)
	if !withReceipt {
		return invoice, nil
	}
	payment := fixture.recordPayment(t, invoice, 1, 250, 1)
	return invoice, payment
}

func invoiceChildOwnerAtomicAssertCanonicalWriterControl(
	t *testing.T,
	fixture *invoiceReportingFixture,
	invoice *tammyv1.Invoice,
	payment *tammyv1.InvoicePayment,
	wantReceipt bool,
	wantJournalCount int,
) *ReportingPort {
	t.Helper()
	port, err := NewReportingPort(fixture.tx)
	if err != nil {
		t.Fatal(err)
	}
	stateBefore := invoiceReportingBudgetState(t, fixture)
	preflight, err := port.PreflightBASWithPayloadBudget(
		fixture.ctx, orgID, invoiceReportingPeriod(), int64(maxBASReportingBytes))
	if err != nil || preflight == nil || preflight.CandidateCount != 1 ||
		preflight.ReceiptCount != invoiceChildOwnerAtomicBoolInt(wantReceipt) || preflight.JournalCount != wantJournalCount {
		t.Fatalf("canonical real-writer preflight=%#v err=%v; want candidate=1 receipt=%t journals=%d",
			preflight, err, wantReceipt, wantJournalCount)
	}
	capture, err := port.CaptureBASWithPayloadBudget(
		fixture.ctx, orgID, invoiceReportingPeriod(), preflight.PayloadBytes)
	if err != nil || capture == nil || capture.CandidateCount != 1 || len(capture.Candidates) != 1 ||
		capture.ReceiptCount != invoiceChildOwnerAtomicBoolInt(wantReceipt) || capture.JournalCount != wantJournalCount ||
		capture.PayloadBytes != preflight.PayloadBytes {
		t.Fatalf("canonical real-writer capture=%#v err=%v", capture, err)
	}
	candidate := capture.Candidates[0]
	assertInvoiceReportingCandidateSource(t, capture.Candidates)
	if !proto.Equal(candidate.Invoice, invoice) || candidate.Posting == nil {
		t.Fatalf("canonical writer candidate lost invoice or posting source: %#v", candidate)
	}
	if wantReceipt {
		if payment == nil || len(candidate.Receipts) != 1 || candidate.Receipts[0].Payment == nil ||
			!proto.Equal(candidate.Receipts[0].Payment, payment) {
			t.Fatalf("canonical writer receipt did not preserve its complete payment proto: %#v", candidate.Receipts)
		}
		encoded, err := (proto.MarshalOptions{Deterministic: true}).Marshal(payment)
		if err != nil || !bytes.Equal(candidate.Receipts[0].PaymentProto, encoded) {
			t.Fatalf("canonical writer receipt bytes differ from its exact deterministic proto: %v", err)
		}
	} else if len(candidate.Receipts) != 0 {
		t.Fatalf("unexpected canonical writer receipts: %#v", candidate.Receipts)
	}
	if wantJournalCount == 0 && candidate.Posting.JournalID != nil {
		t.Fatalf("canonical zero-total writer retained journal ID %q", *candidate.Posting.JournalID)
	}
	if wantJournalCount > 0 && candidate.Posting.JournalID == nil {
		t.Fatal("canonical nonzero posting lost its journal ID")
	}
	if !fixture.tx.Authenticated() || invoiceReportingBudgetState(t, fixture) != stateBefore {
		t.Fatal("canonical real-writer preflight/capture changed state or caller authentication")
	}
	return port
}

func invoiceChildOwnerAtomicMoveGraphOutOfTarget(
	t *testing.T,
	fixture *invoiceReportingFixture,
	invoiceID, paymentID, foreignOwner, missingInvoiceID string,
	moveReferences bool,
) string {
	t.Helper()
	if _, err := fixture.tx.ExecContext(fixture.ctx, "PRAGMA defer_foreign_keys=ON"); err != nil {
		t.Fatalf("defer corruption-fixture references: %v", err)
	}
	if err := invoiceReportingMutateWithTriggersRestored(fixture, "invoices_v1",
		"UPDATE invoices_v1 SET organisation_id=? WHERE id=?", foreignOwner, invoiceID); err != nil {
		t.Fatalf("move root owner to foreign canonical TEXT: %v", err)
	}
	childInvoiceID := invoiceID
	postingUpdate := "UPDATE invoice_income_postings_v1 SET organisation_id=? WHERE invoice_id=?"
	postingArgs := []any{foreignOwner, invoiceID}
	receiptUpdate := "UPDATE invoice_payment_v1 SET organisation_id=? WHERE id=?"
	receiptArgs := []any{foreignOwner, paymentID}
	if moveReferences {
		childInvoiceID = missingInvoiceID
		postingUpdate = "UPDATE invoice_income_postings_v1 SET organisation_id=?,invoice_id=? WHERE invoice_id=?"
		postingArgs = []any{foreignOwner, missingInvoiceID, invoiceID}
		receiptUpdate = "UPDATE invoice_payment_v1 SET organisation_id=?,invoice_id=? WHERE id=?"
		receiptArgs = []any{foreignOwner, missingInvoiceID, paymentID}
	}
	if err := invoiceReportingMutateWithTriggersRestored(fixture, "invoice_income_postings_v1", postingUpdate, postingArgs...); err != nil {
		t.Fatalf("move posting owner/reference out of target: %v", err)
	}
	if err := invoiceReportingMutateWithTriggersRestored(fixture, "invoice_payment_v1", receiptUpdate, receiptArgs...); err != nil {
		t.Fatalf("move receipt owner/reference out of target: %v", err)
	}
	return childInvoiceID
}

func invoiceChildOwnerAtomicAssertEmptyBaseline(t *testing.T, port *ReportingPort, fixture *invoiceReportingFixture) {
	t.Helper()
	preflight, err := port.PreflightBASWithPayloadBudget(
		fixture.ctx, orgID, invoiceReportingPeriod(), int64(maxBASReportingBytes))
	if err != nil || preflight == nil || preflight.CandidateCount != 0 || preflight.ReceiptCount != 0 ||
		preflight.JournalCount != 0 || preflight.BasePayloadBytes != 0 || preflight.PayloadBytes != 0 ||
		len(preflight.JournalIDs) != 0 {
		t.Fatalf("foreign/missing-parent empty preflight=%#v err=%v", preflight, err)
	}
	capture, err := port.CaptureBASWithPayloadBudget(fixture.ctx, orgID, invoiceReportingPeriod(), 0)
	if err != nil || capture == nil || capture.CandidateCount != 0 || len(capture.Candidates) != 0 ||
		capture.ReceiptCount != 0 || capture.JournalCount != 0 || capture.PayloadBytes != 0 {
		t.Fatalf("foreign/missing-parent empty capture=%#v err=%v", capture, err)
	}
}

func invoiceChildOwnerAtomicAssertReferenceParent(
	t *testing.T,
	fixture *invoiceReportingFixture,
	childInvoiceID, parentOwner string,
	invoiceParentExpected, receiptParentExpected bool,
) {
	t.Helper()
	var invoiceParents int
	var invoiceOwnerType string
	var invoiceOwner sql.NullString
	if err := fixture.tx.QueryRowContext(fixture.ctx,
		"SELECT count(*),coalesce(max(typeof(organisation_id)),''),max(organisation_id) FROM invoices_v1 WHERE id=?",
		childInvoiceID).Scan(&invoiceParents, &invoiceOwnerType, &invoiceOwner); err != nil {
		t.Fatalf("read child invoice reference parent: %v", err)
	}
	wantInvoiceParents := 0
	if invoiceParentExpected {
		wantInvoiceParents = 1
	}
	if invoiceParents != wantInvoiceParents {
		t.Fatalf("child invoice reference %s parent rows=%d; want %d", childInvoiceID, invoiceParents, wantInvoiceParents)
	}
	if invoiceParentExpected && (invoiceOwnerType != "text" || !invoiceOwner.Valid || invoiceOwner.String != parentOwner) {
		t.Fatalf("foreign invoice parent type/value=%s/%#v; want canonical TEXT %q", invoiceOwnerType, invoiceOwner, parentOwner)
	}
	if receiptParentExpected {
		var postingParents int
		var postingOwnerType string
		var postingOwner sql.NullString
		if err := fixture.tx.QueryRowContext(fixture.ctx,
			"SELECT count(*),coalesce(max(typeof(organisation_id)),''),max(organisation_id) FROM invoice_income_postings_v1 WHERE invoice_id=?",
			childInvoiceID).Scan(&postingParents, &postingOwnerType, &postingOwner); err != nil {
			t.Fatalf("read receipt posting reference parent: %v", err)
		}
		if postingParents != 1 || postingOwnerType != "text" || !postingOwner.Valid || postingOwner.String != parentOwner {
			t.Fatalf("receipt posting reference parent rows/type/value=%d/%s/%#v; want one TEXT parent", postingParents, postingOwnerType, postingOwner)
		}
	}
}

func invoiceChildOwnerAtomicFamilyKey(family, invoiceID, paymentID string) string {
	if family == "receipt" {
		return paymentID
	}
	return invoiceID
}

func invoiceChildOwnerAtomicPaymentID(payment *tammyv1.InvoicePayment) string {
	if payment == nil {
		return ""
	}
	return payment.Id
}

func invoiceChildOwnerAtomicAssertLargeBodyCell(
	t *testing.T,
	fixture *invoiceReportingFixture,
	family, childInvoiceID, paymentID string,
	ownerIsForeignText bool,
	foreignOwner string,
) {
	t.Helper()
	key := invoiceChildOwnerAtomicFamilyKey(family, childInvoiceID, paymentID)
	var ownerType, payloadType string
	var owner []byte
	var payloadLength int64
	column := "created_at"
	table := "invoice_income_postings_v1"
	if family == "receipt" {
		column = "payment_proto"
		table = "invoice_payment_v1"
	}
	if err := fixture.tx.QueryRowContext(fixture.ctx,
		"SELECT typeof(organisation_id),organisation_id,typeof("+column+"),length("+column+") FROM "+table+" WHERE "+map[string]string{"posting": "invoice_id", "receipt": "id"}[family]+"=?", key,
	).Scan(&ownerType, &owner, &payloadType, &payloadLength); err != nil {
		t.Fatalf("read masked child owner/body metadata: %v", err)
	}
	wantOwner := orgID
	wantType := "blob"
	if ownerIsForeignText {
		wantOwner = foreignOwner
		wantType = "text"
	}
	if ownerType != wantType || !bytes.Equal(owner, []byte(wantOwner)) || payloadType != "blob" ||
		payloadLength != invoiceChildOwnerAtomicLargeCellBytes {
		t.Fatalf("corrupted %s owner/body metadata=%s/%q %s/%d; want %s/%q blob/%d",
			family, ownerType, owner, payloadType, payloadLength, wantType, wantOwner, invoiceChildOwnerAtomicLargeCellBytes)
	}
}

func invoiceChildOwnerAtomicAssertLargeJournalID(
	t *testing.T,
	fixture *invoiceReportingFixture,
	family, invoiceID string,
	payment *tammyv1.InvoicePayment,
) {
	t.Helper()
	table, keyColumn, key := "invoice_income_postings_v1", "invoice_id", invoiceID
	if family == "receipt" {
		table, keyColumn, key = "invoice_payment_v1", "id", invoiceChildOwnerAtomicPaymentID(payment)
	}
	var ownerType, journalType string
	var owner []byte
	var journalLength int64
	if err := fixture.tx.QueryRowContext(fixture.ctx,
		"SELECT typeof(organisation_id),organisation_id,typeof(journal_id),length(journal_id) FROM "+table+" WHERE "+keyColumn+"=?", key,
	).Scan(&ownerType, &owner, &journalType, &journalLength); err != nil || ownerType != "blob" ||
		!bytes.Equal(owner, []byte(orgID)) || journalType != "blob" ||
		journalLength != invoiceChildOwnerAtomicLargeCellBytes {
		t.Fatalf("corrupted %s owner/journal metadata=%s/%x %s/%d err=%v; want exact target BLOB and 9 MiB journal BLOB",
			family, ownerType, owner, journalType, journalLength, err)
	}
}

func invoiceChildOwnerAtomicCompact(query string) string {
	return strings.ReplaceAll(strings.ToLower(strings.Join(strings.Fields(query), " ")), " ", "")
}

func invoiceChildOwnerAtomicEqualStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func invoiceChildOwnerAtomicBoolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

type invoiceChildOwnerAtomicProbe struct {
	*sqlcipher.Transaction
	targetMarker                     string
	beforeTargetQuery                func()
	targetHookFired                  bool
	bodyPrefixes                     []string
	journalIDPrefixes                []string
	journalPayloadPreflights         int
	journalBodyQueries               int
	journalIDsAtTarget               int
	stateAfterMutation               [8]int64
	hasStateAfterMutation            bool
	journalPayloadPreflightsAtTarget int
	journalBodyQueriesAtTarget       int
	memStatsAfterMutation            runtime.MemStats
	hasMemStatsAfterMutation         bool
}

func (executor *invoiceChildOwnerAtomicProbe) QueryContext(
	ctx context.Context,
	query string,
	arguments ...any,
) (*sql.Rows, error) {
	compact := invoiceChildOwnerAtomicCompact(query)
	for _, marker := range []struct{ name, value string }{
		{name: "root", value: invoiceChildOwnerAtomicRootBodyMarker},
		{name: "posting", value: invoiceChildOwnerAtomicPostingBodyMarker},
		{name: "income-line", value: invoiceChildOwnerAtomicIncomeLineBodyMarker},
		{name: "receipt", value: invoiceChildOwnerAtomicReceiptBodyMarker},
		{name: "allocation", value: invoiceChildOwnerAtomicAllocationBodyMarker},
	} {
		if strings.Contains(compact, invoiceChildOwnerAtomicCompact(marker.value)) {
			executor.bodyPrefixes = append(executor.bodyPrefixes, marker.value)
			break
		}
	}
	for _, marker := range []string{
		invoiceChildOwnerAtomicPostingIDsMarker,
		invoiceChildOwnerAtomicReceiptIDsMarker,
	} {
		if strings.Contains(compact, invoiceChildOwnerAtomicCompact(marker)) {
			executor.journalIDPrefixes = append(executor.journalIDPrefixes, marker)
		}
	}
	if strings.Contains(compact, "fromjournalswhereid=?") ||
		strings.Contains(compact, "fromjournal_linesaslinewherejournal_id=?") {
		executor.journalPayloadPreflights++
	}
	if strings.Contains(compact, "selectid,organisation_id,version,source_type,source_kind") ||
		strings.Contains(compact, "selectid,journal_id,line_number,account_id") {
		executor.journalBodyQueries++
	}
	if executor.targetMarker != "" && !executor.targetHookFired && strings.Contains(compact, executor.targetMarker) {
		executor.targetHookFired = true
		if executor.beforeTargetQuery != nil {
			executor.beforeTargetQuery()
		}
	}
	return executor.Transaction.QueryContext(ctx, query, arguments...)
}

func (executor *invoiceChildOwnerAtomicProbe) snapshotTargetCounters() {
	executor.journalIDsAtTarget = len(executor.journalIDPrefixes)
	executor.journalPayloadPreflightsAtTarget = executor.journalPayloadPreflights
	executor.journalBodyQueriesAtTarget = executor.journalBodyQueries
}

func invoiceChildOwnerAtomicAssertMutationState(
	t *testing.T,
	fixture *invoiceReportingFixture,
	before [8]int64,
	probe *invoiceChildOwnerAtomicProbe,
) {
	t.Helper()
	if !probe.hasStateAfterMutation {
		t.Fatal("mutation hook did not snapshot the retained state")
	}
	for index := 0; index < 7; index++ {
		if probe.stateAfterMutation[index] != before[index] {
			t.Fatalf("late corruption changed financial revision vector at %d: before=%v after-mutation=%v",
				index, before, probe.stateAfterMutation)
		}
	}
	if probe.stateAfterMutation[7] <= before[7] {
		t.Fatalf("late corruption did not register its SQL mutation: before=%v after-mutation=%v", before, probe.stateAfterMutation)
	}
	if stateAfter := invoiceReportingBudgetState(t, fixture); stateAfter != probe.stateAfterMutation || !fixture.tx.Authenticated() {
		t.Fatalf("read-only rejection changed state after mutation or ended caller transaction: mutation=%v final=%v authenticated=%t",
			probe.stateAfterMutation, stateAfter, fixture.tx.Authenticated())
	}
}
