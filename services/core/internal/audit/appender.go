// Package audit owns immutable hash-linked audit events and signed evidence export.
package audit

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/canonical"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/structpb"
)

const (
	genesisDomain                   = "tammy-audit-v1"
	eventDomain                     = "tammy-audit-event-v3"
	canonicalEventVersion           = "tammy.audit.canonical-event.v3"
	hiddenMetadataCommitmentDomain  = "tammy-audit-hidden-metadata-commitment-v2"
	payloadIdentityCommitmentDomain = "tammy-audit-payload-identity-commitment-v1"
	eventTypeCommitmentDomain       = "tammy-audit-event-type-commitment-v1"
	occurredAtCommitmentDomain      = "tammy-audit-occurred-at-commitment-v1"
	actorUserIDCommitmentDomain     = "tammy-audit-actor-user-id-commitment-v1"
	protobufTypeURLPrefix           = "type.googleapis.com/"
)

var (
	ErrInvalidChainInput = errors.New("audit: invalid chain input")
	ErrInvalidEvent      = errors.New("audit: invalid event")
	ErrWriteGate         = errors.New("audit: write gate denied operation")
	auditSourceType      = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	auditOutcomeCode     = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	auditPayloadIdentity = map[tammyv1.AuditEventType]payloadIdentity{
		tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_STATE_CHANGED:     {field: "workspace_state_changed", message: "tammy.v1.WorkspaceStateChangedEvent"},
		tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_TRUST_ESTABLISHED: {field: "workspace_trust_established", message: "tammy.v1.WorkspaceTrustEstablishedEvent"},
		tammyv1.AuditEventType_AUDIT_EVENT_TYPE_USER_STATE_CHANGED:          {field: "user_state_changed", message: "tammy.v1.UserStateChangedEvent"},
		tammyv1.AuditEventType_AUDIT_EVENT_TYPE_FACTOR_STATE_CHANGED:        {field: "factor_state_changed", message: "tammy.v1.FactorStateChangedEvent"},
		tammyv1.AuditEventType_AUDIT_EVENT_TYPE_ORGANISATION_CHANGED:        {field: "organisation_changed", message: "tammy.v1.OrganisationChangedEvent"},
		tammyv1.AuditEventType_AUDIT_EVENT_TYPE_ENTITY_VERIFICATION_CHANGED: {field: "entity_verification_changed", message: "tammy.v1.EntityVerificationChangedEvent"},
		tammyv1.AuditEventType_AUDIT_EVENT_TYPE_ACCOUNT_STATUS_CHANGED:      {field: "account_status_changed", message: "tammy.v1.AccountStatusChangedEvent"},
		tammyv1.AuditEventType_AUDIT_EVENT_TYPE_OPENING_CONVERSION_CHANGED:  {field: "opening_conversion_changed", message: "tammy.v1.OpeningConversionChangedEvent"},
		tammyv1.AuditEventType_AUDIT_EVENT_TYPE_JOURNAL_POSTED:              {field: "journal_posted", message: "tammy.v1.JournalPostedEvent"},
		tammyv1.AuditEventType_AUDIT_EVENT_TYPE_PERIOD_STATE_CHANGED:        {field: "period_state_changed", message: "tammy.v1.PeriodStateChangedEvent"},
		tammyv1.AuditEventType_AUDIT_EVENT_TYPE_BACKUP_JOB_CHANGED:          {field: "backup_job_changed", message: "tammy.v1.BackupJobChangedEvent"},
		tammyv1.AuditEventType_AUDIT_EVENT_TYPE_RESTORE_STATE_CHANGED:       {field: "restore_state_changed", message: "tammy.v1.RestoreStateChangedEvent"},
		tammyv1.AuditEventType_AUDIT_EVENT_TYPE_PRE_RESTORE_ARCHIVE_CHANGED: {field: "pre_restore_archive_changed", message: "tammy.v1.PreRestoreArchiveChangedEvent"},
		tammyv1.AuditEventType_AUDIT_EVENT_TYPE_EVIDENCE_EXPORT_CHANGED:     {field: "evidence_export_changed", message: "tammy.v1.EvidenceExportChangedEvent"},
		tammyv1.AuditEventType_AUDIT_EVENT_TYPE_SIGNING_KEY_ROTATED:         {field: "signing_key_rotated", message: "tammy.v1.SigningKeyRotatedEvent"},
		tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_RESTORED:          {field: "workspace_restored", message: "tammy.v1.WorkspaceRestoredEvent"},
	}
)

type payloadIdentity struct {
	field   protoreflect.Name
	message protoreflect.FullName
}

// StoredEvent contains the exact immutable bytes persisted for one event.
type StoredEvent struct {
	Event                  *tammyv1.AuditEvent
	PayloadType            string
	PayloadProto           []byte
	PayloadJSON            []byte
	AffectedResourcesProto []byte
	CanonicalEvent         []byte
	EventProto             []byte
}

// AfterCommitRegistrar is the lifecycle capability required by mirrored
// appends. It deliberately cannot commit or roll back the caller's SQL unit of
// work; it can only enqueue work that the owner runs after a successful commit.
type AfterCommitRegistrar interface {
	AfterCommit(func(context.Context) error) error
}

// Appender appends through caller-owned SQL only and has no commit capability.
// When configured with a mirror it also registers the new durable head with
// the caller's post-commit lifecycle.
type Appender struct {
	mirror    MirrorStore
	gate      *WriteGate
	publisher *mirrorPublisher
}

func NewMirroringAppender(mirror MirrorStore, gate *WriteGate) (*Appender, error) {
	if mirror == nil || gate == nil {
		return nil, ErrMirrorInvalid
	}
	publisher, err := gate.publisherFor(mirror)
	if err != nil {
		return nil, err
	}
	return &Appender{mirror: mirror, gate: gate, publisher: publisher}, nil
}

