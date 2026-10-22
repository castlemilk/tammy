//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type auditLifecycleTransaction struct {
	*sqlcipher.Transaction
	afterCommit []func(context.Context) error
}

func (transaction *auditLifecycleTransaction) AfterCommit(callback func(context.Context) error) error {
	if callback == nil {
		return errors.New("nil post-commit callback")
	}
	transaction.afterCommit = append(transaction.afterCommit, callback)
	return nil
}

func (transaction *auditLifecycleTransaction) CommitAndPublish(ctx context.Context) error {
	if err := transaction.Transaction.Commit(); err != nil {
		return err
	}
	for _, callback := range transaction.afterCommit {
		if err := callback(ctx); err != nil {
			return err
		}
	}
	return nil
}

type failingMirrorStore struct{ memoryMirrorStore }

func (*failingMirrorStore) CompareAndSwap(context.Context, *tammyv1.AuditMirrorBaseline, *tammyv1.AuditMirrorBaseline) error {
	return errors.New("credential store unavailable")
}

func newEncryptedAuditDatabase(t *testing.T) *sqlcipher.Database {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "audit.db")
	key := bytes.Repeat([]byte{0x41}, sqlcipher.KeySize)
	if _, err := sqlcipher.MigrateWorkspace(ctx, path, key, 3); err != nil {
		t.Fatalf("migrate audit database: %v", err)
	}
	database, err := sqlcipher.Open(ctx, path, key)
	if err != nil {
		t.Fatalf("open audit database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func TestAuditChainHeaderRejectsDirectSQLTampering(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		statement string
	}{
		{name: "workspace identity", statement: `UPDATE audit_chain_headers_v1 SET workspace_id='workspace-tampered'`},
		{name: "generation identity", statement: `UPDATE audit_chain_headers_v1 SET generation=2`},
		{name: "chain salt", statement: `UPDATE audit_chain_headers_v1 SET chain_salt=zeroblob(32)`},
		{name: "genesis", statement: `UPDATE audit_chain_headers_v1 SET genesis_hash=zeroblob(32), current_head=zeroblob(32)`},
		{name: "creation time", statement: `UPDATE audit_chain_headers_v1 SET created_at='2026-08-05T01:00:00.000000000Z'`},
		{name: "delete", statement: `DELETE FROM audit_chain_headers_v1`},
		{name: "sequence skip", statement: `UPDATE audit_chain_headers_v1 SET current_sequence=2, current_head=randomblob(32)`},
		{name: "unlinked head", statement: `UPDATE audit_chain_headers_v1 SET current_sequence=1, current_head=randomblob(32)`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			database := newEncryptedAuditDatabase(t)
			workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
			salt := bytes.Repeat([]byte{0x23}, sha256.Size)
			genesis, err := Genesis(workspaceID, salt)
			if err != nil {
				t.Fatal(err)
			}
			if err := InitializeChain(ctx, database, ChainHeader{
				WorkspaceID: workspaceID, Generation: 1, ChainSalt: salt, GenesisHash: genesis,
				CurrentHead: genesis, CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC),
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := database.ExecContext(ctx, testCase.statement); err == nil {
				t.Fatal("direct SQL header tampering succeeded")
			}
		})
	}
}

func TestAuditChainHeaderRejectsFakeLinkedEventThatRepeatsCurrentHead(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	salt := bytes.Repeat([]byte{0x23}, sha256.Size)
	genesis, err := Genesis(workspaceID, salt)
	if err != nil {
		t.Fatal(err)
	}
	if err := InitializeChain(ctx, database, ChainHeader{
		WorkspaceID: workspaceID, Generation: 1, ChainSalt: salt, GenesisHash: genesis,
		CurrentSequence: 3, CurrentHead: genesis,
		CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO audit_events_v1(
		workspace_id, generation, sequence, event_id, event_type, occurred_at, command_type,
		payload_type, payload_schema_fingerprint, payload_proto, payload_json, canonical_event,
		event_proto, previous_hash, event_hash
	) VALUES (?, 1, 4, 'fake-linked-event', 1, '2026-08-05T01:00:00.000000000Z',
		'fake.command', 'fake.payload', zeroblob(32), x'', x'', x'', x'', ?, ?)`,
		workspaceID, genesis[:], genesis[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE audit_chain_headers_v1
		SET current_sequence=4, current_head=? WHERE workspace_id=? AND generation=1`, genesis[:], workspaceID); err == nil {
		t.Fatal("header advanced without changing its current head")
	}
}

func TestAuditChainHeaderRejectsInsertOrReplace(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	salt := bytes.Repeat([]byte{0x23}, sha256.Size)
	genesis, err := Genesis(workspaceID, salt)
	if err != nil {
		t.Fatal(err)
	}
	if err := InitializeChain(ctx, database, ChainHeader{
		WorkspaceID: workspaceID, Generation: 1, ChainSalt: salt, GenesisHash: genesis,
		CurrentHead: genesis, CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `INSERT OR REPLACE INTO audit_chain_headers_v1(
		workspace_id, generation, chain_salt, genesis_hash, current_sequence, current_head, created_at
	) VALUES (?, 1, randomblob(32), randomblob(32), 9, randomblob(32),
		'2026-08-05T01:00:00.000000000Z')`, workspaceID); err == nil {
		t.Fatal("INSERT OR REPLACE mutated an existing audit chain header")
	}
}

func TestAuditChainHeadersRejectInvalidCreationTimestamps(t *testing.T) {
	for _, createdAt := range []string{
		"not-a-time",
		"10000-01-01T00:00:00.000000000Z",
	} {
		t.Run(createdAt, func(t *testing.T) {
			ctx := context.Background()
			database := newEncryptedAuditDatabase(t)
			if _, err := database.ExecContext(ctx, `INSERT INTO audit_chain_headers_v1(
				workspace_id, generation, chain_salt, genesis_hash, current_sequence, current_head, created_at
			) VALUES ('01890f60-4d6d-7c12-8f02-6c9129d5b001', 1, zeroblob(32), zeroblob(32), 0,
				zeroblob(32), ?)`, createdAt); err == nil {
				t.Fatal("audit chain header accepted an invalid creation timestamp")
			}
		})
	}
}

func TestAuditEventsRejectInsertOrReplaceConflicts(t *testing.T) {
	for _, testCase := range []struct {
		name           string
		sequence       uint64
		eventID        string
		reuseEventHash bool
	}{
		{name: "primary key", sequence: 1, eventID: "replacement-by-primary-key"},
		{name: "event id", sequence: 2, eventID: "original-event"},
		{name: "event hash", sequence: 2, eventID: "replacement-by-event-hash", reuseEventHash: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			database := newEncryptedAuditDatabase(t)
			workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
			salt := bytes.Repeat([]byte{0x23}, sha256.Size)
			genesis, err := Genesis(workspaceID, salt)
			if err != nil {
				t.Fatal(err)
			}
			if err := InitializeChain(ctx, database, ChainHeader{
				WorkspaceID: workspaceID, Generation: 1, ChainSalt: salt, GenesisHash: genesis,
				CurrentHead: genesis, CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC),
			}); err != nil {
				t.Fatal(err)
			}
			originalHash := bytes.Repeat([]byte{0x31}, sha256.Size)
			if _, err := database.ExecContext(ctx, `INSERT INTO audit_events_v1(
				workspace_id, generation, sequence, event_id, event_type, occurred_at, command_type,
				payload_type, payload_schema_fingerprint, payload_proto, payload_json, canonical_event,
				event_proto, previous_hash, event_hash
			) VALUES (?, 1, 1, 'original-event', 1, '2026-08-05T01:00:00.000000000Z',
				'original.command', 'original.payload', zeroblob(32), x'01', x'01', x'01', x'01', ?, ?)`,
				workspaceID, genesis[:], originalHash); err != nil {
				t.Fatal(err)
			}
			replacementHash := bytes.Repeat([]byte{0x32}, sha256.Size)
			if testCase.reuseEventHash {
				replacementHash = originalHash
			}
			if _, err := database.ExecContext(ctx, `INSERT OR REPLACE INTO audit_events_v1(
				workspace_id, generation, sequence, event_id, event_type, occurred_at, command_type,
				payload_type, payload_schema_fingerprint, payload_proto, payload_json, canonical_event,
				event_proto, previous_hash, event_hash
			) VALUES (?, 1, ?, ?, 2, '2026-08-05T02:00:00.000000000Z',
				'replacement.command', 'replacement.payload', zeroblob(32), x'02', x'02', x'02', x'02', ?, ?)`,
				workspaceID, testCase.sequence, testCase.eventID, genesis[:], replacementHash); err == nil {
				t.Fatal("INSERT OR REPLACE mutated an immutable audit event")
			}
		})
	}
}

