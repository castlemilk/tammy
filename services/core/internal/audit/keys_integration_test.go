//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package audit

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func seedActiveSigningKey(t *testing.T, ctx context.Context, executor Executor, workspaceID string) SigningKeyRecord {
	return seedSigningKeyAt(t, ctx, executor, workspaceID, time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC))
}

func seedSigningKeyAt(t *testing.T, ctx context.Context, executor Executor, workspaceID string, createdAt time.Time) SigningKeyRecord {
	t.Helper()
	record, _, err := GenerateSigningKey(workspaceID, bytes.Repeat([]byte{0x41}, 32),
		createdAt, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := PersistSigningKey(ctx, executor, record); err != nil {
		t.Fatal(err)
	}
	if err := InitializeSigningKeyState(ctx, executor, record); err != nil {
		t.Fatal(err)
	}
	return record
}

func TestSigningKeysRejectDirectSQLTampering(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		statement string
	}{
		{name: "key identity", statement: `UPDATE audit_signing_keys_v1 SET key_id=key_id || '-tampered'`},
		{name: "workspace identity", statement: `UPDATE audit_signing_keys_v1 SET workspace_id='workspace-tampered'`},
		{name: "public key", statement: `UPDATE audit_signing_keys_v1 SET public_key=randomblob(32)`},
		{name: "encrypted private key", statement: `UPDATE audit_signing_keys_v1 SET encrypted_private_key=randomblob(65)`},
		{name: "nonce", statement: `UPDATE audit_signing_keys_v1 SET nonce=randomblob(12)`},
		{name: "lineage", statement: `UPDATE audit_signing_keys_v1 SET predecessor_key_id=key_id, predecessor_signature=zeroblob(64)`},
		{name: "creation time", statement: `UPDATE audit_signing_keys_v1 SET created_at='2026-08-03T01:00:00.000000000Z'`},
		{name: "delete", statement: `DELETE FROM audit_signing_keys_v1`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			database := newEncryptedAuditDatabase(t)
			seedActiveSigningKey(t, ctx, database, "01890f60-4d6d-7c12-8f02-6c9129d5b001")
			if _, err := database.ExecContext(ctx, testCase.statement); err == nil {
				t.Fatal("direct SQL signing-key tampering succeeded")
			}
		})
	}
}

func TestSigningKeyRetirementRejectsIllegalTransitions(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		prepare   string
		statement string
		createdAt time.Time
	}{
		{name: "malformed time", statement: `UPDATE audit_signing_keys_v1 SET retired_at='not-a-time'`},
		{name: "impossible calendar date", statement: `UPDATE audit_signing_keys_v1 SET retired_at='2026-02-31T01:00:00.000000000Z'`,
			createdAt: time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)},
		{name: "hour twenty four", statement: `UPDATE audit_signing_keys_v1 SET retired_at='2026-08-05T24:00:00.000000000Z'`},
		{name: "time not after creation", statement: `UPDATE audit_signing_keys_v1 SET retired_at=created_at`},
		{name: "unretire", prepare: `UPDATE audit_signing_keys_v1 SET retired_at='2026-08-05T01:00:00.000000000Z'`,
			statement: `UPDATE audit_signing_keys_v1 SET retired_at=NULL`},
		{name: "second retirement", prepare: `UPDATE audit_signing_keys_v1 SET retired_at='2026-08-05T01:00:00.000000000Z'`,
			statement: `UPDATE audit_signing_keys_v1 SET retired_at='2026-08-06T01:00:00.000000000Z'`},
		{name: "retirement with material change",
			statement: `UPDATE audit_signing_keys_v1 SET retired_at='2026-08-05T01:00:00.000000000Z', public_key=randomblob(32)`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			database := newEncryptedAuditDatabase(t)
			createdAt := testCase.createdAt
			if createdAt.IsZero() {
				createdAt = time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)
			}
			seedSigningKeyAt(t, ctx, database, "01890f60-4d6d-7c12-8f02-6c9129d5b001", createdAt)
			if testCase.prepare != "" {
				if _, err := database.ExecContext(ctx, testCase.prepare); err != nil {
					t.Fatalf("prepare valid retirement: %v", err)
				}
			}
			if _, err := database.ExecContext(ctx, testCase.statement); err == nil {
				t.Fatal("illegal signing-key retirement transition succeeded")
			}
		})
	}
}

func TestSigningKeysRejectInsertOrReplace(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		sameKeyID bool
	}{
		{name: "existing key id", sameKeyID: true},
		{name: "different active key"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			database := newEncryptedAuditDatabase(t)
			workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
			current := seedActiveSigningKey(t, ctx, database, workspaceID)
			replacement, _, err := GenerateSigningKey(workspaceID, bytes.Repeat([]byte{0x41}, 32),
				time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC), rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			if testCase.sameKeyID {
				replacement.KeyID = current.KeyID
			}
			_, err = database.ExecContext(ctx, `INSERT OR REPLACE INTO audit_signing_keys_v1(
				key_id, workspace_id, generation, epoch, public_key, encrypted_private_key, nonce,
				encryption_algorithm, signing_algorithm, predecessor_key_id, predecessor_signature,
				successor_possession_signature, rotation_prior_sequence, rotation_prior_head, created_at, retired_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, NULL, NULL, NULL, ?, NULL)`,
				replacement.KeyID, replacement.WorkspaceID, replacement.Generation, replacement.Epoch,
				replacement.PublicKey, replacement.EncryptedPrivateKey, replacement.Nonce,
				replacement.EncryptionAlgorithm, replacement.SigningAlgorithm, formatTimestamp(replacement.CreatedAt))
			if err == nil || !strings.Contains(err.Error(), "audit signing keys cannot be replaced") {
				t.Fatalf("INSERT OR REPLACE error=%v, want conflict trigger", err)
			}
		})
	}
}

