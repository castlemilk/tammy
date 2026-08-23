//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package sbr

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
)

const (
	testWorkspaceID  = "018f0000-0000-7000-8000-000000000701"
	testOrganisation = "018f0000-0000-7000-8000-000000000702"
	testABN          = "11000000560"
	testTime         = "2026-08-23T00:00:00.000000000Z"
)

func TestSQLCipherRepositoryPersistsOnlyRedactedBindingsAndEnforcesScope(t *testing.T) {
	ctx := context.Background()
	repository, database, path := newRepositoryHarness(t)
	key := testBindingKey(0x11)
	binding := Binding{Key: key, ComponentVersion: "simulator-v1", SubjectHash: digest(0x12),
		ExpiresAt: testTime, State: BindingActive, Revision: 1, UpdatedAt: testTime}
	if err := repository.PutBinding(ctx, binding); err != nil {
		t.Fatal(err)
	}
	loaded, err := repository.GetBinding(ctx, key)
	if err != nil || loaded != binding {
		t.Fatalf("GetBinding() = %#v, %v", loaded, err)
	}

	for _, wrong := range []BindingKey{
		withWorkspace(key, "018f0000-0000-7000-8000-000000000711"),
		withOrganisation(key, "018f0000-0000-7000-8000-000000000712"),
		withABN(key, "53004085616"),
		withFingerprint(key, digest(0x13)),
	} {
		if _, err := repository.GetBinding(ctx, wrong); !errors.Is(err, ErrNotFound) {
			t.Fatalf("cross-binding GetBinding error = %v, want ErrNotFound", err)
		}
		if err := repository.TransitionBinding(ctx, wrong, BindingRemoved, testTime); !errors.Is(err, ErrNotFound) {
			t.Fatalf("cross-binding mutation error = %v, want ErrNotFound", err)
		}
	}
	if err := repository.TransitionBinding(ctx, key, BindingReimportRequired, testTime); err != nil {
		t.Fatal(err)
	}
	if err := repository.TransitionBinding(ctx, key, BindingRemoved, testTime); err != nil {
		t.Fatal(err)
	}
	if err := repository.TransitionBinding(ctx, key, BindingActive, testTime); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("resurrection error = %v, want ErrInvalidTransition", err)
	}

	assertSchemaHasNoSecretColumns(t, database)
	if err := database.Checkpoint(ctx); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, sentinel := range [][]byte{
		[]byte("SENTINEL-CREDENTIAL-BYTES"), []byte("SENTINEL-PASSWORD"),
		[]byte("SENTINEL-PRODUCT-ID"), []byte("https://sentinel.invalid"),
	} {
		if bytes.Contains(raw, sentinel) {
			t.Fatalf("encrypted database contains sentinel secret %q", sentinel)
		}
	}
}

