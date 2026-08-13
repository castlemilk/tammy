// Package taxrules provides pure, versioned tax calculations over immutable
// Artefacts-owned rule projections.
package taxrules

import (
	"errors"
	"math/big"
	"regexp"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
)

var ErrInvalidRule = errors.New("taxrules: invalid rule")

const million = int64(1_000_000)

var taxCodePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,31}$`)

type Rule struct {
	Code           string
	Treatment      tammyv1.TaxTreatment
	RateMillionths int64
	Source         *tammyv1.SourceRef
}

type Fact struct {
	Taxable   *tammyv1.Money
	Tax       *tammyv1.Money
	Treatment tammyv1.TaxTreatment
	Rule      *tammyv1.SourceRef
}

type Kernel struct{ version string }

func NewKernel(version string) (*Kernel, error) {
	if version != "au_gst_v1" {
		return nil, ErrInvalidRule
	}
	return &Kernel{version: version}, nil
}

// CalculateNonCash attributes GST from the source document's exact taxable
// minor units. Products use arbitrary precision before half-away rounding.
func (kernel *Kernel) CalculateNonCash(rule Rule, taxable *tammyv1.Money) (Fact, error) {
	if kernel == nil || kernel.version != "au_gst_v1" || taxable == nil || taxable.CurrencyCode != "AUD" ||
		!validRule(rule) {
		return Fact{}, ErrInvalidRule
	}
	taxMinor := int64(0)
	if rule.Treatment == tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE {
		product := new(big.Int).Mul(big.NewInt(taxable.MinorUnits), big.NewInt(rule.RateMillionths))
		quotient, remainder := new(big.Int), new(big.Int)
		quotient.QuoRem(product, big.NewInt(million), remainder)
		absoluteRemainder := new(big.Int).Abs(new(big.Int).Set(remainder))
		if absoluteRemainder.Mul(absoluteRemainder, big.NewInt(2)).Cmp(big.NewInt(million)) >= 0 {
			if product.Sign() < 0 {
				quotient.Sub(quotient, big.NewInt(1))
			} else {
				quotient.Add(quotient, big.NewInt(1))
			}
		}
		if !quotient.IsInt64() {
			return Fact{}, ErrInvalidRule
		}
		taxMinor = quotient.Int64()
	}
	return Fact{
		Taxable:   &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: taxable.MinorUnits},
		Tax:       &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: taxMinor},
		Treatment: rule.Treatment,
		Rule:      proto.Clone(rule.Source).(*tammyv1.SourceRef),
	}, nil
}

func validRule(rule Rule) bool {
	if !taxCodePattern.MatchString(rule.Code) || rule.RateMillionths < 0 || rule.RateMillionths > million ||
		rule.Source == nil || rule.Source.Type != "tax_rule_bundle" || !ids.IsCanonicalV7(rule.Source.Id) ||
		rule.Source.Revision == 0 || len(rule.Source.ContentHash) != 32 {
		return false
	}
	switch rule.Treatment {
	case tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE:
		return rule.RateMillionths > 0
	case tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE,
		tammyv1.TaxTreatment_TAX_TREATMENT_INPUT_TAXED,
		tammyv1.TaxTreatment_TAX_TREATMENT_OUT_OF_SCOPE:
		return rule.RateMillionths == 0
	default:
		return false
	}
}
