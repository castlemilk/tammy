//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"math"
	"time"

	"buf.build/go/protovalidate"
	"github.com/tammyapp/tammy/services/core/internal/app"
	"github.com/tammyapp/tammy/services/core/internal/audit"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// BAS commands append directly to the existing hash-linked chain. This adapter
// intentionally does not write a BAS-specific audit side table.
type ordinaryBASAuditAppender struct {
	appender    *audit.Appender
	gate        *audit.WriteGate
	workspaceID string
	fingerprint []byte
	newID       func() (string, error)
	reconcile   func(context.Context, workspace.MutationExecutor) error
}

func newOrdinaryBASAuditAppender(activation app.LocalWorkspaceActivation) (*ordinaryBASAuditAppender, error) {
	if activation.AuditAppender == nil || activation.AuditGate == nil ||
		len(activation.AuditSchemaFingerprint) != sha256.Size || activation.NewID == nil ||
		!ids.IsCanonicalV7(activation.WorkspaceID) {
		return nil, ErrLedgerModule
	}
	return &ordinaryBASAuditAppender{
		appender: activation.AuditAppender, gate: activation.AuditGate, workspaceID: activation.WorkspaceID,
		fingerprint: append([]byte(nil), activation.AuditSchemaFingerprint...), newID: activation.NewID,
		reconcile: activation.ReconcileAuditWithin,
	}, nil
}

func (port *ordinaryBASAuditAppender) RequireCoverageWithin(ctx context.Context, executor app.CommandSQLExecutor) error {
	if port == nil || executor == nil {
		return app.ErrLocalAuditCoverageUnavailable
	}
	if port.gate != nil && !port.gate.Writable() && port.reconcile != nil {
		if err := port.reconcile(ctx, executor); err != nil {
			return err
		}
	}
	return port.RequireCoverage()
}
func (port *ordinaryBASAuditAppender) RequireCoverage() error {
	if port == nil || port.appender == nil || port.gate == nil || !port.gate.Writable() {
		return app.ErrLocalAuditCoverageUnavailable
	}
	return nil
}

type basPreparationAuditFactory struct {
	workspaceID string
	fingerprint []byte
	now         func() time.Time
	newID       func() (string, error)
}

