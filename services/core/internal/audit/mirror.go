package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	ErrMirrorMissing    = errors.New("audit: OS mirror is unavailable")
	ErrRollbackDetected = errors.New("audit: mirror is ahead of or diverges from database")
	ErrMirrorInvalid    = errors.New("audit: mirror baseline is invalid")
	ErrMirrorConflict   = errors.New("audit: mirror baseline changed concurrently")
	ErrTrustProof       = errors.New("audit: moved-workspace trust proof is incomplete")
)

type MirrorStore interface {
	Load(context.Context, string) (*tammyv1.AuditMirrorBaseline, error)
	CompareAndSwap(context.Context, *tammyv1.AuditMirrorBaseline, *tammyv1.AuditMirrorBaseline) error
}

type InitialMirrorPhase string

const (
	InitialMirrorCreating    InitialMirrorPhase = "CREATING"
	InitialMirrorEstablished InitialMirrorPhase = "ESTABLISHED"
)

// InitialMirrorLifecycle is installation-local creation state. It contains no
// credentials, accounting data, or audit evidence; SetupID binds the marker to
// the locally reserved creation attempt so a copied database cannot impersonate
// a fresh setup.
type InitialMirrorLifecycle struct {
	WorkspaceID string
	SetupID     string
	Phase       InitialMirrorPhase
}

type InitialMirrorLifecycleStore interface {
	LoadInitialMirrorLifecycle(context.Context, string) (*InitialMirrorLifecycle, error)
	SaveInitialMirrorLifecycle(context.Context, *InitialMirrorLifecycle) error
}

type mirrorCredentials interface {
	put(string, []byte) error
	get(string) ([]byte, error)
	compareAndSwap(string, []byte, []byte) (bool, error)
}

type encodedMirrorStore struct{ credentials mirrorCredentials }

const initialMirrorLifecycleLabelPrefix = "tammy-audit-initial-mirror-v1-"

func newEncodedMirrorStore(credentials mirrorCredentials) *encodedMirrorStore {
	return &encodedMirrorStore{credentials: credentials}
}

func (store *encodedMirrorStore) CompareAndSwap(_ context.Context, expected, baseline *tammyv1.AuditMirrorBaseline) error {
	if store == nil || store.credentials == nil || !validBaseline(baseline) ||
		expected != nil && (!validBaseline(expected) || expected.WorkspaceId != baseline.WorkspaceId) {
		return ErrMirrorInvalid
	}
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(baseline)
	if err != nil {
		return ErrMirrorInvalid
	}
	var expectedPayload []byte
	if expected != nil {
		expectedPayload, err = proto.MarshalOptions{Deterministic: true}.Marshal(expected)
		if err != nil {
			return ErrMirrorInvalid
		}
	}
	swapped, err := store.credentials.compareAndSwap(baseline.WorkspaceId, expectedPayload, payload)
	if err != nil {
		return err
	}
	if !swapped {
		return ErrMirrorConflict
	}
	return nil
}

func (store *encodedMirrorStore) Load(_ context.Context, workspaceID string) (*tammyv1.AuditMirrorBaseline, error) {
	if store == nil || store.credentials == nil || workspaceID == "" {
		return nil, ErrMirrorInvalid
	}
	payload, err := store.credentials.get(workspaceID)
	if err != nil {
		return nil, err
	}
	baseline := &tammyv1.AuditMirrorBaseline{}
	if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(payload, baseline); err != nil ||
		len(baseline.ProtoReflect().GetUnknown()) != 0 || !validBaseline(baseline) || baseline.WorkspaceId != workspaceID {
		return nil, ErrMirrorInvalid
	}
	canonical, err := proto.MarshalOptions{Deterministic: true}.Marshal(baseline)
	if err != nil || !bytes.Equal(canonical, payload) {
		return nil, ErrMirrorInvalid
	}
	return baseline, nil
}

func (store *encodedMirrorStore) SaveInitialMirrorLifecycle(_ context.Context, lifecycle *InitialMirrorLifecycle) error {
	if store == nil || store.credentials == nil || !validInitialMirrorLifecycle(lifecycle) {
		return ErrMirrorInvalid
	}
	payload := []byte(string(lifecycle.Phase) + "\n" + lifecycle.WorkspaceID + "\n" + lifecycle.SetupID)
	return store.credentials.put(initialMirrorLifecycleLabel(lifecycle.WorkspaceID), payload)
}

