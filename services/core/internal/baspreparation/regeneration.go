package baspreparation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"math"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/app"
	"github.com/tammyapp/tammy/services/core/internal/artefacts"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/invoices"
	"github.com/tammyapp/tammy/services/core/internal/organisations"
	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/reporting"
	"github.com/tammyapp/tammy/services/core/internal/revisions"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// NewBASRegenerationHandler binds all reader and writer repositories to the
// transaction capability supplied by the existing ordinary-command
// coordinator. The returned callback has the exact CommandRepositories
// signature used by Service.RegenerateBasPreparation.
func newBASRegenerationHandler(
	executor app.CommandSQLExecutor,
	cursors *paging.Codec,
	now func() time.Time,
	newID func() (string, error),
) (*basRegenerationHandler, error) {
	if executor == nil || cursors == nil || now == nil || newID == nil {
		return nil, ErrInvalidCommand
	}
	profiles, err := NewProfileRepository(executor, cursors)
	if err != nil {
		return nil, err
	}
	periods, err := NewPeriodRepository(executor, cursors)
	if err != nil {
		return nil, err
	}
	preparations, err := NewPreparationRepository(executor, cursors)
	if err != nil {
		return nil, err
	}
	savedPayg, err := NewSavedPaygRepository(executor, cursors)
	if err != nil {
		return nil, err
	}
	calculations, err := NewCalculationRepository(executor, cursors)
	if err != nil {
		return nil, err
	}
	familyIdentities, err := NewFamilyIdentityRepository(executor)
	if err != nil {
		return nil, err
	}
	familyStates, err := NewFamilyStateRepository(executor, cursors)
	if err != nil {
		return nil, err
	}
	branchIdentities, err := NewBranchIdentityRepository(executor, cursors)
	if err != nil {
		return nil, err
	}
	branchStates, err := NewBranchStateRepository(executor, cursors)
	if err != nil {
		return nil, err
	}
	invoicePort, err := invoices.NewReportingPort(executor)
	if err != nil {
		return nil, err
	}
	purchaseRepository, err := purchases.NewRepository(executor, now, newID)
	if err != nil {
		return nil, err
	}
	journalRepository, err := accounting.NewJournalRepository(executor)
	if err != nil {
		return nil, err
	}
	ruleRepository, err := artefacts.NewRepository(executor)
	if err != nil {
		return nil, err
	}
	organisationRepository, err := organisations.NewRepository(executor)
	if err != nil {
		return nil, err
	}
	workpapers, err := reporting.NewRepositoryWithClock(executor, now, purchaseRepository)
	if err != nil {
		return nil, err
	}
	revisionRepository, err := revisions.NewWithExecutor(executor.ExecContext,
		func(ctx context.Context, query string, args ...any) revisions.RowScanner {
			return basQueryRow{executor: executor, ctx: ctx, query: query, args: args}
		}, clock.Func(now))
	if err != nil {
		return nil, err
	}
	return &basRegenerationHandler{
		profiles: profiles, periods: periods, preparations: preparations, savedPayg: savedPayg,
		calculations: calculations, familyIdentities: familyIdentities, familyStates: familyStates,
		branchIdentities: branchIdentities, branchStates: branchStates, organisations: organisationRepository,
		workpapers: workpapers, revisions: revisionRepository,
		sourcePorts: BASBoundedSourcePorts{Invoices: invoicePort, Purchases: purchaseRepository,
			Accounting: journalRepository, Rules: ruleRepository},
		now: now, newID: newID,
	}, nil
}

type basRegenerationHandler struct {
	profiles         *ProfileRepository
	periods          *PeriodRepository
	preparations     *PreparationRepository
	savedPayg        *SavedPaygRepository
	calculations     *CalculationRepository
	familyIdentities *FamilyIdentityRepository
	familyStates     *FamilyStateRepository
	branchIdentities *BranchIdentityRepository
	branchStates     *BranchStateRepository
	organisations    *organisations.Repository
	workpapers       *reporting.Repository
	revisions        *revisions.Repository
	sourcePorts      BASBoundedSourcePorts
	now              func() time.Time
	newID            func() (string, error)
}

