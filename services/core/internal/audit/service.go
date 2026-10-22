package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/authorisation"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	tammyv1connect "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1/tammyv1connect"
	"github.com/tammyapp/tammy/services/core/internal/idempotency"
	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
	"github.com/tammyapp/tammy/services/core/internal/platform/faults"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var ErrAuditService = errors.New("audit: service request failed")

type ServiceAccess interface {
	Require(context.Context, ServiceTransaction, *tammyv1.AuthenticationContext, authorisation.Action) error
}

type ServiceTransaction interface {
	Executor
	TransactionID() string
	AfterCommit(func(context.Context) error) error
}

// ServiceTransactions owns the outer read/write transaction. Audit and
// idempotency receive only the supplied Executor and cannot commit it.
type ServiceTransactions interface {
	WorkspaceID() string
	Read(context.Context, func(ServiceTransaction) error) error
	Mutate(context.Context, func(ServiceTransaction) error) error
}

type ServiceConfig struct {
	Access            ServiceAccess
	Transactions      ServiceTransactions
	Elector           *idempotency.Elector
	Clock             clock.Clock
	NewID             func() (string, error)
	Cursors           *paging.Codec
	SchemaFingerprint []byte
	Appender          *Appender
}

type Service struct {
	access            ServiceAccess
	transactions      ServiceTransactions
	elector           *idempotency.Elector
	clock             clock.Clock
	newID             func() (string, error)
	cursors           *paging.Codec
	schemaFingerprint []byte
	appender          *Appender
}

var _ tammyv1connect.AuditServiceHandler = (*Service)(nil)

func NewService(config ServiceConfig) (*Service, error) {
	if config.Access == nil || config.Transactions == nil || config.Transactions.WorkspaceID() == "" ||
		config.Elector == nil || config.Clock == nil || config.NewID == nil || config.Cursors == nil || config.Appender == nil ||
		config.Appender.mirror == nil || config.Appender.gate == nil ||
		len(config.SchemaFingerprint) != sha256.Size {
		return nil, ErrAuditService
	}
	return &Service{access: config.Access, transactions: config.Transactions, elector: config.Elector,
		clock: config.Clock, newID: config.NewID, cursors: config.Cursors, appender: config.Appender,
		schemaFingerprint: append([]byte(nil), config.SchemaFingerprint...)}, nil
}

