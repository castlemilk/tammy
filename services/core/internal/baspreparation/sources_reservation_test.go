package baspreparation

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/invoices"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
)

const sourceScalarTestOrganisation = "01930000-0000-7000-8000-000000000001"

type sourceScalarFixture struct {
	invoices   invoices.BASScalarSummary
	purchases  purchases.BASScalarSummary
	accounting accounting.TaxFactBASScalarSummary
	calls      []string
	contexts   []context.Context
	owners     []string
	errAt      string
	nilAt      string
	err        error
	after      func(string)
}

type sourceScalarInvoicePort struct{ fixture *sourceScalarFixture }
type sourceScalarPurchasePort struct{ fixture *sourceScalarFixture }
type sourceScalarAccountingPort struct{ fixture *sourceScalarFixture }

func newSourceScalarFixture() *sourceScalarFixture {
	f := &sourceScalarFixture{
		invoices: invoices.BASScalarSummary{OrganisationID: sourceScalarTestOrganisation,
			RootCount: 2, PostingCount: 1, IncomeLineCount: 3, ReceiptCount: 2, AllocationCount: 2,
			ScalarKeyCount: 10, ScalarBytes: 6400},
		purchases: purchases.BASScalarSummary{OrganisationID: sourceScalarTestOrganisation,
			RootCount: 1, ReceiptCount: 2, ScalarKeyCount: 8, ScalarBytes: 6144},
		accounting: accounting.TaxFactBASScalarSummary{OrganisationID: sourceScalarTestOrganisation,
			ScalarKeyCount: 3, ScalarBytes: 2700},
	}
	f.purchases.FamilyCounts[purchases.BASScalarPosting-1] = 1
	f.purchases.FamilyCounts[purchases.BASScalarPostingAllocation-1] = 2
	f.purchases.FamilyCounts[purchases.BASScalarPayment-1] = 2
	f.purchases.FamilyCounts[purchases.BASScalarPaymentAllocation-1] = 2
	f.purchases.FamilyCounts[purchases.BASScalarDocument-1] = 1
	return f
}

func (f *sourceScalarFixture) ports() BASSourceScalarPorts {
	return BASSourceScalarPorts{Invoices: sourceScalarInvoicePort{f}, Purchases: sourceScalarPurchasePort{f}, Accounting: sourceScalarAccountingPort{f}}
}

func (f *sourceScalarFixture) call(ctx context.Context, owner, name string) error {
	f.calls = append(f.calls, name)
	f.contexts = append(f.contexts, ctx)
	f.owners = append(f.owners, owner)
	if f.after != nil {
		f.after(name)
	}
	if f.errAt == name {
		return f.err
	}
	return nil
}

func (p sourceScalarInvoicePort) SummarizeBASKeys(ctx context.Context, owner string) (*invoices.BASScalarSummary, error) {
	if err := p.fixture.call(ctx, owner, "invoices"); err != nil {
		return nil, err
	}
	if p.fixture.nilAt == "invoices" {
		return nil, nil
	}
	return &p.fixture.invoices, nil
}

func (p sourceScalarPurchasePort) SummarizeBASKeys(ctx context.Context, owner string) (*purchases.BASScalarSummary, error) {
	if err := p.fixture.call(ctx, owner, "purchases"); err != nil {
		return nil, err
	}
	if p.fixture.nilAt == "purchases" {
		return nil, nil
	}
	return &p.fixture.purchases, nil
}

func (p sourceScalarAccountingPort) SummarizeTaxFactBASKeys(ctx context.Context, owner string) (*accounting.TaxFactBASScalarSummary, error) {
	if err := p.fixture.call(ctx, owner, "accounting"); err != nil {
		return nil, err
	}
	if p.fixture.nilAt == "accounting" {
		return nil, nil
	}
	return &p.fixture.accounting, nil
}

