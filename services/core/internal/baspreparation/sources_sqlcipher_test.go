//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/artefacts"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/invoices"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
)

type basSourceProducerTrace struct{ stages []string }

func (t *basSourceProducerTrace) add(stage string) { t.stages = append(t.stages, stage) }
func (t *basSourceProducerTrace) index(stage string) int {
	for i, got := range t.stages {
		if got == stage {
			return i
		}
	}
	return -1
}

type basSourceProducerSQLProbe struct {
	*sqlcipher.Transaction
	queries []string
	writes  int
}

func (p *basSourceProducerSQLProbe) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	p.queries = append(p.queries, query)
	return p.Transaction.QueryContext(ctx, query, args...)
}
func (p *basSourceProducerSQLProbe) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	p.writes++
	return p.Transaction.ExecContext(ctx, query, args...)
}

type basSourceProducerInvoiceTrace struct {
	BASInvoiceRecordPort
	trace *basSourceProducerTrace
}

func (p basSourceProducerInvoiceTrace) SummarizeBASKeys(ctx context.Context, org string) (*invoices.BASScalarSummary, error) {
	p.trace.add("invoice.summary")
	return p.BASInvoiceRecordPort.SummarizeBASKeys(ctx, org)
}
func (p basSourceProducerInvoiceTrace) ReadBASKeys(ctx context.Context, org string, s invoices.BASScalarSummary, n, b int64) ([]invoices.BASScalarKey, error) {
	p.trace.add("invoice.keys")
	return p.BASInvoiceRecordPort.ReadBASKeys(ctx, org, s, n, b)
}
func (p basSourceProducerInvoiceTrace) PreflightBASWithPayloadBudget(ctx context.Context, org string, d *tammyv1.BasCivilDateRange, n int64) (*invoices.BASReportingPreflight, error) {
	p.trace.add("invoice.preflight")
	return p.BASInvoiceRecordPort.PreflightBASWithPayloadBudget(ctx, org, d, n)
}
func (p basSourceProducerInvoiceTrace) CaptureBASWithPayloadBudget(ctx context.Context, org string, d *tammyv1.BasCivilDateRange, n int64) (*invoices.BASReportingCapture, error) {
	p.trace.add("invoice.capture")
	return p.BASInvoiceRecordPort.CaptureBASWithPayloadBudget(ctx, org, d, n)
}

type basSourceProducerPurchaseTrace struct {
	BASPurchaseRecordPort
	trace *basSourceProducerTrace
}

func (p basSourceProducerPurchaseTrace) SummarizeBASKeys(ctx context.Context, org string) (*purchases.BASScalarSummary, error) {
	p.trace.add("purchase.summary")
	return p.BASPurchaseRecordPort.SummarizeBASKeys(ctx, org)
}
func (p basSourceProducerPurchaseTrace) ReadModernBASCandidates(ctx context.Context, org string, s purchases.BASScalarSummary, n, b, c int64) (*purchases.BASModernCandidateDiscovery, error) {
	p.trace.add("purchase.keys")
	return p.BASPurchaseRecordPort.ReadModernBASCandidates(ctx, org, s, n, b, c)
}
func (p basSourceProducerPurchaseTrace) PreflightCoverageForBASWithPayloadBudget(ctx context.Context, org string, n int64) (*purchases.PurchaseCoverageReportingPreflight, error) {
	p.trace.add("purchase.preflight")
	return p.BASPurchaseRecordPort.PreflightCoverageForBASWithPayloadBudget(ctx, org, n)
}
func (p basSourceProducerPurchaseTrace) CaptureCoverageForBASWithPayloadBudget(ctx context.Context, org string, n int64) (*purchases.PurchaseCoverageReportingCapture, error) {
	p.trace.add("purchase.capture")
	return p.BASPurchaseRecordPort.CaptureCoverageForBASWithPayloadBudget(ctx, org, n)
}
func (p basSourceProducerPurchaseTrace) SummarizeBASLegacyDecisions(ctx context.Context, org string) (*purchases.BASLegacyDecisionSummary, error) {
	p.trace.add("purchase.decision.summary")
	return p.BASPurchaseRecordPort.SummarizeBASLegacyDecisions(ctx, org)
}
func (p basSourceProducerPurchaseTrace) ReadBASLegacyDecisions(ctx context.Context, org string, s purchases.BASLegacyDecisionSummary, n, b int64) (*purchases.BASLegacyDecisionRead, error) {
	p.trace.add("purchase.decision.read")
	return p.BASPurchaseRecordPort.ReadBASLegacyDecisions(ctx, org, s, n, b)
}
func (p basSourceProducerPurchaseTrace) SummarizeBASDocumentStates(ctx context.Context, org string) (*purchases.BASDocumentStateSummary, error) {
	p.trace.add("purchase.document.summary")
	return p.BASPurchaseRecordPort.SummarizeBASDocumentStates(ctx, org)
}
func (p basSourceProducerPurchaseTrace) ReadBASDocumentStates(ctx context.Context, org string, s purchases.BASDocumentStateSummary, n, b int64) (*purchases.BASDocumentStateRead, error) {
	p.trace.add("purchase.document.read")
	return p.BASPurchaseRecordPort.ReadBASDocumentStates(ctx, org, s, n, b)
}