func TestSigningKeysRejectInvalidInsertTimestamps(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		createdAt string
		retiredAt any
	}{
		{name: "malformed creation time", createdAt: "not-a-time"},
		{name: "out of range creation year", createdAt: "10000-01-01T00:00:00.000000000Z"},
		{name: "impossible retirement date", createdAt: "2026-01-01T00:00:00.000000000Z",
			retiredAt: "2026-02-31T01:00:00.000000000Z"},
		{name: "retirement before creation", createdAt: "2026-08-04T01:00:00.000000000Z",
			retiredAt: "2026-08-03T01:00:00.000000000Z"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			database := newEncryptedAuditDatabase(t)
			record, _, err := GenerateSigningKey(
				"01890f60-4d6d-7c12-8f02-6c9129d5b001", bytes.Repeat([]byte{0x41}, 32),
				time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), rand.Reader,
			)
			if err != nil {
				t.Fatal(err)
			}
			_, err = database.ExecContext(ctx, `INSERT INTO audit_signing_keys_v1(
				key_id, workspace_id, generation, epoch, public_key, encrypted_private_key, nonce,
				encryption_algorithm, signing_algorithm, predecessor_key_id, predecessor_signature,
				successor_possession_signature, rotation_prior_sequence, rotation_prior_head, created_at, retired_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, NULL, NULL, NULL, ?, ?)`,
				record.KeyID, record.WorkspaceID, record.Generation, record.Epoch, record.PublicKey,
				record.EncryptedPrivateKey, record.Nonce, record.EncryptionAlgorithm, record.SigningAlgorithm,
				testCase.createdAt, testCase.retiredAt)
			if err == nil || !strings.Contains(err.Error(), "CHECK constraint failed") {
				t.Fatalf("invalid insertion timestamp error=%v, want timestamp CHECK constraint", err)
			}
		})
	}
}

func TestSigningKeySQLRejectsNonCanonicalCiphertextLength(t *testing.T) {
	for _, ciphertextLength := range []int{79, 81} {
		t.Run(fmt.Sprintf("length_%d", ciphertextLength), func(t *testing.T) {
			ctx := context.Background()
			database := newEncryptedAuditDatabase(t)
			record, _, err := GenerateSigningKey(
				"01890f60-4d6d-7c12-8f02-6c9129d5b001", bytes.Repeat([]byte{0x41}, 32),
				time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC), rand.Reader,
			)
			if err != nil {
				t.Fatal(err)
			}
			_, err = database.ExecContext(ctx, `INSERT INTO audit_signing_keys_v1(
				key_id, workspace_id, generation, epoch, public_key, encrypted_private_key, nonce,
				encryption_algorithm, signing_algorithm, predecessor_key_id, predecessor_signature,
				successor_possession_signature, rotation_prior_sequence, rotation_prior_head, created_at, retired_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, NULL, NULL, NULL, ?, NULL)`,
				record.KeyID, record.WorkspaceID, record.Generation, record.Epoch, record.PublicKey,
				bytes.Repeat([]byte{0x91}, ciphertextLength), record.Nonce, record.EncryptionAlgorithm,
				record.SigningAlgorithm, formatTimestamp(record.CreatedAt))
			if err == nil || !strings.Contains(err.Error(), "CHECK constraint failed") {
				t.Fatalf("%d-byte ciphertext error=%v, want CHECK constraint", ciphertextLength, err)
			}
		})
	}
}

func TestLoadSigningKeyRejectsPersistedOverlongCiphertext(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	record, _, err := GenerateSigningKey(
		"01890f60-4d6d-7c12-8f02-6c9129d5b001", bytes.Repeat([]byte{0x41}, 32),
		time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC), rand.Reader,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `PRAGMA ignore_check_constraints=ON`); err != nil {
		t.Fatal(err)
	}
	_, err = database.ExecContext(ctx, `INSERT INTO audit_signing_keys_v1(
		key_id, workspace_id, generation, epoch, public_key, encrypted_private_key, nonce,
		encryption_algorithm, signing_algorithm, predecessor_key_id, predecessor_signature,
		successor_possession_signature, rotation_prior_sequence, rotation_prior_head, created_at, retired_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, NULL, NULL, NULL, ?, NULL)`,
		record.KeyID, record.WorkspaceID, record.Generation, record.Epoch, record.PublicKey,
		append(record.EncryptedPrivateKey, 0xff), record.Nonce, record.EncryptionAlgorithm,
		record.SigningAlgorithm, formatTimestamp(record.CreatedAt))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `PRAGMA ignore_check_constraints=OFF`); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSigningKey(ctx, database, record.WorkspaceID, record.KeyID); !errors.Is(err, ErrSigningKey) {
		t.Fatalf("LoadSigningKey overlong persisted ciphertext error=%v, want ErrSigningKey", err)
	}
}