func TestMovedWorkspaceTrustUsesProductionAppenderWhileOrdinaryWritesStayLocked(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		kind     TrustProofKind
		approval TrustApproval
	}{
		{name: "normal", kind: TrustProofNormal, approval: TrustApproval{
			Actor: &tammyv1.AuthenticationContext{ActorUserId: "01890f60-4d6d-7c12-8f02-6c9129d5b006",
				SessionId: "01890f60-4d6d-7c12-8f02-6c9129d5b007"},
			PassphraseVerified: true, AdministratorPasswordVerified: true, FreshTOTPVerified: true,
		}},
		{name: "recovery_break_glass", kind: TrustProofRecoveryBreakGlass, approval: TrustApproval{
			Actor: &tammyv1.AuthenticationContext{ActorUserId: "01890f60-4d6d-7c12-8f02-6c9129d5b006",
				SessionId: "01890f60-4d6d-7c12-8f02-6c9129d5b007"},
			RecoveryProofVerified: true, AdministratorBreakGlassAudited: true,
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			database := newEncryptedAuditDatabase(t)
			workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
			priorHead := bytes.Repeat([]byte{0x22}, sha256.Size)
			prior := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1, Sequence: 3, Head: priorHead}
			setup, err := database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			salt := bytes.Repeat([]byte{0x23}, sha256.Size)
			genesis, _ := Genesis(workspaceID, salt)
			var head [sha256.Size]byte
			copy(head[:], priorHead)
			if err := InitializeChain(ctx, setup, ChainHeader{WorkspaceID: workspaceID, Generation: 1,
				ChainSalt: salt, GenesisHash: genesis, CurrentSequence: 3, CurrentHead: head,
				CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)}); err != nil {
				t.Fatal(err)
			}
			if err := setup.Commit(); err != nil {
				t.Fatal(err)
			}
			store := &memoryMirrorStore{}
			gate := NewWriteGate()
			appender, err := NewMirroringAppender(store, gate)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			transaction := &auditLifecycleTransaction{Transaction: raw}
			proof := &fixedTrustProofVerifier{approval: testCase.approval}
			verifier := &fixedMirrorVerifier{}
			coordinator := NewTrustCoordinator(proof, verifier, appender)
			if _, err := coordinator.Establish(ctx, transaction, TrustCommand{
				ProofKind: testCase.kind, Prior: prior,
				DestinationInstallationID: "01890f60-4d6d-7c12-8f02-6c9129d5b009",
				EventID:                   "01890f60-4d6d-7c12-8f02-6c9129d5b008", CommandID: "01890f60-4d6d-7c12-8f02-6c9129d5b00a",
				IdempotencyKey: "01890f60-4d6d-7c12-8f02-6c9129d5b004",
				OccurredAt:     time.Date(2026, 8, 4, 1, 2, 3, 0, time.UTC), SchemaFingerprint: bytes.Repeat([]byte{0x44}, sha256.Size),
				ResultHash: bytes.Repeat([]byte{0x55}, sha256.Size),
			}); !errors.Is(err, ErrTrustProof) {
				t.Fatalf("non-moved trust error=%v, want ErrTrustProof", err)
			}
			DeclineMovedTrust(gate)
			ordinary, ordinaryPayload := integrationAuditEvent("01890f60-4d6d-7c12-8f02-6c9129d5b010")
			if _, err := appender.Append(ctx, transaction, ordinary, ordinaryPayload); !errors.Is(err, ErrWriteGate) {
				t.Fatalf("ordinary locked append error=%v, want ErrWriteGate", err)
			}
			pending, err := coordinator.Establish(ctx, transaction, TrustCommand{
				ProofKind: testCase.kind, Prior: prior,
				DestinationInstallationID: "01890f60-4d6d-7c12-8f02-6c9129d5b009",
				EventID:                   "01890f60-4d6d-7c12-8f02-6c9129d5b008", CommandID: "01890f60-4d6d-7c12-8f02-6c9129d5b00a",
				IdempotencyKey: "01890f60-4d6d-7c12-8f02-6c9129d5b004",
				OccurredAt:     time.Date(2026, 8, 4, 1, 2, 3, 0, time.UTC), SchemaFingerprint: bytes.Repeat([]byte{0x44}, sha256.Size),
				ResultHash: bytes.Repeat([]byte{0x55}, sha256.Size),
			})
			if err != nil {
				t.Fatal(err)
			}
			if pending == nil || gate.Writable() || !gate.EvidenceExportAllowed() || store.saves != 0 {
				t.Fatalf("pending=%v writable=%v evidence=%v saves=%d", pending, gate.Writable(), gate.EvidenceExportAllowed(), store.saves)
			}
			verifier.verification = verifiedTrustPublication(prior, pending.baseline)
			stored, err := LoadStoredEvents(ctx, transaction, workspaceID, 1, 4, 4)
			wantInstallationHash := sha256.Sum256([]byte("01890f60-4d6d-7c12-8f02-6c9129d5b009"))
			var trustPayload *tammyv1.WorkspaceTrustEstablishedEvent
			if len(stored) == 1 && stored[0].Event != nil {
				trustPayload = stored[0].Event.GetPayload().GetWorkspaceTrustEstablished()
			}
			if err != nil || len(stored) != 1 || stored[0].Event.Type != tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_TRUST_ESTABLISHED ||
				stored[0].Event.Generation != prior.Generation || stored[0].Event.Sequence != prior.Sequence+1 ||
				!bytes.Equal(stored[0].Event.PreviousHash, prior.Head) || trustPayload == nil || trustPayload.WorkspaceId != workspaceID ||
				!trustPayload.PriorMirrorUnavailable || !bytes.Equal(trustPayload.PriorHead, prior.Head) ||
				!bytes.Equal(trustPayload.DestinationInstallationHash, wantInstallationHash[:]) {
				t.Fatalf("stored trust event=%#v err=%v", stored, err)
			}
			if err := transaction.CommitAndPublish(ctx); err != nil {
				t.Fatal(err)
			}
			if store.saves != 1 || store.baseline == nil || store.baseline.Sequence != 4 || !gate.Writable() || !gate.EvidenceExportAllowed() {
				t.Fatalf("baseline=%v saves=%d writable=%v evidence=%v", store.baseline, store.saves, gate.Writable(), gate.EvidenceExportAllowed())
			}
		})
	}
}