func (factory basPreparationAuditFactory) Build(_ context.Context, operation app.OrdinaryOperation,
	authentication *tammyv1.AuthenticationContext, operationKey, resourceID string,
	result proto.Message, payload proto.Message) (app.CommandResult, error) {
	typed, ok := payload.(*tammyv1.BasPreparationChangedEvent)
	if !ok || authentication == nil || result == nil || factory.now == nil || factory.newID == nil ||
		!ids.IsCanonicalV7(factory.workspaceID) || !ids.IsCanonicalV7(operationKey) ||
		len(factory.fingerprint) != sha256.Size || !ids.IsCanonicalV7(resourceID) {
		return app.CommandResult{}, ErrLedgerModule
	}
	if err := protovalidate.Validate(typed); err != nil {
		return app.CommandResult{}, ErrLedgerModule
	}
	action, ok := basPreparationActionForOperation(operation)
	if !ok || typed.Action != action {
		return app.CommandResult{}, ErrLedgerModule
	}
	organisationID, resultSource, err := basPreparationAuditResult(result)
	if err != nil || !ids.IsCanonicalV7(organisationID) || !sameLocalBASRef(typed.Source, resultSource) ||
		typed.Source.Id != resourceID || !validLocalBASAuditRef(typed.Source) {
		return app.CommandResult{}, ErrLedgerModule
	}
	if typed.Previous != nil {
		if !validLocalBASAuditRef(typed.Previous) || typed.Previous.Type != typed.Source.Type {
			return app.CommandResult{}, ErrLedgerModule
		}
	}
	if len(typed.AffectedResources) > 8 {
		return app.CommandResult{}, ErrLedgerModule
	}
	seen := map[string]bool{}
	for _, ref := range typed.AffectedResources {
		if !validLocalBASAuditRef(ref) {
			return app.CommandResult{}, ErrLedgerModule
		}
		key := ref.Type + "\x00" + ref.Id + "\x00" + string(ref.ContentHash)
		if seen[key] {
			return app.CommandResult{}, ErrLedgerModule
		}
		seen[key] = true
	}
	resultBytes, err := (proto.MarshalOptions{Deterministic: true}).Marshal(result)
	if err != nil {
		return app.CommandResult{}, ErrLedgerModule
	}
	resultHash := sha256.Sum256(resultBytes)
	payloadBytes, err := (proto.MarshalOptions{Deterministic: true}).Marshal(typed)
	if err != nil {
		return app.CommandResult{}, ErrLedgerModule
	}
	eventID, err := factory.newID()
	if err != nil || !ids.IsCanonicalV7(eventID) {
		return app.CommandResult{}, ErrLedgerModule
	}
	commandID := operationKey
	occurred := timestamppb.New(factory.now().UTC())
	event := &tammyv1.AuditEvent{
		Id: eventID, WorkspaceId: factory.workspaceID, OrganisationId: &organisationID,
		CommandId: &commandID, Actor: proto.Clone(authentication).(*tammyv1.AuthenticationContext),
		CommandType: string(operation), IdempotencyKey: &operationKey,
		Type:       tammyv1.AuditEventType_AUDIT_EVENT_TYPE_BAS_PREPARATION_CHANGED,
		OccurredAt: occurred, Source: cloneLocalBASRef(typed.Source),
		PayloadSchemaFingerprint: append([]byte(nil), factory.fingerprint...),
		Payload: &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_BasPreparationChanged{
			BasPreparationChanged: proto.Clone(typed).(*tammyv1.BasPreparationChangedEvent),
		}},
		AfterSemanticHash: append([]byte(nil), typed.Source.ContentHash...),
		Result: &tammyv1.AuditResultMetadata{
			TypeName:            string(result.ProtoReflect().Descriptor().FullName()),
			DeterministicSha256: append([]byte(nil), resultHash[:]...), OutcomeCode: "SUCCESS",
		},
	}
	if typed.Previous != nil {
		event.BeforeSemanticHash = append([]byte(nil), typed.Previous.ContentHash...)
	}
	event.AffectedResources = append(event.AffectedResources, cloneLocalBASRef(typed.Source))
	if typed.Previous != nil {
		event.AffectedResources = append(event.AffectedResources, cloneLocalBASRef(typed.Previous))
	}
	for _, ref := range typed.AffectedResources {
		event.AffectedResources = append(event.AffectedResources, cloneLocalBASRef(ref))
	}
	return app.CommandResult{Result: result, ResourceID: resourceID, AuditPayload: payloadBytes, AuditEvent: event}, nil
}

type basPreparationCommandAuditor struct{ port *ordinaryBASAuditAppender }

func (auditor basPreparationCommandAuditor) Append(ctx context.Context, executor app.CommandSQLExecutor,
	event *tammyv1.AuditEvent, payload []byte) error {
	if auditor.port == nil || executor == nil || event == nil || event.Actor == nil ||
		event.IdempotencyKey == nil || event.Result == nil || event.Payload == nil ||
		len(payload) == 0 || len(payload) > 1<<20 || event.WorkspaceId != auditor.port.workspaceID ||
		event.Type != tammyv1.AuditEventType_AUDIT_EVENT_TYPE_BAS_PREPARATION_CHANGED ||
		!bytes.Equal(event.PayloadSchemaFingerprint, auditor.port.fingerprint) {
		return ErrLedgerModule
	}
	action, ok := basPreparationActionForOperation(app.OrdinaryOperation(event.CommandType))
	body := event.Payload.GetBasPreparationChanged()
	if !ok || body == nil || body.Action != action || !sameLocalBASRef(event.Source, body.Source) {
		return ErrLedgerModule
	}
	decoded := &tammyv1.BasPreparationChangedEvent{}
	if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(payload, decoded); err != nil ||
		!proto.Equal(decoded, body) {
		return ErrLedgerModule
	}
	if err := auditor.port.RequireCoverage(); err != nil {
		return err
	}
	_, err := auditor.port.appender.Append(ctx, executor, event, payload)
	return err
}