// Genesis computes SHA-256("tammy-audit-v1" || workspace_id || chain_salt).
func Genesis(workspaceID string, chainSalt []byte) ([sha256.Size]byte, error) {
	if workspaceID == "" || len(chainSalt) != sha256.Size {
		return [sha256.Size]byte{}, ErrInvalidChainInput
	}
	digest := sha256.New()
	_, _ = digest.Write([]byte(genesisDomain))
	_, _ = digest.Write([]byte(workspaceID))
	_, _ = digest.Write(chainSalt)
	var sum [sha256.Size]byte
	copy(sum[:], digest.Sum(nil))
	return sum, nil
}

// EventHash computes SHA-256("tammy-audit-event-v3" || predecessor ||
// uint64_be(len(canonical_event)) || canonical_event).
func EventHash(previous [sha256.Size]byte, canonicalEvent []byte) ([sha256.Size]byte, error) {
	if len(canonicalEvent) == 0 {
		return [sha256.Size]byte{}, ErrInvalidChainInput
	}
	length := make([]byte, 8)
	binary.BigEndian.PutUint64(length, uint64(len(canonicalEvent)))
	digest := sha256.New()
	_, _ = digest.Write([]byte(eventDomain))
	_, _ = digest.Write(previous[:])
	_, _ = digest.Write(length)
	_, _ = digest.Write(canonicalEvent)
	var sum [sha256.Size]byte
	copy(sum[:], digest.Sum(nil))
	return sum, nil
}

// PrepareEvent validates a typed Protobuf event, preserves the supplied exact
// payload bytes, and assigns its predecessor and domain-separated event hash.
// It never mutates the caller's message.
func PrepareEvent(previous [sha256.Size]byte, event *tammyv1.AuditEvent, payloadProto []byte) (StoredEvent, error) {
	return prepareEventWithBlindingSource(previous, event, payloadProto, rand.Reader)
}

func prepareEventWithBlindingSource(previous [sha256.Size]byte, event *tammyv1.AuditEvent, payloadProto []byte, blindingSource io.Reader) (StoredEvent, error) {
	if !validEventEnvelope(event) || event.CommitmentOpenings != nil || blindingSource == nil {
		return StoredEvent{}, ErrInvalidEvent
	}
	openings, err := generateCommitmentOpenings(blindingSource)
	if err != nil {
		return StoredEvent{}, err
	}
	return prepareEventWithCommitmentOpenings(previous, event, payloadProto, openings)
}

func reconstructEventWithStoredOpenings(previous [sha256.Size]byte, event *tammyv1.AuditEvent, payloadProto []byte) (StoredEvent, error) {
	if event == nil || !validCommitmentOpenings(event.CommitmentOpenings) {
		return StoredEvent{}, ErrInvalidEvent
	}
	withoutOpenings := proto.Clone(event).(*tammyv1.AuditEvent)
	openings := proto.Clone(event.CommitmentOpenings).(*tammyv1.AuditCommitmentOpenings)
	withoutOpenings.CommitmentOpenings = nil
	return prepareEventWithCommitmentOpenings(previous, withoutOpenings, payloadProto, openings)
}

func prepareEventWithCommitmentOpenings(previous [sha256.Size]byte, event *tammyv1.AuditEvent, payloadProto []byte,
	openings *tammyv1.AuditCommitmentOpenings) (StoredEvent, error) {
	if !validEventEnvelope(event) || event.CommitmentOpenings != nil || !validCommitmentOpenings(openings) {
		return StoredEvent{}, ErrInvalidEvent
	}
	working, ok := proto.Clone(event).(*tammyv1.AuditEvent)
	if !ok {
		return StoredEvent{}, ErrInvalidEvent
	}
	working.CommitmentOpenings = proto.Clone(openings).(*tammyv1.AuditCommitmentOpenings)
	payloadMessage, payloadType, err := selectedPayload(working.Type, working.Payload)
	if err != nil {
		return StoredEvent{}, err
	}
	if len(payloadProto) == 0 {
		payloadProto, err = proto.MarshalOptions{Deterministic: true}.Marshal(payloadMessage)
		if err != nil {
			return StoredEvent{}, fmt.Errorf("%w: marshal payload", ErrInvalidEvent)
		}
	} else {
		decoded := payloadMessage.ProtoReflect().New().Interface()
		if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(payloadProto, decoded); err != nil || !proto.Equal(decoded, payloadMessage) {
			return StoredEvent{}, fmt.Errorf("%w: payload bytes do not match typed payload", ErrInvalidEvent)
		}
		if _, err := canonical.NormalizedJSON(decoded); err != nil {
			return StoredEvent{}, fmt.Errorf("%w: payload canonicalization", ErrInvalidEvent)
		}
	}
	payloadJSON, err := canonical.NormalizedJSON(payloadMessage)
	if err != nil {
		return StoredEvent{}, fmt.Errorf("%w: payload canonicalization", ErrInvalidEvent)
	}
	working.PreviousHash = append([]byte(nil), previous[:]...)
	working.EventHash = nil
	chainView := proto.Clone(working).(*tammyv1.AuditEvent)
	chainView.PreviousHash = nil
	chainView.EventHash = nil
	chainView.Payload = nil
	chainView.PayloadSchemaFingerprint = nil
	canonicalEvent, err := canonicalEventEnvelope(chainView, payloadProto, payloadJSON, payloadType, working.PayloadSchemaFingerprint)
	if err != nil {
		return StoredEvent{}, fmt.Errorf("%w: event canonicalization", ErrInvalidEvent)
	}
	eventHash, err := EventHash(previous, canonicalEvent)
	if err != nil {
		return StoredEvent{}, err
	}
	working.EventHash = append([]byte(nil), eventHash[:]...)
	eventProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(working)
	if err != nil {
		return StoredEvent{}, fmt.Errorf("%w: marshal event", ErrInvalidEvent)
	}
	affectedResourcesProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(&tammyv1.AuditEvent{
		AffectedResources: working.AffectedResources,
	})
	if err != nil {
		return StoredEvent{}, fmt.Errorf("%w: marshal affected resources", ErrInvalidEvent)
	}
	return StoredEvent{
		Event: working, PayloadType: payloadType,
		PayloadProto: append([]byte(nil), payloadProto...), PayloadJSON: append([]byte(nil), payloadJSON...),
		AffectedResourcesProto: affectedResourcesProto,
		CanonicalEvent:         append([]byte(nil), canonicalEvent...), EventProto: eventProto,
	}, nil
}

