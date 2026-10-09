//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"errors"
	"sync"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/audit"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
	"google.golang.org/protobuf/proto"
)

// Existing workspaces that predate ordinary audit bootstrap remain readable.
// Starting coverage requires a separately implemented explicit adoption flow;
// a new-workspace creation marker must never fabricate their historical trust.
var ErrLocalAuditCoverageUnavailable = errors.New("app: ordinary audit coverage is unavailable for this workspace")

type localAuditRuntime struct {
	adapter       *audit.WorkspaceAuditAdapter
	appender      *audit.Appender
	gate          *audit.WriteGate
	mirror        audit.MirrorStore
	descriptors   []byte
	fingerprint   [sha256.Size]byte
	newID         func() (string, error)
	stateMu       sync.RWMutex
	openMu        sync.Mutex
	workspaceID   string
	legacy        bool
	movedReadOnly bool
}

type localAuditWorkspaceState struct {
	workspaceID   string
	legacy        bool
	movedReadOnly bool
}

func (runtime *localAuditRuntime) workspaceState() localAuditWorkspaceState {
	if runtime == nil {
		return localAuditWorkspaceState{}
	}
	runtime.stateMu.RLock()
	defer runtime.stateMu.RUnlock()
	return localAuditWorkspaceState{workspaceID: runtime.workspaceID, legacy: runtime.legacy, movedReadOnly: runtime.movedReadOnly}
}

func (runtime *localAuditRuntime) setWorkspaceState(state localAuditWorkspaceState) {
	if runtime == nil {
		return
	}
	runtime.stateMu.Lock()
	runtime.workspaceID = state.workspaceID
	runtime.legacy = state.legacy
	runtime.movedReadOnly = state.movedReadOnly
	runtime.stateMu.Unlock()
}

func newLocalAuditRuntime(mirror audit.MirrorStore, source clock.Clock, newID func() (string, error)) (*localAuditRuntime, error) {
	if mirror == nil {
		var err error
		mirror, err = audit.NewPlatformMirrorStore()
		if err != nil {
			return nil, err
		}
	}
	descriptors, err := audit.CurrentDescriptorSet()
	if err != nil {
		return nil, err
	}
	fingerprint := sha256.Sum256(descriptors)
	gate := audit.NewWriteGate()
	adapter, err := audit.NewWorkspaceAuditAdapter(audit.WorkspaceAuditAdapterConfig{Clock: source, Random: rand.Reader, NewID: newID, SchemaFingerprint: fingerprint[:], Mirror: mirror, Gate: gate})
	if err != nil {
		return nil, err
	}
	appender, err := audit.NewMirroringAppender(mirror, gate)
	if err != nil {
		return nil, err
	}
	return &localAuditRuntime{adapter: adapter, appender: appender, gate: gate, mirror: mirror, descriptors: descriptors, fingerprint: fingerprint, newID: newID}, nil
}

