//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
)

type boundedInitialMirrorExecutor struct {
	*auditLifecycleTransaction
	auditQueries []string
	auditArgs    [][]any
}

func (executor *boundedInitialMirrorExecutor) QueryContext(
	ctx context.Context,
	query string,
	arguments ...any,
) (*sql.Rows, error) {
	if strings.Contains(query, "FROM audit_events_v1") {
		executor.auditQueries = append(executor.auditQueries, query)
		executor.auditArgs = append(executor.auditArgs, append([]any(nil), arguments...))
		if !strings.Contains(query, "LIMIT ?") || len(arguments) == 0 {
			return nil, errors.New("initial mirror issued an unbounded audit event query")
		}
		limit, ok := arguments[len(arguments)-1].(int)
		if !ok || limit > int(StoredEventPageSizeLimit) {
			return nil, errors.New("initial mirror exceeded the audit event page limit")
		}
	}
	return executor.auditLifecycleTransaction.QueryContext(ctx, query, arguments...)
}

func TestWorkspaceAuditAdapterBootstrapsChainAndEncryptedSigningKeyInCallerTransaction(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	now := time.Date(2026, 8, 4, 7, 0, 0, 0, time.UTC)
	identifiers := []string{"01890f60-4d6d-7c12-8f02-6c9129d5b101", "01890f60-4d6d-7c12-8f02-6c9129d5b103",
		"01890f60-4d6d-7c12-8f02-6c9129d5b106", "01890f60-4d6d-7c12-8f02-6c9129d5b107"}
	store := &memoryMirrorStore{}
	gate := NewWriteGate()
	adapter, err := NewWorkspaceAuditAdapter(WorkspaceAuditAdapterConfig{
		Clock: clock.Func(func() time.Time { return now }), Random: bytes.NewReader(bytes.Repeat([]byte{0x43}, 256)),
		NewID:             func() (string, error) { id := identifiers[0]; identifiers = identifiers[1:]; return id, nil },
		SchemaFingerprint: bytes.Repeat([]byte{0x44}, sha256.Size),
		Mirror:            store, Gate: gate,
	})
	if err != nil {
		t.Fatal(err)
	}
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	setupID := "01890f60-4d6d-7c12-8f02-6c9129d5b102"
	if err := adapter.BeginInitialMirrorLifecycle(ctx, workspaceID, setupID); err != nil {
		t.Fatal(err)
	}
	dek := bytes.Repeat([]byte{0x52}, 32)
	raw, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	transaction := &auditLifecycleTransaction{Transaction: raw}
	metadata, err := adapter.BootstrapWorkspaceAudit(ctx, transaction, workspaceID, dek, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.AppendWorkspaceMutation(ctx, transaction, workspace.WorkspaceMutation{
		OperationID: "01890f60-4d6d-7c12-8f02-6c9129d5b102", Kind: "CREATE", WorkspaceID: workspaceID, Version: 1,
		SemanticHash: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", HeaderOperation: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Rollback(); err != nil {
		t.Fatal(err)
	}
	if store.saves != 0 {
		t.Fatalf("rolled-back mirror saves=%d", store.saves)
	}
	var count int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM audit_signing_keys_v1`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("bootstrap committed independently: count=%d err=%v", count, err)
	}
	raw, _ = database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	transaction = &auditLifecycleTransaction{Transaction: raw}
	metadata, err = adapter.BootstrapWorkspaceAudit(ctx, transaction, workspaceID, dek, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.AppendWorkspaceMutation(ctx, transaction, workspace.WorkspaceMutation{
		OperationID: "01890f60-4d6d-7c12-8f02-6c9129d5b102", Kind: "CREATE", WorkspaceID: workspaceID, Version: 1,
		SemanticHash: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", HeaderOperation: true,
	}); err != nil {
		t.Fatal(err)
	}
	if store.saves != 0 {
		t.Fatal("bootstrap mirror was published before commit")
	}
	if err := transaction.CommitAndPublish(ctx); err != nil {
		t.Fatal(err)
	}
	if store.saves != 0 || store.baseline != nil || gate.Writable() {
		t.Fatalf("creation established mirror early: baseline=%v saves=%d writable=%v", store.baseline, store.saves, gate.Writable())
	}
	if len(metadata.ChainSalt) != 32 || len(metadata.GenesisHash) != 32 || len(metadata.SigningPublicKey) != 32 || metadata.SigningKeyID == "" {
		t.Fatalf("public header metadata = %#v", metadata)
	}
	loaded, err := adapter.LoadWorkspaceAuditHeader(ctx, database, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(loaded.ChainSalt, metadata.ChainSalt) || !bytes.Equal(loaded.GenesisHash, metadata.GenesisHash) ||
		!bytes.Equal(loaded.SigningPublicKey, metadata.SigningPublicKey) || loaded.SigningKeyID != metadata.SigningKeyID {
		t.Fatalf("loaded public header changed: %#v != %#v", loaded, metadata)
	}
	header, err := LoadChainHeader(ctx, database, workspaceID, 1)
	if err != nil || header.CurrentSequence != 1 {
		t.Fatalf("bootstrapped chain header=%#v err=%v", header, err)
	}
	record, err := LoadSigningKey(ctx, database, workspaceID, metadata.SigningKeyID)
	if err != nil {
		t.Fatal(err)
	}
	privateKey, err := DecryptSigningKey(record, dek)
	if err != nil {
		t.Fatal(err)
	}
	Zero(privateKey)

	for index, mutation := range []workspace.WorkspaceMutation{
		{OperationID: "01890f60-4d6d-7c12-8f02-6c9129d5b104", Kind: "RECOVERY_CONFIRMATION", WorkspaceID: workspaceID, Version: 1,
			SemanticHash: "1123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
		{OperationID: "01890f60-4d6d-7c12-8f02-6c9129d5b105", Kind: "SESSION_STARTED", WorkspaceID: workspaceID, Version: 1,
			SemanticHash: "2123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
	} {
		raw, _ = database.BeginEncryptedTx(ctx, nil)
		transaction = &auditLifecycleTransaction{Transaction: raw}
		if err := adapter.AppendWorkspaceMutation(ctx, transaction, mutation); err != nil {
			t.Fatal(err)
		}
		if mutation.Kind == "RECOVERY_CONFIRMATION" {
			if err := adapter.EstablishInitialMirror(ctx, transaction, workspaceID, setupID); !errors.Is(err, ErrMirrorInvalid) {
				t.Fatalf("premature establishment error=%v, want ErrMirrorInvalid", err)
			}
		}
		if mutation.Kind == "SESSION_STARTED" {
			identityEvent, identityPayload := integrationAuditEvent("01890f60-4d6d-7c12-8f02-6c9129d5b108")
			if _, err := adapter.AppendInitialAdministratorAudit(ctx, transaction, setupID, identityEvent, identityPayload); err != nil {
				t.Fatal(err)
			}
			bounded := &boundedInitialMirrorExecutor{auditLifecycleTransaction: transaction}
			if err := adapter.EstablishInitialMirror(ctx, bounded, workspaceID, setupID); err != nil {
				t.Fatal(err)
			}
			if len(bounded.auditQueries) != 2 {
				t.Fatalf("initial mirror audit queries=%d, want one metadata and one body query: %q",
					len(bounded.auditQueries), bounded.auditQueries)
			}
			metadataQueries, bodyQueries := 0, 0
			for _, query := range bounded.auditQueries {
				normalized := strings.Join(strings.Fields(query), " ")
				if strings.HasPrefix(normalized, "SELECT sequence, length(payload_type),") {
					metadataQueries++
				}
				if strings.HasPrefix(normalized, "SELECT sequence, payload_type, payload_proto") {
					bodyQueries++
				}
			}
			if metadataQueries != 1 || bodyQueries != 1 {
				t.Fatalf("initial mirror metadata queries=%d body queries=%d: %q",
					metadataQueries, bodyQueries, bounded.auditQueries)
			}
		}
		if err := transaction.CommitAndPublish(ctx); err != nil {
			t.Fatal(err)
		}
		if index == 0 && (store.saves != 0 || gate.Writable()) {
			t.Fatalf("recovery confirmation established mirror early: saves=%d writable=%v", store.saves, gate.Writable())
		}
	}
	if store.saves != 1 || store.baseline == nil || store.baseline.Sequence != 4 || !gate.Writable() {
		t.Fatalf("administrator sign-in baseline=%v saves=%d writable=%v", store.baseline, store.saves, gate.Writable())
	}
	stored, err := LoadStoredEvents(ctx, database, workspaceID, 1, 1, 4)
	if err != nil {
		t.Fatal(err)
	}
	wantSessionPayload := `{"from_state":"WORKSPACE_STATE_UNAUTHENTICATED","reason_code":"SESSION_STARTED","to_state":"WORKSPACE_STATE_AUTHENTICATED","workspace_id":"01890f60-4d6d-7c12-8f02-6c9129d5b001"}`
	if got := string(stored[2].PayloadJSON); got != wantSessionPayload {
		t.Fatalf("session transition payload = %s, want %s", got, wantSessionPayload)
	}
}

func TestWorkspaceAuditAdapterRepairsInitialMirrorAfterCommittedSignInCallbackCrash(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	now := time.Date(2026, 8, 4, 7, 30, 0, 0, time.UTC)
	store := &memoryMirrorStore{}
	gate := NewWriteGate()
	identifiers := []string{"01890f60-4d6d-7c12-8f02-6c9129d5b111", "01890f60-4d6d-7c12-8f02-6c9129d5b112", "01890f60-4d6d-7c12-8f02-6c9129d5b113"}
	adapter, err := NewWorkspaceAuditAdapter(WorkspaceAuditAdapterConfig{Clock: clock.Func(func() time.Time { return now }),
		Random: bytes.NewReader(bytes.Repeat([]byte{0x53}, 256)), NewID: func() (string, error) { id := identifiers[0]; identifiers = identifiers[1:]; return id, nil },
		SchemaFingerprint: bytes.Repeat([]byte{0x44}, sha256.Size), Mirror: store, Gate: gate})
	if err != nil {
		t.Fatal(err)
	}
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	setupID := "01890f60-4d6d-7c12-8f02-6c9129d5b121"
	if err := adapter.BeginInitialMirrorLifecycle(ctx, workspaceID, setupID); err != nil {
		t.Fatal(err)
	}
	raw, _ := database.BeginEncryptedTx(ctx, nil)
	transaction := &auditLifecycleTransaction{Transaction: raw}
	if _, err := adapter.BootstrapWorkspaceAudit(ctx, transaction, workspaceID, bytes.Repeat([]byte{0x62}, 32), now); err != nil {
		t.Fatal(err)
	}
	operationIDs := []string{"01890f60-4d6d-7c12-8f02-6c9129d5b121", "01890f60-4d6d-7c12-8f02-6c9129d5b122", "01890f60-4d6d-7c12-8f02-6c9129d5b123"}
	for index, kind := range []string{"CREATE", "RECOVERY_CONFIRMATION", "SESSION_STARTED"} {
		if err := adapter.AppendWorkspaceMutation(ctx, transaction, workspace.WorkspaceMutation{OperationID: operationIDs[index], Kind: kind,
			WorkspaceID: workspaceID, Version: 1, SemanticHash: "3123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}); err != nil {
			t.Fatal(err)
		}
	}
	identityEvent, identityPayload := integrationAuditEvent("01890f60-4d6d-7c12-8f02-6c9129d5b124")
	if _, err := adapter.AppendInitialAdministratorAudit(ctx, transaction, setupID, identityEvent, identityPayload); err != nil {
		t.Fatal(err)
	}
	if err := adapter.EstablishInitialMirror(ctx, transaction, workspaceID, setupID); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	if store.saves != 0 {
		t.Fatal("crash simulation unexpectedly published initial mirror")
	}

	restartedGate := NewWriteGate()
	header, err := LoadChainHeader(ctx, database, workspaceID, 1)
	if err != nil {
		t.Fatal(err)
	}
	databaseBaseline := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: header.Generation,
		Sequence: header.CurrentSequence, Head: append([]byte(nil), header.CurrentHead[:]...)}
	verifier := &fixedMirrorVerifier{verification: VerifiedChain{Baseline: databaseBaseline,
		Heads: map[uint64][]byte{4: databaseBaseline.Head}, Valid: true, InitialAdministratorSessionComplete: true}}
	decision, err := NewMirrorReconciler(store, verifier, restartedGate).Open(ctx, databaseBaseline)
	if err != nil {
		t.Fatal(err)
	}
	if decision != MirrorDecisionInitialEstablished || store.saves != 1 || store.baseline.Sequence != 4 ||
		store.lifecycle.Phase != InitialMirrorEstablished || !restartedGate.Writable() {
		t.Fatalf("restarted decision=%v baseline=%v lifecycle=%v saves=%d writable=%v",
			decision, store.baseline, store.lifecycle, store.saves, restartedGate.Writable())
	}
}

func TestRestartedInitialSignInBeforeReconcileCannotOverwriteMirrorAhead(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	setupID := "01890f60-4d6d-7c12-8f02-6c9129d5b121"
	salt := bytes.Repeat([]byte{0x71}, sha256.Size)
	genesis, err := Genesis(workspaceID, salt)
	if err != nil {
		t.Fatal(err)
	}
	setup, err := database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := InitializeChain(ctx, setup, ChainHeader{WorkspaceID: workspaceID, Generation: 1,
		ChainSalt: salt, GenesisHash: genesis, CurrentHead: genesis, CreatedAt: time.Date(2026, 8, 4, 8, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	if err := setup.Commit(); err != nil {
		t.Fatal(err)
	}
	store := &memoryMirrorStore{baseline: &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1,
		Sequence: 1, Head: bytes.Repeat([]byte{0x72}, sha256.Size)},
		lifecycle: &InitialMirrorLifecycle{WorkspaceID: workspaceID, SetupID: setupID, Phase: InitialMirrorCreating}}
	gate := NewWriteGate()
	adapter, err := NewWorkspaceAuditAdapter(WorkspaceAuditAdapterConfig{Clock: clock.Func(func() time.Time {
		return time.Date(2026, 8, 4, 8, 1, 0, 0, time.UTC)
	}), Random: bytes.NewReader(bytes.Repeat([]byte{0x73}, 128)), NewID: func() (string, error) {
		return "01890f60-4d6d-7c12-8f02-6c9129d5b122", nil
	}, SchemaFingerprint: bytes.Repeat([]byte{0x44}, sha256.Size), Mirror: store, Gate: gate})
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.BeginInitialMirrorLifecycle(ctx, workspaceID, setupID); err != nil {
		t.Fatal(err)
	}
	transaction, err := database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	identityEvent, identityPayload := integrationAuditEvent("01890f60-4d6d-7c12-8f02-6c9129d5b123")
	if _, err := adapter.AppendInitialAdministratorAudit(ctx, transaction, setupID, identityEvent, identityPayload); !errors.Is(err, ErrWriteGate) {
		t.Fatalf("sign-in before reconcile error=%v, want ErrWriteGate", err)
	}
	databaseBaseline := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1, Head: genesis[:]}
	verifier := &fixedMirrorVerifier{verification: VerifiedChain{Baseline: databaseBaseline,
		Heads: map[uint64][]byte{0: genesis[:]}, Valid: true}}
	decision, err := NewMirrorReconciler(store, verifier, gate).Open(ctx, databaseBaseline)
	if decision != MirrorDecisionRollbackDenied || !errors.Is(err, ErrRollbackDetected) || gate.Writable() ||
		gate.initialMirrorPending() || store.saves != 0 || store.baseline.Sequence != 1 {
		t.Fatalf("decision=%v err=%v pending=%v writable=%v saves=%d baseline=%v",
			decision, err, gate.initialMirrorPending(), gate.Writable(), store.saves, store.baseline)
	}
}

func TestInitialLifecycleCannotBeCreatedOverExistingBaseline(t *testing.T) {
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	store := &memoryMirrorStore{baseline: &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1,
		Sequence: 7, Head: bytes.Repeat([]byte{0x81}, sha256.Size)}}
	gate := NewWriteGate()
	adapter, err := NewWorkspaceAuditAdapter(WorkspaceAuditAdapterConfig{Clock: clock.Func(time.Now),
		Random: bytes.NewReader(bytes.Repeat([]byte{0x82}, 128)), NewID: func() (string, error) {
			return "01890f60-4d6d-7c12-8f02-6c9129d5b122", nil
		}, SchemaFingerprint: bytes.Repeat([]byte{0x44}, sha256.Size), Mirror: store, Gate: gate})
	if err != nil {
		t.Fatal(err)
	}
	err = adapter.BeginInitialMirrorLifecycle(context.Background(), workspaceID, "01890f60-4d6d-7c12-8f02-6c9129d5b121")
	if !errors.Is(err, ErrWriteGate) || store.lifecycle != nil || gate.initialMirrorPending() || gate.Writable() {
		t.Fatalf("begin error=%v lifecycle=%v pending=%v writable=%v", err, store.lifecycle, gate.initialMirrorPending(), gate.Writable())
	}
}