func (store *encodedMirrorStore) LoadInitialMirrorLifecycle(_ context.Context, workspaceID string) (*InitialMirrorLifecycle, error) {
	if store == nil || store.credentials == nil || workspaceID == "" {
		return nil, ErrMirrorInvalid
	}
	payload, err := store.credentials.get(initialMirrorLifecycleLabel(workspaceID))
	if err != nil {
		return nil, err
	}
	parts := strings.Split(string(payload), "\n")
	if len(parts) != 3 {
		return nil, ErrMirrorInvalid
	}
	lifecycle := &InitialMirrorLifecycle{WorkspaceID: parts[1], SetupID: parts[2], Phase: InitialMirrorPhase(parts[0])}
	if !validInitialMirrorLifecycle(lifecycle) || lifecycle.WorkspaceID != workspaceID ||
		!bytes.Equal(payload, []byte(string(lifecycle.Phase)+"\n"+lifecycle.WorkspaceID+"\n"+lifecycle.SetupID)) {
		return nil, ErrMirrorInvalid
	}
	return lifecycle, nil
}

func initialMirrorLifecycleLabel(workspaceID string) string {
	digest := sha256.Sum256([]byte(workspaceID))
	return initialMirrorLifecycleLabelPrefix + hex.EncodeToString(digest[:])
}

func validInitialMirrorLifecycle(lifecycle *InitialMirrorLifecycle) bool {
	if lifecycle == nil || lifecycle.WorkspaceID == "" || lifecycle.SetupID == "" ||
		strings.ContainsAny(lifecycle.WorkspaceID, "\r\n") || strings.ContainsAny(lifecycle.SetupID, "\r\n") {
		return false
	}
	return lifecycle.Phase == InitialMirrorCreating || lifecycle.Phase == InitialMirrorEstablished
}

type TrustProofKind uint8

const (
	TrustProofNormal TrustProofKind = iota + 1
	TrustProofRecoveryBreakGlass
)

type TrustApproval struct {
	Actor                          *tammyv1.AuthenticationContext
	PassphraseVerified             bool
	AdministratorPasswordVerified  bool
	FreshTOTPVerified              bool
	RecoveryProofVerified          bool
	AdministratorBreakGlassAudited bool
}

type TrustProofVerifier interface {
	Verify(context.Context, Executor, TrustProofKind) (TrustApproval, error)
}

type TrustCommand struct {
	ProofKind                 TrustProofKind
	Prior                     *tammyv1.AuditMirrorBaseline
	DestinationInstallationID string
	EventID                   string
	CommandID                 string
	IdempotencyKey            string
	OccurredAt                time.Time
	SchemaFingerprint         []byte
	ResultHash                []byte
}

type TrustCoordinator struct {
	proof    TrustProofVerifier
	verifier MirrorVerifier
	appender *Appender
	store    MirrorStore
	gate     *WriteGate
}

func NewTrustCoordinator(proof TrustProofVerifier, verifier MirrorVerifier, appender *Appender) *TrustCoordinator {
	if appender == nil {
		return &TrustCoordinator{proof: proof, verifier: verifier}
	}
	return &TrustCoordinator{proof: proof, verifier: verifier, appender: appender, store: appender.mirror, gate: appender.gate}
}

type PendingTrust struct {
	mu        sync.Mutex
	store     MirrorStore
	verifier  MirrorVerifier
	gate      *WriteGate
	baseline  *tammyv1.AuditMirrorBaseline
	published bool
}

// afterCommit is registered on the caller-owned transaction by Establish. It
// is intentionally unexported so a consumer cannot publish the baseline or
// enable writes before SQL commit.
func (pending *PendingTrust) afterCommit(ctx context.Context) error {
	if pending == nil || pending.store == nil || pending.verifier == nil || pending.gate == nil || !validBaseline(pending.baseline) {
		return ErrMirrorInvalid
	}
	pending.mu.Lock()
	defer pending.mu.Unlock()
	if pending.published {
		return nil
	}
	verification, err := pending.verifier.VerifyFull(ctx, pending.baseline.WorkspaceId, pending.baseline.Generation)
	if err != nil || !verification.Valid || !sameBaseline(verification.Baseline, pending.baseline) {
		pending.gate.set(false, true)
		return ErrMirrorInvalid
	}
	err = publishVerifiedMirror(ctx, pending.store, verification, pending.baseline)
	if err != nil {
		pending.gate.set(false, true)
		return err
	}
	pending.gate.set(true, true)
	pending.published = true
	return nil
}