func generateCommitmentOpenings(source io.Reader) (*tammyv1.AuditCommitmentOpenings, error) {
	openings := &tammyv1.AuditCommitmentOpenings{
		HiddenMetadataBlinding:  make([]byte, sha256.Size),
		PayloadIdentityBlinding: make([]byte, sha256.Size),
		EventTypeBlinding:       make([]byte, sha256.Size),
		OccurredAtBlinding:      make([]byte, sha256.Size),
		ActorUserIdBlinding:     make([]byte, sha256.Size),
	}
	for _, opening := range commitmentOpeningBytes(openings) {
		if _, err := io.ReadFull(source, opening); err != nil {
			return nil, fmt.Errorf("%w: generate commitment opening", ErrInvalidEvent)
		}
	}
	if !validCommitmentOpenings(openings) {
		return nil, ErrInvalidEvent
	}
	return openings, nil
}

func commitmentOpeningBytes(openings *tammyv1.AuditCommitmentOpenings) [][]byte {
	if openings == nil {
		return nil
	}
	return [][]byte{
		openings.HiddenMetadataBlinding,
		openings.PayloadIdentityBlinding,
		openings.EventTypeBlinding,
		openings.OccurredAtBlinding,
		openings.ActorUserIdBlinding,
	}
}

func validCommitmentOpenings(openings *tammyv1.AuditCommitmentOpenings) bool {
	seen := make(map[[sha256.Size]byte]struct{}, 5)
	for _, opening := range commitmentOpeningBytes(openings) {
		if len(opening) != sha256.Size || bytes.Equal(opening, make([]byte, sha256.Size)) {
			return false
		}
		var key [sha256.Size]byte
		copy(key[:], opening)
		if _, duplicate := seen[key]; duplicate {
			return false
		}
		seen[key] = struct{}{}
	}
	return len(seen) == 5
}

func addUniqueCommitmentOpenings(seen map[[sha256.Size]byte]struct{}, openings *tammyv1.AuditCommitmentOpenings) bool {
	if !validCommitmentOpenings(openings) {
		return false
	}
	for _, opening := range commitmentOpeningBytes(openings) {
		var key [sha256.Size]byte
		copy(key[:], opening)
		if _, duplicate := seen[key]; duplicate {
			return false
		}
		seen[key] = struct{}{}
	}
	return true
}

func canonicalEventEnvelope(eventMetadata *tammyv1.AuditEvent, payloadProto, payloadJSON []byte, payloadType string, schemaFingerprint []byte) ([]byte, error) {
	if eventMetadata == nil || len(payloadJSON) == 0 || payloadType == "" || len(schemaFingerprint) != sha256.Size ||
		eventMetadata.WorkspaceId == "" || eventMetadata.Generation == 0 || eventMetadata.Sequence == 0 ||
		eventMetadata.Type == tammyv1.AuditEventType_AUDIT_EVENT_TYPE_UNSPECIFIED || eventMetadata.OccurredAt == nil || !eventMetadata.OccurredAt.IsValid() ||
		!validCommitmentOpenings(eventMetadata.CommitmentOpenings) {
		return nil, ErrInvalidEvent
	}
	openings := eventMetadata.CommitmentOpenings
	hiddenMetadata := proto.Clone(eventMetadata).(*tammyv1.AuditEvent)
	hiddenMetadata.WorkspaceId = ""
	hiddenMetadata.Generation = 0
	hiddenMetadata.Sequence = 0
	hiddenMetadata.Type = tammyv1.AuditEventType_AUDIT_EVENT_TYPE_UNSPECIFIED
	hiddenMetadata.OccurredAt = nil
	hiddenMetadata.PreviousHash = nil
	hiddenMetadata.EventHash = nil
	hiddenMetadata.Payload = nil
	hiddenMetadata.PayloadSchemaFingerprint = nil
	hiddenMetadata.CommitmentOpenings = nil
	actorUserID := ""
	if hiddenMetadata.Actor != nil {
		actorUserID = hiddenMetadata.Actor.ActorUserId
		hiddenMetadata.Actor.ActorUserId = ""
	}
	hiddenMetadataJSON, err := canonical.NormalizedJSON(hiddenMetadata)
	if err != nil {
		return nil, err
	}
	eventTypeValue := strconv.FormatInt(int64(eventMetadata.Type), 10)
	occurredAtValue := eventMetadata.OccurredAt.AsTime().UTC().Format(time.RFC3339Nano)
	hiddenMetadataCommitment := blindedFramedSHA256(hiddenMetadataCommitmentDomain, openings.HiddenMetadataBlinding, hiddenMetadataJSON)
	payloadIdentityCommitment := blindedFramedSHA256(payloadIdentityCommitmentDomain, openings.PayloadIdentityBlinding,
		[]byte(protobufTypeURLPrefix+payloadType), schemaFingerprint, payloadProto, payloadJSON)
	eventTypeCommitment := blindedFramedSHA256(eventTypeCommitmentDomain, openings.EventTypeBlinding, []byte(eventTypeValue))
	occurredAtCommitment := blindedFramedSHA256(occurredAtCommitmentDomain, openings.OccurredAtBlinding, []byte(occurredAtValue))
	actorUserIDCommitment := blindedFramedSHA256(actorUserIDCommitmentDomain, openings.ActorUserIdBlinding, []byte(actorUserID))
	envelope := &structpb.Struct{Fields: map[string]*structpb.Value{
		"identity_projection": structpb.NewStructValue(&structpb.Struct{Fields: map[string]*structpb.Value{
			"generation":   structpb.NewStringValue(strconv.FormatUint(eventMetadata.Generation, 10)),
			"sequence":     structpb.NewStringValue(strconv.FormatUint(eventMetadata.Sequence, 10)),
			"workspace_id": structpb.NewStringValue(eventMetadata.WorkspaceId),
		}}),
		"actor_user_id_commitment":    structpb.NewStringValue(hex.EncodeToString(actorUserIDCommitment[:])),
		"event_type_commitment":       structpb.NewStringValue(hex.EncodeToString(eventTypeCommitment[:])),
		"hidden_metadata_commitment":  structpb.NewStringValue(hex.EncodeToString(hiddenMetadataCommitment[:])),
		"occurred_at_commitment":      structpb.NewStringValue(hex.EncodeToString(occurredAtCommitment[:])),
		"payload_identity_commitment": structpb.NewStringValue(hex.EncodeToString(payloadIdentityCommitment[:])),
		"version":                     structpb.NewStringValue(canonicalEventVersion),
	}}
	return canonical.NormalizedJSON(envelope)
}

