package baspreparation

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

var (
	ErrReviewInput      = errors.New("baspreparation: malformed or mismatched review input")
	ErrFreezeNotReady   = errors.New("baspreparation: preparation is not eligible to freeze")
	ErrChainSnapshot    = errors.New("baspreparation: malformed or inconsistent family chain snapshot")
	ErrObservationInput = errors.New("baspreparation: malformed or mismatched external observation")
	ErrExternalHistory  = errors.New("baspreparation: incomplete or inconsistent retained external history")
)

var uuidV7Pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var findingCodePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
var sourceTypePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Complete histories are loaded with limit+1 by storage. Overflow is rejected
// before reduction so a partial event or predecessor census cannot authorise a transition.
const maxExternalHistoryRecords = 1000
const maxExternalHistoryAttempts = 1000
const maxExternalDisputeSubjects = 200

// FamilyScope is the stable BAS obligation identity. Period/profile/rule source
// revisions are provenance and intentionally do not participate in its identity.
type FamilyScope struct {
	OrganisationID string
	Start          *tammyv1.CivilDate
	End            *tammyv1.CivilDate
	Window         tammyv1.BasPeriodWindow
}

// FamilyScopeFromPeriod derives a stable family key from actual confirmed dates,
// organisation, and obligation window. The exact period SourceRef stays separate.
func FamilyScopeFromPeriod(period *tammyv1.BasConfirmedPeriod) (FamilyScope, error) {
	if period == nil || !validUUIDv7(period.OrganisationId) || !validUUIDv7(period.PeriodId) || period.Version == 0 || len(period.ContentHash) != sha256.Size || period.Confirmation == nil || period.Confirmation.Period == nil || !validCalendarDate(period.Confirmation.Period.StartDate) || !validCalendarDate(period.Confirmation.Period.EndDate) || period.Confirmation.Window == tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_UNSPECIFIED || period.Confirmation.Window > tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_CUSTOM {
		return FamilyScope{}, ErrChainSnapshot
	}
	start := cloneCalendarDate(period.Confirmation.Period.StartDate)
	end := cloneCalendarDate(period.Confirmation.Period.EndDate)
	if calendarKey(start) > calendarKey(end) {
		return FamilyScope{}, ErrChainSnapshot
	}
	return FamilyScope{OrganisationID: period.OrganisationId, Start: start, End: end, Window: period.Confirmation.Window}, nil
}

func SameFamilyScope(left, right FamilyScope) bool {
	return left.OrganisationID != "" && left.OrganisationID == right.OrganisationID && left.Window == right.Window && calendarKey(left.Start) == calendarKey(right.Start) && calendarKey(left.End) == calendarKey(right.End) && validCalendarDate(left.Start) && validCalendarDate(left.End) && validCalendarDate(right.Start) && validCalendarDate(right.End)
}

// Family holds the exact retained family identity and its independent mutable
// compare-and-swap version.
type Family struct {
	Reference *tammyv1.SourceRef
	Version   uint64
	Scope     FamilyScope
}

// Branch is the current correction/replacement lineage. RootHead is the exact
// retained confirmed preparation version at which this branch began; it is not
// the immutable family RootPreparation. ActiveAttempt is a loaded aggregate
// version and has its own CAS identity. A populated frozen barrier records an
// attempt that predates a corrective resolution and therefore cannot qualify as
// that corrective revision.
type Branch struct {
	Reference     *tammyv1.SourceRef
	Version       uint64
	RootHead      *tammyv1.BasPreparation
	ActiveAttempt *tammyv1.BasPreparation
	Resolution    *tammyv1.SourceRef
	FrozenBarrier *tammyv1.SourceRef
}

// ChainSnapshot contains retained family pointers loaded inside a repository
// transaction. LatestHead and Branch.RootHead are exact confirmed preparation
// snapshots chosen by the family pointers, not freshly re-read mutable workflow
// aggregates. Overlay/export metadata changes therefore cannot reinterpret the
// effective predecessor; current mutable attempts are pinned separately.
type ChainSnapshot struct {
	Family            *Family
	RootPreparation   *tammyv1.BasPreparation
	EffectiveOriginal *tammyv1.BasPreparation
	LatestHead        *tammyv1.BasPreparation
	Branch            *Branch
	DisputeOverlay    tammyv1.BasDisputeOverlayState
	PeriodReference   *tammyv1.SourceRef
}

type CurrentCapture struct {
	SourceSnapshotHash []byte
	InputRevision      uint64
	InputHash          []byte
	RevisionVector     *tammyv1.BasSourceRevisionVector
}

type ReviewInput struct {
	Preparation      *tammyv1.BasPreparation
	Calculation      *tammyv1.BasCalculation
	Validation       *tammyv1.BasValidationSnapshot
	Acknowledgements []*tammyv1.BasWarningAcknowledgement
	CurrentCapture   CurrentCapture
}

type ReviewAssessment struct {
	State                  tammyv1.BasPreparationState
	CanFreeze              bool
	SourceChanged          bool
	UnacknowledgedWarnings []*tammyv1.BasValidationFinding
}

type FreezeInput struct {
	Review ReviewInput
	Chain  ChainSnapshot
}

type FreezePlan struct {
	ExpectedPreparationVersion uint64
	ExpectedPreparationHash    []byte
	ExpectedChain              *tammyv1.BasChainExpectation
	Calculation                *tammyv1.BasCalculation
	Validation                 *tammyv1.BasValidationSnapshot
	Acknowledgements           []*tammyv1.BasWarningAcknowledgement
}

type AttemptSnapshot struct {
	Preparation     *tammyv1.BasPreparation
	FrozenReference *tammyv1.SourceRef
	Frozen          *tammyv1.BasFrozenBas
}

// ExternalHistoryRecord supplies the Core-assigned append order beside the
// complete typed retained protobuf. Claimed timestamps and UUIDs are never used
// to infer ordering. Repositories still verify full body hashes and retention.
type ExternalHistoryRecord struct {
	Ordinal     uint64
	Observation *tammyv1.BasExternalObservation
	Resolution  *tammyv1.BasExternalDisputeResolution
}

type ExternalHistoryInput struct {
	Chain ChainSnapshot
	// Attempts are current loaded workflow aggregates, including nonterminal and
	// superseded attempts. ConfirmedDependencies are separate exact retained
	// confirmed versions ordered from oldest confirmed ancestor through LatestHead.
	// Their immutable references do not change when aggregate metadata is saved.
	Attempts              []AttemptSnapshot
	ConfirmedDependencies []AttemptSnapshot
	Records               []ExternalHistoryRecord
	LastOrdinal           uint64
}

type ObservationInput struct {
	Chain       ChainSnapshot
	Attempt     AttemptSnapshot
	Observation *tammyv1.BasExternalObservationInput
	History     ExternalHistoryInput
}

type ObservationPlan struct {
	Observation             *tammyv1.BasExternalObservationInput
	Frozen                  *tammyv1.SourceRef
	Trust                   tammyv1.BasExternalTrust
	Qualifies               bool
	AdvancesHead            bool
	OpensDispute            bool
	AffectedConfirmedChain  []*tammyv1.SourceRef
	CompletedResolution     *tammyv1.SourceRef
	NextOverlay             tammyv1.BasDisputeOverlayState
	ReserveCorrectiveBranch bool
	ExpectedChain           *tammyv1.BasChainExpectation
}

type ReplacementInput struct {
	Chain         ChainSnapshot
	ExpectedChain *tammyv1.BasChainExpectation
	Attempt       AttemptSnapshot
	History       ExternalHistoryInput
	Reason        string
}

type ReplacementPlan struct {
	ExpectedChain          *tammyv1.BasChainExpectation
	PredecessorPreparation *tammyv1.SourceRef
	PredecessorFrozen      *tammyv1.SourceRef
	SuccessorState         tammyv1.BasPreparationState
	RetainedHistory        []ExternalHistoryRecord
	ClearFrozenBarrier     bool
}

type RejectedRecoveryInput struct {
	Chain             ChainSnapshot
	ExpectedChain     *tammyv1.BasChainExpectation
	Attempt           AttemptSnapshot
	History           ExternalHistoryInput
	ResolvedRejection *tammyv1.SourceRef
	Reason            string
}

type RejectedRecoveryPlan struct {
	ExpectedChain          *tammyv1.BasChainExpectation
	PredecessorPreparation *tammyv1.SourceRef
	PredecessorFrozen      *tammyv1.SourceRef
	ResolvedRejection      *tammyv1.SourceRef
	SuccessorState         tammyv1.BasPreparationState
	ClearFrozenBarrier     bool
}

type RevisionInput struct {
	Chain           ChainSnapshot
	ExpectedChain   *tammyv1.BasChainExpectation
	History         ExternalHistoryInput
	PeriodSnapshot  *tammyv1.BasConfirmedPeriod
	ProfileSnapshot *tammyv1.BasObligationsProfile
	Reason          string
}

type RevisionPlan struct {
	ExpectedChain     *tammyv1.BasChainExpectation
	EffectiveOriginal *tammyv1.SourceRef
	LatestHead        *tammyv1.SourceRef
	SuccessorState    tammyv1.BasPreparationState
}

type CorrectionInput struct {
	Chain         ChainSnapshot
	ExpectedChain *tammyv1.BasChainExpectation
	Attempt       AttemptSnapshot
	History       ExternalHistoryInput
	Correction    *tammyv1.BasExternalObservationCorrectionInput
}

type CorrectionPlan struct {
	ExpectedChain          *tammyv1.BasChainExpectation
	Correction             *tammyv1.BasExternalObservationCorrectionInput
	Frozen                 *tammyv1.SourceRef
	Qualifies              bool
	AdvancesHead           bool
	OpensDispute           bool
	NextOverlay            tammyv1.BasDisputeOverlayState
	AffectedConfirmedChain []*tammyv1.SourceRef
}

type DisputeResolutionInput struct {
	Chain                 ChainSnapshot
	ExpectedChain         *tammyv1.BasChainExpectation
	History               ExternalHistoryInput
	ConfirmedDependencies []AttemptSnapshot
	Resolution            *tammyv1.BasExternalDisputeInput
}

type DisputeResolutionPlan struct {
	ExpectedChain                   *tammyv1.BasChainExpectation
	DisputedObservations            []*tammyv1.SourceRef
	AffectedConfirmedChain          []*tammyv1.SourceRef
	NextOverlay                     tammyv1.BasDisputeOverlayState
	ReserveCorrectiveBranch         bool
	AdoptedExistingBranch           bool
	ResetMutableAttemptToCollecting bool
	ClearCurrentReviewPointers      bool
	FrozenBarrier                   *tammyv1.SourceRef
	PreserveFrozenAttempt           bool
}

// AssessReview is a pure projection of retained calculation/validation facts
// and the fresh capture. The policy checks the acknowledgement subset and
// canonical finding identity; loading repositories remain responsible for
// verifying retained message hashes and complete snapshot bodies.
func AssessReview(input ReviewInput) (ReviewAssessment, error) {
	prep := input.Preparation
	if !validPreparationIdentity(prep) || !validCapture(input.CurrentCapture) {
		return ReviewAssessment{}, ErrReviewInput
	}
	if !validCurrentCalculationReference(prep, input.Calculation) {
		return ReviewAssessment{}, ErrReviewInput
	}
	if input.Calculation == nil {
		if input.Validation != nil || prep.CurrentValidation != nil || len(input.Acknowledgements) != 0 {
			return ReviewAssessment{}, ErrReviewInput
		}
		return ReviewAssessment{State: tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING}, nil
	}
	if input.Validation == nil {
		if prep.CurrentValidation != nil || len(input.Acknowledgements) != 0 {
			return ReviewAssessment{}, ErrReviewInput
		}
		return ReviewAssessment{State: tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING}, nil
	}
	if err := validateReviewBinding(input); err != nil {
		return ReviewAssessment{}, err
	}

	sourceChanged := !bytes.Equal(input.CurrentCapture.SourceSnapshotHash, prep.SourceSnapshotHash) ||
		input.CurrentCapture.InputRevision != prep.InputRevision || !bytes.Equal(input.CurrentCapture.InputHash, prep.InputHash) ||
		!bytes.Equal(input.CurrentCapture.SourceSnapshotHash, input.Calculation.SourceSnapshotHash) ||
		!bytes.Equal(input.CurrentCapture.SourceSnapshotHash, input.Validation.SourceSnapshotHash) ||
		input.CurrentCapture.InputRevision != input.Calculation.InputRevision || !bytes.Equal(input.CurrentCapture.InputHash, input.Calculation.InputHash) ||
		input.CurrentCapture.InputRevision != input.Validation.InputRevision || !bytes.Equal(input.CurrentCapture.InputHash, input.Validation.InputHash)
	if sourceChanged {
		if isTerminalPreparationState(prep.State) {
			return ReviewAssessment{State: prep.State, SourceChanged: true}, nil
		}
		return ReviewAssessment{State: tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING, SourceChanged: true}, nil
	}

	if isTerminalPreparationState(prep.State) {
		return ReviewAssessment{State: prep.State}, nil
	}
	if input.Calculation.Result.Outcome != tammyv1.BasCalculationOutcome_BAS_CALCULATION_OUTCOME_COMPLETE {
		return ReviewAssessment{State: tammyv1.BasPreparationState_BAS_PREPARATION_STATE_BLOCKED}, nil
	}

	acknowledged, err := validateAcknowledgements(input)
	if err != nil {
		return ReviewAssessment{}, err
	}
	assessment := ReviewAssessment{State: tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY}
	for _, finding := range input.Validation.Findings {
		switch finding.Severity {
		case tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_BLOCKER,
			tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_UNSUPPORTED:
			assessment.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_BLOCKED
		case tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_WARNING:
			key := findingKey(finding, input.Validation.Revision)
			if !acknowledged[key] {
				assessment.UnacknowledgedWarnings = append(assessment.UnacknowledgedWarnings, cloneFinding(finding))
			}
		case tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_INFORMATION:
		default:
			return ReviewAssessment{}, ErrReviewInput
		}
	}
	if assessment.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY {
		return assessment, nil
	}
	assessment.CanFreeze = len(assessment.UnacknowledgedWarnings) == 0
	return assessment, nil
}