// publishVerifiedMirror only advances a baseline whose exact value was covered
// by the completed full-chain verification. A bounded retry permits an equal or
// verified predecessor to appear between Load and CompareAndSwap without ever
// overwriting an ahead or divergent value.
func publishVerifiedMirror(ctx context.Context, store MirrorStore, verified VerifiedChain,
	target *tammyv1.AuditMirrorBaseline,
) error {
	if store == nil || !verified.Valid || !validBaseline(target) || !sameBaseline(verified.Baseline, target) {
		return ErrMirrorInvalid
	}
	const attempts = 4
	for range attempts {
		existing, err := store.Load(ctx, target.WorkspaceId)
		var expected *tammyv1.AuditMirrorBaseline
		switch {
		case errors.Is(err, ErrMirrorMissing):
			expected = nil
		case err != nil || !validBaseline(existing):
			return ErrMirrorInvalid
		case sameBaseline(existing, target):
			return nil
		case existing.WorkspaceId != target.WorkspaceId || existing.Generation != target.Generation ||
			existing.Sequence >= target.Sequence:
			return ErrRollbackDetected
		default:
			verifiedHead, ok := verified.Heads[existing.Sequence]
			if !ok || len(verifiedHead) != sha256.Size || !bytes.Equal(verifiedHead, existing.Head) {
				return ErrRollbackDetected
			}
			expected = existing
		}
		err = store.CompareAndSwap(ctx, expected, proto.Clone(target).(*tammyv1.AuditMirrorBaseline))
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrMirrorConflict) {
			return err
		}
	}
	return ErrRollbackDetected
}

func (coordinator *TrustCoordinator) Establish(
	ctx context.Context,
	executor Executor,
	command TrustCommand,
) (*PendingTrust, error) {
	if coordinator == nil || coordinator.proof == nil || coordinator.verifier == nil || coordinator.appender == nil || coordinator.store == nil ||
		coordinator.gate == nil || executor == nil || !validBaseline(command.Prior) ||
		command.DestinationInstallationID == "" || command.EventID == "" || command.CommandID == "" ||
		command.IdempotencyKey == "" || command.OccurredAt.IsZero() || len(command.SchemaFingerprint) != sha256.Size ||
		len(command.ResultHash) != sha256.Size {
		return nil, ErrTrustProof
	}
	registrar, ok := executor.(AfterCommitRegistrar)
	if !ok || !coordinator.gate.movedTrustPending() {
		return nil, ErrTrustProof
	}
	approval, err := coordinator.proof.Verify(ctx, executor, command.ProofKind)
	if err != nil || !validTrustApproval(command.ProofKind, approval) {
		return nil, ErrTrustProof
	}
	installationHash := sha256.Sum256([]byte(command.DestinationInstallationID))
	payload := &tammyv1.WorkspaceTrustEstablishedEvent{
		WorkspaceId:                 command.Prior.WorkspaceId,
		PriorHead:                   append([]byte(nil), command.Prior.Head...),
		DestinationInstallationHash: append([]byte(nil), installationHash[:]...),
		PriorMirrorUnavailable:      true,
	}
	commandID := command.CommandID
	idempotencyKey := command.IdempotencyKey
	event := &tammyv1.AuditEvent{
		Id: command.EventID, WorkspaceId: command.Prior.WorkspaceId,
		Type:       tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_TRUST_ESTABLISHED,
		OccurredAt: timestamppb.New(command.OccurredAt.UTC()), Actor: proto.Clone(approval.Actor).(*tammyv1.AuthenticationContext),
		Source: &tammyv1.SourceRef{Type: "workspace", Id: command.Prior.WorkspaceId,
			Revision: max(uint64(1), command.Prior.Sequence), ContentHash: append([]byte(nil), command.Prior.Head...)},
		Payload: &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_WorkspaceTrustEstablished{
			WorkspaceTrustEstablished: payload,
		}},
		PayloadSchemaFingerprint: append([]byte(nil), command.SchemaFingerprint...),
		CommandId:                &commandID, CommandType: "tammy.v1.WorkspaceService.EstablishMovedWorkspaceTrust",
		IdempotencyKey: &idempotencyKey,
		Result: &tammyv1.AuditResultMetadata{TypeName: "tammy.v1.EstablishMovedWorkspaceTrustResponse",
			DeterministicSha256: append([]byte(nil), command.ResultHash...), OutcomeCode: "OK"},
	}
	stored, err := coordinator.appender.appendMovedTrust(ctx, executor, command.Prior, event, nil)
	if err != nil || stored.Event == nil || stored.Event.Generation != command.Prior.Generation ||
		stored.Event.Sequence != command.Prior.Sequence+1 || !bytes.Equal(stored.Event.PreviousHash, command.Prior.Head) ||
		len(stored.Event.EventHash) != sha256.Size {
		return nil, ErrMirrorInvalid
	}
	baseline := &tammyv1.AuditMirrorBaseline{WorkspaceId: command.Prior.WorkspaceId,
		Generation: stored.Event.Generation, Sequence: stored.Event.Sequence,
		Head: append([]byte(nil), stored.Event.EventHash...)}
	pending := &PendingTrust{store: coordinator.store, verifier: coordinator.verifier, gate: coordinator.gate, baseline: baseline}
	if err := registrar.AfterCommit(pending.afterCommit); err != nil {
		return nil, ErrMirrorInvalid
	}
	return pending, nil
}