// Regenerate computes every source and deterministic projection first, then
// appends the immutable records and successor pointers in the existing
// serializable transaction. Any error leaves rollback to the command owner.
func (handler *basRegenerationHandler) Regenerate(
	ctx context.Context,
	request *tammyv1.RegenerateBasPreparationRequest,
	actor string,
	at *timestamppb.Timestamp,
) (*RegenerationResult, error) {
	fail := func(err error) (*RegenerationResult, error) {
		if ctx != nil && ctx.Err() != nil {
			err = errors.Join(err, ctx.Err())
		}
		if err == nil {
			err = ErrInvalidCommand
		}
		return nil, err
	}
	if handler == nil || ctx == nil || request == nil || request.CommandContext == nil || !ids.IsCanonicalV7(request.OrganisationId) ||
		!ids.IsCanonicalV7(actor) || at == nil || !at.IsValid() || request.ExpectedVersion >= math.MaxInt64 ||
		handler.profiles == nil || handler.periods == nil || handler.preparations == nil ||
		handler.savedPayg == nil || handler.calculations == nil || handler.familyIdentities == nil ||
		handler.familyStates == nil || handler.branchIdentities == nil || handler.branchStates == nil ||
		handler.organisations == nil || handler.workpapers == nil || handler.revisions == nil {
		return fail(nil)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}

	// Resolve exact current scope owners before any financial source capture.
	period, err := handler.periods.GetExact(ctx, request.OrganisationId, request.Period)
	if err != nil {
		return fail(err)
	}
	currentPeriod, currentPeriodRef, err := handler.periods.CurrentForScope(ctx, request.OrganisationId, period.Confirmation)
	if err != nil || currentPeriod == nil || !sameBASSourceRef(currentPeriodRef, request.Period) ||
		!sameBASSourceRef(&tammyv1.SourceRef{Type: periodSnapshotSpec.resource, Id: period.PeriodId,
			Revision: period.Version, ContentHash: period.ContentHash}, request.Period) {
		return fail(errors.Join(ErrCommandConflict, err))
	}
	profile, profileRef, err := handler.profiles.GetEffectiveForPeriod(ctx, request.OrganisationId, period.Confirmation.Period)
	if err != nil || profile == nil || !sameBASSourceRef(profileRef, request.Profile) ||
		!sameBASSourceRef(period.Confirmation.Profile, request.Profile) {
		return fail(errors.Join(ErrCommandConflict, err))
	}
	organisation, err := handler.organisations.GetOrganisation(ctx, request.OrganisationId)
	if err != nil || organisation == nil || organisation.ActiveTaxRuleBundle == nil ||
		(profile.Profile == nil || profile.Profile.Gst == nil) {
		return fail(errors.Join(ErrCommandConflict, err))
	}
	familyScope, err := FamilyScopeFromPeriod(period)
	if err != nil {
		return fail(err)
	}
	family, familyFound, err := handler.familyIdentities.FindByScope(ctx, familyScope)
	if err != nil {
		return fail(err)
	}
	var familyRef *tammyv1.SourceRef
	var familyState *tammyv1.BasFamilyState
	var familyStateRef *tammyv1.SourceRef
	var current *PreparationRetainedSnapshot
	var currentRef *tammyv1.SourceRef
	var branch *tammyv1.BasBranchState
	var branchRef *tammyv1.SourceRef
	var branchIdentity *tammyv1.BasBranchIdentity
	if familyFound {
		familyRef = basFamilyIdentityReference(family)
		if !basExpectedIdentityMatches(request.FamilyExpectation, familyRef) {
			return fail(ErrCommandConflict)
		}
		familyState, familyFound, err = handler.familyStates.loadCurrent(ctx, request.OrganisationId, familyRef)
		if err != nil || !familyFound {
			return fail(errors.Join(ErrSnapshotCorrupt, err))
		}
		familyStateRef = familyStateReference(familyState)
		head := familyState.RootPreparation
		if confirmedHead := familyState.LatestHead.GetRetained(); confirmedHead != nil {
			head = confirmedHead
		}
		if head == nil {
			return fail(ErrSnapshotCorrupt)
		}
		// A branch attempt is the current working preparation. LatestHead remains
		// the confirmed effective head and must never be used as a replacement for
		// this active attempt.
		if activeBranch := familyState.ActiveBranch.GetRetained(); activeBranch != nil {
			branchRef = cloneBASRef(activeBranch)
			branchIdentity, err = handler.branchIdentities.GetExact(ctx, request.OrganisationId, branchRef)
			if err != nil || branchIdentity == nil || !sameBASSourceRef(branchIdentity.Family, familyRef) {
				return fail(errors.Join(ErrSnapshotCorrupt, err))
			}
			branch, err = handler.branchStates.getVersion(ctx, request.OrganisationId, branchRef,
				familyState.ActiveBranch.GetExpectedVersion())
			if err != nil || branch == nil || !basBranchAllowsMutableAttempt(branch, familyState) || branch.ActiveAttempt.GetRetained() == nil {
				return fail(errors.Join(ErrCommandConflict, err))
			}
			head = branch.ActiveAttempt.GetRetained()
		}
		current, err = handler.preparations.GetCurrent(ctx, request.OrganisationId, head.Id)
		if err != nil || current == nil ||
			(familyState.ActiveBranch.GetRetained() != nil && !sameBASSourceRef(preparationReference(current.Preparation), head)) ||
			(familyState.ActiveBranch.GetRetained() == nil && familyState.LatestHead.GetRetained() != nil && !sameBASSourceRef(preparationReference(current.Preparation), head)) ||
			!sameBASSourceRef(current.Preparation.Family, familyRef) {
			return fail(errors.Join(ErrSnapshotCorrupt, err))
		}
		currentRef = preparationReference(current.Preparation)
		if branchRef == nil && current.BranchID != nil || branchRef != nil &&
			(current.BranchID == nil || *current.BranchID != branchRef.Id || !sameBASSourceRef(branch.ActiveAttempt.GetRetained(), currentRef)) {
			return fail(ErrSnapshotCorrupt)
		}
		if !basExpectedIdentityMatches(request.PreparationExpectation, currentRef) ||
			request.ExpectedVersion != current.Preparation.Version {
			return fail(ErrCommandConflict)
		}
		if current.Preparation.FrozenBas != nil || current.Preparation.SupersededBy != nil ||
			current.Preparation.State == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF ||
			current.Preparation.State == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED ||
			current.Preparation.State == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION ||
			current.Preparation.State == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_AFTER_RECORDED_REJECTION ||
			current.Preparation.State == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BY_CONFIRMED_REVISION {
			return fail(ErrCommandConflict)
		}
	} else {
		if !basExpectedIdentityMatches(request.FamilyExpectation, nil) ||
			!basExpectedIdentityMatches(request.PreparationExpectation, nil) || request.ExpectedVersion != 0 {
			return fail(ErrCommandConflict)
		}
	}
	if !familyFound && current != nil || familyFound && family == nil {
		return fail(ErrSnapshotCorrupt)
	}

	// Carry the exact active-attempt lineage through every preparation
	// successor. Confirmed LatestHead and immutable RootPreparation stay pinned.
	var branchID *string
	attemptID := ""
	if current != nil {
		branchID = cloneBASString(current.BranchID)
		attemptID = current.AttemptID
		if (branchRef == nil && branchID != nil) || (branchRef != nil && (branchID == nil || *branchID != branchRef.Id)) {
			return fail(ErrSnapshotCorrupt)
		}
	}
	if attemptID == "" {
		attemptID, err = basRegenerationNewID(handler.newID)
		if err != nil {
			return fail(err)
		}
	}

	// Reuse the reviewable PAYG pair exactly; an absent pair is valid only for
	// the initial unknown pair and is materialized below as its own envelope.
	paygw, paygi := basInitialUnknownPaygInputs()
	var savedInputRef *tammyv1.SourceRef
	inputRevision := uint64(1)
	if current != nil {
		inputRevision = current.Preparation.InputRevision
		if current.SavedPaygInputs != nil {
			saved, err := handler.savedPayg.GetExact(ctx, request.OrganisationId, current.SavedPaygInputs)
			if err != nil || saved == nil {
				return fail(errors.Join(ErrSnapshotCorrupt, err))
			}
			paygw, paygi = proto.Clone(saved.PaygwInputs).(*tammyv1.BasPaygwInputs),
				proto.Clone(saved.PaygiInputs).(*tammyv1.BasPaygiInputs)
			savedInputRef = cloneBASRef(current.SavedPaygInputs)
		} else {
			defaultHash, hashErr := hashPaygInputs(paygw, paygi)
			if hashErr != nil || !bytes.Equal(defaultHash, current.Preparation.InputHash) {
				return fail(errors.Join(ErrSnapshotCorrupt, hashErr))
			}
		}
	}
	inputHash, err := hashPaygInputs(paygw, paygi)
	if err != nil {
		return fail(err)
	}
	if current != nil && (!bytes.Equal(inputHash, current.Preparation.InputHash) || inputRevision != current.Preparation.InputRevision) {
		return fail(ErrSnapshotCorrupt)
	}

	source, err := handler.captureSourceSnapshot(ctx, organisation, profile, period, paygw, paygi, inputRevision, inputHash, at.AsTime())
	if err != nil || source == nil {
		return fail(err)
	}
	capture, coverage, binding, fingerprints, revisionVector := source.capture, source.coverage, source.binding, source.fingerprints, source.revisionVector

	workpaperID, err := basRegenerationNewID(handler.newID)
	if err != nil {
		return fail(err)
	}
	workpaper, legacyPurchaseVersions, err := buildCapturedBASWorkpaper(
		ctx, capture, organisation, period, workpaperID, at, binding.SourceSnapshotHash)
	if err != nil || workpaper == nil {
		return fail(err)
	}
	gst := profile.Profile.Gst
	var g1, oneA, oneB *tammyv1.BasAmountInput
	if gst.Applicability != tammyv1.BasApplicability_BAS_APPLICABILITY_NOT_APPLICABLE {
		g1 = basKnownAUDAmount(workpaper.SalesG1.MinorUnits)
		oneA = basKnownAUDAmount(workpaper.GstOnSales_1A.MinorUnits)
		oneB = basKnownAUDAmount(workpaper.GstCredits_1B.MinorUnits)
	}
	calculationResult, calculationTemplates := ConvertBasCalculationInputs(
		profile, period, paygw, paygi, g1, oneA, oneB)
	if calculationResult == nil {
		return fail(ErrSnapshotCorrupt)
	}
	hasUnresolved, hasUnsupported := basCoverageBlocksCompletion(coverage.Coverage)
	if hasUnsupported {
		calculationResult.Outcome = tammyv1.BasCalculationOutcome_BAS_CALCULATION_OUTCOME_UNSUPPORTED
	} else if hasUnresolved && calculationResult.Outcome != tammyv1.BasCalculationOutcome_BAS_CALCULATION_OUTCOME_UNSUPPORTED {
		calculationResult.Outcome = tammyv1.BasCalculationOutcome_BAS_CALCULATION_OUTCOME_INCOMPLETE
	}
	if (gst.GstBasis == tammyv1.BasGstBasis_BAS_GST_BASIS_CASH && organisation.GstBasis != tammyv1.GstBasis_GST_BASIS_CASH) ||
		(gst.GstBasis == tammyv1.BasGstBasis_BAS_GST_BASIS_ACCRUAL && organisation.GstBasis != tammyv1.GstBasis_GST_BASIS_NON_CASH) {
		calculationTemplates = append(calculationTemplates, CalculationFindingTemplate{
			Severity:    tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_BLOCKER,
			StableCode:  "GST_BASIS_SOURCE_CONFLICT",
			SafeMessage: "The confirmed GST profile basis differs from the organisation's current source attribution basis.",
		})
		if calculationResult.Outcome != tammyv1.BasCalculationOutcome_BAS_CALCULATION_OUTCOME_UNSUPPORTED {
			calculationResult.Outcome = tammyv1.BasCalculationOutcome_BAS_CALCULATION_OUTCOME_INCOMPLETE
		}
	}
	if gst.Applicability == tammyv1.BasApplicability_BAS_APPLICABILITY_NOT_APPLICABLE &&
		(workpaper.SalesG1.MinorUnits != 0 || workpaper.GstOnSales_1A.MinorUnits != 0 ||
			workpaper.GstCredits_1B.MinorUnits != 0) {
		calculationTemplates = append(calculationTemplates, CalculationFindingTemplate{
			Severity:    tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_BLOCKER,
			StableCode:  "GST_PROFILE_SOURCE_CONFLICT",
			SafeMessage: "Retained financial sources contain GST effects while the confirmed profile marks GST as not applicable.",
		})
		calculationResult.Outcome = tammyv1.BasCalculationOutcome_BAS_CALCULATION_OUTCOME_INCOMPLETE
	}
	calculationID, err := basRegenerationNewID(handler.newID)
	if err != nil {
		return fail(err)
	}
	findings, err := basMaterializeCalculationFindings(ctx, handler.newID, coverage.FindingTemplates, calculationTemplates)
	if err != nil {
		return fail(err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}

	// Allocate all stable IDs before the first write. The surrounding
	// coordinator still owns rollback if any subsequent append fails.
	var familyID, preparationID string
	if !familyFound {
		familyID, err = basRegenerationNewID(handler.newID)
		if err != nil {
			return fail(err)
		}
		preparationID, err = basRegenerationNewID(handler.newID)
		if err != nil {
			return fail(err)
		}
	}
	var savedID string
	if savedInputRef == nil {
		savedID, err = basRegenerationNewID(handler.newID)
		if err != nil {
			return fail(err)
		}
	}
	if preparationID == "" {
		preparationID = current.Preparation.PreparationId
	}

	now := at.AsTime().UTC()
	periodRef := confirmedPeriodReference(period)
	profileRef = profileReference(profile)
	if familyRef == nil {
		family = &tammyv1.BasFamilyIdentity{
			FamilyId: familyID, OrganisationId: request.OrganisationId,
			PeriodScope: proto.Clone(period.Confirmation.Period).(*tammyv1.BasCivilDateRange),
			Window:      period.Confirmation.Window, CreatedAt: timestamppb.New(now),
		}
		familyRef = basFamilyIdentityReference(family)
	}

	var previousRef *tammyv1.SourceRef
	if currentRef != nil {
		previousRef = cloneBASRef(currentRef)
	}
	var base *PreparationRetainedSnapshot
	var baseRef *tammyv1.SourceRef
	var rootRef *tammyv1.SourceRef
	if current == nil {
		root := &tammyv1.BasPreparation{
			PreparationId: preparationID, OrganisationId: request.OrganisationId, Version: 1,
			Family: cloneBASRef(familyRef), Period: cloneBASRef(periodRef), Profile: cloneBASRef(profileRef),
			State:              tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING,
			DisputeOverlay:     tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_NONE,
			SourceSnapshotHash: bytes.Clone(binding.SourceSnapshotHash),
			CreatedAt:          timestamppb.New(now), UpdatedAt: timestamppb.New(now),
			InputRevision: 1, InputHash: bytes.Clone(inputHash),
		}
		base = &PreparationRetainedSnapshot{Preparation: root, AttemptID: attemptID}
	} else {
		next := proto.Clone(current.Preparation).(*tammyv1.BasPreparation)
		if next.Version >= math.MaxInt64 {
			return fail(ErrCommandConflict)
		}
		next.Version++
		next.ContentHash = nil
		next.Profile = cloneBASRef(profileRef)
		next.Period = cloneBASRef(periodRef)
		next.SourceSnapshotHash = bytes.Clone(binding.SourceSnapshotHash)
		next.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING
		next.CurrentCalculation, next.CurrentValidation, next.FrozenBas = nil, nil, nil
		next.SupersededBy = nil
		next.UpdatedAt = timestamppb.New(now)
		base = &PreparationRetainedSnapshot{
			Preparation: next, SavedPaygInputs: cloneBASRef(savedInputRef),
			BranchID: cloneBASString(current.BranchID), AttemptID: current.AttemptID,
		}
	}

	// The first retained preparation is the family's immutable root. The
	// stable family state starts with that root and explicit absent head/branch.
	if !familyFound {
		familyRef, err = handler.familyIdentities.Append(ctx, family)
		if err != nil {
			return fail(err)
		}
		base.Preparation.Family = cloneBASRef(familyRef)
		rootRef, err = handler.preparations.Append(ctx, base, nil)
		if err != nil {
			return fail(err)
		}
		base.Preparation.ContentHash = bytes.Clone(rootRef.ContentHash)
		baseRef = cloneBASRef(rootRef)
		initialState := &tammyv1.BasFamilyState{
			Family: cloneBASRef(familyRef), Version: 1, RootPreparation: cloneBASRef(rootRef),
			LatestHead: expectedPreparation(nil), ActiveBranch: basExpectedBranchIdentity(nil, 0),
			DisputeOverlay: tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_NONE,
			UpdatedAt:      timestamppb.New(now),
		}
		familyStateRef, err = handler.familyStates.Append(ctx, request.OrganisationId, initialState, nil)
		if err != nil {
			return fail(err)
		}
		initialState.ContentHash = bytes.Clone(familyStateRef.ContentHash)
		familyState, familyStateRef = initialState, cloneBASRef(familyStateRef)
	} else {
		baseRef, err = handler.preparations.Append(ctx, base, currentRef)
		if err != nil {
			return fail(err)
		}
		base.Preparation.ContentHash = bytes.Clone(baseRef.ContentHash)
	}

	// A never-reviewed initial pair is retained as an exact immutable PAYG
	// envelope. Existing pairs are reused verbatim and never replaced.
	if savedInputRef == nil {
		saved := &tammyv1.BasSavedPaygInputs{
			InputSnapshotId: savedID, OrganisationId: request.OrganisationId,
			PreparationId: preparationID, Preparation: cloneBASRef(baseRef),
			InputRevision: base.Preparation.InputRevision, InputHash: bytes.Clone(base.Preparation.InputHash),
			PaygwInputs: proto.Clone(paygw).(*tammyv1.BasPaygwInputs),
			PaygiInputs: proto.Clone(paygi).(*tammyv1.BasPaygiInputs),
			ActorUserId: actor, RecordedAt: timestamppb.New(now),
		}
		savedInputRef, err = handler.savedPayg.Append(ctx, saved)
		if err != nil {
			return fail(err)
		}
		next := proto.Clone(base.Preparation).(*tammyv1.BasPreparation)
		next.Version++
		next.ContentHash = nil
		next.UpdatedAt = timestamppb.New(now)
		withInputs := &PreparationRetainedSnapshot{
			Preparation: next, SavedPaygInputs: cloneBASRef(savedInputRef),
			BranchID: cloneBASString(base.BranchID), AttemptID: base.AttemptID,
		}
		baseRef, err = handler.preparations.Append(ctx, withInputs, baseRef)
		if err != nil {
			return fail(err)
		}
		next.ContentHash = bytes.Clone(baseRef.ContentHash)
		base = withInputs
	}

	workpaperCapture := reporting.BASDraftCapture{
		Workpaper: workpaper, LegacyPurchaseVersions: legacyPurchaseVersions,
	}
	savedWorkpaper, err := handler.workpapers.SaveCapturedBASDraft(ctx,
		request.CommandContext.IdempotencyKey, workpaperCapture)
	if err != nil || savedWorkpaper == nil {
		return fail(err)
	}
	workpaperHash, err := reporting.BASWorkpaperContentHash(savedWorkpaper)
	if err != nil || len(workpaperHash) != sha256.Size {
		return fail(err)
	}
	workpaperRef := &tammyv1.SourceRef{Type: "bas_workpaper", Id: savedWorkpaper.Id,
		Revision: savedWorkpaper.Version, ContentHash: bytes.Clone(workpaperHash)}
	organisationWire, err := (proto.MarshalOptions{Deterministic: true}).Marshal(organisation)
	if err != nil {
		return fail(err)
	}
	organisationHash := sha256.Sum256(organisationWire)
	calculation := &tammyv1.BasCalculation{
		CalculationId: calculationID, OrganisationId: request.OrganisationId, Version: 1,
		Preparation: cloneBASRef(baseRef), Family: cloneBASRef(familyRef),
		Period: cloneBASRef(periodRef), Profile: cloneBASRef(profileRef),
		CentWorkpaper: cloneBASRef(workpaperRef), WorkpaperSnapshot: proto.Clone(savedWorkpaper).(*tammyv1.BasWorkpaper),
		SourceSnapshotHash:   bytes.Clone(binding.SourceSnapshotHash),
		CoverageManifestHash: bytes.Clone(coverage.ManifestHash),
		RevisionVectorHash:   bytes.Clone(binding.RevisionVectorHash),
		RuleVersion:          fingerprints.ruleVersion, RuleHash: bytes.Clone(fingerprints.ruleHash),
		CalculatorVersion: fingerprints.calculatorVersion, CalculatorHash: bytes.Clone(fingerprints.calculatorHash),
		FormVersion: fingerprints.formVersion, FormHash: bytes.Clone(fingerprints.formHash),
		SchemaVersion: fingerprints.schemaVersion, SchemaHash: bytes.Clone(fingerprints.schemaHash),
		DescriptorVersion: fingerprints.descriptorVersion, DescriptorHash: bytes.Clone(fingerprints.descriptorHash),
		Result: calculationResult, Coverage: cloneBASCoverage(coverage.Coverage),
		Findings: findings, CreatedAt: timestamppb.New(now),
		InputRevision: base.Preparation.InputRevision, InputHash: bytes.Clone(base.Preparation.InputHash),
		PeriodSnapshot:       proto.Clone(period).(*tammyv1.BasConfirmedPeriod),
		ProfileSnapshot:      proto.Clone(profile).(*tammyv1.BasObligationsProfile),
		PaygwInputs:          proto.Clone(paygw).(*tammyv1.BasPaygwInputs),
		PaygiInputs:          proto.Clone(paygi).(*tammyv1.BasPaygiInputs),
		OrganisationSnapshot: proto.Clone(organisation).(*tammyv1.Organisation),
		OrganisationIdentity: &tammyv1.SourceRef{Type: "organisation", Id: organisation.Id,
			Revision: organisation.Version, ContentHash: bytes.Clone(organisationHash[:])},
		OrganisationSnapshotHash: bytes.Clone(organisationHash[:]),
		RevisionVector:           proto.Clone(revisionVector).(*tammyv1.BasSourceRevisionVector),
	}
	calculationRef, err := handler.calculations.Append(ctx, calculation, savedInputRef)
	if err != nil {
		return fail(err)
	}
	calculation.ContentHash = bytes.Clone(calculationRef.ContentHash)

	finalPreparation := proto.Clone(base.Preparation).(*tammyv1.BasPreparation)
	if finalPreparation.Version >= math.MaxInt64 {
		return fail(ErrCommandConflict)
	}
	finalPreparation.Version++
	finalPreparation.ContentHash = nil
	finalPreparation.CurrentCalculation = cloneBASRef(calculationRef)
	finalPreparation.CurrentValidation = nil
	finalPreparation.FrozenBas = nil
	finalPreparation.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING
	finalPreparation.UpdatedAt = timestamppb.New(now)
	finalRetained := &PreparationRetainedSnapshot{
		Preparation: finalPreparation, SavedPaygInputs: cloneBASRef(savedInputRef),
		BranchID: cloneBASString(base.BranchID), AttemptID: base.AttemptID,
	}
	finalPreparationRef, err := handler.preparations.Append(ctx, finalRetained, baseRef)
	if err != nil {
		return fail(err)
	}
	finalPreparation.ContentHash = bytes.Clone(finalPreparationRef.ContentHash)

	// Advance only a currently active branch attempt. This preserves the
	// confirmed family LatestHead and immutable RootPreparation.
	if branch != nil {
		if err := advanceBASActiveAttempt(ctx, request.OrganisationId, familyRef, currentRef, finalPreparationRef,
			timestamppb.New(now), handler.preparations, handler.familyStates, handler.branchStates); err != nil {
			return fail(err)
		}
	}

	affected := []*tammyv1.SourceRef{cloneBASRef(calculationRef), cloneBASRef(workpaperRef)}
	return &RegenerationResult{
		Response: &tammyv1.RegenerateBasPreparationResponse{
			Preparation: finalPreparation, Calculation: calculation,
		},
		Source: cloneBASRef(finalPreparationRef), Previous: previousRef, Affected: affected,
	}, nil
}

type basQueryRow struct {
	executor app.CommandSQLExecutor
	ctx      context.Context
	query    string
	args     []any
}

func (row basQueryRow) Scan(destinations ...any) error {
	if row.executor == nil || row.ctx == nil || row.ctx.Err() != nil {
		if row.ctx != nil && row.ctx.Err() != nil {
			return row.ctx.Err()
		}
		return ErrInvalidCommand
	}
	rows, err := row.executor.QueryContext(row.ctx, row.query, row.args...)
	if err != nil {
		return err
	}
	if !rows.Next() {
		return errors.Join(sql.ErrNoRows, rows.Err(), rows.Close())
	}
	scanErr := rows.Scan(destinations...)
	if scanErr != nil {
		return errors.Join(scanErr, rows.Close())
	}
	if rows.Next() {
		return errors.Join(ErrSnapshotCorrupt, rows.Close())
	}
	return errors.Join(rows.Err(), rows.Close())
}

func basRegenerationNewID(newID func() (string, error)) (string, error) {
	if newID == nil {
		return "", ErrInvalidCommand
	}
	value, err := newID()
	if err != nil || !ids.IsCanonicalV7(value) {
		return "", errors.Join(ErrInvalidCommand, err)
	}
	return value, nil
}

func basFamilyIdentityReference(value *tammyv1.BasFamilyIdentity) *tammyv1.SourceRef {
	if value == nil {
		return nil
	}
	return &tammyv1.SourceRef{Type: "bas_family", Id: value.FamilyId, Revision: 1,
		ContentHash: bytes.Clone(value.ContentHash)}
}

func profileReference(value *tammyv1.BasObligationsProfile) *tammyv1.SourceRef {
	if value == nil {
		return nil
	}
	return &tammyv1.SourceRef{Type: profileSnapshotSpec.resource, Id: value.ProfileId,
		Revision: value.Revision, ContentHash: bytes.Clone(value.ContentHash)}
}

func confirmedPeriodReference(value *tammyv1.BasConfirmedPeriod) *tammyv1.SourceRef {
	if value == nil {
		return nil
	}
	return &tammyv1.SourceRef{Type: periodSnapshotSpec.resource, Id: value.PeriodId,
		Revision: value.Version, ContentHash: bytes.Clone(value.ContentHash)}
}

func basExpectedIdentityMatches(expected *tammyv1.BasExpectedIdentity, actual *tammyv1.SourceRef) bool {
	if expected == nil {
		return false
	}
	if actual == nil {
		_, absent := expected.ExpectedState.(*tammyv1.BasExpectedIdentity_Absent)
		return absent && expected.GetAbsent() && expected.ExpectedVersion == nil
	}
	return expected.ExpectedVersion != nil && expected.GetExpectedVersion() == actual.Revision &&
		sameBASSourceRef(expected.GetRetained(), actual)
}

func basExpectedBranchIdentity(identity *tammyv1.SourceRef, version uint64) *tammyv1.BasExpectedIdentity {
	if identity == nil {
		return &tammyv1.BasExpectedIdentity{ExpectedState: &tammyv1.BasExpectedIdentity_Absent{Absent: true}}
	}
	versionCopy := version
	return &tammyv1.BasExpectedIdentity{
		ExpectedState:   &tammyv1.BasExpectedIdentity_Retained{Retained: cloneBASRef(identity)},
		ExpectedVersion: &versionCopy,
	}
}

func basInitialUnknownPaygInputs() (*tammyv1.BasPaygwInputs, *tammyv1.BasPaygiInputs) {
	unknown := func() *tammyv1.BasAmountInput {
		return &tammyv1.BasAmountInput{State: tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN}
	}
	return &tammyv1.BasPaygwInputs{
			Category:        tammyv1.BasPaygwCategory_BAS_PAYGW_CATEGORY_UNKNOWN,
			W1TotalPayments: unknown(), W2AmountWithheld: unknown(),
			W3OtherWithholding: unknown(), W4NoAbnWithholding: unknown(),
			PrefillReview: tammyv1.BasPrefillRequirement_BAS_PREFILL_REQUIREMENT_UNKNOWN,
		}, &tammyv1.BasPaygiInputs{
			Method:                tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_UNKNOWN,
			NoticeOperativeStatus: tammyv1.BasNoticeOperativeStatus_BAS_NOTICE_OPERATIVE_STATUS_UNKNOWN,
			PriorVariation:        tammyv1.BasPriorVariationDisposition_BAS_PRIOR_VARIATION_DISPOSITION_UNKNOWN,
			UnsupportedState:      tammyv1.BasPaygiUnsupportedState_BAS_PAYGI_UNSUPPORTED_STATE_NONE,
			T1ReviewDecision:      tammyv1.BasT1ReviewDecision_BAS_T1_REVIEW_DECISION_NOT_REVIEWED,
		}
}

func basKnownAUDAmount(value int64) *tammyv1.BasAmountInput {
	state := tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN
	if value == 0 {
		state = tammyv1.BasAmountState_BAS_AMOUNT_STATE_KNOWN_ZERO
	}
	return &tammyv1.BasAmountInput{State: state,
		Amount: &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: value}}
}