// PlanFreeze returns an immutable intent plus the exact aggregate and family
// chain versions the later transaction must compare-and-swap. It allocates no
// identifiers and does not make or simulate an approval decision.
func PlanFreeze(input FreezeInput) (*FreezePlan, error) {
	assessment, err := AssessReview(input.Review)
	if err != nil {
		return nil, err
	}
	if assessment.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY || !assessment.CanFreeze {
		return nil, ErrFreezeNotReady
	}
	chainExpectation, err := BuildChainExpectation(input.Chain)
	if err != nil {
		return nil, err
	}
	prep := input.Review.Preparation
	if prep.State == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF || isTerminalPreparationState(prep.State) {
		return nil, ErrFreezeNotReady
	}
	periodScope, scopeErr := FamilyScopeFromPeriod(input.Review.Calculation.PeriodSnapshot)
	if scopeErr != nil || !sameRef(prep.Family, input.Chain.Family.Reference) || prep.OrganisationId != input.Chain.Family.Scope.OrganisationID || !SameFamilyScope(periodScope, input.Chain.Family.Scope) ||
		!sameRef(input.Review.Calculation.Period, prep.Period) || !sameRef(input.Review.Calculation.Profile, prep.Profile) ||
		!sameRef(input.Review.Calculation.PeriodSnapshot.Confirmation.Profile, prep.Profile) || !sameRef(input.Chain.PeriodReference, prep.Period) {
		return nil, ErrChainSnapshot
	}
	if input.Chain.Branch == nil {
		if input.Chain.LatestHead != nil || !samePreparationIdentity(input.Chain.RootPreparation, prep) {
			return nil, ErrChainSnapshot
		}
	} else if input.Chain.Branch.ActiveAttempt == nil || !samePreparationAggregate(input.Chain.Branch.ActiveAttempt, prep) {
		return nil, ErrChainSnapshot
	}
	if prep.Version == 0 || len(prep.ContentHash) != sha256.Size {
		return nil, ErrReviewInput
	}
	return &FreezePlan{
		ExpectedPreparationVersion: prep.Version,
		ExpectedPreparationHash:    bytes.Clone(prep.ContentHash),
		ExpectedChain:              chainExpectation,
		Calculation:                proto.Clone(input.Review.Calculation).(*tammyv1.BasCalculation),
		Validation:                 proto.Clone(input.Review.Validation).(*tammyv1.BasValidationSnapshot),
		Acknowledgements:           cloneAcknowledgements(input.Review.Acknowledgements),
	}, nil
}

// BuildChainExpectation creates exact explicit-present/absent CAS pins for the
// generated lifecycle contract from repository-loaded immutable pointers.
func BuildChainExpectation(chain ChainSnapshot) (*tammyv1.BasChainExpectation, error) {
	if err := validateChainSnapshot(chain); err != nil {
		return nil, err
	}
	expectation := &tammyv1.BasChainExpectation{
		Family: cloneRef(chain.Family.Reference), ExpectedFamilyVersion: chain.Family.Version,
		RootPreparation: preparationReference(chain.RootPreparation),
		LatestHead:      expectedPreparation(chain.LatestHead), Branch: expectedBranch(chain.Branch),
		ActiveAttempt: expectedPreparation(activeAttempt(chain.Branch)),
	}
	return expectation, nil
}

// PlanObservation accepts a complete ordinary event assertion. It derives the
// current history from typed retained records and their Core-assigned append
// ordinals, then pins the exact family chain. User-recorded trust is fixed here;
// caller evidence does not upgrade it.
func PlanObservation(input ObservationInput) (*ObservationPlan, error) {
	if input.Observation == nil || !validObservationPayload(input.Observation) || input.Attempt.Preparation == nil || input.Attempt.Frozen == nil || !validRef(input.Attempt.FrozenReference, "bas_frozen") || !sameRef(input.Attempt.Preparation.FrozenBas, input.Attempt.FrozenReference) {
		return nil, ErrObservationInput
	}
	currentChain, err := BuildChainExpectation(input.Chain)
	if err != nil {
		return nil, ErrChainSnapshot
	}
	if !sameLoadedChain(input.Chain, input.History.Chain) {
		return nil, ErrExternalHistory
	}
	history, err := deriveExternalHistory(input.History)
	if err != nil {
		return nil, err
	}
	if !attemptMatchesFrozen(input.Attempt) || input.Attempt.Preparation.OrganisationId != input.Chain.Family.Scope.OrganisationID || !sameRef(input.Attempt.Preparation.Family, input.Chain.Family.Reference) || !sameRef(input.Attempt.Frozen.Family, input.Chain.Family.Reference) {
		return nil, ErrObservationInput
	}
	periodScope, scopeErr := FamilyScopeFromPeriod(input.Attempt.Frozen.CalculationSnapshot.PeriodSnapshot)
	if scopeErr != nil || !SameFamilyScope(periodScope, input.Chain.Family.Scope) {
		return nil, ErrObservationInput
	}
	plan, err := planObservationFromHistory(input, currentChain, history)
	if err != nil {
		return nil, err
	}
	nextOrdinal, ok := nextExternalHistoryOrdinal(input.History.LastOrdinal)
	projectedHistory := history.withAppendedObservation(input.Attempt, input.Observation.EventKind, nextOrdinal)
	if !ok || !projectedMutationWithinHistoryBound(len(input.History.Records), plan.OpensDispute, projectedHistory) {
		return nil, ErrExternalHistory
	}
	return plan, nil
}

func PlanCreateReplacement(input ReplacementInput) (*ReplacementPlan, error) {
	if err := requireExactExpectedChain(input.Chain, input.ExpectedChain); err != nil {
		return nil, err
	}
	if !validReason(input.Reason) || !attemptMatchesFrozen(input.Attempt) || !sameRef(input.Attempt.Preparation.Family, input.Chain.Family.Reference) ||
		input.Attempt.Preparation.OrganisationId != input.Chain.Family.Scope.OrganisationID || !frozenScopeMatchesFamily(input.Attempt, input.Chain.Family.Scope) ||
		!replacementAttemptIsCurrent(input.Chain, input.Attempt) {
		return nil, ErrObservationInput
	}
	if !sameLoadedChain(input.Chain, input.History.Chain) {
		return nil, ErrExternalHistory
	}
	history, err := deriveExternalHistory(input.History)
	if err != nil {
		return nil, err
	}
	attemptCount, err := externalPreparationCount(input.History)
	if err != nil || len(input.History.Attempts) >= maxExternalHistoryAttempts || attemptCount >= maxExternalHistoryAttempts {
		return nil, ErrExternalHistory
	}
	if input.Chain.DisputeOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_NONE &&
		input.Chain.DisputeOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_RESOLVED_UPHELD &&
		input.Chain.DisputeOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_CORRECTION_REQUIRED {
		return nil, ErrExternalHistory
	}
	if input.Chain.DisputeOverlay == tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_CORRECTION_REQUIRED &&
		(input.Chain.Branch == nil || input.Chain.Branch.Resolution == nil || input.Chain.Branch.ActiveAttempt == nil || !sameRef(input.Chain.Branch.ActiveAttempt.FrozenBas, input.Attempt.FrozenReference)) {
		return nil, ErrExternalHistory
	}
	if history.blocksUnused(input.Attempt.FrozenReference) {
		return nil, ErrExternalHistory
	}
	return &ReplacementPlan{
		ExpectedChain:          proto.Clone(input.ExpectedChain).(*tammyv1.BasChainExpectation),
		PredecessorPreparation: preparationReference(input.Attempt.Preparation),
		PredecessorFrozen:      cloneRef(input.Attempt.FrozenReference),
		SuccessorState:         tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING,
		RetainedHistory:        cloneExternalHistoryRecords(input.History.Records),
		ClearFrozenBarrier:     input.Chain.Branch != nil && input.Chain.Branch.FrozenBarrier != nil,
	}, nil
}

func PlanRejectedRecovery(input RejectedRecoveryInput) (*RejectedRecoveryPlan, error) {
	if err := requireExactExpectedChain(input.Chain, input.ExpectedChain); err != nil {
		return nil, err
	}
	if !validReason(input.Reason) || !attemptMatchesFrozen(input.Attempt) || !sameRef(input.Attempt.Preparation.Family, input.Chain.Family.Reference) ||
		!replacementAttemptIsCurrent(input.Chain, input.Attempt) || !frozenScopeMatchesFamily(input.Attempt, input.Chain.Family.Scope) ||
		!sameLoadedChain(input.Chain, input.History.Chain) {
		return nil, ErrObservationInput
	}
	history, err := deriveExternalHistory(input.History)
	if err != nil {
		return nil, err
	}
	attemptCount, err := externalPreparationCount(input.History)
	if err != nil || len(input.History.Attempts) >= maxExternalHistoryAttempts || attemptCount >= maxExternalHistoryAttempts {
		return nil, ErrExternalHistory
	}
	if input.Chain.DisputeOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_NONE &&
		input.Chain.DisputeOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_RESOLVED_UPHELD &&
		input.Chain.DisputeOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_CORRECTION_REQUIRED {
		return nil, ErrExternalHistory
	}
	if input.Chain.DisputeOverlay == tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_CORRECTION_REQUIRED &&
		(input.Chain.Branch == nil || input.Chain.Branch.Resolution == nil || input.Chain.Branch.ActiveAttempt == nil || !sameRef(input.Chain.Branch.ActiveAttempt.FrozenBas, input.Attempt.FrozenReference)) {
		return nil, ErrExternalHistory
	}
	rejection, ok := history.unambiguousRejection(input.Attempt.FrozenReference)
	if !ok || input.ResolvedRejection == nil || !sameRef(input.ResolvedRejection, rejection.reference) || history.hasRecoveryConflict(input.Attempt.FrozenReference) {
		return nil, ErrExternalHistory
	}
	return &RejectedRecoveryPlan{
		ExpectedChain:          proto.Clone(input.ExpectedChain).(*tammyv1.BasChainExpectation),
		PredecessorPreparation: preparationReference(input.Attempt.Preparation),
		PredecessorFrozen:      cloneRef(input.Attempt.FrozenReference),
		ResolvedRejection:      cloneRef(rejection.reference),
		SuccessorState:         tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING,
		ClearFrozenBarrier:     input.Chain.Branch != nil && input.Chain.Branch.FrozenBarrier != nil,
	}, nil
}

