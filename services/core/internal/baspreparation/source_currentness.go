package baspreparation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/organisations"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"time"
)

type BASRegenerationCallback func(
	context.Context,
	*tammyv1.RegenerateBasPreparationRequest,
	string,
	*timestamppb.Timestamp,
) (*RegenerationResult, error)

type BASCurrentnessCapture func(
	context.Context,
	*PreparationRetainedSnapshot,
	*tammyv1.BasCalculation,
) (CurrentCapture, error)

type BASPreparationWorkflowAdvance func(context.Context, string, *tammyv1.SourceRef, *tammyv1.SourceRef, *tammyv1.SourceRef, *timestamppb.Timestamp) error

type BASCommandCaptureCallbacks struct {
	CreateExternalAttempt BASExternalAttemptCreator
	Regenerate            BASRegenerationCallback
	CaptureCurrent        BASCurrentnessCapture
	AdvancePreparation    BASPreparationWorkflowAdvance
}

func NewBASCommandCapture(
	executor app.CommandSQLExecutor,
	cursors *paging.Codec,
	now func() time.Time,
	newID func() (string, error),
) (BASCommandCaptureCallbacks, error) {
	handler, err := newBASRegenerationHandler(executor, cursors, now, newID)
	if err != nil {
		return BASCommandCaptureCallbacks{}, err
	}
	return BASCommandCaptureCallbacks{
		Regenerate:            handler.Regenerate,
		CreateExternalAttempt: handler.CreateExternalAttempt,
		CaptureCurrent:        handler.CaptureCurrent,
		AdvancePreparation: func(ctx context.Context, org string, family, prior, next *tammyv1.SourceRef, at *timestamppb.Timestamp) error {
			return advanceBASActiveAttempt(ctx, org, family, prior, next, at, handler.preparations, handler.familyStates, handler.branchStates)
		},
	}, nil
}

func NewBASRegenerationHandler(
	executor app.CommandSQLExecutor,
	cursors *paging.Codec,
	now func() time.Time,
	newID func() (string, error),
) (BASRegenerationCallback, error) {
	callbacks, err := NewBASCommandCapture(executor, cursors, now, newID)
	if err != nil {
		return nil, err
	}
	return callbacks.Regenerate, nil
}

// Shared output from the one read-only source-capture composition.
type basRegenerationSourceSnapshot struct {
	capture        *BASBoundedSourceRecordCapture
	coverage       *BASSourceCoverageResult
	binding        *BASSourceSnapshotBindingResult
	fingerprints   basRegenerationFingerprints
	revisionVector *tammyv1.BasSourceRevisionVector
}