func basCoverageBlocksCompletion(rows []*tammyv1.BasSourceCoverage) (unresolved, unsupported bool) {
	for _, row := range rows {
		if row == nil {
			return true, true
		}
		switch row.Disposition {
		case tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_UNRESOLVED:
			unresolved = true
		case tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_UNSUPPORTED:
			unsupported = true
		}
	}
	return unresolved, unsupported
}

func basMaterializeCalculationFindings(
	ctx context.Context,
	newID func() (string, error),
	source []BASSourceFindingTemplate,
	calculation []CalculationFindingTemplate,
) ([]*tammyv1.BasValidationFinding, error) {
	if ctx == nil || len(source)+len(calculation) > 200 {
		return nil, ErrSnapshotBound
	}
	findings := make([]*tammyv1.BasValidationFinding, 0, len(source)+len(calculation))
	appendOne := func(severity tammyv1.BasFindingSeverity, code, message string,
		refs []*tammyv1.SourceRef, identities []*tammyv1.BasSourceIdentity) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if severity == tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_UNSPECIFIED || code == "" || message == "" {
			return ErrSnapshotCorrupt
		}
		id, err := basRegenerationNewID(newID)
		if err != nil {
			return err
		}
		findings = append(findings, &tammyv1.BasValidationFinding{
			FindingId: id, Severity: severity, StableCode: code, SafeMessage: message,
			Sources: cloneBASRefs(refs), SourceIdentities: cloneBASSourceIdentities(identities),
		})
		return nil
	}
	for _, template := range source {
		if err := appendOne(template.Severity, template.StableCode, template.SafeMessage, nil, template.SourceIdentities); err != nil {
			return nil, err
		}
	}
	for _, template := range calculation {
		if err := appendOne(template.Severity, template.StableCode, template.SafeMessage, template.Sources, nil); err != nil {
			return nil, err
		}
	}
	return findings, nil
}