func (runtime *localAuditRuntime) retainDescriptors(ctx context.Context, executor workspace.MutationExecutor, now time.Time) error {
	rows, err := executor.QueryContext(ctx, `SELECT count(*) FROM audit_descriptor_sets_v1 WHERE fingerprint=?`, runtime.fingerprint[:])
	if err != nil {
		return err
	}
	var count int
	if !rows.Next() || rows.Scan(&count) != nil || rows.Next() || rows.Err() != nil {
		_ = rows.Close()
		return audit.ErrDescriptorSetRegistry
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if count == 1 {
		_, err := audit.LoadDescriptorSet(ctx, executor, runtime.fingerprint)
		return err
	}
	if count != 0 {
		return audit.ErrDescriptorSetRegistry
	}
	_, err = audit.PersistDescriptorSet(ctx, executor, runtime.descriptors, now)
	return err
}

func (runtime *localAuditRuntime) openWithin(ctx context.Context, executor workspace.MutationExecutor, workspaceID string) error {
	if runtime == nil || executor == nil || workspaceID == "" {
		return ErrComposition
	}
	runtime.openMu.Lock()
	defer runtime.openMu.Unlock()
	return runtime.openWithinLocked(ctx, executor, workspaceID)
}

// openWithinLocked classifies one active workspace while openMu serializes
// activation and unlock against identity audit appends.
func (runtime *localAuditRuntime) openWithinLocked(ctx context.Context, executor workspace.MutationExecutor, workspaceID string) error {
	if runtime == nil || executor == nil || workspaceID == "" {
		return ErrComposition
	}
	runtime.setWorkspaceState(localAuditWorkspaceState{})
	header, err := audit.LoadChainHeader(ctx, executor, workspaceID, 0)
	if errors.Is(err, audit.ErrChainNotFound) {
		// No local baseline or creation marker may survive an alleged legacy
		// database. Missing SQL beneath either marker is corruption, not adoption.
		_, mirrorErr := runtime.mirror.Load(ctx, workspaceID)
		lifecycleStore, ok := runtime.mirror.(audit.InitialMirrorLifecycleStore)
		if !ok {
			return audit.ErrMirrorInvalid
		}
		_, lifecycleErr := lifecycleStore.LoadInitialMirrorLifecycle(ctx, workspaceID)
		if !errors.Is(mirrorErr, audit.ErrMirrorMissing) || !errors.Is(lifecycleErr, audit.ErrMirrorMissing) {
			return audit.ErrMirrorInvalid
		}
		rows, queryErr := executor.QueryContext(ctx, `SELECT (SELECT count(*) FROM audit_events_v1)+(SELECT count(*) FROM audit_signing_keys_v1)+(SELECT count(*) FROM audit_signing_key_state_v1)+(SELECT count(*) FROM audit_chain_headers_v1)`)
		if queryErr != nil {
			return queryErr
		}
		var count int64
		if !rows.Next() || rows.Scan(&count) != nil || rows.Next() || rows.Err() != nil {
			_ = rows.Close()
			return audit.ErrMirrorInvalid
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if count != 0 {
			return audit.ErrMirrorInvalid
		}
		runtime.setWorkspaceState(localAuditWorkspaceState{workspaceID: workspaceID, legacy: true})
		audit.DeclineMovedTrust(runtime.gate)
		return nil
	}
	if err != nil {
		return err
	}
	verifier, err := audit.NewSQLMirrorVerifier(localAuditReadTransactions{workspaceID: workspaceID, executor: executor})
	if err != nil {
		return err
	}
	baseline := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: header.Generation, Sequence: header.CurrentSequence, Head: append([]byte(nil), header.CurrentHead[:]...)}
	decision, err := audit.NewMirrorReconciler(runtime.mirror, verifier, runtime.gate).Open(ctx, baseline)
	if err != nil {
		return err
	}
	runtime.setWorkspaceState(localAuditWorkspaceState{workspaceID: workspaceID, movedReadOnly: decision == audit.MirrorDecisionMovedReadOnly})
	return nil
}

func (runtime *localAuditRuntime) activate(ctx context.Context, database *sqlcipher.Database, workspaceID string, now time.Time) error {
	tx, err := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	runtime.openMu.Lock()
	defer runtime.openMu.Unlock()
	if err := runtime.openWithinLocked(ctx, tx, workspaceID); err != nil {
		return err
	}
	if !runtime.workspaceState().legacy {
		if err := runtime.retainDescriptors(ctx, tx, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type localAuditReadTransactions struct {
	workspaceID string
	executor    workspace.MutationExecutor
}

func (transactions localAuditReadTransactions) WorkspaceID() string { return transactions.workspaceID }
func (transactions localAuditReadTransactions) Read(_ context.Context, work func(audit.ServiceTransaction) error) error {
	return work(localAuditReadTransaction{MutationExecutor: transactions.executor, id: transactions.workspaceID})
}
func (localAuditReadTransactions) Mutate(context.Context, func(audit.ServiceTransaction) error) error {
	return audit.ErrWriteGate
}

type localAuditReadTransaction struct {
	workspace.MutationExecutor
	id string
}

func (transaction localAuditReadTransaction) TransactionID() string { return transaction.id }
func (localAuditReadTransaction) AfterCommit(func(context.Context) error) error {
	return audit.ErrWriteGate
}

type localWorkspaceAudit struct{ runtime *localAuditRuntime }

func (port localWorkspaceAudit) BeginInitialMirrorLifecycle(ctx context.Context, workspaceID, setupID string) error {
	return port.runtime.adapter.BeginInitialMirrorLifecycle(ctx, workspaceID, setupID)
}
func (port localWorkspaceAudit) BootstrapWorkspaceAudit(ctx context.Context, executor workspace.MutationExecutor, workspaceID string, dek []byte, createdAt time.Time) (*workspace.AuditHeaderMetadata, error) {
	if err := port.runtime.retainDescriptors(ctx, executor, createdAt); err != nil {
		return nil, err
	}
	return port.runtime.adapter.BootstrapWorkspaceAudit(ctx, executor, workspaceID, dek, createdAt)
}
func (port localWorkspaceAudit) LoadWorkspaceAuditHeader(ctx context.Context, executor workspace.MutationExecutor, workspaceID string) (*workspace.AuditHeaderMetadata, error) {
	return port.runtime.adapter.LoadWorkspaceAuditHeader(ctx, executor, workspaceID)
}
func (port localWorkspaceAudit) AppendWorkspaceMutation(ctx context.Context, executor workspace.MutationExecutor, mutation workspace.WorkspaceMutation) error {
	if port.runtime == nil {
		return ErrComposition
	}
	port.runtime.openMu.Lock()
	defer port.runtime.openMu.Unlock()
	if mutation.Kind == "UNLOCK" {
		if err := port.runtime.openWithinLocked(ctx, executor, mutation.WorkspaceID); err != nil {
			return err
		}
	}
	state := port.runtime.workspaceState()
	if state.workspaceID == mutation.WorkspaceID && state.legacy {
		return nil
	}
	if state.workspaceID == mutation.WorkspaceID && state.movedReadOnly {
		switch mutation.Kind {
		case "LOCK", "UNLOCK", "SESSION_STARTED":
			// Security and authentication transitions remain available on a moved
			// workspace, but do not extend its untrusted local audit history.
			return nil
		}
	}
	return port.runtime.adapter.AppendWorkspaceMutation(ctx, executor, mutation)
}
func (port localWorkspaceAudit) EstablishInitialMirror(ctx context.Context, executor workspace.MutationExecutor, workspaceID, setupID string) error {
	if port.runtime == nil {
		return ErrComposition
	}
	port.runtime.openMu.Lock()
	defer port.runtime.openMu.Unlock()
	state := port.runtime.workspaceState()
	if state.workspaceID == workspaceID && (state.legacy || state.movedReadOnly) {
		return nil
	}
	return port.runtime.adapter.EstablishInitialMirror(ctx, executor, workspaceID, setupID)
}

// Existing non-session identity audit coverage remains unchanged. The typed
// session path records actual actor/session facts needed for initial trust.
type localIdentityAudit struct{ runtime *localAuditRuntime }

func (localIdentityAudit) Record(context.Context, workspace.MutationExecutor, string, string) error {
	return nil
}
func (port localIdentityAudit) RecordSessionStarted(ctx context.Context, executor workspace.MutationExecutor, operation string, user *tammyv1.User, session *tammyv1.Session) error {
	if port.runtime == nil || executor == nil || user == nil || session == nil || session.UserId != user.Id || session.State != tammyv1.SessionState_SESSION_STATE_ACTIVE || session.CreatedAt == nil || !session.CreatedAt.IsValid() {
		return audit.ErrInvalidEvent
	}
	port.runtime.openMu.Lock()
	defer port.runtime.openMu.Unlock()
	state := port.runtime.workspaceState()
	if state.workspaceID != "" && (state.legacy || state.movedReadOnly) {
		return nil
	}
	from := tammyv1.UserState_USER_STATE_ACTIVE
	reason := "SIGN_IN"
	var result proto.Message = &tammyv1.SignInResponse{User: user, Session: session}
	if operation == "tammy.v1.IdentityService.ActivateUser" {
		from = tammyv1.UserState_USER_STATE_PENDING_ACTIVATION
		reason = "ACTIVATION"
		result = &tammyv1.ActivateUserResponse{User: user, Session: session}
	} else if operation != "tammy.v1.IdentityService.SignIn" {
		return audit.ErrInvalidEvent
	}
	resultBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(result)
	if err != nil {
		return err
	}
	resultHash := sha256.Sum256(resultBytes)
	sessionBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(session)
	if err != nil {
		return err
	}
	sessionHash := sha256.Sum256(sessionBytes)
	userBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(user)
	if err != nil {
		return err
	}
	userHash := sha256.Sum256(userBytes)
	eventID, err := port.runtime.newID()
	if err != nil {
		return err
	}
	commandID := session.Id
	event := &tammyv1.AuditEvent{Id: eventID, WorkspaceId: state.workspaceID, Type: tammyv1.AuditEventType_AUDIT_EVENT_TYPE_USER_STATE_CHANGED, OccurredAt: session.CreatedAt, Actor: &tammyv1.AuthenticationContext{ActorUserId: user.Id, SessionId: session.Id}, Source: &tammyv1.SourceRef{Type: "identity_session", Id: session.Id, Revision: 1, ContentHash: sessionHash[:]}, AffectedResources: []*tammyv1.SourceRef{{Type: "identity_user", Id: user.Id, Revision: user.Version, ContentHash: userHash[:]}}, Payload: &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_UserStateChanged{UserStateChanged: &tammyv1.UserStateChangedEvent{UserId: user.Id, FromState: from, ToState: user.State, ReasonCode: reason}}}, PayloadSchemaFingerprint: append([]byte(nil), port.runtime.fingerprint[:]...), CommandId: &commandID, CommandType: operation, Result: &tammyv1.AuditResultMetadata{TypeName: string(result.ProtoReflect().Descriptor().FullName()), DeterministicSha256: resultHash[:], OutcomeCode: "SUCCESS"}}
	if port.runtime.gate.Writable() {
		_, err := port.runtime.appender.Append(ctx, executor, event, nil)
		return err
	}
	lifecycleStore := port.runtime.mirror.(audit.InitialMirrorLifecycleStore)
	lifecycle, err := lifecycleStore.LoadInitialMirrorLifecycle(ctx, state.workspaceID)
	if err != nil {
		return err
	}
	_, err = port.runtime.adapter.AppendInitialAdministratorAudit(ctx, executor, lifecycle.SetupID, event, nil)
	return err
}