func (handler *basRegenerationHandler) captureSourceSnapshot(
	ctx context.Context,
	organisation *tammyv1.Organisation,
	profile *tammyv1.BasObligationsProfile,
	period *tammyv1.BasConfirmedPeriod,
	paygw *tammyv1.BasPaygwInputs,
	paygi *tammyv1.BasPaygiInputs,
	inputRevision uint64,
	inputHash []byte,
	asOf time.Time,
) (*basRegenerationSourceSnapshot, error) {
	fail := func(err error) (*basRegenerationSourceSnapshot, error) {
		if ctx != nil && ctx.Err() != nil {
			err = errors.Join(err, ctx.Err())
		}
		return nil, errors.Join(ErrSourceReservation, err)
	}
	if handler == nil || ctx == nil || organisation == nil || profile == nil || period == nil ||
		paygw == nil || paygi == nil || inputRevision == 0 || len(inputHash) != sha256.Size ||
		asOf.IsZero() || handler.revisions == nil || handler.sourcePorts.Invoices == nil ||
		handler.sourcePorts.Purchases == nil || handler.sourcePorts.Accounting == nil ||
		handler.sourcePorts.Rules == nil || !ids.IsCanonicalV7(organisation.Id) ||
		organisation.ActiveTaxRuleBundle == nil || profile.OrganisationId != organisation.Id ||
		period.OrganisationId != organisation.Id || period.Confirmation == nil ||
		period.Confirmation.Period == nil || profile.Profile == nil || profile.Profile.Gst == nil {
		return fail(nil)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}

	revisionsNow, err := handler.revisions.Current(ctx)
	if err != nil {
		return fail(err)
	}
	revisionVector := &tammyv1.BasSourceRevisionVector{
		FinancialRevision:           revisionsNow.Financial,
		LedgerRevision:              revisionsNow.Ledger,
		SettlementRevision:          revisionsNow.Settlement,
		BankingRevision:             revisionsNow.Banking,
		TaxSourceRevision:           revisionsNow.TaxSource,
		OrganisationProfileRevision: revisionsNow.OrganisationProfile,
		RuleBundleRevision:          revisionsNow.RuleBundle,
		PaygRevision:                inputRevision,
		BasProfileRevision:          profile.Revision,
		PeriodRevision:              period.Version,
	}
	workspaceAsOf, err := basWorkspaceAsOf(asOf)
	if err != nil {
		return fail(err)
	}

	capture, err := CaptureBASFinancialSourceRecords(ctx, BASFinancialSourceRequest{
		Source: BASBoundedSourceRequest{
			OrganisationID:       organisation.Id,
			ConfirmedPeriod:      period,
			ActiveRuleBundle:     cloneBASRef(organisation.ActiveTaxRuleBundle),
			MaximumPayloadBytes:  maximumBASSourcePayloadBytes,
			MaximumCandidateRows: maximumBASRecordCandidates,
		},
		GstBasis: organisation.GstBasis,
		AsOf:     workspaceAsOf,
	}, handler.sourcePorts)
	if err != nil || capture == nil || capture.PayloadReservation == nil {
		return fail(err)
	}
	coverage, err := BuildBASSourceCoverage(ctx, capture)
	if err != nil || coverage == nil || len(coverage.ManifestHash) != sha256.Size {
		return fail(err)
	}
	reserved := capture.PayloadReservation.ReservedPayloadBytes
	if reserved < 0 || reserved > maximumBASSourcePayloadBytes {
		return fail(ErrSnapshotBound)
	}
	// Bind charges source records, coverage manifest, and the exact retained
	// binding protos against one ceiling. The producer's source bytes are already
	// reserved, so provide only the remaining amount.
	binding, err := BindBASSourceSnapshot(ctx, capture, BASSourceSnapshotBinding{
		Organisation:   organisation,
		Profile:        profile,
		Period:         period,
		PaygwInputs:    paygw,
		PaygiInputs:    paygi,
		RevisionVector: revisionVector,
	}, coverage, maximumBASSourcePayloadBytes-reserved)
	if err != nil || binding == nil || len(binding.SourceSnapshotHash) != sha256.Size ||
		len(binding.RevisionVectorHash) != sha256.Size {
		return fail(err)
	}
	fingerprints, err := computeBASRegenerationFingerprints(capture.RuleBundle)
	if err != nil {
		return fail(err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	return &basRegenerationSourceSnapshot{
		capture: capture, coverage: coverage, binding: binding,
		fingerprints: fingerprints, revisionVector: revisionVector,
	}, nil
}

func basWorkspaceAsOf(instant time.Time) (time.Time, error) {
	if instant.IsZero() {
		return time.Time{}, ErrSourceReservation
	}
	location, err := time.LoadLocation(organisations.WorkspaceTimezone)
	if err != nil {
		return time.Time{}, errors.Join(ErrSourceReservation, err)
	}
	return instant.In(location), nil
}

// CaptureCurrent has no writes, generated IDs, workpaper creation, or revision
// bumps. It resolves the latest exact period/profile/organisation and current
// saved PAYG pair on the command owner's transaction, then runs the same
// source-summary, reservation, full-capture, coverage, binding, and
// implementation-fingerprint path as regeneration.
func (handler *basRegenerationHandler) CaptureCurrent(
	ctx context.Context,
	owner *PreparationRetainedSnapshot,
	calculation *tammyv1.BasCalculation,
) (CurrentCapture, error) {
	fail := func(err error) (CurrentCapture, error) {
		if ctx != nil && ctx.Err() != nil {
			err = errors.Join(err, ctx.Err())
		}
		if err == nil {
			err = ErrCommandConflict
		}
		return CurrentCapture{}, err
	}
	if handler == nil || ctx == nil || owner == nil || owner.Preparation == nil || calculation == nil ||
		handler.periods == nil || handler.profiles == nil || handler.organisations == nil ||
		handler.savedPayg == nil || handler.revisions == nil || handler.now == nil ||
		handler.sourcePorts.Invoices == nil || handler.sourcePorts.Purchases == nil ||
		handler.sourcePorts.Accounting == nil || handler.sourcePorts.Rules == nil {
		return fail(nil)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	prep := owner.Preparation
	if !ids.IsCanonicalV7(prep.OrganisationId) || !ids.IsCanonicalV7(prep.PreparationId) ||
		prep.CurrentCalculation == nil || !sameBASSourceRef(prep.CurrentCalculation, calculationReference(calculation)) ||
		calculation.Preparation == nil || calculation.Preparation.Id != prep.PreparationId ||
		calculation.Preparation.Revision >= prep.Version ||
		!sameBASSourceRef(calculation.Family, prep.Family) ||
		!sameBASSourceRef(calculation.Profile, prep.Profile) ||
		!sameBASSourceRef(calculation.Period, prep.Period) ||
		calculation.InputRevision != prep.InputRevision || !bytes.Equal(calculation.InputHash, prep.InputHash) ||
		owner.SavedPaygInputs == nil || calculation.RevisionVector == nil {
		return fail(nil)
	}

	pinnedPeriod, err := handler.periods.GetExact(ctx, prep.OrganisationId, prep.Period)
	if err != nil || pinnedPeriod == nil {
		return fail(err)
	}
	currentPeriod, _, err := handler.periods.CurrentForScope(ctx, prep.OrganisationId, pinnedPeriod.Confirmation)
	if err != nil || currentPeriod == nil {
		return fail(err)
	}
	currentProfile, currentProfileRef, err := handler.profiles.GetEffectiveForPeriod(ctx,
		prep.OrganisationId, currentPeriod.Confirmation.Period)
	if err != nil || currentProfile == nil ||
		!sameBASSourceRef(currentPeriod.Confirmation.Profile, currentProfileRef) {
		return fail(err)
	}
	organisation, err := handler.organisations.GetOrganisation(ctx, prep.OrganisationId)
	if err != nil || organisation == nil || organisation.ActiveTaxRuleBundle == nil {
		return fail(err)
	}

	saved, err := handler.savedPayg.GetExact(ctx, prep.OrganisationId, owner.SavedPaygInputs)
	if err != nil || saved == nil || saved.PreparationId != prep.PreparationId ||
		saved.InputRevision != prep.InputRevision || !bytes.Equal(saved.InputHash, prep.InputHash) {
		return fail(err)
	}
	actualInputHash, err := hashPaygInputs(saved.PaygwInputs, saved.PaygiInputs)
	if err != nil || !bytes.Equal(actualInputHash, prep.InputHash) {
		return fail(err)
	}
	source, err := handler.captureSourceSnapshot(ctx, organisation, currentProfile, currentPeriod,
		saved.PaygwInputs, saved.PaygiInputs, saved.InputRevision, actualInputHash, handler.now().UTC())
	if err != nil || source == nil {
		return fail(err)
	}
	if !basRegenerationFingerprintsMatchCalculation(source.fingerprints, calculation) {
		return fail(nil)
	}
	return CurrentCapture{
		SourceSnapshotHash: bytes.Clone(source.binding.SourceSnapshotHash),
		InputRevision:      saved.InputRevision,
		InputHash:          bytes.Clone(actualInputHash),
		RevisionVector:     proto.Clone(source.revisionVector).(*tammyv1.BasSourceRevisionVector),
	}, nil
}

func basRegenerationFingerprintsMatchCalculation(
	value basRegenerationFingerprints,
	calculation *tammyv1.BasCalculation,
) bool {
	return calculation != nil &&
		calculation.RuleVersion == value.ruleVersion && bytes.Equal(calculation.RuleHash, value.ruleHash) &&
		calculation.CalculatorVersion == value.calculatorVersion && bytes.Equal(calculation.CalculatorHash, value.calculatorHash) &&
		calculation.FormVersion == value.formVersion && bytes.Equal(calculation.FormHash, value.formHash) &&
		calculation.SchemaVersion == value.schemaVersion && bytes.Equal(calculation.SchemaHash, value.schemaHash) &&
		calculation.DescriptorVersion == value.descriptorVersion && bytes.Equal(calculation.DescriptorHash, value.descriptorHash)
}
