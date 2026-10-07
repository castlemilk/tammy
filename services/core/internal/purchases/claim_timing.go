package purchases

import (
	"bytes"
	"errors"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

var ErrInvalidClaimTiming = errors.New("purchases: invalid linked claim timing facts")

const OrdinaryClaimLimitRuleVersion = "au-gst-ordinary-claim-limit-v1"

type ClaimTimingStatus string

const (
	ClaimTimingNormalSupported      ClaimTimingStatus = "NORMAL_SUPPORTED"
	ClaimTimingReviewRequired       ClaimTimingStatus = "TIMING_REVIEW_REQUIRED"
	ClaimTimingMissingEvidence      ClaimTimingStatus = "MISSING_EVIDENCE"
	ClaimTimingFutureEvidence       ClaimTimingStatus = "FUTURE_EVIDENCE"
	ClaimTimingUndeterminableLimit  ClaimTimingStatus = "UNDETERMINABLE_LIMIT"
	ClaimTimingExpired              ClaimTimingStatus = "EXPIRED"
	ClaimTimingUnsupportedException ClaimTimingStatus = "UNSUPPORTED_EXCEPTION"
)

// ClaimTimingUse is selected by the command owner from retained accounting
// history. A previously suspended or deferred source cannot use Automatic to
// bypass the actual required-return evidence needed to resolve its claim.
type ClaimTimingUse uint8

const (
	ClaimTimingAutomatic ClaimTimingUse = iota + 1
	ClaimTimingDelayed
	ClaimTimingDeferral
)

// ReviewedRequiredReturn is a core-verified capability, not request authority.
// Before supplying it the transaction owner verifies the uniquely owned event
// portion, earliest otherwise-eligible period (ignoring invoice holding), actual
// required date and retained evidence ownership/version/hash. The pure helper
// checks that binding; a well-formed request ref alone cannot establish it.
type ReviewedRequiredReturn struct {
	EventSource                     *tammyv1.PurchaseRetainedSourceRef
	NormalEventDate                 *tammyv1.CivilDate
	EarliestOtherwiseEligiblePeriod *tammyv1.PurchaseClaimPeriod
	DueDate                         *tammyv1.CivilDate
	Evidence                        *tammyv1.PurchaseRetainedSourceRef
}

type ClaimTimingInput struct {
	Use           ClaimTimingUse
	Basis         tammyv1.GstBasis
	InvoiceSource *tammyv1.PurchaseRetainedSourceRef
	InvoiceDate   *tammyv1.CivilDate
	PaymentSource *tammyv1.PurchaseRetainedSourceRef
	PaymentDate   *tammyv1.CivilDate
	Evidence      *tammyv1.PurchaseEvidenceInput
	ExceptionRule *ReviewedExceptionRule
	ClaimPeriod   *tammyv1.PurchaseClaimPeriod
	// Deferral retains a decision without a release journal; its date may be nil.
	ReleaseDate    *tammyv1.CivilDate
	ReviewedAsOf   *tammyv1.CivilDate
	RequiredReturn *ReviewedRequiredReturn
	// Current approved-form/external-notification rules for section 29-10(5)
	// are unverified. A requested election remains unsupported even if every
	// local source, date and review flag is otherwise valid.
	LaterPeriodElection bool
}

// ClaimTimingResult preserves linked civil facts without selecting a new tax
// date. NORMAL_SUPPORTED is a pure evidence/date/ordinary-limit result, not
// authority to post, elect a later period, or establish external acceptance.
// The command owner still verifies applicable return/assessment history,
// allowable-period decisions, source balances and closed-period controls.
type ClaimTimingResult struct {
	Status        ClaimTimingStatus
	RuleVersion   string
	InvoiceSource *tammyv1.PurchaseRetainedSourceRef
	InvoiceDate   *tammyv1.CivilDate
	PaymentSource *tammyv1.PurchaseRetainedSourceRef
	PaymentDate   *tammyv1.CivilDate
	EventSource   *tammyv1.PurchaseRetainedSourceRef
	EventDate     *tammyv1.CivilDate
	// Keep the complete retained provenance/review, including unsupported
	// exception and supporting-record refs, distinct from the date summary.
	Evidence              *tammyv1.PurchaseEvidenceInput
	HeldDate              *tammyv1.CivilDate
	ClaimPeriod           *tammyv1.PurchaseClaimPeriod
	ReleaseDate           *tammyv1.CivilDate
	ReviewedAsOf          *tammyv1.CivilDate
	RequiredReturn        *ReviewedRequiredReturn
	OrdinaryLastClaimDate *tammyv1.CivilDate
}

// EvaluateClaimTiming evaluates one owned source portion. Cash uses its actual
// payment event; this product rejects advances before invoice. Non-cash uses
// the retained invoice event. Held, release and claim-period dates stay separate.
func EvaluateClaimTiming(input ClaimTimingInput) (ClaimTimingResult, error) {
	if input.Use < ClaimTimingAutomatic || input.Use > ClaimTimingDeferral ||
		(input.Basis != tammyv1.GstBasis_GST_BASIS_CASH && input.Basis != tammyv1.GstBasis_GST_BASIS_NON_CASH) ||
		!validPurchaseSourceRef(input.InvoiceSource) || !validPurchaseDate(input.InvoiceDate) ||
		!validPurchaseDate(input.ReviewedAsOf) || !validClaimTimingPeriod(input.ClaimPeriod) ||
		(input.ReleaseDate == nil && input.Use != ClaimTimingDeferral) ||
		(input.ReleaseDate != nil && !validPurchaseDate(input.ReleaseDate)) {
		return ClaimTimingResult{}, ErrInvalidClaimTiming
	}
	if input.PaymentDate != nil || input.PaymentSource != nil {
		if !validPurchaseDate(input.PaymentDate) || !validPurchaseSourceRef(input.PaymentSource) ||
			purchaseDateKey(input.PaymentDate) < purchaseDateKey(input.InvoiceDate) {
			return ClaimTimingResult{}, ErrInvalidClaimTiming
		}
	}
	eventSource, eventDate := input.InvoiceSource, input.InvoiceDate
	if input.Basis == tammyv1.GstBasis_GST_BASIS_CASH {
		if input.PaymentDate == nil {
			return ClaimTimingResult{}, ErrInvalidClaimTiming
		}
		eventSource, eventDate = input.PaymentSource, input.PaymentDate
	}
	if purchaseDateKey(eventDate) > purchaseDateKey(input.ReviewedAsOf) ||
		purchaseDateKey(input.ClaimPeriod.PeriodEnd) < purchaseDateKey(eventDate) ||
		(input.ReleaseDate != nil && (purchaseDateKey(input.ReleaseDate) < purchaseDateKey(eventDate) ||
			!claimTimingPeriodContains(input.ClaimPeriod, input.ReleaseDate))) ||
		(input.Use == ClaimTimingAutomatic && !claimTimingPeriodContains(input.ClaimPeriod, eventDate)) {
		return ClaimTimingResult{}, ErrInvalidClaimTiming
	}
	readiness, err := EvidenceReadinessFor(input.Evidence, eventDate, input.ReviewedAsOf, input.ExceptionRule)
	if err != nil {
		return ClaimTimingResult{}, err
	}
	result := ClaimTimingResult{
		RuleVersion:   OrdinaryClaimLimitRuleVersion,
		InvoiceSource: cloneClaimRef(input.InvoiceSource), InvoiceDate: cloneClaimDate(input.InvoiceDate),
		PaymentSource: cloneClaimRef(input.PaymentSource), PaymentDate: cloneClaimDate(input.PaymentDate),
		EventSource: cloneClaimRef(eventSource), EventDate: cloneClaimDate(eventDate), HeldDate: cloneClaimDate(input.Evidence.HeldDate),
		Evidence:    proto.Clone(input.Evidence).(*tammyv1.PurchaseEvidenceInput),
		ClaimPeriod: proto.Clone(input.ClaimPeriod).(*tammyv1.PurchaseClaimPeriod), ReleaseDate: cloneClaimDate(input.ReleaseDate),
		ReviewedAsOf: cloneClaimDate(input.ReviewedAsOf),
	}
	result.Status = map[EvidenceReadiness]ClaimTimingStatus{
		EvidenceNormalSupported: ClaimTimingNormalSupported, EvidenceMissing: ClaimTimingMissingEvidence,
		EvidenceTimingReviewRequired: ClaimTimingReviewRequired, EvidenceFuture: ClaimTimingFutureEvidence,
		EvidenceUnsupportedException: ClaimTimingUnsupportedException,
	}[readiness]
	if input.RequiredReturn != nil {
		required := input.RequiredReturn
		if !validPurchaseSourceRef(required.EventSource) || !sameClaimRef(required.EventSource, eventSource) ||
			!validPurchaseDate(required.NormalEventDate) || purchaseDateKey(required.NormalEventDate) != purchaseDateKey(eventDate) ||
			!validClaimTimingPeriod(required.EarliestOtherwiseEligiblePeriod) || !claimTimingPeriodContains(required.EarliestOtherwiseEligiblePeriod, eventDate) ||
			!validPurchaseDate(required.DueDate) || purchaseDateKey(required.DueDate) < purchaseDateKey(required.EarliestOtherwiseEligiblePeriod.PeriodEnd) {
			return ClaimTimingResult{}, ErrInvalidClaimTiming
		}
		result.RequiredReturn = &ReviewedRequiredReturn{EventSource: cloneClaimRef(required.EventSource), NormalEventDate: cloneClaimDate(required.NormalEventDate),
			EarliestOtherwiseEligiblePeriod: proto.Clone(required.EarliestOtherwiseEligiblePeriod).(*tammyv1.PurchaseClaimPeriod),
			DueDate:                         cloneClaimDate(required.DueDate), Evidence: cloneClaimRef(required.Evidence)}
		if validPurchaseSourceRef(required.Evidence) {
			result.OrdinaryLastClaimDate = ordinaryClaimAnniversary(required.DueDate)
		}
	}
	if input.LaterPeriodElection || readiness == EvidenceUnsupportedException {
		result.Status = ClaimTimingUnsupportedException
		return result, nil
	}
	if input.Use != ClaimTimingAutomatic && result.OrdinaryLastClaimDate == nil {
		// Preserve evidence blockers; they cannot become claim-ready merely
		// because a required-return date is missing or outside supported bounds.
		if readiness == EvidenceNormalSupported || readiness == EvidenceTimingReviewRequired {
			result.Status = ClaimTimingUndeterminableLimit
		}
		return result, nil
	}
	if result.OrdinaryLastClaimDate != nil &&
		(purchaseDateKey(input.ReviewedAsOf) > purchaseDateKey(result.OrdinaryLastClaimDate) ||
			(input.ReleaseDate != nil && purchaseDateKey(input.ReleaseDate) > purchaseDateKey(result.OrdinaryLastClaimDate))) {
		result.Status = ClaimTimingExpired
	}
	return result, nil
}

func validClaimTimingPeriod(period *tammyv1.PurchaseClaimPeriod) bool {
	return period != nil && validPurchaseDate(period.PeriodStart) && validPurchaseDate(period.PeriodEnd) &&
		purchaseDateKey(period.PeriodStart) <= purchaseDateKey(period.PeriodEnd)
}

func claimTimingPeriodContains(period *tammyv1.PurchaseClaimPeriod, date *tammyv1.CivilDate) bool {
	return purchaseDateKey(date) >= purchaseDateKey(period.PeriodStart) && purchaseDateKey(date) <= purchaseDateKey(period.PeriodEnd)
}

func ordinaryClaimAnniversary(due *tammyv1.CivilDate) *tammyv1.CivilDate {
	if due.Year > 9995 {
		return nil
	}
	anniversary := time.Date(int(due.Year+4), time.Month(due.Month), int(due.Day), 0, 0, 0, 0, time.UTC)
	if anniversary.Year() != int(due.Year+4) || anniversary.Month() != time.Month(due.Month) || anniversary.Day() != int(due.Day) {
		return nil
	}
	return &tammyv1.CivilDate{Year: int32(anniversary.Year()), Month: int32(anniversary.Month()), Day: int32(anniversary.Day())}
}

func sameClaimRef(a, b *tammyv1.PurchaseRetainedSourceRef) bool {
	return a.SourceId == b.SourceId && a.SourceVersion == b.SourceVersion && bytes.Equal(a.SourceHash, b.SourceHash)
}

func cloneClaimDate(date *tammyv1.CivilDate) *tammyv1.CivilDate {
	if date == nil {
		return nil
	}
	return proto.Clone(date).(*tammyv1.CivilDate)
}

func cloneClaimRef(ref *tammyv1.PurchaseRetainedSourceRef) *tammyv1.PurchaseRetainedSourceRef {
	if ref == nil {
		return nil
	}
	return proto.Clone(ref).(*tammyv1.PurchaseRetainedSourceRef)
}