func blindedFramedSHA256(domain string, blinding []byte, fields ...[]byte) [sha256.Size]byte {
	digest := sha256.New()
	_, _ = digest.Write([]byte(domain))
	_, _ = digest.Write(blinding)
	var length [8]byte
	for _, field := range fields {
		binary.BigEndian.PutUint64(length[:], uint64(len(field)))
		_, _ = digest.Write(length[:])
		_, _ = digest.Write(field)
	}
	var result [sha256.Size]byte
	copy(result[:], digest.Sum(nil))
	return result
}

func validEventEnvelope(event *tammyv1.AuditEvent) bool {
	if event == nil || event.Payload == nil || !exportReferencePattern.MatchString(event.Id) || !exportReferencePattern.MatchString(event.WorkspaceId) ||
		event.Generation == 0 || event.Sequence == 0 || event.Type == tammyv1.AuditEventType_AUDIT_EVENT_TYPE_UNSPECIFIED ||
		event.OccurredAt == nil || !event.OccurredAt.IsValid() || event.CommandType == "" || len(event.CommandType) > 256 ||
		!validAuditSource(event.Source) || event.Result == nil || len(event.Result.TypeName) == 0 || len(event.Result.TypeName) > 256 ||
		len(event.Result.DeterministicSha256) != sha256.Size || len(event.Result.OutcomeCode) > 96 ||
		!auditOutcomeCode.MatchString(event.Result.OutcomeCode) || len(event.PayloadSchemaFingerprint) != sha256.Size ||
		len(event.AffectedResources) > 64 || !optionalAuditDigest(event.BeforeSemanticHash) || !optionalAuditDigest(event.AfterSemanticHash) {
		return false
	}
	if _, defined := tammyv1.AuditEventType_name[int32(event.Type)]; !defined {
		return false
	}
	if event.Actor != nil && (!exportReferencePattern.MatchString(event.Actor.ActorUserId) || !exportReferencePattern.MatchString(event.Actor.SessionId)) {
		return false
	}
	for _, identifier := range []*string{event.OrganisationId, event.CommandId, event.IdempotencyKey} {
		if identifier != nil && !exportReferencePattern.MatchString(*identifier) {
			return false
		}
	}
	for _, resource := range event.AffectedResources {
		if !validAuditSource(resource) {
			return false
		}
	}
	return true
}

func validAuditSource(source *tammyv1.SourceRef) bool {
	return source != nil && len(source.Type) <= 64 && auditSourceType.MatchString(source.Type) &&
		exportReferencePattern.MatchString(source.Id) && source.Revision > 0 && len(source.ContentHash) == sha256.Size
}

func optionalAuditDigest(value []byte) bool {
	return len(value) == 0 || len(value) == sha256.Size
}

// Append assigns the latest generation/sequence, prepares exact canonical
// bytes, and persists the event and new head inside executor's transaction.
func (appender *Appender) Append(
	ctx context.Context,
	executor Executor,
	event *tammyv1.AuditEvent,
	payloadProto []byte,
) (StoredEvent, error) {
	if appender == nil || executor == nil || event == nil || event.WorkspaceId == "" {
		return StoredEvent{}, ErrInvalidEvent
	}
	if appender.mirror == nil || appender.gate == nil || appender.publisher == nil {
		return StoredEvent{}, ErrWriteGate
	}
	mirrorEpoch, accepting := appender.publisher.registrationEpoch()
	if !accepting || !appender.gate.Writable() {
		return StoredEvent{}, ErrWriteGate
	}
	return appender.append(ctx, executor, event, payloadProto, true, mirrorEpoch)
}

// appendInitial is the narrow creation-lifecycle capability. It permits SQL
// audit appends before the first baseline exists only while the durable local
// marker proves this installation owns the matching creation attempt.
func (appender *Appender) appendInitial(
	ctx context.Context,
	executor Executor,
	setupID string,
	event *tammyv1.AuditEvent,
	payloadProto []byte,
) (StoredEvent, error) {
	if appender == nil || executor == nil || event == nil || event.WorkspaceId == "" || appender.mirror == nil || appender.gate == nil {
		return StoredEvent{}, ErrInvalidEvent
	}
	if !appender.gate.initialMirrorPending() {
		return StoredEvent{}, ErrWriteGate
	}
	lifecycleStore, ok := appender.mirror.(InitialMirrorLifecycleStore)
	if !ok {
		return StoredEvent{}, ErrWriteGate
	}
	lifecycle, err := lifecycleStore.LoadInitialMirrorLifecycle(ctx, event.WorkspaceId)
	if err != nil || !validInitialMirrorLifecycle(lifecycle) || lifecycle.WorkspaceID != event.WorkspaceId ||
		lifecycle.Phase != InitialMirrorCreating || setupID != "" && lifecycle.SetupID != setupID {
		return StoredEvent{}, ErrWriteGate
	}
	return appender.append(ctx, executor, event, payloadProto, false, 0)
}

