//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

// Package sbr owns durable, redacted SBR readiness state. Vault records and
// transient credential inputs never cross this repository boundary.
package sbr

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/platform/abn"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
)

var (
	ErrNotFound            = errors.New("sbr: binding not found")
	ErrPermissionDenied    = errors.New("sbr: binding permission denied")
	ErrConflict            = errors.New("sbr: durable state conflict")
	ErrInvalidTransition   = errors.New("sbr: invalid state transition")
	ErrIdempotencyConflict = errors.New("sbr: idempotency conflict")
)

type BindingState string

const (
	BindingActive           BindingState = "ACTIVE"
	BindingReimportRequired BindingState = "REIMPORT_REQUIRED"
	BindingRemoved          BindingState = "REMOVED"
)

type BindingKey struct {
	WorkspaceID           string
	OrganisationID        string
	CanonicalABN          string
	SchemaVersion         uint32
	CredentialFingerprint [sha256.Size]byte
}

type Binding struct {
	Key              BindingKey
	ComponentVersion string
	SubjectHash      [sha256.Size]byte
	ExpiresAt        string
	State            BindingState
	Revision         uint64
	UpdatedAt        string
}

type Environment string

const (
	EnvironmentSimulator Environment = "SIMULATOR"
	EnvironmentEVTE      Environment = "EVTE"
)

type AuthenticatedProfile struct {
	Key                     BindingKey
	Environment             Environment
	ProfileFingerprint      [sha256.Size]byte
	RegistrationFingerprint [sha256.Size]byte
	ComponentFingerprint    [sha256.Size]byte
	Conformance             Conformance
	EvidenceSequence        uint64
	AuthenticatedAt         string
}

type Conformance string

const (
	ConformanceSimulator Conformance = "SIMULATOR"
	ConformancePre       Conformance = "PRE_CONFORMANCE"
	ConformancePost      Conformance = "POST_CONFORMANCE"
)

type ReadinessState string

const (
	ReadinessUnavailable                 ReadinessState = "UNAVAILABLE"
	ReadinessReadyForSimulator           ReadinessState = "READY_FOR_SIMULATOR"
	ReadinessReadyForEVTEPreConformance  ReadinessState = "READY_FOR_EVTE_PRE_CONFORMANCE"
	ReadinessReadyForEVTEPostConformance ReadinessState = "READY_FOR_EVTE_POST_CONFORMANCE"
)

type ReadinessTransition struct {
	TransitionID string
	Key          BindingKey
	State        ReadinessState
	ReasonCode   string
	Sequence     uint64
	OccurredAt   string
}

type MutationKind string

const (
	MutationImportCredential  MutationKind = "IMPORT_CREDENTIAL"
	MutationReplaceCredential MutationKind = "REPLACE_CREDENTIAL"
	MutationRemoveCredential  MutationKind = "REMOVE_CREDENTIAL"
	MutationImportProductID   MutationKind = "IMPORT_PRODUCT_ID"
	MutationRemoveProductID   MutationKind = "REMOVE_PRODUCT_ID"
)

type MutationState string

const (
	MutationPrepared          MutationState = "PREPARED"
	MutationStaged            MutationState = "STAGED"
	MutationCoreCommitted     MutationState = "CORE_COMMITTED"
	MutationHelperCommitted   MutationState = "HELPER_COMMITTED"
	MutationAbortRequired     MutationState = "ABORT_REQUIRED"
	MutationAborting          MutationState = "ABORTING"
	MutationAborted           MutationState = "ABORTED"
	MutationReconcileRequired MutationState = "RECONCILE_REQUIRED"
)

type Mutation struct {
	OperationID  string
	Key          BindingKey
	Kind         MutationKind
	State        MutationState
	PendingID    string
	MetadataHash [sha256.Size]byte
	CreatedAt    string
	UpdatedAt    string
}

// MutationEffectExecutor is the SQL authority available to core-owned audit
// effects. It deliberately exposes no transaction lifecycle operation.
type MutationEffectExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type MutationCommit struct {
	NewBinding *Binding
	Profile    *AuthenticatedProfile
	Readiness  *ReadinessTransition
	Audit      func(context.Context, MutationEffectExecutor) error
}

type mutationEffectExecutor struct{ transaction *sqlcipher.Transaction }

func (executor mutationEffectExecutor) ExecContext(ctx context.Context, query string, arguments ...any) (sql.Result, error) {
	return executor.transaction.ExecContext(ctx, query, arguments...)
}

func (executor mutationEffectExecutor) QueryContext(ctx context.Context, query string, arguments ...any) (*sql.Rows, error) {
	return executor.transaction.QueryContext(ctx, query, arguments...)
}

type ReconcileAction string

const (
	ReconcileNone   ReconcileAction = "NONE"
	ReconcileAbort  ReconcileAction = "ABORT"
	ReconcileCommit ReconcileAction = "COMMIT"
)

type TransportState string

const (
	TransportPrepared         TransportState = "PREPARED"
	TransportDispatching      TransportState = "DISPATCHING"
	TransportNotStarted       TransportState = "NOT_STARTED"
	TransportMaybeSent        TransportState = "MAYBE_SENT"
	TransportResponseReceived TransportState = "RESPONSE_RECEIVED"
	TransportAccepted         TransportState = "ACCEPTED"
	TransportFailed           TransportState = "FAILED"
	TransportUnknown          TransportState = "UNKNOWN"
)

type SimulatorTransport struct {
	OperationID       string
	Key               BindingKey
	IdempotencyKey    string
	SemanticHash      [sha256.Size]byte
	ResultHash        *[sha256.Size]byte
	State             TransportState
	CreatedAt         string
	UpdatedAt         string
	pendingTerminal   TransportState
	pendingResultHash *[sha256.Size]byte
}

type SimulatorCase string

const (
	SimulatorCasePreDispatchFailure SimulatorCase = "PRE_DISPATCH_FAILURE"
	SimulatorCaseUncertainWrite     SimulatorCase = "UNCERTAIN_WRITE"
	SimulatorCaseHelperDeath        SimulatorCase = "HELPER_DEATH"
	SimulatorCaseTimeout            SimulatorCase = "TIMEOUT"
	SimulatorCaseSyntacticResponse  SimulatorCase = "SYNTACTIC_RESPONSE"
	SimulatorCaseMalformedResponse  SimulatorCase = "MALFORMED_RESPONSE"
	SimulatorCaseAccepted           SimulatorCase = "ACCEPTED"
)

type repositoryHooks struct{ afterResponseReceived func() error }

type SQLCipherRepository struct {
	database *sqlcipher.Database
	now      func() time.Time
	hooks    *repositoryHooks
}

func NewSQLCipherRepository(database *sqlcipher.Database) (*SQLCipherRepository, error) {
	return newSQLCipherRepository(database, time.Now)
}

func newSQLCipherRepository(database *sqlcipher.Database, now func() time.Time) (*SQLCipherRepository, error) {
	if database == nil || database.DB == nil {
		return nil, ErrRepository
	}
	if now == nil {
		return nil, ErrRepository
	}
	return &SQLCipherRepository{database: database, now: now}, nil
}