func TestMovedWorkspaceTrustRollbackAndMirrorFailureRemainReadOnly(t *testing.T) {
	ctx := context.Background()
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	priorHead := bytes.Repeat([]byte{0x22}, sha256.Size)
	command := TrustCommand{
		ProofKind:                 TrustProofNormal,
		Prior:                     &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1, Sequence: 3, Head: priorHead},
		DestinationInstallationID: "01890f60-4d6d-7c12-8f02-6c9129d5b009",
		EventID:                   "01890f60-4d6d-7c12-8f02-6c9129d5b008", CommandID: "01890f60-4d6d-7c12-8f02-6c9129d5b00a",
		IdempotencyKey: "01890f60-4d6d-7c12-8f02-6c9129d5b004",
		OccurredAt:     time.Date(2026, 8, 4, 1, 2, 3, 0, time.UTC), SchemaFingerprint: bytes.Repeat([]byte{0x44}, sha256.Size),
		ResultHash: bytes.Repeat([]byte{0x55}, sha256.Size),
	}
	approval := TrustApproval{Actor: &tammyv1.AuthenticationContext{
		ActorUserId: "01890f60-4d6d-7c12-8f02-6c9129d5b006", SessionId: "01890f60-4d6d-7c12-8f02-6c9129d5b007",
	}, PassphraseVerified: true, AdministratorPasswordVerified: true, FreshTOTPVerified: true}

	for _, testCase := range []struct {
		name       string
		store      MirrorStore
		rollBack   bool
		wantEvents int
	}{
		{name: "rollback", store: &memoryMirrorStore{}, rollBack: true},
		{name: "mirror_save_failure", store: &failingMirrorStore{}, wantEvents: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			database := newEncryptedAuditDatabase(t)
			setup, err := database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			salt := bytes.Repeat([]byte{0x23}, sha256.Size)
			genesis, _ := Genesis(workspaceID, salt)
			var head [sha256.Size]byte
			copy(head[:], priorHead)
			if err := InitializeChain(ctx, setup, ChainHeader{WorkspaceID: workspaceID, Generation: 1,
				ChainSalt: salt, GenesisHash: genesis, CurrentSequence: 3, CurrentHead: head,
				CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)}); err != nil {
				t.Fatal(err)
			}
			if err := setup.Commit(); err != nil {
				t.Fatal(err)
			}
			gate := NewWriteGate()
			DeclineMovedTrust(gate)
			appender, err := NewMirroringAppender(testCase.store, gate)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			transaction := &auditLifecycleTransaction{Transaction: raw}
			verifier := &fixedMirrorVerifier{}
			pending, err := NewTrustCoordinator(&fixedTrustProofVerifier{approval: approval}, verifier, appender).Establish(ctx, transaction, command)
			if err != nil || pending == nil {
				t.Fatalf("establish pending=%v err=%v", pending, err)
			}
			verifier.verification = verifiedTrustPublication(command.Prior, pending.baseline)
			if testCase.rollBack {
				if err := transaction.Rollback(); err != nil {
					t.Fatal(err)
				}
			} else if err := transaction.CommitAndPublish(ctx); err == nil {
				t.Fatal("mirror save failure unexpectedly succeeded")
			}
			var count int
			if err := database.QueryRowContext(ctx, `SELECT count(*) FROM audit_events_v1`).Scan(&count); err != nil || count != testCase.wantEvents {
				t.Fatalf("events=%d want=%d err=%v", count, testCase.wantEvents, err)
			}
			if gate.Writable() || !gate.EvidenceExportAllowed() {
				t.Fatalf("fail-closed gate writable=%v evidence=%v", gate.Writable(), gate.EvidenceExportAllowed())
			}
		})
	}
}