func PlanCreateRevision(input RevisionInput) (*RevisionPlan, error) {
	if err := requireExactExpectedChain(input.Chain, input.ExpectedChain); err != nil {
		return nil, err
	}
	if input.Chain.LatestHead == nil || input.Chain.LatestHead.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED ||
		input.Chain.Branch != nil || !validReason(input.Reason) || input.PeriodSnapshot == nil || input.ProfileSnapshot == nil {
		return nil, ErrChainSnapshot
	}
	if !sameLoadedChain(input.Chain, input.History.Chain) {
		return nil, ErrExternalHistory
	}
	if _, err := deriveExternalHistory(input.History); err != nil {
		return nil, err
	}
	attemptCount, err := externalPreparationCount(input.History)
	if err != nil || len(input.History.Attempts) >= maxExternalHistoryAttempts || attemptCount >= maxExternalHistoryAttempts {
		return nil, ErrExternalHistory
	}
	profileRef := &tammyv1.SourceRef{Type: "bas_obligations_profile", Id: input.ProfileSnapshot.ProfileId,
		Revision: input.ProfileSnapshot.Revision, ContentHash: bytes.Clone(input.ProfileSnapshot.ContentHash)}
	periodScope, err := FamilyScopeFromPeriod(input.PeriodSnapshot)
	if err != nil || !SameFamilyScope(periodScope, input.Chain.Family.Scope) ||
		!validProfileReference(profileRef, input.ProfileSnapshot, input.Chain.Family.Scope.OrganisationID) ||
		input.PeriodSnapshot.OrganisationId != input.Chain.Family.Scope.OrganisationID ||
		!sameRef(input.PeriodSnapshot.Confirmation.Profile, profileRef) {
		return nil, ErrChainSnapshot
	}
	return &RevisionPlan{
		ExpectedChain:     proto.Clone(input.ExpectedChain).(*tammyv1.BasChainExpectation),
		EffectiveOriginal: preparationReference(input.Chain.EffectiveOriginal),
		LatestHead:        preparationReference(input.Chain.LatestHead),
		SuccessorState:    tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING,
	}, nil
}

func PlanCorrection(input CorrectionInput) (*CorrectionPlan, error) {
	if err := requireExactExpectedChain(input.Chain, input.ExpectedChain); err != nil {
		return nil, err
	}
	if input.Correction == nil || !validCorrectionPayload(input.Correction) || !attemptMatchesFrozen(input.Attempt) ||
		!sameRef(input.Attempt.Preparation.Family, input.Chain.Family.Reference) || !frozenScopeMatchesFamily(input.Attempt, input.Chain.Family.Scope) ||
		!sameLoadedChain(input.Chain, input.History.Chain) {
		return nil, ErrObservationInput
	}
	history, err := deriveExternalHistory(input.History)
	if err != nil {
		return nil, err
	}
	target := history.observations[refKey(input.Correction.DisputedObservation)]
	if target == nil || !sameRef(target.frozen, input.Attempt.FrozenReference) {
		return nil, ErrExternalHistory
	}
	qualifying := qualifyingEvent(target.directKind) || qualifyingEvent(target.effectiveKind)
	if target.logical != nil {
		// A later resolution may uphold a retraction, leaving the logical
		// assertion nonqualifying while a stale-target correction still
		// competes with its retained direct qualifying evidence.
		qualifying = qualifyingEvent(target.logical.directKind) || qualifyingEvent(target.logical.effectiveKind)
	}
	staleTarget := false
	for _, prior := range history.observations {
		if prior.logical == target.logical && prior.message.GetCorrection() != nil && prior.ordinal > target.ordinal {
			staleTarget = true
			break
		}
	}
	losesQualification := input.Correction.CorrectionKind == tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_RETRACT_AS_INCORRECT ||
		(input.Correction.CorrectedEventKind != nil && !qualifyingEvent(*input.Correction.CorrectedEventKind))
	terminalAttempt := input.Attempt.Preparation.State == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION ||
		input.Attempt.Preparation.State == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_AFTER_RECORDED_REJECTION
	reclassifiesTerminalUse := terminalAttempt && !qualifying && input.Correction.CorrectedEventKind != nil &&
		qualifyingEvent(*input.Correction.CorrectedEventKind)
	// A correction against an assertion superseded by a later correction creates
	// a competing retained history even when both event kinds still qualify.
	opensDispute := (qualifying && (losesQualification || staleTarget)) || reclassifiesTerminalUse
	nextOrdinal, ok := nextExternalHistoryOrdinal(input.History.LastOrdinal)
	projectedHistory := history.withAppendedCorrection(input.Correction, nextOrdinal)
	if !ok || !projectedMutationWithinHistoryBound(len(input.History.Records), opensDispute, projectedHistory) {
		return nil, ErrExternalHistory
	}
	plan := &CorrectionPlan{
		ExpectedChain: proto.Clone(input.ExpectedChain).(*tammyv1.BasChainExpectation),
		Correction:    proto.Clone(input.Correction).(*tammyv1.BasExternalObservationCorrectionInput),
		Frozen:        cloneRef(input.Attempt.FrozenReference),
		Qualifies:     false,
		AdvancesHead:  false,
		OpensDispute:  opensDispute,
		NextOverlay:   input.Chain.DisputeOverlay,
	}
	if opensDispute {
		plan.NextOverlay = tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_DISPUTED
		if index := history.disputeDependencyIndex(input.Attempt); index >= 0 {
			plan.AffectedConfirmedChain = refsFromConfirmed(history.confirmed, index)
		}
	}
	return plan, nil
}

func PlanResolveDispute(input DisputeResolutionInput) (*DisputeResolutionPlan, error) {
	if err := requireExactExpectedChain(input.Chain, input.ExpectedChain); err != nil {
		return nil, err
	}
	if input.Chain.DisputeOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_DISPUTED || input.Resolution == nil ||
		!validDisputeInput(input.Resolution) || !sameLoadedChain(input.Chain, input.History.Chain) ||
		len(input.History.Records) >= maxExternalHistoryRecords ||
		(input.Chain.LatestHead == nil && input.Resolution.Outcome == tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_REQUIRE_CORRECTIVE_REVISION) {
		return nil, ErrExternalHistory
	}
	if !sameAttemptLists(input.ConfirmedDependencies, input.History.ConfirmedDependencies) {
		return nil, ErrExternalHistory
	}
	history, err := deriveExternalHistory(input.History)
	if err != nil {
		return nil, err
	}
	derivedDispute := history.disputeSummary()
	if len(derivedDispute.observations) == 0 || !sameRefSlices(input.Resolution.DisputedObservations, derivedDispute.observations) ||
		!sameRefSlices(input.Resolution.AffectedConfirmedChain, derivedDispute.affected) ||
		!bytes.Equal(input.Resolution.DependentChainManifestHash, dependencyRefsManifest(derivedDispute.affected)) {
		return nil, ErrExternalHistory
	}
	plan := &DisputeResolutionPlan{
		ExpectedChain:          proto.Clone(input.ExpectedChain).(*tammyv1.BasChainExpectation),
		DisputedObservations:   cloneRefs(derivedDispute.observations),
		AffectedConfirmedChain: cloneRefs(derivedDispute.affected),
	}
	switch input.Resolution.Outcome {
	case tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_UPHOLD_RECORDED_CHAIN:
		plan.NextOverlay = tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_RESOLVED_UPHELD
		// Upholding only clears the overlay. Existing review and branch workflow
		// remain intact, including a replacement reserved before the first head.
		return plan, nil
	case tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_REQUIRE_CORRECTIVE_REVISION:
		plan.NextOverlay = tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_CORRECTION_REQUIRED
		plan.ReserveCorrectiveBranch = true
	default:
		return nil, ErrExternalHistory
	}
	if input.Chain.Branch != nil {
		branch := input.Chain.Branch
		if branch.RootHead == nil || !samePreparationAggregate(branch.RootHead, input.Chain.LatestHead) || branch.Resolution != nil || branch.FrozenBarrier != nil {
			return nil, ErrChainSnapshot
		}
		plan.AdoptedExistingBranch = true
		if branch.ActiveAttempt == nil {
			return plan, nil
		}
		if branch.ActiveAttempt.State == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING ||
			branch.ActiveAttempt.State == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY {
			plan.ResetMutableAttemptToCollecting = true
			plan.ClearCurrentReviewPointers = true
			return plan, nil
		}
		if branch.ActiveAttempt.State == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF && validRef(branch.ActiveAttempt.FrozenBas, "bas_frozen") {
			plan.FrozenBarrier = cloneRef(branch.ActiveAttempt.FrozenBas)
			plan.PreserveFrozenAttempt = true
			return plan, nil
		}
		return nil, ErrChainSnapshot
	}
	return plan, nil
}

func planObservationFromHistory(input ObservationInput, expected *tammyv1.BasChainExpectation, history *derivedExternalHistory) (*ObservationPlan, error) {
	attempt := input.Attempt
	if !attemptPresentInHistory(attempt, input.History) {
		return nil, ErrExternalHistory
	}
	if input.Chain.DisputeOverlay == tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_CORRECTION_REQUIRED {
		if input.Chain.Branch == nil || input.Chain.Branch.ActiveAttempt == nil || input.Chain.Branch.Resolution == nil ||
			!samePreparationAggregate(input.Chain.Branch.ActiveAttempt, attempt.Preparation) ||
			!sameRef(input.Chain.Branch.ActiveAttempt.FrozenBas, attempt.FrozenReference) ||
			(input.Chain.Branch.FrozenBarrier != nil && sameRef(input.Chain.Branch.FrozenBarrier, attempt.FrozenReference)) {
			return nil, ErrObservationInput
		}
	}
	if input.Chain.Branch != nil && input.Chain.Branch.ActiveAttempt != nil &&
		attempt.Preparation.State == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF &&
		!samePreparationAggregate(input.Chain.Branch.ActiveAttempt, attempt.Preparation) {
		return nil, ErrObservationInput
	}

	kind := input.Observation.EventKind
	qualifies := kind == tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT ||
		kind == tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE
	plan := &ObservationPlan{
		Observation:             proto.Clone(input.Observation).(*tammyv1.BasExternalObservationInput),
		Frozen:                  cloneRef(attempt.FrozenReference),
		Trust:                   tammyv1.BasExternalTrust_BAS_EXTERNAL_TRUST_USER_RECORDED_UNVERIFIED,
		Qualifies:               qualifies,
		ExpectedChain:           proto.Clone(expected).(*tammyv1.BasChainExpectation),
		NextOverlay:             input.Chain.DisputeOverlay,
		ReserveCorrectiveBranch: input.Chain.DisputeOverlay == tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_CORRECTION_REQUIRED,
	}

	var existingQualification, existingRejection bool
	for _, prior := range history.byFrozen[refKey(attempt.FrozenReference)] {
		if qualifyingEvent(prior.directKind) &&
			(prior.ordinal > prior.resolvedOrdinal || prior.invalidatedOrdinal > prior.resolvedOrdinal) {
			existingQualification = true
		}
		if prior.directKind == tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION && prior.ordinal > prior.resolvedOrdinal {
			existingRejection = true
		}
	}

	if qualifies {
		if attempt.Preparation.State == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION ||
			attempt.Preparation.State == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_AFTER_RECORDED_REJECTION ||
			attempt.Preparation.State == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BY_CONFIRMED_REVISION {
			plan.OpensDispute = true
			plan.NextOverlay = tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_DISPUTED
			if index := history.disputeDependencyIndex(attempt); index >= 0 {
				plan.AffectedConfirmedChain = refsFromConfirmed(history.confirmed, index)
			}
			return plan, nil
		}
		if existingRejection {
			plan.OpensDispute = true
			plan.NextOverlay = tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_DISPUTED
			if index := history.disputeDependencyIndex(attempt); index >= 0 {
				plan.AffectedConfirmedChain = refsFromConfirmed(history.confirmed, index)
			}
			return plan, nil
		}
		if attemptIsCurrentFrozen(input.Chain, attempt) {
			if input.Chain.LatestHead != nil && samePreparationIdentity(input.Chain.LatestHead, attempt.Preparation) {
				// A second user report for the exact current retained head appends history,
				// but cannot advance again or create a synthetic dispute.
				return plan, nil
			}
			if existingQualification {
				return nil, ErrExternalHistory
			}
			if input.Chain.DisputeOverlay == tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_DISPUTED {
				return nil, ErrExternalHistory
			}
			if input.Chain.Branch != nil && input.Chain.Branch.RootHead != nil &&
				(input.Chain.LatestHead == nil || !samePreparationAggregate(input.Chain.Branch.RootHead, input.Chain.LatestHead)) {
				return nil, ErrChainSnapshot
			}
			if input.Chain.Branch != nil && input.Chain.Branch.Resolution != nil {
				resolution := history.resolutions[refKey(input.Chain.Branch.Resolution)]
				if resolution == nil || resolution.message.Resolution.Outcome != tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_REQUIRE_CORRECTIVE_REVISION {
					return nil, ErrExternalHistory
				}
				plan.CompletedResolution = cloneRef(resolution.reference)
				plan.AffectedConfirmedChain = cloneRefs(resolution.message.Resolution.AffectedConfirmedChain)
				plan.NextOverlay = tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_NONE
				plan.ReserveCorrectiveBranch = false
			}
			plan.AdvancesHead = true
			return plan, nil
		}
	}

	if kind == tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION {
		if index := confirmedDependencyIndex(history.confirmed, attempt.FrozenReference); index >= 0 {
			plan.OpensDispute = true
			plan.NextOverlay = tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_DISPUTED
			plan.AffectedConfirmedChain = refsFromConfirmed(history.confirmed, index)
		}
	}
	if input.Chain.DisputeOverlay == tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_CORRECTION_REQUIRED &&
		kind == tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION {
		return nil, ErrObservationInput
	}
	return plan, nil
}