// appendMovedTrust is the only audit append permitted while a verified moved
// workspace remains read-only. The unexported capability accepts exactly the
// trust-establishment event for the fully matched database baseline and never
// publishes a mirror itself; TrustCoordinator owns the post-commit handoff.
func (appender *Appender) appendMovedTrust(
	ctx context.Context,
	executor Executor,
	prior *tammyv1.AuditMirrorBaseline,
	event *tammyv1.AuditEvent,
	payloadProto []byte,
) (StoredEvent, error) {
	if appender == nil || executor == nil || appender.mirror == nil || appender.gate == nil ||
		!appender.gate.movedTrustPending() || !validBaseline(prior) || !validMovedTrustEvent(prior, event) {
		return StoredEvent{}, ErrWriteGate
	}
	repository, err := NewRepository(executor)
	if err != nil {
		return StoredEvent{}, err
	}
	header, err := repository.latestHeader(ctx, prior.WorkspaceId, prior.Generation)
	if err != nil || header.Generation != prior.Generation || header.CurrentSequence != prior.Sequence ||
		!bytes.Equal(header.CurrentHead[:], prior.Head) {
		return StoredEvent{}, ErrRollbackDetected
	}
	return appender.append(ctx, executor, event, payloadProto, false, 0)
}

func validMovedTrustEvent(prior *tammyv1.AuditMirrorBaseline, event *tammyv1.AuditEvent) bool {
	if !validBaseline(prior) || event == nil || event.WorkspaceId != prior.WorkspaceId || event.Generation != 0 || event.Sequence != 0 ||
		event.Type != tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_TRUST_ESTABLISHED || event.Payload == nil ||
		event.Source == nil || event.Source.Type != "workspace" || event.Source.Id != prior.WorkspaceId ||
		event.Source.Revision != max(uint64(1), prior.Sequence) || !bytes.Equal(event.Source.ContentHash, prior.Head) {
		return false
	}
	if _, ok := event.Payload.Payload.(*tammyv1.AuditEventPayload_WorkspaceTrustEstablished); !ok {
		return false
	}
	payload := event.Payload.GetWorkspaceTrustEstablished()
	return payload != nil && payload.WorkspaceId == prior.WorkspaceId && payload.PriorMirrorUnavailable &&
		bytes.Equal(payload.PriorHead, prior.Head) && len(payload.DestinationInstallationHash) == sha256.Size
}

// AppendEvidence permits the narrowly allowed evidence-export mutation while
// a verified moved workspace remains read-only. It never establishes a
// missing mirror as a side effect.
func (appender *Appender) AppendEvidence(
	ctx context.Context,
	executor Executor,
	event *tammyv1.AuditEvent,
	payloadProto []byte,
) (StoredEvent, error) {
	if appender == nil || executor == nil || event == nil || event.WorkspaceId == "" || appender.mirror == nil || appender.gate == nil {
		return StoredEvent{}, ErrInvalidEvent
	}
	if !appender.gate.EvidenceExportAllowed() {
		return StoredEvent{}, ErrWriteGate
	}
	publishMirror := appender.gate.Writable()
	var mirrorEpoch uint64
	if publishMirror {
		var accepting bool
		mirrorEpoch, accepting = appender.publisher.registrationEpoch()
		if !accepting {
			return StoredEvent{}, ErrWriteGate
		}
	}
	return appender.append(ctx, executor, event, payloadProto, publishMirror, mirrorEpoch)
}

// AppendStagedWorkspaceRestored is the sole mirror-free append permitted for
// restore. The caller must already have initialized a fresh, empty generation
// in an isolated staged database; the exact event then becomes its first head.
func AppendStagedWorkspaceRestored(
	ctx context.Context,
	executor Executor,
	event *tammyv1.AuditEvent,
	payloadProto []byte,
) (StoredEvent, error) {
	if ctx == nil || executor == nil || event == nil || event.Generation != 0 || event.Sequence != 0 ||
		event.Type != tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_RESTORED || event.Payload == nil {
		return StoredEvent{}, ErrInvalidEvent
	}
	payload := event.Payload.GetWorkspaceRestored()
	if payload == nil || payload.WorkspaceId != event.WorkspaceId || payload.RestoredGeneration < 2 ||
		payload.PredecessorGeneration+1 != payload.RestoredGeneration || payload.BackupGeneration == 0 ||
		payload.BackupGeneration >= payload.RestoredGeneration || len(payload.BackupManifestHash) != sha256.Size ||
		len(payload.PreRestoreArchiveHash) != sha256.Size || len(payload.PredecessorHead) != sha256.Size ||
		len(payload.ArchivedHead) != sha256.Size || payload.OperationId == "" || payload.PreRestoreArchiveId == "" {
		return StoredEvent{}, ErrInvalidEvent
	}
	header, err := LoadChainHeader(ctx, executor, event.WorkspaceId, payload.RestoredGeneration)
	if err != nil || header.Generation != payload.RestoredGeneration || header.CurrentSequence != 0 ||
		header.CurrentHead != header.GenesisHash {
		return StoredEvent{}, ErrInvalidEvent
	}
	stored, _, _, err := (&Appender{}).appendSQL(ctx, executor, event, payloadProto)
	if err != nil || stored.Event == nil || stored.Event.Generation != payload.RestoredGeneration || stored.Event.Sequence != 1 {
		return StoredEvent{}, errors.Join(ErrInvalidEvent, err)
	}
	return stored, nil
}

func (appender *Appender) append(
	ctx context.Context,
	executor Executor,
	event *tammyv1.AuditEvent,
	payloadProto []byte,
	publishMirror bool,
	mirrorEpoch uint64,
) (StoredEvent, error) {
	stored, expected, baseline, err := appender.appendSQL(ctx, executor, event, payloadProto)
	if err != nil {
		return StoredEvent{}, err
	}
	if publishMirror {
		if appender.publisher == nil {
			return StoredEvent{}, ErrMirrorInvalid
		}
		if err := appender.publisher.registerAfterCommitAtEpoch(executor, expected, baseline, mirrorEpoch); err != nil {
			return StoredEvent{}, err
		}
	}
	return stored, nil
}

