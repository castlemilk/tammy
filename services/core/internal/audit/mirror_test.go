package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"sync"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

type noopExecutor struct{}

func (noopExecutor) ExecContext(context.Context, string, ...any) (sql.Result, error) { return nil, nil }
func (noopExecutor) QueryContext(context.Context, string, ...any) (*sql.Rows, error) { return nil, nil }

type callbackExecutor struct {
	noopExecutor
	callback func(context.Context) error
}

func (executor *callbackExecutor) AfterCommit(callback func(context.Context) error) error {
	executor.callback = callback
	return nil
}

type failAtCASMirrorStore struct {
	memoryMirrorStore
	failAt int
	calls  int
	err    error
}

func (store *failAtCASMirrorStore) CompareAndSwap(ctx context.Context,
	expected, target *tammyv1.AuditMirrorBaseline,
) error {
	store.calls++
	if store.calls == store.failAt {
		return store.err
	}
	return store.memoryMirrorStore.CompareAndSwap(ctx, expected, target)
}

func mirrorPublisherState(publisher *mirrorPublisher) (int, int) {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	workspaces := len(publisher.workspaces)
	edges := 0
	for _, workspace := range publisher.workspaces {
		workspace.mu.Lock()
		edges += len(workspace.edges)
		workspace.mu.Unlock()
	}
	return workspaces, edges
}

type fixedTrustProofVerifier struct {
	approval TrustApproval
	kind     TrustProofKind
	calls    int
}

func (verifier *fixedTrustProofVerifier) Verify(_ context.Context, _ Executor, kind TrustProofKind) (TrustApproval, error) {
	verifier.calls++
	verifier.kind = kind
	return verifier.approval, nil
}

type memoryMirrorCredentials struct {
	mu     sync.Mutex
	values map[string][]byte
}

func (store *memoryMirrorCredentials) put(label string, value []byte) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.values == nil {
		store.values = make(map[string][]byte)
	}
	store.values[label] = append([]byte(nil), value...)
	return nil
}

func (store *memoryMirrorCredentials) get(label string) ([]byte, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	value, ok := store.values[label]
	if !ok {
		return nil, ErrMirrorMissing
	}
	return append([]byte(nil), value...), nil
}

func (store *memoryMirrorCredentials) compareAndSwap(label string, expected, replacement []byte) (bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	current, exists := store.values[label]
	if exists && bytes.Equal(current, replacement) {
		return true, nil
	}
	if exists != (expected != nil) || exists && !bytes.Equal(current, expected) {
		return false, nil
	}
	if store.values == nil {
		store.values = make(map[string][]byte)
	}
	store.values[label] = append([]byte(nil), replacement...)
	return true, nil
}

type memoryMirrorStore struct {
	mu         sync.Mutex
	baseline   *tammyv1.AuditMirrorBaseline
	lifecycle  *InitialMirrorLifecycle
	beforeSave func(*tammyv1.AuditMirrorBaseline)
	saves      int
}

func TestNewMirroringAppenderRejectsDifferentMirrorForSharedGate(t *testing.T) {
	gate := NewWriteGate()
	if _, err := NewMirroringAppender(&memoryMirrorStore{}, gate); err != nil {
		t.Fatal(err)
	}
	if _, err := NewMirroringAppender(&memoryMirrorStore{}, gate); !errors.Is(err, ErrMirrorInvalid) {
		t.Fatalf("second mirror error=%v, want ErrMirrorInvalid", err)
	}
}