func (repository *SQLCipherRepository) PutBinding(ctx context.Context, binding Binding) error {
	if !repository.valid(ctx) || !validBinding(binding) {
		return ErrInvalid
	}
	_, err := repository.database.ExecContext(ctx, `INSERT INTO sbr_credential_bindings_v1(
workspace_id,organisation_id,canonical_abn,schema_version,credential_fingerprint,component_version,
subject_hash,expires_at,binding_state,revision,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		binding.Key.WorkspaceID, binding.Key.OrganisationID, binding.Key.CanonicalABN, binding.Key.SchemaVersion,
		binding.Key.CredentialFingerprint[:], binding.ComponentVersion, binding.SubjectHash[:], binding.ExpiresAt,
		binding.State, binding.Revision, binding.UpdatedAt)
	if err != nil {
		return ErrConflict
	}
	return nil
}

func (repository *SQLCipherRepository) GetBinding(ctx context.Context, key BindingKey) (Binding, error) {
	if !repository.valid(ctx) || !validBindingKey(key) {
		return Binding{}, ErrInvalid
	}
	var binding Binding
	binding.Key = key
	var fingerprint, subject []byte
	err := repository.database.QueryRowContext(ctx, `SELECT credential_fingerprint,component_version,subject_hash,
expires_at,binding_state,revision,updated_at FROM sbr_credential_bindings_v1
WHERE workspace_id=? AND organisation_id=? AND canonical_abn=? AND schema_version=? AND credential_fingerprint=?`,
		key.WorkspaceID, key.OrganisationID, key.CanonicalABN, key.SchemaVersion, key.CredentialFingerprint[:]).Scan(
		&fingerprint, &binding.ComponentVersion, &subject, &binding.ExpiresAt, &binding.State, &binding.Revision, &binding.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Binding{}, ErrNotFound
	}
	if err != nil || len(fingerprint) != sha256.Size || len(subject) != sha256.Size ||
		!bytes.Equal(fingerprint, key.CredentialFingerprint[:]) {
		return Binding{}, ErrRepository
	}
	copy(binding.SubjectHash[:], subject)
	return binding, nil
}

func (repository *SQLCipherRepository) TransitionBinding(ctx context.Context, key BindingKey, next BindingState, updatedAt string) error {
	if !repository.valid(ctx) || !validBindingKey(key) || !validTimestamp(updatedAt) || !validBindingTransitionTarget(next) {
		return ErrInvalid
	}
	current, err := repository.GetBinding(ctx, key)
	if err != nil {
		return err
	}
	if !bindingTransitionAllowed(current.State, next) {
		return ErrInvalidTransition
	}
	result, err := repository.database.ExecContext(ctx, `UPDATE sbr_credential_bindings_v1
SET binding_state=?,revision=revision+1,updated_at=?
WHERE workspace_id=? AND organisation_id=? AND canonical_abn=? AND schema_version=? AND credential_fingerprint=?
AND binding_state=? AND revision=?`, next, updatedAt, key.WorkspaceID, key.OrganisationID, key.CanonicalABN,
		key.SchemaVersion, key.CredentialFingerprint[:], current.State, current.Revision)
	if err != nil {
		return ErrRepository
	}
	if !exactlyOne(result) {
		return ErrConflict
	}
	return nil
}

func (repository *SQLCipherRepository) PutAuthenticatedProfile(ctx context.Context, profile AuthenticatedProfile) error {
	if !repository.valid(ctx) || !validBindingKey(profile.Key) ||
		(profile.Environment != EnvironmentSimulator && profile.Environment != EnvironmentEVTE) ||
		!validProfileConformance(profile.Environment, profile.Conformance) || zeroHash(profile.ProfileFingerprint) ||
		zeroHash(profile.RegistrationFingerprint) || zeroHash(profile.ComponentFingerprint) {
		return ErrInvalid
	}
	if profile.Environment == EnvironmentSimulator && profile.Conformance == "" {
		profile.Conformance = ConformanceSimulator
	}
	tx, err := repository.database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		return ErrRepository
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	var sequence uint64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(evidence_sequence),0)+1 FROM sbr_authenticated_profiles_v1
WHERE workspace_id=? AND organisation_id=? AND canonical_abn=? AND schema_version=? AND credential_fingerprint=? AND environment=?`,
		profile.Key.WorkspaceID, profile.Key.OrganisationID, profile.Key.CanonicalABN, profile.Key.SchemaVersion,
		profile.Key.CredentialFingerprint[:], profile.Environment).Scan(&sequence); err != nil || sequence == 0 {
		return ErrRepository
	}
	authenticatedAt, err := repository.timestamp()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO sbr_authenticated_profiles_v1(
workspace_id,organisation_id,canonical_abn,schema_version,credential_fingerprint,environment,profile_fingerprint,
registration_fingerprint,component_fingerprint,conformance,evidence_sequence,authenticated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		profile.Key.WorkspaceID, profile.Key.OrganisationID, profile.Key.CanonicalABN, profile.Key.SchemaVersion,
		profile.Key.CredentialFingerprint[:], profile.Environment, profile.ProfileFingerprint[:],
		profile.RegistrationFingerprint[:], profile.ComponentFingerprint[:], profile.Conformance, sequence, authenticatedAt)
	if err != nil {
		return ErrConflict
	}
	if err := tx.Commit(); err != nil {
		return ErrRepository
	}
	committed = true
	return nil
}

func (repository *SQLCipherRepository) GetAuthenticatedProfile(ctx context.Context, key BindingKey, environment Environment) (AuthenticatedProfile, error) {
	if !repository.valid(ctx) || !validBindingKey(key) || (environment != EnvironmentSimulator && environment != EnvironmentEVTE) {
		return AuthenticatedProfile{}, ErrInvalid
	}
	profile := AuthenticatedProfile{Key: key, Environment: environment}
	var credential, profileHash, registrationHash, componentHash []byte
	err := repository.database.QueryRowContext(ctx, `SELECT credential_fingerprint,profile_fingerprint,registration_fingerprint,
component_fingerprint,conformance,evidence_sequence,authenticated_at FROM sbr_authenticated_profiles_v1 WHERE workspace_id=? AND organisation_id=?
AND canonical_abn=? AND schema_version=? AND credential_fingerprint=? AND environment=?
ORDER BY evidence_sequence DESC LIMIT 1`, key.WorkspaceID,
		key.OrganisationID, key.CanonicalABN, key.SchemaVersion, key.CredentialFingerprint[:], environment).Scan(
		&credential, &profileHash, &registrationHash, &componentHash, &profile.Conformance, &profile.EvidenceSequence, &profile.AuthenticatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return AuthenticatedProfile{}, ErrNotFound
	}
	if err != nil || len(credential) != sha256.Size || len(profileHash) != sha256.Size ||
		len(registrationHash) != sha256.Size || len(componentHash) != sha256.Size {
		return AuthenticatedProfile{}, ErrRepository
	}
	copy(profile.ProfileFingerprint[:], profileHash)
	copy(profile.RegistrationFingerprint[:], registrationHash)
	copy(profile.ComponentFingerprint[:], componentHash)
	return profile, nil
}

func (repository *SQLCipherRepository) AppendReadinessTransition(ctx context.Context, transition ReadinessTransition) error {
	if !repository.valid(ctx) || !ids.IsCanonicalV7(transition.TransitionID) || !validBindingKey(transition.Key) ||
		!validReadiness(transition.State) || len(transition.ReasonCode) < 1 || len(transition.ReasonCode) > 128 ||
		strings.IndexByte(transition.ReasonCode, 0) >= 0 {
		return ErrInvalid
	}
	tx, err := repository.database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		return ErrRepository
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	var sequence uint64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence),0)+1 FROM sbr_readiness_transitions_v1
WHERE workspace_id=? AND organisation_id=? AND canonical_abn=? AND schema_version=? AND credential_fingerprint=?`,
		transition.Key.WorkspaceID, transition.Key.OrganisationID, transition.Key.CanonicalABN, transition.Key.SchemaVersion,
		transition.Key.CredentialFingerprint[:]).Scan(&sequence); err != nil || sequence == 0 {
		return ErrRepository
	}
	occurredAt, err := repository.timestamp()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO sbr_readiness_transitions_v1(transition_id,workspace_id,
organisation_id,canonical_abn,schema_version,credential_fingerprint,readiness_state,reason_code,sequence,occurred_at)
VALUES (?,?,?,?,?,?,?,?,?,?)`, transition.TransitionID, transition.Key.WorkspaceID, transition.Key.OrganisationID,
		transition.Key.CanonicalABN, transition.Key.SchemaVersion, transition.Key.CredentialFingerprint[:],
		transition.State, transition.ReasonCode, sequence, occurredAt)
	if err != nil {
		return ErrConflict
	}
	if err := tx.Commit(); err != nil {
		return ErrRepository
	}
	committed = true
	return nil
}