func basPreparationActionForOperation(operation app.OrdinaryOperation) (tammyv1.BasPreparationAuditAction, bool) {
	switch operation {
	case app.OrdinaryOperationSetBasObligationsProfile:
		return tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_PROFILE_SET, true
	case app.OrdinaryOperationConfirmBasPeriod:
		return tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_PERIOD_CONFIRMED, true
	case app.OrdinaryOperationSaveBasPaygInputs:
		return tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_PAYG_INPUTS_SAVED, true
	case app.OrdinaryOperationRegenerateBasPreparation:
		return tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_PREPARATION_REGENERATED, true
	case app.OrdinaryOperationValidateBasPreparation:
		return tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_VALIDATION_RECORDED, true
	case app.OrdinaryOperationFreezeBasForManualHandoff:
		return tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_FROZEN_FOR_MANUAL_HANDOFF, true
	case app.OrdinaryOperationRecordExternalBasObservation:
		return tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_EXTERNAL_OBSERVATION_RECORDED, true
	case app.OrdinaryOperationCorrectExternalBasObservation:
		return tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_EXTERNAL_OBSERVATION_CORRECTED, true
	case app.OrdinaryOperationResolveExternalBasDispute:
		return tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_EXTERNAL_DISPUTE_RESOLVED, true
	case app.OrdinaryOperationRecordBasExportEvent:
		return tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_EXPORT_EVENT_RECORDED, true
	case app.OrdinaryOperationCreateBasReplacement:
		return tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_EXTERNAL_REPLACEMENT_CREATED, true
	case app.OrdinaryOperationCreateBasRevision:
		return tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_REVISION_CREATED, true
	case app.OrdinaryOperationReplaceRejectedBasAttempt:
		return tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_REJECTED_ATTEMPT_REPLACED, true
	case app.OrdinaryOperationAcknowledgeBasWarnings:
		return tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_WARNINGS_ACKNOWLEDGED, true
	default:
		return tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_UNSPECIFIED, false
	}
}

