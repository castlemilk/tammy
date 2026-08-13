package accounting

import (
	"crypto/sha256"
	"errors"
	"math"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
)

var ErrInvalidTaxFact = errors.New("accounting: invalid tax fact")

type TaxFact struct {
	ID, OrganisationID, JournalLineID string
	TaxCode                           string
	Treatment                         tammyv1.TaxTreatment
	OriginalGrossMinor                int64
	OriginalNetMinor                  int64
	OriginalGSTMinor                  int64
	AttributedGrossMinor              int64
	AttributedNetMinor                int64
	AttributedGSTMinor                int64
	RemainingGrossMinor               int64
	RemainingNetMinor                 int64
	RemainingGSTMinor                 int64
	Rule                              *tammyv1.SourceRef
	Source                            *tammyv1.SourceRef
}

func BuildNonCashTaxFact(id, organisationID, lineID string, code *tammyv1.TaxCode,
	netMinor, gstMinor int64, source *tammyv1.SourceRef, fallbackSourceID string,
) (TaxFact, error) {
	if code == nil || code.Rule == nil || !ids.IsCanonicalV7(code.Id) {
		return TaxFact{}, ErrInvalidTaxFact
	}
	gross, ok := checkedSignedAdd(netMinor, gstMinor)
	if !ok {
		return TaxFact{}, ErrInvalidTaxFact
	}
	if source == nil {
		digest := sha256.Sum256([]byte(fallbackSourceID))
		source = &tammyv1.SourceRef{Type: "manual_journal_line", Id: fallbackSourceID, Revision: 1, ContentHash: digest[:]}
	}
	fact := TaxFact{ID: id, OrganisationID: organisationID, JournalLineID: lineID, TaxCode: code.Code,
		Treatment: code.Treatment, OriginalGrossMinor: gross, OriginalNetMinor: netMinor, OriginalGSTMinor: gstMinor,
		AttributedGrossMinor: gross, AttributedNetMinor: netMinor, AttributedGSTMinor: gstMinor,
		Rule: proto.Clone(code.Rule).(*tammyv1.SourceRef), Source: proto.Clone(source).(*tammyv1.SourceRef)}
	if ValidateTaxFact(fact) != nil {
		return TaxFact{}, ErrInvalidTaxFact
	}
	return fact, nil
}

func ValidateTaxFact(fact TaxFact) error {
	if fact.ID == "" || !ids.IsCanonicalV7(fact.OrganisationID) || !ids.IsCanonicalV7(fact.JournalLineID) ||
		fact.TaxCode == "" || fact.Rule == nil || fact.Source == nil || !validFactSource(fact.Rule) || !validFactSource(fact.Source) {
		return ErrInvalidTaxFact
	}
	if fact.OriginalGrossMinor != fact.OriginalNetMinor+fact.OriginalGSTMinor ||
		fact.AttributedGrossMinor != fact.AttributedNetMinor+fact.AttributedGSTMinor ||
		fact.RemainingGrossMinor != fact.RemainingNetMinor+fact.RemainingGSTMinor ||
		fact.OriginalGrossMinor != fact.AttributedGrossMinor+fact.RemainingGrossMinor ||
		fact.OriginalNetMinor != fact.AttributedNetMinor+fact.RemainingNetMinor ||
		fact.OriginalGSTMinor != fact.AttributedGSTMinor+fact.RemainingGSTMinor {
		return ErrInvalidTaxFact
	}
	switch fact.Treatment {
	case tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE:
	case tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE,
		tammyv1.TaxTreatment_TAX_TREATMENT_INPUT_TAXED,
		tammyv1.TaxTreatment_TAX_TREATMENT_OUT_OF_SCOPE:
		if fact.OriginalGSTMinor != 0 {
			return ErrInvalidTaxFact
		}
	default:
		return ErrInvalidTaxFact
	}
	return nil
}

func validFactSource(source *tammyv1.SourceRef) bool {
	return source != nil && source.Type != "" && ids.IsCanonicalV7(source.Id) && source.Revision > 0 && len(source.ContentHash) == 32
}

func checkedSignedAdd(left, right int64) (int64, bool) {
	if right > 0 && left > math.MaxInt64-right || right < 0 && left < math.MinInt64-right {
		return 0, false
	}
	return left + right, true
}

func taxTreatmentName(value tammyv1.TaxTreatment) string {
	return map[tammyv1.TaxTreatment]string{
		tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE:      "TAXABLE",
		tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE:     "GST_FREE",
		tammyv1.TaxTreatment_TAX_TREATMENT_INPUT_TAXED:  "INPUT_TAXED",
		tammyv1.TaxTreatment_TAX_TREATMENT_OUT_OF_SCOPE: "OUT_OF_SCOPE",
	}[value]
}