func TestReserveBASSourceScalarsPreservesSummariesAndSeparatesCounts(t *testing.T) {
	f := newSourceScalarFixture()
	ctx := context.WithValue(context.Background(), sourceScalarContextKey{}, "caller transaction")
	got, err := ReserveBASSourceScalars(ctx, sourceScalarTestOrganisation, f.ports(), 8*1024*1024)
	if err != nil || got == nil {
		t.Fatalf("reserve mixed source summaries: result=%v error=%v", got, err)
	}
	if got.RootCount != 3 || got.ReceiptCount != 4 || got.ScalarKeyCount != 21 || got.ScalarBytes != 15244 {
		t.Fatalf("combined reservation = %#v; want 3 roots, 4 receipts, 21 keys, 15244 bytes", got)
	}
	if got.Invoices != f.invoices || got.Purchases != f.purchases || got.Accounting != f.accounting {
		t.Fatal("reservation changed an original module summary")
	}
	if !reflect.DeepEqual(f.calls, []string{"invoices", "purchases", "accounting"}) {
		t.Fatalf("aggregate read order = %v", f.calls)
	}
	for index, received := range f.contexts {
		if received != ctx || f.owners[index] != sourceScalarTestOrganisation {
			t.Fatal("summary did not receive the exact caller context and organisation")
		}
	}
	f.invoices.RootCount++
	f.purchases.FamilyCounts[0]++
	f.accounting.ScalarBytes++
	if got.Invoices.RootCount != 2 || got.Purchases.FamilyCounts[0] != 1 || got.Accounting.ScalarBytes != 2700 {
		t.Fatal("reservation aliases a mutable port summary")
	}
}

type sourceScalarContextKey struct{}

func TestReserveBASSourceScalarsEmptyAndCallerByteBoundary(t *testing.T) {
	f := newSourceScalarFixture()
	f.invoices = invoices.BASScalarSummary{OrganisationID: sourceScalarTestOrganisation}
	f.purchases = purchases.BASScalarSummary{OrganisationID: sourceScalarTestOrganisation}
	f.accounting = accounting.TaxFactBASScalarSummary{OrganisationID: sourceScalarTestOrganisation}
	got, err := ReserveBASSourceScalars(context.Background(), sourceScalarTestOrganisation, f.ports(), 0)
	if err != nil || got == nil || got.RootCount != 0 || got.ReceiptCount != 0 || got.ScalarKeyCount != 0 || got.ScalarBytes != 0 {
		t.Fatalf("empty zero-budget reservation = %#v, %v", got, err)
	}
	for _, budget := range []int64{15244, 15243} {
		f := newSourceScalarFixture()
		got, err := ReserveBASSourceScalars(context.Background(), sourceScalarTestOrganisation, f.ports(), budget)
		if budget == 15244 {
			if err != nil || got == nil || got.ScalarBytes != budget {
				t.Fatalf("exact byte boundary = %#v, %v", got, err)
			}
		} else {
			assertSourceScalarBound(t, got, err, "scalar bytes", budget)
		}
	}
}