func validTrustApproval(kind TrustProofKind, approval TrustApproval) bool {
	if approval.Actor == nil || approval.Actor.ActorUserId == "" || approval.Actor.SessionId == "" {
		return false
	}
	switch kind {
	case TrustProofNormal:
		return approval.PassphraseVerified && approval.AdministratorPasswordVerified && approval.FreshTOTPVerified
	case TrustProofRecoveryBreakGlass:
		return approval.RecoveryProofVerified && approval.AdministratorBreakGlassAudited
	default:
		return false
	}
}

type VerifiedChain struct {
	Baseline                            *tammyv1.AuditMirrorBaseline
	Heads                               map[uint64][]byte
	Valid                               bool
	InitialAdministratorSessionComplete bool
}

func (verification VerifiedChain) Clone() VerifiedChain {
	clone := VerifiedChain{Valid: verification.Valid, InitialAdministratorSessionComplete: verification.InitialAdministratorSessionComplete,
		Heads: make(map[uint64][]byte, len(verification.Heads))}
	if verification.Baseline != nil {
		clone.Baseline = proto.Clone(verification.Baseline).(*tammyv1.AuditMirrorBaseline)
	}
	for sequence, head := range verification.Heads {
		clone.Heads[sequence] = append([]byte(nil), head...)
	}
	return clone
}

type MirrorVerifier interface {
	VerifyFull(context.Context, string, uint64) (VerifiedChain, error)
}

type WriteGate struct {
	mu             sync.RWMutex
	writable       bool
	evidenceExport bool
	initialPending bool

	publisherMu sync.Mutex
	publisher   *mirrorPublisher
}

func NewWriteGate() *WriteGate { return &WriteGate{} }

func (gate *WriteGate) publisherFor(mirror MirrorStore) (*mirrorPublisher, error) {
	if gate == nil || mirror == nil {
		return nil, ErrMirrorInvalid
	}
	gate.publisherMu.Lock()
	defer gate.publisherMu.Unlock()
	if gate.publisher == nil {
		gate.publisher = newMirrorPublisher(mirror, gate)
		return gate.publisher, nil
	}
	if !sameMirrorStore(gate.publisher.mirror, mirror) {
		return nil, ErrMirrorInvalid
	}
	return gate.publisher, nil
}

func (gate *WriteGate) transition(writable, evidenceExport bool, initialPending *bool) {
	if gate == nil {
		return
	}
	gate.publisherMu.Lock()
	publisher := gate.publisher
	if publisher != nil {
		publisher.mu.Lock()
		if writable {
			publisher.disabled = false
		} else {
			publisher.epoch++
			publisher.disabled = true
		}
	}
	gate.mu.Lock()
	gate.writable = writable
	gate.evidenceExport = evidenceExport
	if initialPending != nil {
		gate.initialPending = *initialPending
	}
	gate.mu.Unlock()
	if publisher != nil {
		if !writable {
			publisher.clearLocked()
		}
		publisher.mu.Unlock()
	}
	gate.publisherMu.Unlock()
}

func (gate *WriteGate) Writable() bool {
	if gate == nil {
		return false
	}
	gate.mu.RLock()
	defer gate.mu.RUnlock()
	return gate.writable
}

func (gate *WriteGate) EvidenceExportAllowed() bool {
	if gate == nil {
		return false
	}
	gate.mu.RLock()
	defer gate.mu.RUnlock()
	return gate.evidenceExport
}