func TestMirrorPublisherPrunesUnreplacedFutureRollbackReservation(t *testing.T) {
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	zero := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1,
		Head: bytes.Repeat([]byte{0x10}, sha256.Size)}
	one := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1, Sequence: 1,
		Head: bytes.Repeat([]byte{0x11}, sha256.Size)}
	two := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1, Sequence: 2,
		Head: bytes.Repeat([]byte{0x12}, sha256.Size)}
	replacement := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1, Sequence: 1,
		Head: bytes.Repeat([]byte{0x21}, sha256.Size)}
	store := &memoryMirrorStore{baseline: proto.Clone(zero).(*tammyv1.AuditMirrorBaseline)}
	gate := NewWriteGate()
	gate.set(true, true)
	appender, err := NewMirroringAppender(store, gate)
	if err != nil {
		t.Fatal(err)
	}
	first := &callbackExecutor{}
	future := &callbackExecutor{}
	committed := &callbackExecutor{}
	if err := appender.publisher.registerAfterCommit(first, zero, one); err != nil {
		t.Fatal(err)
	}
	if err := appender.publisher.registerAfterCommit(future, one, two); err != nil {
		t.Fatal(err)
	}
	if err := appender.publisher.registerAfterCommit(committed, zero, replacement); err != nil {
		t.Fatal(err)
	}
	if workspaces, edges := mirrorPublisherState(appender.publisher); workspaces != 1 || edges != 1 {
		t.Fatalf("publisher state after lower reuse workspaces=%d edges=%d, want 1/1", workspaces, edges)
	}
	if err := committed.callback(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := future.callback(context.Background()); err != nil {
		t.Fatalf("pruned future callback: %v", err)
	}
	if err := first.callback(context.Background()); err != nil {
		t.Fatalf("replaced callback: %v", err)
	}
	if !sameBaseline(store.baseline, replacement) || !gate.Writable() {
		t.Fatalf("baseline=%v writable=%v", store.baseline, gate.Writable())
	}
}

func TestMirrorPublisherClearsReservationsAfterPartialCASFailure(t *testing.T) {
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	zero := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1,
		Head: bytes.Repeat([]byte{0x30}, sha256.Size)}
	one := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1, Sequence: 1,
		Head: bytes.Repeat([]byte{0x31}, sha256.Size)}
	two := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1, Sequence: 2,
		Head: bytes.Repeat([]byte{0x32}, sha256.Size)}
	credentialError := errors.New("credential store failed on second CAS")
	store := &failAtCASMirrorStore{memoryMirrorStore: memoryMirrorStore{
		baseline: proto.Clone(zero).(*tammyv1.AuditMirrorBaseline),
	}, failAt: 2, err: credentialError}
	gate := NewWriteGate()
	gate.set(true, true)
	appender, err := NewMirroringAppender(store, gate)
	if err != nil {
		t.Fatal(err)
	}
	priorEpoch, accepting := appender.publisher.registrationEpoch()
	if !accepting {
		t.Fatal("writable publisher did not issue a registration epoch")
	}
	predecessor := &callbackExecutor{}
	successor := &callbackExecutor{}
	if err := appender.publisher.registerAfterCommit(predecessor, zero, one); err != nil {
		t.Fatal(err)
	}
	if err := appender.publisher.registerAfterCommit(successor, one, two); err != nil {
		t.Fatal(err)
	}
	if err := successor.callback(context.Background()); !errors.Is(err, credentialError) {
		t.Fatalf("partial publication error=%v, want credential error", err)
	}
	if !sameBaseline(store.baseline, one) || gate.Writable() || !gate.EvidenceExportAllowed() {
		t.Fatalf("baseline=%v writable=%v evidence=%v", store.baseline, gate.Writable(), gate.EvidenceExportAllowed())
	}
	if workspaces, edges := mirrorPublisherState(appender.publisher); workspaces != 0 || edges != 0 {
		t.Fatalf("publisher state after partial failure workspaces=%d edges=%d", workspaces, edges)
	}
	if err := predecessor.callback(context.Background()); err != nil {
		t.Fatalf("cleared predecessor callback: %v", err)
	}
	late := &callbackExecutor{}
	if err := appender.publisher.registerAfterCommitAtEpoch(late, one, two, priorEpoch); !errors.Is(err, ErrWriteGate) {
		t.Fatalf("registration from pre-failure epoch error=%v, want ErrWriteGate", err)
	}
	if late.callback != nil {
		t.Fatal("pre-failure epoch registered a post-commit callback")
	}
}

func TestMirrorPublisherRemovesEmptyWorkspaceAfterPublication(t *testing.T) {
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	expected := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1,
		Head: bytes.Repeat([]byte{0x40}, sha256.Size)}
	target := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1, Sequence: 1,
		Head: bytes.Repeat([]byte{0x41}, sha256.Size)}
	store := &memoryMirrorStore{baseline: proto.Clone(expected).(*tammyv1.AuditMirrorBaseline)}
	gate := NewWriteGate()
	gate.set(true, true)
	appender, err := NewMirroringAppender(store, gate)
	if err != nil {
		t.Fatal(err)
	}
	executor := &callbackExecutor{}
	if err := appender.publisher.registerAfterCommit(executor, expected, target); err != nil {
		t.Fatal(err)
	}
	if err := executor.callback(context.Background()); err != nil {
		t.Fatal(err)
	}
	if workspaces, edges := mirrorPublisherState(appender.publisher); workspaces != 0 || edges != 0 {
		t.Fatalf("publisher state after success workspaces=%d edges=%d", workspaces, edges)
	}
}