func (repository *SQLCipherRepository) LatestReadiness(ctx context.Context, key BindingKey) (ReadinessTransition, error) {
	if !repository.valid(ctx) || !validBindingKey(key) {
		return ReadinessTransition{}, ErrInvalid
	}
	transition := ReadinessTransition{Key: key}
	var fingerprint []byte
	err := repository.database.QueryRowContext(ctx, `SELECT transition_id,credential_fingerprint,readiness_state,
reason_code,sequence,occurred_at FROM sbr_readiness_transitions_v1 WHERE workspace_id=? AND organisation_id=? AND canonical_abn=?
AND schema_version=? AND credential_fingerprint=? ORDER BY sequence DESC LIMIT 1`,
		key.WorkspaceID, key.OrganisationID, key.CanonicalABN, key.SchemaVersion, key.CredentialFingerprint[:]).Scan(
		&transition.TransitionID, &fingerprint, &transition.State, &transition.ReasonCode, &transition.Sequence, &transition.OccurredAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ReadinessTransition{}, ErrNotFound
	}
	if err != nil || len(fingerprint) != sha256.Size || !bytes.Equal(fingerprint, key.CredentialFingerprint[:]) {
		return ReadinessTransition{}, ErrRepository
	}
	return transition, nil
}

func (repository *SQLCipherRepository) PrepareMutation(ctx context.Context, mutation Mutation) error {
	if !repository.valid(ctx) || !validMutation(mutation) || mutation.State != MutationPrepared || mutation.PendingID != "" {
		return ErrInvalid
	}
	var fingerprint any
	if mutation.Kind == MutationImportCredential {
		fingerprint = nil
	} else if _, err := repository.GetBinding(ctx, mutation.Key); err != nil {
		return err
	} else {
		fingerprint = mutation.Key.CredentialFingerprint[:]
	}
	_, err := repository.database.ExecContext(ctx, `INSERT INTO sbr_mutations_v1(
operation_id,workspace_id,organisation_id,canonical_abn,schema_version,credential_fingerprint,mutation_kind,
mutation_state,pending_id,metadata_hash,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,NULL,?,?,?)`,
		mutation.OperationID, mutation.Key.WorkspaceID, mutation.Key.OrganisationID, mutation.Key.CanonicalABN,
		mutation.Key.SchemaVersion, fingerprint, mutation.Kind, mutation.State,
		mutation.MetadataHash[:], mutation.CreatedAt, mutation.UpdatedAt)
	if err != nil {
		return ErrConflict
	}
	return nil
}

func (repository *SQLCipherRepository) MarkImportMutationStaged(ctx context.Context, scope BindingKey, operationID,
	pendingID string, actualFingerprint [sha256.Size]byte, updatedAt string,
) error {
	if !repository.valid(ctx) || !validBindingScope(scope) || !zeroHash(scope.CredentialFingerprint) ||
		zeroHash(actualFingerprint) || !ids.IsCanonicalV7(operationID) || !ids.IsCanonicalV7(pendingID) || !validTimestamp(updatedAt) {
		return ErrInvalid
	}
	result, err := repository.database.ExecContext(ctx, `UPDATE sbr_mutations_v1 SET mutation_state=?,pending_id=?,
credential_fingerprint=?,updated_at=? WHERE operation_id=? AND workspace_id=? AND organisation_id=? AND canonical_abn=?
AND schema_version=? AND credential_fingerprint IS NULL AND mutation_kind=? AND mutation_state=?`, MutationStaged,
		pendingID, actualFingerprint[:], updatedAt, operationID, scope.WorkspaceID, scope.OrganisationID, scope.CanonicalABN,
		scope.SchemaVersion, MutationImportCredential, MutationPrepared)
	if err != nil {
		return ErrRepository
	}
	if !exactlyOne(result) {
		if _, lookupErr := repository.GetMutation(ctx, scope, operationID); errors.Is(lookupErr, ErrNotFound) {
			return ErrNotFound
		}
		return ErrInvalidTransition
	}
	return nil
}

func (repository *SQLCipherRepository) MarkMutationStaged(ctx context.Context, key BindingKey, operationID, pendingID, updatedAt string) error {
	if !validOperationInput(repository, ctx, key, operationID, updatedAt) || !ids.IsCanonicalV7(pendingID) {
		return ErrInvalid
	}
	return repository.transitionMutation(ctx, key, operationID, MutationPrepared, MutationStaged, pendingID, updatedAt)
}

