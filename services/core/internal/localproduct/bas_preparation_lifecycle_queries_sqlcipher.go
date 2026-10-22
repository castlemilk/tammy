//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"bytes"
	"connectrpc.com/connect"
	"context"
	"github.com/tammyapp/tammy/services/core/internal/app"
	"github.com/tammyapp/tammy/services/core/internal/baspreparation"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
)

/*
Task 9 query handlers to append to localproduct/bas_preparation_queries_sqlcipher.go.
The repository API additions in tammy-task9-exact-query-repository-methods-2026-10-22.go.txt
provide GetExactByHash(...)(value, found, error); they distinguish absent/wrong-hash
identity (NotFound) from a corrupt body proof (Internal).
*/

type basPreparationLifecycleQueryRepositories struct {
	frozen       *baspreparation.FrozenRepository
	observations *baspreparation.ExternalObservationRepository
	resolutions  *baspreparation.DisputeResolutionRepository
	exports      *baspreparation.ExportEventRepository
}

func newBASPreparationLifecycleQueryRepositories(tx app.CommandSQLExecutor, cursors *paging.Codec) (*basPreparationLifecycleQueryRepositories, error) {
	frozen, err := baspreparation.NewFrozenRepository(tx, cursors)
	if err != nil {
		return nil, err
	}
	observations, err := baspreparation.NewExternalObservationRepository(tx, cursors)
	if err != nil {
		return nil, err
	}
	resolutions, err := baspreparation.NewDisputeResolutionRepository(tx, cursors)
	if err != nil {
		return nil, err
	}
	exports, err := baspreparation.NewExportEventRepository(tx, cursors)
	if err != nil {
		return nil, err
	}
	return &basPreparationLifecycleQueryRepositories{frozen, observations, resolutions, exports}, nil
}

func basLifecycleFamilyRef(id *string, hash []byte) *tammyv1.SourceRef {
	if id == nil {
		return nil
	}
	return &tammyv1.SourceRef{Type: "bas_family", Id: *id, Revision: 1, ContentHash: bytes.Clone(hash)}
}

func (handler *basPreparationHandler) GetFrozenBas(ctx context.Context,
	request *connect.Request[tammyv1.GetFrozenBasRequest]) (*connect.Response[tammyv1.GetFrozenBasResponse], error) {
	if request == nil || validateBASQuery(request.Msg) != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	tx, err := handler.beginBASRead(ctx, request.Msg.Authentication)
	if err != nil {
		return nil, basQueryError(err)
	}
	defer tx.Rollback()
	repos, err := newBASPreparationLifecycleQueryRepositories(tx, handler.cursors)
	if err != nil {
		return nil, basQueryError(err)
	}
	exact := &tammyv1.SourceRef{Type: "bas_frozen", Id: request.Msg.FrozenId, Revision: 1, ContentHash: bytes.Clone(request.Msg.FrozenHash)}
	value, found, err := repos.frozen.GetExactByHash(ctx, request.Msg.OrganisationId, exact)
	if err != nil {
		return nil, basQueryError(err)
	}
	if !found || value == nil {
		return nil, basQueryError(errBASQueryNotFound)
	}
	response := &tammyv1.GetFrozenBasResponse{FrozenBas: value}
	preparations, err := baspreparation.NewPreparationRepository(tx, handler.cursors)
	if err != nil {
		return nil, basQueryError(err)
	}
	owner, err := preparations.GetExact(ctx, request.Msg.OrganisationId, value.Preparation)
	if err != nil {
		return nil, basQueryError(err)
	}
	response.LiveCurrentness = handler.basLiveCurrentness(ctx, tx, owner, value.CalculationSnapshot)

	if !basQueryFrameFits(response) {
		return nil, basQueryError(baspreparation.ErrSnapshotBound)
	}
	if err := tx.Commit(); err != nil {
		return nil, basQueryError(err)
	}
	return connect.NewResponse(response), nil
}