// appendSigningKeyRotationEvent performs the durable event/head mutation but
// deliberately defers mirror callback registration. Rotation must first CAS
// its workspace signing-key state against the newly linked event.
func (appender *Appender) appendSigningKeyRotationEvent(
	ctx context.Context,
	executor Executor,
	event *tammyv1.AuditEvent,
	payloadProto []byte,
) (StoredEvent, *tammyv1.AuditMirrorBaseline, *tammyv1.AuditMirrorBaseline, uint64, error) {
	if appender == nil || executor == nil || event == nil || event.WorkspaceId == "" ||
		appender.mirror == nil || appender.gate == nil || appender.publisher == nil {
		return StoredEvent{}, nil, nil, 0, ErrInvalidEvent
	}
	mirrorEpoch, accepting := appender.publisher.registrationEpoch()
	if !accepting || !appender.gate.Writable() {
		return StoredEvent{}, nil, nil, 0, ErrWriteGate
	}
	stored, expected, target, err := appender.appendSQL(ctx, executor, event, payloadProto)
	return stored, expected, target, mirrorEpoch, err
}

func (appender *Appender) appendSQL(
	ctx context.Context,
	executor Executor,
	event *tammyv1.AuditEvent,
	payloadProto []byte,
) (StoredEvent, *tammyv1.AuditMirrorBaseline, *tammyv1.AuditMirrorBaseline, error) {
	repository, err := NewRepository(executor)
	if err != nil {
		return StoredEvent{}, nil, nil, err
	}
	header, err := repository.latestHeader(ctx, event.WorkspaceId, event.Generation)
	if err != nil {
		return StoredEvent{}, nil, nil, err
	}
	positioned := proto.Clone(event).(*tammyv1.AuditEvent)
	positioned.Generation = header.Generation
	positioned.Sequence = header.CurrentSequence + 1
	stored, err := PrepareEvent(header.CurrentHead, positioned, payloadProto)
	if err != nil {
		return StoredEvent{}, nil, nil, err
	}
	if err := repository.insertEvent(ctx, stored, header); err != nil {
		return StoredEvent{}, nil, nil, err
	}
	expected := &tammyv1.AuditMirrorBaseline{WorkspaceId: header.WorkspaceID,
		Generation: header.Generation, Sequence: header.CurrentSequence,
		Head: append([]byte(nil), header.CurrentHead[:]...)}
	baseline := &tammyv1.AuditMirrorBaseline{WorkspaceId: stored.Event.WorkspaceId,
		Generation: stored.Event.Generation, Sequence: stored.Event.Sequence,
		Head: append([]byte(nil), stored.Event.EventHash...)}
	return stored, expected, baseline, nil
}

// mirrorPublisher orders post-commit publication without treating callback
// arrival order as SQL commit order. WriteGate owns one publisher for the
// mirror runtime so every Appender sharing that gate observes the same edges.
type mirrorPublisher struct {
	mirror MirrorStore
	gate   *WriteGate

	mu         sync.Mutex
	epoch      uint64
	disabled   bool
	workspaces map[string]*workspaceMirrorPublisher
}

type workspaceMirrorPublisher struct {
	mu      sync.Mutex
	edges   map[uint64]*mirrorPublicationEdge
	invalid bool
}

type mirrorPublicationEdge struct {
	expected  *tammyv1.AuditMirrorBaseline
	target    *tammyv1.AuditMirrorBaseline
	committed bool
	published bool
	stale     bool
	epoch     uint64
}

type guardedMirrorPublication struct {
	publisher *mirrorPublisher
	edge      *mirrorPublicationEdge
	armed     atomic.Bool
}

func (publication *guardedMirrorPublication) arm() {
	if publication != nil {
		publication.armed.Store(true)
	}
}

func (publication *guardedMirrorPublication) cancel() {
	if publication == nil {
		return
	}
	publication.armed.Store(false)
	publication.publisher.discard(publication.edge)
}

func newMirrorPublisher(mirror MirrorStore, gate *WriteGate) *mirrorPublisher {
	return &mirrorPublisher{mirror: mirror, gate: gate, disabled: gate == nil || !gate.Writable(),
		workspaces: make(map[string]*workspaceMirrorPublisher)}
}

func sameMirrorStore(left, right MirrorStore) bool {
	leftType := reflect.TypeOf(left)
	return leftType != nil && leftType == reflect.TypeOf(right) && leftType.Comparable() && left == right
}

func (publisher *mirrorPublisher) registrationEpoch() (uint64, bool) {
	if publisher == nil {
		return 0, false
	}
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	return publisher.epoch, !publisher.disabled
}

func (publisher *mirrorPublisher) acceptsRegistration(epoch uint64) bool {
	if publisher == nil {
		return false
	}
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	return !publisher.disabled && publisher.epoch == epoch
}

func (publisher *mirrorPublisher) lockWorkspaceForRegistration(workspaceID string,
	epoch uint64,
) (*workspaceMirrorPublisher, bool) {
	publisher.mu.Lock()
	if publisher.disabled || publisher.epoch != epoch {
		publisher.mu.Unlock()
		return nil, false
	}
	workspace := publisher.workspaces[workspaceID]
	if workspace == nil {
		workspace = &workspaceMirrorPublisher{edges: make(map[uint64]*mirrorPublicationEdge)}
		publisher.workspaces[workspaceID] = workspace
	}
	workspace.mu.Lock()
	publisher.mu.Unlock()
	if workspace.invalid {
		workspace.mu.Unlock()
		return nil, false
	}
	return workspace, true
}

func (publisher *mirrorPublisher) lockWorkspaceForEdge(edge *mirrorPublicationEdge) *workspaceMirrorPublisher {
	publisher.mu.Lock()
	if publisher.disabled || edge == nil || publisher.epoch != edge.epoch {
		publisher.mu.Unlock()
		return nil
	}
	workspace := publisher.workspaces[edge.target.WorkspaceId]
	if workspace != nil {
		workspace.mu.Lock()
	}
	publisher.mu.Unlock()
	return workspace
}

