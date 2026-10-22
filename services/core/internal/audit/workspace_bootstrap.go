//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type WorkspaceAuditAdapterConfig struct {
	Clock             clock.Clock
	Random            io.Reader
	NewID             func() (string, error)
	SchemaFingerprint []byte
	Mirror            MirrorStore
	Gate              *WriteGate
}

// WorkspaceAuditAdapter joins audit bootstrap and each workspace mutation to
// the workspace-owned SQLCipher transaction.
type WorkspaceAuditAdapter struct {
	clock             clock.Clock
	random            io.Reader
	newID             func() (string, error)
	schemaFingerprint []byte
	appender          *Appender
}

func NewWorkspaceAuditAdapter(config WorkspaceAuditAdapterConfig) (*WorkspaceAuditAdapter, error) {
	_, lifecycleStore := config.Mirror.(InitialMirrorLifecycleStore)
	if config.Clock == nil || config.Random == nil || config.NewID == nil || len(config.SchemaFingerprint) != sha256.Size ||
		config.Mirror == nil || !lifecycleStore || config.Gate == nil {
		return nil, ErrInvalidChainInput
	}
	appender, err := NewMirroringAppender(config.Mirror, config.Gate)
	if err != nil {
		return nil, err
	}
	return &WorkspaceAuditAdapter{clock: config.Clock, random: config.Random, newID: config.NewID,
		schemaFingerprint: append([]byte(nil), config.SchemaFingerprint...), appender: appender}, nil
}

// BeginInitialMirrorLifecycle durably binds the one-time bootstrap exception
// to the locally reserved creation attempt before any workspace SQL is made.
func (adapter *WorkspaceAuditAdapter) BeginInitialMirrorLifecycle(ctx context.Context, workspaceID, setupID string) error {
	if adapter == nil || workspaceID == "" || setupID == "" || adapter.appender == nil {
		return ErrMirrorInvalid
	}
	lifecycleStore, ok := adapter.appender.mirror.(InitialMirrorLifecycleStore)
	if !ok {
		return ErrMirrorInvalid
	}
	lifecycle, err := lifecycleStore.LoadInitialMirrorLifecycle(ctx, workspaceID)
	if errors.Is(err, ErrMirrorMissing) {
		_, mirrorErr := adapter.appender.mirror.Load(ctx, workspaceID)
		if mirrorErr == nil {
			return ErrWriteGate
		}
		if !errors.Is(mirrorErr, ErrMirrorMissing) {
			return mirrorErr
		}
		lifecycle = &InitialMirrorLifecycle{WorkspaceID: workspaceID, SetupID: setupID, Phase: InitialMirrorCreating}
		if err := lifecycleStore.SaveInitialMirrorLifecycle(ctx, lifecycle); err != nil {
			return err
		}
		adapter.appender.gate.beginInitialMirror()
		return nil
	}
	if err != nil || !validInitialMirrorLifecycle(lifecycle) || lifecycle.WorkspaceID != workspaceID || lifecycle.SetupID != setupID {
		return ErrMirrorInvalid
	}
	if lifecycle.Phase == InitialMirrorCreating {
		_, mirrorErr := adapter.appender.mirror.Load(ctx, workspaceID)
		if errors.Is(mirrorErr, ErrMirrorMissing) {
			adapter.appender.gate.beginInitialMirror()
			return nil
		}
		if mirrorErr != nil {
			return mirrorErr
		}
		// Any existing baseline requires full reconciliation before writes.
		// Its relationship to the database cannot be proven from local creation
		// metadata alone.
	}
	return nil
}