// CommitMutation applies the core-owned binding/evidence/audit effects and the
// durable CORE_COMMITTED decision in one SQLCipher transaction. Helper commit
// remains a later acknowledged step.
func (repository *SQLCipherRepository) CommitMutation(ctx context.Context, key BindingKey, operationID string, commit MutationCommit) error {
	if !repository.valid(ctx) || !validBindingScope(key) || !ids.IsCanonicalV7(operationID) || commit.Audit == nil {
		return ErrInvalid
	}
	mutation, err := repository.GetMutation(ctx, key, operationID)
	if err != nil {
		return err
	}
	if mutation.State != MutationStaged {
		return ErrInvalidTransition
	}
	tx, err := repository.database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		return ErrRepository
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	updatedAt := mutation.UpdatedAt
	effectKey := mutation.Key
	switch mutation.Kind {
	case MutationImportCredential:
		if commit.NewBinding == nil || !validBinding(*commit.NewBinding) || !sameScope(commit.NewBinding.Key, mutation.Key) ||
			commit.NewBinding.Key.CredentialFingerprint != mutation.Key.CredentialFingerprint {
			return ErrInvalid
		}
		if err := insertBindingWithin(ctx, tx, *commit.NewBinding); err != nil {
			return err
		}
		effectKey = commit.NewBinding.Key
	case MutationReplaceCredential:
		if commit.NewBinding == nil || commit.NewBinding.Key.CredentialFingerprint == mutation.Key.CredentialFingerprint ||
			!validBinding(*commit.NewBinding) || !sameScope(commit.NewBinding.Key, mutation.Key) {
			return ErrInvalid
		}
		if err := transitionBindingWithin(ctx, tx, mutation.Key, BindingActive, BindingRemoved, updatedAt); err != nil {
			return err
		}
		if err := insertBindingWithin(ctx, tx, *commit.NewBinding); err != nil {
			return err
		}
		effectKey = commit.NewBinding.Key
	case MutationRemoveCredential:
		if commit.NewBinding != nil || commit.Profile != nil {
			return ErrInvalid
		}
		if err := transitionBindingWithin(ctx, tx, mutation.Key, BindingActive, BindingRemoved, updatedAt); err != nil {
			return err
		}
	case MutationImportProductID, MutationRemoveProductID:
		if commit.NewBinding != nil || commit.Profile != nil {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	if commit.Profile != nil && commit.Profile.Key != effectKey ||
		commit.Readiness != nil && commit.Readiness.Key != effectKey {
		return ErrInvalid
	}
	if commit.Profile != nil {
		if err := repository.insertProfileEvidenceWithin(ctx, tx, *commit.Profile); err != nil {
			return err
		}
	}
	if commit.Readiness != nil {
		if err := repository.insertReadinessWithin(ctx, tx, *commit.Readiness); err != nil {
			return err
		}
	}
	if err := commit.Audit(ctx, mutationEffectExecutor{transaction: tx}); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE sbr_mutations_v1 SET mutation_state=?,updated_at=?
WHERE operation_id=? AND workspace_id=? AND organisation_id=? AND canonical_abn=? AND schema_version=?
AND credential_fingerprint=? AND mutation_state=?`, MutationCoreCommitted, updatedAt, operationID, key.WorkspaceID,
		mutation.Key.OrganisationID, mutation.Key.CanonicalABN, mutation.Key.SchemaVersion, mutation.Key.CredentialFingerprint[:], MutationStaged)
	if err != nil || !exactlyOne(result) {
		return ErrConflict
	}
	if err := tx.Commit(); err != nil {
		return ErrRepository
	}
	committed = true
	return nil
}

func (repository *SQLCipherRepository) insertProfileEvidenceWithin(ctx context.Context, tx *sqlcipher.Transaction,
	profile AuthenticatedProfile,
) error {
	if !validBindingKey(profile.Key) || !validProfileConformance(profile.Environment, profile.Conformance) ||
		zeroHash(profile.ProfileFingerprint) || zeroHash(profile.RegistrationFingerprint) || zeroHash(profile.ComponentFingerprint) {
		return ErrInvalid
	}
	if profile.Environment == EnvironmentSimulator && profile.Conformance == "" {
		profile.Conformance = ConformanceSimulator
	}
	var sequence uint64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(evidence_sequence),0)+1 FROM sbr_authenticated_profiles_v1
WHERE workspace_id=? AND organisation_id=? AND canonical_abn=? AND schema_version=? AND credential_fingerprint=? AND environment=?`,
		profile.Key.WorkspaceID, profile.Key.OrganisationID, profile.Key.CanonicalABN, profile.Key.SchemaVersion,
		profile.Key.CredentialFingerprint[:], profile.Environment).Scan(&sequence); err != nil || sequence == 0 {
		return ErrRepository
	}
	authenticatedAt, err := repository.timestamp()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO sbr_authenticated_profiles_v1(workspace_id,organisation_id,canonical_abn,
schema_version,credential_fingerprint,environment,profile_fingerprint,registration_fingerprint,component_fingerprint,
conformance,evidence_sequence,authenticated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`, profile.Key.WorkspaceID,
		profile.Key.OrganisationID, profile.Key.CanonicalABN, profile.Key.SchemaVersion, profile.Key.CredentialFingerprint[:],
		profile.Environment, profile.ProfileFingerprint[:], profile.RegistrationFingerprint[:], profile.ComponentFingerprint[:],
		profile.Conformance, sequence, authenticatedAt)
	if err != nil {
		return ErrConflict
	}
	return nil
}

func (repository *SQLCipherRepository) insertReadinessWithin(ctx context.Context, tx *sqlcipher.Transaction,
	transition ReadinessTransition,
) error {
	if !ids.IsCanonicalV7(transition.TransitionID) || !validBindingKey(transition.Key) || !validReadiness(transition.State) ||
		len(transition.ReasonCode) < 1 || len(transition.ReasonCode) > 128 || strings.IndexByte(transition.ReasonCode, 0) >= 0 {
		return ErrInvalid
	}
	var sequence uint64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence),0)+1 FROM sbr_readiness_transitions_v1
WHERE workspace_id=? AND organisation_id=? AND canonical_abn=? AND schema_version=? AND credential_fingerprint=?`,
		transition.Key.WorkspaceID, transition.Key.OrganisationID, transition.Key.CanonicalABN, transition.Key.SchemaVersion,
		transition.Key.CredentialFingerprint[:]).Scan(&sequence); err != nil || sequence == 0 {
		return ErrRepository
	}
	occurredAt, err := repository.timestamp()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO sbr_readiness_transitions_v1(transition_id,workspace_id,organisation_id,
canonical_abn,schema_version,credential_fingerprint,readiness_state,reason_code,sequence,occurred_at)
VALUES (?,?,?,?,?,?,?,?,?,?)`, transition.TransitionID, transition.Key.WorkspaceID, transition.Key.OrganisationID,
		transition.Key.CanonicalABN, transition.Key.SchemaVersion, transition.Key.CredentialFingerprint[:], transition.State,
		transition.ReasonCode, sequence, occurredAt)
	if err != nil {
		return ErrConflict
	}
	return nil
}

func insertBindingWithin(ctx context.Context, tx *sqlcipher.Transaction, binding Binding) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO sbr_credential_bindings_v1(
workspace_id,organisation_id,canonical_abn,schema_version,credential_fingerprint,component_version,
subject_hash,expires_at,binding_state,revision,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		binding.Key.WorkspaceID, binding.Key.OrganisationID, binding.Key.CanonicalABN, binding.Key.SchemaVersion,
		binding.Key.CredentialFingerprint[:], binding.ComponentVersion, binding.SubjectHash[:], binding.ExpiresAt,
		binding.State, binding.Revision, binding.UpdatedAt)
	if err != nil {
		return ErrConflict
	}
	return nil
}

func transitionBindingWithin(ctx context.Context, tx *sqlcipher.Transaction, key BindingKey, from, to BindingState, updatedAt string) error {
	result, err := tx.ExecContext(ctx, `UPDATE sbr_credential_bindings_v1 SET binding_state=?,revision=revision+1,updated_at=?
WHERE workspace_id=? AND organisation_id=? AND canonical_abn=? AND schema_version=? AND credential_fingerprint=? AND binding_state=?`,
		to, updatedAt, key.WorkspaceID, key.OrganisationID, key.CanonicalABN, key.SchemaVersion,
		key.CredentialFingerprint[:], from)
	if err != nil || !exactlyOne(result) {
		return ErrConflict
	}
	return nil
}

func (repository *SQLCipherRepository) MarkMutationHelperCommitted(ctx context.Context, key BindingKey, operationID, updatedAt string) error {
	if !validOperationInput(repository, ctx, key, operationID, updatedAt) {
		return ErrInvalid
	}
	result, err := repository.database.ExecContext(ctx, `UPDATE sbr_mutations_v1 SET mutation_state=?,pending_id=NULL,updated_at=?
WHERE operation_id=? AND workspace_id=? AND organisation_id=? AND canonical_abn=? AND schema_version=?
AND credential_fingerprint=? AND mutation_state IN (?,?)`, MutationHelperCommitted, updatedAt, operationID,
		key.WorkspaceID, key.OrganisationID, key.CanonicalABN, key.SchemaVersion, key.CredentialFingerprint[:],
		MutationCoreCommitted, MutationReconcileRequired)
	if err != nil {
		return ErrRepository
	}
	if !exactlyOne(result) {
		return mutationMissingOrTransition(repository, ctx, key, operationID)
	}
	return nil
}

func (repository *SQLCipherRepository) AbortMutation(ctx context.Context, key BindingKey, operationID, updatedAt string) error {
	if !validOperationInput(repository, ctx, key, operationID, updatedAt) {
		return ErrInvalid
	}
	mutation, err := repository.GetMutation(ctx, key, operationID)
	if err != nil {
		return err
	}
	next := MutationAborted
	clearPending := ",pending_id=NULL"
	if mutation.State == MutationStaged {
		next = MutationAbortRequired
		clearPending = ""
	} else if mutation.State != MutationPrepared {
		return ErrInvalidTransition
	}
	result, err := repository.database.ExecContext(ctx, `UPDATE sbr_mutations_v1 SET mutation_state=?`+clearPending+`,updated_at=?
WHERE operation_id=? AND workspace_id=? AND organisation_id=? AND canonical_abn=? AND schema_version=?
	AND mutation_state=?`, next, updatedAt, operationID,
		mutation.Key.WorkspaceID, mutation.Key.OrganisationID, mutation.Key.CanonicalABN, mutation.Key.SchemaVersion,
		mutation.State)
	if err != nil {
		return ErrRepository
	}
	if !exactlyOne(result) {
		return mutationMissingOrTransition(repository, ctx, key, operationID)
	}
	return nil
}

func (repository *SQLCipherRepository) MarkMutationAbortDispatched(ctx context.Context, key BindingKey, operationID, updatedAt string) error {
	if !validOperationInput(repository, ctx, key, operationID, updatedAt) {
		return ErrInvalid
	}
	return repository.transitionMutation(ctx, key, operationID, MutationAbortRequired, MutationAborting, "", updatedAt)
}

func (repository *SQLCipherRepository) AcknowledgeMutationAbort(ctx context.Context, key BindingKey, operationID, updatedAt string) error {
	if !validOperationInput(repository, ctx, key, operationID, updatedAt) {
		return ErrInvalid
	}
	result, err := repository.database.ExecContext(ctx, `UPDATE sbr_mutations_v1 SET mutation_state=?,pending_id=NULL,updated_at=?
WHERE operation_id=? AND workspace_id=? AND organisation_id=? AND canonical_abn=? AND schema_version=?
AND credential_fingerprint=? AND mutation_state IN (?,?)`, MutationAborted, updatedAt, operationID, key.WorkspaceID,
		key.OrganisationID, key.CanonicalABN, key.SchemaVersion, key.CredentialFingerprint[:], MutationAbortRequired, MutationAborting)
	if err != nil {
		return ErrRepository
	}
	if !exactlyOne(result) {
		return mutationMissingOrTransition(repository, ctx, key, operationID)
	}
	return nil
}

func (repository *SQLCipherRepository) GetMutation(ctx context.Context, key BindingKey, operationID string) (Mutation, error) {
	if !repository.valid(ctx) || !validBindingScope(key) || !ids.IsCanonicalV7(operationID) {
		return Mutation{}, ErrInvalid
	}
	mutation := Mutation{OperationID: operationID, Key: key}
	var fingerprint, metadata []byte
	var pending sql.NullString
	err := repository.database.QueryRowContext(ctx, `SELECT credential_fingerprint,mutation_kind,mutation_state,pending_id,
metadata_hash,created_at,updated_at FROM sbr_mutations_v1 WHERE operation_id=? AND workspace_id=? AND organisation_id=?
AND canonical_abn=? AND schema_version=?`, operationID, key.WorkspaceID,
		key.OrganisationID, key.CanonicalABN, key.SchemaVersion).Scan(&fingerprint,
		&mutation.Kind, &mutation.State, &pending, &metadata, &mutation.CreatedAt, &mutation.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Mutation{}, ErrNotFound
	}
	if err != nil || (len(fingerprint) != 0 && len(fingerprint) != sha256.Size) || len(metadata) != sha256.Size ||
		(!zeroHash(key.CredentialFingerprint) && !bytes.Equal(fingerprint, key.CredentialFingerprint[:])) {
		return Mutation{}, ErrRepository
	}
	mutation.PendingID = pending.String
	if len(fingerprint) == sha256.Size {
		copy(mutation.Key.CredentialFingerprint[:], fingerprint)
	}
	copy(mutation.MetadataHash[:], metadata)
	return mutation, nil
}

func (repository *SQLCipherRepository) ReconcileMutation(ctx context.Context, key BindingKey, operationID, updatedAt string) (ReconcileAction, error) {
	mutation, err := repository.GetMutation(ctx, key, operationID)
	if err != nil {
		return ReconcileNone, err
	}
	switch mutation.State {
	case MutationPrepared, MutationStaged:
		if err := repository.AbortMutation(ctx, key, operationID, updatedAt); err != nil {
			return ReconcileNone, err
		}
		return ReconcileAbort, nil
	case MutationAbortRequired, MutationAborting:
		return ReconcileAbort, nil
	case MutationCoreCommitted:
		result, err := repository.database.ExecContext(ctx, `UPDATE sbr_mutations_v1 SET mutation_state=?,updated_at=?
WHERE operation_id=? AND workspace_id=? AND organisation_id=? AND canonical_abn=? AND schema_version=?
AND credential_fingerprint=? AND mutation_state=?`, MutationReconcileRequired, updatedAt, operationID,
			mutation.Key.WorkspaceID, mutation.Key.OrganisationID, mutation.Key.CanonicalABN, mutation.Key.SchemaVersion,
			mutation.Key.CredentialFingerprint[:], MutationCoreCommitted)
		if err != nil || !exactlyOne(result) {
			return ReconcileNone, ErrConflict
		}
		return ReconcileCommit, nil
	case MutationReconcileRequired:
		return ReconcileCommit, nil
	case MutationHelperCommitted, MutationAborted:
		return ReconcileNone, nil
	default:
		return ReconcileNone, ErrRepository
	}
}

func (repository *SQLCipherRepository) PrepareSimulatorTransport(ctx context.Context, transport SimulatorTransport) (SimulatorTransport, bool, error) {
	if !repository.valid(ctx) || !validTransport(transport) || transport.State != TransportPrepared || transport.ResultHash != nil {
		return SimulatorTransport{}, false, ErrInvalid
	}
	tx, err := repository.database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		return SimulatorTransport{}, false, ErrRepository
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	_, err = tx.ExecContext(ctx, `INSERT INTO sbr_idempotency_v1(idempotency_key,workspace_id,organisation_id,
canonical_abn,schema_version,credential_fingerprint,semantic_hash,result_hash,original_operation_id,created_at)
VALUES (?,?,?,?,?,?,?,NULL,?,?)`, transport.IdempotencyKey, transport.Key.WorkspaceID, transport.Key.OrganisationID,
		transport.Key.CanonicalABN, transport.Key.SchemaVersion, transport.Key.CredentialFingerprint[:],
		transport.SemanticHash[:], transport.OperationID, transport.CreatedAt)
	if err != nil {
		original, lookupErr := loadTransportByIdempotency(ctx, func(ctx context.Context, query string, arguments ...any) rowScanner {
			return tx.QueryRowContext(ctx, query, arguments...)
		}, transport.Key, transport.IdempotencyKey)
		if lookupErr != nil {
			return SimulatorTransport{}, false, ErrConflict
		}
		if original.SemanticHash != transport.SemanticHash {
			return SimulatorTransport{}, false, ErrIdempotencyConflict
		}
		if err := tx.Commit(); err != nil {
			return SimulatorTransport{}, false, ErrRepository
		}
		committed = true
		return original, true, nil
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO sbr_simulator_transports_v1(operation_id,workspace_id,organisation_id,
canonical_abn,schema_version,credential_fingerprint,idempotency_key,semantic_hash,result_hash,state,created_at,updated_at)
VALUES (?,?,?,?,?,?,?,?,NULL,?,?,?)`, transport.OperationID, transport.Key.WorkspaceID, transport.Key.OrganisationID,
		transport.Key.CanonicalABN, transport.Key.SchemaVersion, transport.Key.CredentialFingerprint[:],
		transport.IdempotencyKey, transport.SemanticHash[:], transport.State, transport.CreatedAt, transport.UpdatedAt)
	if err != nil || tx.Commit() != nil {
		return SimulatorTransport{}, false, ErrConflict
	}
	committed = true
	return cloneTransport(transport), false, nil
}