func (publisher *mirrorPublisher) removeWorkspaceIfEmpty(workspaceID string, expected *workspaceMirrorPublisher) {
	publisher.mu.Lock()
	workspace := publisher.workspaces[workspaceID]
	if workspace == expected {
		workspace.mu.Lock()
		if len(workspace.edges) == 0 {
			delete(publisher.workspaces, workspaceID)
		}
		workspace.mu.Unlock()
	}
	publisher.mu.Unlock()
}

func (publisher *mirrorPublisher) clearLocked() {
	for workspaceID, workspace := range publisher.workspaces {
		workspace.mu.Lock()
		workspace.invalid = true
		for sequence, edge := range workspace.edges {
			edge.stale = true
			delete(workspace.edges, sequence)
		}
		delete(publisher.workspaces, workspaceID)
		workspace.mu.Unlock()
	}
}

func (publisher *mirrorPublisher) registerAfterCommit(executor Executor,
	expected, target *tammyv1.AuditMirrorBaseline,
) error {
	epoch, accepting := publisher.registrationEpoch()
	if !accepting {
		return ErrWriteGate
	}
	return publisher.registerAfterCommitAtEpoch(executor, expected, target, epoch)
}

func (publisher *mirrorPublisher) registerAfterCommitAtEpoch(executor Executor,
	expected, target *tammyv1.AuditMirrorBaseline, epoch uint64,
) error {
	registrar, ok := executor.(AfterCommitRegistrar)
	if !ok || publisher == nil || publisher.mirror == nil || publisher.gate == nil ||
		!validMirrorPublicationEdge(expected, target) {
		return ErrMirrorInvalid
	}
	if !publisher.acceptsRegistration(epoch) {
		return ErrWriteGate
	}
	edge := &mirrorPublicationEdge{
		expected: proto.Clone(expected).(*tammyv1.AuditMirrorBaseline),
		target:   proto.Clone(target).(*tammyv1.AuditMirrorBaseline),
		epoch:    epoch,
	}
	if err := registrar.AfterCommit(func(ctx context.Context) error {
		if ctx == nil {
			ctx = context.Background()
		} else {
			ctx = context.WithoutCancel(ctx)
		}
		return publisher.publish(ctx, edge)
	}); err != nil {
		return err
	}
	workspace, accepting := publisher.lockWorkspaceForRegistration(target.WorkspaceId, epoch)
	if !accepting {
		return ErrWriteGate
	}
	for sequence, candidate := range workspace.edges {
		if sequence >= target.Sequence {
			candidate.stale = true
			delete(workspace.edges, sequence)
		}
	}
	workspace.edges[target.Sequence] = edge
	workspace.mu.Unlock()
	return nil
}

func (publisher *mirrorPublisher) registerGuardedAfterCommitAtEpoch(executor Executor,
	expected, target *tammyv1.AuditMirrorBaseline, epoch uint64,
) (*guardedMirrorPublication, error) {
	registrar, ok := executor.(AfterCommitRegistrar)
	if !ok || publisher == nil || publisher.mirror == nil || publisher.gate == nil ||
		!validMirrorPublicationEdge(expected, target) {
		return nil, ErrMirrorInvalid
	}
	if !publisher.acceptsRegistration(epoch) {
		return nil, ErrWriteGate
	}
	edge := &mirrorPublicationEdge{
		expected: proto.Clone(expected).(*tammyv1.AuditMirrorBaseline),
		target:   proto.Clone(target).(*tammyv1.AuditMirrorBaseline),
		epoch:    epoch,
	}
	publication := &guardedMirrorPublication{publisher: publisher, edge: edge}
	if err := registrar.AfterCommit(func(ctx context.Context) error {
		if !publication.armed.Load() {
			publisher.discard(edge)
			return nil
		}
		if ctx == nil {
			ctx = context.Background()
		} else {
			ctx = context.WithoutCancel(ctx)
		}
		return publisher.publish(ctx, edge)
	}); err != nil {
		return nil, err
	}
	workspace, accepting := publisher.lockWorkspaceForRegistration(target.WorkspaceId, epoch)
	if !accepting {
		return nil, ErrWriteGate
	}
	for sequence, candidate := range workspace.edges {
		if sequence >= target.Sequence {
			candidate.stale = true
			delete(workspace.edges, sequence)
		}
	}
	workspace.edges[target.Sequence] = edge
	workspace.mu.Unlock()
	return publication, nil
}

func (publisher *mirrorPublisher) discard(edge *mirrorPublicationEdge) {
	if publisher == nil || edge == nil || edge.target == nil {
		return
	}
	publisher.mu.Lock()
	workspace := publisher.workspaces[edge.target.WorkspaceId]
	publisher.mu.Unlock()
	if workspace == nil {
		return
	}
	workspace.mu.Lock()
	if workspace.edges[edge.target.Sequence] == edge {
		edge.stale = true
		delete(workspace.edges, edge.target.Sequence)
	}
	workspace.mu.Unlock()
	publisher.removeWorkspaceIfEmpty(edge.target.WorkspaceId, workspace)
}

func validMirrorPublicationEdge(expected, target *tammyv1.AuditMirrorBaseline) bool {
	return validBaseline(expected) && validBaseline(target) && expected.WorkspaceId == target.WorkspaceId &&
		expected.Generation == target.Generation && expected.Sequence+1 == target.Sequence
}