func attemptPresentInHistory(attempt AttemptSnapshot, history ExternalHistoryInput) bool {
	for _, candidate := range history.Attempts {
		if sameRef(candidate.FrozenReference, attempt.FrozenReference) && proto.Equal(candidate.Frozen, attempt.Frozen) &&
			candidate.Preparation.PreparationId == attempt.Preparation.PreparationId {
			return true
		}
	}
	for _, candidate := range history.ConfirmedDependencies {
		if sameRef(candidate.FrozenReference, attempt.FrozenReference) && proto.Equal(candidate.Frozen, attempt.Frozen) &&
			candidate.Preparation.PreparationId == attempt.Preparation.PreparationId {
			return true
		}
	}
	return false
}

func attemptIsCurrentFrozen(chain ChainSnapshot, attempt AttemptSnapshot) bool {
	if attempt.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF ||
		chain.Branch != nil && chain.Branch.FrozenBarrier != nil && sameRef(chain.Branch.FrozenBarrier, attempt.FrozenReference) {
		return false
	}
	if chain.Branch != nil {
		return chain.Branch.ActiveAttempt != nil && samePreparationAggregate(chain.Branch.ActiveAttempt, attempt.Preparation) &&
			sameRef(chain.Branch.ActiveAttempt.FrozenBas, attempt.FrozenReference)
	}
	return chain.LatestHead == nil && samePreparationIdentity(chain.RootPreparation, attempt.Preparation)
}

func confirmedDependencyIndex(attempts []AttemptSnapshot, frozen *tammyv1.SourceRef) int {
	for index, attempt := range attempts {
		if sameRef(attempt.FrozenReference, frozen) {
			return index
		}
	}
	return -1
}

func refsFromConfirmed(attempts []AttemptSnapshot, start int) []*tammyv1.SourceRef {
	if start < 0 || start >= len(attempts) {
		return nil
	}
	return confirmedDependencyRefs(attempts[start:])
}

func requireExactExpectedChain(chain ChainSnapshot, expected *tammyv1.BasChainExpectation) error {
	current, err := BuildChainExpectation(chain)
	if err != nil || expected == nil || !proto.Equal(current, expected) {
		return ErrChainSnapshot
	}
	return nil
}

func validateReviewBinding(input ReviewInput) error {
	prep, calc, validation := input.Preparation, input.Calculation, input.Validation
	if validation == nil || calc == nil || calc.Result == nil || !validUUIDv7(validation.ValidationId) || validation.Revision == 0 || len(validation.ContentHash) != sha256.Size || len(validation.PreparationHash) != sha256.Size || len(validation.CalculationHash) != sha256.Size || len(validation.SourceSnapshotHash) != sha256.Size || validation.PreparationId != prep.PreparationId || validation.CalculationId != calc.CalculationId || !bytes.Equal(validation.CalculationHash, calc.ContentHash) || !sameSourceContent(validation.SourceSnapshotHash, calc.SourceSnapshotHash) || validation.InputRevision != prep.InputRevision || !bytes.Equal(validation.InputHash, prep.InputHash) {
		return ErrReviewInput
	}
	if !sameRef(prep.CurrentValidation, &tammyv1.SourceRef{Type: "bas_validation", Id: validation.ValidationId, Revision: validation.Revision, ContentHash: validation.ContentHash}) {
		return ErrReviewInput
	}
	if calc.OrganisationId != prep.OrganisationId || calc.InputRevision != prep.InputRevision || !bytes.Equal(calc.InputHash, prep.InputHash) || !sameRef(calc.Family, prep.Family) ||
		!sameRef(calc.Period, prep.Period) || !sameRef(calc.Profile, prep.Profile) ||
		!validConfirmedPeriodReference(calc.Period, calc.PeriodSnapshot, prep.OrganisationId) || !validProfileReference(calc.Profile, calc.ProfileSnapshot, prep.OrganisationId) ||
		!sameRef(calc.Profile, calc.PeriodSnapshot.Confirmation.Profile) || !sameRef(calc.PeriodSnapshot.Confirmation.Profile, prep.Profile) {
		return ErrReviewInput
	}
	if calc.SourceSnapshotHash == nil || len(calc.SourceSnapshotHash) != sha256.Size || len(calc.InputHash) != sha256.Size {
		return ErrReviewInput
	}
	for _, finding := range validation.Findings {
		if !validFinding(finding) {
			return ErrReviewInput
		}
	}
	return nil
}

func validCurrentCalculationReference(prep *tammyv1.BasPreparation, calc *tammyv1.BasCalculation) bool {
	if calc == nil {
		return prep.CurrentCalculation == nil
	}
	if !validUUIDv7(calc.CalculationId) || calc.OrganisationId != prep.OrganisationId || calc.Version == 0 || len(calc.ContentHash) != sha256.Size || calc.Preparation == nil || !validRef(calc.Preparation, "bas_preparation") || calc.Preparation.Id != prep.PreparationId || len(calc.Preparation.ContentHash) != sha256.Size || !validRef(calc.Family, "bas_family") || !validRef(calc.Period, "bas_period") || !validRef(calc.Profile, "bas_obligations_profile") || !validConfirmedPeriodReference(calc.Period, calc.PeriodSnapshot, prep.OrganisationId) || !validProfileReference(calc.Profile, calc.ProfileSnapshot, prep.OrganisationId) {
		return false
	}
	current := &tammyv1.SourceRef{Type: "bas_calculation", Id: calc.CalculationId, Revision: calc.Version, ContentHash: calc.ContentHash}
	return sameRef(prep.CurrentCalculation, current)
}

func validateAcknowledgements(input ReviewInput) (map[string]bool, error) {
	byFinding := make(map[string]*tammyv1.BasValidationFinding, len(input.Validation.Findings))
	for _, finding := range input.Validation.Findings {
		if finding.Severity == tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_WARNING {
			key := findingKey(finding, input.Validation.Revision)
			if _, exists := byFinding[key]; exists {
				return nil, ErrReviewInput
			}
			byFinding[key] = finding
		}
	}
	acknowledged := make(map[string]bool, len(input.Acknowledgements))
	for _, ack := range input.Acknowledgements {
		if ack == nil || !validUUIDv7(ack.AcknowledgementId) || !validUUIDv7(ack.ActorUserId) || !validTimestamp(ack.AcknowledgedAt) || len(ack.SourceSnapshotHash) != sha256.Size || !bytes.Equal(ack.SourceSnapshotHash, input.Validation.SourceSnapshotHash) {
			return nil, ErrReviewInput
		}
		if !sameRef(ack.Validation, validationReference(input.Validation)) || !validRef(ack.Finding, "bas_validation_finding") {
			return nil, ErrReviewInput
		}
		key := ack.Finding.Id + ":" + fmt.Sprint(ack.Finding.Revision)
		finding, exists := byFinding[key]
		if !exists || !bytes.Equal(ack.Finding.ContentHash, canonicalFindingHash(finding)) || acknowledged[key] {
			return nil, ErrReviewInput
		}
		acknowledged[key] = true
	}
	return acknowledged, nil
}

func validateChainSnapshot(chain ChainSnapshot) error {
	if chain.Family == nil || !validRef(chain.Family.Reference, "bas_family") || chain.Family.Version == 0 || !validScope(chain.Family.Scope) ||
		!validPreparationIdentity(chain.RootPreparation) || !sameRef(chain.RootPreparation.Family, chain.Family.Reference) || chain.RootPreparation.OrganisationId != chain.Family.Scope.OrganisationID ||
		!validRef(chain.PeriodReference, "bas_period") || !validDisputeOverlay(chain.DisputeOverlay) {
		return ErrChainSnapshot
	}
	if chain.LatestHead != nil {
		if !validPreparationIdentity(chain.LatestHead) || chain.LatestHead.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED || !sameRef(chain.LatestHead.Family, chain.Family.Reference) || chain.LatestHead.OrganisationId != chain.Family.Scope.OrganisationID {
			return ErrChainSnapshot
		}
		if !validPreparationIdentity(chain.EffectiveOriginal) || chain.EffectiveOriginal.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED ||
			!sameRef(chain.EffectiveOriginal.Family, chain.Family.Reference) || chain.EffectiveOriginal.OrganisationId != chain.Family.Scope.OrganisationID {
			return ErrChainSnapshot
		}
	} else if chain.EffectiveOriginal != nil {
		return ErrChainSnapshot
	}
	if chain.Branch == nil {
		if chain.DisputeOverlay == tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_CORRECTION_REQUIRED {
			return ErrChainSnapshot
		}
		return nil
	}
	branch := chain.Branch
	if !validRef(branch.Reference, "bas_branch") || branch.Version == 0 {
		return ErrChainSnapshot
	}
	if branch.RootHead != nil {
		if chain.LatestHead == nil || !samePreparationAggregate(branch.RootHead, chain.LatestHead) {
			return ErrChainSnapshot
		}
	} else if chain.LatestHead != nil {
		return ErrChainSnapshot
	}
	if branch.ActiveAttempt != nil {
		if !validPreparationIdentity(branch.ActiveAttempt) || !sameRef(branch.ActiveAttempt.Family, chain.Family.Reference) || branch.ActiveAttempt.OrganisationId != chain.Family.Scope.OrganisationID {
			return ErrChainSnapshot
		}
	}
	if branch.Resolution != nil && !validRef(branch.Resolution, "bas_external_dispute_resolution") {
		return ErrChainSnapshot
	}
	if branch.FrozenBarrier != nil && !validRef(branch.FrozenBarrier, "bas_frozen") {
		return ErrChainSnapshot
	}
	if branch.FrozenBarrier != nil && (branch.ActiveAttempt == nil || !sameRef(branch.ActiveAttempt.FrozenBas, branch.FrozenBarrier)) {
		return ErrChainSnapshot
	}
	if chain.DisputeOverlay == tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_CORRECTION_REQUIRED && branch.Resolution == nil {
		return ErrChainSnapshot
	}
	return nil
}

func validDisputeOverlay(value tammyv1.BasDisputeOverlayState) bool {
	return value == tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_NONE ||
		value == tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_DISPUTED ||
		value == tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_CORRECTION_REQUIRED ||
		value == tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_RESOLVED_UPHELD
}

type derivedObservation struct {
	reference          *tammyv1.SourceRef
	frozen             *tammyv1.SourceRef
	ordinal            uint64
	message            *tammyv1.BasExternalObservation
	directKind         tammyv1.BasExternalEventKind
	effectiveKind      tammyv1.BasExternalEventKind
	assertedKind       tammyv1.BasExternalEventKind
	retracted          bool
	conflicting        bool
	invalidatedOrdinal uint64
	resolvedOrdinal    uint64
	logical            *derivedObservation
}

type derivedResolution struct {
	reference *tammyv1.SourceRef
	message   *tammyv1.BasExternalDisputeResolution
	ordinal   uint64
}

type derivedExternalHistory struct {
	observations     map[string]*derivedObservation
	direct           []*derivedObservation
	resolutions      map[string]*derivedResolution
	byFrozen         map[string][]*derivedObservation
	latestCorrection map[string]string
	confirmed        []AttemptSnapshot
	retainedAttempts []AttemptSnapshot
	preparations     map[string]*tammyv1.BasPreparation
	root             *tammyv1.BasPreparation
}

type derivedDispute struct {
	observations []*tammyv1.SourceRef
	affected     []*tammyv1.SourceRef
}

const (
	projectedObservationKey = "\x00projected-observation"
	projectedCorrectionKey  = "\x00projected-correction"
)

func nextExternalHistoryOrdinal(last uint64) (uint64, bool) {
	if last == ^uint64(0) {
		return 0, false
	}
	return last + 1, true
}

func projectedMutationWithinHistoryBound(recordCount int, opensDispute bool, history *derivedExternalHistory) bool {
	if recordCount >= maxExternalHistoryRecords || history == nil {
		return false
	}
	subjectCount := len(history.disputeSummary().observations)
	if subjectCount > maxExternalDisputeSubjects {
		return false
	}
	// A mutation that leaves a dispute open must preserve room for the explicit
	// resolution record, even when the mutation itself still fits the history
	// record bound.
	disputeNeedsResolution := opensDispute || subjectCount > 0
	return !disputeNeedsResolution || recordCount+1 < maxExternalHistoryRecords
}