func (repository *SQLCipherRepository) TransitionSimulatorTransport(ctx context.Context, key BindingKey, operationID string,
	next TransportState, resultHash *[sha256.Size]byte, updatedAt string,
) error {
	if !repository.valid(ctx) || !validBindingKey(key) || !ids.IsCanonicalV7(operationID) || !validTimestamp(updatedAt) {
		return ErrInvalid
	}
	current, err := repository.getTransport(ctx, key, operationID)
	if err != nil {
		return err
	}
	if !transportTransitionAllowed(current.State, next) || ((next == TransportAccepted || next == TransportFailed) != (resultHash != nil)) {
		return ErrInvalidTransition
	}
	tx, err := repository.database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		return ErrRepository
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	var result any
	if resultHash != nil {
		result = resultHash[:]
	}
	updated, err := tx.ExecContext(ctx, `UPDATE sbr_simulator_transports_v1 SET state=?,result_hash=?,updated_at=?
WHERE operation_id=? AND workspace_id=? AND organisation_id=? AND canonical_abn=? AND schema_version=?
AND credential_fingerprint=? AND state=?`, next, result, updatedAt, operationID, key.WorkspaceID, key.OrganisationID,
		key.CanonicalABN, key.SchemaVersion, key.CredentialFingerprint[:], current.State)
	if err != nil || !exactlyOne(updated) {
		return ErrConflict
	}
	if resultHash != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE sbr_idempotency_v1 SET result_hash=? WHERE workspace_id=? AND organisation_id=?
AND canonical_abn=? AND schema_version=? AND credential_fingerprint=? AND idempotency_key=? AND result_hash IS NULL`,
			resultHash[:], key.WorkspaceID, key.OrganisationID, key.CanonicalABN, key.SchemaVersion,
			key.CredentialFingerprint[:], current.IdempotencyKey); err != nil {
			return ErrRepository
		}
	}
	if err := tx.Commit(); err != nil {
		return ErrRepository
	}
	committed = true
	return nil
}

func (repository *SQLCipherRepository) ApplySimulatorCase(ctx context.Context, key BindingKey, operationID string,
	caseValue SimulatorCase, resultHash *[sha256.Size]byte, _ string,
) error {
	updatedAt, err := repository.timestamp()
	if err != nil {
		return err
	}
	switch caseValue {
	case SimulatorCasePreDispatchFailure:
		return repository.TransitionSimulatorTransport(ctx, key, operationID, TransportNotStarted, nil, updatedAt)
	case SimulatorCaseUncertainWrite, SimulatorCaseHelperDeath, SimulatorCaseTimeout:
		return repository.TransitionSimulatorTransport(ctx, key, operationID, TransportMaybeSent, nil, updatedAt)
	case SimulatorCaseSyntacticResponse:
		return repository.TransitionSimulatorTransport(ctx, key, operationID, TransportResponseReceived, nil, updatedAt)
	case SimulatorCaseMalformedResponse, SimulatorCaseAccepted:
		if resultHash == nil {
			return ErrInvalid
		}
		terminal := TransportFailed
		if caseValue == SimulatorCaseAccepted {
			terminal = TransportAccepted
		}
		alreadyTerminal, err := repository.stageSimulatorOutcome(ctx, key, operationID, terminal, *resultHash, updatedAt)
		if err != nil || alreadyTerminal {
			return err
		}
		if repository.hooks != nil && repository.hooks.afterResponseReceived != nil {
			if err := repository.hooks.afterResponseReceived(); err != nil {
				return err
			}
		}
		return repository.finalizeSimulatorOutcome(ctx, key, operationID, terminal, *resultHash, updatedAt)
	default:
		return ErrInvalid
	}
}

func (repository *SQLCipherRepository) stageSimulatorOutcome(ctx context.Context, key BindingKey, operationID string,
	terminal TransportState, resultHash [sha256.Size]byte, updatedAt string,
) (bool, error) {
	if !repository.valid(ctx) || !validBindingKey(key) || !ids.IsCanonicalV7(operationID) || !validTimestamp(updatedAt) ||
		terminal != TransportAccepted && terminal != TransportFailed || zeroHash(resultHash) {
		return false, ErrInvalid
	}
	tx, err := repository.database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		return false, ErrRepository
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	var state TransportState
	var pendingTerminal sql.NullString
	var pendingResult, storedResult []byte
	if err := tx.QueryRowContext(ctx, `SELECT state,pending_terminal_state,pending_result_hash,result_hash
FROM sbr_simulator_transports_v1 WHERE operation_id=? AND workspace_id=? AND organisation_id=? AND canonical_abn=?
AND schema_version=? AND credential_fingerprint=?`, operationID, key.WorkspaceID, key.OrganisationID, key.CanonicalABN,
		key.SchemaVersion, key.CredentialFingerprint[:]).Scan(&state, &pendingTerminal, &pendingResult, &storedResult); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrNotFound
		}
		return false, ErrRepository
	}
	if state == terminal && bytes.Equal(storedResult, resultHash[:]) && !pendingTerminal.Valid && len(pendingResult) == 0 {
		if err := tx.Commit(); err != nil {
			return false, ErrRepository
		}
		committed = true
		return true, nil
	}
	if state != TransportDispatching && state != TransportResponseReceived {
		return false, ErrInvalidTransition
	}
	if pendingTerminal.Valid {
		if TransportState(pendingTerminal.String) != terminal || !bytes.Equal(pendingResult, resultHash[:]) {
			return false, ErrConflict
		}
	} else {
		result, err := tx.ExecContext(ctx, `UPDATE sbr_simulator_transports_v1 SET state=?,pending_terminal_state=?,
pending_result_hash=?,updated_at=? WHERE operation_id=? AND workspace_id=? AND organisation_id=? AND canonical_abn=?
AND schema_version=? AND credential_fingerprint=? AND state=? AND pending_terminal_state IS NULL AND pending_result_hash IS NULL`,
			TransportResponseReceived, terminal, resultHash[:], updatedAt, operationID, key.WorkspaceID, key.OrganisationID,
			key.CanonicalABN, key.SchemaVersion, key.CredentialFingerprint[:], state)
		if err != nil || !exactlyOne(result) {
			return false, ErrConflict
		}
	}
	if err := tx.Commit(); err != nil {
		return false, ErrRepository
	}
	committed = true
	return false, nil
}

func (repository *SQLCipherRepository) finalizeSimulatorOutcome(ctx context.Context, key BindingKey, operationID string,
	terminal TransportState, resultHash [sha256.Size]byte, updatedAt string,
) error {
	tx, err := repository.database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		return ErrRepository
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	var state TransportState
	var idempotencyKey string
	var pendingTerminal sql.NullString
	var pendingResult, storedResult []byte
	if err := tx.QueryRowContext(ctx, `SELECT state,idempotency_key,pending_terminal_state,pending_result_hash,result_hash
FROM sbr_simulator_transports_v1 WHERE operation_id=? AND workspace_id=? AND organisation_id=? AND canonical_abn=?
AND schema_version=? AND credential_fingerprint=?`, operationID, key.WorkspaceID, key.OrganisationID, key.CanonicalABN,
		key.SchemaVersion, key.CredentialFingerprint[:]).Scan(&state, &idempotencyKey, &pendingTerminal, &pendingResult, &storedResult); err != nil {
		return ErrRepository
	}
	if state == terminal && bytes.Equal(storedResult, resultHash[:]) && !pendingTerminal.Valid && len(pendingResult) == 0 {
		if err := tx.Commit(); err != nil {
			return ErrRepository
		}
		committed = true
		return nil
	}
	if state != TransportResponseReceived || !pendingTerminal.Valid || TransportState(pendingTerminal.String) != terminal ||
		!bytes.Equal(pendingResult, resultHash[:]) {
		return ErrConflict
	}
	updated, err := tx.ExecContext(ctx, `UPDATE sbr_simulator_transports_v1 SET state=?,result_hash=?,
pending_terminal_state=NULL,pending_result_hash=NULL,updated_at=? WHERE operation_id=? AND workspace_id=? AND organisation_id=?
AND canonical_abn=? AND schema_version=? AND credential_fingerprint=? AND state=? AND pending_terminal_state=? AND pending_result_hash=?`,
		terminal, resultHash[:], updatedAt, operationID, key.WorkspaceID, key.OrganisationID, key.CanonicalABN,
		key.SchemaVersion, key.CredentialFingerprint[:], TransportResponseReceived, terminal, resultHash[:])
	if err != nil || !exactlyOne(updated) {
		return ErrConflict
	}
	idempotency, err := tx.ExecContext(ctx, `UPDATE sbr_idempotency_v1 SET result_hash=? WHERE workspace_id=? AND organisation_id=?
AND canonical_abn=? AND schema_version=? AND credential_fingerprint=? AND idempotency_key=? AND result_hash IS NULL`,
		resultHash[:], key.WorkspaceID, key.OrganisationID, key.CanonicalABN, key.SchemaVersion,
		key.CredentialFingerprint[:], idempotencyKey)
	if err != nil || !exactlyOne(idempotency) {
		return ErrConflict
	}
	if err := tx.Commit(); err != nil {
		return ErrRepository
	}
	committed = true
	return nil
}

func (repository *SQLCipherRepository) RecoverSimulatorOrphans(ctx context.Context, updatedAt string) (int64, error) {
	if !repository.valid(ctx) || !validTimestamp(updatedAt) {
		return 0, ErrInvalid
	}
	result, err := repository.database.ExecContext(ctx, `UPDATE sbr_simulator_transports_v1 SET state='UNKNOWN',updated_at=?
WHERE state IN ('DISPATCHING','MAYBE_SENT')`, updatedAt)
	if err != nil {
		return 0, ErrRepository
	}
	count, err := result.RowsAffected()
	if err != nil || count < 0 {
		return 0, ErrRepository
	}
	return count, nil
}

func (repository *SQLCipherRepository) RetryNotStarted(ctx context.Context, key BindingKey, originalOperationID string, retry SimulatorTransport) error {
	if !repository.valid(ctx) || !validBindingKey(key) || !ids.IsCanonicalV7(originalOperationID) || !validTransport(retry) ||
		retry.Key != key || retry.State != TransportPrepared || retry.ResultHash != nil {
		return ErrInvalid
	}
	tx, err := repository.database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		return ErrRepository
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	var originalState TransportState
	var originalSemantic []byte
	var originalKey string
	if err := tx.QueryRowContext(ctx, `SELECT state,semantic_hash,idempotency_key FROM sbr_simulator_transports_v1
WHERE operation_id=? AND workspace_id=? AND organisation_id=? AND canonical_abn=? AND schema_version=? AND credential_fingerprint=?`,
		originalOperationID, key.WorkspaceID, key.OrganisationID, key.CanonicalABN, key.SchemaVersion,
		key.CredentialFingerprint[:]).Scan(&originalState, &originalSemantic, &originalKey); err != nil {
		return ErrNotFound
	}
	if originalState != TransportNotStarted || !bytes.Equal(originalSemantic, retry.SemanticHash[:]) || retry.IdempotencyKey == originalKey {
		return ErrInvalidTransition
	}
	var existingOperation, existingKey string
	var existingSemantic []byte
	existingErr := tx.QueryRowContext(ctx, `SELECT operation_id,idempotency_key,semantic_hash FROM sbr_simulator_transports_v1
WHERE retry_of_operation_id=?`, originalOperationID).Scan(&existingOperation, &existingKey, &existingSemantic)
	if existingErr == nil {
		if existingOperation == retry.OperationID && existingKey == retry.IdempotencyKey && bytes.Equal(existingSemantic, retry.SemanticHash[:]) {
			if err := tx.Commit(); err != nil {
				return ErrRepository
			}
			committed = true
			return nil
		}
		return ErrConflict
	}
	if !errors.Is(existingErr, sql.ErrNoRows) {
		return ErrRepository
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO sbr_idempotency_v1(idempotency_key,workspace_id,organisation_id,
canonical_abn,schema_version,credential_fingerprint,semantic_hash,result_hash,original_operation_id,created_at)
VALUES (?,?,?,?,?,?,?,NULL,?,?)`, retry.IdempotencyKey, key.WorkspaceID, key.OrganisationID, key.CanonicalABN,
		key.SchemaVersion, key.CredentialFingerprint[:], retry.SemanticHash[:], retry.OperationID, retry.CreatedAt)
	if err != nil {
		return ErrConflict
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO sbr_simulator_transports_v1(operation_id,workspace_id,organisation_id,
canonical_abn,schema_version,credential_fingerprint,idempotency_key,semantic_hash,result_hash,retry_of_operation_id,state,created_at,updated_at)
VALUES (?,?,?,?,?,?,?,?,NULL,?,?,?,?)`, retry.OperationID, key.WorkspaceID, key.OrganisationID, key.CanonicalABN,
		key.SchemaVersion, key.CredentialFingerprint[:], retry.IdempotencyKey, retry.SemanticHash[:], originalOperationID,
		retry.State, retry.CreatedAt, retry.UpdatedAt)
	if err != nil {
		return ErrConflict
	}
	if err := tx.Commit(); err != nil {
		return ErrConflict
	}
	committed = true
	return nil
}

func (repository *SQLCipherRepository) getTransport(ctx context.Context, key BindingKey, operationID string) (SimulatorTransport, error) {
	transport := SimulatorTransport{OperationID: operationID, Key: key}
	var fingerprint, semantic, result, pendingResult []byte
	var pendingTerminal sql.NullString
	err := repository.database.QueryRowContext(ctx, `SELECT credential_fingerprint,idempotency_key,semantic_hash,result_hash,
state,created_at,updated_at,pending_terminal_state,pending_result_hash FROM sbr_simulator_transports_v1 WHERE operation_id=? AND workspace_id=? AND organisation_id=?
AND canonical_abn=? AND schema_version=? AND credential_fingerprint=?`, operationID, key.WorkspaceID, key.OrganisationID,
		key.CanonicalABN, key.SchemaVersion, key.CredentialFingerprint[:]).Scan(&fingerprint, &transport.IdempotencyKey,
		&semantic, &result, &transport.State, &transport.CreatedAt, &transport.UpdatedAt, &pendingTerminal, &pendingResult)
	if errors.Is(err, sql.ErrNoRows) {
		return SimulatorTransport{}, ErrNotFound
	}
	if err != nil || len(fingerprint) != sha256.Size || len(semantic) != sha256.Size {
		return SimulatorTransport{}, ErrRepository
	}
	copy(transport.SemanticHash[:], semantic)
	if len(result) != 0 {
		if len(result) != sha256.Size {
			return SimulatorTransport{}, ErrRepository
		}
		value := [sha256.Size]byte{}
		copy(value[:], result)
		transport.ResultHash = &value
	}
	if pendingTerminal.Valid {
		if len(pendingResult) != sha256.Size {
			return SimulatorTransport{}, ErrRepository
		}
		transport.pendingTerminal = TransportState(pendingTerminal.String)
		value := [sha256.Size]byte{}
		copy(value[:], pendingResult)
		transport.pendingResultHash = &value
	} else if len(pendingResult) != 0 {
		return SimulatorTransport{}, ErrRepository
	}
	return transport, nil
}

type rowScanner interface{ Scan(...any) error }
type queryRowFunc func(context.Context, string, ...any) rowScanner

func loadTransportByIdempotency(ctx context.Context, queryRow queryRowFunc, key BindingKey, idempotencyKey string) (SimulatorTransport, error) {
	transport := SimulatorTransport{Key: key, IdempotencyKey: idempotencyKey}
	var fingerprint, semantic, result []byte
	err := queryRow(ctx, `SELECT operation_id,credential_fingerprint,semantic_hash,result_hash,state,created_at,updated_at
FROM sbr_simulator_transports_v1 WHERE workspace_id=? AND organisation_id=? AND canonical_abn=? AND schema_version=?
AND credential_fingerprint=? AND idempotency_key=?`, key.WorkspaceID, key.OrganisationID, key.CanonicalABN,
		key.SchemaVersion, key.CredentialFingerprint[:], idempotencyKey).Scan(&transport.OperationID, &fingerprint,
		&semantic, &result, &transport.State, &transport.CreatedAt, &transport.UpdatedAt)
	if err != nil || len(fingerprint) != sha256.Size || len(semantic) != sha256.Size {
		return SimulatorTransport{}, ErrRepository
	}
	copy(transport.SemanticHash[:], semantic)
	if len(result) == sha256.Size {
		value := [sha256.Size]byte{}
		copy(value[:], result)
		transport.ResultHash = &value
	}
	return transport, nil
}

func (repository *SQLCipherRepository) transitionMutation(ctx context.Context, key BindingKey, operationID string,
	from, to MutationState, pendingID, updatedAt string,
) error {
	query := `UPDATE sbr_mutations_v1 SET mutation_state=?,updated_at=? WHERE operation_id=? AND workspace_id=?
AND organisation_id=? AND canonical_abn=? AND schema_version=? AND credential_fingerprint=? AND mutation_state=?`
	arguments := []any{to, updatedAt, operationID, key.WorkspaceID, key.OrganisationID, key.CanonicalABN,
		key.SchemaVersion, key.CredentialFingerprint[:], from}
	if pendingID != "" {
		query = `UPDATE sbr_mutations_v1 SET mutation_state=?,pending_id=?,updated_at=? WHERE operation_id=? AND workspace_id=?
AND organisation_id=? AND canonical_abn=? AND schema_version=? AND credential_fingerprint=? AND mutation_state=?`
		arguments = []any{to, pendingID, updatedAt, operationID, key.WorkspaceID, key.OrganisationID,
			key.CanonicalABN, key.SchemaVersion, key.CredentialFingerprint[:], from}
	}
	result, err := repository.database.ExecContext(ctx, query, arguments...)
	if err != nil {
		return ErrRepository
	}
	if !exactlyOne(result) {
		return mutationMissingOrTransition(repository, ctx, key, operationID)
	}
	return nil
}

func mutationMissingOrTransition(repository *SQLCipherRepository, ctx context.Context, key BindingKey, operationID string) error {
	if _, err := repository.GetMutation(ctx, key, operationID); errors.Is(err, ErrNotFound) {
		return ErrNotFound
	}
	return ErrInvalidTransition
}

func validOperationInput(repository *SQLCipherRepository, ctx context.Context, key BindingKey, operationID, updatedAt string) bool {
	return repository.valid(ctx) && validBindingScope(key) && ids.IsCanonicalV7(operationID) && validTimestamp(updatedAt)
}

func (repository *SQLCipherRepository) valid(ctx context.Context) bool {
	return repository != nil && repository.database != nil && repository.database.DB != nil && repository.now != nil && ctx != nil && ctx.Err() == nil
}

func (repository *SQLCipherRepository) timestamp() (string, error) {
	if repository == nil || repository.now == nil {
		return "", ErrRepository
	}
	now := repository.now().UTC()
	if now.IsZero() {
		return "", ErrRepository
	}
	value := now.Format("2006-01-02T15:04:05.000000000Z")
	if !validTimestamp(value) {
		return "", ErrRepository
	}
	return value, nil
}

func validBinding(binding Binding) bool {
	return validBindingKey(binding.Key) && len(binding.ComponentVersion) >= 1 && len(binding.ComponentVersion) <= 64 &&
		!zeroHash(binding.SubjectHash) && validTimestamp(binding.ExpiresAt) && validTimestamp(binding.UpdatedAt) &&
		binding.Revision == 1 && binding.State == BindingActive
}

func validBindingKey(key BindingKey) bool {
	return validBindingScope(key) && !zeroHash(key.CredentialFingerprint)
}

func validBindingScope(key BindingKey) bool {
	if !ids.IsCanonicalV7(key.WorkspaceID) || !ids.IsCanonicalV7(key.OrganisationID) || key.SchemaVersion != 1 || !abn.Valid(key.CanonicalABN) {
		return false
	}
	return true
}

func sameScope(left, right BindingKey) bool {
	return left.WorkspaceID == right.WorkspaceID && left.OrganisationID == right.OrganisationID &&
		left.CanonicalABN == right.CanonicalABN && left.SchemaVersion == right.SchemaVersion
}

func zeroHash(value [sha256.Size]byte) bool { return value == [sha256.Size]byte{} }

func validBindingTransitionTarget(state BindingState) bool {
	return state == BindingActive || state == BindingReimportRequired || state == BindingRemoved
}
func bindingTransitionAllowed(from, to BindingState) bool {
	return from == BindingActive && (to == BindingReimportRequired || to == BindingRemoved) ||
		from == BindingReimportRequired && (to == BindingActive || to == BindingRemoved)
}
func validMutation(mutation Mutation) bool {
	validKind := mutation.Kind == MutationImportCredential || mutation.Kind == MutationReplaceCredential || mutation.Kind == MutationRemoveCredential ||
		mutation.Kind == MutationImportProductID || mutation.Kind == MutationRemoveProductID
	validKey := validBindingKey(mutation.Key)
	if mutation.Kind == MutationImportCredential {
		validKey = validBindingScope(mutation.Key) && zeroHash(mutation.Key.CredentialFingerprint)
	}
	return ids.IsCanonicalV7(mutation.OperationID) && validKey && validKind &&
		!zeroHash(mutation.MetadataHash) && validTimestamp(mutation.CreatedAt) && validTimestamp(mutation.UpdatedAt)
}
func validTransport(transport SimulatorTransport) bool {
	return ids.IsCanonicalV7(transport.OperationID) && validBindingKey(transport.Key) && len(transport.IdempotencyKey) >= 1 &&
		len(transport.IdempotencyKey) <= 128 && strings.IndexByte(transport.IdempotencyKey, 0) < 0 &&
		!zeroHash(transport.SemanticHash) && (transport.ResultHash == nil || !zeroHash(*transport.ResultHash)) &&
		validTimestamp(transport.CreatedAt) && validTimestamp(transport.UpdatedAt)
}
func transportTransitionAllowed(from, to TransportState) bool {
	switch from {
	case TransportPrepared:
		return to == TransportDispatching || to == TransportNotStarted
	case TransportDispatching:
		return to == TransportMaybeSent || to == TransportResponseReceived
	case TransportMaybeSent:
		return to == TransportUnknown
	default:
		return false
	}
}
func exactlyOne(result sql.Result) bool {
	count, err := result.RowsAffected()
	return err == nil && count == 1
}
func cloneTransport(value SimulatorTransport) SimulatorTransport {
	if value.ResultHash != nil {
		result := *value.ResultHash
		value.ResultHash = &result
	}
	return value
}

func validReadiness(state ReadinessState) bool {
	return state == ReadinessUnavailable || state == ReadinessReadyForSimulator ||
		state == ReadinessReadyForEVTEPreConformance || state == ReadinessReadyForEVTEPostConformance
}

func validProfileConformance(environment Environment, conformance Conformance) bool {
	return environment == EnvironmentSimulator && (conformance == "" || conformance == ConformanceSimulator) ||
		environment == EnvironmentEVTE && (conformance == ConformancePre || conformance == ConformancePost)
}
