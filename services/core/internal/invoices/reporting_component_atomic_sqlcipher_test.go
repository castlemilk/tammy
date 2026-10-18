//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package invoices

import (
	"context"
	"database/sql"
	"errors"
	"runtime"
	"strings"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

func TestInvoiceComponentOrphanLateBodyDriftIsRejectedBeforeLargeCellsReachGo(t *testing.T) {
	families := []struct {
		name, bodyMarker, preflightMarker, table, referenceColumn, updateTrigger string
		wantBodyPrefixes                                                         []string
	}{
		{
			name: "income-line", bodyMarker: invoiceChildOwnerAtomicIncomeLineBodyMarker,
			preflightMarker: "from invoice_income_lines_v1 as income_line",
			table:           "invoice_income_lines_v1", referenceColumn: "invoice_id",
			updateTrigger: "invoice_income_lines_no_update",
			wantBodyPrefixes: []string{
				invoiceChildOwnerAtomicRootBodyMarker,
				invoiceChildOwnerAtomicPostingBodyMarker,
				invoiceChildOwnerAtomicIncomeLineBodyMarker,
			},
		},
		{
			name: "allocation", bodyMarker: invoiceChildOwnerAtomicAllocationBodyMarker,
			preflightMarker: "from invoice_payment_lines_v1 as allocation",
			table:           "invoice_payment_lines_v1", referenceColumn: "payment_id",
			updateTrigger: "invoice_payment_lines_no_update",
			wantBodyPrefixes: []string{
				invoiceChildOwnerAtomicRootBodyMarker,
				invoiceChildOwnerAtomicPostingBodyMarker,
				invoiceChildOwnerAtomicIncomeLineBodyMarker,
				invoiceChildOwnerAtomicReceiptBodyMarker,
				invoiceChildOwnerAtomicAllocationBodyMarker,
			},
		},
	}
	lateCases := []struct {
		name            string
		orphanReference bool
	}{
		{name: "orphan-reference-and-blob-gross", orphanReference: true},
		{name: "linked-reference-and-blob-gross"},
	}

	for familyIndex, family := range families {
		family := family
		for caseIndex, lateCase := range lateCases {
			lateCase := lateCase
			t.Run(family.name+"/"+lateCase.name, func(t *testing.T) {
				fixture := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_CASH)
				invoiceID := invoiceReportingID(uint64(987000 + familyIndex*10 + caseIndex))
				invoice, payment := invoiceChildOwnerAtomicWriteGraph(t, fixture, invoiceID, true, 1000)
				if payment == nil {
					t.Fatal("late component writer omitted its real invoice payment")
				}

				// Pin ordinary writer behavior and its exact source-derived reservation
				// before the probe installs corruption after the next fresh preflight.
				port := invoiceChildOwnerAtomicAssertCanonicalWriterControl(
					t, fixture, invoice, payment, true, 2,
				)
				controlState := invoiceReportingBudgetState(t, fixture)
				controlCapture, controlErr := port.CaptureBASWithPayloadBudget(
					fixture.ctx, orgID, invoiceReportingPeriod(), int64(maxBASReportingBytes),
				)
				if controlErr != nil || controlCapture == nil || controlCapture.CandidateCount != 1 ||
					controlCapture.ReceiptCount != 1 || controlCapture.JournalCount != 2 {
					t.Fatalf("real-writer control capture=%#v err=%.256v; want one invoice, one receipt and two journals",
						controlCapture, controlErr)
				}
				wantBasePayload, chargeValid := invoiceChildOwnerStaticExpectedBasePayload(controlCapture)
				var journalPayloadBytes int64
				for _, charge := range controlCapture.JournalPayloadBytes {
					if charge <= 0 {
						t.Fatalf("real-writer control has a nonpositive journal charge: %d", charge)
					}
					journalPayloadBytes += charge
				}
				if !chargeValid || controlCapture.BasePayloadBytes != wantBasePayload ||
					controlCapture.PayloadBytes != wantBasePayload+journalPayloadBytes {
					t.Fatalf("real-writer control charges base=%d want=%d total=%d journal=%d valid=%t",
						controlCapture.BasePayloadBytes, wantBasePayload, controlCapture.PayloadBytes,
						journalPayloadBytes, chargeValid)
				}
				if stateAfterControl := invoiceReportingBudgetState(t, fixture); stateAfterControl != controlState ||
					!fixture.tx.Authenticated() {
					t.Fatalf("real-writer charge control changed state or caller authentication: before=%v after=%v authenticated=%t",
						controlState, stateAfterControl, fixture.tx.Authenticated())
				}
				if lateCase.orphanReference {
					foreignOwner := invoiceChildOwnerStaticMoveWriterGraphToForeign(t, invoiceChildOwnerStaticSources{
						fixture: fixture, invoice: invoice, payment: payment,
					})
					if foreignOwner == orgID {
						t.Fatal("foreign TEXT writer graph retained the target owner")
					}
					invoiceChildOwnerStaticAssertNoTargetTextOwnerEdges(t, fixture)
					foreignGraphState := invoiceReportingBudgetState(t, fixture)
					invoiceChildOwnerStaticAssertEmptyScalar(t, fixture)
					invoiceChildOwnerAtomicAssertEmptyBaseline(t, port, fixture)
					if stateAfterBaseline := invoiceReportingBudgetState(t, fixture); stateAfterBaseline != foreignGraphState ||
						!fixture.tx.Authenticated() {
						t.Fatalf("foreign-graph empty baseline changed state or caller authentication: before=%v after=%v authenticated=%t",
							foreignGraphState, stateAfterBaseline, fixture.tx.Authenticated())
					}
				}

				reference := invoice.Id
				missingReference := invoiceReportingID(uint64(989000 + familyIndex*10 + caseIndex))
				if family.name == "allocation" {
					reference = payment.Id
					missingReference = invoiceReportingID(uint64(990000 + familyIndex*10 + caseIndex))
				}

				atomicProbe := &invoiceChildOwnerAtomicProbe{
					Transaction:  fixture.tx,
					targetMarker: invoiceChildOwnerAtomicCompact(family.bodyMarker),
				}
				probe := &invoiceComponentOrphanAtomicProbe{
					invoiceChildOwnerAtomicProbe: atomicProbe,
					componentPreflightMarker:     invoiceChildOwnerAtomicCompact(family.preflightMarker),
				}
				atomicProbe.beforeTargetQuery = func() {
					if probe.componentPreflightQueries != 1 {
						t.Fatalf("late %s body hook saw %d component preflight queries; want one fresh preflight",
							family.name, probe.componentPreflightQueries)
					}
					if lateCase.orphanReference {
						if _, err := fixture.tx.ExecContext(fixture.ctx, "PRAGMA defer_foreign_keys=ON"); err != nil {
							t.Fatalf("defer orphan component reference foreign key: %.256v", err)
						}
					}
					if _, err := fixture.tx.ExecContext(fixture.ctx, "PRAGMA ignore_check_constraints=ON"); err != nil {
						t.Fatalf("temporarily disable component numeric checks: %.256v", err)
					}

					update := "UPDATE " + family.table + " SET "
					arguments := []any{invoiceChildOwnerAtomicLargeCellBytes, reference}
					if lateCase.orphanReference {
						update += family.referenceColumn + "=?,"
						arguments = []any{missingReference, invoiceChildOwnerAtomicLargeCellBytes, reference}
					}
					update += "gross_minor=zeroblob(?) WHERE " + family.referenceColumn + "=? AND sequence=1"
					mutationErr := invoiceReportingMutateWithTriggersRestored(
						fixture, family.table, update, arguments...,
					)
					_, restoreChecksErr := fixture.tx.ExecContext(fixture.ctx, "PRAGMA ignore_check_constraints=OFF")
					if mutationErr != nil || restoreChecksErr != nil {
						t.Fatalf("install late %s component drift: %.256v; restore numeric checks: %.256v",
							family.name, mutationErr, restoreChecksErr)
					}

					actualReference := reference
					if lateCase.orphanReference {
						actualReference = missingReference
					}
					invoiceComponentOrphanAtomicAssertLateRow(
						t, fixture, family.name, actualReference, lateCase.orphanReference,
					)
					var ignoreChecks int
					if err := fixture.tx.QueryRowContext(fixture.ctx, "PRAGMA ignore_check_constraints").Scan(&ignoreChecks); err != nil || ignoreChecks != 0 {
						t.Fatalf("component mutation left ignore_check_constraints=%d err=%.256v; want OFF", ignoreChecks, err)
					}
					var restoredTriggers int
					if err := fixture.tx.QueryRowContext(fixture.ctx,
						"SELECT count(*) FROM sqlite_master WHERE type='trigger' AND tbl_name=? AND name=?",
						family.table, family.updateTrigger,
					).Scan(&restoredTriggers); err != nil || restoredTriggers != 1 {
						t.Fatalf("component update trigger count=%d err=%.256v; want restored trigger", restoredTriggers, err)
					}

					probe.snapshotTargetCounters()
					probe.stateAfterMutation = invoiceReportingBudgetState(t, fixture)
					probe.hasStateAfterMutation = true
					// Start the allocation ceiling after all mutation and metadata checks.
					runtime.ReadMemStats(&probe.memStatsAfterMutation)
					probe.hasMemStatsAfterMutation = true
				}

				capturePort, err := NewReportingPort(probe)
				if err != nil {
					t.Fatal(err)
				}
				beforeCallState := invoiceReportingBudgetState(t, fixture)
				result, captureErr := capturePort.CaptureBASWithPayloadBudget(
					fixture.ctx, orgID, invoiceReportingPeriod(), int64(maxBASReportingBytes),
				)
				var after runtime.MemStats
				runtime.ReadMemStats(&after)
				allocated := after.TotalAlloc - probe.memStatsAfterMutation.TotalAlloc
				errorBytes := 0
				if captureErr != nil {
					errorBytes = len(captureErr.Error())
				}

				if result != nil || !errors.Is(captureErr, ErrRepository) || errorBytes >= 512 ||
					!probe.targetHookFired || !probe.hasMemStatsAfterMutation {
					t.Fatalf("late %s %s capture=%#v err=%.256v errorBytes=%d allocated=%d hooked=%t; want nil and short ErrRepository",
						family.name, lateCase.name, result, captureErr, errorBytes, allocated, probe.targetHookFired)
				}
				if allocated >= 2*1024*1024 {
					t.Fatalf("late %s %s copied its rejected 9 MiB gross cell: allocated=%d",
						family.name, lateCase.name, allocated)
				}
				if len(probe.bodyPrefixes) != len(family.wantBodyPrefixes) ||
					!invoiceChildOwnerAtomicEqualStrings(probe.bodyPrefixes, family.wantBodyPrefixes) {
					t.Fatalf("late %s %s body prefixes=%v; want exact pre-failure sequence %v",
						family.name, lateCase.name, probe.bodyPrefixes, family.wantBodyPrefixes)
				}
				wantJournalIDPrefixes := []string{
					invoiceChildOwnerAtomicPostingIDsMarker,
					invoiceChildOwnerAtomicReceiptIDsMarker,
				}
				if !invoiceChildOwnerAtomicEqualStrings(probe.journalIDPrefixes, wantJournalIDPrefixes) ||
					probe.journalIDsAtTarget != len(probe.journalIDPrefixes) ||
					probe.journalPayloadPreflightsAtTarget != probe.journalPayloadPreflights ||
					probe.journalBodyQueriesAtTarget != probe.journalBodyQueries {
					t.Fatalf("late %s %s ran later journal proof reads: IDs at target=%d final=%v, payloads=%d/%d, bodies=%d/%d",
						family.name, lateCase.name, probe.journalIDsAtTarget, probe.journalIDPrefixes,
						probe.journalPayloadPreflightsAtTarget, probe.journalPayloadPreflights,
						probe.journalBodyQueriesAtTarget, probe.journalBodyQueries)
				}
				invoiceChildOwnerAtomicAssertMutationState(t, fixture, beforeCallState, atomicProbe)
			})
		}
	}
}