type basSourceProducerAccountingTrace struct {
	BASAccountingRecordPort
	trace *basSourceProducerTrace
}

func (p basSourceProducerAccountingTrace) SummarizeTaxFactBASKeys(ctx context.Context, org string) (*accounting.TaxFactBASScalarSummary, error) {
	p.trace.add("accounting.summary")
	return p.BASAccountingRecordPort.SummarizeTaxFactBASKeys(ctx, org)
}
func (p basSourceProducerAccountingTrace) ReadTaxFactBASKeys(ctx context.Context, org string, s accounting.TaxFactBASScalarSummary, n, b int64) ([]accounting.TaxFactBASScalarKey, error) {
	p.trace.add("accounting.keys")
	return p.BASAccountingRecordPort.ReadTaxFactBASKeys(ctx, org, s, n, b)
}
func (p basSourceProducerAccountingTrace) PreflightTaxFactsForReportingWithPayloadBudget(ctx context.Context, org string, n int64) (*accounting.TaxFactReportingPreflight, error) {
	p.trace.add("accounting.preflight")
	return p.BASAccountingRecordPort.PreflightTaxFactsForReportingWithPayloadBudget(ctx, org, n)
}
func (p basSourceProducerAccountingTrace) CaptureTaxFactsForReportingWithPayloadBudget(ctx context.Context, org string, n int64) (*accounting.TaxFactReportingCapture, error) {
	p.trace.add("accounting.capture")
	return p.BASAccountingRecordPort.CaptureTaxFactsForReportingWithPayloadBudget(ctx, org, n)
}
func (p basSourceProducerAccountingTrace) SummarizeBASLegacyCash(ctx context.Context, q accounting.BASLegacyCashQuery) (*accounting.BASLegacyCashSummary, error) {
	p.trace.add("cash.summary")
	return p.BASAccountingRecordPort.SummarizeBASLegacyCash(ctx, q)
}
func (p basSourceProducerAccountingTrace) ReadBASLegacyCash(ctx context.Context, q accounting.BASLegacyCashQuery, s accounting.BASLegacyCashSummary, n, b int64) (*accounting.BASLegacyCashRead, error) {
	p.trace.add("cash.read")
	return p.BASAccountingRecordPort.ReadBASLegacyCash(ctx, q, s, n, b)
}

type basSourceProducerRuleTrace struct {
	BASRuleRecordPort
	trace *basSourceProducerTrace
}