// cloneForProjection copies reducer state so projecting a proposed correction
// cannot mutate the current loaded history's logical observations.
func (history *derivedExternalHistory) cloneForProjection() *derivedExternalHistory {
	if history == nil {
		return nil
	}
	projection := *history
	clones := make(map[*derivedObservation]*derivedObservation, len(history.observations))
	for _, observation := range history.observations {
		copyObservation := *observation
		clones[observation] = &copyObservation
	}
	projection.observations = make(map[string]*derivedObservation, len(history.observations)+1)
	for key, observation := range history.observations {
		copyObservation := clones[observation]
		if observation.logical != nil {
			copyObservation.logical = clones[observation.logical]
		}
		projection.observations[key] = copyObservation
	}
	projection.direct = make([]*derivedObservation, 0, len(history.direct))
	for _, observation := range history.direct {
		projection.direct = append(projection.direct, clones[observation])
	}
	projection.byFrozen = make(map[string][]*derivedObservation, len(history.byFrozen))
	for key, observations := range history.byFrozen {
		copyObservations := make([]*derivedObservation, 0, len(observations))
		for _, observation := range observations {
			copyObservations = append(copyObservations, clones[observation])
		}
		projection.byFrozen[key] = copyObservations
	}
	projection.latestCorrection = make(map[string]string, len(history.latestCorrection)+1)
	for logicalKey, correctionKey := range history.latestCorrection {
		projection.latestCorrection[logicalKey] = correctionKey
	}
	return &projection
}

func (history *derivedExternalHistory) withAppendedObservation(attempt AttemptSnapshot, kind tammyv1.BasExternalEventKind, ordinal uint64) *derivedExternalHistory {
	projection := history.cloneForProjection()
	if projection == nil {
		return nil
	}
	candidate := &derivedObservation{
		frozen: cloneRef(attempt.FrozenReference), ordinal: ordinal,
		message:    &tammyv1.BasExternalObservation{},
		directKind: kind, effectiveKind: kind, assertedKind: kind,
	}
	candidate.logical = candidate
	projection.observations[projectedObservationKey] = candidate
	projection.direct = append(projection.direct, candidate)
	frozenKey := refKey(candidate.frozen)
	projection.byFrozen[frozenKey] = append(projection.byFrozen[frozenKey], candidate)
	return projection
}

func (history *derivedExternalHistory) withAppendedCorrection(correction *tammyv1.BasExternalObservationCorrectionInput, ordinal uint64) *derivedExternalHistory {
	projection := history.cloneForProjection()
	if projection == nil {
		return nil
	}
	target := projection.observations[refKey(correction.DisputedObservation)]
	if target == nil {
		return nil
	}
	candidate := &derivedObservation{
		frozen: cloneRef(target.frozen), ordinal: ordinal,
		message: &tammyv1.BasExternalObservation{Payload: &tammyv1.BasExternalObservation_Correction{Correction: correction}},
	}
	if err := projection.applyCorrection(candidate, correction, projectedCorrectionKey); err != nil {
		return nil
	}
	projection.observations[projectedCorrectionKey] = candidate
	return projection
}

func (history *derivedExternalHistory) applyCorrection(derived *derivedObservation, correction *tammyv1.BasExternalObservationCorrectionInput, correctionKey string) error {
	target := history.observations[refKey(correction.DisputedObservation)]
	if target == nil || !sameRef(target.frozen, derived.frozen) || target.logical == nil {
		return ErrExternalHistory
	}
	logical := target.logical
	logicalKey := refKey(logical.reference)
	if latest, exists := history.latestCorrection[logicalKey]; exists && latest != refKey(target.reference) {
		logical.conflicting = true
		logical.invalidatedOrdinal = derived.ordinal
	}
	history.latestCorrection[logicalKey] = correctionKey
	derived.logical = logical
	derived.directKind = logical.directKind
	if correction.CorrectedEventKind != nil {
		derived.assertedKind = *correction.CorrectedEventKind
	}
	losesCurrentQualification := qualifyingEvent(logical.effectiveKind) &&
		(correction.CorrectionKind == tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_RETRACT_AS_INCORRECT ||
			(correction.CorrectedEventKind != nil && !qualifyingEvent(*correction.CorrectedEventKind)))
	if losesCurrentQualification {
		// Preserve which qualifying correction assertions a later retraction
		// invalidated. A resolution suppresses them until another append ordinal
		// changes that retained assertion's status.
		for _, prior := range history.observations {
			if prior.logical == logical && prior.message.GetCorrection() != nil &&
				qualifyingEvent(prior.assertedKind) && prior.ordinal < derived.ordinal {
				prior.invalidatedOrdinal = derived.ordinal
			}
		}
	}
	if correction.CorrectionKind == tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_RETRACT_AS_INCORRECT {
		logical.retracted = true
		logical.effectiveKind = tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_UNSPECIFIED
		logical.invalidatedOrdinal = derived.ordinal
	} else if correction.CorrectedEventKind != nil {
		logical.effectiveKind = *correction.CorrectedEventKind
		logical.retracted = false
		logical.invalidatedOrdinal = derived.ordinal
	}
	return nil
}

func validateExternalHistory(history ExternalHistoryInput) error {
	_, err := deriveExternalHistory(history)
	return err
}

// externalPreparationCount counts distinct workflow attempts, including exact
// attempts referenced by family and branch pointers. Those pointers remain part
// of the retained family even when a caller omits them from Attempts.
func externalPreparationCount(history ExternalHistoryInput) (int, error) {
	seen := make(map[string]struct{}, len(history.Attempts)+len(history.ConfirmedDependencies)+5)
	add := func(preparation *tammyv1.BasPreparation) error {
		if !validPreparationIdentity(preparation) {
			return ErrExternalHistory
		}
		seen[preparation.PreparationId] = struct{}{}
		return nil
	}
	for _, attempt := range history.Attempts {
		if err := add(attempt.Preparation); err != nil {
			return 0, err
		}
	}
	for _, attempt := range history.ConfirmedDependencies {
		if err := add(attempt.Preparation); err != nil {
			return 0, err
		}
	}
	for _, preparation := range []*tammyv1.BasPreparation{
		history.Chain.RootPreparation,
		history.Chain.EffectiveOriginal,
		history.Chain.LatestHead,
	} {
		if preparation != nil {
			if err := add(preparation); err != nil {
				return 0, err
			}
		}
	}
	if history.Chain.Branch != nil {
		for _, preparation := range []*tammyv1.BasPreparation{history.Chain.Branch.RootHead, history.Chain.Branch.ActiveAttempt} {
			if preparation != nil {
				if err := add(preparation); err != nil {
					return 0, err
				}
			}
		}
	}
	return len(seen), nil
}

// deriveExternalHistory reduces only complete retained protobuf events ordered
// by Core's append ordinal. A correction remains a separate observation and
// never becomes fresh confirmation authority.
func deriveExternalHistory(history ExternalHistoryInput) (*derivedExternalHistory, error) {
	if len(history.Records) > maxExternalHistoryRecords || len(history.Attempts) > maxExternalHistoryAttempts || len(history.ConfirmedDependencies) > 200 {
		return nil, ErrExternalHistory
	}
	if _, err := BuildChainExpectation(history.Chain); err != nil {
		return nil, ErrExternalHistory
	}
	attemptCount, err := externalPreparationCount(history)
	if err != nil || attemptCount > maxExternalHistoryAttempts {
		return nil, ErrExternalHistory
	}
	if err := validateConfirmedDependencies(history); err != nil {
		return nil, err
	}
	if err := validateActiveAttemptPredecessors(history); err != nil {
		return nil, err
	}
	if (len(history.Records) == 0 && history.LastOrdinal != 0) || (len(history.Records) != 0 && history.Records[len(history.Records)-1].Ordinal != history.LastOrdinal) {
		return nil, ErrExternalHistory
	}
	attemptByFrozen := make(map[string]AttemptSnapshot, len(history.Attempts)+len(history.ConfirmedDependencies))
	registerAttempt := func(attempt AttemptSnapshot, confirmed bool) error {
		if !attemptMatchesFrozen(attempt) || !sameRef(attempt.Preparation.Family, history.Chain.Family.Reference) ||
			attempt.Preparation.OrganisationId != history.Chain.Family.Scope.OrganisationID || !frozenScopeMatchesFamily(attempt, history.Chain.Family.Scope) {
			return ErrExternalHistory
		}
		key := refKey(attempt.FrozenReference)
		if prior, exists := attemptByFrozen[key]; exists {
			if !proto.Equal(prior.Frozen, attempt.Frozen) || prior.Preparation.PreparationId != attempt.Preparation.PreparationId {
				return ErrExternalHistory
			}
			if confirmed {
				attemptByFrozen[key] = attempt
			}
			return nil
		}
		attemptByFrozen[key] = attempt
		return nil
	}
	for _, attempt := range history.Attempts {
		if err := registerAttempt(attempt, false); err != nil {
			return nil, err
		}
	}
	for _, attempt := range history.ConfirmedDependencies {
		if attempt.Preparation == nil || attempt.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED {
			return nil, ErrExternalHistory
		}
		if err := registerAttempt(attempt, true); err != nil {
			return nil, err
		}
	}

	result := &derivedExternalHistory{
		observations:     make(map[string]*derivedObservation, len(history.Records)),
		resolutions:      make(map[string]*derivedResolution),
		byFrozen:         make(map[string][]*derivedObservation),
		latestCorrection: make(map[string]string),
		confirmed:        cloneAttemptSnapshots(history.ConfirmedDependencies),
		retainedAttempts: cloneAttemptSnapshots(history.Attempts),
		preparations:     make(map[string]*tammyv1.BasPreparation),
		root:             proto.Clone(history.Chain.RootPreparation).(*tammyv1.BasPreparation),
	}
	result.preparations[refKey(preparationReference(result.root))] = result.root
	for _, attempt := range append(append([]AttemptSnapshot{}, history.Attempts...), history.ConfirmedDependencies...) {
		result.preparations[refKey(preparationReference(attempt.Preparation))] = attempt.Preparation
	}
	var previous uint64
	for _, record := range history.Records {
		if record.Ordinal == 0 || record.Ordinal <= previous || record.Ordinal > history.LastOrdinal || (record.Observation == nil) == (record.Resolution == nil) {
			return nil, ErrExternalHistory
		}
		previous = record.Ordinal
		if observation := record.Observation; observation != nil {
			if !validRetainedObservation(observation, history.Chain.Family, history.Chain.Family.Scope) {
				return nil, ErrExternalHistory
			}
			frozenKey := refKey(observation.FrozenBas)
			if _, ok := attemptByFrozen[frozenKey]; !ok {
				return nil, ErrExternalHistory
			}
			ref := observationReferenceFor(observation)
			key := refKey(ref)
			if result.observations[key] != nil {
				return nil, ErrExternalHistory
			}
			derived := &derivedObservation{reference: ref, frozen: cloneRef(observation.FrozenBas), ordinal: record.Ordinal, message: observation}
			if event := observation.GetObservation(); event != nil {
				if observation.CorrectsObservation != nil || !validObservationPayload(event) {
					return nil, ErrExternalHistory
				}
				derived.directKind = event.EventKind
				derived.effectiveKind = event.EventKind
				derived.assertedKind = event.EventKind
				derived.logical = derived
				result.direct = append(result.direct, derived)
				result.byFrozen[frozenKey] = append(result.byFrozen[frozenKey], derived)
			} else {
				correction := observation.GetCorrection()
				if !validCorrectionPayload(correction) || !sameRef(observation.CorrectsObservation, correction.DisputedObservation) || observation.ObservationId == correction.DisputedObservation.Id {
					return nil, ErrExternalHistory
				}
				derived.frozen = cloneRef(observation.FrozenBas)
				if err := result.applyCorrection(derived, correction, key); err != nil {
					return nil, err
				}
			}
			result.observations[key] = derived
		} else {
			resolution := record.Resolution
			if !validResolutionRecord(resolution, history.Chain.Family) {
				return nil, ErrExternalHistory
			}
			ref := resolutionReferenceFor(resolution)
			key := refKey(ref)
			if result.resolutions[key] != nil {
				return nil, ErrExternalHistory
			}
			retained := &derivedResolution{reference: ref, message: resolution, ordinal: record.Ordinal}
			if err := validateHistoricalResolution(history, result, retained); err != nil {
				return nil, err
			}
			result.resolutions[key] = retained
		}
	}
	if history.Chain.Branch != nil && history.Chain.Branch.Resolution != nil {
		if result.resolutions[refKey(history.Chain.Branch.Resolution)] == nil {
			return nil, ErrExternalHistory
		}
	}
	return result, nil
}