func TestMirrorReconciliationClearsUnpublishedReservations(t *testing.T) {
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	expected := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1,
		Head: bytes.Repeat([]byte{0x50}, sha256.Size)}
	target := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1, Sequence: 1,
		Head: bytes.Repeat([]byte{0x51}, sha256.Size)}
	store := &memoryMirrorStore{baseline: proto.Clone(expected).(*tammyv1.AuditMirrorBaseline)}
	gate := NewWriteGate()
	gate.set(true, true)
	appender, err := NewMirroringAppender(store, gate)
	if err != nil {
		t.Fatal(err)
	}
	executor := &callbackExecutor{}
	if err := appender.publisher.registerAfterCommit(executor, expected, target); err != nil {
		t.Fatal(err)
	}
	gate.beginReconciliation()
	if workspaces, edges := mirrorPublisherState(appender.publisher); workspaces != 0 || edges != 0 {
		t.Fatalf("publisher state after reconciliation workspaces=%d edges=%d", workspaces, edges)
	}
	if err := executor.callback(context.Background()); err != nil {
		t.Fatalf("cleared rollback callback: %v", err)
	}
}

func TestMirrorReconciliationRejectsRegistrationThatPassedThePriorEpochFence(t *testing.T) {
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	expected := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1,
		Head: bytes.Repeat([]byte{0x60}, sha256.Size)}
	target := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1, Sequence: 1,
		Head: bytes.Repeat([]byte{0x61}, sha256.Size)}
	store := &memoryMirrorStore{baseline: proto.Clone(expected).(*tammyv1.AuditMirrorBaseline)}
	gate := NewWriteGate()
	gate.set(true, true)
	appender, err := NewMirroringAppender(store, gate)
	if err != nil {
		t.Fatal(err)
	}
	epoch, accepting := appender.publisher.registrationEpoch()
	if !accepting {
		t.Fatal("writable publisher did not issue a registration epoch")
	}

	started := make(chan struct{})
	release := make(chan struct{})
	result := make(chan error, 1)
	executor := &callbackExecutor{}
	go func() {
		close(started)
		<-release
		result <- appender.publisher.registerAfterCommitAtEpoch(executor, expected, target, epoch)
	}()
	<-started
	gate.beginReconciliation()
	close(release)
	if err := <-result; !errors.Is(err, ErrWriteGate) {
		t.Fatalf("stale epoch registration error=%v, want ErrWriteGate", err)
	}
	if executor.callback != nil {
		t.Fatal("stale epoch registered a post-commit callback")
	}
	if workspaces, edges := mirrorPublisherState(appender.publisher); workspaces != 0 || edges != 0 {
		t.Fatalf("publisher state after stale registration workspaces=%d edges=%d", workspaces, edges)
	}

	gate.set(true, true)
	nextEpoch, accepting := appender.publisher.registrationEpoch()
	if !accepting || nextEpoch == epoch {
		t.Fatalf("re-enabled publisher epoch=%d accepting=%v, prior=%d", nextEpoch, accepting, epoch)
	}
}

func TestMirrorPublisherPrunesPredecessorsWhenMirrorAlreadyEqualsCommittedTarget(t *testing.T) {
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	zero := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1,
		Head: bytes.Repeat([]byte{0x70}, sha256.Size)}
	one := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1, Sequence: 1,
		Head: bytes.Repeat([]byte{0x71}, sha256.Size)}
	two := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1, Sequence: 2,
		Head: bytes.Repeat([]byte{0x72}, sha256.Size)}
	store := &memoryMirrorStore{baseline: proto.Clone(two).(*tammyv1.AuditMirrorBaseline)}
	gate := NewWriteGate()
	gate.set(true, true)
	appender, err := NewMirroringAppender(store, gate)
	if err != nil {
		t.Fatal(err)
	}
	predecessor := &callbackExecutor{}
	committed := &callbackExecutor{}
	if err := appender.publisher.registerAfterCommit(predecessor, zero, one); err != nil {
		t.Fatal(err)
	}
	if err := appender.publisher.registerAfterCommit(committed, one, two); err != nil {
		t.Fatal(err)
	}
	if err := committed.callback(context.Background()); err != nil {
		t.Fatalf("already-published committed target: %v", err)
	}
	if workspaces, edges := mirrorPublisherState(appender.publisher); workspaces != 0 || edges != 0 {
		t.Fatalf("publisher state after exact target workspaces=%d edges=%d", workspaces, edges)
	}
	if err := predecessor.callback(context.Background()); err != nil {
		t.Fatalf("already-published predecessor callback: %v", err)
	}
	if !gate.Writable() || !sameBaseline(store.baseline, two) {
		t.Fatalf("baseline=%v writable=%v", store.baseline, gate.Writable())
	}
}

