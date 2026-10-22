package baspreparation

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/artefacts"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/invoices"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
)

const sourceProducerFakeRuleID = "01930000-0000-7000-8000-000000000099"

type sourceProducerFake struct {
	org                    string
	calls                  []string
	failAt                 string
	failure                error
	cancelAt               string
	cancel                 context.CancelFunc
	mismatchDocumentRead   bool
	accountingKeyCount     int
	accountingKeys         []accounting.TaxFactBASScalarKey
	invoicePayloadBytes    int64
	invoiceCandidate       bool
	mismatchInvoiceCapture bool
}

func newSourceProducerFake() *sourceProducerFake {
	return &sourceProducerFake{org: sourceScalarTestOrganisation, failure: errors.New("injected producer port failure")}
}

func (f *sourceProducerFake) call(ctx context.Context, stage string) error {
	f.calls = append(f.calls, stage)
	if f.cancelAt == stage && f.cancel != nil {
		f.cancel()
	}
	if f.failAt == stage {
		return f.failure
	}
	return ctx.Err()
}

func (f *sourceProducerFake) ports() BASBoundedSourcePorts {
	return BASBoundedSourcePorts{
		Invoices: sourceProducerFakeInvoices{f}, Purchases: sourceProducerFakePurchases{f},
		Accounting: sourceProducerFakeAccounting{f}, Rules: sourceProducerFakeRules{f},
	}
}

func (f *sourceProducerFake) request(maximumPayloadBytes, maximumCandidateRows int64) BASBoundedSourceRequest {
	return BASBoundedSourceRequest{
		OrganisationID: f.org, ConfirmedPeriod: confirmedPeriod(testUUID(9988), f.org, 1, 88),
		ActiveRuleBundle:    &tammyv1.SourceRef{Type: "tax_rule_bundle", Id: sourceProducerFakeRuleID, Revision: 1, ContentHash: testHash(89)},
		MaximumPayloadBytes: maximumPayloadBytes, MaximumCandidateRows: maximumCandidateRows,
	}
}

type sourceProducerFakeInvoices struct{ f *sourceProducerFake }

func (p sourceProducerFakeInvoices) SummarizeBASKeys(ctx context.Context, org string) (*invoices.BASScalarSummary, error) {
	if err := p.f.call(ctx, "invoice.summary"); err != nil {
		return nil, err
	}
	summary := &invoices.BASScalarSummary{OrganisationID: org}
	if p.f.invoiceCandidate {
		summary.RootCount = 1
		summary.ScalarKeyCount = 1
		summary.ScalarBytes = int64(512 + len(org) + len(testUUID(10000)))
	}
	return summary, nil
}
func (p sourceProducerFakeInvoices) ReadBASKeys(ctx context.Context, org string, _ invoices.BASScalarSummary, _, _ int64) ([]invoices.BASScalarKey, error) {
	if err := p.f.call(ctx, "invoice.keys"); err != nil {
		return nil, err
	}
	if p.f.invoiceCandidate {
		rootVersion := uint64(1)
		return []invoices.BASScalarKey{{Kind: invoices.BASScalarInvoice, OrganisationID: org, InvoiceID: testUUID(10000), RootVersion: &rootVersion}}, nil
	}
	return nil, nil
}
func (p sourceProducerFakeInvoices) PreflightBASWithPayloadBudget(ctx context.Context, _ string, _ *tammyv1.BasCivilDateRange, _ int64) (*invoices.BASReportingPreflight, error) {
	if err := p.f.call(ctx, "invoice.preflight"); err != nil {
		return nil, err
	}
	candidates := 0
	if p.f.invoiceCandidate {
		candidates = 1
	}
	return &invoices.BASReportingPreflight{CandidateCount: candidates, BasePayloadBytes: p.f.invoicePayloadBytes, PayloadBytes: p.f.invoicePayloadBytes}, nil
}
func (p sourceProducerFakeInvoices) CaptureBASWithPayloadBudget(ctx context.Context, _ string, _ *tammyv1.BasCivilDateRange, _ int64) (*invoices.BASReportingCapture, error) {
	if err := p.f.call(ctx, "invoice.capture"); err != nil {
		return nil, err
	}
	capturedBytes := p.f.invoicePayloadBytes
	if p.f.mismatchInvoiceCapture {
		capturedBytes++
	}
	return &invoices.BASReportingCapture{Fingerprint: testHash(71), BasePayloadBytes: p.f.invoicePayloadBytes, PayloadBytes: capturedBytes}, nil
}