func (adapter *WorkspaceAuditAdapter) BootstrapWorkspaceAudit(
	ctx context.Context,
	executor workspace.MutationExecutor,
	workspaceID string,
	dek []byte,
	createdAt time.Time,
) (*workspace.AuditHeaderMetadata, error) {
	if adapter == nil || executor == nil || workspaceID == "" || len(dek) != 32 || createdAt.IsZero() {
		return nil, ErrInvalidChainInput
	}
	salt := make([]byte, sha256.Size)
	if _, err := io.ReadFull(adapter.random, salt); err != nil {
		return nil, ErrInvalidChainInput
	}
	genesis, err := Genesis(workspaceID, salt)
	if err != nil {
		return nil, err
	}
	record, public, err := GenerateSigningKey(workspaceID, dek, createdAt, adapter.random)
	if err != nil {
		return nil, err
	}
	if err := InitializeChain(ctx, executor, ChainHeader{WorkspaceID: workspaceID, Generation: 1,
		ChainSalt: salt, GenesisHash: genesis, CurrentHead: genesis, CreatedAt: createdAt}); err != nil {
		return nil, err
	}
	if err := PersistSigningKey(ctx, executor, record); err != nil {
		return nil, err
	}
	if err := InitializeSigningKeyState(ctx, executor, record); err != nil {
		return nil, err
	}
	return &workspace.AuditHeaderMetadata{ChainSalt: append([]byte(nil), salt...), GenesisHash: append([]byte(nil), genesis[:]...),
		SigningPublicKey: append([]byte(nil), public.PublicKey...), SigningKeyID: public.KeyID}, nil
}

func (adapter *WorkspaceAuditAdapter) LoadWorkspaceAuditHeader(
	ctx context.Context,
	executor workspace.MutationExecutor,
	workspaceID string,
) (*workspace.AuditHeaderMetadata, error) {
	if adapter == nil || executor == nil || workspaceID == "" {
		return nil, ErrInvalidChainInput
	}
	header, err := LoadChainHeader(ctx, executor, workspaceID, 0)
	if err != nil {
		return nil, err
	}
	active, err := LoadActiveSigningKey(ctx, executor, workspaceID)
	if err != nil {
		return nil, ErrSigningKey
	}
	return &workspace.AuditHeaderMetadata{ChainSalt: append([]byte(nil), header.ChainSalt...),
		GenesisHash: append([]byte(nil), header.GenesisHash[:]...), SigningPublicKey: append([]byte(nil), active.PublicKey...), SigningKeyID: active.KeyID,
		PreviousSigningKeyID: active.PreviousKeyID, RotationSignature: append([]byte(nil), active.PreviousSignature...)}, nil
}

func (adapter *WorkspaceAuditAdapter) AppendWorkspaceMutation(
	ctx context.Context,
	executor workspace.MutationExecutor,
	mutation workspace.WorkspaceMutation,
) error {
	if adapter == nil || executor == nil || mutation.WorkspaceID == "" || mutation.OperationID == "" || mutation.Kind == "" || mutation.Version == 0 {
		return ErrInvalidEvent
	}
	eventID, err := adapter.newID()
	if err != nil {
		return ErrInvalidEvent
	}
	contentHash, err := hex.DecodeString(mutation.SemanticHash)
	if err != nil || len(contentHash) != sha256.Size {
		digest := sha256.Sum256([]byte(mutation.SemanticHash))
		contentHash = digest[:]
	}
	fromState, toState := workspaceMutationStates(mutation.Kind)
	payload := &tammyv1.WorkspaceStateChangedEvent{
		WorkspaceId: mutation.WorkspaceID, FromState: fromState,
		ToState: toState, ReasonCode: stableReasonCode(mutation.Kind),
	}
	payloadProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(payload)
	if err != nil {
		return ErrInvalidEvent
	}
	commandID := mutation.OperationID
	event := &tammyv1.AuditEvent{
		Id: eventID, WorkspaceId: mutation.WorkspaceID,
		Type: tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_STATE_CHANGED, OccurredAt: timestamppb.New(adapter.clock.Now().UTC()),
		Source:                   &tammyv1.SourceRef{Type: "workspace", Id: mutation.WorkspaceID, Revision: mutation.Version, ContentHash: contentHash},
		Payload:                  &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_WorkspaceStateChanged{WorkspaceStateChanged: payload}},
		PayloadSchemaFingerprint: append([]byte(nil), adapter.schemaFingerprint...), CommandId: &commandID,
		CommandType: "tammy.workspace." + mutation.Kind,
		Result:      &tammyv1.AuditResultMetadata{TypeName: "tammy.v1.Workspace", DeterministicSha256: contentHash, OutcomeCode: "OK"},
	}
	if adapter.appender.gate.initialMirrorPending() {
		switch mutation.Kind {
		case "CREATE", "RECOVERY_CONFIRMATION", "SESSION_STARTED":
			setupID := ""
			if mutation.Kind == "CREATE" {
				setupID = mutation.OperationID
			}
			_, appendErr := adapter.appender.appendInitial(ctx, executor, setupID, event, payloadProto)
			return appendErr
		}
	}
	_, err = adapter.appender.Append(ctx, executor, event, payloadProto)
	return err
}

