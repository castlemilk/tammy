//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"bytes"
	"context"
	"errors"

	"buf.build/go/protovalidate"
	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/app"
	"github.com/tammyapp/tammy/services/core/internal/authorisation"
	"github.com/tammyapp/tammy/services/core/internal/baspreparation"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	tammyv1connect "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1/tammyv1connect"
	"github.com/tammyapp/tammy/services/core/internal/platform/faults"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

// Responses include the retained calculation (up to 4 MiB) or frozen record
// (up to 8 MiB), plus response and Any framing. Storage budgets stay separate.
const basQueryMaximumResponseBytes = 16 * 1024 * 1024

var errBASQueryNotFound = errors.New("local product: BAS query result not found")

func (handler *basPreparationHandler) beginBASRead(ctx context.Context,
	authentication *tammyv1.AuthenticationContext) (*sqlcipher.Transaction, error) {
	if handler == nil || handler.database == nil || handler.identity == nil || handler.cursors == nil || authentication == nil {
		return nil, ErrLedgerModule
	}
	if err := handler.identity.RequireActiveSessionReadOnly(ctx, authentication); err != nil {
		return nil, err
	}
	// These queries only read. Pin one deferred snapshot without reserving the
	// SQLite writer needed by concurrent identity and preparation commands.
	tx, err := handler.database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if err := handler.identity.ValidateAuthorizationWithin(ctx, tx, authentication, authorisation.ActionReadFinancial); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	return tx, nil
}

type basPreparationQueryRepositories struct {
	profiles         *baspreparation.ProfileRepository
	periods          *baspreparation.PeriodRepository
	preparations     *baspreparation.PreparationRepository
	calculations     *baspreparation.CalculationRepository
	validations      *baspreparation.ValidationRepository
	acknowledgements *baspreparation.WarningAcknowledgementRepository
	savedPayg        *baspreparation.SavedPaygRepository
	familyStates     *baspreparation.FamilyStateRepository
	branchStates     *baspreparation.BranchStateRepository
}

func newBASPreparationQueryRepositories(tx app.CommandSQLExecutor, cursors *paging.Codec) (*basPreparationQueryRepositories, error) {
	profiles, err := baspreparation.NewProfileRepository(tx, cursors)
	if err != nil {
		return nil, err
	}
	periods, err := baspreparation.NewPeriodRepository(tx, cursors)
	if err != nil {
		return nil, err
	}
	preparations, err := baspreparation.NewPreparationRepository(tx, cursors)
	if err != nil {
		return nil, err
	}
	calculations, err := baspreparation.NewCalculationRepository(tx, cursors)
	if err != nil {
		return nil, err
	}
	validations, err := baspreparation.NewValidationRepository(tx, cursors)
	if err != nil {
		return nil, err
	}
	acknowledgements, err := baspreparation.NewWarningAcknowledgementRepository(tx, cursors)
	if err != nil {
		return nil, err
	}
	savedPayg, err := baspreparation.NewSavedPaygRepository(tx, cursors)
	if err != nil {
		return nil, err
	}
	familyStates, err := baspreparation.NewFamilyStateRepository(tx, cursors)
	if err != nil {
		return nil, err
	}
	branchStates, err := baspreparation.NewBranchStateRepository(tx, cursors)
	if err != nil {
		return nil, err
	}
	return &basPreparationQueryRepositories{profiles, periods, preparations, calculations, validations,
		acknowledgements, savedPayg, familyStates, branchStates}, nil
}