func validateHistoricalResolution(history ExternalHistoryInput, result *derivedExternalHistory, resolution *derivedResolution) error {
	body := resolution.message.Resolution
	for _, disputed := range body.DisputedObservations {
		observation := result.observations[refKey(disputed)]
		if observation == nil || observation.ordinal >= resolution.ordinal {
			return ErrExternalHistory
		}
	}
	// A historical resolution covers the exact chain at its append ordinal.
	// Later confirmed descendants never rewrite that retained manifest.
	if !bytes.Equal(body.DependentChainManifestHash, dependencyRefsManifest(body.AffectedConfirmedChain)) {
		return ErrExternalHistory
	}
	previousIndex := -1
	for _, ref := range body.AffectedConfirmedChain {
		index := -1
		for i, dependency := range history.ConfirmedDependencies {
			if sameRef(ref, preparationReference(dependency.Preparation)) {
				index = i
				break
			}
		}
		if index < 0 || (previousIndex >= 0 && index != previousIndex+1) {
			return ErrExternalHistory
		}
		confirmedBefore := false
		for _, observation := range result.byFrozen[refKey(history.ConfirmedDependencies[index].FrozenReference)] {
			if observation.ordinal < resolution.ordinal && qualifyingEvent(observation.directKind) {
				confirmedBefore = true
				break
			}
		}
		if !confirmedBefore {
			return ErrExternalHistory
		}
		previousIndex = index
	}
	// Only the prefix with direct qualifying assertions already retained can
	// have been confirmed at this resolution's append ordinal.
	prefix := 0
	for _, attempt := range result.confirmed {
		confirmedBefore := false
		for _, event := range result.byFrozen[refKey(attempt.FrozenReference)] {
			if qualifyingEvent(event.directKind) {
				confirmedBefore = true
				break
			}
		}
		if !confirmedBefore {
			break
		}
		prefix++
	}
	atResolution := *result
	atResolution.confirmed = result.confirmed[:prefix]
	dispute := atResolution.disputeSummary()
	if !sameRefSlices(body.DisputedObservations, dispute.observations) || !sameRefSlices(body.AffectedConfirmedChain, dispute.affected) {
		return ErrExternalHistory
	}
	for _, subject := range body.DisputedObservations {
		result.observations[refKey(subject)].resolvedOrdinal = resolution.ordinal
	}
	return nil
}

func validateConfirmedDependencies(history ExternalHistoryInput) error {
	deps := history.ConfirmedDependencies
	if history.Chain.LatestHead == nil {
		if len(deps) != 0 {
			return ErrExternalHistory
		}
		return nil
	}
	if len(deps) == 0 || !sameRef(preparationReference(deps[0].Preparation), preparationReference(history.Chain.EffectiveOriginal)) ||
		!sameRef(preparationReference(deps[len(deps)-1].Preparation), preparationReference(history.Chain.LatestHead)) {
		return ErrExternalHistory
	}
	seen := make(map[string]bool, len(deps))
	predecessors := make(map[string]*tammyv1.BasPreparation, len(history.Attempts)+len(deps))
	for _, attempt := range append(append([]AttemptSnapshot{}, history.Attempts...), deps...) {
		if attempt.Preparation == nil {
			return ErrExternalHistory
		}
		predecessors[refKey(preparationReference(attempt.Preparation))] = attempt.Preparation
	}
	for i, attempt := range deps {
		if !attemptMatchesFrozen(attempt) || attempt.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED ||
			!sameRef(attempt.Preparation.Family, history.Chain.Family.Reference) || attempt.Preparation.OrganisationId != history.Chain.Family.Scope.OrganisationID ||
			!frozenScopeMatchesFamily(attempt, history.Chain.Family.Scope) {
			return ErrExternalHistory
		}
		key := refKey(preparationReference(attempt.Preparation))
		if seen[key] {
			return ErrExternalHistory
		}
		seen[key] = true
		if i == 0 {
			if err := validatePredecessorPathToRoot(attempt.Preparation, history.Chain.RootPreparation, predecessors); err != nil {
				return err
			}
		} else {
			anchor := preparationReference(deps[i-1].Preparation)
			current := attempt.Preparation
			visited := make(map[string]bool)
			for !sameRef(current.Predecessor, anchor) {
				key := refKey(current.Predecessor)
				if visited[key] || !validRef(current.Predecessor, "bas_preparation") {
					return ErrExternalHistory
				}
				visited[key] = true
				prior := predecessors[key]
				if prior == nil || (prior.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION &&
					prior.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_AFTER_RECORDED_REJECTION) {
					return ErrExternalHistory
				}
				current = prior
			}
		}
	}
	return nil
}

func validatePredecessorPathToRoot(current, root *tammyv1.BasPreparation, predecessors map[string]*tammyv1.BasPreparation) error {
	if current == nil || root == nil {
		return ErrExternalHistory
	}
	rootReference := preparationReference(root)
	visited := make(map[string]bool)
	for !samePreparationIdentity(current, root) {
		currentKey := refKey(preparationReference(current))
		if visited[currentKey] || !validRef(current.Predecessor, "bas_preparation") {
			return ErrExternalHistory
		}
		visited[currentKey] = true
		if sameRef(current.Predecessor, rootReference) {
			return nil
		}
		prior := predecessors[refKey(current.Predecessor)]
		if prior == nil {
			return ErrExternalHistory
		}
		// Predecessor refs select exact retained versions. The family root's
		// preparation ID is stable even when its current retained version has
		// advanced from the immutable family-root reference.
		if samePreparationIdentity(prior, root) {
			return nil
		}
		if prior.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION &&
			prior.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_AFTER_RECORDED_REJECTION {
			return ErrExternalHistory
		}
		current = prior
	}
	return nil
}

func validRetainedObservation(observation *tammyv1.BasExternalObservation, family *Family, scope FamilyScope) bool {
	if observation == nil || family == nil || !validUUIDv7(observation.ObservationId) || !validUUIDv7(observation.OrganisationId) ||
		observation.OrganisationId != scope.OrganisationID || observation.Trust != tammyv1.BasExternalTrust_BAS_EXTERNAL_TRUST_USER_RECORDED_UNVERIFIED ||
		!validRef(observation.FrozenBas, "bas_frozen") || !validUUIDv7(observation.ActorUserId) || !validTimestamp(observation.RecordedAt) ||
		len(observation.ContentHash) != sha256.Size || (observation.GetObservation() == nil) == (observation.GetCorrection() == nil) {
		return false
	}
	if event := observation.GetObservation(); event != nil {
		return observation.CorrectsObservation == nil && validObservationPayload(event)
	}
	correction := observation.GetCorrection()
	return validCorrectionPayload(correction) && observation.CorrectsObservation != nil &&
		sameRef(observation.CorrectsObservation, correction.DisputedObservation) && observation.ObservationId != correction.DisputedObservation.Id
}

func validCorrectionPayload(correction *tammyv1.BasExternalObservationCorrectionInput) bool {
	if correction == nil || !validRef(correction.DisputedObservation, "bas_external_observation") || !validReason(correction.Reason) || len(correction.Evidence) > 20 {
		return false
	}
	for _, evidence := range correction.Evidence {
		if evidence == nil || !validRef(evidence, evidence.Type) {
			return false
		}
	}
	switch correction.CorrectionKind {
	case tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_RETRACT_AS_INCORRECT:
		return correction.CorrectedEventKind == nil && correction.CorrectedClaimedEventAt == nil
	case tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_REPLACE_ASSERTION:
		if correction.CorrectedEventKind == nil && correction.CorrectedClaimedEventAt == nil {
			return false
		}
		if correction.CorrectedEventKind != nil && !validEventKind(*correction.CorrectedEventKind) {
			return false
		}
		return correction.CorrectedClaimedEventAt == nil || validTimestamp(correction.CorrectedClaimedEventAt)
	default:
		return false
	}
}

func validResolutionRecord(resolution *tammyv1.BasExternalDisputeResolution, family *Family) bool {
	if resolution == nil || family == nil || !validUUIDv7(resolution.ResolutionId) || !sameRef(resolution.Family, family.Reference) ||
		resolution.Resolution == nil || !validUUIDv7(resolution.ActorUserId) || !validTimestamp(resolution.ResolvedAt) || len(resolution.ContentHash) != sha256.Size {
		return false
	}
	return validDisputeInput(resolution.Resolution)
}

func validDisputeInput(input *tammyv1.BasExternalDisputeInput) bool {
	if input == nil || (input.Outcome != tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_UPHOLD_RECORDED_CHAIN &&
		input.Outcome != tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_REQUIRE_CORRECTIVE_REVISION) ||
		len(input.DisputedObservations) == 0 || len(input.DisputedObservations) > 200 ||
		(input.Outcome == tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_REQUIRE_CORRECTIVE_REVISION && len(input.AffectedConfirmedChain) == 0) ||
		len(input.AffectedConfirmedChain) > 200 || len(input.DependentChainManifestHash) != sha256.Size || !validReason(input.Reason) || len(input.Evidence) > 20 {
		return false
	}
	if !uniqueRefs(input.DisputedObservations, "bas_external_observation") || !uniqueRefs(input.AffectedConfirmedChain, "bas_preparation") {
		return false
	}
	for _, evidence := range input.Evidence {
		if evidence == nil || !validRef(evidence, evidence.Type) {
			return false
		}
	}
	return true
}

func (history *derivedExternalHistory) blocksUnused(frozen *tammyv1.SourceRef) bool {
	for _, observation := range history.byFrozen[refKey(frozen)] {
		if observation.conflicting || observation.logical == nil || observation.logical.conflicting {
			return true
		}
		if observation.directKind == tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT ||
			observation.directKind == tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE ||
			observation.effectiveKind == tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT ||
			observation.effectiveKind == tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE ||
			observation.effectiveKind == tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING ||
			observation.effectiveKind == tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_UNKNOWN ||
			observation.effectiveKind == tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION {
			return true
		}
	}
	return false
}

func (history *derivedExternalHistory) unambiguousRejection(frozen *tammyv1.SourceRef) (*derivedObservation, bool) {
	var rejection *derivedObservation
	for _, observation := range history.byFrozen[refKey(frozen)] {
		if observation.logical == nil || observation.logical.conflicting || observation.conflicting {
			return nil, false
		}
		switch observation.effectiveKind {
		case tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION:
			if observation.directKind != tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION || observation.logical.retracted || rejection != nil {
				return nil, false
			}
			rejection = observation
		case tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING,
			tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_UNKNOWN,
			tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT,
			tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE:
			return nil, false
		}
	}
	return rejection, rejection != nil
}

func (history *derivedExternalHistory) hasRecoveryConflict(frozen *tammyv1.SourceRef) bool {
	_, ok := history.unambiguousRejection(frozen)
	return !ok
}

// A late assertion of use affects the confirmed suffix rooted at its exact
// predecessor. Replacements of the immutable family root affect the full chain.
func (history *derivedExternalHistory) disputeDependencyIndex(attempt AttemptSnapshot) int {
	if index := confirmedDependencyIndex(history.confirmed, attempt.FrozenReference); index >= 0 {
		return index
	}
	if len(history.confirmed) == 0 || attempt.Preparation == nil {
		return -1
	}
	current := attempt.Preparation
	seen := make(map[string]bool)
	for current != nil {
		if samePreparationIdentity(current, history.root) {
			return 0
		}
		for index, dependency := range history.confirmed {
			if sameRef(current.Predecessor, preparationReference(dependency.Preparation)) {
				return index
			}
		}
		key := refKey(current.Predecessor)
		if seen[key] {
			return -1
		}
		seen[key] = true
		current = history.preparations[key]
	}
	return -1
}