func TestRepositoryRejectsInvalidABNChecksumAndReservedFingerprints(t *testing.T) {
	repository, _, _ := newRepositoryHarness(t)
	invalidABN := testBindingKey(0x21)
	invalidABN.CanonicalABN = "11000000561"
	zeroFingerprint := testBindingKey(0x22)
	zeroFingerprint.CredentialFingerprint = [sha256.Size]byte{}
	for name, key := range map[string]BindingKey{"invalid ABN checksum": invalidABN, "zero fingerprint": zeroFingerprint} {
		t.Run(name, func(t *testing.T) {
			err := repository.PutBinding(context.Background(), Binding{Key: key, ComponentVersion: "simulator-v1",
				SubjectHash: digest(0x23), ExpiresAt: testTime, State: BindingActive, Revision: 1, UpdatedAt: testTime})
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("PutBinding() error = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestMutationReconciliationConvergesAtEveryCrashPoint(t *testing.T) {
	ctx := context.Background()
	for _, kind := range []MutationKind{MutationImportCredential, MutationReplaceCredential, MutationRemoveCredential,
		MutationImportProductID, MutationRemoveProductID} {
		for _, crash := range []string{"before_stage", "after_stage", "after_sql_commit", "after_helper_commit"} {
			t.Run(string(kind)+"/"+crash, func(t *testing.T) {
				repository, _, _ := newRepositoryHarness(t)
				key := testBindingKey(byte(len(kind) + len(crash)))
				if kind == MutationImportCredential {
					key.CredentialFingerprint = [sha256.Size]byte{}
				} else {
					putTestBinding(t, repository, key)
				}
				operationID := operationFor(kind, crash)
				mutation := Mutation{OperationID: operationID, Key: key, Kind: kind, State: MutationPrepared,
					MetadataHash: digest(0x31), CreatedAt: testTime, UpdatedAt: testTime}
				command := prepareTestCommandMutation(t, repository, mutation)
				pendingID := "018f0000-0000-7000-8000-000000000741"
				actualKey := key
				if crash != "before_stage" {
					if kind == MutationImportCredential {
						actualKey.CredentialFingerprint = digest(0x91)
						if err := repository.MarkImportMutationStaged(ctx, key, operationID, pendingID,
							actualKey.CredentialFingerprint, testTime); err != nil {
							t.Fatal(err)
						}
					} else if err := repository.MarkMutationStaged(ctx, key, operationID, pendingID, testTime); err != nil {
						t.Fatal(err)
					}
				}
				if crash == "after_sql_commit" || crash == "after_helper_commit" {
					staged, err := repository.GetMutation(ctx, key, operationID)
					if err != nil {
						t.Fatal(err)
					}
					commit := testMutationCommit(staged, command)
					if err := repository.CommitMutation(ctx, key, operationID, commit); err != nil {
						t.Fatal(err)
					}
				}
				if crash == "after_helper_commit" {
					if err := repository.FinalizeMutation(ctx, actualKey, operationID, testTime,
						func(context.Context, MutationEffectExecutor, AuditRecord) error { return nil }); err != nil {
						t.Fatal(err)
					}
				}
				action, err := repository.ReconcileMutation(ctx, key, operationID, testTime)
				if err != nil {
					t.Fatal(err)
				}
				want := ReconcileAbort
				if crash == "after_sql_commit" {
					want = ReconcileCommit
				} else if crash == "after_helper_commit" {
					want = ReconcileNone
				}
				if action != want {
					t.Fatalf("ReconcileMutation() = %q, want %q", action, want)
				}
				stored, err := repository.GetMutation(ctx, key, operationID)
				if err != nil {
					t.Fatal(err)
				}
				wantState := MutationAborted
				if crash == "after_stage" {
					wantState = MutationAbortRequired
				}
				if want == ReconcileCommit {
					wantState = MutationReconcileRequired
				} else if want == ReconcileNone {
					wantState = MutationHelperCommitted
				}
				if stored.State != wantState {
					t.Fatalf("state = %q, want %q", stored.State, wantState)
				}
				if stored.State == MutationHelperCommitted && stored.PendingID != "" {
					t.Fatalf("committed mutation retained helper pending authority %q", stored.PendingID)
				}
				if crash == "after_sql_commit" {
					switch kind {
					case MutationImportCredential:
						if _, err := repository.GetBinding(ctx, actualKey); !errors.Is(err, ErrNotFound) {
							t.Fatalf("import visible before helper ack: %v", err)
						}
					default:
						if binding, err := repository.GetBinding(ctx, key); err != nil || binding.State != BindingActive {
							t.Fatalf("old binding before helper ack=%#v err=%v", binding, err)
						}
					}
				}
				if crash == "after_helper_commit" {
					switch kind {
					case MutationImportCredential:
						if binding, err := repository.GetBinding(ctx, actualKey); err != nil || binding.State != BindingActive {
							t.Fatalf("imported binding=%#v err=%v", binding, err)
						}
					case MutationReplaceCredential:
						if binding, err := repository.GetBinding(ctx, key); err != nil || binding.State != BindingRemoved {
							t.Fatalf("replaced old binding=%#v err=%v", binding, err)
						}
						replacement := withFingerprint(key, digest(0x92))
						if binding, err := repository.GetBinding(ctx, replacement); err != nil || binding.State != BindingActive {
							t.Fatalf("replacement binding=%#v err=%v", binding, err)
						}
					case MutationRemoveCredential:
						if binding, err := repository.GetBinding(ctx, key); err != nil || binding.State != BindingRemoved {
							t.Fatalf("removed binding=%#v err=%v", binding, err)
						}
						if err := repository.TransitionBinding(ctx, key, BindingActive, testTime); !errors.Is(err, ErrInvalidTransition) {
							t.Fatalf("removed binding resurrection error=%v", err)
						}
					case MutationImportProductID, MutationRemoveProductID:
						if binding, err := repository.GetBinding(ctx, key); err != nil || binding.State != BindingActive {
							t.Fatalf("product mutation changed binding=%#v err=%v", binding, err)
						}
					}
				}
			})
		}
	}
}

func TestAbortRecoveryRepeatsUntilHelperAcknowledges(t *testing.T) {
	ctx := context.Background()
	repository, _, _ := newRepositoryHarness(t)
	key := testBindingKey(0x2a)
	putTestBinding(t, repository, key)
	operationID := "018f0000-0000-7000-8000-00000000072a"
	pendingID := "018f0000-0000-7000-8000-00000000072b"
	if err := repository.PrepareMutation(ctx, Mutation{OperationID: operationID, Key: key, Kind: MutationReplaceCredential,
		State: MutationPrepared, MetadataHash: digest(0x2b), CreatedAt: testTime, UpdatedAt: testTime}); err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkMutationStaged(ctx, key, operationID, pendingID, testTime); err != nil {
		t.Fatal(err)
	}
	for restart := 0; restart < 2; restart++ {
		action, err := repository.ReconcileMutation(ctx, key, operationID, testTime)
		if err != nil || action != ReconcileAbort {
			t.Fatalf("restart %d action=%q err=%v", restart, action, err)
		}
		stored, err := repository.GetMutation(ctx, key, operationID)
		if err != nil || stored.State != MutationAbortRequired || stored.PendingID != pendingID {
			t.Fatalf("restart %d mutation=%#v err=%v", restart, stored, err)
		}
	}
	if err := repository.MarkMutationAbortDispatched(ctx, key, operationID, testTime); err != nil {
		t.Fatal(err)
	}
	if action, err := repository.ReconcileMutation(ctx, key, operationID, testTime); err != nil || action != ReconcileAbort {
		t.Fatalf("ABORTING reconcile action=%q err=%v", action, err)
	}
	if err := repository.AcknowledgeMutationAbort(ctx, key, operationID, testTime); err != nil {
		t.Fatal(err)
	}
	stored, err := repository.GetMutation(ctx, key, operationID)
	if err != nil || stored.State != MutationAborted || stored.PendingID != "" {
		t.Fatalf("acked mutation=%#v err=%v", stored, err)
	}
	if action, err := repository.ReconcileMutation(ctx, key, operationID, testTime); err != nil || action != ReconcileNone {
		t.Fatalf("acked reconcile action=%q err=%v", action, err)
	}
}

func TestInitialCredentialImportCanBePreparedBeforeAKeychainFingerprintExists(t *testing.T) {
	repository, _, _ := newRepositoryHarness(t)
	key := testBindingKey(0x30)
	key.CredentialFingerprint = [sha256.Size]byte{}
	mutation := Mutation{OperationID: "018f0000-0000-7000-8000-000000000731", Key: key,
		Kind: MutationImportCredential, State: MutationPrepared, MetadataHash: digest(0x32), CreatedAt: testTime, UpdatedAt: testTime}
	if err := repository.PrepareMutation(context.Background(), mutation); err != nil {
		t.Fatalf("PrepareMutation() before credential binding = %v", err)
	}
	actual := digest(0x33)
	if err := repository.MarkImportMutationStaged(context.Background(), key, mutation.OperationID,
		"018f0000-0000-7000-8000-000000000732", actual, testTime); err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkImportMutationStaged(context.Background(), key, mutation.OperationID,
		"018f0000-0000-7000-8000-000000000733", digest(0x34), testTime); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("second staged fingerprint error = %v, want ErrInvalidTransition", err)
	}
	stored, err := repository.GetMutation(context.Background(), key, mutation.OperationID)
	if err != nil || stored.Key.CredentialFingerprint != actual {
		t.Fatalf("staged mutation = %#v, %v", stored, err)
	}
}

func TestCommitMutationAtomicallyAppliesBindingAndAuditEffects(t *testing.T) {
	ctx := context.Background()
	repository, database, _ := newRepositoryHarness(t)
	oldKey := testBindingKey(0x35)
	putTestBinding(t, repository, oldKey)
	operationID := "018f0000-0000-7000-8000-000000000735"
	mutation := Mutation{OperationID: operationID, Key: oldKey, Kind: MutationReplaceCredential,
		State: MutationPrepared, MetadataHash: digest(0x36), CreatedAt: testTime, UpdatedAt: testTime}
	command := prepareTestCommandMutation(t, repository, mutation)
	if err := repository.MarkMutationStaged(ctx, oldKey, operationID, "018f0000-0000-7000-8000-000000000736", testTime); err != nil {
		t.Fatal(err)
	}
	newKey := withFingerprint(oldKey, digest(0x37))
	newBinding := Binding{Key: newKey, ComponentVersion: "simulator-v2", SubjectHash: digest(0x38),
		ExpiresAt: testTime, State: BindingActive, Revision: 1, UpdatedAt: testTime}
	profile := AuthenticatedProfile{Key: newKey, Environment: EnvironmentSimulator, ProfileFingerprint: digest(0x39),
		RegistrationFingerprint: digest(0x3a), ComponentFingerprint: digest(0x3b), Conformance: ConformanceSimulator}
	readiness := ReadinessTransition{TransitionID: "018f0000-0000-7000-8000-000000000737", Key: newKey,
		State: ReadinessReadyForSimulator, ReasonCode: "ATOMIC_REPLACEMENT"}
	completion := CommandCompletion{Scope: command.Scope, UpdatedAt: testTime}
	auditRecord := AuditRecord{Action: AuditCredentialReplaced, StatusCode: "ATOMIC_REPLACEMENT"}
	if _, err := database.ExecContext(ctx, `CREATE TABLE sbr_audit_probe(operation_id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	wrongProfile := profile
	wrongProfile.Key = oldKey
	if err := repository.CommitMutation(ctx, oldKey, operationID, MutationCommit{NewBinding: &newBinding, Profile: &wrongProfile,
		Command: &completion, CompletionAudit: auditRecord,
		Decision: func(context.Context, MutationEffectExecutor) error { return nil }}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("CommitMutation() cross-binding evidence error = %v, want ErrInvalid", err)
	}
	injected := errors.New("audit injection")
	if err := repository.CommitMutation(ctx, oldKey, operationID, MutationCommit{NewBinding: &newBinding, Profile: &profile, Readiness: &readiness,
		Command: &completion, CompletionAudit: auditRecord,
		Decision: func(ctx context.Context, tx MutationEffectExecutor) error {
			if _, err := tx.ExecContext(ctx, `INSERT INTO sbr_audit_probe(operation_id) VALUES (?)`, operationID); err != nil {
				return err
			}
			return injected
		}}); !errors.Is(err, injected) {
		t.Fatalf("CommitMutation() error = %v, want injected", err)
	}
	if binding, err := repository.GetBinding(ctx, oldKey); err != nil || binding.State != BindingActive {
		t.Fatalf("old binding before successful transaction = %#v, %v", binding, err)
	}
	if _, err := repository.GetBinding(ctx, newKey); !errors.Is(err, ErrNotFound) {
		t.Fatalf("new binding escaped rollback: %v", err)
	}
	var auditCount int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sbr_audit_probe`).Scan(&auditCount); err != nil || auditCount != 0 {
		t.Fatalf("audit rollback count = %d, %v", auditCount, err)
	}
	for _, table := range []string{"sbr_authenticated_profiles_v1", "sbr_readiness_transitions_v1"} {
		var count int
		if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s rollback count = %d, %v", table, count, err)
		}
	}
	if err := repository.CommitMutation(ctx, oldKey, operationID, MutationCommit{NewBinding: &newBinding, Profile: &profile, Readiness: &readiness,
		Command: &completion, CompletionAudit: auditRecord,
		Decision: func(ctx context.Context, tx MutationEffectExecutor) error {
			_, err := tx.ExecContext(ctx, `INSERT INTO sbr_audit_probe(operation_id) VALUES (?)`, operationID)
			return err
		}}); err != nil {
		t.Fatal(err)
	}
	if oldBinding, err := repository.GetBinding(ctx, oldKey); err != nil || oldBinding.State != BindingActive {
		t.Fatalf("old binding before helper ack = %#v, %v", oldBinding, err)
	}
	if _, err := repository.GetBinding(ctx, newKey); !errors.Is(err, ErrNotFound) {
		t.Fatalf("replacement visible before helper ack: %v", err)
	}
	if err := repository.FinalizeMutation(ctx, oldKey, operationID, testTime,
		func(context.Context, MutationEffectExecutor, AuditRecord) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if oldBinding, err := repository.GetBinding(ctx, oldKey); err != nil || oldBinding.State != BindingRemoved {
		t.Fatalf("old binding after commit = %#v, %v", oldBinding, err)
	}
	if replacement, err := repository.GetBinding(ctx, newKey); err != nil || replacement.State != BindingActive {
		t.Fatalf("replacement after commit = %#v, %v", replacement, err)
	}
	stored, err := repository.GetMutation(ctx, oldKey, operationID)
	if err != nil || stored.State != MutationHelperCommitted {
		t.Fatalf("mutation = %#v, %v", stored, err)
	}
	if latestProfile, err := repository.GetAuthenticatedProfile(ctx, newKey, EnvironmentSimulator); err != nil || latestProfile.ProfileFingerprint != profile.ProfileFingerprint {
		t.Fatalf("atomic profile = %#v, %v", latestProfile, err)
	}
	if latestReadiness, err := repository.LatestReadiness(ctx, newKey); err != nil || latestReadiness.TransitionID != readiness.TransitionID {
		t.Fatalf("atomic readiness = %#v, %v", latestReadiness, err)
	}
}

func TestCoreCommittedMutationEffectsRemainInvisibleUntilHelperAck(t *testing.T) {
	ctx := context.Background()
	repository, database, _ := newRepositoryHarness(t)
	oldKey := testBindingKey(0xe1)
	putTestBinding(t, repository, oldKey)
	operationID := "018f0000-0000-7000-8000-000000000e01"
	pendingID := "018f0000-0000-7000-8000-000000000e02"
	command := CommandRecord{OperationID: operationID, ActorUserID: "018f0000-0000-7000-8000-000000000e03",
		Scope: BindingKey{WorkspaceID: oldKey.WorkspaceID, OrganisationID: oldKey.OrganisationID,
			CanonicalABN: oldKey.CanonicalABN, SchemaVersion: oldKey.SchemaVersion},
		IdempotencyKey: "replace-visible-after-ack", SemanticHash: digest(0xe2), Kind: MutationReplaceCredential,
		State: CommandPrepared, CreatedAt: testTime, UpdatedAt: testTime}
	mutation := Mutation{OperationID: operationID, Key: oldKey, Kind: MutationReplaceCredential,
		State: MutationPrepared, MetadataHash: digest(0xe3), CreatedAt: testTime, UpdatedAt: testTime}
	if err := repository.PrepareCommandMutation(ctx, command, mutation); err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkMutationStaged(ctx, oldKey, operationID, pendingID, testTime); err != nil {
		t.Fatal(err)
	}
	newKey := withFingerprint(oldKey, digest(0xe4))
	newBinding := testBinding(newKey, "simulator-v2")
	completion := CommandCompletion{Scope: command.Scope, Credential: CredentialMetadata{State: tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT,
		Fingerprint: newKey.CredentialFingerprint, CanonicalABN: oldKey.CanonicalABN, Issuer: "synthetic issuer", Serial: "e4",
		CreatedAt: time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC), ExpiresAt: time.Date(2027, 8, 23, 0, 0, 0, 0, time.UTC), ComponentVersion: "simulator-v2"}, UpdatedAt: testTime}
	audit := AuditRecord{Action: AuditCredentialReplaced, CredentialFingerprint: newKey.CredentialFingerprint,
		ProfileFingerprint: digest(0xe5), ComponentFingerprint: digest(0xe6), StatusCode: string(AuditCredentialReplaced)}
	if err := repository.CommitMutation(ctx, oldKey, operationID, MutationCommit{NewBinding: newBinding, Command: &completion,
		CompletionAudit: audit, Decision: func(context.Context, MutationEffectExecutor) error { return nil }}); err != nil {
		t.Fatal(err)
	}
	if old, err := repository.GetBinding(ctx, oldKey); err != nil || old.State != BindingActive {
		t.Fatalf("old binding before helper ack = %#v, %v", old, err)
	}
	if _, err := repository.GetBinding(ctx, newKey); !errors.Is(err, ErrNotFound) {
		t.Fatalf("pending replacement visible before helper ack: %v", err)
	}
	if stored, err := repository.GetCommand(ctx, command.Scope, command.IdempotencyKey); err != nil || stored.State != CommandPrepared {
		t.Fatalf("command before helper ack = %#v, %v", stored, err)
	}
	var audited AuditRecord
	if err := repository.FinalizeMutation(ctx, oldKey, operationID, testTime,
		func(_ context.Context, _ MutationEffectExecutor, record AuditRecord) error {
			audited = record
			return nil
		}); err != nil {
		t.Fatal(err)
	}
	if old, err := repository.GetBinding(ctx, oldKey); err != nil || old.State != BindingRemoved {
		t.Fatalf("old binding after helper ack = %#v, %v", old, err)
	}
	if replacement, err := repository.GetBinding(ctx, newKey); err != nil || replacement.State != BindingActive {
		t.Fatalf("replacement after helper ack = %#v, %v", replacement, err)
	}
	if stored, err := repository.GetCommand(ctx, command.Scope, command.IdempotencyKey); err != nil || stored.State != CommandCompleted {
		t.Fatalf("command after helper ack = %#v, %v", stored, err)
	}
	if audited != audit {
		t.Fatalf("completion audit = %#v, want %#v", audited, audit)
	}
	var pendingEffects int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sbr_pending_mutation_effects_v1 WHERE operation_id=?`, operationID).Scan(&pendingEffects); err != nil || pendingEffects != 1 {
		t.Fatalf("durable pending effect history count=%d err=%v", pendingEffects, err)
	}
	var encodedEffect []byte
	if err := database.QueryRowContext(ctx, `SELECT effect_json FROM sbr_pending_mutation_effects_v1 WHERE operation_id=?`, operationID).Scan(&encodedEffect); err != nil {
		t.Fatal(err)
	}
	for _, secret := range [][]byte{[]byte("SENTINEL-PASSWORD"), []byte("SENTINEL-PRODUCT-ID"),
		[]byte("SENTINEL-BOOKMARK"), []byte("https://sentinel.invalid"), []byte("PRIVATE KEY")} {
		if bytes.Contains(encodedEffect, secret) {
			t.Fatalf("pending effect retained forbidden secret %q", secret)
		}
	}
}

func TestCommitMutationRollsBackCreateAndRemoveOnAuditFailure(t *testing.T) {
	ctx := context.Background()
	injected := errors.New("audit injection")
	t.Run("create", func(t *testing.T) {
		repository, _, _ := newRepositoryHarness(t)
		scope := testBindingKey(0x3c)
		scope.CredentialFingerprint = [sha256.Size]byte{}
		operationID := "018f0000-0000-7000-8000-00000000073c"
		mutation := Mutation{OperationID: operationID, Key: scope, Kind: MutationImportCredential,
			State: MutationPrepared, MetadataHash: digest(0x3d), CreatedAt: testTime, UpdatedAt: testTime}
		command := prepareTestCommandMutation(t, repository, mutation)
		actualKey := withFingerprint(scope, digest(0x3e))
		if err := repository.MarkImportMutationStaged(ctx, scope, operationID,
			"018f0000-0000-7000-8000-00000000073d", actualKey.CredentialFingerprint, testTime); err != nil {
			t.Fatal(err)
		}
		binding := testBinding(actualKey, "simulator-import")
		if err := repository.CommitMutation(ctx, scope, operationID, MutationCommit{NewBinding: binding,
			Command:         &CommandCompletion{Scope: command.Scope, UpdatedAt: testTime},
			CompletionAudit: AuditRecord{Action: AuditCredentialImported, StatusCode: "TEST_IMPORT"},
			Decision:        func(context.Context, MutationEffectExecutor) error { return injected }}); !errors.Is(err, injected) {
			t.Fatalf("CommitMutation() error = %v, want injected", err)
		}
		if _, err := repository.GetBinding(ctx, actualKey); !errors.Is(err, ErrNotFound) {
			t.Fatalf("created binding escaped rollback: %v", err)
		}
		if stored, err := repository.GetMutation(ctx, scope, operationID); err != nil || stored.State != MutationStaged {
			t.Fatalf("rolled-back create mutation = %#v, %v", stored, err)
		}
	})
	t.Run("remove", func(t *testing.T) {
		repository, _, _ := newRepositoryHarness(t)
		key := testBindingKey(0x3f)
		putTestBinding(t, repository, key)
		operationID := "018f0000-0000-7000-8000-00000000073e"
		mutation := Mutation{OperationID: operationID, Key: key, Kind: MutationRemoveCredential,
			State: MutationPrepared, MetadataHash: digest(0x40), CreatedAt: testTime, UpdatedAt: testTime}
		command := prepareTestCommandMutation(t, repository, mutation)
		if err := repository.MarkMutationStaged(ctx, key, operationID,
			"018f0000-0000-7000-8000-00000000073f", testTime); err != nil {
			t.Fatal(err)
		}
		if err := repository.CommitMutation(ctx, key, operationID, MutationCommit{
			Command:         &CommandCompletion{Scope: command.Scope, UpdatedAt: testTime},
			CompletionAudit: AuditRecord{Action: AuditCredentialRemoved, StatusCode: "TEST_REMOVE"},
			Decision:        func(context.Context, MutationEffectExecutor) error { return injected }}); !errors.Is(err, injected) {
			t.Fatalf("CommitMutation() error = %v, want injected", err)
		}
		if binding, err := repository.GetBinding(ctx, key); err != nil || binding.State != BindingActive {
			t.Fatalf("removed binding escaped rollback = %#v, %v", binding, err)
		}
		if stored, err := repository.GetMutation(ctx, key, operationID); err != nil || stored.State != MutationStaged {
			t.Fatalf("rolled-back remove mutation = %#v, %v", stored, err)
		}
	})
}

func TestCommitMutationAuditReceivesNoTransactionLifecycleAuthority(t *testing.T) {
	ctx := context.Background()
	repository, _, _ := newRepositoryHarness(t)
	key := testBindingKey(0x45)
	putTestBinding(t, repository, key)
	operationID := "018f0000-0000-7000-8000-000000000745"
	mutation := Mutation{OperationID: operationID, Key: key, Kind: MutationRemoveCredential,
		State: MutationPrepared, MetadataHash: digest(0x46), CreatedAt: testTime, UpdatedAt: testTime}
	command := prepareTestCommandMutation(t, repository, mutation)
	if err := repository.MarkMutationStaged(ctx, key, operationID,
		"018f0000-0000-7000-8000-000000000746", testTime); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("audit injection")
	sawConcreteTransaction, sawCommit, sawRollback := false, false, false
	err := repository.CommitMutation(ctx, key, operationID, MutationCommit{
		Command:         &CommandCompletion{Scope: command.Scope, UpdatedAt: testTime},
		CompletionAudit: AuditRecord{Action: AuditCredentialRemoved, StatusCode: "TEST_REMOVE"},
		Decision: func(ctx context.Context, executor MutationEffectExecutor) error {
			_, sawConcreteTransaction = any(executor).(*sqlcipher.Transaction)
			_, sawCommit = any(executor).(interface{ Commit() error })
			_, sawRollback = any(executor).(interface{ Rollback() error })
			if _, err := executor.ExecContext(ctx, `UPDATE organisations SET legal_name=legal_name WHERE id=?`, key.OrganisationID); err != nil {
				return err
			}
			return injected
		}})
	if !errors.Is(err, injected) {
		t.Fatalf("CommitMutation() error = %v, want injected", err)
	}
	if sawConcreteTransaction || sawCommit || sawRollback {
		t.Fatalf("audit authority leaked transaction=%t commit=%t rollback=%t", sawConcreteTransaction, sawCommit, sawRollback)
	}
	if binding, err := repository.GetBinding(ctx, key); err != nil || binding.State != BindingActive {
		t.Fatalf("binding escaped audit rollback = %#v, %v", binding, err)
	}
}

func TestSimulatorTransportStateAndIdempotencyAreDurableAndClosed(t *testing.T) {
	ctx := context.Background()
	repository, _, _ := newRepositoryHarness(t)
	key := testBindingKey(0x41)
	putTestBinding(t, repository, key)
	original := SimulatorTransport{OperationID: "018f0000-0000-7000-8000-000000000751", ActorUserID: serviceUserID, Key: key,
		IdempotencyKey: "fixture-1", SemanticHash: digest(0x42), State: TransportPrepared,
		CreatedAt: testTime, UpdatedAt: testTime}
	stored, replay, err := repository.PrepareSimulatorTransport(ctx, original)
	if err != nil || replay || stored.OperationID != original.OperationID {
		t.Fatalf("first prepare = %#v, replay=%v err=%v", stored, replay, err)
	}
	stored, replay, err = repository.PrepareSimulatorTransport(ctx, SimulatorTransport{OperationID: "018f0000-0000-7000-8000-000000000752",
		ActorUserID: serviceUserID, Key: key, IdempotencyKey: original.IdempotencyKey, SemanticHash: original.SemanticHash, State: TransportPrepared,
		CreatedAt: testTime, UpdatedAt: testTime})
	if err != nil || !replay || stored.OperationID != original.OperationID {
		t.Fatalf("same semantic replay = %#v, replay=%v err=%v", stored, replay, err)
	}
	conflict := original
	conflict.OperationID = "018f0000-0000-7000-8000-000000000753"
	conflict.SemanticHash = digest(0x43)
	if _, _, err := repository.PrepareSimulatorTransport(ctx, conflict); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflicting prepare error = %v", err)
	}

	if err := repository.TransitionSimulatorTransport(ctx, key, original.OperationID, TransportDispatching, nil, testTime); err != nil {
		t.Fatal(err)
	}
	if err := repository.TransitionSimulatorTransport(ctx, key, original.OperationID, TransportResponseReceived, nil, testTime); err != nil {
		t.Fatal(err)
	}
	if err := repository.TransitionSimulatorTransport(ctx, key, original.OperationID, TransportAccepted, digestPtr(0x44), testTime); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("terminal transition bypassed durable validation outcome: %v", err)
	}
	if err := repository.ApplySimulatorCase(ctx, key, original.OperationID, SimulatorCaseAccepted, digestPtr(0x44), testTime); err != nil {
		t.Fatal(err)
	}
	if err := repository.TransitionSimulatorTransport(ctx, key, original.OperationID, TransportDispatching, nil, testTime); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("terminal resend error = %v", err)
	}
	terminalRetry := original
	terminalRetry.OperationID = "018f0000-0000-7000-8000-000000000757"
	terminalRetry.IdempotencyKey = "terminal-retry"
	if err := repository.RetryNotStarted(ctx, key, original.OperationID, terminalRetry); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("terminal retry error = %v", err)
	}

	maybe := original
	maybe.OperationID = "018f0000-0000-7000-8000-000000000754"
	maybe.IdempotencyKey = "fixture-2"
	if _, _, err := repository.PrepareSimulatorTransport(ctx, maybe); err != nil {
		t.Fatal(err)
	}
	if err := repository.TransitionSimulatorTransport(ctx, key, maybe.OperationID, TransportDispatching, nil, testTime); err != nil {
		t.Fatal(err)
	}
	if err := repository.TransitionSimulatorTransport(ctx, key, maybe.OperationID, TransportMaybeSent, nil, testTime); err != nil {
		t.Fatal(err)
	}
	if recovered, err := repository.RecoverSimulatorOrphans(ctx, testTime); err != nil || recovered != 1 {
		t.Fatalf("RecoverSimulatorOrphans() = %d, %v", recovered, err)
	}
	if err := repository.TransitionSimulatorTransport(ctx, key, maybe.OperationID, TransportDispatching, nil, testTime); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("UNKNOWN resend error = %v", err)
	}
	newKeyUnknown := original
	newKeyUnknown.OperationID = "018f0000-0000-7000-8000-000000000759"
	newKeyUnknown.IdempotencyKey = "unknown-new-key"
	newKeyUnknown.SemanticHash = maybe.SemanticHash
	if _, _, err := repository.PrepareSimulatorTransport(ctx, newKeyUnknown); !errors.Is(err, ErrUncertainTransport) {
		t.Fatalf("UNKNOWN semantic operation under new key error = %v, want ErrUncertainTransport", err)
	}
	unknownRetry := original
	unknownRetry.OperationID = "018f0000-0000-7000-8000-000000000758"
	unknownRetry.IdempotencyKey = "unknown-retry"
	if err := repository.RetryNotStarted(ctx, key, maybe.OperationID, unknownRetry); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("UNKNOWN retry error = %v", err)
	}

	notStarted := original
	notStarted.OperationID = "018f0000-0000-7000-8000-000000000755"
	notStarted.IdempotencyKey = "fixture-3"
	notStarted.SemanticHash = digest(0x45)
	if _, _, err := repository.PrepareSimulatorTransport(ctx, notStarted); err != nil {
		t.Fatal(err)
	}
	if err := repository.TransitionSimulatorTransport(ctx, key, notStarted.OperationID, TransportNotStarted, nil, testTime); err != nil {
		t.Fatal(err)
	}
	retry := original
	retry.OperationID = "018f0000-0000-7000-8000-000000000756"
	retry.IdempotencyKey = "fixture-4"
	retry.SemanticHash = notStarted.SemanticHash
	if err := repository.RetryNotStarted(ctx, key, notStarted.OperationID, retry); err != nil {
		t.Fatal(err)
	}
}

func TestSimulatorCasesMapExactlyAndRecordSyntaxBeforeValidation(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct {
		name          string
		caseValue     SimulatorCase
		dispatchFirst bool
		want          TransportState
	}{
		{"pre dispatch", SimulatorCasePreDispatchFailure, false, TransportNotStarted},
		{"uncertain write", SimulatorCaseUncertainWrite, true, TransportMaybeSent},
		{"helper death", SimulatorCaseHelperDeath, true, TransportMaybeSent},
		{"timeout", SimulatorCaseTimeout, true, TransportMaybeSent},
		{"syntax received", SimulatorCaseSyntacticResponse, true, TransportResponseReceived},
		{"malformed response", SimulatorCaseMalformedResponse, true, TransportFailed},
		{"accepted", SimulatorCaseAccepted, true, TransportAccepted},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository, _, _ := newRepositoryHarness(t)
			key := testBindingKey(byte(0x80 + len(test.name)))
			putTestBinding(t, repository, key)
			operationID := operationFor(MutationKind("SIM"), test.name)
			transport := SimulatorTransport{OperationID: operationID, ActorUserID: serviceUserID, Key: key, IdempotencyKey: "case-" + test.name,
				SemanticHash: digest(0x81), State: TransportPrepared, CreatedAt: testTime, UpdatedAt: testTime}
			if _, _, err := repository.PrepareSimulatorTransport(ctx, transport); err != nil {
				t.Fatal(err)
			}
			if test.dispatchFirst {
				if err := repository.TransitionSimulatorTransport(ctx, key, operationID, TransportDispatching, nil, testTime); err != nil {
					t.Fatal(err)
				}
			}
			observedSyntax := false
			repository.hooks = &repositoryHooks{afterResponseReceived: func() error {
				stored, err := repository.getTransport(ctx, key, operationID)
				observedSyntax = err == nil && stored.State == TransportResponseReceived
				return nil
			}}
			if err := repository.ApplySimulatorCase(ctx, key, operationID, test.caseValue, digestPtr(0x82), testTime); err != nil {
				t.Fatal(err)
			}
			stored, err := repository.getTransport(ctx, key, operationID)
			if err != nil || stored.State != test.want {
				t.Fatalf("transport=%#v err=%v", stored, err)
			}
			if (test.caseValue == SimulatorCaseMalformedResponse || test.caseValue == SimulatorCaseAccepted) && !observedSyntax {
				t.Fatal("response validation occurred before RESPONSE_RECEIVED was durable")
			}
		})
	}
}

func TestSimulatorTerminalOutcomeSurvivesCrashAfterResponseReceived(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct {
		name      string
		caseValue SimulatorCase
		want      TransportState
	}{
		{name: "accepted", caseValue: SimulatorCaseAccepted, want: TransportAccepted},
		{name: "malformed", caseValue: SimulatorCaseMalformedResponse, want: TransportFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository, database, _ := newRepositoryHarness(t)
			key := testBindingKey(byte(0x90 + len(test.name)))
			putTestBinding(t, repository, key)
			operationID := operationFor(MutationKind("SIMULATOR_CRASH"), test.name)
			transport := SimulatorTransport{OperationID: operationID, ActorUserID: serviceUserID, Key: key, IdempotencyKey: "crash-" + test.name,
				SemanticHash: digest(0x91), State: TransportPrepared, CreatedAt: testTime, UpdatedAt: testTime}
			if _, _, err := repository.PrepareSimulatorTransport(ctx, transport); err != nil {
				t.Fatal(err)
			}
			if err := repository.TransitionSimulatorTransport(ctx, key, operationID, TransportDispatching, nil, testTime); err != nil {
				t.Fatal(err)
			}
			crash := errors.New("crash after durable response")
			repository.hooks = &repositoryHooks{afterResponseReceived: func() error { return crash }}
			result := digestPtr(0x92)
			if err := repository.ApplySimulatorCase(ctx, key, operationID, test.caseValue, result, testTime); !errors.Is(err, crash) {
				t.Fatalf("ApplySimulatorCase() error = %v, want crash", err)
			}
			staged, err := repository.getTransport(ctx, key, operationID)
			if err != nil || staged.State != TransportResponseReceived || staged.pendingTerminal != test.want ||
				staged.pendingResultHash == nil || *staged.pendingResultHash != *result {
				t.Fatalf("durable pending outcome = %#v, %v", staged, err)
			}
			fixed, _ := time.Parse(time.RFC3339Nano, testTime)
			reopened, err := newSQLCipherRepository(database, func() time.Time { return fixed })
			if err != nil {
				t.Fatal(err)
			}
			if err := reopened.ApplySimulatorCase(ctx, key, operationID, test.caseValue, result, testTime); err != nil {
				t.Fatalf("reapply pending outcome: %v", err)
			}
			terminal, err := reopened.getTransport(ctx, key, operationID)
			if err != nil || terminal.State != test.want || terminal.ResultHash == nil || *terminal.ResultHash != *result ||
				terminal.pendingTerminal != "" || terminal.pendingResultHash != nil {
				t.Fatalf("reconciled terminal = %#v, %v", terminal, err)
			}
		})
	}
}

func TestNotStartedHasOneDurableIdempotentRetryEdge(t *testing.T) {
	ctx := context.Background()
	repository, database, _ := newRepositoryHarness(t)
	fixed, _ := time.Parse(time.RFC3339Nano, testTime)
	secondRepository, err := newSQLCipherRepository(database, func() time.Time { return fixed })
	if err != nil {
		t.Fatal(err)
	}
	key := testBindingKey(0x8a)
	putTestBinding(t, repository, key)
	original := SimulatorTransport{OperationID: "018f0000-0000-7000-8000-00000000078a", ActorUserID: serviceUserID, Key: key,
		IdempotencyKey: "retry-original", SemanticHash: digest(0x8b), State: TransportPrepared, CreatedAt: testTime, UpdatedAt: testTime}
	if _, _, err := repository.PrepareSimulatorTransport(ctx, original); err != nil {
		t.Fatal(err)
	}
	if err := repository.ApplySimulatorCase(ctx, key, original.OperationID, SimulatorCasePreDispatchFailure, nil, testTime); err != nil {
		t.Fatal(err)
	}
	retries := []SimulatorTransport{
		{OperationID: "018f0000-0000-7000-8000-00000000078b", ActorUserID: serviceUserID, Key: key, IdempotencyKey: "retry-a", SemanticHash: original.SemanticHash, State: TransportPrepared, CreatedAt: testTime, UpdatedAt: testTime},
		{OperationID: "018f0000-0000-7000-8000-00000000078c", ActorUserID: serviceUserID, Key: key, IdempotencyKey: "retry-b", SemanticHash: original.SemanticHash, State: TransportPrepared, CreatedAt: testTime, UpdatedAt: testTime},
	}
	start := make(chan struct{})
	type retryResult struct {
		index int
		err   error
	}
	errorsFound := make(chan retryResult, 2)
	for index, owner := range []*SQLCipherRepository{repository, secondRepository} {
		go func(index int, owner *SQLCipherRepository, retry SimulatorTransport) {
			<-start
			errorsFound <- retryResult{index: index, err: owner.RetryNotStarted(ctx, key, original.OperationID, retry)}
		}(index, owner, retries[index])
	}
	close(start)
	results := []retryResult{<-errorsFound, <-errorsFound}
	successes, conflicts := 0, 0
	winnerIndex := -1
	for _, result := range results {
		if result.err == nil {
			successes++
			winnerIndex = result.index
		} else if errors.Is(result.err, ErrConflict) {
			conflicts++
		} else {
			t.Fatalf("retry error = %v", result.err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("retry outcomes success=%d conflict=%d", successes, conflicts)
	}
	var retryCount int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sbr_simulator_transports_v1 WHERE retry_of_operation_id=?`, original.OperationID).Scan(&retryCount); err != nil {
		t.Fatal(err)
	}
	if retryCount != 1 {
		t.Fatalf("durable retry edge count = %d, want 1", retryCount)
	}
	winner := retries[winnerIndex]
	if err := repository.RetryNotStarted(ctx, key, original.OperationID, winner); err != nil {
		t.Fatalf("same retry replay = %v, want idempotent success", err)
	}
	if err := repository.TransitionSimulatorTransport(ctx, key, winner.OperationID, TransportNotStarted, nil, testTime); err != nil {
		t.Fatal(err)
	}
	chainRetries := []SimulatorTransport{
		{OperationID: "018f0000-0000-7000-8000-00000000078d", ActorUserID: serviceUserID, Key: key, IdempotencyKey: "retry-chain-a", SemanticHash: original.SemanticHash, State: TransportPrepared, CreatedAt: testTime, UpdatedAt: testTime},
		{OperationID: "018f0000-0000-7000-8000-00000000078e", ActorUserID: serviceUserID, Key: key, IdempotencyKey: "retry-chain-b", SemanticHash: original.SemanticHash, State: TransportPrepared, CreatedAt: testTime, UpdatedAt: testTime},
	}
	chainResults := make(chan retryResult, 2)
	startChain := make(chan struct{})
	for index, owner := range []*SQLCipherRepository{repository, secondRepository} {
		go func(index int, owner *SQLCipherRepository, retry SimulatorTransport) {
			<-startChain
			chainResults <- retryResult{index: index, err: owner.RetryNotStarted(ctx, key, winner.OperationID, retry)}
		}(index, owner, chainRetries[index])
	}
	close(startChain)
	chainOutcomes := []retryResult{<-chainResults, <-chainResults}
	chainSuccesses, chainConflicts, chainWinnerIndex := 0, 0, -1
	for _, result := range chainOutcomes {
		if result.err == nil {
			chainSuccesses++
			chainWinnerIndex = result.index
		} else if errors.Is(result.err, ErrConflict) {
			chainConflicts++
		} else {
			t.Fatalf("chained retry error = %v", result.err)
		}
	}
	if chainSuccesses != 1 || chainConflicts != 1 {
		t.Fatalf("chained retry outcomes success=%d conflict=%d", chainSuccesses, chainConflicts)
	}
	chainWinner := chainRetries[chainWinnerIndex]
	if err := repository.RetryNotStarted(ctx, key, winner.OperationID, chainWinner); err != nil {
		t.Fatalf("same chained retry replay = %v, want idempotent success", err)
	}
}

func TestAuthenticatedProfileAndReadinessHistoryStayBoundAndImmutable(t *testing.T) {
	ctx := context.Background()
	repository, database, _ := newRepositoryHarness(t)
	key := testBindingKey(0x61)
	putTestBinding(t, repository, key)
	profile := AuthenticatedProfile{Key: key, Environment: EnvironmentSimulator, ProfileFingerprint: digest(0x62),
		RegistrationFingerprint: digest(0x63), ComponentFingerprint: digest(0x64), Conformance: ConformanceSimulator,
		EvidenceSequence: 1, AuthenticatedAt: testTime}
	if err := repository.PutAuthenticatedProfile(ctx, profile); err != nil {
		t.Fatal(err)
	}
	loaded, err := repository.GetAuthenticatedProfile(ctx, key, EnvironmentSimulator)
	if err != nil || loaded != profile {
		t.Fatalf("GetAuthenticatedProfile() = %#v, %v", loaded, err)
	}
	wrong := withABN(key, "53004085616")
	if _, err := repository.GetAuthenticatedProfile(ctx, wrong, EnvironmentSimulator); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-binding profile error = %v", err)
	}
	transition := ReadinessTransition{TransitionID: "018f0000-0000-7000-8000-000000000761", Key: key,
		State: ReadinessReadyForSimulator, ReasonCode: "SIMULATOR_PROFILE_AUTHENTICATED", OccurredAt: testTime}
	if err := repository.AppendReadinessTransition(ctx, transition); err != nil {
		t.Fatal(err)
	}
	latest, err := repository.LatestReadiness(ctx, key)
	expectedTransition := transition
	expectedTransition.Sequence = 1
	if err != nil || latest != expectedTransition {
		t.Fatalf("LatestReadiness() = %#v, %v", latest, err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE sbr_readiness_transitions_v1 SET reason_code='forged' WHERE transition_id=?`, transition.TransitionID); err == nil {
		t.Fatal("immutable readiness transition was updated")
	}
}

func TestProfileEvidenceRotatesImmutablyAndLatestUsesRepositorySequence(t *testing.T) {
	ctx := context.Background()
	repository, _, _ := newRepositoryHarness(t)
	key := testBindingKey(0x65)
	putTestBinding(t, repository, key)
	first := AuthenticatedProfile{Key: key, Environment: EnvironmentEVTE, ProfileFingerprint: digest(0x66),
		RegistrationFingerprint: digest(0x67), ComponentFingerprint: digest(0x68),
		Conformance: ConformancePre, AuthenticatedAt: "2099-01-01T00:00:00.000000000Z"}
	second := AuthenticatedProfile{Key: key, Environment: EnvironmentEVTE, ProfileFingerprint: digest(0x69),
		RegistrationFingerprint: digest(0x6a), ComponentFingerprint: digest(0x6b),
		Conformance: ConformancePost, AuthenticatedAt: "2000-01-01T00:00:00.000000000Z"}
	if err := repository.PutAuthenticatedProfile(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := repository.PutAuthenticatedProfile(ctx, second); err != nil {
		t.Fatal(err)
	}
	latest, err := repository.GetAuthenticatedProfile(ctx, key, EnvironmentEVTE)
	if err != nil {
		t.Fatal(err)
	}
	if latest.ProfileFingerprint != second.ProfileFingerprint || latest.Conformance != ConformancePost || latest.EvidenceSequence != 2 {
		t.Fatalf("latest rotated profile = %#v", latest)
	}
	if latest.AuthenticatedAt != testTime {
		t.Fatalf("authenticated_at = %q, want repository clock %q", latest.AuthenticatedAt, testTime)
	}
	var count int
	if err := repository.database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sbr_authenticated_profiles_v1`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("immutable evidence row count = %d, %v", count, err)
	}
}

func TestLatestReadinessUsesRepositorySequenceNotCallerTimestamp(t *testing.T) {
	ctx := context.Background()
	repository, _, _ := newRepositoryHarness(t)
	key := testBindingKey(0x6c)
	putTestBinding(t, repository, key)
	first := ReadinessTransition{TransitionID: "018f0000-0000-7000-8000-00000000076c", Key: key,
		State: ReadinessUnavailable, ReasonCode: "FIRST", OccurredAt: "2099-01-01T00:00:00.000000000Z"}
	second := ReadinessTransition{TransitionID: "018f0000-0000-7000-8000-00000000076d", Key: key,
		State: ReadinessReadyForSimulator, ReasonCode: "SECOND", OccurredAt: "2000-01-01T00:00:00.000000000Z"}
	if err := repository.AppendReadinessTransition(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := repository.AppendReadinessTransition(ctx, second); err != nil {
		t.Fatal(err)
	}
	latest, err := repository.LatestReadiness(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if latest.TransitionID != second.TransitionID || latest.Sequence != 2 || latest.OccurredAt != testTime {
		t.Fatalf("latest readiness = %#v", latest)
	}
}

func TestBackupSanitizationAndRestoreNeverCarryVaultAuthority(t *testing.T) {
	ctx := context.Background()
	repository, database, _ := newRepositoryHarness(t)
	key := testBindingKey(0x71)
	putTestBinding(t, repository, key)
	mutation := Mutation{OperationID: "018f0000-0000-7000-8000-000000000771", Key: key,
		Kind: MutationReplaceCredential, State: MutationPrepared, MetadataHash: digest(0x72), CreatedAt: testTime, UpdatedAt: testTime}
	if err := repository.PrepareMutation(ctx, mutation); err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkMutationStaged(ctx, key, mutation.OperationID, "018f0000-0000-7000-8000-000000000772", testTime); err != nil {
		t.Fatal(err)
	}
	transport := SimulatorTransport{OperationID: "018f0000-0000-7000-8000-000000000773", ActorUserID: serviceUserID, Key: key,
		IdempotencyKey: "backup-fixture", SemanticHash: digest(0x73), State: TransportPrepared, CreatedAt: testTime, UpdatedAt: testTime}
	if _, _, err := repository.PrepareSimulatorTransport(ctx, transport); err != nil {
		t.Fatal(err)
	}
	if err := repository.TransitionSimulatorTransport(ctx, key, transport.OperationID, TransportDispatching, nil, testTime); err != nil {
		t.Fatal(err)
	}

	tx, err := database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := SanitizeBackupState(ctx, tx); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	stored, err := repository.GetMutation(ctx, key, mutation.OperationID)
	if err != nil || stored.State != MutationAborted || stored.PendingID != "" {
		t.Fatalf("sanitized mutation = %#v, %v", stored, err)
	}
	if err := MarkRestoredState(ctx, database, testTime); err != nil {
		t.Fatal(err)
	}
	restored, err := repository.GetBinding(ctx, key)
	if err != nil || restored.State != BindingReimportRequired {
		t.Fatalf("restored binding = %#v, %v", restored, err)
	}
	var state string
	if err := database.QueryRowContext(ctx, `SELECT state FROM sbr_simulator_transports_v1 WHERE operation_id=?`, transport.OperationID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != string(TransportUnknown) {
		t.Fatalf("restored transport = %q, want UNKNOWN", state)
	}
}

func TestBackupSanitizationFailsClosedForPartialSBRSchema(t *testing.T) {
	for _, table := range []string{"sbr_simulator_transports_v1", "sbr_pending_mutation_effects_v1"} {
		t.Run(table, func(t *testing.T) {
			ctx := context.Background()
			_, database, _ := newRepositoryHarness(t)
			if _, err := database.ExecContext(ctx, `DROP TABLE `+table); err != nil {
				t.Fatal(err)
			}
			tx, err := database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback() }()
			if err := SanitizeBackupState(ctx, tx); !errors.Is(err, ErrRepository) {
				t.Fatalf("SanitizeBackupState() error = %v, want ErrRepository", err)
			}
		})
	}
}

func newRepositoryHarness(t *testing.T) (*SQLCipherRepository, *sqlcipher.Database, string) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "workspace.db")
	key := bytes.Repeat([]byte{0x71}, sqlcipher.KeySize)
	if _, err := sqlcipher.MigrateWorkspace(ctx, path, key, 7); err != nil {
		t.Fatal(err)
	}
	database, err := sqlcipher.Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.ExecContext(ctx, `INSERT INTO organisations(id, legal_name, abn, status, created_at) VALUES (?,?,?,?,?)`,
		testOrganisation, "Wattle & Co Test Pty Ltd", testABN, "ACTIVE", testTime); err != nil {
		t.Fatal(err)
	}
	fixedTime, err := time.Parse(time.RFC3339Nano, testTime)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := newSQLCipherRepository(database, func() time.Time { return fixedTime })
	if err != nil {
		t.Fatal(err)
	}
	return repository, database, path
}

func testBindingKey(seed byte) BindingKey {
	return BindingKey{WorkspaceID: testWorkspaceID, OrganisationID: testOrganisation, CanonicalABN: testABN,
		SchemaVersion: 1, CredentialFingerprint: digest(seed)}
}

func putTestBinding(t *testing.T, repository *SQLCipherRepository, key BindingKey) {
	t.Helper()
	if err := repository.PutBinding(context.Background(), *testBinding(key, "simulator-v1")); err != nil {
		t.Fatal(err)
	}
}

func testBinding(key BindingKey, componentVersion string) *Binding {
	return &Binding{Key: key, ComponentVersion: componentVersion, SubjectHash: digest(0x77),
		ExpiresAt: testTime, State: BindingActive, Revision: 1, UpdatedAt: testTime}
}

func TestRepositoryPersistsMutationCommandElectionAndOwnedResultAcrossRestart(t *testing.T) {
	ctx := context.Background()
	repository, database, _ := newRepositoryHarness(t)
	semantic := digest(0xd1)
	operationID := "018f0000-0000-7000-8000-000000000d01"
	record := CommandRecord{
		OperationID: operationID,
		ActorUserID: serviceUserID,
		Scope: BindingKey{WorkspaceID: testWorkspaceID, OrganisationID: testOrganisation,
			CanonicalABN: testABN, SchemaVersion: 1},
		IdempotencyKey: "018f0000-0000-7000-8000-000000000d02",
		SemanticHash:   semantic,
		Kind:           MutationImportCredential,
		State:          CommandPrepared,
		CreatedAt:      testTime,
		UpdatedAt:      testTime,
	}
	if err := repository.PrepareCommand(ctx, record); err != nil {
		t.Fatalf("PrepareCommand() error = %v", err)
	}

	restarted, err := newSQLCipherRepository(database, repository.now)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := restarted.GetCommand(ctx, record.Scope, record.IdempotencyKey)
	if err != nil {
		t.Fatalf("GetCommand() after restart error = %v", err)
	}
	if stored.OperationID != operationID || stored.SemanticHash != semantic || stored.State != CommandPrepared {
		t.Fatalf("stored command = %+v", stored)
	}

	credential := CredentialMetadata{Fingerprint: digest(0xd2), CanonicalABN: testABN,
		Issuer: "Synthetic Issuer", Serial: "SIM-DURABLE", ComponentVersion: "simulator-v1",
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		ExpiresAt: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		State:     tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT}
	if err := restarted.CompleteCommand(ctx, record.Scope, operationID, credential, 0, "2026-08-23T00:00:01.000000000Z"); err != nil {
		t.Fatalf("CompleteCommand() error = %v", err)
	}
	completed, err := repository.GetCommand(ctx, record.Scope, record.IdempotencyKey)
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != CommandCompleted || completed.Credential != credential {
		t.Fatalf("completed command = %+v", completed)
	}

	conflict := record
	conflict.OperationID = "018f0000-0000-7000-8000-000000000d03"
	conflict.SemanticHash = digest(0xd3)
	if err := repository.PrepareCommand(ctx, conflict); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("different semantic PrepareCommand() error = %v", err)
	}
}