func (store *memoryMirrorStore) LoadInitialMirrorLifecycle(context.Context, string) (*InitialMirrorLifecycle, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.lifecycle == nil {
		return nil, ErrMirrorMissing
	}
	owned := *store.lifecycle
	return &owned, nil
}

func (store *memoryMirrorStore) SaveInitialMirrorLifecycle(_ context.Context, lifecycle *InitialMirrorLifecycle) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if lifecycle == nil {
		return ErrMirrorInvalid
	}
	owned := *lifecycle
	store.lifecycle = &owned
	return nil
}

func TestMirrorAheadDeniesWritesAndEvidenceExportAsRollback(t *testing.T) {
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	databaseHead := bytes.Repeat([]byte{0x22}, 32)
	store := &memoryMirrorStore{baseline: &tammyv1.AuditMirrorBaseline{
		WorkspaceId: workspaceID, Generation: 1, Sequence: 4, Head: bytes.Repeat([]byte{0x44}, 32),
	}}
	verifier := &fixedMirrorVerifier{verification: VerifiedChain{
		Baseline: &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1, Sequence: 3, Head: databaseHead},
		Heads:    map[uint64][]byte{3: databaseHead}, Valid: true,
	}}
	gate := NewWriteGate()
	decision, err := NewMirrorReconciler(store, verifier, gate).Open(context.Background(), verifier.verification.Baseline)
	if decision != MirrorDecisionRollbackDenied || !errors.Is(err, ErrRollbackDetected) || gate.Writable() ||
		gate.EvidenceExportAllowed() || store.saves != 0 {
		t.Fatalf("rollback decision=%v err=%v writable=%v export=%v saves=%d",
			decision, err, gate.Writable(), gate.EvidenceExportAllowed(), store.saves)
	}
}

func (store *memoryMirrorStore) Load(context.Context, string) (*tammyv1.AuditMirrorBaseline, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.baseline == nil {
		return nil, ErrMirrorMissing
	}
	return proto.Clone(store.baseline).(*tammyv1.AuditMirrorBaseline), nil
}

func (store *memoryMirrorStore) CompareAndSwap(_ context.Context, expected, baseline *tammyv1.AuditMirrorBaseline) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.beforeSave != nil {
		hook := store.beforeSave
		store.beforeSave = nil
		hook(baseline)
	}
	if sameBaseline(store.baseline, baseline) {
		return nil
	}
	if store.baseline == nil && expected != nil || store.baseline != nil && expected == nil ||
		store.baseline != nil && !sameBaseline(store.baseline, expected) {
		return ErrMirrorConflict
	}
	store.saves++
	store.baseline = proto.Clone(baseline).(*tammyv1.AuditMirrorBaseline)
	return nil
}

type fixedMirrorVerifier struct {
	verification VerifiedChain
	calls        int
}

func (verifier *fixedMirrorVerifier) VerifyFull(context.Context, string, uint64) (VerifiedChain, error) {
	verifier.calls++
	return verifier.verification.Clone(), nil
}

func TestMirrorRepairsDatabaseAheadOnlyAfterFullVerification(t *testing.T) {
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	mirroredHead := bytes.Repeat([]byte{0x11}, 32)
	databaseHead := bytes.Repeat([]byte{0x22}, 32)
	store := &memoryMirrorStore{baseline: &tammyv1.AuditMirrorBaseline{
		WorkspaceId: workspaceID, Generation: 1, Sequence: 2, Head: mirroredHead,
	}}
	verifier := &fixedMirrorVerifier{verification: VerifiedChain{
		Baseline: &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1, Sequence: 3, Head: databaseHead},
		Heads:    map[uint64][]byte{0: bytes.Repeat([]byte{0x01}, 32), 1: bytes.Repeat([]byte{0x02}, 32), 2: mirroredHead, 3: databaseHead},
		Valid:    true,
	}}
	gate := NewWriteGate()
	decision, err := NewMirrorReconciler(store, verifier, gate).Open(context.Background(), verifier.verification.Baseline)
	if err != nil {
		t.Fatal(err)
	}
	if decision != MirrorDecisionRepaired || verifier.calls != 1 || store.saves != 1 || !gate.Writable() ||
		store.baseline.Sequence != 3 || !bytes.Equal(store.baseline.Head, databaseHead) {
		t.Fatalf("repair decision=%v verifier=%d saves=%d writable=%v baseline=%v",
			decision, verifier.calls, store.saves, gate.Writable(), store.baseline)
	}
}

