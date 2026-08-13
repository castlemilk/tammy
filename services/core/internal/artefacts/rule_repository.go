// Package artefacts owns immutable versioned tax rule bundles and catalogues.
package artefacts

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

var ErrInvalidRuleBundle = errors.New("artefacts: invalid rule bundle")

var ruleCodePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_-]{0,31}$`)

type TaxCodeRule struct {
	ID             string
	Code           string
	Label          string
	Treatment      tammyv1.TaxTreatment
	RateMillionths int64
}

type RuleBundle struct {
	Version       string
	EffectiveFrom string
	Source        *tammyv1.SourceRef
	TaxCodes      []TaxCodeRule
	RetainedBytes []byte
}

type bundleDocument struct {
	BundleID      string            `json:"bundle_id"`
	Version       string            `json:"version"`
	Revision      uint64            `json:"revision"`
	EffectiveFrom string            `json:"effective_from"`
	TaxCodes      []taxCodeDocument `json:"tax_codes"`
}

type taxCodeDocument struct {
	ID             string `json:"id"`
	Code           string `json:"code"`
	Label          string `json:"label"`
	Treatment      string `json:"treatment"`
	RateMillionths int64  `json:"rate_millionths"`
}

//go:embed bundles/au_gst_v1.pb.json
var embeddedAUGSTV1 []byte

//go:embed bundles/au_gst_v1.sha256
var embeddedAUGSTV1Checksum []byte

func LoadAUGSTV1() (RuleBundle, error) {
	digest, err := hex.DecodeString(strings.TrimSpace(string(embeddedAUGSTV1Checksum)))
	if err != nil || len(digest) != sha256.Size {
		return RuleBundle{}, ErrInvalidRuleBundle
	}
	return ParseRuleBundle(embeddedAUGSTV1, digest)
}

func ParseRuleBundle(retained, expectedSHA256 []byte) (RuleBundle, error) {
	if len(retained) == 0 || len(expectedSHA256) != sha256.Size {
		return RuleBundle{}, ErrInvalidRuleBundle
	}
	digest := sha256.Sum256(retained)
	if !bytes.Equal(digest[:], expectedSHA256) {
		return RuleBundle{}, ErrInvalidRuleBundle
	}
	decoder := json.NewDecoder(bytes.NewReader(retained))
	decoder.DisallowUnknownFields()
	var document bundleDocument
	if err := decoder.Decode(&document); err != nil {
		return RuleBundle{}, ErrInvalidRuleBundle
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) || document.Version != "au_gst_v1" ||
		!ids.IsCanonicalV7(document.BundleID) || document.Revision != 1 || document.EffectiveFrom != "2000-07-01" ||
		len(document.TaxCodes) == 0 || len(document.TaxCodes) > 200 {
		return RuleBundle{}, ErrInvalidRuleBundle
	}
	bundle := RuleBundle{Version: document.Version, EffectiveFrom: document.EffectiveFrom,
		Source: &tammyv1.SourceRef{Type: "tax_rule_bundle", Id: document.BundleID, Revision: document.Revision,
			ContentHash: append([]byte(nil), digest[:]...)},
		TaxCodes: make([]TaxCodeRule, 0, len(document.TaxCodes)), RetainedBytes: append([]byte(nil), retained...)}
	seenIDs := make(map[string]struct{}, len(document.TaxCodes))
	seenCodes := make(map[string]struct{}, len(document.TaxCodes))
	for _, raw := range document.TaxCodes {
		treatment, ok := parseTreatment(raw.Treatment)
		if !ok || !ids.IsCanonicalV7(raw.ID) || !ruleCodePattern.MatchString(raw.Code) ||
			raw.Label == "" || len(raw.Label) > 160 || strings.TrimSpace(raw.Label) != raw.Label ||
			raw.RateMillionths < 0 || raw.RateMillionths > 1_000_000 ||
			(treatment == tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE) != (raw.RateMillionths > 0) {
			return RuleBundle{}, ErrInvalidRuleBundle
		}
		if _, duplicate := seenIDs[raw.ID]; duplicate {
			return RuleBundle{}, ErrInvalidRuleBundle
		}
		if _, duplicate := seenCodes[raw.Code]; duplicate {
			return RuleBundle{}, ErrInvalidRuleBundle
		}
		seenIDs[raw.ID] = struct{}{}
		seenCodes[raw.Code] = struct{}{}
		bundle.TaxCodes = append(bundle.TaxCodes, TaxCodeRule{ID: raw.ID, Code: raw.Code, Label: raw.Label,
			Treatment: treatment, RateMillionths: raw.RateMillionths})
	}
	return bundle, nil
}

func embeddedAUGSTV1ForVerification() ([]byte, []byte) {
	digest, _ := hex.DecodeString(strings.TrimSpace(string(embeddedAUGSTV1Checksum)))
	return append([]byte(nil), embeddedAUGSTV1...), append([]byte(nil), digest...)
}

func parseTreatment(value string) (tammyv1.TaxTreatment, bool) {
	switch value {
	case "TAXABLE":
		return tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE, true
	case "GST_FREE":
		return tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE, true
	case "INPUT_TAXED":
		return tammyv1.TaxTreatment_TAX_TREATMENT_INPUT_TAXED, true
	case "OUT_OF_SCOPE":
		return tammyv1.TaxTreatment_TAX_TREATMENT_OUT_OF_SCOPE, true
	default:
		return tammyv1.TaxTreatment_TAX_TREATMENT_UNSPECIFIED, false
	}
}