func TestSigningKeyReferencesCannotCrossWorkspaces(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		wantMessage string
		try         func(context.Context, *sqlcipher.Database, SigningKeyRecord) error
	}{
		{
			name:        "predecessor",
			wantMessage: "audit signing keys cannot be replaced",
			try: func(ctx context.Context, database *sqlcipher.Database, predecessor SigningKeyRecord) error {
				const successorWorkspaceID = "01890f60-4d6d-7c12-8f02-6c9129d5b002"
				dek := bytes.Repeat([]byte{0x41}, 32)
				root := seedSigningKeyAt(t, ctx, database, successorWorkspaceID,
					time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC))
				rotatedAt := time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC)
				priorHead := [32]byte{1}
				_, successor, _, err := createSigningKeySuccessor(root, dek, root.Generation, 0, priorHead, rotatedAt, rand.Reader)
				if err != nil {
					return err
				}
				if _, err := database.ExecContext(ctx, `UPDATE audit_signing_keys_v1 SET retired_at=?
					WHERE workspace_id=? AND key_id=?`, formatTimestamp(rotatedAt), root.WorkspaceID, root.KeyID); err != nil {
					return err
				}
				successor.PreviousKeyID = predecessor.KeyID
				_, err = database.ExecContext(ctx, `INSERT INTO audit_signing_keys_v1(
					key_id, workspace_id, generation, epoch, public_key, encrypted_private_key, nonce,
					encryption_algorithm, signing_algorithm, predecessor_key_id, predecessor_signature,
					successor_possession_signature, rotation_prior_sequence, rotation_prior_head, created_at, retired_at
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)`,
					successor.KeyID, successor.WorkspaceID, successor.Generation, successor.Epoch,
					successor.PublicKey, successor.EncryptedPrivateKey, successor.Nonce, successor.EncryptionAlgorithm,
					successor.SigningAlgorithm, successor.PreviousKeyID, successor.PreviousSignature,
					successor.PossessionSignature, successor.RotationSequence, successor.RotationPriorHead,
					formatTimestamp(successor.CreatedAt))
				return err
			},
		},
		{
			name:        "export signing key",
			wantMessage: "FOREIGN KEY constraint failed",
			try: func(ctx context.Context, database *sqlcipher.Database, signingKey SigningKeyRecord) error {
				_, err := database.ExecContext(ctx, `INSERT INTO audit_export_jobs_v1(
					id, workspace_id, operation_key, operation_hash, input_hash, filter_proto,
					snapshot_generation, snapshot_sequence, snapshot_head, destination_provider, evidence_provider,
					destination_capability, state, stage, progress_proto, signing_key_id, created_at, updated_at
				) VALUES ('cross-workspace-export', '01890f60-4d6d-7c12-8f02-6c9129d5b002', 'operation-key',
					zeroblob(32), zeroblob(32), x'', 1, 0, zeroblob(32), 'approved_file', 'audit_chain',
					'approved-file-capability-v1', 'QUEUED', 'QUEUED', x'', ?,
					'2026-08-05T01:00:00.000000000Z', '2026-08-05T01:00:00.000000000Z')`, signingKey.KeyID)
				return err
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			database := newEncryptedAuditDatabase(t)
			key := seedActiveSigningKey(t, ctx, database, "01890f60-4d6d-7c12-8f02-6c9129d5b001")
			err := testCase.try(ctx, database, key)
			if err == nil || !strings.Contains(err.Error(), testCase.wantMessage) {
				t.Fatalf("cross-workspace signing-key reference error=%v, want %q", err, testCase.wantMessage)
			}
		})
	}
}