// AppendInitialAdministratorAudit is the lifecycle-scoped path for the
// identity event that completes first-administrator sign-in. It is denied
// unless the durable creation marker matches setupID and the gate is pending.
func (adapter *WorkspaceAuditAdapter) AppendInitialAdministratorAudit(
	ctx context.Context,
	executor workspace.MutationExecutor,
	setupID string,
	event *tammyv1.AuditEvent,
	payloadProto []byte,
) (StoredEvent, error) {
	if adapter == nil || setupID == "" {
		return StoredEvent{}, ErrWriteGate
	}
	return adapter.appender.appendInitial(ctx, executor, setupID, event, payloadProto)
}

// EstablishInitialMirror repairs the narrow crash window where the first
// administrator-session transaction committed but its post-commit OS mirror
// callback did not run. The Task 5 lifecycle calls this only after recovery
// confirmation and a successful administrator sign-in.
func (adapter *WorkspaceAuditAdapter) EstablishInitialMirror(
	ctx context.Context,
	executor workspace.MutationExecutor,
	workspaceID string,
	setupID string,
) error {
	if adapter == nil || executor == nil || workspaceID == "" || setupID == "" {
		return ErrWriteGate
	}
	if !adapter.appender.gate.initialMirrorPending() {
		if adapter.appender.gate.Writable() {
			return nil
		}
		return ErrWriteGate
	}
	lifecycleStore, ok := adapter.appender.mirror.(InitialMirrorLifecycleStore)
	if !ok {
		return ErrMirrorInvalid
	}
	lifecycle, err := lifecycleStore.LoadInitialMirrorLifecycle(ctx, workspaceID)
	if err != nil || !validInitialMirrorLifecycle(lifecycle) || lifecycle.WorkspaceID != workspaceID ||
		lifecycle.SetupID != setupID || lifecycle.Phase != InitialMirrorCreating {
		return ErrWriteGate
	}
	header, err := LoadChainHeader(ctx, executor, workspaceID, 0)
	if err != nil {
		return err
	}
	if !initialAdministratorAuditComplete(ctx, executor, header) {
		return ErrMirrorInvalid
	}
	return registerInitialMirrorAfterCommit(executor, adapter.appender.mirror, lifecycleStore, adapter.appender.gate, lifecycle,
		&tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: header.Generation,
			Sequence: header.CurrentSequence, Head: append([]byte(nil), header.CurrentHead[:]...)})
}