func validateBASQuery(message proto.Message) error {
	if message == nil || protovalidate.Validate(message) != nil {
		return connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	return nil
}
func basQueryError(err error) error {
	code := connect.CodeInternal
	switch {
	case errors.Is(err, context.Canceled):
		code = connect.CodeCanceled
	case errors.Is(err, context.DeadlineExceeded):
		code = connect.CodeDeadlineExceeded
	case errors.Is(err, errBASQueryNotFound), errors.Is(err, baspreparation.ErrProfileNotEffective):
		code = connect.CodeNotFound
	case errors.Is(err, baspreparation.ErrInvalidCommand):
		code = connect.CodeInvalidArgument
	case errors.Is(err, baspreparation.ErrSnapshotBound):
		code = connect.CodeResourceExhausted
	case errors.Is(err, baspreparation.ErrCalendarInvalid), errors.Is(err, baspreparation.ErrCalendarUnsupported),
		errors.Is(err, baspreparation.ErrCommandConflict), errors.Is(err, baspreparation.ErrProfileConflict),
		errors.Is(err, baspreparation.ErrPeriodConflict), errors.Is(err, baspreparation.ErrPreparationConflict),
		errors.Is(err, baspreparation.ErrSavedPaygConflict):
		code = connect.CodeFailedPrecondition
	}
	if fault, ok := faults.CodeOf(err); ok {
		if fault == faults.CodeAuthenticationRequired {
			code = connect.CodeUnauthenticated
		}
		if fault == faults.CodePermissionDenied {
			code = connect.CodePermissionDenied
		}
	}
	return connect.NewError(code, ErrLedgerModule)
}
func basQueryFrameFits(message proto.Message) bool {
	frame, err := anypb.New(message)
	return err == nil && proto.Size(frame) <= basQueryMaximumResponseBytes
}
func (handler *basPreparationHandler) GetBasObligationsProfile(ctx context.Context,
	request *connect.Request[tammyv1.GetBasObligationsProfileRequest]) (*connect.Response[tammyv1.GetBasObligationsProfileResponse], error) {
	if request == nil || validateBASQuery(request.Msg) != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	tx, err := handler.beginBASRead(ctx, request.Msg.Authentication)
	if err != nil {
		return nil, basQueryError(err)
	}
	defer tx.Rollback()
	repos, err := newBASPreparationQueryRepositories(tx, handler.cursors)
	if err != nil {
		return nil, basQueryError(err)
	}
	var value *tammyv1.BasObligationsProfile
	var found bool
	if request.Msg.ProfileId != nil {
		value, found, err = repos.profiles.GetExactByHash(ctx, request.Msg.OrganisationId, *request.Msg.ProfileId, request.Msg.ProfileHash)
	} else {
		value, _, err = repos.profiles.GetEffectiveAt(ctx, request.Msg.OrganisationId, request.Msg.AsOfDate)
		found = err == nil
	}
	if err != nil {
		return nil, basQueryError(err)
	}
	if !found {
		return nil, basQueryError(errBASQueryNotFound)
	}
	response := &tammyv1.GetBasObligationsProfileResponse{Profile: value}
	if !basQueryFrameFits(response) {
		return nil, basQueryError(baspreparation.ErrSnapshotBound)
	}
	if err := tx.Commit(); err != nil {
		return nil, basQueryError(err)
	}
	return connect.NewResponse(response), nil
}

func (handler *basPreparationHandler) ListBasObligationsProfiles(ctx context.Context,
	request *connect.Request[tammyv1.ListBasObligationsProfilesRequest]) (*connect.Response[tammyv1.ListBasObligationsProfilesResponse], error) {
	if request == nil || validateBASQuery(request.Msg) != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	tx, err := handler.beginBASRead(ctx, request.Msg.Authentication)
	if err != nil {
		return nil, basQueryError(err)
	}
	defer tx.Rollback()
	repos, err := newBASPreparationQueryRepositories(tx, handler.cursors)
	if err != nil {
		return nil, basQueryError(err)
	}
	page, err := repos.profiles.List(ctx, baspreparation.ProfileListRequest{OrganisationID: request.Msg.OrganisationId,
		PageSize: request.Msg.Page.PageSize, Cursor: request.Msg.Page.Cursor})
	if err != nil {
		return nil, basQueryError(err)
	}
	response := &tammyv1.ListBasObligationsProfilesResponse{Profiles: page.Profiles, Page: page.Page}
	if !basQueryFrameFits(response) {
		return nil, basQueryError(baspreparation.ErrSnapshotBound)
	}
	if err := tx.Commit(); err != nil {
		return nil, basQueryError(err)
	}
	return connect.NewResponse(response), nil
}

func (handler *basPreparationHandler) GetBasCalendar(ctx context.Context,
	request *connect.Request[tammyv1.GetBasCalendarRequest]) (*connect.Response[tammyv1.GetBasCalendarResponse], error) {
	if request == nil || validateBASQuery(request.Msg) != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	tx, err := handler.beginBASRead(ctx, request.Msg.Authentication)
	if err != nil {
		return nil, basQueryError(err)
	}
	defer tx.Rollback()
	repos, err := newBASPreparationQueryRepositories(tx, handler.cursors)
	if err != nil {
		return nil, basQueryError(err)
	}
	var profile *tammyv1.BasObligationsProfile
	var found bool
	if request.Msg.Profile != nil {
		profile, err = repos.profiles.GetExact(ctx, request.Msg.OrganisationId, request.Msg.Profile)
		found = err == nil
	} else {
		profile, _, err = repos.profiles.GetEffectiveAt(ctx, request.Msg.OrganisationId, request.Msg.AsOfDate)
		found = err == nil
	}
	if err != nil {
		return nil, basQueryError(err)
	}
	if !found || profile == nil {
		return nil, basQueryError(errBASQueryNotFound)
	}
	profileRef := &tammyv1.SourceRef{Type: "bas_obligations_profile", Id: profile.ProfileId,
		Revision: profile.Revision, ContentHash: bytes.Clone(profile.ContentHash)}
	cadences, err := basCalendarCadences(profile.Profile)
	if err != nil {
		return nil, basQueryError(err)
	}
	ruleHash := baspreparation.DueDateRuleFingerprint()
	response := &tammyv1.GetBasCalendarResponse{Suggestions: make([]*tammyv1.BasPeriodSuggestion, 0, 160)}
	startYear, endYear := int(request.Msg.Horizon.StartDate.Year), int(request.Msg.Horizon.EndDate.Year)
	for year := startYear; year <= endYear; year++ {
		for _, cadence := range cadences {
			periods, err := baspreparation.CandidateReportingPeriods(year, cadence)
			if err != nil {
				return nil, basQueryError(err)
			}
			for _, period := range periods {
				if !basCalendarOverlaps(period.Start, period.End, request.Msg.Horizon) {
					continue
				}
				_, effective, err := repos.profiles.GetEffectiveForPeriod(ctx, request.Msg.OrganisationId,
					&tammyv1.BasCivilDateRange{StartDate: period.Start, EndDate: period.End})
				if errors.Is(err, baspreparation.ErrProfileNotEffective) {
					continue
				}
				if err != nil {
					return nil, basQueryError(err)
				}
				if !proto.Equal(effective, profileRef) {
					continue
				}
				response.Suggestions = append(response.Suggestions, &tammyv1.BasPeriodSuggestion{
					Period: &tammyv1.BasCivilDateRange{StartDate: proto.Clone(period.Start).(*tammyv1.CivilDate),
						EndDate: proto.Clone(period.End).(*tammyv1.CivilDate)},
					Window: basCalendarWindow(period), NominalDueDate: proto.Clone(period.SuggestedDueDate).(*tammyv1.CivilDate),
					DueDateRuleVersion: period.DueDateRuleVersion, DueDateRuleHash: bytes.Clone(ruleHash),
					Profile: proto.Clone(profileRef).(*tammyv1.SourceRef),
				})
			}
		}
	}
	if len(response.Suggestions) > 160 || !basQueryFrameFits(response) {
		return nil, basQueryError(baspreparation.ErrSnapshotBound)
	}
	if err := tx.Commit(); err != nil {
		return nil, basQueryError(err)
	}
	return connect.NewResponse(response), nil
}

func basCalendarCadences(profile *tammyv1.BasObligationsProfileInput) ([]baspreparation.ReportingCadence, error) {
	if profile == nil {
		return nil, baspreparation.ErrCalendarUnsupported
	}
	seen := map[baspreparation.ReportingCadence]bool{}
	add := func(active bool, cadence tammyv1.BasCadence) error {
		if !active {
			return nil
		}
		var value baspreparation.ReportingCadence
		switch cadence {
		case tammyv1.BasCadence_BAS_CADENCE_MONTHLY:
			value = baspreparation.ReportingCadenceMonthly
		case tammyv1.BasCadence_BAS_CADENCE_QUARTERLY:
			value = baspreparation.ReportingCadenceQuarterly
		default:
			return baspreparation.ErrCalendarUnsupported
		}
		seen[value] = true
		return nil
	}
	if profile.Gst != nil {
		if err := add(profile.Gst.Applicability == tammyv1.BasApplicability_BAS_APPLICABILITY_ACTIVE, profile.Gst.Cadence); err != nil {
			return nil, err
		}
	}
	if profile.Paygw != nil {
		if err := add(profile.Paygw.Applicability.GetApplicability() == tammyv1.BasApplicability_BAS_APPLICABILITY_ACTIVE, profile.Paygw.Cadence); err != nil {
			return nil, err
		}
	}
	if profile.Paygi != nil {
		if err := add(profile.Paygi.Applicability.GetApplicability() == tammyv1.BasApplicability_BAS_APPLICABILITY_ACTIVE, profile.Paygi.Cadence); err != nil {
			return nil, err
		}
	}
	result := make([]baspreparation.ReportingCadence, 0, 2)
	for _, cadence := range []baspreparation.ReportingCadence{baspreparation.ReportingCadenceMonthly, baspreparation.ReportingCadenceQuarterly} {
		if seen[cadence] {
			result = append(result, cadence)
		}
	}
	if len(result) > 1 {
		return nil, baspreparation.ErrCalendarUnsupported
	}
	return result, nil
}
func basCalendarDateKey(value *tammyv1.CivilDate) int64 {
	if value == nil {
		return 0
	}
	return int64(value.Year)*10000 + int64(value.Month)*100 + int64(value.Day)
}
func basCalendarOverlaps(start, end *tammyv1.CivilDate, horizon *tammyv1.BasCivilDateRange) bool {
	return start != nil && end != nil && horizon != nil &&
		basCalendarDateKey(start) <= basCalendarDateKey(horizon.EndDate) &&
		basCalendarDateKey(end) >= basCalendarDateKey(horizon.StartDate)
}
func basCalendarWindow(period baspreparation.ReportingPeriod) tammyv1.BasPeriodWindow {
	if period.Cadence == baspreparation.ReportingCadenceMonthly {
		return tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_MONTHLY
	}
	switch period.Start.Month {
	case 7:
		return tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JUL_SEP
	case 10:
		return tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_OCT_DEC
	case 1:
		return tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JAN_MAR
	case 4:
		return tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_APR_JUN
	default:
		return tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_UNSPECIFIED
	}
}

func (handler *basPreparationHandler) ListBasPeriods(ctx context.Context,
	request *connect.Request[tammyv1.ListBasPeriodsRequest]) (*connect.Response[tammyv1.ListBasPeriodsResponse], error) {
	if request == nil || validateBASQuery(request.Msg) != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	tx, err := handler.beginBASRead(ctx, request.Msg.Authentication)
	if err != nil {
		return nil, basQueryError(err)
	}
	defer tx.Rollback()
	repos, err := newBASPreparationQueryRepositories(tx, handler.cursors)
	if err != nil {
		return nil, basQueryError(err)
	}
	page, err := repos.periods.List(ctx, baspreparation.PeriodListRequest{OrganisationID: request.Msg.OrganisationId,
		Range: request.Msg.Range, PageSize: request.Msg.Page.PageSize, Cursor: request.Msg.Page.Cursor})
	if err != nil {
		return nil, basQueryError(err)
	}
	response := &tammyv1.ListBasPeriodsResponse{Periods: page.Periods, Page: page.Page}
	if !basQueryFrameFits(response) {
		return nil, basQueryError(baspreparation.ErrSnapshotBound)
	}
	if err := tx.Commit(); err != nil {
		return nil, basQueryError(err)
	}
	return connect.NewResponse(response), nil
}

func (handler *basPreparationHandler) GetBasPreparation(ctx context.Context,
	request *connect.Request[tammyv1.GetBasPreparationRequest]) (*connect.Response[tammyv1.GetBasPreparationResponse], error) {
	if request == nil || validateBASQuery(request.Msg) != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	tx, err := handler.beginBASRead(ctx, request.Msg.Authentication)
	if err != nil {
		return nil, basQueryError(err)
	}
	defer tx.Rollback()
	repos, err := newBASPreparationQueryRepositories(tx, handler.cursors)
	if err != nil {
		return nil, basQueryError(err)
	}
	retained, found, err := repos.preparations.GetExactByHash(ctx, request.Msg.OrganisationId,
		request.Msg.PreparationId, request.Msg.PreparationHash)
	if err != nil {
		return nil, basQueryError(err)
	}
	if !found || retained == nil || retained.Preparation == nil {
		return nil, basQueryError(errBASQueryNotFound)
	}
	preparation := retained.Preparation
	response := &tammyv1.GetBasPreparationResponse{Preparation: preparation,
		Acknowledgements: []*tammyv1.BasWarningAcknowledgement{}}
	if preparation.CurrentCalculation != nil {
		response.CurrentCalculation, err = repos.calculations.GetExact(ctx, request.Msg.OrganisationId, preparation.CurrentCalculation)
		if err != nil {
			return nil, basQueryError(err)
		}
	}
	if preparation.CurrentValidation != nil {
		response.CurrentValidation, err = repos.validations.GetExact(ctx, request.Msg.OrganisationId, preparation.CurrentValidation)
		if err != nil {
			return nil, basQueryError(err)
		}
		response.Acknowledgements, err = repos.acknowledgements.ListForValidation(ctx, request.Msg.OrganisationId, preparation.CurrentValidation)
		if err != nil {
			return nil, basQueryError(err)
		}
	}
	if retained.SavedPaygInputs != nil {
		saved, getErr := repos.savedPayg.GetExact(ctx, request.Msg.OrganisationId, retained.SavedPaygInputs)
		if getErr != nil {
			return nil, basQueryError(getErr)
		}
		response.PaygwInputs, response.PaygiInputs = proto.Clone(saved.PaygwInputs).(*tammyv1.BasPaygwInputs), proto.Clone(saved.PaygiInputs).(*tammyv1.BasPaygiInputs)
	}
	response.CurrentChain, err = basCurrentChainExpectation(ctx, request.Msg.OrganisationId, preparation, repos)
	if err != nil {
		return nil, basQueryError(err)
	}
	familyState, err := repos.familyStates.GetCurrent(ctx, request.Msg.OrganisationId, preparation.Family)
	if err != nil {
		return nil, basQueryError(err)
	}
	response.EffectiveOriginal = cloneLocalBASRef(familyState.EffectiveOriginal)
	response.CurrentDisputeOverlay = familyState.DisputeOverlay
	if branchRef := familyState.ActiveBranch.GetRetained(); branchRef != nil {
		branch, err := repos.branchStates.GetCurrent(ctx, request.Msg.OrganisationId, branchRef)
		if err != nil || branch == nil || branch.Version != familyState.ActiveBranch.GetExpectedVersion() {
			return nil, basQueryError(errors.Join(baspreparation.ErrSnapshotCorrupt, err))
		}
		response.CurrentBranchResolution = cloneLocalBASRef(branch.Resolution)
		response.CurrentFrozenBarrier = cloneLocalBASRef(branch.FrozenBarrier)
	}
	if familyState.DisputeOverlay == tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_DISPUTED {
		reader, err := baspreparation.NewBASExternalHistoryReader(tx, handler.cursors)
		if err != nil {
			return nil, basQueryError(err)
		}
		history, err := reader.Read(ctx, request.Msg.OrganisationId, preparation.Family)
		if err != nil {
			return nil, basQueryError(err)
		}
		response.CurrentDispute, err = baspreparation.BuildBASExternalDisputeSummary(history)
		if err != nil {
			return nil, basQueryError(err)
		}
	}
	response.LiveCurrentness = handler.basLiveCurrentness(ctx, tx, retained, response.CurrentCalculation)
	if err := protovalidate.Validate(response); err != nil || !basQueryFrameFits(response) {
		return nil, basQueryError(baspreparation.ErrSnapshotBound)
	}
	if err := tx.Commit(); err != nil {
		return nil, basQueryError(err)
	}
	return connect.NewResponse(response), nil
}

func basCurrentChainExpectation(ctx context.Context, organisationID string, preparation *tammyv1.BasPreparation,
	repos *basPreparationQueryRepositories) (*tammyv1.BasChainExpectation, error) {
	if preparation == nil || repos == nil {
		return nil, baspreparation.ErrSnapshotCorrupt
	}
	state, err := repos.familyStates.GetCurrent(ctx, organisationID, preparation.Family)
	if err != nil {
		return nil, err
	}
	expectation := &tammyv1.BasChainExpectation{
		Family: proto.Clone(state.Family).(*tammyv1.SourceRef), ExpectedFamilyVersion: state.Version,
		RootPreparation: proto.Clone(state.RootPreparation).(*tammyv1.SourceRef),
		LatestHead:      proto.Clone(state.LatestHead).(*tammyv1.BasExpectedIdentity),
		Branch:          proto.Clone(state.ActiveBranch).(*tammyv1.BasExpectedIdentity),
	}
	if branchRef := state.ActiveBranch.GetRetained(); branchRef != nil {
		branch, err := repos.branchStates.GetCurrent(ctx, organisationID, branchRef)
		if err != nil {
			return nil, err
		}
		if branch.Version != state.ActiveBranch.GetExpectedVersion() {
			return nil, baspreparation.ErrSnapshotCorrupt
		}
		expectation.ActiveAttempt = proto.Clone(branch.ActiveAttempt).(*tammyv1.BasExpectedIdentity)
	} else {
		expectation.ActiveAttempt = &tammyv1.BasExpectedIdentity{
			ExpectedState: &tammyv1.BasExpectedIdentity_Absent{Absent: true},
		}
	}
	if err := protovalidate.Validate(expectation); err != nil {
		return nil, baspreparation.ErrSnapshotCorrupt
	}
	return expectation, nil
}

func (handler *basPreparationHandler) ListBasPreparations(ctx context.Context,
	request *connect.Request[tammyv1.ListBasPreparationsRequest]) (*connect.Response[tammyv1.ListBasPreparationsResponse], error) {
	if request == nil || validateBASQuery(request.Msg) != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	tx, err := handler.beginBASRead(ctx, request.Msg.Authentication)
	if err != nil {
		return nil, basQueryError(err)
	}
	defer tx.Rollback()
	repos, err := newBASPreparationQueryRepositories(tx, handler.cursors)
	if err != nil {
		return nil, basQueryError(err)
	}
	var family *tammyv1.SourceRef
	if request.Msg.FamilyId != nil {
		family = &tammyv1.SourceRef{Type: "bas_family", Id: *request.Msg.FamilyId, Revision: 1,
			ContentHash: bytes.Clone(request.Msg.FamilyHash)}
	}
	page, err := repos.preparations.List(ctx, baspreparation.PreparationListRequest{OrganisationID: request.Msg.OrganisationId,
		Family: family, PageSize: request.Msg.Page.PageSize, Cursor: request.Msg.Page.Cursor})
	if err != nil {
		return nil, basQueryError(err)
	}
	response := &tammyv1.ListBasPreparationsResponse{Preparations: page.Preparations, Page: page.Page}
	if !basQueryFrameFits(response) {
		return nil, basQueryError(baspreparation.ErrSnapshotBound)
	}
	if err := tx.Commit(); err != nil {
		return nil, basQueryError(err)
	}
	return connect.NewResponse(response), nil
}

func (handler *basPreparationHandler) GetBasCalculation(ctx context.Context,
	request *connect.Request[tammyv1.GetBasCalculationRequest]) (*connect.Response[tammyv1.GetBasCalculationResponse], error) {
	if request == nil || validateBASQuery(request.Msg) != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	tx, err := handler.beginBASRead(ctx, request.Msg.Authentication)
	if err != nil {
		return nil, basQueryError(err)
	}
	defer tx.Rollback()
	repos, err := newBASPreparationQueryRepositories(tx, handler.cursors)
	if err != nil {
		return nil, basQueryError(err)
	}
	value, found, err := repos.calculations.GetExactByHash(ctx, request.Msg.OrganisationId,
		request.Msg.CalculationId, request.Msg.CalculationHash)
	if err != nil {
		return nil, basQueryError(err)
	}
	if !found {
		return nil, basQueryError(errBASQueryNotFound)
	}
	response := &tammyv1.GetBasCalculationResponse{Calculation: value}
	if !basQueryFrameFits(response) {
		return nil, basQueryError(baspreparation.ErrSnapshotBound)
	}
	if err := tx.Commit(); err != nil {
		return nil, basQueryError(err)
	}
	return connect.NewResponse(response), nil
}

func (handler *basPreparationHandler) ListBasCalculations(ctx context.Context,
	request *connect.Request[tammyv1.ListBasCalculationsRequest]) (*connect.Response[tammyv1.ListBasCalculationsResponse], error) {
	if request == nil || validateBASQuery(request.Msg) != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	tx, err := handler.beginBASRead(ctx, request.Msg.Authentication)
	if err != nil {
		return nil, basQueryError(err)
	}
	defer tx.Rollback()
	repos, err := newBASPreparationQueryRepositories(tx, handler.cursors)
	if err != nil {
		return nil, basQueryError(err)
	}
	preparation, found, err := repos.preparations.GetExactByHash(ctx, request.Msg.OrganisationId,
		request.Msg.PreparationId, request.Msg.PreparationHash)
	if err != nil {
		return nil, basQueryError(err)
	}
	if !found || preparation == nil || preparation.Preparation == nil {
		return nil, basQueryError(errBASQueryNotFound)
	}
	page, err := repos.calculations.List(ctx, baspreparation.CalculationListRequest{
		OrganisationID: request.Msg.OrganisationId,
		Preparation: &tammyv1.SourceRef{Type: "bas_preparation", Id: preparation.Preparation.PreparationId,
			Revision: preparation.Preparation.Version, ContentHash: bytes.Clone(preparation.Preparation.ContentHash)},
		PageSize: request.Msg.Page.PageSize, Cursor: request.Msg.Page.Cursor,
	})
	if err != nil {
		return nil, basQueryError(err)
	}
	response := &tammyv1.ListBasCalculationsResponse{Calculations: page.Calculations, Page: page.Page}
	if !basQueryFrameFits(response) {
		return nil, basQueryError(baspreparation.ErrSnapshotBound)
	}
	if err := tx.Commit(); err != nil {
		return nil, basQueryError(err)
	}
	return connect.NewResponse(response), nil
}

var _ tammyv1connect.BasPreparationServiceHandler = (*basPreparationHandler)(nil)