func (publisher *mirrorPublisher) publish(ctx context.Context, edge *mirrorPublicationEdge) error {
	workspace := publisher.lockWorkspaceForEdge(edge)
	if workspace == nil {
		return nil
	}
	if edge.stale || edge.published {
		workspace.mu.Unlock()
		return nil
	}
	if workspace.edges[edge.target.Sequence] != edge {
		edge.stale = true
		workspace.mu.Unlock()
		return nil
	}
	edge.committed = true
	err := publisher.publishThrough(ctx, workspace, edge)
	if err != nil {
		workspace.invalid = true
		for sequence, candidate := range workspace.edges {
			candidate.stale = true
			delete(workspace.edges, sequence)
		}
	}
	workspace.mu.Unlock()
	if err != nil {
		publisher.gate.set(false, true)
		return err
	}
	publisher.removeWorkspaceIfEmpty(edge.target.WorkspaceId, workspace)
	return nil
}

func (publisher *mirrorPublisher) publishThrough(ctx context.Context, workspace *workspaceMirrorPublisher,
	committed *mirrorPublicationEdge,
) error {
	current, err := publisher.mirror.Load(ctx, committed.target.WorkspaceId)
	if err != nil {
		if errors.Is(err, ErrMirrorMissing) {
			return ErrRollbackDetected
		}
		return err
	}
	if !validBaseline(current) || current.WorkspaceId != committed.target.WorkspaceId ||
		current.Generation != committed.target.Generation {
		return ErrRollbackDetected
	}
	if sameBaseline(current, committed.target) {
		publisher.markPublishedThrough(workspace, committed.target.Sequence)
		return nil
	}
	if current.Sequence >= committed.target.Sequence {
		return ErrRollbackDetected
	}

	cursor := current
	chain := make([]*mirrorPublicationEdge, 0, committed.target.Sequence-current.Sequence)
	for sequence := current.Sequence + 1; sequence <= committed.target.Sequence; sequence++ {
		candidate := workspace.edges[sequence]
		if candidate == nil || candidate.stale || !sameBaseline(candidate.expected, cursor) {
			return ErrRollbackDetected
		}
		chain = append(chain, candidate)
		cursor = candidate.target
	}
	// The committed edge read its expected baseline from SQL, which proves its
	// matching registered predecessor committed. Repeating that proof permits
	// only this exact contiguous chain to be drained; gaps never cause a jump.
	if len(chain) == 0 || chain[len(chain)-1] != committed || !committed.committed {
		return ErrRollbackDetected
	}
	for _, candidate := range chain {
		if err := publisher.mirror.CompareAndSwap(ctx,
			proto.Clone(candidate.expected).(*tammyv1.AuditMirrorBaseline),
			proto.Clone(candidate.target).(*tammyv1.AuditMirrorBaseline)); err != nil {
			if errors.Is(err, ErrMirrorConflict) {
				return ErrRollbackDetected
			}
			return err
		}
		publisher.markPublished(workspace, candidate)
	}
	return nil
}

func (publisher *mirrorPublisher) markPublished(workspace *workspaceMirrorPublisher, edge *mirrorPublicationEdge) {
	edge.published = true
	if workspace.edges[edge.target.Sequence] == edge {
		delete(workspace.edges, edge.target.Sequence)
	}
}

func (publisher *mirrorPublisher) markPublishedThrough(workspace *workspaceMirrorPublisher, sequence uint64) {
	for candidateSequence, candidate := range workspace.edges {
		if candidateSequence > sequence {
			continue
		}
		candidate.published = true
		delete(workspace.edges, candidateSequence)
	}
}

func registerMirrorAfterCommit(executor Executor, mirror MirrorStore, gate *WriteGate,
	expected, baseline *tammyv1.AuditMirrorBaseline,
) error {
	if mirror == nil || gate == nil {
		return ErrMirrorInvalid
	}
	return newMirrorPublisher(mirror, gate).registerAfterCommit(executor, expected, baseline)
}

func selectedPayload(eventType tammyv1.AuditEventType, payload *tammyv1.AuditEventPayload) (proto.Message, string, error) {
	if payload == nil {
		return nil, "", ErrInvalidEvent
	}
	expected, ok := auditPayloadIdentity[eventType]
	if !ok {
		return nil, "", ErrInvalidEvent
	}
	reflected := payload.ProtoReflect()
	oneofs := reflected.Descriptor().Oneofs()
	if oneofs.Len() != 1 {
		return nil, "", ErrInvalidEvent
	}
	field := reflected.WhichOneof(oneofs.Get(0))
	if field == nil || field.Name() != expected.field || field.Message() == nil || field.Message().FullName() != expected.message {
		return nil, "", ErrInvalidEvent
	}
	message := reflected.Get(field).Message().Interface()
	if message == nil || !message.ProtoReflect().IsValid() {
		return nil, "", ErrInvalidEvent
	}
	if payloadDescriptorContainsForbiddenSecret(message.ProtoReflect().Descriptor()) {
		return nil, "", ErrInvalidEvent
	}
	return message, string(message.ProtoReflect().Descriptor().FullName()), nil
}

func payloadDescriptorContainsForbiddenSecret(root protoreflect.MessageDescriptor) bool {
	visited := make(map[protoreflect.FullName]bool)
	var inspect func(protoreflect.MessageDescriptor) bool
	inspect = func(message protoreflect.MessageDescriptor) bool {
		if message == nil {
			return false
		}
		name := message.FullName()
		if name == "tammy.v1.SecretInput" || name == "tammy.v1.OneTimeSecretOutput" {
			return true
		}
		if visited[name] {
			return false
		}
		visited[name] = true
		fields := message.Fields()
		for index := 0; index < fields.Len(); index++ {
			field := fields.Get(index)
			if forbiddenAuditFieldName(string(field.Name())) {
				return true
			}
			if field.Message() != nil && inspect(field.Message()) {
				return true
			}
		}
		return false
	}
	return inspect(root)
}

func forbiddenAuditFieldName(name string) bool {
	normalized := strings.ToLower(name)
	for _, fragment := range []string{"password", "passphrase", "secret", "bearer", "access_token", "refresh_token", "totp_code", "activation_code", "recovery_code"} {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return false
}

func equalDigest(left, right []byte) bool {
	return len(left) == sha256.Size && len(right) == sha256.Size && bytes.Equal(left, right)
}