func (service *Service) VerifyChain(ctx context.Context, request *connect.Request[tammyv1.VerifyChainRequest]) (*connect.Response[tammyv1.VerifyChainResponse], error) {
	if request == nil || request.Msg == nil || request.Msg.Authentication == nil || request.Msg.WorkspaceId != service.transactions.WorkspaceID() {
		return nil, ErrAuditService
	}
	if request.Msg.StartSequence != nil && *request.Msg.StartSequence == 0 ||
		request.Msg.EndSequence != nil && *request.Msg.EndSequence == 0 ||
		request.Msg.StartSequence != nil && request.Msg.EndSequence != nil && *request.Msg.EndSequence < *request.Msg.StartSequence {
		return nil, ErrAuditService
	}
	var response *tammyv1.VerifyChainResponse
	err := service.transactions.Read(ctx, func(executor ServiceTransaction) error {
		if err := service.access.Require(ctx, executor, request.Msg.Authentication, authorisation.ActionReadAudit); err != nil {
			return err
		}
		generation := request.Msg.GetGeneration()
		header, err := LoadChainHeader(ctx, executor, request.Msg.WorkspaceId, generation)
		if err != nil {
			return err
		}
		if header.CurrentSequence == 0 {
			if request.Msg.StartSequence != nil || request.Msg.EndSequence != nil {
				return ErrAuditService
			}
		}
		start, end := uint64(1), header.CurrentSequence
		if header.CurrentSequence != 0 && request.Msg.StartSequence != nil {
			start = *request.Msg.StartSequence
		}
		if header.CurrentSequence != 0 && request.Msg.EndSequence != nil {
			end = *request.Msg.EndSequence
		}
		if header.CurrentSequence != 0 && (start > end || end > header.CurrentSequence) {
			return ErrAuditService
		}
		snapshot := storedEventSnapshotFromHeader(header)
		var endHead [sha256.Size]byte
		response, endHead, err = verifyAuditSnapshot(ctx, executor, header, snapshot, end)
		if err != nil || response.Integrity != tammyv1.AuditChainIntegrity_AUDIT_CHAIN_INTEGRITY_VALID {
			return err
		}
		if header.CurrentSequence != 0 && end != header.CurrentSequence {
			response.VerifiedThroughSequence = end
			response.VerifiedHead = append([]byte(nil), endHead[:]...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response), nil
}

func (service *Service) ListAuditEvents(ctx context.Context, request *connect.Request[tammyv1.ListAuditEventsRequest]) (*connect.Response[tammyv1.ListAuditEventsResponse], error) {
	if request == nil || request.Msg == nil || request.Msg.Authentication == nil || request.Msg.Filter == nil || request.Msg.Page == nil ||
		request.Msg.WorkspaceId != service.transactions.WorkspaceID() || request.Msg.Page.PageSize == 0 || request.Msg.Page.PageSize > 200 ||
		!canonicalAuditEventFilter(request.Msg.Filter) {
		return nil, ErrAuditService
	}
	queryHash, err := auditEventQueryHash(request.Msg.WorkspaceId, request.Msg.Filter)
	if err != nil {
		return nil, err
	}
	var snapshot StoredEventSnapshot
	position := uint64(0)
	hasCursor := request.Msg.Page.Cursor != nil
	if hasCursor {
		cursor, decodeErr := service.cursors.Decode(*request.Msg.Page.Cursor, queryHash)
		if decodeErr != nil {
			return nil, ErrAuditService
		}
		snapshot, decodeErr = parseAuditEventSnapshot(cursor.Snapshot, request.Msg.WorkspaceId)
		if decodeErr != nil {
			return nil, ErrAuditService
		}
		position, decodeErr = parseCanonicalAuditCursorPosition(cursor.Position, snapshot.EndSequence)
		if decodeErr != nil {
			return nil, ErrAuditService
		}
	}
	var response *tammyv1.ListAuditEventsResponse
	err = service.transactions.Read(ctx, func(executor ServiceTransaction) error {
		if err := service.access.Require(ctx, executor, request.Msg.Authentication, authorisation.ActionReadAudit); err != nil {
			return err
		}
		generation := uint64(0)
		if hasCursor {
			generation = snapshot.Generation
		}
		header, err := LoadChainHeader(ctx, executor, request.Msg.WorkspaceId, generation)
		if err != nil {
			return err
		}
		if !hasCursor {
			snapshot = storedEventSnapshotFromHeader(header)
		} else if header.Generation != snapshot.Generation || header.CurrentSequence < snapshot.EndSequence {
			return ErrAuditService
		}
		verified, _, err := verifyAuditSnapshot(ctx, executor, header, snapshot, snapshot.EndSequence)
		if err != nil {
			if errors.Is(err, ErrInvalidEvent) {
				return ErrInvalidEvent
			}
			return err
		}
		if verified.Integrity != tammyv1.AuditChainIntegrity_AUDIT_CHAIN_INTEGRITY_VALID {
			return ErrInvalidEvent
		}
		matching, err := LoadMatchingAuditEventPage(ctx, executor, snapshot, request.Msg.Filter,
			position, request.Msg.Page.PageSize)
		if err != nil {
			if errors.Is(err, ErrInvalidEvent) {
				return ErrInvalidEvent
			}
			return err
		}
		page := &tammyv1.PageInfo{ReturnedCount: uint32(len(matching.Events))}
		if matching.HasMore {
			if len(matching.Events) == 0 {
				return ErrRepository
			}
			token, encodeErr := service.cursors.Encode(paging.Cursor{Snapshot: formatAuditEventSnapshot(snapshot),
				Position: strconv.FormatUint(matching.Events[len(matching.Events)-1].Sequence, 10), QueryHash: queryHash})
			if encodeErr != nil {
				return encodeErr
			}
			page.NextCursor = &token
		}
		response = &tammyv1.ListAuditEventsResponse{Events: matching.Events, Page: page}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response), nil
}

func (service *Service) ExportEvidence(ctx context.Context, request *connect.Request[tammyv1.ExportEvidenceRequest]) (*connect.Response[tammyv1.ExportEvidenceResponse], error) {
	if request == nil || request.Msg == nil || request.Msg.CommandContext == nil || request.Msg.CommandContext.Authentication == nil ||
		request.Msg.Filter == nil || request.Msg.Destination == nil || request.Msg.WorkspaceId != service.transactions.WorkspaceID() {
		return nil, ErrAuditService
	}
	if !service.appender.gate.EvidenceExportAllowed() {
		return nil, ErrWriteGate
	}
	var response *tammyv1.ExportEvidenceResponse
	err := service.transactions.Mutate(ctx, func(executor ServiceTransaction) error {
		if err := service.access.Require(ctx, executor, request.Msg.CommandContext.Authentication, authorisation.ActionExportAudit); err != nil {
			return err
		}
		scope := idempotency.Scope{WorkspaceID: request.Msg.WorkspaceId,
			ActorUserID: request.Msg.CommandContext.Authentication.ActorUserId,
			RPCName:     "tammy.v1.AuditService.ExportEvidence", OperationKey: request.Msg.CommandContext.IdempotencyKey}
		election, err := service.elector.Elect(ctx, executor, scope, request.Msg)
		if err != nil {
			return mapIdempotencyServiceError(err, scope.OperationKey)
		}
		if election.Decision == idempotency.DecisionReplay {
			response = &tammyv1.ExportEvidenceResponse{}
			return (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(election.ResultProto, response)
		}
		jobID, err := service.newID()
		if err != nil {
			return err
		}
		header, err := LoadChainHeader(ctx, executor, request.Msg.WorkspaceId, 0)
		if err != nil {
			return err
		}
		job, err := EnqueueExportJob(ctx, executor, ExportJobSpec{ID: jobID, WorkspaceID: request.Msg.WorkspaceId,
			OperationKey: scope.OperationKey, OperationHash: election.NormalizedHash[:], InputHash: election.NormalizedHash[:],
			Filter: proto.Clone(request.Msg.Filter).(*tammyv1.AuditEventFilter), SnapshotGeneration: header.Generation,
			SnapshotSequence: header.CurrentSequence, SnapshotHead: header.CurrentHead[:],
			DestinationProvider: "approved_file", EvidenceProvider: "audit_chain", DestinationCapability: request.Msg.Destination.CapabilityId,
			Progress: &tammyv1.JobProgress{Stage: "COLLECTING"}, CreatedAt: service.clock.Now().UTC()})
		if err != nil {
			return err
		}
		response = &tammyv1.ExportEvidenceResponse{Job: exportJobProjection(job)}
		resultProto, err := service.elector.Complete(ctx, executor, election, response, job.ID)
		if err != nil {
			return err
		}
		commandID, err := service.newID()
		if err != nil {
			return err
		}
		return service.appendEvidenceExportEvent(ctx, executor, request.Msg.CommandContext, election,
			job, commandID, "", "QUEUED", "tammy.v1.AuditService.ExportEvidence", resultProto)
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response), nil
}

func (service *Service) CancelAuditExport(ctx context.Context, request *connect.Request[tammyv1.CancelAuditExportRequest]) (*connect.Response[tammyv1.CancelAuditExportResponse], error) {
	if request == nil || request.Msg == nil || request.Msg.CommandContext == nil || request.Msg.CommandContext.Authentication == nil || request.Msg.ExpectedVersion == 0 {
		return nil, ErrAuditService
	}
	if !service.appender.gate.EvidenceExportAllowed() {
		return nil, ErrWriteGate
	}
	var response *tammyv1.CancelAuditExportResponse
	err := service.transactions.Mutate(ctx, func(executor ServiceTransaction) error {
		if err := service.access.Require(ctx, executor, request.Msg.CommandContext.Authentication, authorisation.ActionExportAudit); err != nil {
			return err
		}
		scope := idempotency.Scope{WorkspaceID: service.transactions.WorkspaceID(), ActorUserID: request.Msg.CommandContext.Authentication.ActorUserId,
			RPCName: "tammy.v1.AuditService.CancelAuditExport", OperationKey: request.Msg.CommandContext.IdempotencyKey}
		election, err := service.elector.Elect(ctx, executor, scope, request.Msg)
		if err != nil {
			return mapIdempotencyServiceError(err, scope.OperationKey)
		}
		if election.Decision == idempotency.DecisionReplay {
			response = &tammyv1.CancelAuditExportResponse{}
			return (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(election.ResultProto, response)
		}
		job, err := LoadExportJob(ctx, executor, request.Msg.JobId)
		if err != nil || job.WorkspaceID != service.transactions.WorkspaceID() {
			return ErrExportJobConflict
		}
		if job.RenameCommitted || job.State == tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_COMPLETED ||
			job.Stage == "DESTINATION_COMMITTING" || job.Stage == "COMMIT_DESTINATION_REAPPROVAL" {
			return exportCommitAlreadyCompletedError(job)
		}
		if job.Version != request.Msg.ExpectedVersion {
			return ErrExportJobConflict
		}
		fromState := exportJobStateName(job.State)
		if err := RequestExportCancellation(ctx, executor, job.ID, service.clock.Now().UTC()); err != nil {
			if current, ok := exportCommitPointJob(err); ok {
				return exportCommitAlreadyCompletedError(current)
			}
			if errors.Is(err, ErrExportCommitAlreadyCompleted) {
				return exportCommitAlreadyCompletedError(job)
			}
			return err
		}
		job, err = LoadExportJob(ctx, executor, job.ID)
		if err != nil {
			return err
		}
		response = &tammyv1.CancelAuditExportResponse{Job: exportJobProjection(job)}
		resultProto, err := service.elector.Complete(ctx, executor, election, response, job.ID)
		if err != nil {
			return err
		}
		commandID, err := service.newID()
		if err != nil {
			return err
		}
		return service.appendEvidenceExportEvent(ctx, executor, request.Msg.CommandContext, election,
			job, commandID, fromState, exportJobStateName(job.State), "tammy.v1.AuditService.CancelAuditExport", resultProto)
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response), nil
}

func (service *Service) GetAuditExportJob(ctx context.Context, request *connect.Request[tammyv1.GetAuditExportJobRequest]) (*connect.Response[tammyv1.GetAuditExportJobResponse], error) {
	if request == nil || request.Msg == nil || request.Msg.Authentication == nil {
		return nil, ErrAuditService
	}
	var response *tammyv1.GetAuditExportJobResponse
	err := service.transactions.Read(ctx, func(executor ServiceTransaction) error {
		if err := service.access.Require(ctx, executor, request.Msg.Authentication, authorisation.ActionReadAudit); err != nil {
			return err
		}
		job, err := LoadExportJob(ctx, executor, request.Msg.JobId)
		if err != nil || job.WorkspaceID != service.transactions.WorkspaceID() {
			return ErrExportJob
		}
		response = &tammyv1.GetAuditExportJobResponse{Job: exportJobProjection(job)}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response), nil
}

func (service *Service) ListAuditExportJobs(ctx context.Context, request *connect.Request[tammyv1.ListAuditExportJobsRequest]) (*connect.Response[tammyv1.ListAuditExportJobsResponse], error) {
	if request == nil || request.Msg == nil || request.Msg.Authentication == nil || request.Msg.Page == nil ||
		request.Msg.Page.PageSize == 0 || request.Msg.Page.PageSize > 200 {
		return nil, ErrAuditService
	}
	var response *tammyv1.ListAuditExportJobsResponse
	err := service.transactions.Read(ctx, func(executor ServiceTransaction) error {
		if err := service.access.Require(ctx, executor, request.Msg.Authentication, authorisation.ActionReadAudit); err != nil {
			return err
		}
		jobs, err := ListExportJobs(ctx, executor, service.transactions.WorkspaceID(), request.Msg.GetState())
		if err != nil {
			return err
		}
		queryHash := sha256.Sum256([]byte(fmt.Sprintf("audit-jobs\x00%s\x00%d", service.transactions.WorkspaceID(), request.Msg.GetState())))
		position := ""
		snapshot := exportJobSnapshot(jobs)
		if request.Msg.Page.Cursor != nil {
			cursor, decodeErr := service.cursors.Decode(*request.Msg.Page.Cursor, queryHash)
			if decodeErr != nil || cursor.Snapshot != snapshot {
				return ErrAuditService
			}
			position = cursor.Position
		}
		start, err := exportJobPageStart(jobs, position)
		if err != nil {
			return err
		}
		jobs = jobs[start:]
		limit := int(request.Msg.Page.PageSize)
		more := len(jobs) > limit
		if more {
			jobs = jobs[:limit]
		}
		projections := make([]*tammyv1.AuditExportJob, len(jobs))
		for index := range jobs {
			projections[index] = exportJobProjection(jobs[index])
		}
		page := &tammyv1.PageInfo{ReturnedCount: uint32(len(jobs))}
		if more {
			token, encodeErr := service.cursors.Encode(paging.Cursor{Snapshot: snapshot, Position: jobs[len(jobs)-1].ID, QueryHash: queryHash})
			if encodeErr != nil {
				return encodeErr
			}
			page.NextCursor = &token
		}
		response = &tammyv1.ListAuditExportJobsResponse{Jobs: projections, Page: page}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response), nil
}

func exportJobPageStart(jobs []ExportJob, position string) (int, error) {
	if position == "" {
		return 0, nil
	}
	for index := range jobs {
		if jobs[index].ID == position {
			return index + 1, nil
		}
	}
	return 0, ErrAuditService
}

func exportJobSnapshot(jobs []ExportJob) string {
	digest := sha256.New()
	_, _ = digest.Write([]byte("tammy.audit.export-jobs-snapshot.v1\x00"))
	for index := range jobs {
		_, _ = digest.Write([]byte(jobs[index].ID))
		_, _ = digest.Write([]byte{0})
		_, _ = digest.Write([]byte(strconv.FormatUint(jobs[index].Version, 10)))
		_, _ = digest.Write([]byte{0})
	}
	return fmt.Sprintf("%x", digest.Sum(nil))
}

func exportJobProjection(job ExportJob) *tammyv1.AuditExportJob {
	projection := &tammyv1.AuditExportJob{Id: job.ID, Version: job.Version, OperationKey: job.OperationKey,
		State: job.State, Progress: proto.Clone(job.Progress).(*tammyv1.JobProgress), CreatedAt: timestamppb.New(job.CreatedAt)}
	if job.CompletedAt != nil {
		projection.CompletedAt = timestamppb.New(*job.CompletedAt)
	}
	if len(job.DestinationHash) == sha256.Size {
		projection.DestinationHash = append([]byte(nil), job.DestinationHash...)
	}
	if job.SigningKeyID != "" {
		projection.SigningKeyId = proto.String(job.SigningKeyID)
	}
	return projection
}

func storedEventSnapshotFromHeader(header ChainHeader) StoredEventSnapshot {
	return StoredEventSnapshot{WorkspaceID: header.WorkspaceID, Generation: header.Generation,
		EndSequence: header.CurrentSequence, EndHead: header.CurrentHead}
}

func formatAuditEventSnapshot(snapshot StoredEventSnapshot) string {
	return strconv.FormatUint(snapshot.Generation, 10) + ":" + strconv.FormatUint(snapshot.EndSequence, 10) + ":" +
		hex.EncodeToString(snapshot.EndHead[:])
}

func parseAuditEventSnapshot(value, workspaceID string) (StoredEventSnapshot, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 3 || workspaceID == "" || len(parts[2]) != sha256.Size*2 {
		return StoredEventSnapshot{}, ErrAuditService
	}
	generation, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil || generation == 0 || strconv.FormatUint(generation, 10) != parts[0] {
		return StoredEventSnapshot{}, ErrAuditService
	}
	endSequence, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil || strconv.FormatUint(endSequence, 10) != parts[1] {
		return StoredEventSnapshot{}, ErrAuditService
	}
	head, err := hex.DecodeString(parts[2])
	if err != nil || hex.EncodeToString(head) != parts[2] || len(head) != sha256.Size {
		return StoredEventSnapshot{}, ErrAuditService
	}
	snapshot := StoredEventSnapshot{WorkspaceID: workspaceID, Generation: generation, EndSequence: endSequence}
	copy(snapshot.EndHead[:], head)
	if snapshot.EndHead == ([sha256.Size]byte{}) {
		return StoredEventSnapshot{}, ErrAuditService
	}
	return snapshot, nil
}

func parseCanonicalAuditCursorPosition(value string, snapshotEnd uint64) (uint64, error) {
	position, err := strconv.ParseUint(value, 10, 64)
	if err != nil || position == 0 || position > snapshotEnd || strconv.FormatUint(position, 10) != value {
		return 0, ErrAuditService
	}
	return position, nil
}

func verifyAuditSnapshot(
	ctx context.Context,
	executor Executor,
	currentHeader ChainHeader,
	snapshot StoredEventSnapshot,
	captureSequence uint64,
) (response *tammyv1.VerifyChainResponse, capturedHead [sha256.Size]byte, resultErr error) {
	if currentHeader.WorkspaceID != snapshot.WorkspaceID || currentHeader.Generation != snapshot.Generation ||
		currentHeader.CurrentSequence < snapshot.EndSequence || captureSequence > snapshot.EndSequence {
		return nil, [sha256.Size]byte{}, ErrAuditService
	}
	verifiedHeader := currentHeader
	verifiedHeader.CurrentSequence = snapshot.EndSequence
	verifiedHeader.CurrentHead = snapshot.EndHead
	verifier, err := NewStreamingStoredChainVerifier(ctx, verifiedHeader)
	if err != nil {
		return nil, [sha256.Size]byte{}, err
	}
	defer func() {
		if err := verifier.Close(); err != nil && resultErr == nil {
			resultErr = fmt.Errorf("%w: close streaming verifier", ErrRepository)
		}
	}()
	checkpoint := StoredEventCheckpoint{Head: currentHeader.GenesisHash}
	if captureSequence == 0 {
		capturedHead = currentHeader.GenesisHash
	}
	for checkpoint.AfterSequence < snapshot.EndSequence {
		page, err := LoadStoredEventPage(ctx, executor, snapshot, checkpoint,
			StoredEventPageSizeLimit, StoredEventPageByteBudget)
		if err != nil {
			if errors.Is(err, ErrInvalidEvent) {
				return invalidVerification(checkpoint.AfterSequence, checkpoint.AfterSequence+1, checkpoint.Head),
					capturedHead, nil
			}
			return nil, [sha256.Size]byte{}, err
		}
		if len(page.Events) == 0 || page.Checkpoint.AfterSequence <= checkpoint.AfterSequence {
			return nil, [sha256.Size]byte{}, fmt.Errorf("%w: streaming verifier made no progress", ErrRepository)
		}
		if err := verifier.AcceptPage(page.Events); err != nil {
			if terminalErr := verifier.TerminalError(); terminalErr != nil {
				return nil, [sha256.Size]byte{}, terminalErr
			}
			return verifier.Finish(), capturedHead, nil
		}
		for _, stored := range page.Events {
			if stored.Event.Sequence == captureSequence {
				copy(capturedHead[:], stored.Event.EventHash)
			}
		}
		checkpoint = page.Checkpoint
		if !page.HasMore && checkpoint.AfterSequence != snapshot.EndSequence {
			return nil, [sha256.Size]byte{}, fmt.Errorf("%w: incomplete streaming snapshot", ErrRepository)
		}
	}
	response = verifier.Finish()
	if terminalErr := verifier.TerminalError(); terminalErr != nil {
		return nil, [sha256.Size]byte{}, terminalErr
	}
	return response, capturedHead, nil
}

func auditEventQueryHash(workspaceID string, filter *tammyv1.AuditEventFilter) ([sha256.Size]byte, error) {
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(filter)
	if err != nil {
		return [sha256.Size]byte{}, ErrAuditService
	}
	digest := sha256.New()
	_, _ = digest.Write([]byte("tammy.audit.list-query.v1\x00"))
	_, _ = digest.Write([]byte(workspaceID))
	_, _ = digest.Write(encoded)
	var result [sha256.Size]byte
	copy(result[:], digest.Sum(nil))
	return result, nil
}

func filterStoredEvents(events []StoredEvent, filter *tammyv1.AuditEventFilter, after uint64) []StoredEvent {
	types := make(map[tammyv1.AuditEventType]struct{}, len(filter.EventTypes))
	for _, eventType := range filter.EventTypes {
		types[eventType] = struct{}{}
	}
	result := make([]StoredEvent, 0, len(events))
	for _, stored := range events {
		event := stored.Event
		if event.Sequence <= after || filter.StartSequence != nil && event.Sequence < *filter.StartSequence ||
			filter.EndSequence != nil && event.Sequence > *filter.EndSequence || len(types) != 0 && !containsEventType(types, event.Type) ||
			filter.ActorUserId != nil && (event.Actor == nil || event.Actor.ActorUserId != *filter.ActorUserId) ||
			filter.FromTime != nil && event.OccurredAt.AsTime().Before(filter.FromTime.AsTime()) ||
			filter.ToTime != nil && event.OccurredAt.AsTime().After(filter.ToTime.AsTime()) {
			continue
		}
		result = append(result, stored)
	}
	return result
}

func containsEventType(types map[tammyv1.AuditEventType]struct{}, eventType tammyv1.AuditEventType) bool {
	_, ok := types[eventType]
	return ok
}

func (service *Service) appendEvidenceExportEvent(
	ctx context.Context,
	executor Executor,
	command *tammyv1.CommandContext,
	election idempotency.Election,
	job ExportJob,
	commandID, fromState, toState, commandType string,
	resultProto []byte,
) error {
	eventID, err := service.newID()
	if err != nil {
		return err
	}
	payload := &tammyv1.EvidenceExportChangedEvent{JobId: job.ID, ToState: toState}
	if fromState != "" {
		payload.FromState = &fromState
	}
	if len(job.DestinationHash) == sha256.Size {
		payload.DestinationHash = append([]byte(nil), job.DestinationHash...)
	}
	payloadProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(payload)
	if err != nil {
		return err
	}
	resultHash := sha256.Sum256(resultProto)
	idempotencyKey := command.IdempotencyKey
	event := &tammyv1.AuditEvent{Id: eventID, WorkspaceId: job.WorkspaceID,
		Type:       tammyv1.AuditEventType_AUDIT_EVENT_TYPE_EVIDENCE_EXPORT_CHANGED,
		OccurredAt: timestamppb.New(service.clock.Now().UTC()), Actor: proto.Clone(command.Authentication).(*tammyv1.AuthenticationContext),
		Source:                   &tammyv1.SourceRef{Type: "evidence_export", Id: job.ID, Revision: job.Version, ContentHash: election.NormalizedHash[:]},
		Payload:                  &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_EvidenceExportChanged{EvidenceExportChanged: payload}},
		PayloadSchemaFingerprint: append([]byte(nil), service.schemaFingerprint...), CommandId: &commandID,
		CommandType: commandType, IdempotencyKey: &idempotencyKey,
		AffectedResources: []*tammyv1.SourceRef{{Type: "evidence_export", Id: job.ID, Revision: job.Version, ContentHash: election.NormalizedHash[:]}},
		AfterSemanticHash: election.NormalizedHash[:],
		Result: &tammyv1.AuditResultMetadata{TypeName: string(responseTypeForCommand(commandType).ProtoReflect().Descriptor().FullName()),
			DeterministicSha256: resultHash[:], OutcomeCode: "OK"},
	}
	_, err = service.appender.AppendEvidence(ctx, executor, event, payloadProto)
	return err
}

func mapIdempotencyServiceError(err error, operationKey string) error {
	if errors.Is(err, idempotency.ErrConflict) {
		return faults.New(faults.CodeIdempotencyConflict, map[string]string{"operation_key": operationKey})
	}
	if !errors.Is(err, idempotency.ErrAborted) {
		return err
	}
	connectErr := connect.NewError(connect.CodeAborted, errors.New("command remains in flight"))
	detail, detailErr := connect.NewErrorDetail(&tammyv1.ErrorContext{Code: "COMMAND_IN_FLIGHT",
		Category:    tammyv1.ErrorCategory_ERROR_CATEGORY_CONFLICT,
		SafeSummary: "The same command is still running.", Remediation: "Retry the same operation key.",
		Retry: tammyv1.RetryClassification_RETRY_CLASSIFICATION_SAFE})
	if detailErr == nil {
		connectErr.AddDetail(detail)
	}
	return connectErr
}

func exportCommitAlreadyCompletedError(job ExportJob) error {
	currentState := exportJobStateName(job.State)
	if job.Stage == "DESTINATION_COMMITTING" || job.Stage == "COMMIT_DESTINATION_REAPPROVAL" {
		currentState = job.Stage
	}
	connectErr := connect.NewError(connect.CodeFailedPrecondition, ErrExportCommitAlreadyCompleted)
	detail, err := connect.NewErrorDetail(&tammyv1.InvalidStateTransitionErrorDetail{
		Context: &tammyv1.ErrorContext{
			Code:        "COMMIT_ALREADY_COMPLETED",
			Category:    tammyv1.ErrorCategory_ERROR_CATEGORY_CONFLICT,
			SafeSummary: "The evidence export commit point has already been reached.",
			Remediation: "Refresh the export job to inspect the committed outcome.",
			Retry:       tammyv1.RetryClassification_RETRY_CLASSIFICATION_NEVER,
		},
		Resource:     "audit_export_job",
		CurrentState: currentState,
	})
	if err == nil {
		connectErr.AddDetail(detail)
	}
	return connectErr
}

func responseTypeForCommand(commandType string) proto.Message {
	if commandType == "tammy.v1.AuditService.CancelAuditExport" {
		return &tammyv1.CancelAuditExportResponse{}
	}
	return &tammyv1.ExportEvidenceResponse{}
}