func initialAdministratorAuditComplete(ctx context.Context, executor Executor, header ChainHeader) bool {
	if header.CurrentSequence < 3 {
		return false
	}
	verifier, err := NewStreamingStoredChainVerifier(ctx, header)
	if err != nil {
		return false
	}
	defer verifier.Close()
	snapshot := StoredEventSnapshot{WorkspaceID: header.WorkspaceID, Generation: header.Generation,
		EndSequence: header.CurrentSequence, EndHead: header.CurrentHead}
	checkpoint := StoredEventCheckpoint{Head: header.GenesisHash}
	recoverySequence := uint64(0)
	sessionSequence := uint64(0)
	finalSequence := uint64(0)
	finalHasActor := false
	finalCommandType := ""
	for checkpoint.AfterSequence < snapshot.EndSequence {
		page, err := LoadStoredEventPage(ctx, executor, snapshot, checkpoint,
			StoredEventPageSizeLimit, StoredEventPageByteBudget)
		if err != nil || len(page.Events) == 0 || page.Checkpoint.AfterSequence <= checkpoint.AfterSequence {
			return false
		}
		if err := verifier.AcceptPage(page.Events); err != nil {
			return false
		}
		for _, stored := range page.Events {
			event := stored.Event
			if event == nil || event.WorkspaceId != header.WorkspaceID {
				return false
			}
			transition := event.GetPayload().GetWorkspaceStateChanged()
			if transition != nil && transition.WorkspaceId == header.WorkspaceID {
				switch transition.ReasonCode {
				case "RECOVERY_CONFIRMATION":
					if transition.FromState == tammyv1.WorkspaceState_WORKSPACE_STATE_PENDING_RECOVERY &&
						transition.ToState == tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED {
						recoverySequence = event.Sequence
					}
				case "SESSION_STARTED":
					if recoverySequence != 0 && event.Sequence > recoverySequence &&
						transition.FromState == tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED &&
						transition.ToState == tammyv1.WorkspaceState_WORKSPACE_STATE_AUTHENTICATED {
						sessionSequence = event.Sequence
					}
				}
			}
			finalSequence = event.Sequence
			finalHasActor = event.Actor != nil
			finalCommandType = event.CommandType
		}
		checkpoint = page.Checkpoint
		if !page.HasMore && checkpoint.AfterSequence != snapshot.EndSequence {
			return false
		}
	}
	verification := verifier.Finish()
	var verifiedHead [sha256.Size]byte
	if len(verification.VerifiedHead) == sha256.Size {
		copy(verifiedHead[:], verification.VerifiedHead)
	}
	if verifier.TerminalError() != nil ||
		verification.Integrity != tammyv1.AuditChainIntegrity_AUDIT_CHAIN_INTEGRITY_VALID ||
		verification.VerifiedThroughSequence != header.CurrentSequence || verifiedHead != header.CurrentHead {
		return false
	}
	commandType := strings.ToLower(finalCommandType)
	return sessionSequence != 0 && finalSequence > sessionSequence && finalHasActor &&
		strings.Contains(commandType, "identity") && (strings.Contains(commandType, "sign_in") || strings.Contains(commandType, "signin"))
}

func workspaceMutationStates(kind string) (tammyv1.WorkspaceState, tammyv1.WorkspaceState) {
	switch kind {
	case "CREATE", "CREATE_READY":
		return tammyv1.WorkspaceState_WORKSPACE_STATE_UNSPECIFIED, tammyv1.WorkspaceState_WORKSPACE_STATE_PENDING_RECOVERY
	case "RECOVERY_CONFIRMATION":
		return tammyv1.WorkspaceState_WORKSPACE_STATE_PENDING_RECOVERY, tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED
	case "SESSION_STARTED":
		return tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED, tammyv1.WorkspaceState_WORKSPACE_STATE_AUTHENTICATED
	case "LOCK", "EXPIRE_SETUP":
		return tammyv1.WorkspaceState_WORKSPACE_STATE_UNSPECIFIED, tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED
	default:
		return tammyv1.WorkspaceState_WORKSPACE_STATE_UNSPECIFIED, tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED
	}
}

func stableReasonCode(kind string) string {
	code := strings.ToUpper(strings.TrimSpace(kind))
	if code == "" {
		return "WORKSPACE_MUTATION"
	}
	return code
}