func (p basSourceProducerRuleTrace) PreflightForReportingWithPayloadBudget(ctx context.Context, org string, r *tammyv1.SourceRef, n int64) (*artefacts.RuleBundleReportingPreflight, error) {
	p.trace.add("rule.preflight")
	return p.BASRuleRecordPort.PreflightForReportingWithPayloadBudget(ctx, org, r, n)
}
func (p basSourceProducerRuleTrace) GetForReportingWithPayloadBudget(ctx context.Context, org string, r *tammyv1.SourceRef, n int64) (*artefacts.RuleBundleReportingRecord, error) {
	p.trace.add("rule.capture")
	return p.BASRuleRecordPort.GetForReportingWithPayloadBudget(ctx, org, r, n)
}

func newBASSourceProducerNativeFixture(t *testing.T, mixed bool) (*testkit.EncryptedWorkspace, *sqlcipher.Transaction, BASBoundedSourceRequest, BASBoundedSourcePorts, *basSourceProducerTrace, *basSourceProducerSQLProbe) {
	return newBASSourceProducerNativeFixtureWithBasis(t, mixed, tammyv1.GstBasis_GST_BASIS_NON_CASH)
}

func newBASSourceProducerNativeFixtureWithBasis(t *testing.T, mixed bool, basis tammyv1.GstBasis) (*testkit.EncryptedWorkspace, *sqlcipher.Transaction, BASBoundedSourceRequest, BASBoundedSourcePorts, *basSourceProducerTrace, *basSourceProducerSQLProbe) {
	t.Helper()
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 14)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	org, actor := testUUID(9961), testUUID(9962)
	bundle, err := artefacts.LoadAUGSTV1()
	if err != nil {
		t.Fatal(err)
	}
	seedSourceScalarOrganisationWithTaxProfile(t, ctx, tx, org, actor, now, basis, proto.Clone(bundle.Source).(*tammyv1.SourceRef))
	ruleRepo, err := artefacts.NewRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	if err := ruleRepo.Retain(ctx, org, bundle, now); err != nil {
		t.Fatal(err)
	}
	if mixed {
		seedSourceScalarInvoice(t, ctx, tx, org, now, testUUID(9970))
		seedSourceScalarPurchaseDocument(t, ctx, tx, org, actor, now, testUUID(9928), testUUID(9912))
		seedSourceScalarTaxFact(t, ctx, tx, org, now, testUUID(9920), testUUID(9921), testUUID(9922), testUUID(9923), testUUID(9924), testUUID(9925), testUUID(9926))
	}
	probe := &basSourceProducerSQLProbe{Transaction: tx}
	trace := &basSourceProducerTrace{}
	invoicePort, err := invoices.NewReportingPort(probe)
	if err != nil {
		t.Fatal(err)
	}
	purchaseRepo, err := purchases.NewRepository(probe, func() time.Time { return now }, func() (string, error) { return testUUID(9999), nil })
	if err != nil {
		t.Fatal(err)
	}
	accountRepo, err := accounting.NewJournalRepository(probe)
	if err != nil {
		t.Fatal(err)
	}
	probedRuleRepo, err := artefacts.NewRepository(probe)
	if err != nil {
		t.Fatal(err)
	}
	ports := BASBoundedSourcePorts{
		Invoices:   basSourceProducerInvoiceTrace{invoicePort, trace},
		Purchases:  basSourceProducerPurchaseTrace{purchaseRepo, trace},
		Accounting: basSourceProducerAccountingTrace{accountRepo, trace},
		Rules:      basSourceProducerRuleTrace{probedRuleRepo, trace},
	}
	request := BASBoundedSourceRequest{OrganisationID: org, ConfirmedPeriod: confirmedPeriod(testUUID(9963), org, 1, 96),
		ActiveRuleBundle: proto.Clone(bundle.Source).(*tammyv1.SourceRef), MaximumPayloadBytes: 8 * 1024 * 1024, MaximumCandidateRows: 1000}
	return workspace, tx, request, ports, trace, probe
}