func (handler *basPreparationHandler) ListFrozenBas(ctx context.Context,
	request *connect.Request[tammyv1.ListFrozenBasRequest]) (*connect.Response[tammyv1.ListFrozenBasResponse], error) {
	if request == nil || validateBASQuery(request.Msg) != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	tx, err := handler.beginBASRead(ctx, request.Msg.Authentication)
	if err != nil {
		return nil, basQueryError(err)
	}
	defer tx.Rollback()
	repos, err := newBASPreparationLifecycleQueryRepositories(tx, handler.cursors)
	if err != nil {
		return nil, basQueryError(err)
	}
	page, err := repos.frozen.List(ctx, baspreparation.FrozenListRequest{
		OrganisationID: request.Msg.OrganisationId, Family: basLifecycleFamilyRef(request.Msg.FamilyId, request.Msg.FamilyHash),
		PageSize: request.Msg.Page.PageSize, Cursor: request.Msg.Page.Cursor,
	})
	if err != nil {
		return nil, basQueryError(err)
	}
	response := &tammyv1.ListFrozenBasResponse{FrozenBas: page.Frozen, Page: page.Page}
	if !basQueryFrameFits(response) {
		return nil, basQueryError(baspreparation.ErrSnapshotBound)
	}
	if err := tx.Commit(); err != nil {
		return nil, basQueryError(err)
	}
	return connect.NewResponse(response), nil
}

func (handler *basPreparationHandler) GetBasExternalObservation(ctx context.Context,
	request *connect.Request[tammyv1.GetBasExternalObservationRequest]) (*connect.Response[tammyv1.GetBasExternalObservationResponse], error) {
	if request == nil || validateBASQuery(request.Msg) != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	tx, err := handler.beginBASRead(ctx, request.Msg.Authentication)
	if err != nil {
		return nil, basQueryError(err)
	}
	defer tx.Rollback()
	repos, err := newBASPreparationLifecycleQueryRepositories(tx, handler.cursors)
	if err != nil {
		return nil, basQueryError(err)
	}
	exact := &tammyv1.SourceRef{Type: "bas_external_observation", Id: request.Msg.ObservationId, Revision: 1, ContentHash: bytes.Clone(request.Msg.ObservationHash)}
	value, found, err := repos.observations.GetExactByHash(ctx, request.Msg.OrganisationId, exact)
	if err != nil {
		return nil, basQueryError(err)
	}
	if !found || value == nil {
		return nil, basQueryError(errBASQueryNotFound)
	}
	response := &tammyv1.GetBasExternalObservationResponse{Observation: value}
	if !basQueryFrameFits(response) {
		return nil, basQueryError(baspreparation.ErrSnapshotBound)
	}
	if err := tx.Commit(); err != nil {
		return nil, basQueryError(err)
	}
	return connect.NewResponse(response), nil
}

func (handler *basPreparationHandler) ListBasExternalObservations(ctx context.Context,
	request *connect.Request[tammyv1.ListBasExternalObservationsRequest]) (*connect.Response[tammyv1.ListBasExternalObservationsResponse], error) {
	if request == nil || validateBASQuery(request.Msg) != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	tx, err := handler.beginBASRead(ctx, request.Msg.Authentication)
	if err != nil {
		return nil, basQueryError(err)
	}
	defer tx.Rollback()
	repos, err := newBASPreparationLifecycleQueryRepositories(tx, handler.cursors)
	if err != nil {
		return nil, basQueryError(err)
	}
	frozen := &tammyv1.SourceRef{Type: "bas_frozen", Id: request.Msg.FrozenId, Revision: 1, ContentHash: bytes.Clone(request.Msg.FrozenHash)}
	page, err := repos.observations.List(ctx, baspreparation.ExternalObservationListRequest{
		OrganisationID: request.Msg.OrganisationId, Frozen: frozen, PageSize: request.Msg.Page.PageSize, Cursor: request.Msg.Page.Cursor,
	})
	if err != nil {
		return nil, basQueryError(err)
	}
	response := &tammyv1.ListBasExternalObservationsResponse{Observations: page.Observations, Page: page.Page}
	if !basQueryFrameFits(response) {
		return nil, basQueryError(baspreparation.ErrSnapshotBound)
	}
	if err := tx.Commit(); err != nil {
		return nil, basQueryError(err)
	}
	return connect.NewResponse(response), nil
}

