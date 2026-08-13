package accounting

import (
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

func TestBuildNonCashTaxFactPreservesOriginalAttributedAndRemainingEquations(t *testing.T) {
	for _, test := range []struct {
		name      string
		treatment tammyv1.TaxTreatment
		net, gst  int64
	}{
		{name: "taxable", treatment: tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE, net: 29000, gst: 2900},
		{name: "gst free", treatment: tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE, net: 6400, gst: 0},
		{name: "input taxed", treatment: tammyv1.TaxTreatment_TAX_TREATMENT_INPUT_TAXED, net: 12000, gst: 0},
		{name: "credit", treatment: tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE, net: -29000, gst: -2900},
	} {
		t.Run(test.name, func(t *testing.T) {
			code := &tammyv1.TaxCode{Id: "018f0000-0000-7000-8000-000000000601", Code: "GST",
				Treatment: test.treatment, Rule: &tammyv1.SourceRef{Type: "tax_rule_bundle",
					Id: "018f0000-0000-7000-8000-000000000602", Revision: 1, ContentHash: make([]byte, 32)}}
			fact, err := BuildNonCashTaxFact("fact", "018f0000-0000-7000-8000-000000000020",
				"018f0000-0000-7000-8000-000000000603", code, test.net, test.gst, nil,
				"018f0000-0000-7000-8000-000000000604")
			if err != nil {
				t.Fatal(err)
			}
			if fact.OriginalGrossMinor != test.net+test.gst || fact.AttributedGrossMinor != fact.OriginalGrossMinor ||
				fact.RemainingGrossMinor != 0 || ValidateTaxFact(fact) != nil {
				t.Fatalf("fact = %#v", fact)
			}
		})
	}
}