func TestMirrorRepairNeverOverwritesBaselineThatAdvancesDuringPublication(t *testing.T) {
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	priorHead := bytes.Repeat([]byte{0x11}, sha256.Size)
	target := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1, Sequence: 3,
		Head: bytes.Repeat([]byte{0x22}, sha256.Size)}
	ahead := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1, Sequence: 4,
		Head: bytes.Repeat([]byte{0x33}, sha256.Size)}
	store := &memoryMirrorStore{baseline: &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID,
		Generation: 1, Sequence: 2, Head: priorHead}}
	store.beforeSave = func(*tammyv1.AuditMirrorBaseline) {
		store.baseline = proto.Clone(ahead).(*tammyv1.AuditMirrorBaseline)
	}
	verifier := &fixedMirrorVerifier{verification: VerifiedChain{Baseline: target,
		Heads: map[uint64][]byte{2: priorHead, 3: target.Head}, Valid: true}}
	gate := NewWriteGate()
	decision, err := NewMirrorReconciler(store, verifier, gate).Open(context.Background(), target)
	if decision != MirrorDecisionRollbackDenied || !errors.Is(err, ErrRollbackDetected) ||
		store.saves != 0 || !sameBaseline(store.baseline, ahead) || gate.Writable() || gate.EvidenceExportAllowed() {
		t.Fatalf("decision=%v err=%v saves=%d baseline=%v writable=%v export=%v",
			decision, err, store.saves, store.baseline, gate.Writable(), gate.EvidenceExportAllowed())
	}
}

func TestMirrorStoreUsesExactDeterministicProtobufWithoutCredentialProofs(t *testing.T) {
	backend := &memoryMirrorCredentials{}
	store := newEncodedMirrorStore(backend)
	baseline := &tammyv1.AuditMirrorBaseline{
		WorkspaceId: "01890f60-4d6d-7c12-8f02-6c9129d5b001", Generation: 2, Sequence: 7,
		Head: bytes.Repeat([]byte{0x42}, 32),
	}
	if err := store.CompareAndSwap(context.Background(), nil, baseline); err != nil {
		t.Fatal(err)
	}
	want, err := proto.MarshalOptions{Deterministic: true}.Marshal(baseline)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(backend.values[baseline.WorkspaceId], want) {
		t.Fatalf("credential bytes changed\nwant %x\n got %x", want, backend.values[baseline.WorkspaceId])
	}
	loaded, err := store.Load(context.Background(), baseline.WorkspaceId)
	if err != nil || !proto.Equal(loaded, baseline) {
		t.Fatalf("loaded baseline=%v err=%v", loaded, err)
	}
	backend.values[baseline.WorkspaceId] = append(backend.values[baseline.WorkspaceId], 0xa0, 0x06, 0x01)
	if _, err := store.Load(context.Background(), baseline.WorkspaceId); !errors.Is(err, ErrMirrorInvalid) {
		t.Fatalf("unknown-field mirror error=%v, want ErrMirrorInvalid", err)
	}
}