func (history *derivedExternalHistory) disputeSummary() derivedDispute {
	if history == nil {
		return derivedDispute{}
	}
	firstAffected := -1
	// Retained references are unique after history reduction. Pointer identity
	// also lets an in-memory projected event count as a new subject without
	// inventing a persisted SourceRef for it.
	disputedKeys := make(map[*derivedObservation]bool)
	var disputed []*derivedObservation
	for index, attempt := range history.confirmed {
		var qualifying, rejected []*derivedObservation
		var invalidated []*derivedObservation
		for _, observation := range history.byFrozen[refKey(attempt.FrozenReference)] {
			if observation.directKind == tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT ||
				observation.directKind == tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE {
				qualifying = append(qualifying, observation)
				if observation.invalidatedOrdinal > observation.resolvedOrdinal &&
					(observation.retracted || observation.conflicting || !qualifyingEvent(observation.effectiveKind)) {
					invalidated = append(invalidated, observation)
				}
			}
			if observation.directKind == tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION && observation.ordinal > observation.resolvedOrdinal {
				rejected = append(rejected, observation)
			}
		}
		if len(qualifying) == 0 || (len(rejected) == 0 && len(invalidated) == 0) {
			continue
		}
		if firstAffected == -1 || index < firstAffected {
			firstAffected = index
		}
		for _, observation := range append(append([]*derivedObservation{}, qualifying...), rejected...) {
			if !disputedKeys[observation] {
				disputedKeys[observation] = true
				disputed = append(disputed, observation)
			}
		}
		// Typed retractions/corrections are dispute subjects in their own right;
		// retaining only the original assertion would hide why it was invalidated.
		for _, correction := range history.observations {
			if correction.message.GetCorrection() == nil {
				continue
			}
			for _, original := range invalidated {
				if correction.logical == original && correction.ordinal > correction.resolvedOrdinal && !disputedKeys[correction] {
					disputedKeys[correction] = true
					disputed = append(disputed, correction)
				}
			}
		}
	}
	// Unconfirmed attempts can have disputed use: a late qualifying assertion
	// on an unused/rejected predecessor, or competing assertions on the active
	// frozen attempt. Neither may create a head through dispute resolution.
	seenAttempts := make(map[string]bool)
	for _, attempt := range history.retainedAttempts {
		state := attempt.Preparation.GetState()
		terminal := state == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION ||
			state == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_AFTER_RECORDED_REJECTION
		if !terminal && state != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF {
			continue
		}
		key := refKey(attempt.FrozenReference)
		if seenAttempts[key] || confirmedDependencyIndex(history.confirmed, attempt.FrozenReference) >= 0 {
			continue
		}
		seenAttempts[key] = true
		var qualifying []*derivedObservation
		conflicting := terminal
		for _, observation := range history.observations {
			if !sameRef(observation.frozen, attempt.FrozenReference) {
				continue
			}
			if observation.logical == observation &&
				(qualifyingEvent(observation.directKind) || qualifyingEvent(observation.effectiveKind)) &&
				(observation.ordinal > observation.resolvedOrdinal || observation.invalidatedOrdinal > observation.resolvedOrdinal) {
				qualifying = append(qualifying, observation)
				if observation.invalidatedOrdinal > observation.resolvedOrdinal &&
					(observation.retracted || observation.conflicting || !qualifyingEvent(observation.effectiveKind)) {
					conflicting = true
				}
			}
			if observation.message.GetCorrection() != nil && qualifyingEvent(observation.assertedKind) &&
				observation.invalidatedOrdinal > observation.resolvedOrdinal {
				qualifying = append(qualifying, observation)
				conflicting = true
			}
			if observation.logical == observation && observation.directKind == tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION &&
				observation.ordinal > observation.resolvedOrdinal {
				conflicting = true
			}
		}
		if len(qualifying) == 0 || !conflicting {
			continue
		}
		index := history.disputeDependencyIndex(attempt)
		if index < 0 {
			if len(history.confirmed) != 0 {
				continue
			}
			// Before the first head, the conflicting retained use still needs a
			// reviewed resolution, with an explicitly empty confirmed manifest.
			index = 0
		}
		if firstAffected < 0 || index < firstAffected {
			firstAffected = index
		}
		add := func(subject *derivedObservation) {
			if !disputedKeys[subject] {
				disputedKeys[subject] = true
				disputed = append(disputed, subject)
			}
		}
		for _, subject := range qualifying {
			add(subject)
		}
		for _, subject := range history.byFrozen[key] {
			if subject.directKind == tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION && subject.ordinal > subject.resolvedOrdinal {
				add(subject)
			}
		}
		for _, correction := range history.observations {
			if correction.message.GetCorrection() == nil || correction.ordinal <= correction.resolvedOrdinal {
				continue
			}
			for _, original := range qualifying {
				if correction.logical == original.logical {
					add(correction)
				}
			}
		}
		for _, dependency := range history.confirmed[index:] {
			for _, subject := range history.byFrozen[refKey(dependency.FrozenReference)] {
				if qualifyingEvent(subject.directKind) {
					add(subject)
				}
			}
		}
	}
	if firstAffected < 0 {
		return derivedDispute{}
	}
	// Retain event order, which came from Core append ordinals, for stable resolution input.
	for i := 0; i < len(disputed); i++ {
		for j := i + 1; j < len(disputed); j++ {
			if disputed[j].ordinal < disputed[i].ordinal {
				disputed[i], disputed[j] = disputed[j], disputed[i]
			}
		}
	}
	refs := make([]*tammyv1.SourceRef, 0, len(disputed))
	for _, observation := range disputed {
		refs = append(refs, cloneRef(observation.reference))
	}
	affected := make([]*tammyv1.SourceRef, 0, len(history.confirmed)-firstAffected)
	for _, attempt := range history.confirmed[firstAffected:] {
		affected = append(affected, preparationReference(attempt.Preparation))
	}
	return derivedDispute{observations: refs, affected: affected}
}

func validEventKind(kind tammyv1.BasExternalEventKind) bool {
	return kind >= tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT &&
		kind <= tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REFUND
}

func validReason(reason string) bool {
	return len(strings.TrimSpace(reason)) > 0 && utf8.RuneCountInString(reason) <= 512
}

func observationReferenceFor(observation *tammyv1.BasExternalObservation) *tammyv1.SourceRef {
	if observation == nil {
		return nil
	}
	return &tammyv1.SourceRef{Type: "bas_external_observation", Id: observation.ObservationId, Revision: 1, ContentHash: bytes.Clone(observation.ContentHash)}
}

func resolutionReferenceFor(resolution *tammyv1.BasExternalDisputeResolution) *tammyv1.SourceRef {
	if resolution == nil {
		return nil
	}
	return &tammyv1.SourceRef{Type: "bas_external_dispute_resolution", Id: resolution.ResolutionId, Revision: 1, ContentHash: bytes.Clone(resolution.ContentHash)}
}

func refKey(ref *tammyv1.SourceRef) string {
	if ref == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%s:%s:%d:%x", ref.Type, ref.Id, ref.Revision, ref.ContentHash)
}

func uniqueRefs(refs []*tammyv1.SourceRef, expectedType string) bool {
	seen := make(map[string]bool, len(refs))
	for _, ref := range refs {
		if !validRef(ref, expectedType) || seen[refKey(ref)] {
			return false
		}
		seen[refKey(ref)] = true
	}
	return true
}

func confirmedDependencyRefs(attempts []AttemptSnapshot) []*tammyv1.SourceRef {
	refs := make([]*tammyv1.SourceRef, 0, len(attempts))
	for _, attempt := range attempts {
		refs = append(refs, preparationReference(attempt.Preparation))
	}
	return refs
}

func confirmedDependencyManifest(attempts []AttemptSnapshot) []byte {
	return dependencyRefsManifest(confirmedDependencyRefs(attempts))
}

func dependencyRefsManifest(refs []*tammyv1.SourceRef) []byte {
	var encoded bytes.Buffer
	for _, ref := range refs {
		body, err := (proto.MarshalOptions{Deterministic: true}).Marshal(ref)
		if err != nil || len(body) > int(^uint32(0)) {
			return nil
		}
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(len(body)))
		encoded.Write(size[:])
		encoded.Write(body)
	}
	digest := sha256.Sum256(encoded.Bytes())
	return bytes.Clone(digest[:])
}

func sameRefSlices(left, right []*tammyv1.SourceRef) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if !sameRef(left[i], right[i]) {
			return false
		}
	}
	return true
}

func cloneRefs(refs []*tammyv1.SourceRef) []*tammyv1.SourceRef {
	result := make([]*tammyv1.SourceRef, len(refs))
	for i, ref := range refs {
		result[i] = cloneRef(ref)
	}
	return result
}

func cloneAttemptSnapshots(attempts []AttemptSnapshot) []AttemptSnapshot {
	result := make([]AttemptSnapshot, len(attempts))
	for i, attempt := range attempts {
		result[i] = cloneAttemptPolicy(attempt)
	}
	return result
}

func cloneAttemptPolicy(attempt AttemptSnapshot) AttemptSnapshot {
	result := AttemptSnapshot{Preparation: proto.Clone(attempt.Preparation).(*tammyv1.BasPreparation)}
	if attempt.FrozenReference != nil {
		result.FrozenReference = cloneRef(attempt.FrozenReference)
	}
	if attempt.Frozen != nil {
		result.Frozen = proto.Clone(attempt.Frozen).(*tammyv1.BasFrozenBas)
	}
	return result
}

func cloneExternalHistoryRecords(records []ExternalHistoryRecord) []ExternalHistoryRecord {
	result := make([]ExternalHistoryRecord, len(records))
	for i, record := range records {
		result[i].Ordinal = record.Ordinal
		if record.Observation != nil {
			result[i].Observation = proto.Clone(record.Observation).(*tammyv1.BasExternalObservation)
		}
		if record.Resolution != nil {
			result[i].Resolution = proto.Clone(record.Resolution).(*tammyv1.BasExternalDisputeResolution)
		}
	}
	return result
}

func frozenScopeMatchesFamily(attempt AttemptSnapshot, scope FamilyScope) bool {
	if attempt.Frozen == nil || attempt.Frozen.CalculationSnapshot == nil {
		return false
	}
	actual, err := FamilyScopeFromPeriod(attempt.Frozen.CalculationSnapshot.PeriodSnapshot)
	return err == nil && SameFamilyScope(actual, scope)
}

func replacementAttemptIsCurrent(chain ChainSnapshot, attempt AttemptSnapshot) bool {
	if attempt.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF {
		return false
	}
	if chain.Branch != nil {
		return chain.Branch.ActiveAttempt != nil && samePreparationAggregate(chain.Branch.ActiveAttempt, attempt.Preparation) &&
			sameRef(chain.Branch.ActiveAttempt.FrozenBas, attempt.FrozenReference)
	}
	return chain.LatestHead == nil && samePreparationIdentity(chain.RootPreparation, attempt.Preparation)
}

func qualifyingEvent(kind tammyv1.BasExternalEventKind) bool {
	return kind == tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT || kind == tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE
}

// The repository loads complete retained attempts, including the exact versions
// referenced by predecessor links. A current branch cannot skip a retained
// unused/rejected predecessor rooted at the same effective head.
func validateActiveAttemptPredecessors(history ExternalHistoryInput) error {
	branch := history.Chain.Branch
	if branch == nil || branch.ActiveAttempt == nil {
		return nil
	}
	anchor := history.Chain.RootPreparation
	if branch.RootHead != nil {
		anchor = branch.RootHead
	}
	byRef := make(map[string]*tammyv1.BasPreparation)
	for _, attempt := range append(append([]AttemptSnapshot{}, history.Attempts...), history.ConfirmedDependencies...) {
		if attempt.Preparation == nil {
			return ErrExternalHistory
		}
		byRef[refKey(preparationReference(attempt.Preparation))] = attempt.Preparation
	}
	visited := make(map[string]bool)
	current := branch.ActiveAttempt
	for !samePreparationIdentity(current, anchor) {
		key := refKey(preparationReference(current))
		if visited[key] || !validRef(current.Predecessor, "bas_preparation") {
			return ErrExternalHistory
		}
		visited[key] = true
		if sameRef(current.Predecessor, preparationReference(anchor)) {
			break
		}
		prior := byRef[refKey(current.Predecessor)]
		if prior == nil || (prior.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION &&
			prior.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_AFTER_RECORDED_REJECTION) {
			return ErrExternalHistory
		}
		current = prior
	}
	for _, attempt := range history.Attempts {
		prep := attempt.Preparation
		if prep == nil || samePreparationIdentity(prep, anchor) || visited[refKey(preparationReference(prep))] {
			continue
		}
		if (prep.State == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION ||
			prep.State == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_AFTER_RECORDED_REJECTION) &&
			sameRef(prep.Predecessor, preparationReference(anchor)) {
			return ErrExternalHistory
		}
	}
	return nil
}

func sameAttemptLists(left, right []AttemptSnapshot) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if !sameRef(preparationReference(left[i].Preparation), preparationReference(right[i].Preparation)) ||
			!sameRef(left[i].FrozenReference, right[i].FrozenReference) || !proto.Equal(left[i].Frozen, right[i].Frozen) {
			return false
		}
	}
	return true
}

func sameLoadedChain(left, right ChainSnapshot) bool {
	if left.Family == nil || right.Family == nil || left.Family.Version != right.Family.Version ||
		!sameRef(left.Family.Reference, right.Family.Reference) || !SameFamilyScope(left.Family.Scope, right.Family.Scope) ||
		!proto.Equal(left.RootPreparation, right.RootPreparation) || !proto.Equal(left.EffectiveOriginal, right.EffectiveOriginal) ||
		!proto.Equal(left.LatestHead, right.LatestHead) || !proto.Equal(left.PeriodReference, right.PeriodReference) ||
		left.DisputeOverlay != right.DisputeOverlay || (left.Branch == nil) != (right.Branch == nil) {
		return false
	}
	if left.Branch == nil {
		return true
	}
	return left.Branch.Version == right.Branch.Version && proto.Equal(left.Branch.Reference, right.Branch.Reference) &&
		proto.Equal(left.Branch.RootHead, right.Branch.RootHead) && proto.Equal(left.Branch.ActiveAttempt, right.Branch.ActiveAttempt) &&
		proto.Equal(left.Branch.Resolution, right.Branch.Resolution) && proto.Equal(left.Branch.FrozenBarrier, right.Branch.FrozenBarrier)
}

