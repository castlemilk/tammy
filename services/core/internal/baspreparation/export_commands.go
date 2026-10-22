package baspreparation

import (
	"buf.build/go/protovalidate"
	"bytes"
	"context"
	"errors"
	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"math"
)

func (repository *ExportEventRepository) LatestForTarget(ctx context.Context, org string,
	target *tammyv1.BasExportTarget) (*tammyv1.BasExportEvent, uint64, error) {
	if !repository.ready(ctx) || !ids.IsCanonicalV7(org) {
		return nil, 0, ErrExportEventRepository
	}
	request := ExportEventListRequest{OrganisationID: org, Target: target}
	records, err := repository.loadExportPageRecords(ctx, request, "ORDER BY append_ordinal DESC LIMIT 1")
	if err != nil {
		return nil, 0, errors.Join(ErrExportEventRepository, err)
	}
	if len(records) == 0 {
		return nil, 0, nil
	}
	if len(records) != 1 || records[0].version <= 0 {
		return nil, 0, errors.Join(ErrExportEventRepository, ErrSnapshotCorrupt)
	}
	value, err := repository.GetExact(ctx, org, records[0].exact)
	if err != nil {
		return nil, 0, err
	}
	return value, uint64(records[0].version), nil
}

func (service *Service) RecordBasExportEvent(ctx context.Context,
	request *tammyv1.RecordBasExportEventRequest) (*tammyv1.RecordBasExportEventResponse, error) {
	if service == nil || request == nil || !validBASCommandContext(request.CommandContext) ||
		!ids.IsCanonicalV7(request.OrganisationId) || request.Event == nil ||
		request.ExpectedVersion >= math.MaxInt64 || protovalidate.Validate(request.Event) != nil {
		return nil, ErrInvalidCommand
	}
	var response *tammyv1.RecordBasExportEventResponse
	var auditPayload *tammyv1.BasPreparationChangedEvent
	completed, err := service.commands.Execute(ctx, app.OrdinaryCommand[CommandRepositories]{
		Operation:      app.OrdinaryOperationRecordBasExportEvent,
		OperationKey:   request.CommandContext.IdempotencyKey,
		Authentication: request.CommandContext.Authentication,
		Request:        request,
		NewResult:      func() proto.Message { return &tammyv1.RecordBasExportEventResponse{} },
		SaveSource: func(ctx context.Context, repos CommandRepositories, owned proto.Message) error {
			r, ok := owned.(*tammyv1.RecordBasExportEventRequest)
			if !ok || repos.ExportEvents == nil || r.Event == nil {
				return ErrInvalidCommand
			}

			latest, currentVersion, err := repos.ExportEvents.LatestForTarget(ctx, r.OrganisationId, r.Event.Target)
			if err != nil {
				return err
			}
			if currentVersion != r.ExpectedVersion {
				return ErrExportEventConflict
			}
			switch r.Event.Phase {
			case tammyv1.BasExportPhase_BAS_EXPORT_PHASE_STARTED:
				if latest != nil && latest.Event != nil &&
					latest.Event.Phase == tammyv1.BasExportPhase_BAS_EXPORT_PHASE_STARTED {
					return ErrExportEventConflict
				}
			case tammyv1.BasExportPhase_BAS_EXPORT_PHASE_FINISHED:
				if latest == nil || latest.Event == nil ||
					latest.Event.Phase != tammyv1.BasExportPhase_BAS_EXPORT_PHASE_STARTED ||
					latest.Event.Format != r.Event.Format {
					return ErrExportEventConflict
				}
			default:
				return ErrInvalidCommand
			}

			eventID, err := service.newID()
			if err != nil || !ids.IsCanonicalV7(eventID) {
				return ErrInvalidCommand
			}
			// Core retains only event metadata and the optional artifact digest.
			// Reason is intentionally normalized to empty so a caller cannot put
			// a local path or renderer diagnostic text into retained history.
			input := proto.Clone(r.Event).(*tammyv1.BasExportEventInput)
			input.Reason = "Export attempt recorded"
			value := &tammyv1.BasExportEvent{
				ExportEventId:  eventID,
				OrganisationId: r.OrganisationId,
				Event:          input,
				ActorUserId:    r.CommandContext.Authentication.ActorUserId,
				RecordedAt:     timestamppb.New(service.now().UTC()),
			}
			ref, err := repos.ExportEvents.Append(ctx, r.OrganisationId, r.CommandContext.IdempotencyKey,
				r.ExpectedVersion, value)
			if err != nil {
				return err
			}
			value.ContentHash = bytes.Clone(ref.ContentHash)
			response = &tammyv1.RecordBasExportEventResponse{Event: value}
			_, targetRef, ok := exportTargetRef(value.Event.Target)
			if !ok {
				return ErrInvalidCommand
			}
			auditPayload = &tammyv1.BasPreparationChangedEvent{
				Action:            tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_EXPORT_EVENT_RECORDED,
				Source:            cloneBASRef(ref),
				AffectedResources: []*tammyv1.SourceRef{cloneBASRef(targetRef)},
			}
			return nil
		},
		BuildResult: func(ctx context.Context, _ CommandRepositories, _ proto.Message) (app.CommandResult, error) {
			if response == nil || response.Event == nil || auditPayload == nil {
				return app.CommandResult{}, ErrInvalidCommand
			}
			return service.audit.Build(ctx, app.OrdinaryOperationRecordBasExportEvent,
				request.CommandContext.Authentication, request.CommandContext.IdempotencyKey,
				response.Event.ExportEventId, response, auditPayload)
		},
	})
	if err != nil {
		return nil, err
	}
	value, ok := completed.(*tammyv1.RecordBasExportEventResponse)
	if !ok || value.Event == nil {
		return nil, ErrInvalidCommand
	}
	return value, nil
}
