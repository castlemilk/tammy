package artefacts

import (
	"errors"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

func TestEmbeddedAUGSTV1BundleIsImmutableAndSelfAuthenticating(t *testing.T) {
	bundle, err := LoadAUGSTV1()
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Version != "au_gst_v1" || bundle.Source == nil || bundle.Source.Type != "tax_rule_bundle" ||
		len(bundle.Source.ContentHash) != 32 || len(bundle.TaxCodes) < 4 {
		t.Fatalf("bundle = %#v", bundle)
	}
	want := map[string]struct {
		treatment tammyv1.TaxTreatment
		rate      int64
	}{
		"GST":      {tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE, 100_000},
		"GST_FREE": {tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE, 0},
		"INPUT":    {tammyv1.TaxTreatment_TAX_TREATMENT_INPUT_TAXED, 0},
		"N-T":      {tammyv1.TaxTreatment_TAX_TREATMENT_OUT_OF_SCOPE, 0},
	}
	for _, code := range bundle.TaxCodes {
		expected, ok := want[code.Code]
		if ok {
			if code.Treatment != expected.treatment || code.RateMillionths != expected.rate {
				t.Fatalf("code %q = %#v", code.Code, code)
			}
			delete(want, code.Code)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing tax codes: %v", want)
	}

	bundle.TaxCodes[0].Label = "mutated"
	bundle.Source.ContentHash[0] ^= 0xff
	reloaded, err := LoadAUGSTV1()
	if err != nil || reloaded.TaxCodes[0].Label == "mutated" || reloaded.Source.ContentHash[0] == bundle.Source.ContentHash[0] {
		t.Fatalf("reloaded bundle = %#v, %v", reloaded, err)
	}
}

func TestEmbeddedBundleRejectsByteOrChecksumMutation(t *testing.T) {
	bundleBytes, checksum := embeddedAUGSTV1ForVerification()
	bundleBytes[0] ^= 0xff
	if _, err := ParseRuleBundle(bundleBytes, checksum); !errors.Is(err, ErrInvalidRuleBundle) {
		t.Fatalf("mutated bytes error = %v", err)
	}
	bundleBytes, checksum = embeddedAUGSTV1ForVerification()
	checksum[0] ^= 0xff
	if _, err := ParseRuleBundle(bundleBytes, checksum); !errors.Is(err, ErrInvalidRuleBundle) {
		t.Fatalf("mutated checksum error = %v", err)
	}
}