func TestUnlockDispatchReservationRollsBackWithOwningTransaction(t *testing.T) {
	ctx := context.Background()
	repository, database, _ := newRepositoryHarness(t)
	key := testBindingKey(0xe1)
	putTestBinding(t, repository, key)
	record := HelperDispatchRecord{
		OperationID: "018f0000-0000-7000-8000-000000000e11", ActorUserID: serviceUserID, Key: key,
		IdempotencyKey: "018f0000-0000-7000-8000-000000000e12", SemanticHash: digest(0xe2),
		State: HelperDispatching, CreatedAt: testTime, UpdatedAt: testTime,
	}
	tx, err := database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ReserveUnlockDispatchWithin(ctx, tx, record); err != nil {
		_ = tx.Rollback()
		t.Fatalf("ReserveUnlockDispatchWithin() error = %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sbr_helper_dispatches_v1 WHERE operation_id=?`, record.OperationID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rolled-back unlock dispatch rows = %d, want 0", count)
	}
}

func TestUnlockDispatchDecisionSurvivesRestartAndRecoversUnknown(t *testing.T) {
	ctx := context.Background()
	repository, database, path := newRepositoryHarness(t)
	key := testBindingKey(0xe3)
	putTestBinding(t, repository, key)
	record := HelperDispatchRecord{
		OperationID: "018f0000-0000-7000-8000-000000000e21", ActorUserID: serviceUserID, Key: key,
		IdempotencyKey: "018f0000-0000-7000-8000-000000000e22", SemanticHash: digest(0xe4),
		State: HelperDispatching, CreatedAt: testTime, UpdatedAt: testTime,
	}
	tx, err := database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ReserveUnlockDispatchWithin(ctx, tx, record); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	keyBytes := bytes.Repeat([]byte{0x71}, sqlcipher.KeySize)
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	reopenedDatabase, err := sqlcipher.Open(ctx, path, keyBytes)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopenedDatabase.Close() })
	fixedTime, _ := time.Parse(time.RFC3339Nano, testTime)
	reopened, err := newSQLCipherRepository(reopenedDatabase, func() time.Time { return fixedTime })
	if err != nil {
		t.Fatal(err)
	}
	if recovered, err := reopened.RecoverHelperDispatchOrphans(ctx, testTime); err != nil || recovered != 1 {
		t.Fatalf("RecoverHelperDispatchOrphans() = %d, %v", recovered, err)
	}
	stored, err := reopened.GetHelperDispatch(ctx, key, record.IdempotencyKey)
	if err != nil || stored.State != HelperDispatchUnknown || stored.ActorUserID != record.ActorUserID || stored.SemanticHash != record.SemanticHash {
		t.Fatalf("recovered unlock dispatch = %#v, %v", stored, err)
	}
}