func TestMovedWorkspaceTrustPostCommitNeverOverwritesConcurrentBaseline(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		appearing   func(*tammyv1.AuditMirrorBaseline, *tammyv1.AuditMirrorBaseline) *tammyv1.AuditMirrorBaseline
		wantSuccess bool
		wantSaves   int
	}{
		{name: "missing", appearing: func(_, _ *tammyv1.AuditMirrorBaseline) *tammyv1.AuditMirrorBaseline { return nil }, wantSuccess: true, wantSaves: 1},
		{name: "equal", appearing: func(_, target *tammyv1.AuditMirrorBaseline) *tammyv1.AuditMirrorBaseline {
			return proto.Clone(target).(*tammyv1.AuditMirrorBaseline)
		}, wantSuccess: true},
		{name: "behind", appearing: func(prior, _ *tammyv1.AuditMirrorBaseline) *tammyv1.AuditMirrorBaseline {
			return proto.Clone(prior).(*tammyv1.AuditMirrorBaseline)
		}, wantSuccess: true, wantSaves: 1},
		{name: "ahead", appearing: func(_, target *tammyv1.AuditMirrorBaseline) *tammyv1.AuditMirrorBaseline {
			return &tammyv1.AuditMirrorBaseline{WorkspaceId: target.WorkspaceId, Generation: target.Generation,
				Sequence: target.Sequence + 1, Head: bytes.Repeat([]byte{0xa1}, sha256.Size)}
		}},
		{name: "diverged", appearing: func(_, target *tammyv1.AuditMirrorBaseline) *tammyv1.AuditMirrorBaseline {
			return &tammyv1.AuditMirrorBaseline{WorkspaceId: target.WorkspaceId, Generation: target.Generation,
				Sequence: target.Sequence, Head: bytes.Repeat([]byte{0xa2}, sha256.Size)}
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			database := newEncryptedAuditDatabase(t)
			workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
			priorHead := bytes.Repeat([]byte{0x22}, sha256.Size)
			prior := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1, Sequence: 3, Head: priorHead}
			setup, err := database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			salt := bytes.Repeat([]byte{0x23}, sha256.Size)
			genesis, _ := Genesis(workspaceID, salt)
			var head [sha256.Size]byte
			copy(head[:], priorHead)
			if err := InitializeChain(ctx, setup, ChainHeader{WorkspaceID: workspaceID, Generation: 1,
				ChainSalt: salt, GenesisHash: genesis, CurrentSequence: 3, CurrentHead: head,
				CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)}); err != nil {
				t.Fatal(err)
			}
			if err := setup.Commit(); err != nil {
				t.Fatal(err)
			}
			store := &memoryMirrorStore{}
			gate := NewWriteGate()
			DeclineMovedTrust(gate)
			appender, err := NewMirroringAppender(store, gate)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			transaction := &auditLifecycleTransaction{Transaction: raw}
			approval := TrustApproval{Actor: &tammyv1.AuthenticationContext{
				ActorUserId: "01890f60-4d6d-7c12-8f02-6c9129d5b006", SessionId: "01890f60-4d6d-7c12-8f02-6c9129d5b007",
			}, PassphraseVerified: true, AdministratorPasswordVerified: true, FreshTOTPVerified: true}
			verifier := &fixedMirrorVerifier{}
			pending, err := NewTrustCoordinator(&fixedTrustProofVerifier{approval: approval}, verifier, appender).Establish(ctx, transaction, TrustCommand{
				ProofKind: TrustProofNormal, Prior: prior,
				DestinationInstallationID: "01890f60-4d6d-7c12-8f02-6c9129d5b009",
				EventID:                   "01890f60-4d6d-7c12-8f02-6c9129d5b008", CommandID: "01890f60-4d6d-7c12-8f02-6c9129d5b00a",
				IdempotencyKey: "01890f60-4d6d-7c12-8f02-6c9129d5b004",
				OccurredAt:     time.Date(2026, 8, 4, 1, 2, 3, 0, time.UTC), SchemaFingerprint: bytes.Repeat([]byte{0x44}, sha256.Size),
				ResultHash: bytes.Repeat([]byte{0x55}, sha256.Size),
			})
			if err != nil {
				t.Fatal(err)
			}
			target := proto.Clone(pending.baseline).(*tammyv1.AuditMirrorBaseline)
			verifier.verification = verifiedTrustPublication(prior, target)
			store.beforeSave = func(*tammyv1.AuditMirrorBaseline) {
				store.baseline = testCase.appearing(prior, target)
			}
			publishErr := transaction.CommitAndPublish(ctx)
			if testCase.wantSuccess && publishErr != nil || !testCase.wantSuccess && publishErr == nil {
				t.Fatalf("publish error=%v want_success=%v", publishErr, testCase.wantSuccess)
			}
			if store.saves != testCase.wantSaves {
				t.Fatalf("saves=%d want=%d baseline=%v", store.saves, testCase.wantSaves, store.baseline)
			}
			if testCase.wantSuccess {
				if !sameBaseline(store.baseline, target) || !gate.Writable() {
					t.Fatalf("successful baseline=%v target=%v writable=%v", store.baseline, target, gate.Writable())
				}
			} else if gate.Writable() || !gate.EvidenceExportAllowed() || sameBaseline(store.baseline, target) {
				t.Fatalf("failed baseline=%v target=%v writable=%v evidence=%v",
					store.baseline, target, gate.Writable(), gate.EvidenceExportAllowed())
			}
		})
	}
}