func TestSigningKeysPermitOnlyOneActiveKeyPerWorkspace(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	seedActiveSigningKey(t, ctx, database, workspaceID)
	second, _, err := GenerateSigningKey(workspaceID, bytes.Repeat([]byte{0x41}, 32),
		time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := PersistSigningKey(ctx, database, second); err == nil {
		t.Fatal("second active signing key succeeded")
	}
}

func TestSigningKeySQLRejectsDuplicateRootEpochForkAndWrongRetirementLink(t *testing.T) {
	for _, testCase := range []struct {
		name string
		run  func(*testing.T, context.Context, *sqlcipher.Database, SigningKeyRecord, []byte, [32]byte)
	}{
		{
			name: "second root after retirement",
			run: func(t *testing.T, ctx context.Context, database *sqlcipher.Database,
				root SigningKeyRecord, dek []byte, _ [32]byte,
			) {
				retiredAt := time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC)
				if _, err := database.ExecContext(ctx, `UPDATE audit_signing_keys_v1 SET retired_at=?
					WHERE workspace_id=? AND key_id=?`, formatTimestamp(retiredAt), root.WorkspaceID, root.KeyID); err != nil {
					t.Fatal(err)
				}
				secondRoot, _, err := GenerateSigningKey(root.WorkspaceID, dek, retiredAt, rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
				if err := PersistSigningKey(ctx, database, secondRoot); err == nil {
					t.Fatal("SQL accepted a second epoch-one root")
				}
			},
		},
		{
			name: "duplicate epoch fork",
			run: func(t *testing.T, ctx context.Context, database *sqlcipher.Database,
				root SigningKeyRecord, dek []byte, head [32]byte,
			) {
				rotatedAt := time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC)
				_, first, _, err := createSigningKeySuccessor(root, dek, 1, 0, head, rotatedAt, rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
				_, fork, _, err := createSigningKeySuccessor(root, dek, 1, 0, head, rotatedAt, rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := database.ExecContext(ctx, `UPDATE audit_signing_keys_v1 SET retired_at=?
					WHERE workspace_id=? AND key_id=?`, formatTimestamp(rotatedAt), root.WorkspaceID, root.KeyID); err != nil {
					t.Fatal(err)
				}
				if err := PersistSigningKey(ctx, database, first); err != nil {
					t.Fatal(err)
				}
				if err := PersistSigningKey(ctx, database, fork); err == nil {
					t.Fatal("SQL accepted a second successor at the same epoch")
				}
			},
		},
		{
			name: "skipped predecessor epoch",
			run: func(t *testing.T, ctx context.Context, database *sqlcipher.Database,
				root SigningKeyRecord, dek []byte, head [32]byte,
			) {
				rotatedAt := time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC)
				_, successor, _, err := createSigningKeySuccessor(root, dek, 1, 0, head, rotatedAt, rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
				successor.Epoch = 3
				if _, err := database.ExecContext(ctx, `UPDATE audit_signing_keys_v1 SET retired_at=?
					WHERE workspace_id=? AND key_id=?`, formatTimestamp(rotatedAt), root.WorkspaceID, root.KeyID); err != nil {
					t.Fatal(err)
				}
				if err := PersistSigningKey(ctx, database, successor); err == nil {
					t.Fatal("SQL accepted a successor that skipped an epoch")
				}
			},
		},
		{
			name: "retirement creation mismatch",
			run: func(t *testing.T, ctx context.Context, database *sqlcipher.Database,
				root SigningKeyRecord, dek []byte, head [32]byte,
			) {
				createdAt := time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC)
				_, successor, _, err := createSigningKeySuccessor(root, dek, 1, 0, head, createdAt, rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := database.ExecContext(ctx, `UPDATE audit_signing_keys_v1 SET retired_at=?
					WHERE workspace_id=? AND key_id=?`, formatTimestamp(createdAt.Add(time.Second)), root.WorkspaceID, root.KeyID); err != nil {
					t.Fatal(err)
				}
				if err := PersistSigningKey(ctx, database, successor); err == nil {
					t.Fatal("SQL accepted a successor whose creation did not equal predecessor retirement")
				}
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			database := newEncryptedAuditDatabase(t)
			workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
			dek := bytes.Repeat([]byte{0x41}, 32)
			header, root, _, _ := seedSigningKeyRotationFixture(t, ctx, database, workspaceID, dek)
			testCase.run(t, ctx, database, root, dek, header.CurrentHead)
		})
	}
}

func TestSigningKeyStateSQLRejectsWrongRotationEventLinkage(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		priorHead  []byte
		eventKind  string
		eventDelta time.Duration
		extraEvent bool
	}{
		{name: "missing event"},
		{name: "wrong prior head", priorHead: bytes.Repeat([]byte{0x88}, 32), eventKind: "rotation"},
		{name: "wrong event type", eventKind: "ordinary"},
		{name: "wrong event timestamp", eventKind: "rotation", eventDelta: time.Nanosecond},
		{name: "header advanced past rotation event", eventKind: "rotation", extraEvent: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			database := newEncryptedAuditDatabase(t)
			workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
			dek := bytes.Repeat([]byte{0x41}, 32)
			header, root, appender, _ := seedSigningKeyRotationFixture(t, ctx, database, workspaceID, dek)
			rotatedAt := time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC)
			_, successor, link, err := createSigningKeySuccessor(root, dek, 1, 0, header.CurrentHead,
				rotatedAt, rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			if len(testCase.priorHead) != 0 {
				successor.RotationPriorHead = append([]byte(nil), testCase.priorHead...)
			}
			transaction := beginSigningKeyRotationTransaction(t, ctx, database)
			if _, err := transaction.ExecContext(ctx, `UPDATE audit_signing_keys_v1 SET retired_at=?
				WHERE workspace_id=? AND key_id=?`, formatTimestamp(rotatedAt), workspaceID, root.KeyID); err != nil {
				t.Fatal(err)
			}
			if err := PersistSigningKey(ctx, transaction, successor); err != nil {
				t.Fatal(err)
			}
			if testCase.eventKind != "" {
				var event *tammyv1.AuditEvent
				var payloadProto []byte
				if testCase.eventKind == "ordinary" {
					event, payloadProto = integrationAuditEvent("01890f60-4d6d-7c12-8f02-6c9129d5b108")
					event.OccurredAt = timestamppb.New(rotatedAt)
				} else {
					linkDigest, digestErr := signedSigningKeyRotationLinkDigest(link)
					if digestErr != nil {
						t.Fatal(digestErr)
					}
					payload := &tammyv1.SigningKeyRotatedEvent{WorkspaceId: workspaceID, Generation: 1,
						SuccessorEpoch: 2, PredecessorKeyId: root.KeyID, SuccessorKeyId: successor.KeyID,
						RotationLinkSha256: linkDigest[:]}
					payloadProto, err = proto.MarshalOptions{Deterministic: true}.Marshal(payload)
					if err != nil {
						t.Fatal(err)
					}
					event = signingKeyRotationEventTemplate("01890f60-4d6d-7c12-8f02-6c9129d5b109", workspaceID)
					event.Type = tammyv1.AuditEventType_AUDIT_EVENT_TYPE_SIGNING_KEY_ROTATED
					event.OccurredAt = timestamppb.New(rotatedAt.Add(testCase.eventDelta))
					event.Payload = &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_SigningKeyRotated{
						SigningKeyRotated: payload,
					}}
				}
				if _, err := appender.Append(ctx, transaction, event, payloadProto); err != nil {
					t.Fatal(err)
				}
			}
			if testCase.extraEvent {
				event, payload := integrationAuditEvent("01890f60-4d6d-7c12-8f02-6c9129d5b110")
				if _, err := appender.Append(ctx, transaction, event, payload); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := transaction.ExecContext(ctx, `UPDATE audit_signing_key_state_v1
				SET active_key_id=?, active_epoch=2
				WHERE workspace_id=? AND root_key_id=? AND active_key_id=? AND active_epoch=1`,
				successor.KeyID, workspaceID, root.KeyID, root.KeyID); err == nil {
				t.Fatal("SQL advanced active signing state through invalid event linkage")
			}
			if err := transaction.Rollback(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSigningKeyRepositoryUsesCallerOwnedEncryptedTransaction(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	dek := bytes.Repeat([]byte{0x41}, 32)
	record, _, err := GenerateSigningKey("01890f60-4d6d-7c12-8f02-6c9129d5b001", dek,
		time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	transaction, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err := PersistSigningKey(ctx, transaction, record); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Rollback(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM audit_signing_keys_v1`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("signing key repository committed independently: %d", count)
	}
	commitTransaction, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err := PersistSigningKey(ctx, commitTransaction, record); err != nil {
		t.Fatal(err)
	}
	if err := commitTransaction.Commit(); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSigningKey(ctx, database, record.WorkspaceID, record.KeyID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(loaded.EncryptedPrivateKey, record.EncryptedPrivateKey) || !bytes.Equal(loaded.PublicKey, record.PublicKey) {
		t.Fatal("signing key bytes changed in encrypted persistence")
	}
}

func TestSigningKeyRotationAtomicallyRetiresInsertsAppendsAndAdvancesState(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	dek := bytes.Repeat([]byte{0x41}, 32)
	header, root, appender, mirror := seedSigningKeyRotationFixture(t, ctx, database, workspaceID, dek)
	transaction := beginSigningKeyRotationTransaction(t, ctx, database)
	rotatedAt := time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC)

	result, err := appender.RotateSigningKey(ctx, transaction, SigningKeyRotationInput{
		ExpectedHeader: header,
		ExpectedState: SigningKeyState{WorkspaceID: workspaceID, RootKeyID: root.KeyID,
			ActiveKeyID: root.KeyID, ActiveEpoch: 1},
		DEK: dek, RotatedAt: rotatedAt, Random: rand.Reader,
		Event: signingKeyRotationEventTemplate("01890f60-4d6d-7c12-8f02-6c9129d5b101", workspaceID),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Retired.KeyID != root.KeyID || result.Retired.RetiredAt == nil ||
		!result.Retired.RetiredAt.Equal(rotatedAt) || result.Successor.Epoch != 2 ||
		result.Successor.PreviousKeyID != root.KeyID || !verifySigningKeyRotationLink(result.Link) ||
		result.StoredEvent.Event.Type != tammyv1.AuditEventType_AUDIT_EVENT_TYPE_SIGNING_KEY_ROTATED {
		t.Fatalf("rotation result=%#v", result)
	}
	if _, ok := rotationEventMatchesLink(result.Link, result.StoredEvent.EventProto, result.StoredEvent.PayloadProto); !ok {
		t.Fatal("persisted rotation event does not authenticate the exact signed link")
	}
	stagedState, err := LoadSigningKeyState(ctx, transaction, workspaceID)
	if err != nil || stagedState.RootKeyID != root.KeyID || stagedState.ActiveKeyID != result.Successor.KeyID ||
		stagedState.ActiveEpoch != 2 {
		t.Fatalf("staged state=%#v err=%v", stagedState, err)
	}
	stagedHeader, err := LoadChainHeader(ctx, transaction, workspaceID, header.Generation)
	if err != nil || stagedHeader.CurrentSequence != header.CurrentSequence+1 ||
		!bytes.Equal(stagedHeader.CurrentHead[:], result.StoredEvent.Event.EventHash) {
		t.Fatalf("staged header=%#v err=%v", stagedHeader, err)
	}
	if mirror.saves != 0 || mirror.baseline.Sequence != header.CurrentSequence {
		t.Fatal("rotation published the mirror before caller commit")
	}
	if err := transaction.CommitAndPublish(ctx); err != nil {
		t.Fatal(err)
	}
	if mirror.saves != 1 || mirror.baseline.Sequence != header.CurrentSequence+1 ||
		!bytes.Equal(mirror.baseline.Head, result.StoredEvent.Event.EventHash) {
		t.Fatalf("committed mirror=%#v saves=%d", mirror.baseline, mirror.saves)
	}
	history, active, err := loadSigningKeyHistoryAndActive(ctx, database, workspaceID)
	if err != nil || len(history) != 2 || active.KeyID != result.Successor.KeyID || history[0].RetiredAt == nil {
		t.Fatalf("history=%#v active=%#v err=%v", history, active, err)
	}
}

func TestSigningKeyRotationAppenderFailureRollsBackSavepointEvenWhenCallerCommits(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	dek := bytes.Repeat([]byte{0x41}, 32)
	_, root, appender, mirror := seedSigningKeyRotationFixture(t, ctx, database, workspaceID, dek)
	duplicateID := "01890f60-4d6d-7c12-8f02-6c9129d5b102"
	ordinary, ordinaryPayload := integrationAuditEvent(duplicateID)
	seedEvent := beginSigningKeyRotationTransaction(t, ctx, database)
	if _, err := appender.Append(ctx, seedEvent, ordinary, ordinaryPayload); err != nil {
		t.Fatal(err)
	}
	if err := seedEvent.CommitAndPublish(ctx); err != nil {
		t.Fatal(err)
	}
	header, err := LoadChainHeader(ctx, database, workspaceID, 1)
	if err != nil {
		t.Fatal(err)
	}

	transaction := beginSigningKeyRotationTransaction(t, ctx, database)
	_, err = appender.RotateSigningKey(ctx, transaction, SigningKeyRotationInput{
		ExpectedHeader: header,
		ExpectedState: SigningKeyState{WorkspaceID: workspaceID, RootKeyID: root.KeyID,
			ActiveKeyID: root.KeyID, ActiveEpoch: 1},
		DEK: dek, RotatedAt: time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC), Random: rand.Reader,
		Event: signingKeyRotationEventTemplate(duplicateID, workspaceID),
	})
	if err == nil {
		t.Fatal("rotation unexpectedly appended a duplicate event id")
	}
	if err := transaction.CommitAndPublish(ctx); err != nil {
		t.Fatal(err)
	}

	state, err := LoadSigningKeyState(ctx, database, workspaceID)
	if err != nil || state.RootKeyID != root.KeyID || state.ActiveKeyID != root.KeyID || state.ActiveEpoch != 1 {
		t.Fatalf("state after committed failure=%#v err=%v", state, err)
	}
	active, err := LoadActiveSigningKey(ctx, database, workspaceID)
	if err != nil || active.KeyID != root.KeyID || active.RetiredAt != nil {
		t.Fatalf("active after committed failure=%#v err=%v", active, err)
	}
	history, err := LoadSigningKeyHistory(ctx, database, workspaceID)
	if err != nil || len(history) != 1 {
		t.Fatalf("history after committed failure=%#v err=%v", history, err)
	}
	unchanged, err := LoadChainHeader(ctx, database, workspaceID, 1)
	if err != nil || unchanged.CurrentSequence != header.CurrentSequence || unchanged.CurrentHead != header.CurrentHead {
		t.Fatalf("header after committed failure=%#v err=%v", unchanged, err)
	}
	if mirror.saves != 1 || mirror.baseline.Sequence != header.CurrentSequence ||
		!bytes.Equal(mirror.baseline.Head, header.CurrentHead[:]) {
		t.Fatalf("mirror after committed failure=%#v saves=%d", mirror.baseline, mirror.saves)
	}
}

func TestSigningKeyRotationReleaseFailureDisarmsRegisteredMirrorPublication(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	dek := bytes.Repeat([]byte{0x41}, 32)
	header, root, appender, mirror := seedSigningKeyRotationFixture(t, ctx, database, workspaceID, dek)
	transaction := &failFirstSigningKeyRotationRelease{
		auditLifecycleTransaction: beginSigningKeyRotationTransaction(t, ctx, database),
	}
	_, err := appender.RotateSigningKey(ctx, transaction, SigningKeyRotationInput{
		ExpectedHeader: header,
		ExpectedState: SigningKeyState{WorkspaceID: workspaceID, RootKeyID: root.KeyID,
			ActiveKeyID: root.KeyID, ActiveEpoch: 1},
		DEK: dek, RotatedAt: time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC), Random: rand.Reader,
		Event: signingKeyRotationEventTemplate("01890f60-4d6d-7c12-8f02-6c9129d5b103", workspaceID),
	})
	if err == nil || transaction.releaseAttempts != 2 {
		t.Fatalf("release failure err=%v attempts=%d", err, transaction.releaseAttempts)
	}
	if err := transaction.CommitAndPublish(ctx); err != nil {
		t.Fatal(err)
	}
	state, stateErr := LoadSigningKeyState(ctx, database, workspaceID)
	active, activeErr := LoadActiveSigningKey(ctx, database, workspaceID)
	unchanged, headerErr := LoadChainHeader(ctx, database, workspaceID, 1)
	if stateErr != nil || activeErr != nil || headerErr != nil || state.ActiveKeyID != root.KeyID ||
		state.ActiveEpoch != 1 || active.KeyID != root.KeyID || active.RetiredAt != nil ||
		unchanged.CurrentSequence != 0 || unchanged.CurrentHead != header.CurrentHead {
		t.Fatalf("partial rotation state=%#v active=%#v header=%#v errors=%v/%v/%v",
			state, active, unchanged, stateErr, activeErr, headerErr)
	}
	if mirror.saves != 0 || mirror.baseline.Sequence != 0 || !bytes.Equal(mirror.baseline.Head, header.CurrentHead[:]) {
		t.Fatalf("rolled-back head was published=%#v saves=%d", mirror.baseline, mirror.saves)
	}
	if workspaces, edges := mirrorPublisherState(appender.publisher); workspaces != 0 || edges != 0 {
		t.Fatalf("disarmed publication leaked publisher state: workspaces=%d edges=%d", workspaces, edges)
	}
	subsequent := beginSigningKeyRotationTransaction(t, ctx, database)
	event, payload := integrationAuditEvent("01890f60-4d6d-7c12-8f02-6c9129d5b104")
	if _, err := appender.Append(ctx, subsequent, event, payload); err != nil {
		t.Fatal(err)
	}
	if err := subsequent.CommitAndPublish(ctx); err != nil {
		t.Fatal(err)
	}
	if mirror.saves != 1 || mirror.baseline.Sequence != 1 {
		t.Fatalf("valid append after disarmed rotation did not publish: baseline=%#v saves=%d", mirror.baseline, mirror.saves)
	}
}

func TestSigningKeyRotationRejectsNonLatestCallerHeaderBeforeConsumingEntropy(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	dek := bytes.Repeat([]byte{0x41}, 32)
	staleHeader, root, appender, mirror := seedSigningKeyRotationFixture(t, ctx, database, workspaceID, dek)
	newSalt := bytes.Repeat([]byte{0x24}, 32)
	newGenesis, err := Genesis(workspaceID, newSalt)
	if err != nil {
		t.Fatal(err)
	}
	latestHeader := ChainHeader{WorkspaceID: workspaceID, Generation: 2, ChainSalt: newSalt,
		GenesisHash: newGenesis, CurrentHead: newGenesis,
		CreatedAt: time.Date(2026, 8, 4, 2, 0, 0, 0, time.UTC)}
	setup, err := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	if err := InitializeChain(ctx, setup, latestHeader); err != nil {
		t.Fatal(err)
	}
	if err := setup.Commit(); err != nil {
		t.Fatal(err)
	}
	entropy := &countingReader{reader: bytes.NewReader(bytes.Repeat([]byte{0x73}, 128))}
	transaction := beginSigningKeyRotationTransaction(t, ctx, database)
	_, err = appender.RotateSigningKey(ctx, transaction, SigningKeyRotationInput{
		ExpectedHeader: staleHeader,
		ExpectedState: SigningKeyState{WorkspaceID: workspaceID, RootKeyID: root.KeyID,
			ActiveKeyID: root.KeyID, ActiveEpoch: 1},
		DEK: dek, RotatedAt: time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC), Random: entropy,
		Event: signingKeyRotationEventTemplate("01890f60-4d6d-7c12-8f02-6c9129d5b105", workspaceID),
	})
	if err == nil {
		t.Fatal("rotation accepted a non-latest caller header")
	}
	if entropy.reads != 0 {
		t.Fatalf("stale header consumed successor entropy: reads=%d", entropy.reads)
	}
	if err := transaction.CommitAndPublish(ctx); err != nil {
		t.Fatal(err)
	}
	state, stateErr := LoadSigningKeyState(ctx, database, workspaceID)
	history, historyErr := LoadSigningKeyHistory(ctx, database, workspaceID)
	loadedLatest, headerErr := LoadChainHeader(ctx, database, workspaceID, 0)
	if stateErr != nil || historyErr != nil || headerErr != nil || state.ActiveKeyID != root.KeyID ||
		state.ActiveEpoch != 1 || len(history) != 1 || loadedLatest.Generation != 2 || loadedLatest.CurrentSequence != 0 {
		t.Fatalf("stale-header partial state=%#v history=%#v latest=%#v errors=%v/%v/%v",
			state, history, loadedLatest, stateErr, historyErr, headerErr)
	}
	if mirror.saves != 0 {
		t.Fatalf("stale-header rejection published mirror: saves=%d", mirror.saves)
	}
}

func TestSigningKeyRotationRejectsPersistedLinkWhoseTypedEventDoesNotCommitIt(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	dek := bytes.Repeat([]byte{0x41}, 32)
	header, root, appender, mirror := seedSigningKeyRotationFixture(t, ctx, database, workspaceID, dek)
	forgedAt := time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC)
	retired, forged, link, err := createSigningKeySuccessor(root, dek, 1, 0, header.CurrentHead,
		forgedAt, rand.Reader)
	if err != nil || !verifySigningKeyRotationLink(link) || retired.RetiredAt == nil {
		t.Fatalf("create forged lineage fixture: retired=%#v link=%#v err=%v", retired, link, err)
	}
	seed := beginSigningKeyRotationTransaction(t, ctx, database)
	if _, err := seed.ExecContext(ctx, `UPDATE audit_signing_keys_v1 SET retired_at=?
		WHERE workspace_id=? AND key_id=? AND retired_at IS NULL`, formatTimestamp(forgedAt), workspaceID, root.KeyID); err != nil {
		t.Fatal(err)
	}
	if err := PersistSigningKey(ctx, seed, forged); err != nil {
		t.Fatal(err)
	}
	badPayload := &tammyv1.SigningKeyRotatedEvent{WorkspaceId: workspaceID, Generation: 1,
		SuccessorEpoch: 2, PredecessorKeyId: root.KeyID, SuccessorKeyId: forged.KeyID,
		RotationLinkSha256: bytes.Repeat([]byte{0x66}, 32)}
	badPayloadProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(badPayload)
	if err != nil {
		t.Fatal(err)
	}
	forgedEvent := signingKeyRotationEventTemplate("01890f60-4d6d-7c12-8f02-6c9129d5b106", workspaceID)
	forgedEvent.Type = tammyv1.AuditEventType_AUDIT_EVENT_TYPE_SIGNING_KEY_ROTATED
	forgedEvent.OccurredAt = timestamppb.New(forgedAt)
	forgedEvent.Payload = &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_SigningKeyRotated{
		SigningKeyRotated: badPayload,
	}}
	if _, err := appender.Append(ctx, seed, forgedEvent, badPayloadProto); err != nil {
		t.Fatal(err)
	}
	if _, err := seed.ExecContext(ctx, `UPDATE audit_signing_key_state_v1
		SET active_key_id=?, active_epoch=2
		WHERE workspace_id=? AND root_key_id=? AND active_key_id=? AND active_epoch=1`,
		forged.KeyID, workspaceID, root.KeyID, root.KeyID); err != nil {
		t.Fatal(err)
	}
	if err := seed.CommitAndPublish(ctx); err != nil {
		t.Fatal(err)
	}
	forgedHeader, err := LoadChainHeader(ctx, database, workspaceID, 0)
	if err != nil {
		t.Fatal(err)
	}
	entropy := &countingReader{reader: bytes.NewReader(bytes.Repeat([]byte{0x74}, 128))}
	transaction := beginSigningKeyRotationTransaction(t, ctx, database)
	_, err = appender.RotateSigningKey(ctx, transaction, SigningKeyRotationInput{
		ExpectedHeader: forgedHeader,
		ExpectedState: SigningKeyState{WorkspaceID: workspaceID, RootKeyID: root.KeyID,
			ActiveKeyID: forged.KeyID, ActiveEpoch: 2},
		DEK: dek, RotatedAt: time.Date(2026, 8, 6, 1, 0, 0, 0, time.UTC), Random: entropy,
		Event: signingKeyRotationEventTemplate("01890f60-4d6d-7c12-8f02-6c9129d5b107", workspaceID),
	})
	if err == nil {
		t.Fatal("rotation trusted a persisted lineage link not committed by its typed audit event")
	}
	if entropy.reads != 0 {
		t.Fatalf("forged persisted lineage consumed successor entropy: reads=%d", entropy.reads)
	}
	if err := transaction.CommitAndPublish(ctx); err != nil {
		t.Fatal(err)
	}
	unchanged, headerErr := LoadChainHeader(ctx, database, workspaceID, 0)
	state, stateErr := LoadSigningKeyState(ctx, database, workspaceID)
	if headerErr != nil || stateErr != nil || unchanged.CurrentSequence != forgedHeader.CurrentSequence ||
		unchanged.CurrentHead != forgedHeader.CurrentHead || state.ActiveKeyID != forged.KeyID || state.ActiveEpoch != 2 {
		t.Fatalf("forged-lineage rejection partial header=%#v state=%#v errors=%v/%v",
			unchanged, state, headerErr, stateErr)
	}
	if mirror.saves != 1 || mirror.baseline.Sequence != 1 {
		t.Fatalf("forged-lineage rejection published mirror=%#v saves=%d", mirror.baseline, mirror.saves)
	}
}

type failFirstSigningKeyRotationRelease struct {
	*auditLifecycleTransaction
	releaseAttempts int
}

type countingReader struct {
	reader *bytes.Reader
	reads  int
}

func (reader *countingReader) Read(buffer []byte) (int, error) {
	reader.reads++
	return reader.reader.Read(buffer)
}

func (transaction *failFirstSigningKeyRotationRelease) ExecContext(
	ctx context.Context, query string, arguments ...any,
) (sql.Result, error) {
	if strings.HasPrefix(strings.TrimSpace(query), "RELEASE SAVEPOINT ") {
		transaction.releaseAttempts++
		if transaction.releaseAttempts == 1 {
			return nil, errors.New("injected signing-key rotation release failure")
		}
	}
	return transaction.auditLifecycleTransaction.ExecContext(ctx, query, arguments...)
}

func seedSigningKeyRotationFixture(t *testing.T, ctx context.Context, database *sqlcipher.Database,
	workspaceID string, dek []byte,
) (ChainHeader, SigningKeyRecord, *Appender, *memoryMirrorStore) {
	t.Helper()
	salt := bytes.Repeat([]byte{0x23}, 32)
	genesis, err := Genesis(workspaceID, salt)
	if err != nil {
		t.Fatal(err)
	}
	header := ChainHeader{WorkspaceID: workspaceID, Generation: 1, ChainSalt: salt,
		GenesisHash: genesis, CurrentHead: genesis, CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)}
	root, _, err := GenerateSigningKey(workspaceID, dek, header.CreatedAt, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	setup, err := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	if err := InitializeChain(ctx, setup, header); err != nil {
		t.Fatal(err)
	}
	if err := PersistSigningKey(ctx, setup, root); err != nil {
		t.Fatal(err)
	}
	if err := InitializeSigningKeyState(ctx, setup, root); err != nil {
		t.Fatal(err)
	}
	if err := setup.Commit(); err != nil {
		t.Fatal(err)
	}
	initial := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: 1, Head: genesis[:]}
	mirror := &memoryMirrorStore{baseline: initial}
	gate := NewWriteGate()
	gate.set(true, true)
	appender, err := NewMirroringAppender(mirror, gate)
	if err != nil {
		t.Fatal(err)
	}
	return header, root, appender, mirror
}

func beginSigningKeyRotationTransaction(t *testing.T, ctx context.Context,
	database *sqlcipher.Database,
) *auditLifecycleTransaction {
	t.Helper()
	raw, err := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	return &auditLifecycleTransaction{Transaction: raw}
}

func signingKeyRotationEventTemplate(id, workspaceID string) *tammyv1.AuditEvent {
	event, _ := integrationAuditEvent(id)
	event.WorkspaceId = workspaceID
	event.Type = tammyv1.AuditEventType_AUDIT_EVENT_TYPE_UNSPECIFIED
	event.OccurredAt = nil
	event.Payload = nil
	event.Source = &tammyv1.SourceRef{Type: "audit_signing_key", Id: workspaceID, Revision: 1,
		ContentHash: bytes.Repeat([]byte{0x31}, 32)}
	event.AffectedResources = []*tammyv1.SourceRef{{Type: "audit_signing_key", Id: workspaceID, Revision: 1,
		ContentHash: bytes.Repeat([]byte{0x31}, 32)}}
	event.CommandType = "tammy.v1.AuditService.RotateSigningKey"
	event.Result.TypeName = "tammy.v1.SigningKeyRotatedEvent"
	event.Result.OutcomeCode = "OK"
	return event
}

func loadSigningKeyHistoryAndActive(ctx context.Context, executor Executor,
	workspaceID string,
) ([]SigningKeyRecord, SigningKeyRecord, error) {
	history, err := LoadSigningKeyHistory(ctx, executor, workspaceID)
	if err != nil {
		return nil, SigningKeyRecord{}, err
	}
	active, err := LoadActiveSigningKey(ctx, executor, workspaceID)
	return history, active, err
}
