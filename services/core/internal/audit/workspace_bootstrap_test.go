//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
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
			identityEvent, identityPayload := initialAdministratorIdentityEvent(workspaceID, "01890f60-4d6d-7c12-8f02-6c9129d5b006", mutation.OperationID, now)
			identityEvent.Id = "01890f60-4d6d-7c12-8f02-6c9129d5b108"
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
	identityEvent, identityPayload := initialAdministratorIdentityEvent(workspaceID, "01890f60-4d6d-7c12-8f02-6c9129d5b006", operationIDs[2], now)
	identityEvent.Id = "01890f60-4d6d-7c12-8f02-6c9129d5b124"
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

func TestInitialAdministratorProofSurvivesCommittedLockUnlockAfterSignIn(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	now := time.Date(2026, 8, 4, 7, 30, 0, 0, time.UTC)
	store := &memoryMirrorStore{}
	gate := NewWriteGate()
	identifiers := []string{
		"01890f60-4d6d-7c12-8f02-6c9129d5b131", // CREATE event
		"01890f60-4d6d-7c12-8f02-6c9129d5b132", // recovery event
		"01890f60-4d6d-7c12-8f02-6c9129d5b133", // initial workspace session event
		"01890f60-4d6d-7c12-8f02-6c9129d5b134", // LOCK event
		"01890f60-4d6d-7c12-8f02-6c9129d5b135", // UNLOCK event
	}
	adapter, err := NewWorkspaceAuditAdapter(WorkspaceAuditAdapterConfig{Clock: clock.Func(func() time.Time { return now }),
		Random: bytes.NewReader(bytes.Repeat([]byte{0x53}, 256)), NewID: func() (string, error) { id := identifiers[0]; identifiers = identifiers[1:]; return id, nil },
		SchemaFingerprint: bytes.Repeat([]byte{0x44}, sha256.Size), Mirror: store, Gate: gate})
	if err != nil {
		t.Fatal(err)
	}
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	setupID := "01890f60-4d6d-7c12-8f02-6c9129d5b121"
	recoveryID := "01890f60-4d6d-7c12-8f02-6c9129d5b122"
	userID := "01890f60-4d6d-7c12-8f02-6c9129d5b126"
	sessionID := "01890f60-4d6d-7c12-8f02-6c9129d5b127"
	if err := adapter.BeginInitialMirrorLifecycle(ctx, workspaceID, setupID); err != nil {
		t.Fatal(err)
	}

	raw, err := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	transaction := &auditLifecycleTransaction{Transaction: raw}
	if _, err := adapter.BootstrapWorkspaceAudit(ctx, transaction, workspaceID, bytes.Repeat([]byte{0x62}, 32), now); err != nil {
		t.Fatal(err)
	}
	for index, kind := range []string{"CREATE", "RECOVERY_CONFIRMATION", "SESSION_STARTED"} {
		operationID := []string{setupID, recoveryID, sessionID}[index]
		if err := adapter.AppendWorkspaceMutation(ctx, transaction, workspace.WorkspaceMutation{OperationID: operationID, Kind: kind,
			WorkspaceID: workspaceID, Version: 1, SemanticHash: "3123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}); err != nil {
			t.Fatalf("append initial %s transition: %v", kind, err)
		}
	}
	identityEvent, identityPayload := initialAdministratorIdentityEvent(workspaceID, userID, sessionID, now)
	if _, err := adapter.AppendInitialAdministratorAudit(ctx, transaction, setupID, identityEvent, identityPayload); err != nil {
		t.Fatal(err)
	}
	if err := writeInitialWorkspaceRecord(ctx, transaction, workspaceID, setupID, 1,
		tammyv1.WorkspaceState_WORKSPACE_STATE_AUTHENTICATED); err != nil {
		t.Fatal(err)
	}
	if err := adapter.EstablishInitialMirror(ctx, transaction, workspaceID, setupID); err != nil {
		t.Fatalf("first-admin sign-in should register its post-commit publication: %v", err)
	}
	if len(transaction.afterCommit) != 1 {
		t.Fatalf("sign-in registered %d post-commit publications, want 1", len(transaction.afterCommit))
	}
	// Commit the real SQL chain but skip its callback, matching a process crash
	// after the session transaction commits and before the OS baseline is saved.
	if err := transaction.Transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	if store.saves != 0 {
		t.Fatal("crash simulation unexpectedly published the initial OS baseline")
	}

	appendInitialSecurityTransition(t, ctx, database, adapter, workspaceID, setupID, "LOCK",
		tammyv1.WorkspaceState_WORKSPACE_STATE_AUTHENTICATED, tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED,
		"01890f60-4d6d-7c12-8f02-6c9129d5b128")
	appendInitialSecurityTransition(t, ctx, database, adapter, workspaceID, setupID, "UNLOCK",
		tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED, tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED,
		"01890f60-4d6d-7c12-8f02-6c9129d5b129")

	header, err := LoadChainHeader(ctx, database, workspaceID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !initialAdministratorAuditComplete(ctx, database, header) {
		t.Fatal("validated initial administrator-session proof was lost after real LOCK/UNLOCK transitions")
	}
	verifier, err := NewSQLMirrorVerifier(&auditServiceTransactions{database: database, workspaceID: workspaceID})
	if err != nil {
		t.Fatal(err)
	}
	verified, err := verifier.VerifyFull(ctx, workspaceID, header.Generation)
	if err != nil || !verified.InitialAdministratorSessionComplete {
		t.Fatalf("production verifier lost initial session evidence after LOCK/UNLOCK: complete=%v err=%v", verified.InitialAdministratorSessionComplete, err)
	}
	restartedGate := NewWriteGate()
	decision, err := NewMirrorReconciler(store, verifier, restartedGate).Open(ctx,
		&tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: header.Generation,
			Sequence: header.CurrentSequence, Head: append([]byte(nil), header.CurrentHead[:]...)})
	if err != nil || decision != MirrorDecisionInitialEstablished || !restartedGate.Writable() ||
		store.lifecycle == nil || store.lifecycle.Phase != InitialMirrorEstablished || store.baseline == nil ||
		store.baseline.Sequence != header.CurrentSequence || !bytes.Equal(store.baseline.Head, header.CurrentHead[:]) {
		t.Fatalf("restart did not repair the install-bound baseline after LOCK/UNLOCK: decision=%v gate=%v lifecycle=%v baseline=%v err=%v",
			decision, restartedGate.Writable(), store.lifecycle, store.baseline, err)
	}
}

func appendInitialSecurityTransition(t *testing.T, ctx context.Context, database *sqlcipher.Database, adapter *WorkspaceAuditAdapter,
	workspaceID, setupID, kind string, from, to tammyv1.WorkspaceState, operationID string) {
	t.Helper()
	raw, err := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	transaction := &auditLifecycleTransaction{Transaction: raw}
	if err := writeInitialWorkspaceRecord(ctx, transaction, workspaceID, setupID, 1, from); err != nil {
		t.Fatal(err)
	}
	if err := adapter.AppendWorkspaceMutation(ctx, transaction, workspace.WorkspaceMutation{OperationID: operationID, Kind: kind,
		WorkspaceID: workspaceID, Version: 1, SemanticHash: "4123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}); err != nil {
		t.Fatalf("append initial %s transition: %v", kind, err)
	}
	if err := writeInitialWorkspaceRecord(ctx, transaction, workspaceID, setupID, 1, to); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Transaction.Commit(); err != nil {
		t.Fatal(err)
	}
}

func writeInitialWorkspaceRecord(ctx context.Context, executor Executor, workspaceID, setupID string, version uint64,
	state tammyv1.WorkspaceState) error {
	body, err := json.Marshal(struct {
		ID      string
		Version uint64
		State   tammyv1.WorkspaceState
		SetupID string
	}{ID: workspaceID, Version: version, State: state, SetupID: setupID})
	if err != nil {
		return err
	}
	_, err = executor.ExecContext(ctx, `INSERT INTO workspace_metadata(key,value,revision,updated_at) VALUES(?,?,1,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value,revision=workspace_metadata.revision+1,updated_at=excluded.updated_at`,
		"workspace.record.v1", body, time.Date(2026, 8, 4, 7, 30, 0, 0, time.UTC).Format(time.RFC3339Nano))
	return err
}

func initialAdministratorIdentityEvent(workspaceID, userID, sessionID string, now time.Time) (*tammyv1.AuditEvent, []byte) {
	payload := &tammyv1.UserStateChangedEvent{UserId: userID,
		FromState: tammyv1.UserState_USER_STATE_ACTIVE, ToState: tammyv1.UserState_USER_STATE_ACTIVE, ReasonCode: "SIGN_IN"}
	payloadProto, _ := proto.MarshalOptions{Deterministic: true}.Marshal(payload)
	commandID := sessionID
	sessionHash := bytes.Repeat([]byte{0x66}, sha256.Size)
	userHash := bytes.Repeat([]byte{0x67}, sha256.Size)
	return &tammyv1.AuditEvent{
		Id: "01890f60-4d6d-7c12-8f02-6c9129d5b130", WorkspaceId: workspaceID,
		Type: tammyv1.AuditEventType_AUDIT_EVENT_TYPE_USER_STATE_CHANGED, OccurredAt: timestamppb.New(now),
		Actor:                    &tammyv1.AuthenticationContext{ActorUserId: userID, SessionId: sessionID},
		Source:                   &tammyv1.SourceRef{Type: "identity_session", Id: sessionID, Revision: 1, ContentHash: sessionHash},
		AffectedResources:        []*tammyv1.SourceRef{{Type: "identity_user", Id: userID, Revision: 1, ContentHash: userHash}},
		Payload:                  &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_UserStateChanged{UserStateChanged: payload}},
		PayloadSchemaFingerprint: bytes.Repeat([]byte{0x44}, sha256.Size), CommandId: &commandID,
		CommandType: "tammy.v1.IdentityService.SignIn",
		Result:      &tammyv1.AuditResultMetadata{TypeName: "tammy.v1.SignInResponse", DeterministicSha256: bytes.Repeat([]byte{0x68}, sha256.Size), OutcomeCode: "SUCCESS"},
	}, payloadProto
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