type sourceProducerFakePurchases struct{ f *sourceProducerFake }

func (p sourceProducerFakePurchases) SummarizeBASKeys(ctx context.Context, org string) (*purchases.BASScalarSummary, error) {
	if err := p.f.call(ctx, "purchase.summary"); err != nil {
		return nil, err
	}
	return &purchases.BASScalarSummary{OrganisationID: org}, nil
}
func (p sourceProducerFakePurchases) ReadModernBASCandidates(ctx context.Context, org string, _ purchases.BASScalarSummary, _, _, _ int64) (*purchases.BASModernCandidateDiscovery, error) {
	if err := p.f.call(ctx, "purchase.keys"); err != nil {
		return nil, err
	}
	return &purchases.BASModernCandidateDiscovery{}, nil
}
func (p sourceProducerFakePurchases) PreflightCoverageForBASWithPayloadBudget(ctx context.Context, _ string, _ int64) (*purchases.PurchaseCoverageReportingPreflight, error) {
	if err := p.f.call(ctx, "purchase.preflight"); err != nil {
		return nil, err
	}
	return &purchases.PurchaseCoverageReportingPreflight{}, nil
}
func (p sourceProducerFakePurchases) CaptureCoverageForBASWithPayloadBudget(ctx context.Context, org string, _ int64) (*purchases.PurchaseCoverageReportingCapture, error) {
	if err := p.f.call(ctx, "purchase.capture"); err != nil {
		return nil, err
	}
	return &purchases.PurchaseCoverageReportingCapture{OrganisationID: org, Fingerprint: testHash(72)}, nil
}
func (p sourceProducerFakePurchases) SummarizeBASLegacyDecisions(ctx context.Context, org string) (*purchases.BASLegacyDecisionSummary, error) {
	if err := p.f.call(ctx, "purchase.decision.summary"); err != nil {
		return nil, err
	}
	return &purchases.BASLegacyDecisionSummary{OrganisationID: org}, nil
}
func (p sourceProducerFakePurchases) ReadBASLegacyDecisions(ctx context.Context, org string, expected purchases.BASLegacyDecisionSummary, _, _ int64) (*purchases.BASLegacyDecisionRead, error) {
	if err := p.f.call(ctx, "purchase.decision.read"); err != nil {
		return nil, err
	}
	return &purchases.BASLegacyDecisionRead{Summary: expected}, nil
}
func (p sourceProducerFakePurchases) SummarizeBASDocumentStates(ctx context.Context, org string) (*purchases.BASDocumentStateSummary, error) {
	if err := p.f.call(ctx, "purchase.document.summary"); err != nil {
		return nil, err
	}
	return &purchases.BASDocumentStateSummary{OrganisationID: org}, nil
}
func (p sourceProducerFakePurchases) ReadBASDocumentStates(ctx context.Context, org string, expected purchases.BASDocumentStateSummary, _, _ int64) (*purchases.BASDocumentStateRead, error) {
	if err := p.f.call(ctx, "purchase.document.read"); err != nil {
		return nil, err
	}
	if p.f.mismatchDocumentRead {
		expected.StateBytes++
	}
	return &purchases.BASDocumentStateRead{Summary: expected}, nil
}

type sourceProducerFakeAccounting struct{ f *sourceProducerFake }