func basPreparationAuditResult(result proto.Message) (string, *tammyv1.SourceRef, error) {
	switch value := result.(type) {
	case *tammyv1.SetBasObligationsProfileResponse:
		if value.Profile == nil {
			break
		}
		return value.Profile.OrganisationId, &tammyv1.SourceRef{Type: "bas_obligations_profile", Id: value.Profile.ProfileId,
			Revision: value.Profile.Revision, ContentHash: append([]byte(nil), value.Profile.ContentHash...)}, nil
	case *tammyv1.ConfirmBasPeriodResponse:
		if value.Period == nil {
			break
		}
		return value.Period.OrganisationId, &tammyv1.SourceRef{Type: "bas_period", Id: value.Period.PeriodId,
			Revision: value.Period.Version, ContentHash: append([]byte(nil), value.Period.ContentHash...)}, nil
	case *tammyv1.SaveBasPaygInputsResponse:
		if value.Preparation == nil {
			break
		}
		return value.Preparation.OrganisationId, preparationAuditReference(value.Preparation), nil
	case *tammyv1.ValidateBasPreparationResponse:
		if value.Preparation == nil || value.Validation == nil {
			break
		}
		return value.Preparation.OrganisationId, preparationAuditReference(value.Preparation), nil
	case *tammyv1.FreezeBasForManualHandoffResponse:
		if value.Preparation == nil || value.FrozenBas == nil {
			break
		}
		return value.Preparation.OrganisationId, preparationAuditReference(value.Preparation), nil
	case *tammyv1.RecordExternalBasObservationResponse:
		if value.Observation == nil || value.Preparation == nil {
			break
		}
		return value.Observation.OrganisationId, &tammyv1.SourceRef{Type: "bas_external_observation", Id: value.Observation.ObservationId, Revision: 1, ContentHash: append([]byte(nil), value.Observation.ContentHash...)}, nil
	case *tammyv1.CorrectExternalBasObservationResponse:
		if value.Correction == nil || value.Preparation == nil {
			break
		}
		return value.Correction.OrganisationId, &tammyv1.SourceRef{Type: "bas_external_observation", Id: value.Correction.ObservationId, Revision: 1, ContentHash: append([]byte(nil), value.Correction.ContentHash...)}, nil
	case *tammyv1.ResolveExternalBasDisputeResponse:
		if value.Resolution == nil || value.Preparation == nil {
			break
		}
		return value.Preparation.OrganisationId, &tammyv1.SourceRef{Type: "bas_external_dispute_resolution", Id: value.Resolution.ResolutionId, Revision: 1, ContentHash: append([]byte(nil), value.Resolution.ContentHash...)}, nil
	case *tammyv1.RecordBasExportEventResponse:
		if value.Event == nil {
			break
		}
		return value.Event.OrganisationId, &tammyv1.SourceRef{Type: "bas_export_event", Id: value.Event.ExportEventId, Revision: 1, ContentHash: append([]byte(nil), value.Event.ContentHash...)}, nil
	case *tammyv1.CreateBasReplacementResponse:
		if value.Replacement == nil {
			break
		}
		return value.Replacement.OrganisationId, preparationAuditReference(value.Replacement), nil
	case *tammyv1.CreateBasRevisionResponse:
		if value.Revision == nil {
			break
		}
		return value.Revision.OrganisationId, preparationAuditReference(value.Revision), nil
	case *tammyv1.ReplaceRejectedBasAttemptResponse:
		if value.ReplacementAttempt == nil {
			break
		}
		return value.ReplacementAttempt.OrganisationId, preparationAuditReference(value.ReplacementAttempt), nil
	case *tammyv1.AcknowledgeBasWarningsResponse:
		if value.Preparation == nil || len(value.Acknowledgements) == 0 {
			break
		}
		return value.Preparation.OrganisationId, preparationAuditReference(value.Preparation), nil
	case *tammyv1.RegenerateBasPreparationResponse:
		if value.Preparation == nil || value.Calculation == nil {
			break
		}
		return value.Preparation.OrganisationId, preparationAuditReference(value.Preparation), nil
	}
	return "", nil, errors.New("local product: malformed BAS audit result")
}
func preparationAuditReference(value *tammyv1.BasPreparation) *tammyv1.SourceRef {
	return &tammyv1.SourceRef{Type: "bas_preparation", Id: value.PreparationId, Revision: value.Version,
		ContentHash: append([]byte(nil), value.ContentHash...)}
}
func validLocalBASAuditRef(ref *tammyv1.SourceRef) bool {
	if ref == nil || !ids.IsCanonicalV7(ref.Id) || ref.Revision == 0 || ref.Revision > math.MaxInt64 ||
		len(ref.ContentHash) != sha256.Size {
		return false
	}
	switch ref.Type {
	case "bas_obligations_profile", "bas_period", "bas_preparation", "bas_saved_payg_inputs", "bas_calculation", "bas_workpaper", "bas_validation", "bas_warning_acknowledgement", "bas_validation_finding", "bas_frozen", "bas_family", "bas_branch", "bas_external_observation", "bas_external_dispute_resolution", "bas_export_event":
		return true
	default:
		return false
	}
}
func sameLocalBASRef(left, right *tammyv1.SourceRef) bool {
	return left != nil && right != nil && left.Type == right.Type && left.Id == right.Id &&
		left.Revision == right.Revision && bytes.Equal(left.ContentHash, right.ContentHash)
}
func cloneLocalBASRef(value *tammyv1.SourceRef) *tammyv1.SourceRef {
	if value == nil {
		return nil
	}
	return proto.Clone(value).(*tammyv1.SourceRef)
}
