package taxrules_test

import (
	"errors"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/taxrules"
)

func TestAUGSTV1NonCashKernelUsesExactScaledIntegerRules(t *testing.T) {
	kernel, err := taxrules.NewKernel("au_gst_v1")
	if err != nil {
		t.Fatal(err)
	}
	rule := taxrules.Rule{Code: "GST", Treatment: tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE,
		RateMillionths: 100_000, Source: validTaxRuleSource()}
	for _, test := range []struct {
		taxable int64
		wantTax int64
	}{
		{taxable: 10_000, wantTax: 1_000},
		{taxable: 5, wantTax: 1},
		{taxable: -5, wantTax: -1},
	} {
		fact, err := kernel.CalculateNonCash(rule, &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: test.taxable})
		if err != nil || fact.Tax == nil || fact.Tax.MinorUnits != test.wantTax || fact.Taxable.MinorUnits != test.taxable ||
			fact.Rule == rule.Source {
			t.Fatalf("CalculateNonCash(%d) = %#v, %v", test.taxable, fact, err)
		}
	}

	zeroRule := rule
	zeroRule.Treatment = tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE
	zeroRule.RateMillionths = 0
	fact, err := kernel.CalculateNonCash(zeroRule, &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 100})
	if err != nil || fact.Tax.MinorUnits != 0 {
		t.Fatalf("GST-free fact = %#v, %v", fact, err)
	}
}

func TestTaxKernelRejectsWrongVersionCurrencyAndRule(t *testing.T) {
	if _, err := taxrules.NewKernel("future"); !errors.Is(err, taxrules.ErrInvalidRule) {
		t.Fatalf("future version error = %v", err)
	}
	kernel, err := taxrules.NewKernel("au_gst_v1")
	if err != nil {
		t.Fatal(err)
	}
	rule := taxrules.Rule{Code: "GST", Treatment: tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE,
		RateMillionths: 100_000, Source: validTaxRuleSource()}
	for _, mutate := range []func(*taxrules.Rule, *tammyv1.Money){
		func(_ *taxrules.Rule, money *tammyv1.Money) { money.CurrencyCode = "USD" },
		func(rule *taxrules.Rule, _ *tammyv1.Money) { rule.RateMillionths = 1_000_001 },
		func(rule *taxrules.Rule, _ *tammyv1.Money) { rule.Source.ContentHash = nil },
	} {
		candidate := rule
		candidate.Source = &tammyv1.SourceRef{Type: rule.Source.Type, Id: rule.Source.Id,
			Revision: rule.Source.Revision, ContentHash: append([]byte(nil), rule.Source.ContentHash...)}
		money := &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 100}
		mutate(&candidate, money)
		if _, err := kernel.CalculateNonCash(candidate, money); !errors.Is(err, taxrules.ErrInvalidRule) {
			t.Fatalf("invalid calculation error = %v", err)
		}
	}
}

func validTaxRuleSource() *tammyv1.SourceRef {
	return &tammyv1.SourceRef{Type: "tax_rule_bundle", Id: "018f0000-0000-7000-8000-000000000022",
		Revision: 1, ContentHash: make([]byte, 32)}
}