func (p sourceProducerFakeAccounting) SummarizeTaxFactBASKeys(ctx context.Context, org string) (*accounting.TaxFactBASScalarSummary, error) {
	if err := p.f.call(ctx, "accounting.summary"); err != nil {
		return nil, err
	}
	count := int64(p.f.accountingKeyCount)
	return &accounting.TaxFactBASScalarSummary{OrganisationID: org, ScalarKeyCount: count, ScalarBytes: count * 512}, nil
}
func (p sourceProducerFakeAccounting) ReadTaxFactBASKeys(ctx context.Context, org string, _ accounting.TaxFactBASScalarSummary, _, _ int64) ([]accounting.TaxFactBASScalarKey, error) {
	if err := p.f.call(ctx, "accounting.keys"); err != nil {
		return nil, err
	}
	return p.f.accountingKeys, nil
}
func (p sourceProducerFakeAccounting) PreflightTaxFactsForReportingWithPayloadBudget(ctx context.Context, _ string, _ int64) (*accounting.TaxFactReportingPreflight, error) {
	if err := p.f.call(ctx, "accounting.preflight"); err != nil {
		return nil, err
	}
	bytes := int64(p.f.accountingKeyCount * 512)
	return &accounting.TaxFactReportingPreflight{FactCount: p.f.accountingKeyCount, BasePayloadBytes: bytes, PayloadBytes: bytes}, nil
}
func (p sourceProducerFakeAccounting) CaptureTaxFactsForReportingWithPayloadBudget(ctx context.Context, _ string, _ int64) (*accounting.TaxFactReportingCapture, error) {
	if err := p.f.call(ctx, "accounting.capture"); err != nil {
		return nil, err
	}
	return &accounting.TaxFactReportingCapture{Fingerprint: testHash(73), FactCount: p.f.accountingKeyCount, Facts: make([]accounting.TaxFactReportingRecord, 0)}, nil
}
func (p sourceProducerFakeAccounting) SummarizeBASLegacyCash(ctx context.Context, q accounting.BASLegacyCashQuery) (*accounting.BASLegacyCashSummary, error) {
	if err := p.f.call(ctx, "cash.summary"); err != nil {
		return nil, err
	}
	return &accounting.BASLegacyCashSummary{OrganisationID: q.OrganisationID}, nil
}
func (p sourceProducerFakeAccounting) ReadBASLegacyCash(ctx context.Context, q accounting.BASLegacyCashQuery, s accounting.BASLegacyCashSummary, _, _ int64) (*accounting.BASLegacyCashRead, error) {
	if err := p.f.call(ctx, "cash.read"); err != nil {
		return nil, err
	}
	return &accounting.BASLegacyCashRead{Summary: s}, nil
}

type sourceProducerFakeRules struct{ f *sourceProducerFake }

func (p sourceProducerFakeRules) PreflightForReportingWithPayloadBudget(ctx context.Context, _ string, ref *tammyv1.SourceRef, _ int64) (*artefacts.RuleBundleReportingPreflight, error) {
	if err := p.f.call(ctx, "rule.preflight"); err != nil {
		return nil, err
	}
	return &artefacts.RuleBundleReportingPreflight{BundleID: ref.Id}, nil
}
func (p sourceProducerFakeRules) GetForReportingWithPayloadBudget(ctx context.Context, org string, ref *tammyv1.SourceRef, _ int64) (*artefacts.RuleBundleReportingRecord, error) {
	if err := p.f.call(ctx, "rule.capture"); err != nil {
		return nil, err
	}
	return &artefacts.RuleBundleReportingRecord{ContentHash: testHash(74), OrganisationID: org, Bundle: artefacts.RuleBundle{Source: ref}}, nil
}

func TestCaptureBASBoundedSourceRecordsOrdersReservationsBeforeBodies(t *testing.T) {
	f := newSourceProducerFake()
	got, err := CaptureBASBoundedSourceRecords(context.Background(), f.request(0, 1000), f.ports())
	if err != nil || got == nil {
		t.Fatalf("empty source capture = %#v, %v", got, err)
	}
	wantPrefix := []string{
		"invoice.summary", "purchase.summary", "accounting.summary",
		"invoice.preflight", "accounting.preflight", "purchase.preflight", "rule.preflight",
		"purchase.decision.summary", "purchase.document.summary",
		"invoice.keys", "purchase.keys", "accounting.keys", "cash.summary",
		"purchase.document.read", "purchase.decision.read", "cash.read",
		"invoice.capture", "purchase.capture", "accounting.capture", "rule.capture",
	}
	if strings.Join(f.calls, "|") != strings.Join(wantPrefix, "|") {
		t.Fatalf("producer stage order = %v; want %v", f.calls, wantPrefix)
	}
	if got.Purchases == nil || !got.Purchases.TaxFactBodiesReplaced || got.SharedFactProof == nil || got.SharedFactProof.Count != 0 {
		t.Fatalf("successful empty capture omitted explicit proof-reuse representation: %#v", got.Purchases)
	}
}