func verifiedTrustPublication(prior, target *tammyv1.AuditMirrorBaseline) VerifiedChain {
	return VerifiedChain{Baseline: proto.Clone(target).(*tammyv1.AuditMirrorBaseline), Heads: map[uint64][]byte{
		prior.Sequence:  append([]byte(nil), prior.Head...),
		target.Sequence: append([]byte(nil), target.Head...),
	}, Valid: true}
}

func TestAppenderSerializesConcurrentEncryptedAppends(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	setup, err := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	salt := bytes.Repeat([]byte{0x23}, sha256.Size)
	genesis, _ := Genesis("01890f60-4d6d-7c12-8f02-6c9129d5b001", salt)
	if err := InitializeChain(ctx, setup, ChainHeader{
		WorkspaceID: "01890f60-4d6d-7c12-8f02-6c9129d5b001", Generation: 1,
		ChainSalt: salt, GenesisHash: genesis, CurrentHead: genesis,
		CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	if err := setup.Commit(); err != nil {
		t.Fatal(err)
	}
	const appendCount = 12
	errorsByAppend := make(chan error, appendCount)
	var workers sync.WaitGroup
	workers.Add(appendCount)
	for index := range appendCount {
		go func(index int) {
			defer workers.Done()
			transaction, err := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
			if err != nil {
				errorsByAppend <- err
				return
			}
			committed := false
			defer func() {
				if !committed {
					_ = transaction.Rollback()
				}
			}()
			event, payload := integrationAuditEvent(fmt.Sprintf("01890f60-4d6d-7c12-8f02-%012x", index+8))
			event.OccurredAt = timestamppb.New(event.OccurredAt.AsTime().Add(time.Duration(index) * time.Nanosecond))
			if _, err := appendStoredEventForTest(ctx, transaction, event, payload); err != nil {
				errorsByAppend <- err
				return
			}
			if err := transaction.Commit(); err != nil {
				errorsByAppend <- err
				return
			}
			committed = true
		}(index)
	}
	workers.Wait()
	close(errorsByAppend)
	for err := range errorsByAppend {
		t.Errorf("concurrent append: %v", err)
	}
	header, err := LoadChainHeader(ctx, database, "01890f60-4d6d-7c12-8f02-6c9129d5b001", 1)
	if err != nil {
		t.Fatal(err)
	}
	events, err := LoadStoredEvents(ctx, database, header.WorkspaceID, 1, 1, appendCount)
	if err != nil {
		t.Fatal(err)
	}
	if header.CurrentSequence != appendCount || len(events) != appendCount {
		t.Fatalf("serialized appends = head:%d events:%d, want %d", header.CurrentSequence, len(events), appendCount)
	}
	if result := VerifyStoredChain(header, events); result.Integrity != tammyv1.AuditChainIntegrity_AUDIT_CHAIN_INTEGRITY_VALID {
		t.Fatalf("concurrent chain verification = %#v", result)
	}
}

func integrationAuditEvent(id string) (*tammyv1.AuditEvent, []byte) {
	payload := &tammyv1.WorkspaceStateChangedEvent{
		WorkspaceId: "01890f60-4d6d-7c12-8f02-6c9129d5b001",
		FromState:   tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED,
		ToState:     tammyv1.WorkspaceState_WORKSPACE_STATE_AUTHENTICATED,
		ReasonCode:  "SIGNED_IN",
	}
	payloadProto, _ := proto.MarshalOptions{Deterministic: true}.Marshal(payload)
	return &tammyv1.AuditEvent{
		Id: id, WorkspaceId: payload.WorkspaceId,
		Type:       tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_STATE_CHANGED,
		OccurredAt: timestamppb.New(time.Date(2026, 8, 4, 1, 2, 3, 0, time.UTC)),
		Actor: &tammyv1.AuthenticationContext{
			ActorUserId: "01890f60-4d6d-7c12-8f02-6c9129d5b006",
			SessionId:   "01890f60-4d6d-7c12-8f02-6c9129d5b007",
		},
		Source: &tammyv1.SourceRef{Type: "workspace", Id: payload.WorkspaceId, Revision: 1,
			ContentHash: bytes.Repeat([]byte{0x31}, sha256.Size)},
		Payload: &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_WorkspaceStateChanged{
			WorkspaceStateChanged: payload,
		}},
		PayloadSchemaFingerprint: bytes.Repeat([]byte{0x44}, sha256.Size),
		CommandType:              "tammy.v1.IdentityService.SignIn",
		AffectedResources: []*tammyv1.SourceRef{{Type: "workspace", Id: payload.WorkspaceId, Revision: 1,
			ContentHash: bytes.Repeat([]byte{0x31}, sha256.Size)}},
		Result: &tammyv1.AuditResultMetadata{
			TypeName: "tammy.v1.SignInResponse", DeterministicSha256: bytes.Repeat([]byte{0x55}, sha256.Size), OutcomeCode: "OK",
		},
	}, payloadProto
}

func TestAppenderUsesCallerOwnedEncryptedTransaction(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	salt := bytes.Repeat([]byte{0x23}, sha256.Size)
	genesis, err := Genesis("01890f60-4d6d-7c12-8f02-6c9129d5b001", salt)
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	if err := InitializeChain(ctx, transaction, ChainHeader{
		WorkspaceID: "01890f60-4d6d-7c12-8f02-6c9129d5b001", Generation: 1,
		ChainSalt: salt, GenesisHash: genesis, CurrentHead: genesis,
		CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	event, payload := integrationAuditEvent("01890f60-4d6d-7c12-8f02-6c9129d5b008")
	stored, err := appendStoredEventForTest(ctx, transaction, event, payload)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Event.Sequence != 1 || stored.Event.Generation != 1 {
		t.Fatalf("assigned position = %d/%d", stored.Event.Generation, stored.Event.Sequence)
	}
	if err := transaction.Rollback(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM audit_events_v1`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("appender committed independently: count=%d", count)
	}
}

func TestOrdinaryAppenderCannotWriteWithoutMirrorAndGate(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	transaction, err := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	salt := bytes.Repeat([]byte{0x23}, sha256.Size)
	genesis, _ := Genesis(workspaceID, salt)
	if err := InitializeChain(ctx, transaction, ChainHeader{WorkspaceID: workspaceID, Generation: 1,
		ChainSalt: salt, GenesisHash: genesis, CurrentHead: genesis,
		CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	event, payload := integrationAuditEvent("01890f60-4d6d-7c12-8f02-6c9129d5b008")
	if _, err := (&Appender{}).Append(ctx, transaction, event, payload); !errors.Is(err, ErrWriteGate) {
		t.Fatalf("ungated append error=%v, want ErrWriteGate", err)
	}
}

func TestMirroringAppenderPublishesOnlyAfterCallerCommit(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	salt := bytes.Repeat([]byte{0x23}, sha256.Size)
	genesis, _ := Genesis(workspaceID, salt)
	setup, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err := InitializeChain(ctx, setup, ChainHeader{WorkspaceID: workspaceID, Generation: 1,
		ChainSalt: salt, GenesisHash: genesis, CurrentHead: genesis,
		CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	if err := setup.Commit(); err != nil {
		t.Fatal(err)
	}

	initial := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1, Head: genesis[:]}
	store := &memoryMirrorStore{baseline: proto.Clone(initial).(*tammyv1.AuditMirrorBaseline)}
	gate := NewWriteGate()
	gate.set(true, true)
	appender, err := NewMirroringAppender(store, gate)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	transaction := &auditLifecycleTransaction{Transaction: raw}
	event, payload := integrationAuditEvent("01890f60-4d6d-7c12-8f02-6c9129d5b008")
	stored, err := appender.Append(ctx, transaction, event, payload)
	if err != nil {
		t.Fatal(err)
	}
	if store.saves != 0 || !sameBaseline(store.baseline, initial) {
		t.Fatal("mirror was published before SQL commit")
	}
	if err := transaction.CommitAndPublish(ctx); err != nil {
		t.Fatal(err)
	}
	if store.saves != 1 || store.baseline.Sequence != 1 ||
		!bytes.Equal(store.baseline.Head, stored.Event.EventHash) {
		t.Fatalf("post-commit baseline=%v saves=%d", store.baseline, store.saves)
	}
}

func TestMirroringAppenderPublishesCommittedTransactionsInChainOrderWhenCallbacksRunInReverse(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	salt := bytes.Repeat([]byte{0x23}, sha256.Size)
	genesis, _ := Genesis(workspaceID, salt)
	setup, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err := InitializeChain(ctx, setup, ChainHeader{WorkspaceID: workspaceID, Generation: 1,
		ChainSalt: salt, GenesisHash: genesis, CurrentHead: genesis,
		CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	if err := setup.Commit(); err != nil {
		t.Fatal(err)
	}

	initial := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1, Head: genesis[:]}
	store := &memoryMirrorStore{baseline: proto.Clone(initial).(*tammyv1.AuditMirrorBaseline)}
	gate := NewWriteGate()
	gate.set(true, true)
	appender, err := NewMirroringAppender(store, gate)
	if err != nil {
		t.Fatal(err)
	}

	firstRaw, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	firstTx := &auditLifecycleTransaction{Transaction: firstRaw}
	firstEvent, firstPayload := integrationAuditEvent("01890f60-4d6d-7c12-8f02-6c9129d5b008")
	first, err := appender.Append(ctx, firstTx, firstEvent, firstPayload)
	if err != nil {
		t.Fatal(err)
	}
	if err := firstTx.Transaction.Commit(); err != nil {
		t.Fatal(err)
	}

	secondRaw, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	secondTx := &auditLifecycleTransaction{Transaction: secondRaw}
	secondEvent, secondPayload := integrationAuditEvent("01890f60-4d6d-7c12-8f02-6c9129d5b009")
	second, err := appender.Append(ctx, secondTx, secondEvent, secondPayload)
	if err != nil {
		t.Fatal(err)
	}
	if err := secondTx.Transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	var eventCount int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM audit_events_v1`).Scan(&eventCount); err != nil || eventCount != 2 {
		t.Fatalf("durable events before publication=%d err=%v", eventCount, err)
	}

	if len(firstTx.afterCommit) != 1 || len(secondTx.afterCommit) != 1 {
		t.Fatalf("registered callbacks first=%d second=%d", len(firstTx.afterCommit), len(secondTx.afterCommit))
	}
	if err := secondTx.afterCommit[0](ctx); err != nil {
		t.Fatalf("later callback: %v", err)
	}
	if err := firstTx.afterCommit[0](ctx); err != nil {
		t.Fatalf("earlier callback after drain: %v", err)
	}
	if !sameBaseline(store.baseline, &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1,
		Sequence: second.Event.Sequence, Head: second.Event.EventHash}) || !gate.Writable() {
		t.Fatalf("published baseline=%v first=%x second=%x writable=%v", store.baseline,
			first.Event.EventHash, second.Event.EventHash, gate.Writable())
	}
}

func TestMirroringAppendersSharePublicationOrderThroughRuntimeGate(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	salt := bytes.Repeat([]byte{0x23}, sha256.Size)
	genesis, _ := Genesis(workspaceID, salt)
	setup, _ := database.BeginEncryptedTx(ctx, nil)
	if err := InitializeChain(ctx, setup, ChainHeader{WorkspaceID: workspaceID, Generation: 1,
		ChainSalt: salt, GenesisHash: genesis, CurrentHead: genesis,
		CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	if err := setup.Commit(); err != nil {
		t.Fatal(err)
	}

	store := &memoryMirrorStore{baseline: &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID,
		Generation: 1, Head: genesis[:]}}
	gate := NewWriteGate()
	gate.set(true, true)
	firstAppender, err := NewMirroringAppender(store, gate)
	if err != nil {
		t.Fatal(err)
	}
	secondAppender, err := NewMirroringAppender(store, gate)
	if err != nil {
		t.Fatal(err)
	}

	firstRaw, _ := database.BeginEncryptedTx(ctx, nil)
	firstTx := &auditLifecycleTransaction{Transaction: firstRaw}
	firstEvent, firstPayload := integrationAuditEvent("01890f60-4d6d-7c12-8f02-6c9129d5b008")
	if _, err := firstAppender.Append(ctx, firstTx, firstEvent, firstPayload); err != nil {
		t.Fatal(err)
	}
	if err := firstTx.Transaction.Commit(); err != nil {
		t.Fatal(err)
	}

	secondRaw, _ := database.BeginEncryptedTx(ctx, nil)
	secondTx := &auditLifecycleTransaction{Transaction: secondRaw}
	secondEvent, secondPayload := integrationAuditEvent("01890f60-4d6d-7c12-8f02-6c9129d5b009")
	second, err := secondAppender.Append(ctx, secondTx, secondEvent, secondPayload)
	if err != nil {
		t.Fatal(err)
	}
	if err := secondTx.Transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := secondTx.afterCommit[0](ctx); err != nil {
		t.Fatalf("later appender callback: %v", err)
	}
	if err := firstTx.afterCommit[0](ctx); err != nil {
		t.Fatalf("earlier appender callback: %v", err)
	}
	target := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1,
		Sequence: second.Event.Sequence, Head: second.Event.EventHash}
	if !sameBaseline(store.baseline, target) || !gate.Writable() {
		t.Fatalf("baseline=%v target=%v writable=%v", store.baseline, target, gate.Writable())
	}
}

func TestMirroringAppenderReplacesRolledBackReservationAtSameSequence(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	salt := bytes.Repeat([]byte{0x23}, sha256.Size)
	genesis, _ := Genesis(workspaceID, salt)
	setup, _ := database.BeginEncryptedTx(ctx, nil)
	if err := InitializeChain(ctx, setup, ChainHeader{WorkspaceID: workspaceID, Generation: 1,
		ChainSalt: salt, GenesisHash: genesis, CurrentHead: genesis,
		CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	if err := setup.Commit(); err != nil {
		t.Fatal(err)
	}

	initial := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1, Head: genesis[:]}
	store := &memoryMirrorStore{baseline: proto.Clone(initial).(*tammyv1.AuditMirrorBaseline)}
	gate := NewWriteGate()
	gate.set(true, true)
	appender, err := NewMirroringAppender(store, gate)
	if err != nil {
		t.Fatal(err)
	}

	rolledBackRaw, _ := database.BeginEncryptedTx(ctx, nil)
	rolledBackTx := &auditLifecycleTransaction{Transaction: rolledBackRaw}
	rolledBackEvent, rolledBackPayload := integrationAuditEvent("01890f60-4d6d-7c12-8f02-6c9129d5b008")
	if _, err := appender.Append(ctx, rolledBackTx, rolledBackEvent, rolledBackPayload); err != nil {
		t.Fatal(err)
	}
	if err := rolledBackTx.Transaction.Rollback(); err != nil {
		t.Fatal(err)
	}

	committedRaw, _ := database.BeginEncryptedTx(ctx, nil)
	committedTx := &auditLifecycleTransaction{Transaction: committedRaw}
	committedEvent, committedPayload := integrationAuditEvent("01890f60-4d6d-7c12-8f02-6c9129d5b009")
	committed, err := appender.Append(ctx, committedTx, committedEvent, committedPayload)
	if err != nil {
		t.Fatal(err)
	}
	if err := committedTx.Transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := committedTx.afterCommit[0](ctx); err != nil {
		t.Fatalf("replacement callback: %v", err)
	}
	if err := rolledBackTx.afterCommit[0](ctx); err != nil {
		t.Fatalf("late rolled-back callback: %v", err)
	}
	target := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1,
		Sequence: committed.Event.Sequence, Head: committed.Event.EventHash}
	if !sameBaseline(store.baseline, target) || !gate.Writable() {
		t.Fatalf("baseline=%v target=%v writable=%v", store.baseline, target, gate.Writable())
	}
}

func TestMirroringAppenderReverseCallbackDrainRejectsDivergedMirror(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	salt := bytes.Repeat([]byte{0x23}, sha256.Size)
	genesis, _ := Genesis(workspaceID, salt)
	setup, _ := database.BeginEncryptedTx(ctx, nil)
	if err := InitializeChain(ctx, setup, ChainHeader{WorkspaceID: workspaceID, Generation: 1,
		ChainSalt: salt, GenesisHash: genesis, CurrentHead: genesis,
		CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	if err := setup.Commit(); err != nil {
		t.Fatal(err)
	}

	store := &memoryMirrorStore{baseline: &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID,
		Generation: 1, Head: genesis[:]}}
	gate := NewWriteGate()
	gate.set(true, true)
	appender, err := NewMirroringAppender(store, gate)
	if err != nil {
		t.Fatal(err)
	}
	var transactions []*auditLifecycleTransaction
	for index, id := range []string{
		"01890f60-4d6d-7c12-8f02-6c9129d5b008",
		"01890f60-4d6d-7c12-8f02-6c9129d5b009",
	} {
		raw, _ := database.BeginEncryptedTx(ctx, nil)
		transaction := &auditLifecycleTransaction{Transaction: raw}
		event, payload := integrationAuditEvent(id)
		event.OccurredAt = timestamppb.New(event.OccurredAt.AsTime().Add(time.Duration(index) * time.Nanosecond))
		if _, err := appender.Append(ctx, transaction, event, payload); err != nil {
			t.Fatal(err)
		}
		if err := transaction.Transaction.Commit(); err != nil {
			t.Fatal(err)
		}
		transactions = append(transactions, transaction)
	}
	tampered := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1,
		Head: bytes.Repeat([]byte{0xa7}, sha256.Size)}
	store.baseline = proto.Clone(tampered).(*tammyv1.AuditMirrorBaseline)
	if err := transactions[1].afterCommit[0](ctx); !errors.Is(err, ErrRollbackDetected) {
		t.Fatalf("reverse callback error=%v, want ErrRollbackDetected", err)
	}
	if !sameBaseline(store.baseline, tampered) || store.saves != 0 || gate.Writable() || !gate.EvidenceExportAllowed() {
		t.Fatalf("baseline=%v saves=%d writable=%v evidence=%v", store.baseline, store.saves,
			gate.Writable(), gate.EvidenceExportAllowed())
	}
}

func TestMirroringAppenderRejectsOrdinaryAppendWhileWriteGateIsLockedWithoutPartialSQL(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	salt := bytes.Repeat([]byte{0x23}, sha256.Size)
	genesis, _ := Genesis(workspaceID, salt)
	setup, _ := database.BeginEncryptedTx(ctx, nil)
	if err := InitializeChain(ctx, setup, ChainHeader{WorkspaceID: workspaceID, Generation: 1,
		ChainSalt: salt, GenesisHash: genesis, CurrentHead: genesis, CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	if err := setup.Commit(); err != nil {
		t.Fatal(err)
	}
	gate := NewWriteGate()
	appender, err := NewMirroringAppender(&memoryMirrorStore{}, gate)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := database.BeginEncryptedTx(ctx, nil)
	transaction := &auditLifecycleTransaction{Transaction: raw}
	event, payload := integrationAuditEvent("01890f60-4d6d-7c12-8f02-6c9129d5b008")
	if _, err := appender.Append(ctx, transaction, event, payload); !errors.Is(err, ErrWriteGate) {
		t.Fatalf("locked append error=%v, want ErrWriteGate", err)
	}
	if err := transaction.CommitAndPublish(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM audit_events_v1`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("locked append left events=%d err=%v", count, err)
	}
}

func TestMirroringAppenderFailureLocksWritesAfterDurableCommit(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	salt := bytes.Repeat([]byte{0x23}, sha256.Size)
	genesis, _ := Genesis(workspaceID, salt)
	setup, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err := InitializeChain(ctx, setup, ChainHeader{WorkspaceID: workspaceID, Generation: 1,
		ChainSalt: salt, GenesisHash: genesis, CurrentHead: genesis,
		CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	if err := setup.Commit(); err != nil {
		t.Fatal(err)
	}

	gate := NewWriteGate()
	gate.set(true, true)
	appender, err := NewMirroringAppender(&failingMirrorStore{}, gate)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	transaction := &auditLifecycleTransaction{Transaction: raw}
	event, payload := integrationAuditEvent("01890f60-4d6d-7c12-8f02-6c9129d5b008")
	if _, err := appender.Append(ctx, transaction, event, payload); err != nil {
		t.Fatal(err)
	}
	if err := transaction.CommitAndPublish(ctx); err == nil {
		t.Fatal("mirror failure unexpectedly succeeded")
	}
	if gate.Writable() || !gate.EvidenceExportAllowed() {
		t.Fatalf("gate writable=%v evidence=%v", gate.Writable(), gate.EvidenceExportAllowed())
	}
	var count int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM audit_events_v1`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("durable events=%d err=%v", count, err)
	}
}

func TestMirroringAppenderCrashAfterSQLCommitRepairsOnlyAfterFullVerification(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	salt := bytes.Repeat([]byte{0x23}, sha256.Size)
	genesis, _ := Genesis(workspaceID, salt)
	setup, _ := database.BeginEncryptedTx(ctx, nil)
	if err := InitializeChain(ctx, setup, ChainHeader{WorkspaceID: workspaceID, Generation: 1,
		ChainSalt: salt, GenesisHash: genesis, CurrentHead: genesis, CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	if err := setup.Commit(); err != nil {
		t.Fatal(err)
	}
	store := &memoryMirrorStore{baseline: &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1, Head: genesis[:]}}
	gate := NewWriteGate()
	gate.set(true, true)
	appender, _ := NewMirroringAppender(store, gate)
	raw, _ := database.BeginEncryptedTx(ctx, nil)
	transaction := &auditLifecycleTransaction{Transaction: raw}
	event, payload := integrationAuditEvent("01890f60-4d6d-7c12-8f02-6c9129d5b008")
	stored, err := appender.Append(ctx, transaction, event, payload)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate process death after the caller commits SQL but before it drains
	// registered post-commit callbacks.
	if err := transaction.Transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	if store.saves != 0 || store.baseline.Sequence != 0 {
		t.Fatal("crash simulation unexpectedly published mirror")
	}
	databaseBaseline := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1, Sequence: 1,
		Head: append([]byte(nil), stored.Event.EventHash...)}
	verifier := &fixedMirrorVerifier{verification: VerifiedChain{Baseline: databaseBaseline,
		Heads: map[uint64][]byte{0: genesis[:], 1: stored.Event.EventHash}, Valid: true}}
	decision, err := NewMirrorReconciler(store, verifier, gate).Open(ctx, databaseBaseline)
	if err != nil || decision != MirrorDecisionRepaired || verifier.calls != 1 || store.saves != 1 || !gate.Writable() {
		t.Fatalf("crash repair decision=%v verifier=%d saves=%d writable=%v err=%v", decision, verifier.calls, store.saves, gate.Writable(), err)
	}
}

func TestRepositoryReloadsExactBinaryAndCanonicalBytes(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	transaction, err := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	salt := bytes.Repeat([]byte{0x23}, sha256.Size)
	genesis, _ := Genesis("01890f60-4d6d-7c12-8f02-6c9129d5b001", salt)
	if err := InitializeChain(ctx, transaction, ChainHeader{
		WorkspaceID: "01890f60-4d6d-7c12-8f02-6c9129d5b001", Generation: 1,
		ChainSalt: salt, GenesisHash: genesis, CurrentHead: genesis,
		CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	event, payload := integrationAuditEvent("01890f60-4d6d-7c12-8f02-6c9129d5b008")
	written, err := appendStoredEventForTest(ctx, transaction, event, payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadStoredEvents(ctx, database, event.WorkspaceId, 1, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || !bytes.Equal(loaded[0].PayloadProto, written.PayloadProto) ||
		!bytes.Equal(loaded[0].PayloadJSON, written.PayloadJSON) ||
		!bytes.Equal(loaded[0].CanonicalEvent, written.CanonicalEvent) ||
		!bytes.Equal(loaded[0].EventProto, written.EventProto) || len(loaded[0].AffectedResourcesProto) == 0 ||
		!bytes.Equal(loaded[0].AffectedResourcesProto, written.AffectedResourcesProto) {
		t.Fatalf("stored bytes changed: %#v", loaded)
	}
}