func TestFixtureTransportPersistsActorAndConditionsDispatchReservation(t *testing.T) {
	ctx := context.Background()
	repository, database, _ := newRepositoryHarness(t)
	key := testBindingKey(0xe5)
	putTestBinding(t, repository, key)
	transport := SimulatorTransport{OperationID: "018f0000-0000-7000-8000-000000000e31", ActorUserID: serviceUserID,
		Key: key, IdempotencyKey: "018f0000-0000-7000-8000-000000000e32", SemanticHash: digest(0xe6),
		State: TransportPrepared, CreatedAt: testTime, UpdatedAt: testTime}
	stored, replay, err := repository.PrepareSimulatorTransport(ctx, transport)
	if err != nil || replay || stored.ActorUserID != serviceUserID {
		t.Fatalf("PrepareSimulatorTransport() = %#v, %t, %v", stored, replay, err)
	}
	tx, err := database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	wrongActor := "018f0000-0000-7000-8000-000000000e33"
	if err := repository.ReserveSimulatorDispatchWithin(ctx, tx, transport, wrongActor, testTime); !errors.Is(err, ErrPermissionDenied) {
		_ = tx.Rollback()
		t.Fatalf("cross-actor fixture reservation error = %v, want ErrPermissionDenied", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	tx, err = database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ReserveSimulatorDispatchWithin(ctx, tx, transport, serviceUserID, testTime); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	loaded, err := repository.getTransport(ctx, key, transport.OperationID)
	if err != nil || loaded.State != TransportDispatching {
		t.Fatalf("fixture dispatch reservation = %#v, %v", loaded, err)
	}
}

func TestSimulatorTransportReplayRejectsEveryCorruptRetainedField(t *testing.T) {
	for _, test := range []struct {
		name   string
		tamper func(*testing.T, context.Context, *sqlcipher.Database, SimulatorTransport)
	}{
		{name: "wrong result hash length", tamper: func(t *testing.T, ctx context.Context, database *sqlcipher.Database, transport SimulatorTransport) {
			tamperSimulatorTransport(t, ctx, database, `UPDATE sbr_simulator_transports_v1 SET state='ACCEPTED',result_hash=x'01' WHERE operation_id=?`, transport.OperationID)
		}},
		{name: "wrong credential fingerprint length", tamper: func(t *testing.T, ctx context.Context, database *sqlcipher.Database, transport SimulatorTransport) {
			tamperSimulatorTransport(t, ctx, database, `UPDATE sbr_simulator_transports_v1 SET credential_fingerprint=x'01' WHERE operation_id=?`, transport.OperationID)
		}},
		{name: "wrong semantic hash length", tamper: func(t *testing.T, ctx context.Context, database *sqlcipher.Database, transport SimulatorTransport) {
			tamperSimulatorTransport(t, ctx, database, `UPDATE sbr_simulator_transports_v1 SET semantic_hash=x'01' WHERE operation_id=?`, transport.OperationID)
		}},
		{name: "unknown state", tamper: func(t *testing.T, ctx context.Context, database *sqlcipher.Database, transport SimulatorTransport) {
			tamperSimulatorTransport(t, ctx, database, `UPDATE sbr_simulator_transports_v1 SET state='CORRUPT' WHERE operation_id=?`, transport.OperationID)
		}},
		{name: "invalid operation ID", tamper: func(t *testing.T, ctx context.Context, database *sqlcipher.Database, transport SimulatorTransport) {
			tamperSimulatorTransport(t, ctx, database, `UPDATE sbr_simulator_transports_v1 SET operation_id='zzzzzzzz-zzzz-7zzz-8zzz-zzzzzzzzzzzz' WHERE operation_id=?`, transport.OperationID)
		}},
		{name: "invalid actor ID", tamper: func(t *testing.T, ctx context.Context, database *sqlcipher.Database, transport SimulatorTransport) {
			tamperSimulatorTransport(t, ctx, database, `UPDATE sbr_simulator_transports_v1 SET actor_user_id='zzzzzzzz-zzzz-7zzz-8zzz-zzzzzzzzzzzz' WHERE operation_id=?`, transport.OperationID)
		}},
		{name: "invalid timestamp", tamper: func(t *testing.T, ctx context.Context, database *sqlcipher.Database, transport SimulatorTransport) {
			tamperSimulatorTransport(t, ctx, database, `UPDATE sbr_simulator_transports_v1 SET updated_at='2026-99-99T99:99:99.999999999Z' WHERE operation_id=?`, transport.OperationID)
		}},
		{name: "reversed timestamps", tamper: func(t *testing.T, ctx context.Context, database *sqlcipher.Database, transport SimulatorTransport) {
			tamperSimulatorTransport(t, ctx, database, `UPDATE sbr_simulator_transports_v1 SET updated_at='2026-08-22T00:00:00.000000000Z' WHERE operation_id=?`, transport.OperationID)
		}},
		{name: "semantic relation mismatch", tamper: func(t *testing.T, ctx context.Context, database *sqlcipher.Database, transport SimulatorTransport) {
			tamperSimulatorTransport(t, ctx, database, `UPDATE sbr_simulator_transports_v1 SET semantic_hash=? WHERE operation_id=?`, bytes.Repeat([]byte{0xf7}, sha256.Size), transport.OperationID)
		}},
		{name: "state result relationship", tamper: func(t *testing.T, ctx context.Context, database *sqlcipher.Database, transport SimulatorTransport) {
			tamperSimulatorTransport(t, ctx, database, `UPDATE sbr_simulator_transports_v1 SET result_hash=? WHERE operation_id=?`, bytes.Repeat([]byte{0xf8}, sha256.Size), transport.OperationID)
		}},
		{name: "idempotency operation relation", tamper: func(t *testing.T, ctx context.Context, database *sqlcipher.Database, transport SimulatorTransport) {
			tamperSimulatorTransport(t, ctx, database, `UPDATE sbr_idempotency_v1 SET original_operation_id='018f0000-0000-7000-8000-000000000fff' WHERE idempotency_key=?`, transport.IdempotencyKey)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			repository, database, _ := newRepositoryHarness(t)
			key := testBindingKey(0xf1)
			putTestBinding(t, repository, key)
			transport := SimulatorTransport{OperationID: "018f0000-0000-7000-8000-000000000f11", ActorUserID: serviceUserID,
				Key: key, IdempotencyKey: "018f0000-0000-7000-8000-000000000f12", SemanticHash: digest(0xf2),
				State: TransportPrepared, CreatedAt: testTime, UpdatedAt: testTime}
			if _, replay, err := repository.PrepareSimulatorTransport(ctx, transport); err != nil || replay {
				t.Fatalf("seed transport replay=%t error=%v", replay, err)
			}
			test.tamper(t, ctx, database, transport)
			if _, _, err := repository.PrepareSimulatorTransport(ctx, transport); !errors.Is(err, ErrRepository) {
				t.Fatalf("corrupt replay error=%v, want ErrRepository", err)
			}
		})
	}
}

func tamperSimulatorTransport(t *testing.T, ctx context.Context, database *sqlcipher.Database, query string, args ...any) {
	t.Helper()
	for _, trigger := range []string{"sbr_transport_v1_transition", "sbr_idempotency_v1_result_once"} {
		if _, err := database.ExecContext(ctx, `DROP TRIGGER `+trigger); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.ExecContext(ctx, `PRAGMA ignore_check_constraints = ON`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, query, args...); err != nil {
		t.Fatal(err)
	}
}

func TestFinishFixtureWithAuditAtomicallyPersistsTerminalResultAndAudit(t *testing.T) {
	ctx := context.Background()
	repository, database, _ := newRepositoryHarness(t)
	key := testBindingKey(0xea)
	putTestBinding(t, repository, key)
	transport := SimulatorTransport{OperationID: "018f0000-0000-7000-8000-000000000e41", ActorUserID: serviceUserID,
		Key: key, IdempotencyKey: "018f0000-0000-7000-8000-000000000e42", SemanticHash: digest(0xeb),
		State: TransportPrepared, CreatedAt: testTime, UpdatedAt: testTime}
	if _, _, err := repository.PrepareSimulatorTransport(ctx, transport); err != nil {
		t.Fatal(err)
	}
	tx, err := repository.database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ReserveSimulatorDispatchWithin(ctx, tx, transport, serviceUserID, testTime); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	resultHash := digest(0xec)
	auditRecord := AuditRecord{Action: AuditFixtureCompleted, CredentialFingerprint: key.CredentialFingerprint,
		StatusCode: "SBR_HELPER_FIXTURE_COMPLETED"}
	appender, err := NewRedactedSQLAuditAppender(repository.now)
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("audit unavailable")
	if err := repository.FinishSimulatorTransportWithAudit(ctx, key, transport.OperationID, SimulatorCaseAccepted,
		&resultHash, testTime, auditRecord,
		func(auditCtx context.Context, executor MutationEffectExecutor, record AuditRecord) error {
			if err := appender.Record(auditCtx, executor, record); err != nil {
				return err
			}
			return injected
		}); !errors.Is(err, injected) {
		t.Fatalf("FinishSimulatorTransportWithAudit() error = %v, want injected", err)
	}
	restarted, err := newSQLCipherRepository(database, repository.now)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := restarted.getTransport(ctx, key, transport.OperationID)
	if err != nil || stored.State != TransportDispatching || stored.ResultHash != nil {
		t.Fatalf("terminal state/result escaped failed audit transaction: %+v, %v", stored, err)
	}
	var auditCount int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sbr_audit_events_v1`).Scan(&auditCount); err != nil || auditCount != 0 {
		t.Fatalf("failed terminal audit escaped transaction: count=%d error=%v", auditCount, err)
	}
	if err := restarted.FinishSimulatorTransportWithAudit(ctx, key, transport.OperationID, SimulatorCaseAccepted,
		&resultHash, testTime, auditRecord,
		func(auditCtx context.Context, executor MutationEffectExecutor, record AuditRecord) error {
			return appender.Record(auditCtx, executor, record)
		}); err != nil {
		t.Fatal(err)
	}
	stored, err = restarted.getTransport(ctx, key, transport.OperationID)
	if err != nil || stored.State != TransportAccepted || stored.ResultHash == nil || *stored.ResultHash != resultHash {
		t.Fatalf("terminal fixture state/result = %+v, %v", stored, err)
	}
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sbr_audit_events_v1`).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("terminal audit count=%d error=%v", auditCount, err)
	}
}

func TestRepositoryPersistsOnlyRedactedProductStateAcrossRestart(t *testing.T) {
	ctx := context.Background()
	repository, database, _ := newRepositoryHarness(t)
	key := testBindingKey(0xd4)
	putTestBinding(t, repository, key)
	record := ProductRecord{Key: key, Environment: EnvironmentEVTE,
		ExpectedProductIdentifier: "EVTE.PRODUCT", ExpectedServiceID: "EVTE.SERVICE",
		State: ProductPresent, ProductFingerprint: digest(0xd6), Revision: 1, UpdatedAt: testTime}
	record.ScopeFingerprint = authenticatedProductScopeFingerprint(record.ExpectedProductIdentifier, record.ExpectedServiceID)
	if err := repository.PutProductState(ctx, record); err != nil {
		t.Fatalf("PutProductState() error = %v", err)
	}
	restarted, err := newSQLCipherRepository(database, repository.now)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := restarted.GetProductState(ctx, key, EnvironmentEVTE, record.ScopeFingerprint,
		record.ExpectedProductIdentifier, record.ExpectedServiceID)
	if err != nil || stored != record {
		t.Fatalf("GetProductState() = %+v, %v", stored, err)
	}
	assertSchemaHasNoSecretColumns(t, database)
}

func TestRepositoryProductStateRequiresExactAuthenticatedProductAndServiceScope(t *testing.T) {
	ctx := context.Background()
	repository, database, _ := newRepositoryHarness(t)
	key := testBindingKey(0xe7)
	putTestBinding(t, repository, key)
	record := ProductRecord{Key: key, Environment: EnvironmentEVTE,
		ExpectedProductIdentifier: "EVTE.PRODUCT", ExpectedServiceID: "EVTE.SERVICE.A",
		State: ProductPresent, ProductFingerprint: digest(0xe9), Revision: 1, UpdatedAt: testTime}
	record.ScopeFingerprint = authenticatedProductScopeFingerprint(record.ExpectedProductIdentifier, record.ExpectedServiceID)
	if err := repository.PutProductState(ctx, record); err != nil {
		t.Fatal(err)
	}
	restarted, err := newSQLCipherRepository(database, repository.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.GetProductState(ctx, key, EnvironmentEVTE, record.ScopeFingerprint,
		record.ExpectedProductIdentifier, "EVTE.SERVICE.B"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-service state lookup error = %v, want ErrNotFound", err)
	}
	stored, err := restarted.GetProductState(ctx, key, EnvironmentEVTE, record.ScopeFingerprint,
		record.ExpectedProductIdentifier, record.ExpectedServiceID)
	if err != nil || stored != record {
		t.Fatalf("exact state lookup = %+v, %v", stored, err)
	}
}

func TestPrepareCommandMutationRollsBackElectionWhenMutationInsertConflicts(t *testing.T) {
	ctx := context.Background()
	repository, _, _ := newRepositoryHarness(t)
	scope := BindingKey{WorkspaceID: testWorkspaceID, OrganisationID: testOrganisation, CanonicalABN: testABN, SchemaVersion: 1}
	operationID := "018f0000-0000-7000-8000-000000000d11"
	mutation := Mutation{OperationID: operationID, Key: scope, Kind: MutationImportCredential,
		State: MutationPrepared, MetadataHash: digest(0xd7), CreatedAt: testTime, UpdatedAt: testTime}
	if err := repository.PrepareMutation(ctx, mutation); err != nil {
		t.Fatal(err)
	}
	command := CommandRecord{OperationID: operationID, ActorUserID: serviceUserID, Scope: scope,
		IdempotencyKey: "018f0000-0000-7000-8000-000000000d12", SemanticHash: digest(0xd8),
		Kind: MutationImportCredential, State: CommandPrepared, CreatedAt: testTime, UpdatedAt: testTime}
	if err := repository.PrepareCommandMutation(ctx, command, mutation); !errors.Is(err, ErrConflict) {
		t.Fatalf("PrepareCommandMutation() error = %v", err)
	}
	if _, err := repository.GetCommand(ctx, scope, command.IdempotencyKey); !errors.Is(err, ErrNotFound) {
		t.Fatalf("command election escaped rollback: %v", err)
	}
}

func TestCommitMutationRollsBackFactorAndSessionTouchesWhenLaterPersistenceFails(t *testing.T) {
	ctx := context.Background()
	repository, database, _ := newRepositoryHarness(t)
	key := testBindingKey(0xea)
	putTestBinding(t, repository, key)
	operationID := "018f0000-0000-7000-8000-000000000ea1"
	mutation := Mutation{OperationID: operationID, Key: key, Kind: MutationRemoveCredential,
		State: MutationPrepared, MetadataHash: digest(0xeb), CreatedAt: testTime, UpdatedAt: testTime}
	command := prepareTestCommandMutation(t, repository, mutation)
	if err := repository.MarkMutationStaged(ctx, key, operationID,
		"018f0000-0000-7000-8000-000000000ea2", testTime); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `CREATE TABLE sbr_test_identity_touches(touches INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO sbr_test_identity_touches(touches) VALUES (0)`); err != nil {
		t.Fatal(err)
	}
	// Reserve the immutable effect key first so CommitMutation fails only after
	// its decision callback has written through the owning SQL transaction.
	if _, err := database.ExecContext(ctx, `INSERT INTO sbr_pending_mutation_effects_v1(operation_id,effect_json,created_at)
VALUES (?,?,?)`, operationID, []byte(`{}`), testTime); err != nil {
		t.Fatal(err)
	}
	err := repository.CommitMutation(ctx, key, operationID, MutationCommit{
		Command:         &CommandCompletion{Scope: command.Scope, UpdatedAt: testTime},
		CompletionAudit: AuditRecord{Action: AuditCredentialRemoved, StatusCode: "TEST_REMOVE"},
		Decision: func(decisionCtx context.Context, executor MutationEffectExecutor) error {
			_, updateErr := executor.ExecContext(decisionCtx,
				`UPDATE sbr_test_identity_touches SET touches=touches+1`)
			return updateErr
		},
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("CommitMutation() error = %v, want ErrConflict", err)
	}
	var touches int
	if err := database.QueryRowContext(ctx, `SELECT touches FROM sbr_test_identity_touches`).Scan(&touches); err != nil {
		t.Fatal(err)
	}
	if touches != 0 {
		t.Fatalf("factor/session touch escaped failed decision transaction: %d", touches)
	}
	stored, err := repository.GetMutation(ctx, key, operationID)
	if err != nil || stored.State != MutationStaged {
		t.Fatalf("mutation state after rollback = %+v, %v", stored, err)
	}
}

func TestCommitMutationAtomicallyPersistsCommandProductAndAudit(t *testing.T) {
	ctx := context.Background()
	repository, _, _ := newRepositoryHarness(t)
	key := testBindingKey(0xd9)
	putTestBinding(t, repository, key)
	operationID := "018f0000-0000-7000-8000-000000000d21"
	command := CommandRecord{OperationID: operationID,
		ActorUserID:    serviceUserID,
		Scope:          BindingKey{WorkspaceID: key.WorkspaceID, OrganisationID: key.OrganisationID, CanonicalABN: key.CanonicalABN, SchemaVersion: 1},
		IdempotencyKey: "018f0000-0000-7000-8000-000000000d22", SemanticHash: digest(0xda),
		Kind: MutationImportProductID, State: CommandPrepared, CreatedAt: testTime, UpdatedAt: testTime}
	mutation := Mutation{OperationID: operationID, Key: key, Kind: MutationImportProductID,
		State: MutationPrepared, MetadataHash: digest(0xdb), CreatedAt: testTime, UpdatedAt: testTime}
	if err := repository.PrepareCommandMutation(ctx, command, mutation); err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkMutationStaged(ctx, key, operationID,
		"018f0000-0000-7000-8000-000000000d23", testTime); err != nil {
		t.Fatal(err)
	}
	product := ProductRecord{Key: key, Environment: EnvironmentEVTE,
		ExpectedProductIdentifier: "EVTE.PRODUCT", ExpectedServiceID: "EVTE.SERVICE",
		State: ProductPresent, ProductFingerprint: digest(0xdd), Revision: 1, UpdatedAt: testTime}
	product.ScopeFingerprint = authenticatedProductScopeFingerprint(product.ExpectedProductIdentifier, product.ExpectedServiceID)
	completion := CommandCompletion{Scope: command.Scope, Credential: CredentialMetadata{}, Product: ProductPresent,
		UpdatedAt: testTime}
	auditRecord := AuditRecord{Action: AuditProductIDChanged, StatusCode: "TEST_PRODUCT_CHANGED"}
	injected := errors.New("audit failed")
	if err := repository.CommitMutation(ctx, key, operationID, MutationCommit{Product: &product, Command: &completion,
		CompletionAudit: auditRecord,
		Decision:        func(context.Context, MutationEffectExecutor) error { return injected }}); !errors.Is(err, injected) {
		t.Fatalf("CommitMutation() injected error = %v", err)
	}
	storedCommand, err := repository.GetCommand(ctx, command.Scope, command.IdempotencyKey)
	if err != nil || storedCommand.State != CommandPrepared {
		t.Fatalf("command escaped rollback: %+v, %v", storedCommand, err)
	}
	if _, err := repository.GetProductState(ctx, key, EnvironmentEVTE, product.ScopeFingerprint,
		product.ExpectedProductIdentifier, product.ExpectedServiceID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Product state escaped rollback: %v", err)
	}
	if err := repository.CommitMutation(ctx, key, operationID, MutationCommit{Product: &product, Command: &completion,
		CompletionAudit: auditRecord,
		Decision:        func(context.Context, MutationEffectExecutor) error { return nil }}); err != nil {
		t.Fatalf("CommitMutation() error = %v", err)
	}
	storedCommand, err = repository.GetCommand(ctx, command.Scope, command.IdempotencyKey)
	if err != nil || storedCommand.State != CommandPrepared {
		t.Fatalf("command visible before helper ack = %+v, %v", storedCommand, err)
	}
	if _, err := repository.GetProductState(ctx, key, EnvironmentEVTE, product.ScopeFingerprint,
		product.ExpectedProductIdentifier, product.ExpectedServiceID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Product visible before helper ack: %v", err)
	}
	if err := repository.FinalizeMutation(ctx, key, operationID, testTime,
		func(context.Context, MutationEffectExecutor, AuditRecord) error { return injected }); !errors.Is(err, injected) {
		t.Fatalf("FinalizeMutation() audit error = %v, want injected", err)
	}
	storedCommand, err = repository.GetCommand(ctx, command.Scope, command.IdempotencyKey)
	if err != nil || storedCommand.State != CommandPrepared {
		t.Fatalf("command escaped failed finalization = %+v, %v", storedCommand, err)
	}
	if _, err := repository.GetProductState(ctx, key, EnvironmentEVTE, product.ScopeFingerprint,
		product.ExpectedProductIdentifier, product.ExpectedServiceID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Product escaped failed finalization: %v", err)
	}
	if err := repository.FinalizeMutation(ctx, key, operationID, testTime,
		func(context.Context, MutationEffectExecutor, AuditRecord) error { return nil }); err != nil {
		t.Fatal(err)
	}
	storedCommand, err = repository.GetCommand(ctx, command.Scope, command.IdempotencyKey)
	if err != nil || storedCommand.State != CommandCompleted || storedCommand.Product != ProductPresent {
		t.Fatalf("completed command = %+v, %v", storedCommand, err)
	}
	storedProduct, err := repository.GetProductState(ctx, key, EnvironmentEVTE, product.ScopeFingerprint,
		product.ExpectedProductIdentifier, product.ExpectedServiceID)
	if err != nil || storedProduct != product {
		t.Fatalf("committed Product state = %+v, %v", storedProduct, err)
	}
}

func prepareTestCommandMutation(t *testing.T, repository *SQLCipherRepository, mutation Mutation) CommandRecord {
	t.Helper()
	scope := mutation.Key
	scope.CredentialFingerprint = [sha256.Size]byte{}
	command := CommandRecord{OperationID: mutation.OperationID, ActorUserID: serviceUserID, Scope: scope,
		IdempotencyKey: mutation.OperationID, SemanticHash: mutation.MetadataHash, Kind: mutation.Kind,
		State: CommandPrepared, CreatedAt: mutation.CreatedAt, UpdatedAt: mutation.UpdatedAt}
	if err := repository.PrepareCommandMutation(context.Background(), command, mutation); err != nil {
		t.Fatal(err)
	}
	return command
}

func testMutationCommit(mutation Mutation, command CommandRecord) MutationCommit {
	commit := MutationCommit{Command: &CommandCompletion{Scope: command.Scope, UpdatedAt: testTime},
		CompletionAudit: AuditRecord{Action: AuditProductIDChanged, StatusCode: "TEST_MUTATION_COMPLETED"},
		Decision:        func(context.Context, MutationEffectExecutor) error { return nil }}
	switch mutation.Kind {
	case MutationImportCredential:
		commit.NewBinding = testBinding(mutation.Key, "simulator-import")
		commit.CompletionAudit.Action = AuditCredentialImported
	case MutationReplaceCredential:
		replacement := withFingerprint(mutation.Key, digest(0x92))
		commit.NewBinding = testBinding(replacement, "simulator-replace")
		commit.CompletionAudit.Action = AuditCredentialReplaced
	case MutationRemoveCredential:
		commit.CompletionAudit.Action = AuditCredentialRemoved
	case MutationImportProductID:
		commit.Product = &ProductRecord{Key: mutation.Key, Environment: EnvironmentEVTE,
			ScopeFingerprint: authenticatedProductScopeFingerprint("TEST.PRODUCT", "TEST.SERVICE"), ExpectedProductIdentifier: "TEST.PRODUCT", ExpectedServiceID: "TEST.SERVICE",
			State: ProductPresent, ProductFingerprint: digest(0xf2), Revision: 1, UpdatedAt: testTime}
		commit.Command.Product = ProductPresent
	case MutationRemoveProductID:
		commit.Product = &ProductRecord{Key: mutation.Key, Environment: EnvironmentEVTE,
			ScopeFingerprint: authenticatedProductScopeFingerprint("TEST.PRODUCT", "TEST.SERVICE"), ExpectedProductIdentifier: "TEST.PRODUCT", ExpectedServiceID: "TEST.SERVICE",
			State: ProductMissing, Revision: 1, UpdatedAt: testTime}
		commit.Command.Product = ProductMissing
	}
	return commit
}

func digest(seed byte) [sha256.Size]byte                    { return sha256.Sum256([]byte{seed}) }
func digestPtr(seed byte) *[sha256.Size]byte                { value := digest(seed); return &value }
func withWorkspace(key BindingKey, value string) BindingKey { key.WorkspaceID = value; return key }
func withOrganisation(key BindingKey, value string) BindingKey {
	key.OrganisationID = value
	return key
}
func withABN(key BindingKey, value string) BindingKey { key.CanonicalABN = value; return key }
func withFingerprint(key BindingKey, value [sha256.Size]byte) BindingKey {
	key.CredentialFingerprint = value
	return key
}

func operationFor(kind MutationKind, crash string) string {
	sum := sha256.Sum256([]byte(string(kind) + "/" + crash))
	return "018f0000-0000-7000-8" + hexDigits(sum[:11])[:3] + "-" + hexDigits(sum[11:17])[:12]
}

func hexDigits(value []byte) string {
	const digits = "0123456789abcdef"
	result := make([]byte, len(value)*2)
	for index, item := range value {
		result[index*2] = digits[item>>4]
		result[index*2+1] = digits[item&15]
	}
	return string(result)
}

func assertSchemaHasNoSecretColumns(t *testing.T, database *sqlcipher.Database) {
	t.Helper()
	rows, err := database.Query(`SELECT name, sql FROM sqlite_schema WHERE type='table' AND name LIKE 'sbr_%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	seen := map[string]bool{}
	for rows.Next() {
		var name, definition string
		if err := rows.Scan(&name, &definition); err != nil {
			t.Fatal(err)
		}
		count++
		seen[name] = true
		lower := bytes.ToLower([]byte(definition))
		for _, forbidden := range [][]byte{[]byte("password"), []byte("product_id_value"), []byte("selected_local_path"),
			[]byte("bookmark"), []byte("endpoint_url"), []byte("private_key"), []byte("credential_bytes")} {
			if bytes.Contains(lower, forbidden) {
				t.Errorf("table %s contains forbidden column fragment %q", name, forbidden)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 11 {
		t.Fatalf("SBR table count = %d, want 11", count)
	}
	if !seen["sbr_helper_dispatches_v1"] {
		t.Fatal("schema is missing the durable helper dispatch table")
	}
	if !seen["sbr_pending_mutation_effects_v1"] {
		t.Fatal("schema is missing durable pending mutation effects")
	}
}