func TestCaptureBASBoundedSourceRecordsNativeSQLCipherEmptyAndMixed(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		name := "empty"
		if mixed {
			name = "unreviewed-document-and-independent-manual-fact"
		}
		t.Run(name, func(t *testing.T) {
			workspace, tx, request, ports, trace, probe := newBASSourceProducerNativeFixture(t, mixed)
			beforeChanges := querySourceScalarInt(t, tx, "SELECT total_changes()")
			beforeRevisions := sourceScalarRevisionState(t, tx)
			got, err := CaptureBASBoundedSourceRecords(context.Background(), request, ports)
			if err != nil || got == nil {
				t.Fatalf("native source capture=%#v, %v", got, err)
			}
			wantMarkers := []string{`/* BAS invoice scalar summary */`, `/* BAS purchase scalar summary */`, `/* BAS tax fact scalar summary */`}
			if len(probe.queries) < len(wantMarkers) {
				t.Fatalf("native coordinator issued only %d SQL reads", len(probe.queries))
			}
			for i, marker := range wantMarkers {
				if !strings.Contains(probe.queries[i], marker) {
					t.Fatalf("initial native query[%d] does not match summary marker %q: %s", i, marker, probe.queries[i])
				}
			}
			if probe.writes != 0 {
				t.Fatalf("read-only native capture made %d writes", probe.writes)
			}
			wantPrefix := []string{"invoice.summary", "purchase.summary", "accounting.summary", "invoice.preflight", "accounting.preflight", "purchase.preflight", "rule.preflight", "purchase.decision.summary", "purchase.document.summary", "invoice.keys", "purchase.keys", "accounting.keys", "cash.summary", "purchase.document.read", "purchase.decision.read", "cash.read"}
			if len(trace.stages) < len(wantPrefix) {
				t.Fatalf("incomplete native trace %v", trace.stages)
			}
			for i, want := range wantPrefix {
				if trace.stages[i] != want {
					t.Fatalf("native stage[%d]=%q; want %q; full order %v", i, trace.stages[i], want, trace.stages)
				}
			}
			if trace.index("cash.read") < 0 || trace.index("invoice.capture") <= trace.index("cash.read") || trace.index("purchase.capture") <= trace.index("cash.read") || trace.index("accounting.capture") <= trace.index("cash.read") || trace.index("rule.capture") <= trace.index("cash.read") {
				t.Fatalf("full source bodies started before the bounded selectors: %v", trace.stages)
			}
			if got.RuleBundle == nil || got.RuleBundle.Bundle.Source == nil || got.RuleBundle.Bundle.Source.Id != request.ActiveRuleBundle.Id {
				t.Fatalf("native retained active rule bundle absent: %#v", got.RuleBundle)
			}
			if mixed {
				if len(got.AccountingFactKeys) != 1 || got.Accounting == nil || len(got.Accounting.Facts) != 1 || got.Accounting.Facts[0].SourceType != "manual_evidence" {
					t.Fatalf("manual fact was not independently retained: keys=%#v accounting=%#v", got.AccountingFactKeys, got.Accounting)
				}
				if got.PurchasePreflight.TaxFactCount != 1 || got.SharedFactProof == nil || got.SharedFactProof.Count != 1 || got.Purchases == nil || !got.Purchases.TaxFactBodiesReplaced || got.Purchases.OriginalTaxFactPayloadBytes <= 0 {
					t.Fatalf("linked raw fact reuse proof/view = preflight %#v proof %#v purchase %#v", got.PurchasePreflight, got.SharedFactProof, got.Purchases)
				}
				rawPurchaseFactRows := 0
				for _, row := range got.Purchases.Capture.Rows {
					if row.Table == "tax_facts" {
						rawPurchaseFactRows++
					}
				}
				if rawPurchaseFactRows != 0 || len(got.Accounting.Facts) != 1 {
					t.Fatalf("returned retained view duplicates a proved tax fact: raw purchase rows=%d canonical accounting facts=%d", rawPurchaseFactRows, len(got.Accounting.Facts))
				}
				if len(got.InvoiceKeys) != 1 || got.DocumentSummary.DocumentCount != 1 {
					t.Fatalf("mixed fixture missed invoice/unreviewed document census: invoices=%#v docs=%#v", got.InvoiceCandidates, got.DocumentSummary)
				}
			} else if got.Accounting == nil || len(got.Accounting.Facts) != 0 || got.DocumentSummary.DocumentCount != 0 {
				t.Fatalf("empty writer-backed capture=%#v", got)
			}
			if before := querySourceScalarInt(t, tx, "SELECT total_changes()"); before != beforeChanges {
				t.Fatalf("capture wrote to caller transaction: total_changes %d -> %d", beforeChanges, before)
			}
			assertSourceScalarTransactionUnchanged(t, tx, beforeChanges, beforeRevisions)
			if !tx.Authenticated() {
				t.Fatal("source capture ended the caller-owned SQLCipher transaction")
			}

			// The exact retained-byte boundary is accepted. One byte less rejects
			// after summaries, complete preflights and typed keys, but before any
			// selector bodies or full source bodies are read.
			request.MaximumPayloadBytes = got.PayloadReservation.ReservedPayloadBytes
			trace.stages = nil
			exact, err := CaptureBASBoundedSourceRecords(context.Background(), request, ports)
			if err != nil || exact == nil || exact.PayloadReservation.ReservedPayloadBytes != request.MaximumPayloadBytes {
				t.Fatalf("exact native byte boundary=%#v, %v", exact, err)
			}
			request.MaximumPayloadBytes--
			trace.stages = nil
			short, err := CaptureBASBoundedSourceRecords(context.Background(), request, ports)
			if short != nil || !strings.Contains(errString(err), "reserved payload bytes") {
				t.Fatalf("one-byte-short native budget=%#v, %v", short, err)
			}
			for _, stage := range trace.stages {
				if strings.HasSuffix(stage, ".read") || strings.HasSuffix(stage, ".capture") {
					t.Fatalf("one-byte-short native budget read body %q: %v", stage, trace.stages)
				}
			}
			if len(trace.stages) == 0 || (trace.stages[len(trace.stages)-1] != "cash.summary" && trace.stages[len(trace.stages)-1] != "purchase.document.summary") {
				t.Fatalf("budget rejection was not before document/decision/cash selector reads: %v", trace.stages)
			}
			assertSourceScalarTransactionUnchanged(t, tx, beforeChanges, beforeRevisions)

			if err := tx.Rollback(); err != nil {
				t.Fatalf("caller rollback after native source capture: %v", err)
			}
			readTx, err := workspace.Database.BeginEncryptedTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer readTx.Rollback()
			var organizations, invoicesN, documentsN, factsN int64
			if err := readTx.QueryRowContext(context.Background(), "SELECT count(*) FROM organisations").Scan(&organizations); err != nil {
				t.Fatal(err)
			}
			if err := readTx.QueryRowContext(context.Background(), "SELECT count(*) FROM invoices_v1").Scan(&invoicesN); err != nil {
				t.Fatal(err)
			}
			if err := readTx.QueryRowContext(context.Background(), "SELECT count(*) FROM documents").Scan(&documentsN); err != nil {
				t.Fatal(err)
			}
			if err := readTx.QueryRowContext(context.Background(), "SELECT count(*) FROM tax_facts").Scan(&factsN); err != nil {
				t.Fatal(err)
			}
			if organizations != 0 || invoicesN != 0 || documentsN != 0 || factsN != 0 {
				t.Fatalf("caller-owned transaction rollback retained fixture rows: organisations=%d invoices=%d documents=%d facts=%d", organizations, invoicesN, documentsN, factsN)
			}
		})
	}
}

func errString(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}