// These synthetic summaries exercise reservation arithmetic, not body validity
// or acceptance of 1,000 complete financial sources.
func TestReserveBASSourceScalarsSharedCountAndCapacityBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name     string
		invoice  invoices.BASScalarSummary
		purchase purchases.BASScalarSummary
		facts    accounting.TaxFactBASScalarSummary
		resource string
		limit    int64
	}{
		{name: "exact roots and receipts with independent facts",
			invoice: invoices.BASScalarSummary{RootCount: 600, ReceiptCount: 600, ScalarKeyCount: 1200, ScalarBytes: 768000},
			purchase: purchases.BASScalarSummary{RootCount: 400, ReceiptCount: 400, ScalarKeyCount: 800, ScalarBytes: 512000,
				FamilyCounts: [purchases.BASScalarFamilyCount]int64{400, 0, 400}},
			facts: accounting.TaxFactBASScalarSummary{ScalarKeyCount: 1000, ScalarBytes: 640000}},
		{name: "shared roots exceed while modules fit",
			invoice: invoices.BASScalarSummary{RootCount: 600, ScalarKeyCount: 600, ScalarBytes: 384000},
			purchase: purchases.BASScalarSummary{RootCount: 401, ScalarKeyCount: 401, ScalarBytes: 256640,
				FamilyCounts: [purchases.BASScalarFamilyCount]int64{401}}, resource: "source roots", limit: 1000},
		{name: "shared receipts exceed while modules fit",
			invoice: invoices.BASScalarSummary{ReceiptCount: 600, ScalarKeyCount: 600, ScalarBytes: 384000},
			purchase: purchases.BASScalarSummary{ReceiptCount: 401, ScalarKeyCount: 401, ScalarBytes: 256640,
				FamilyCounts: [purchases.BASScalarFamilyCount]int64{0, 0, 401}}, resource: "receipt sources", limit: 1000},
		{name: "exact scalar capacity", facts: accounting.TaxFactBASScalarSummary{ScalarKeyCount: 16384, ScalarBytes: 8 * 1024 * 1024}},
		{name: "one physical key over", invoice: invoices.BASScalarSummary{IncomeLineCount: 1, ScalarKeyCount: 1, ScalarBytes: 512},
			facts: accounting.TaxFactBASScalarSummary{ScalarKeyCount: 16384, ScalarBytes: 8 * 1024 * 1024}, resource: "scalar keys", limit: 16384},
		{name: "shared bytes exceed while modules fit", invoice: invoices.BASScalarSummary{RootCount: 1, ScalarKeyCount: 1, ScalarBytes: 4194304},
			facts: accounting.TaxFactBASScalarSummary{ScalarKeyCount: 1, ScalarBytes: 4194305}, resource: "scalar bytes", limit: 8 * 1024 * 1024},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSourceScalarFixture()
			f.invoices, f.purchases, f.accounting = tc.invoice, tc.purchase, tc.facts
			f.invoices.OrganisationID, f.purchases.OrganisationID, f.accounting.OrganisationID = sourceScalarTestOrganisation, sourceScalarTestOrganisation, sourceScalarTestOrganisation
			got, err := ReserveBASSourceScalars(context.Background(), sourceScalarTestOrganisation, f.ports(), math.MaxInt64)
			if tc.resource != "" {
				assertSourceScalarBound(t, got, err, tc.resource, tc.limit)
			} else if err != nil || got == nil {
				t.Fatalf("exact reservation boundary = %#v, %v", got, err)
			}
		})
	}
}

func assertSourceScalarBound(t *testing.T, got *BASSourceScalarReservation, err error, resource string, limit int64) {
	t.Helper()
	var bound *SourceBoundError
	if got != nil || !errors.Is(err, ErrSourceReservation) || !errors.Is(err, ErrSourceBound) || !errors.As(err, &bound) || bound.Resource != resource || bound.Limit != limit {
		t.Fatalf("reservation=%#v error=%v; want no result and %s bound %d", got, err, resource, limit)
	}
}

func TestReserveBASSourceScalarsRejectsInvalidInputsBeforeReads(t *testing.T) {
	for _, name := range []string{"nil context", "invalid owner", "negative budget", "missing invoices", "missing purchases", "missing accounting", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			f := newSourceScalarFixture()
			ports, ctx, owner, budget := f.ports(), context.Background(), sourceScalarTestOrganisation, int64(8*1024*1024)
			switch name {
			case "nil context":
				ctx = nil
			case "invalid owner":
				owner = "other"
			case "negative budget":
				budget = -1
			case "missing invoices":
				ports.Invoices = nil
			case "missing purchases":
				ports.Purchases = nil
			case "missing accounting":
				ports.Accounting = nil
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			got, err := ReserveBASSourceScalars(ctx, owner, ports, budget)
			if got != nil || !errors.Is(err, ErrSourceReservation) || len(f.calls) != 0 {
				t.Fatalf("invalid input result=%#v error=%v reads=%v", got, err, f.calls)
			}
			if name == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost caller cancellation: %v", err)
			}
		})
	}
}

func TestReserveBASSourceScalarsStopsOnErrorNilOrCancellation(t *testing.T) {
	for index, module := range []string{"invoices", "purchases", "accounting"} {
		for _, failure := range []string{"error", "nil", "cancel"} {
			t.Run(module+"/"+failure, func(t *testing.T) {
				f := newSourceScalarFixture()
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				marker := errors.New("module failed")
				switch failure {
				case "error":
					f.errAt, f.err = module, marker
				case "nil":
					f.nilAt = module
				case "cancel":
					f.after = func(name string) {
						if name == module {
							cancel()
						}
					}
				}
				got, err := ReserveBASSourceScalars(ctx, sourceScalarTestOrganisation, f.ports(), 8*1024*1024)
				if got != nil || !errors.Is(err, ErrSourceReservation) || len(f.calls) != index+1 {
					t.Fatalf("failure result=%#v error=%v reads=%v", got, err, f.calls)
				}
				if failure == "error" && !errors.Is(err, marker) || failure == "cancel" && !errors.Is(err, context.Canceled) {
					t.Fatalf("lost underlying error: %v", err)
				}
			})
		}
	}
}