func validObservationPayload(observation *tammyv1.BasExternalObservationInput) bool {
	if observation == nil || observation.EventKind < tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT || observation.EventKind > tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REFUND || observation.Channel < tammyv1.BasExternalChannel_BAS_EXTERNAL_CHANNEL_ONLINE_PORTAL || observation.Channel > tammyv1.BasExternalChannel_BAS_EXTERNAL_CHANNEL_OTHER || !validTimestamp(observation.ClaimedEventAt) || !validReason(observation.Reason) || len(observation.Evidence) > 20 || (observation.ExternalReference != nil && utf8.RuneCountInString(*observation.ExternalReference) > 256) {
		return false
	}
	for _, evidence := range observation.Evidence {
		if !validRef(evidence, evidence.GetType()) {
			return false
		}
	}
	return true
}

func attemptMatchesFrozen(attempt AttemptSnapshot) bool {
	prep, frozen := attempt.Preparation, attempt.Frozen
	if prep == nil || frozen == nil || !validPreparationIdentity(prep) || !hasFrozenLifecycleState(prep.State) ||
		!validUUIDv7(frozen.FrozenId) || frozen.OrganisationId != prep.OrganisationId || len(frozen.ContentHash) != sha256.Size ||
		!validRef(frozen.Preparation, "bas_preparation") || frozen.Preparation.Id != prep.PreparationId || !sameRef(frozen.Family, prep.Family) ||
		!validRef(frozen.Period, "bas_period") || !sameRef(frozen.Period, prep.Period) ||
		!validRef(frozen.Profile, "bas_obligations_profile") || !sameRef(frozen.Profile, prep.Profile) ||
		!sameRef(frozen.Calculation, prep.CurrentCalculation) || !sameRef(frozen.Validation, prep.CurrentValidation) ||
		!sameRef(attempt.FrozenReference, &tammyv1.SourceRef{Type: "bas_frozen", Id: frozen.FrozenId, Revision: 1, ContentHash: frozen.ContentHash}) ||
		frozen.CalculationSnapshot == nil || frozen.CalculationSnapshot.OrganisationId != prep.OrganisationId ||
		!sameRef(frozen.CalculationSnapshot.Family, prep.Family) || !sameRef(frozen.Period, frozen.CalculationSnapshot.Period) ||
		!sameRef(frozen.Profile, frozen.CalculationSnapshot.Profile) ||
		!validConfirmedPeriodReference(frozen.CalculationSnapshot.Period, frozen.CalculationSnapshot.PeriodSnapshot, prep.OrganisationId) ||
		!validProfileReference(frozen.CalculationSnapshot.Profile, frozen.CalculationSnapshot.ProfileSnapshot, prep.OrganisationId) ||
		!sameRef(frozen.CalculationSnapshot.PeriodSnapshot.Confirmation.Profile, frozen.CalculationSnapshot.Profile) ||
		frozen.CalculationSnapshot.Preparation == nil || !validRef(frozen.CalculationSnapshot.Preparation, "bas_preparation") ||
		frozen.CalculationSnapshot.Preparation.Id != prep.PreparationId || frozen.CalculationSnapshot.InputRevision != prep.InputRevision ||
		!bytes.Equal(frozen.CalculationSnapshot.InputHash, prep.InputHash) ||
		!bytes.Equal(frozen.CalculationSnapshot.SourceSnapshotHash, prep.SourceSnapshotHash) ||
		!sameRef(frozen.Calculation, calculationReference(frozen.CalculationSnapshot)) {
		return false
	}
	return true
}

func validPreparationIdentity(prep *tammyv1.BasPreparation) bool {
	if prep == nil || !validUUIDv7(prep.PreparationId) || !validUUIDv7(prep.OrganisationId) || prep.Version == 0 || len(prep.ContentHash) != sha256.Size || prep.InputRevision == 0 || len(prep.InputHash) != sha256.Size || len(prep.SourceSnapshotHash) != sha256.Size || prep.State < tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING || prep.State > tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BY_CONFIRMED_REVISION || !validRef(prep.Family, "bas_family") || !validRef(prep.Period, "bas_period") || !validRef(prep.Profile, "bas_obligations_profile") {
		return false
	}
	return true
}

func validCapture(capture CurrentCapture) bool {
	return len(capture.SourceSnapshotHash) == sha256.Size && capture.InputRevision > 0 && len(capture.InputHash) == sha256.Size && capture.RevisionVector != nil
}

func validFinding(finding *tammyv1.BasValidationFinding) bool {
	if finding == nil || !validUUIDv7(finding.FindingId) || len(finding.StableCode) == 0 || len(finding.StableCode) > 96 || !findingCodePattern.MatchString(finding.StableCode) ||
		utf8.RuneCountInString(finding.SafeMessage) == 0 || utf8.RuneCountInString(finding.SafeMessage) > 512 || len(finding.Sources) > 20 {
		return false
	}
	switch finding.Severity {
	case tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_BLOCKER,
		tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_WARNING,
		tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_INFORMATION,
		tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_UNSUPPORTED:
	default:
		return false
	}
	for _, source := range finding.Sources {
		if source == nil || !validRef(source, source.Type) {
			return false
		}
	}
	return true
}

func validRef(ref *tammyv1.SourceRef, expectedType string) bool {
	return ref != nil && expectedType != "" && sourceTypePattern.MatchString(expectedType) && ref.Type == expectedType &&
		validUUIDv7(ref.Id) && ref.Revision > 0 && len(ref.ContentHash) == sha256.Size
}

func validConfirmedPeriodReference(ref *tammyv1.SourceRef, period *tammyv1.BasConfirmedPeriod, organisationID string) bool {
	return validRef(ref, "bas_period") && period != nil && validUUIDv7(period.PeriodId) && period.OrganisationId == organisationID && period.Version > 0 && len(period.ContentHash) == sha256.Size && period.Confirmation != nil && validRef(period.Confirmation.Profile, "bas_obligations_profile") && ref.Id == period.PeriodId && ref.Revision == period.Version && bytes.Equal(ref.ContentHash, period.ContentHash)
}

func validProfileReference(ref *tammyv1.SourceRef, profile *tammyv1.BasObligationsProfile, organisationID string) bool {
	return validRef(ref, "bas_obligations_profile") && profile != nil && validUUIDv7(profile.ProfileId) && profile.OrganisationId == organisationID && profile.Revision > 0 && len(profile.ContentHash) == sha256.Size && ref.Id == profile.ProfileId && ref.Revision == profile.Revision && bytes.Equal(ref.ContentHash, profile.ContentHash)
}

func validScope(scope FamilyScope) bool {
	return validUUIDv7(scope.OrganisationID) && validCalendarDate(scope.Start) && validCalendarDate(scope.End) && calendarKey(scope.Start) <= calendarKey(scope.End) && scope.Window > tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_UNSPECIFIED && scope.Window <= tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_CUSTOM
}

func isTerminalPreparationState(state tammyv1.BasPreparationState) bool {
	switch state {
	case tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF,
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED,
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION,
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_AFTER_RECORDED_REJECTION,
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BY_CONFIRMED_REVISION:
		return true
	default:
		return false
	}
}

func hasFrozenLifecycleState(state tammyv1.BasPreparationState) bool {
	return state == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF ||
		state == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED ||
		state == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION ||
		state == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_AFTER_RECORDED_REJECTION ||
		state == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BY_CONFIRMED_REVISION
}

func cloneFinding(finding *tammyv1.BasValidationFinding) *tammyv1.BasValidationFinding {
	if finding == nil {
		return nil
	}
	return proto.Clone(finding).(*tammyv1.BasValidationFinding)
}

func cloneAcknowledgements(values []*tammyv1.BasWarningAcknowledgement) []*tammyv1.BasWarningAcknowledgement {
	result := make([]*tammyv1.BasWarningAcknowledgement, len(values))
	for i, value := range values {
		if value != nil {
			result[i] = proto.Clone(value).(*tammyv1.BasWarningAcknowledgement)
		}
	}
	return result
}

func canonicalFindingHash(finding *tammyv1.BasValidationFinding) []byte {
	encoded, err := (proto.MarshalOptions{Deterministic: true}).Marshal(finding)
	if err != nil {
		return nil
	}
	digest := sha256.Sum256(encoded)
	return bytes.Clone(digest[:])
}

func findingKey(finding *tammyv1.BasValidationFinding, revision uint64) string {
	if finding == nil {
		return ""
	}
	return finding.FindingId + ":" + fmt.Sprint(revision)
}

func validationReference(validation *tammyv1.BasValidationSnapshot) *tammyv1.SourceRef {
	if validation == nil {
		return nil
	}
	return &tammyv1.SourceRef{Type: "bas_validation", Id: validation.ValidationId, Revision: validation.Revision, ContentHash: bytes.Clone(validation.ContentHash)}
}

func preparationReference(prep *tammyv1.BasPreparation) *tammyv1.SourceRef {
	if prep == nil {
		return nil
	}
	return &tammyv1.SourceRef{Type: "bas_preparation", Id: prep.PreparationId, Revision: prep.Version, ContentHash: bytes.Clone(prep.ContentHash)}
}

func calculationReference(calculation *tammyv1.BasCalculation) *tammyv1.SourceRef {
	if calculation == nil {
		return nil
	}
	return &tammyv1.SourceRef{Type: "bas_calculation", Id: calculation.CalculationId, Revision: calculation.Version, ContentHash: bytes.Clone(calculation.ContentHash)}
}

func expectedPreparation(prep *tammyv1.BasPreparation) *tammyv1.BasExpectedIdentity {
	if prep == nil {
		return &tammyv1.BasExpectedIdentity{ExpectedState: &tammyv1.BasExpectedIdentity_Absent{Absent: true}}
	}
	return &tammyv1.BasExpectedIdentity{ExpectedState: &tammyv1.BasExpectedIdentity_Retained{Retained: preparationReference(prep)}, ExpectedVersion: uint64Pointer(prep.Version)}
}

func expectedBranch(branch *Branch) *tammyv1.BasExpectedIdentity {
	if branch == nil {
		return &tammyv1.BasExpectedIdentity{ExpectedState: &tammyv1.BasExpectedIdentity_Absent{Absent: true}}
	}
	return &tammyv1.BasExpectedIdentity{ExpectedState: &tammyv1.BasExpectedIdentity_Retained{Retained: cloneRef(branch.Reference)}, ExpectedVersion: uint64Pointer(branch.Version)}
}

func activeAttempt(branch *Branch) *tammyv1.BasPreparation {
	if branch == nil {
		return nil
	}
	return branch.ActiveAttempt
}

func cloneRef(ref *tammyv1.SourceRef) *tammyv1.SourceRef {
	if ref == nil {
		return nil
	}
	return proto.Clone(ref).(*tammyv1.SourceRef)
}

func sameRef(left, right *tammyv1.SourceRef) bool {
	return left != nil && right != nil && left.Type == right.Type && left.Id == right.Id && left.Revision == right.Revision && bytes.Equal(left.ContentHash, right.ContentHash)
}

func uint64Pointer(value uint64) *uint64 { return &value }

func samePeriodIdentity(left, right *tammyv1.SourceRef) bool {
	return validRef(left, "bas_period") && validRef(right, "bas_period") && left.Id == right.Id && left.Revision == right.Revision && bytes.Equal(left.ContentHash, right.ContentHash)
}

func sameSourceContent(left, right []byte) bool { return bytes.Equal(left, right) }

func samePreparationAggregate(left, right *tammyv1.BasPreparation) bool {
	return left != nil && right != nil && left.PreparationId == right.PreparationId && left.Version == right.Version && bytes.Equal(left.ContentHash, right.ContentHash)
}

func samePreparationIdentity(left, right *tammyv1.BasPreparation) bool {
	return left != nil && right != nil && left.PreparationId == right.PreparationId && left.OrganisationId == right.OrganisationId && sameRef(left.Family, right.Family)
}

func scopeForChainPeriod(chain ChainSnapshot) FamilyScope {
	return chain.Family.Scope
}

func validUUIDv7(value string) bool { return uuidV7Pattern.MatchString(value) }

func validTimestamp(value interface{ CheckValid() error }) bool {
	return value != nil && value.CheckValid() == nil
}