type invoiceComponentOrphanAtomicProbe struct {
	*invoiceChildOwnerAtomicProbe
	componentPreflightMarker  string
	componentPreflightQueries int
}

func (executor *invoiceComponentOrphanAtomicProbe) QueryContext(
	ctx context.Context,
	query string,
	arguments ...any,
) (*sql.Rows, error) {
	compact := invoiceChildOwnerAtomicCompact(query)
	if strings.Contains(compact, executor.componentPreflightMarker) && strings.Contains(compact, "selectcount(") {
		executor.componentPreflightQueries++
	}
	return executor.invoiceChildOwnerAtomicProbe.QueryContext(ctx, query, arguments...)
}

func invoiceComponentOrphanAtomicAssertLateRow(
	t *testing.T,
	fixture *invoiceReportingFixture,
	family, reference string,
	orphan bool,
) {
	t.Helper()
	var metadataQuery string
	switch family {
	case "income-line":
		metadataQuery = "SELECT typeof(invoice_id),invoice_id,typeof(gross_minor),length(gross_minor) " +
			"FROM invoice_income_lines_v1 WHERE invoice_id=? AND sequence=1"
	case "allocation":
		metadataQuery = "SELECT typeof(payment_id),payment_id,typeof(gross_minor),length(gross_minor) " +
			"FROM invoice_payment_lines_v1 WHERE payment_id=? AND sequence=1"
	default:
		t.Fatalf("unsupported late component family %q", family)
	}

	// Scan only the small reference and SQLite type/length metadata. Never read
	// the corrupted gross_minor BLOB into Go.
	var referenceType, actualReference, grossType string
	var grossLength int64
	if err := fixture.tx.QueryRowContext(fixture.ctx, metadataQuery, reference).Scan(
		&referenceType, &actualReference, &grossType, &grossLength,
	); err != nil || referenceType != "text" || actualReference != reference ||
		grossType != "blob" || grossLength != invoiceChildOwnerAtomicLargeCellBytes {
		t.Fatalf("late %s metadata ref=%s/%q gross=%s/%d err=%.256v; want exact TEXT key and 9 MiB BLOB metadata",
			family, referenceType, actualReference, grossType, grossLength, err)
	}

	var linkedParents int
	switch family {
	case "income-line":
		if orphan {
			var postingParents, invoiceParents int
			if err := fixture.tx.QueryRowContext(fixture.ctx,
				"SELECT count(*) FROM invoice_income_postings_v1 WHERE invoice_id=?", reference,
			).Scan(&postingParents); err != nil {
				t.Fatalf("read orphan income-line posting parent metadata: %.256v", err)
			}
			if err := fixture.tx.QueryRowContext(fixture.ctx,
				"SELECT count(*) FROM invoices_v1 WHERE id=?", reference,
			).Scan(&invoiceParents); err != nil {
				t.Fatalf("read orphan income-line invoice parent metadata: %.256v", err)
			}
			if postingParents != 0 || invoiceParents != 0 {
				t.Fatalf("late income-line orphan has posting/invoice parents=%d/%d; want both missing",
					postingParents, invoiceParents)
			}
			return
		}
		if err := fixture.tx.QueryRowContext(fixture.ctx,
			"SELECT count(*) FROM invoice_income_lines_v1 AS line "+
				"JOIN invoice_income_postings_v1 AS posting ON posting.invoice_id=line.invoice_id "+
				"JOIN invoices_v1 AS invoice ON invoice.id=line.invoice_id "+
				"WHERE line.invoice_id=? AND line.sequence=1 AND posting.organisation_id=? AND invoice.organisation_id=?",
			reference, orgID, orgID,
		).Scan(&linkedParents); err != nil {
			t.Fatalf("read linked income-line parent metadata: %.256v", err)
		}
	case "allocation":
		if orphan {
			var receiptParents int
			if err := fixture.tx.QueryRowContext(fixture.ctx,
				"SELECT count(*) FROM invoice_payment_v1 WHERE id=?", reference,
			).Scan(&receiptParents); err != nil {
				t.Fatalf("read orphan allocation receipt parent metadata: %.256v", err)
			}
			if receiptParents != 0 {
				t.Fatalf("late allocation orphan has %d receipt parents; want missing receipt", receiptParents)
			}
			return
		}
		if err := fixture.tx.QueryRowContext(fixture.ctx,
			"SELECT count(*) FROM invoice_payment_lines_v1 AS allocation "+
				"JOIN invoice_payment_v1 AS receipt ON receipt.id=allocation.payment_id "+
				"JOIN invoice_income_postings_v1 AS posting ON posting.invoice_id=receipt.invoice_id "+
				"JOIN invoices_v1 AS invoice ON invoice.id=receipt.invoice_id "+
				"WHERE allocation.payment_id=? AND allocation.sequence=1 AND receipt.organisation_id=? "+
				"AND posting.organisation_id=? AND invoice.organisation_id=?",
			reference, orgID, orgID, orgID,
		).Scan(&linkedParents); err != nil {
			t.Fatalf("read linked allocation ancestry metadata: %.256v", err)
		}
	}
	if linkedParents != 1 {
		t.Fatalf("late %s reference %q has %d valid target parent chains; want one", family, reference, linkedParents)
	}
}