func TestMirrorStorePersistsCanonicalNonSecretCreationLifecycleSeparately(t *testing.T) {
	ctx := context.Background()
	backend := &memoryMirrorCredentials{}
	store := newEncodedMirrorStore(backend)
	lifecycle := &InitialMirrorLifecycle{WorkspaceID: "01890f60-4d6d-7c12-8f02-6c9129d5b001",
		SetupID: "01890f60-4d6d-7c12-8f02-6c9129d5b002", Phase: InitialMirrorCreating}
	if err := store.SaveInitialMirrorLifecycle(ctx, lifecycle); err != nil {
		t.Fatal(err)
	}
	want := []byte("CREATING\n01890f60-4d6d-7c12-8f02-6c9129d5b001\n01890f60-4d6d-7c12-8f02-6c9129d5b002")
	label := initialMirrorLifecycleLabel(lifecycle.WorkspaceID)
	if !bytes.Equal(backend.values[label], want) || backend.values[lifecycle.WorkspaceID] != nil {
		t.Fatalf("lifecycle label=%q bytes=%q baseline=%x", label, backend.values[label], backend.values[lifecycle.WorkspaceID])
	}
	loaded, err := store.LoadInitialMirrorLifecycle(ctx, lifecycle.WorkspaceID)
	if err != nil || *loaded != *lifecycle {
		t.Fatalf("loaded lifecycle=%#v err=%v", loaded, err)
	}
	backend.values[label] = append(backend.values[label], '\n')
	if _, err := store.LoadInitialMirrorLifecycle(ctx, lifecycle.WorkspaceID); !errors.Is(err, ErrMirrorInvalid) {
		t.Fatalf("noncanonical lifecycle error=%v, want ErrMirrorInvalid", err)
	}
}

func TestMissingMirrorOpensMovedWorkspaceReadOnlyAndDeclinePreservesIt(t *testing.T) {
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	database := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1, Sequence: 3,
		Head: bytes.Repeat([]byte{0x22}, 32)}
	store := &memoryMirrorStore{}
	verifier := &fixedMirrorVerifier{verification: VerifiedChain{Baseline: database,
		Heads: map[uint64][]byte{3: database.Head}, Valid: true}}
	gate := NewWriteGate()
	decision, err := NewMirrorReconciler(store, verifier, gate).Open(context.Background(), database)
	if err != nil || decision != MirrorDecisionMovedReadOnly || gate.Writable() ||
		!gate.EvidenceExportAllowed() || store.saves != 0 || verifier.calls != 1 {
		t.Fatalf("moved open decision=%v err=%v writable=%v export=%v saves=%d verifies=%d",
			decision, err, gate.Writable(), gate.EvidenceExportAllowed(), store.saves, verifier.calls)
	}
	DeclineMovedTrust(gate)
	if gate.Writable() || !gate.EvidenceExportAllowed() || store.saves != 0 {
		t.Fatalf("decline changed read-only state: writable=%v export=%v saves=%d",
			gate.Writable(), gate.EvidenceExportAllowed(), store.saves)
	}
}

func TestReconcileFirstRepairsOnlyDurablyCreatingInitialMirrorAfterFinalAdministratorAudit(t *testing.T) {
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	setupID := "01890f60-4d6d-7c12-8f02-6c9129d5b002"
	database := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1, Sequence: 4,
		Head: bytes.Repeat([]byte{0x22}, 32)}
	store := &memoryMirrorStore{lifecycle: &InitialMirrorLifecycle{WorkspaceID: workspaceID, SetupID: setupID, Phase: InitialMirrorCreating}}
	verifier := &fixedMirrorVerifier{verification: VerifiedChain{Baseline: database,
		Heads: map[uint64][]byte{4: database.Head}, Valid: true, InitialAdministratorSessionComplete: true}}
	gate := NewWriteGate()
	decision, err := NewMirrorReconciler(store, verifier, gate).Open(context.Background(), database)
	if err != nil {
		t.Fatal(err)
	}
	if decision != MirrorDecisionInitialEstablished || store.saves != 1 || store.baseline == nil ||
		store.lifecycle == nil || store.lifecycle.Phase != InitialMirrorEstablished || !gate.Writable() {
		t.Fatalf("decision=%v saves=%d baseline=%v lifecycle=%v writable=%v",
			decision, store.saves, store.baseline, store.lifecycle, gate.Writable())
	}
}

func TestReconcileFirstKeepsCreatingWorkspacePendingBeforeFinalAdministratorAudit(t *testing.T) {
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	database := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1, Sequence: 2,
		Head: bytes.Repeat([]byte{0x22}, 32)}
	store := &memoryMirrorStore{lifecycle: &InitialMirrorLifecycle{WorkspaceID: workspaceID,
		SetupID: "01890f60-4d6d-7c12-8f02-6c9129d5b002", Phase: InitialMirrorCreating}}
	verifier := &fixedMirrorVerifier{verification: VerifiedChain{Baseline: database,
		Heads: map[uint64][]byte{2: database.Head}, Valid: true}}
	gate := NewWriteGate()
	decision, err := NewMirrorReconciler(store, verifier, gate).Open(context.Background(), database)
	if err != nil || decision != MirrorDecisionInitialPending || store.saves != 0 || !gate.initialMirrorPending() ||
		gate.Writable() || gate.EvidenceExportAllowed() {
		t.Fatalf("decision=%v err=%v saves=%d pending=%v writable=%v export=%v",
			decision, err, store.saves, gate.initialMirrorPending(), gate.Writable(), gate.EvidenceExportAllowed())
	}
}