func (gate *WriteGate) movedTrustPending() bool {
	if gate == nil {
		return false
	}
	gate.mu.RLock()
	defer gate.mu.RUnlock()
	return !gate.writable && gate.evidenceExport && !gate.initialPending
}

func (gate *WriteGate) set(writable, evidenceExport bool) {
	gate.transition(writable, evidenceExport, nil)
}

func (gate *WriteGate) initialMirrorPending() bool {
	if gate == nil {
		return false
	}
	gate.mu.RLock()
	defer gate.mu.RUnlock()
	return gate.initialPending
}

func (gate *WriteGate) beginReconciliation() {
	initialPending := false
	gate.transition(false, false, &initialPending)
}

func (gate *WriteGate) beginInitialMirror() {
	initialPending := true
	gate.transition(false, false, &initialPending)
}

func (gate *WriteGate) establishInitialMirror() {
	initialPending := false
	gate.transition(true, true, &initialPending)
}

type MirrorDecision uint8

const (
	MirrorDecisionCurrent MirrorDecision = iota + 1
	MirrorDecisionRepaired
	MirrorDecisionMovedReadOnly
	MirrorDecisionRollbackDenied
	MirrorDecisionInitialPending
	MirrorDecisionInitialEstablished
)

type MirrorReconciler struct {
	store    MirrorStore
	verifier MirrorVerifier
	gate     *WriteGate
}

func NewMirrorReconciler(store MirrorStore, verifier MirrorVerifier, gate *WriteGate) *MirrorReconciler {
	return &MirrorReconciler{store: store, verifier: verifier, gate: gate}
}

// DeclineMovedTrust keeps verified read-only evidence access while leaving all
// mutations disabled and writes no OS baseline.
func DeclineMovedTrust(gate *WriteGate) {
	if gate != nil {
		gate.set(false, true)
	}
}

func registerInitialMirrorAfterCommit(executor Executor, mirror MirrorStore, lifecycleStore InitialMirrorLifecycleStore,
	gate *WriteGate, lifecycle *InitialMirrorLifecycle, baseline *tammyv1.AuditMirrorBaseline,
) error {
	registrar, ok := executor.(AfterCommitRegistrar)
	if !ok || mirror == nil || lifecycleStore == nil || gate == nil || !gate.initialMirrorPending() ||
		!validInitialMirrorLifecycle(lifecycle) || lifecycle.Phase != InitialMirrorCreating || !validBaseline(baseline) ||
		lifecycle.WorkspaceID != baseline.WorkspaceId {
		return ErrMirrorInvalid
	}
	owned := proto.Clone(baseline).(*tammyv1.AuditMirrorBaseline)
	ownedLifecycle := *lifecycle
	return registrar.AfterCommit(func(ctx context.Context) error {
		if err := mirror.CompareAndSwap(ctx, nil, proto.Clone(owned).(*tammyv1.AuditMirrorBaseline)); err != nil {
			gate.set(false, false)
			if errors.Is(err, ErrMirrorConflict) {
				return ErrRollbackDetected
			}
			return err
		}
		ownedLifecycle.Phase = InitialMirrorEstablished
		if err := lifecycleStore.SaveInitialMirrorLifecycle(ctx, &ownedLifecycle); err != nil {
			gate.set(false, false)
			return err
		}
		gate.establishInitialMirror()
		return nil
	})
}