func cloneBASCoverage(values []*tammyv1.BasSourceCoverage) []*tammyv1.BasSourceCoverage {
	out := make([]*tammyv1.BasSourceCoverage, 0, len(values))
	for _, value := range values {
		if value != nil {
			out = append(out, proto.Clone(value).(*tammyv1.BasSourceCoverage))
		}
	}
	return out
}

func basRegenerationCurrentRef(preparation *tammyv1.BasPreparation) *tammyv1.SourceRef {
	if preparation == nil {
		return nil
	}
	return preparationReference(preparation)
}

// advanceBASActiveAttempt keeps the mutable workflow pin in step with an
// ordinary successor to an already-active branch attempt. It deliberately
// leaves RootPreparation, EffectiveOriginal and LatestHead untouched: only
// explicit lifecycle transitions may change those confirmed-chain facts.
// Call it after the successor preparation is current, in the same caller-owned
// transaction. Root attempts have no branch state and require no family-state
// write.
func advanceBASActiveAttempt(
	ctx context.Context,
	org string,
	familyRef, previousRef, nextRef *tammyv1.SourceRef,
	at *timestamppb.Timestamp,
	preparations *PreparationRepository,
	familyStates *FamilyStateRepository,
	branchStates *BranchStateRepository,
) error {
	if ctx == nil || ctx.Err() != nil || !ids.IsCanonicalV7(org) ||
		!validBASSourceRef(familyRef, "bas_family", "") || familyRef.Revision != 1 ||
		!validBASSourceRef(previousRef, "bas_preparation", "") ||
		!validBASSourceRef(nextRef, "bas_preparation", previousRef.Id) ||
		nextRef.Revision <= previousRef.Revision || at == nil || !at.IsValid() ||
		preparations == nil || familyStates == nil || branchStates == nil {
		if ctx != nil && ctx.Err() != nil {
			return errors.Join(ErrCommandConflict, ctx.Err())
		}
		return ErrInvalidCommand
	}
	family, err := familyStates.GetCurrent(ctx, org, familyRef)
	if err != nil || family == nil || !sameBASSourceRef(family.Family, familyRef) {
		return errors.Join(ErrSnapshotCorrupt, err)
	}
	branchRef := family.ActiveBranch.GetRetained()
	if branchRef == nil {
		return nil
	}
	if family.ActiveBranch.GetExpectedVersion() == 0 {
		return ErrSnapshotCorrupt
	}
	branch, err := branchStates.getVersion(ctx, org, branchRef, family.ActiveBranch.GetExpectedVersion())
	if err != nil || branch == nil || !basBranchAllowsMutableAttempt(branch, family) ||
		!sameBASSourceRef(branch.ActiveAttempt.GetRetained(), previousRef) {
		return errors.Join(ErrCommandConflict, err)
	}
	prior, err := preparations.GetExact(ctx, org, previousRef)
	if err != nil || prior == nil {
		return errors.Join(ErrSnapshotCorrupt, err)
	}
	next, err := preparations.GetExact(ctx, org, nextRef)
	if err != nil || next == nil {
		return errors.Join(ErrSnapshotCorrupt, err)
	}
	current, err := preparations.GetCurrent(ctx, org, nextRef.Id)
	if err != nil || current == nil || !sameBASSourceRef(preparationReference(current.Preparation), nextRef) ||
		!sameBASSourceRef(next.Preparation.Family, familyRef) ||
		prior.BranchID == nil || *prior.BranchID != branchRef.Id ||
		next.BranchID == nil || *next.BranchID != branchRef.Id ||
		prior.AttemptID == "" || next.AttemptID != prior.AttemptID {
		return errors.Join(ErrCommandConflict, err)
	}
	if branch.Version >= math.MaxInt64 || family.Version >= math.MaxInt64 {
		return ErrCommandConflict
	}
	nextBranch := proto.Clone(branch).(*tammyv1.BasBranchState)
	nextBranch.Version++
	nextBranch.ContentHash = nil
	nextBranch.ActiveAttempt = expectedPreparation(next.Preparation)
	nextBranch.UpdatedAt = proto.Clone(at).(*timestamppb.Timestamp)
	nextBranchRef, err := branchStates.Append(ctx, org, nextBranch, branchStateReference(branch))
	if err != nil {
		return err
	}
	nextFamily := proto.Clone(family).(*tammyv1.BasFamilyState)
	nextFamily.Version++
	nextFamily.ContentHash = nil
	nextFamily.ActiveBranch = basExpectedBranchIdentity(branchRef, nextBranchRef.Revision)
	nextFamily.UpdatedAt = proto.Clone(at).(*timestamppb.Timestamp)
	_, err = familyStates.Append(ctx, org, nextFamily, familyStateReference(family))
	return err
}

// A resolution-pinned corrective attempt stays in its reserved branch while
// inputs, calculations and review versions advance. Frozen barriers remain closed.
func basBranchAllowsMutableAttempt(branch *tammyv1.BasBranchState, family *tammyv1.BasFamilyState) bool {
	if branch == nil || family == nil || branch.FrozenBarrier != nil || branch.BarrierPreparation != nil {
		return false
	}
	switch branch.WorkflowState {
	case tammyv1.BasBranchWorkflowState_BAS_BRANCH_WORKFLOW_STATE_ACTIVE:
		return branch.Resolution == nil
	case tammyv1.BasBranchWorkflowState_BAS_BRANCH_WORKFLOW_STATE_CORRECTIVE:
		return branch.Resolution != nil && family.DisputeOverlay == tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_CORRECTION_REQUIRED
	default:
		return false
	}
}