func TestReserveBASSourceScalarsRejectsInconsistentOrOverflowingSummaries(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*sourceScalarFixture)
	}{
		{"invoice foreign owner", func(f *sourceScalarFixture) { f.invoices.OrganisationID = "other" }},
		{"purchase foreign owner", func(f *sourceScalarFixture) { f.purchases.OrganisationID = "other" }},
		{"accounting foreign owner", func(f *sourceScalarFixture) { f.accounting.OrganisationID = "other" }},
		{"negative invoice count", func(f *sourceScalarFixture) { f.invoices.PostingCount = -1 }},
		{"negative purchase family", func(f *sourceScalarFixture) { f.purchases.FamilyCounts[14] = -1 }},
		{"negative accounting bytes", func(f *sourceScalarFixture) { f.accounting.ScalarBytes = -1 }},
		{"invoice family count mismatch", func(f *sourceScalarFixture) { f.invoices.ScalarKeyCount++ }},
		{"invoice postings exceed roots", func(f *sourceScalarFixture) { f.invoices.RootCount = 0; f.invoices.IncomeLineCount += 2 }},
		{"purchase family count mismatch", func(f *sourceScalarFixture) { f.purchases.ScalarKeyCount++ }},
		{"purchase roots below modern postings", func(f *sourceScalarFixture) { f.purchases.RootCount = 0 }},
		{"purchase receipts below modern payments", func(f *sourceScalarFixture) { f.purchases.ReceiptCount = 1 }},
		{"purchase roots exceed root families", func(f *sourceScalarFixture) { f.purchases.RootCount = 3 }},
		{"purchase receipts exceed receipt families", func(f *sourceScalarFixture) { f.purchases.ReceiptCount = 3 }},
		{"invoice missing row allowance", func(f *sourceScalarFixture) { f.invoices.ScalarBytes = 5119 }},
		{"purchase missing row allowance", func(f *sourceScalarFixture) { f.purchases.ScalarBytes = 4095 }},
		{"accounting missing row allowance", func(f *sourceScalarFixture) { f.accounting.ScalarBytes = 1535 }},
		{"accounting root must be zero", func(f *sourceScalarFixture) { f.accounting.RootCount = 1 }},
		{"accounting receipt must be zero", func(f *sourceScalarFixture) { f.accounting.ReceiptCount = 1 }},
		{"empty census charged bytes", func(f *sourceScalarFixture) { f.accounting.ScalarKeyCount = 0 }},
		{"purchase root larger than census", func(f *sourceScalarFixture) { f.purchases.RootCount = 9 }},
		{"purchase receipts larger than census", func(f *sourceScalarFixture) { f.purchases.ReceiptCount = 9 }},
		{"invoice family sum overflow", func(f *sourceScalarFixture) {
			f.invoices.RootCount = math.MaxInt64
			f.invoices.PostingCount = math.MaxInt64
		}},
		{"purchase family sum overflow", func(f *sourceScalarFixture) {
			f.purchases.FamilyCounts[0] = math.MaxInt64
			f.purchases.FamilyCounts[1] = math.MaxInt64
		}},
		{"accounting allowance overflow", func(f *sourceScalarFixture) {
			f.accounting.ScalarKeyCount = math.MaxInt64
			f.accounting.ScalarBytes = math.MaxInt64
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSourceScalarFixture()
			tc.edit(f)
			got, err := ReserveBASSourceScalars(context.Background(), sourceScalarTestOrganisation, f.ports(), 8*1024*1024)
			if got != nil || !errors.Is(err, ErrSourceReservation) {
				t.Fatalf("invalid summary result=%#v error=%v", got, err)
			}
		})
	}
}