func (handler *basPreparationHandler) GetBasExternalDisputeResolution(ctx context.Context,
	request *connect.Request[tammyv1.GetBasExternalDisputeResolutionRequest]) (*connect.Response[tammyv1.GetBasExternalDisputeResolutionResponse], error) {
	if request == nil || validateBASQuery(request.Msg) != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	tx, err := handler.beginBASRead(ctx, request.Msg.Authentication)
	if err != nil {
		return nil, basQueryError(err)
	}
	defer tx.Rollback()
	repos, err := newBASPreparationLifecycleQueryRepositories(tx, handler.cursors)
	if err != nil {
		return nil, basQueryError(err)
	}
	exact := &tammyv1.SourceRef{Type: "bas_external_dispute_resolution", Id: request.Msg.ResolutionId, Revision: 1, ContentHash: bytes.Clone(request.Msg.ResolutionHash)}
	value, found, err := repos.resolutions.GetExactByHash(ctx, request.Msg.OrganisationId, exact)
	if err != nil {
		return nil, basQueryError(err)
	}
	if !found || value == nil {
		return nil, basQueryError(errBASQueryNotFound)
	}
	response := &tammyv1.GetBasExternalDisputeResolutionResponse{Resolution: value}
	if !basQueryFrameFits(response) {
		return nil, basQueryError(baspreparation.ErrSnapshotBound)
	}
	if err := tx.Commit(); err != nil {
		return nil, basQueryError(err)
	}
	return connect.NewResponse(response), nil
}

func (handler *basPreparationHandler) ListBasExternalDisputeResolutions(ctx context.Context,
	request *connect.Request[tammyv1.ListBasExternalDisputeResolutionsRequest]) (*connect.Response[tammyv1.ListBasExternalDisputeResolutionsResponse], error) {
	if request == nil || validateBASQuery(request.Msg) != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	tx, err := handler.beginBASRead(ctx, request.Msg.Authentication)
	if err != nil {
		return nil, basQueryError(err)
	}
	defer tx.Rollback()
	repos, err := newBASPreparationLifecycleQueryRepositories(tx, handler.cursors)
	if err != nil {
		return nil, basQueryError(err)
	}
	page, err := repos.resolutions.List(ctx, baspreparation.DisputeResolutionListRequest{
		OrganisationID: request.Msg.OrganisationId, Family: basLifecycleFamilyRef(request.Msg.FamilyId, request.Msg.FamilyHash),
		PageSize: request.Msg.Page.PageSize, Cursor: request.Msg.Page.Cursor,
	})
	if err != nil {
		return nil, basQueryError(err)
	}
	response := &tammyv1.ListBasExternalDisputeResolutionsResponse{Resolutions: page.Resolutions, Page: page.Page}
	if !basQueryFrameFits(response) {
		return nil, basQueryError(baspreparation.ErrSnapshotBound)
	}
	if err := tx.Commit(); err != nil {
		return nil, basQueryError(err)
	}
	return connect.NewResponse(response), nil
}

// ListBasExportEvents deliberately returns one repository page unchanged.
// Callers must keep passing NextCursor until it is absent, even if Events is empty:
// the repository scans a mixed family log and may produce empty target-filtered pages.
func (handler *basPreparationHandler) ListBasExportEvents(ctx context.Context,
	request *connect.Request[tammyv1.ListBasExportEventsRequest]) (*connect.Response[tammyv1.ListBasExportEventsResponse], error) {
	if request == nil || validateBASQuery(request.Msg) != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	tx, err := handler.beginBASRead(ctx, request.Msg.Authentication)
	if err != nil {
		return nil, basQueryError(err)
	}
	defer tx.Rollback()
	repos, err := newBASPreparationLifecycleQueryRepositories(tx, handler.cursors)
	if err != nil {
		return nil, basQueryError(err)
	}
	page, err := repos.exports.List(ctx, baspreparation.ExportEventListRequest{
		OrganisationID: request.Msg.OrganisationId, Target: request.Msg.Target,
		PageSize: request.Msg.Page.PageSize, Cursor: request.Msg.Page.Cursor,
	})
	if err != nil {
		return nil, basQueryError(err)
	}
	response := &tammyv1.ListBasExportEventsResponse{Events: page.Events, Page: page.Page}
	if !basQueryFrameFits(response) {
		return nil, basQueryError(baspreparation.ErrSnapshotBound)
	}
	if err := tx.Commit(); err != nil {
		return nil, basQueryError(err)
	}
	return connect.NewResponse(response), nil
}