func TestNewWriteGateDoesNotAssumeEveryProcessIsCreatingWorkspace(t *testing.T) {
	gate := NewWriteGate()
	if gate.initialMirrorPending() || gate.Writable() || gate.EvidenceExportAllowed() {
		t.Fatalf("new gate pending=%v writable=%v export=%v",
			gate.initialMirrorPending(), gate.Writable(), gate.EvidenceExportAllowed())
	}
}

func TestInitialMirrorPostCommitCallbackNeverOverwritesConcurrentlyAppearingAheadBaseline(t *testing.T) {
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	lifecycle := &InitialMirrorLifecycle{WorkspaceID: workspaceID,
		SetupID: "01890f60-4d6d-7c12-8f02-6c9129d5b002", Phase: InitialMirrorCreating}
	ahead := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1,
		Sequence: 5, Head: bytes.Repeat([]byte{0x91}, sha256.Size)}
	store := &memoryMirrorStore{lifecycle: lifecycle}
	store.beforeSave = func(*tammyv1.AuditMirrorBaseline) {
		store.baseline = proto.Clone(ahead).(*tammyv1.AuditMirrorBaseline)
	}
	gate := NewWriteGate()
	gate.beginInitialMirror()
	executor := &callbackExecutor{}
	target := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1,
		Sequence: 4, Head: bytes.Repeat([]byte{0x92}, sha256.Size)}
	if err := registerInitialMirrorAfterCommit(executor, store, store, gate, lifecycle, target); err != nil {
		t.Fatal(err)
	}
	if executor.callback == nil {
		t.Fatal("post-commit callback was not registered")
	}
	if err := executor.callback(context.Background()); !errors.Is(err, ErrRollbackDetected) {
		t.Fatalf("callback error=%v, want ErrRollbackDetected", err)
	}
	if store.saves != 0 || store.baseline.Sequence != 5 || !bytes.Equal(store.baseline.Head, bytes.Repeat([]byte{0x91}, sha256.Size)) ||
		gate.Writable() || gate.EvidenceExportAllowed() || store.lifecycle.Phase != InitialMirrorCreating {
		t.Fatalf("saves=%d baseline=%v writable=%v export=%v lifecycle=%v",
			store.saves, store.baseline, gate.Writable(), gate.EvidenceExportAllowed(), store.lifecycle)
	}
}

func TestOrdinaryPostCommitCallbackNeverOverwritesConcurrentlyAdvancedBaseline(t *testing.T) {
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	expected := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1,
		Sequence: 4, Head: bytes.Repeat([]byte{0x81}, sha256.Size)}
	target := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1,
		Sequence: 5, Head: bytes.Repeat([]byte{0x82}, sha256.Size)}
	ahead := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1,
		Sequence: 6, Head: bytes.Repeat([]byte{0x83}, sha256.Size)}
	store := &memoryMirrorStore{baseline: proto.Clone(expected).(*tammyv1.AuditMirrorBaseline)}
	store.beforeSave = func(*tammyv1.AuditMirrorBaseline) {
		store.baseline = proto.Clone(ahead).(*tammyv1.AuditMirrorBaseline)
	}
	gate := NewWriteGate()
	gate.set(true, true)
	executor := &callbackExecutor{}
	if err := registerMirrorAfterCommit(executor, store, gate, expected, target); err != nil {
		t.Fatal(err)
	}
	if err := executor.callback(context.Background()); !errors.Is(err, ErrRollbackDetected) {
		t.Fatalf("callback error=%v, want ErrRollbackDetected", err)
	}
	if store.saves != 0 || !sameBaseline(store.baseline, ahead) || gate.Writable() || !gate.EvidenceExportAllowed() {
		t.Fatalf("saves=%d baseline=%v writable=%v export=%v",
			store.saves, store.baseline, gate.Writable(), gate.EvidenceExportAllowed())
	}
}