func (reconciler *MirrorReconciler) Open(
	ctx context.Context,
	database *tammyv1.AuditMirrorBaseline,
) (MirrorDecision, error) {
	if reconciler == nil || reconciler.store == nil || reconciler.verifier == nil || reconciler.gate == nil ||
		!validBaseline(database) {
		return MirrorDecisionRollbackDenied, ErrMirrorInvalid
	}
	reconciler.gate.beginReconciliation()
	verified, err := reconciler.verifier.VerifyFull(ctx, database.WorkspaceId, database.Generation)
	if err != nil || !verified.Valid || !sameBaseline(verified.Baseline, database) {
		return MirrorDecisionRollbackDenied, ErrMirrorInvalid
	}
	mirrored, err := reconciler.store.Load(ctx, database.WorkspaceId)
	if errors.Is(err, ErrMirrorMissing) {
		lifecycleStore, ok := reconciler.store.(InitialMirrorLifecycleStore)
		if !ok {
			reconciler.gate.set(false, true)
			return MirrorDecisionMovedReadOnly, nil
		}
		lifecycle, lifecycleErr := lifecycleStore.LoadInitialMirrorLifecycle(ctx, database.WorkspaceId)
		if errors.Is(lifecycleErr, ErrMirrorMissing) {
			reconciler.gate.set(false, true)
			return MirrorDecisionMovedReadOnly, nil
		}
		if lifecycleErr != nil || !validInitialMirrorLifecycle(lifecycle) || lifecycle.WorkspaceID != database.WorkspaceId {
			return MirrorDecisionRollbackDenied, ErrMirrorInvalid
		}
		if lifecycle.Phase != InitialMirrorCreating {
			reconciler.gate.set(false, true)
			return MirrorDecisionMovedReadOnly, nil
		}
		if !verified.InitialAdministratorSessionComplete {
			reconciler.gate.beginInitialMirror()
			return MirrorDecisionInitialPending, nil
		}
		if err := publishVerifiedMirror(ctx, reconciler.store, verified, database); err != nil {
			return MirrorDecisionRollbackDenied, err
		}
		lifecycle.Phase = InitialMirrorEstablished
		if err := lifecycleStore.SaveInitialMirrorLifecycle(ctx, lifecycle); err != nil {
			return MirrorDecisionRollbackDenied, err
		}
		reconciler.gate.establishInitialMirror()
		return MirrorDecisionInitialEstablished, nil
	}
	if err != nil || !validBaseline(mirrored) {
		return MirrorDecisionRollbackDenied, ErrMirrorInvalid
	}
	if mirrored.Generation != database.Generation {
		return MirrorDecisionRollbackDenied, ErrRollbackDetected
	}
	if mirrored.Sequence > database.Sequence {
		return MirrorDecisionRollbackDenied, ErrRollbackDetected
	}
	if mirrored.Sequence == database.Sequence {
		if !bytes.Equal(mirrored.Head, database.Head) {
			return MirrorDecisionRollbackDenied, ErrRollbackDetected
		}
		if err := reconciler.finishInitialLifecycle(ctx, database.WorkspaceId, verified); err != nil {
			return MirrorDecisionRollbackDenied, err
		}
		reconciler.gate.set(true, true)
		return MirrorDecisionCurrent, nil
	}
	verifiedPredecessor, ok := verified.Heads[mirrored.Sequence]
	if !ok || len(verifiedPredecessor) != 32 || !bytes.Equal(verifiedPredecessor, mirrored.Head) {
		return MirrorDecisionRollbackDenied, ErrRollbackDetected
	}
	if err := publishVerifiedMirror(ctx, reconciler.store, verified, database); err != nil {
		return MirrorDecisionRollbackDenied, err
	}
	if err := reconciler.finishInitialLifecycle(ctx, database.WorkspaceId, verified); err != nil {
		return MirrorDecisionRollbackDenied, err
	}
	reconciler.gate.set(true, true)
	return MirrorDecisionRepaired, nil
}

func (reconciler *MirrorReconciler) finishInitialLifecycle(ctx context.Context, workspaceID string, verified VerifiedChain) error {
	lifecycleStore, ok := reconciler.store.(InitialMirrorLifecycleStore)
	if !ok {
		return nil
	}
	lifecycle, err := lifecycleStore.LoadInitialMirrorLifecycle(ctx, workspaceID)
	if errors.Is(err, ErrMirrorMissing) {
		return nil
	}
	if err != nil || !validInitialMirrorLifecycle(lifecycle) || lifecycle.WorkspaceID != workspaceID {
		return ErrMirrorInvalid
	}
	if lifecycle.Phase == InitialMirrorEstablished {
		return nil
	}
	if !verified.InitialAdministratorSessionComplete {
		return ErrMirrorInvalid
	}
	lifecycle.Phase = InitialMirrorEstablished
	return lifecycleStore.SaveInitialMirrorLifecycle(ctx, lifecycle)
}

func validBaseline(baseline *tammyv1.AuditMirrorBaseline) bool {
	return baseline != nil && baseline.WorkspaceId != "" && baseline.Generation > 0 && len(baseline.Head) == 32
}

func sameBaseline(left, right *tammyv1.AuditMirrorBaseline) bool {
	return validBaseline(left) && validBaseline(right) && left.WorkspaceId == right.WorkspaceId &&
		left.Generation == right.Generation && left.Sequence == right.Sequence && bytes.Equal(left.Head, right.Head)
}