func TestCaptureBASBoundedSourceRecordsByteBoundaryPrecedesSelectorAndFullBodies(t *testing.T) {
	for _, tc := range []struct {
		name      string
		budget    int64
		wantError bool
	}{{"exact", 512, false}, {"one_short", 511, true}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSourceProducerFake()
			f.invoicePayloadBytes = 512
			got, err := CaptureBASBoundedSourceRecords(context.Background(), f.request(tc.budget, 1000), f.ports())
			if tc.wantError {
				if got != nil || !errors.Is(err, ErrSourceBound) || !strings.Contains(err.Error(), "reserved payload bytes") {
					t.Fatalf("one-byte-short capture = %#v, %v", got, err)
				}
				for _, stage := range f.calls {
					if strings.HasSuffix(stage, ".read") || strings.HasSuffix(stage, ".capture") {
						t.Fatalf("payload rejection happened after selector/body read %q: %v", stage, f.calls)
					}
				}
				if got := f.calls[len(f.calls)-1]; got != "purchase.document.summary" {
					t.Fatalf("last stage before budget rejection=%q; want last independent preflight", got)
				}
				return
			}
			if err != nil || got == nil || got.PayloadReservation.ReservedPayloadBytes != tc.budget {
				t.Fatalf("exact-boundary capture=%#v, %v", got, err)
			}
		})
	}
}

func TestCaptureBASBoundedSourceRecordsCapsAllIndependentCandidatesBeforeCapture(t *testing.T) {
	f := newSourceProducerFake()
	f.accountingKeyCount = 1000
	f.invoiceCandidate = true
	f.accountingKeys = make([]accounting.TaxFactBASScalarKey, f.accountingKeyCount)
	for i := range f.accountingKeys {
		f.accountingKeys[i] = accounting.TaxFactBASScalarKey{ID: testUUID(uint64(20000 + i)), OrganisationID: f.org,
			JournalLineID: testUUID(uint64(22000 + i)), JournalID: testUUID(uint64(24000 + i)), LineNumber: int64(i + 1), SourceType: "manual", SourceID: testUUID(26000), SourceRevision: 1}
	}
	got, err := CaptureBASBoundedSourceRecords(context.Background(), f.request(8*1024*1024, 1000), f.ports())
	if got != nil || !errors.Is(err, ErrSourceBound) || !strings.Contains(err.Error(), "candidate") {
		t.Fatalf("1001 independent fact candidates = %#v, %v; want bounded rejection", got, err)
	}
	for _, stage := range f.calls {
		if strings.HasSuffix(stage, ".capture") {
			t.Fatalf("candidate cap was applied after body capture %q: %v", stage, f.calls)
		}
	}
}

func TestCaptureBASBoundedSourceRecordsPropagatesCancellationAndSelectorMismatchWithoutBodies(t *testing.T) {
	t.Run("cancel", func(t *testing.T) {
		f := newSourceProducerFake()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		f.cancelAt = "purchase.decision.read"
		f.cancel = cancel
		got, err := CaptureBASBoundedSourceRecords(ctx, f.request(8*1024*1024, 1000), f.ports())
		if got != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled source capture=%#v, %v", got, err)
		}
		for _, stage := range f.calls {
			if strings.HasSuffix(stage, ".capture") {
				t.Fatalf("cancellation returned partial bodies after %q: %v", stage, f.calls)
			}
		}
	})
	t.Run("summary mismatch", func(t *testing.T) {
		f := newSourceProducerFake()
		f.mismatchDocumentRead = true
		got, err := CaptureBASBoundedSourceRecords(context.Background(), f.request(8*1024*1024, 1000), f.ports())
		if got != nil || !errors.Is(err, ErrSourceReservation) {
			t.Fatalf("changed selector summary=%#v, %v", got, err)
		}
		for _, stage := range f.calls {
			if strings.HasSuffix(stage, ".capture") {
				t.Fatalf("selector mismatch was accepted before body capture %q: %v", stage, f.calls)
			}
		}
	})
}

func TestCaptureBASBoundedSourceRecordsRejectsBodyMismatchWithoutReturningPartialSources(t *testing.T) {
	f := newSourceProducerFake()
	f.mismatchInvoiceCapture = true
	got, err := CaptureBASBoundedSourceRecords(context.Background(), f.request(8*1024*1024, 1000), f.ports())
	if got != nil || !errors.Is(err, ErrSourceReservation) {
		t.Fatalf("mismatched invoice capture=%#v, %v", got, err)
	}
	if f.calls[len(f.calls)-1] != "invoice.capture" {
		t.Fatalf("body mismatch did not stop at first full capture: %v", f.calls)
	}
}
