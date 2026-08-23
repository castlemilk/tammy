package sbr

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/authorisation"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/abn"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	PurposeImportMachineCredential  = "sbr_machine_credential_import"
	PurposeUnlockMachineCredential  = "sbr_machine_credential_unlock"
	PurposeReplaceMachineCredential = "sbr_machine_credential_replace"
	PurposeRemoveMachineCredential  = "sbr_machine_credential_remove"
	PurposeImportProductID          = "sbr_product_id_import"
	PurposeRemoveProductID          = "sbr_product_id_remove"
	PurposeUseMachineCredential     = "sbr_machine_credential_use"

	ReadinessFixtureID = "SIM-SBR-READINESS-V1"
)

var ErrService = errors.New("sbr: readiness service unavailable")

type MutationExecutor = workspace.MutationExecutor

type QueryExecutor interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type IdentityPort interface {
	AuthorizeWithin(context.Context, MutationExecutor, *tammyv1.AuthenticationContext, authorisation.Action) error
	ConsumeFreshFactorWithin(context.Context, MutationExecutor, *tammyv1.AuthenticationContext, *tammyv1.FreshFactorContext, string) error
	ValidateAuthorizationWithin(context.Context, MutationExecutor, *tammyv1.AuthenticationContext, authorisation.Action) error
	ValidateFreshFactorWithin(context.Context, MutationExecutor, *tammyv1.AuthenticationContext, *tammyv1.FreshFactorContext, string) error
}

type UnitOfWork interface {
	Inspect(context.Context, func(context.Context, QueryExecutor) error) error
	Mutate(context.Context, func(context.Context, MutationExecutor) error) error
}

type OrganisationBinding struct {
	OrganisationID        string
	CanonicalABN          string
	VerificationExpiresAt time.Time
}

type OrganisationPort interface {
	Current(context.Context, QueryExecutor, time.Time) (OrganisationBinding, error)
}

type RuntimeProfile struct {
	Environment               tammyv1.SbrEnvironment
	ComponentVersion          string
	ProfileFingerprint        [sha256.Size]byte
	RegistrationFingerprint   [sha256.Size]byte
	ComponentFingerprint      [sha256.Size]byte
	AuthenticatedUntil        time.Time
	EndpointProfile           []byte
	ExpectedProductIdentifier string
	ExpectedServiceID         string
	ProductScopeFingerprint   [sha256.Size]byte
	lease                     ProfileLease
}

func (profile RuntimeProfile) Clone() RuntimeProfile {
	profile.EndpointProfile = bytes.Clone(profile.EndpointProfile)
	return profile
}

type ProfileLease interface {
	HelperPort
	Close() error
}

func BindRuntimeProfileLease(profile RuntimeProfile, lease ProfileLease) RuntimeProfile {
	profile.lease = lease
	return profile
}

// BindAuthenticatedProductScope carries the non-secret Product/service pair
// selected by the authenticated EVTE resource lease into core policy checks.
func BindAuthenticatedProductScope(profile RuntimeProfile, productIdentifier, serviceID string) RuntimeProfile {
	profile.ExpectedProductIdentifier = productIdentifier
	profile.ExpectedServiceID = serviceID
	profile.ProductScopeFingerprint = authenticatedProductScopeFingerprint(productIdentifier, serviceID)
	return profile
}

func (profile RuntimeProfile) Execute(ctx context.Context, request HelperRequest) (HelperResult, error) {
	if profile.lease == nil {
		return HelperResult{}, ErrService
	}
	return profile.lease.Execute(ctx, request)
}

func (profile RuntimeProfile) Close() error {
	if profile.lease == nil {
		return nil
	}
	return profile.lease.Close()
}

type ProfilePort interface {
	Current(context.Context, time.Time) (RuntimeProfile, error)
}

type HelperOperation uint8

const (
	HelperOperationStatus HelperOperation = iota + 1
	HelperOperationUnlock
	HelperOperationFixture
	HelperOperationPrepareMutation
	HelperOperationCommitMutation
	HelperOperationAbortMutation
	HelperOperationReconcileMutation
)

type HelperOutcome uint8

const (
	HelperOutcomeOK HelperOutcome = iota + 1
	HelperOutcomePending
	HelperOutcomeFailed
)

type HelperResultCode uint8

const (
	HelperResultNone HelperResultCode = iota
	HelperResultReady
	HelperResultCredentialLocked
	HelperResultRegistrationRequired
	HelperResultMutationCommitted
	HelperResultMutationAborted
	HelperResultRecoveryRequired
	HelperResultFixtureSelected
	HelperResultNotStarted
)

type ProductState uint8

const (
	ProductMissing ProductState = iota + 1
	ProductPresent
	ProductInaccessible
)

type CredentialMetadata struct {
	Fingerprint      [sha256.Size]byte
	CanonicalABN     string
	Issuer           string
	Serial           string
	CreatedAt        time.Time
	ExpiresAt        time.Time
	ComponentVersion string
	State            tammyv1.MachineCredentialState
}

type HelperRequest struct {
	Operation               HelperOperation
	RequestID               string
	MutationKind            MutationKind
	OperationID             string
	PendingID               string
	Environment             tammyv1.SbrEnvironment
	WorkspaceID             string
	OrganisationID          string
	CanonicalABN            string
	OpaqueScope             []byte
	SelectedLocalPath       string
	Bookmark                []byte
	Password                []byte
	ProductID               []byte
	ProductIdentifier       string
	ServiceIdentifier       string
	EndpointProfile         []byte
	ProfileFingerprint      [sha256.Size]byte
	RegistrationFingerprint [sha256.Size]byte
	ComponentFingerprint    [sha256.Size]byte
	ComponentVersion        string
	FixtureFailureCase      tammyv1.SbrReadinessFixtureFailure
}

func (request HelperRequest) Clone() HelperRequest {
	request.OpaqueScope = bytes.Clone(request.OpaqueScope)
	request.Bookmark = bytes.Clone(request.Bookmark)
	request.Password = bytes.Clone(request.Password)
	request.ProductID = bytes.Clone(request.ProductID)
	request.EndpointProfile = bytes.Clone(request.EndpointProfile)
	return request
}

func (request *HelperRequest) ClearSecrets() {
	if request == nil {
		return
	}
	for _, value := range [][]byte{request.OpaqueScope, request.Bookmark, request.Password, request.ProductID, request.EndpointProfile} {
		clear(value)
	}
	request.OpaqueScope, request.Bookmark, request.Password, request.ProductID, request.EndpointProfile = nil, nil, nil, nil, nil
	request.SelectedLocalPath = ""
}

type HelperResult struct {
	Outcome                 HelperOutcome
	RequestID               string
	ResultCode              HelperResultCode
	PendingID               string
	Credential              CredentialMetadata
	ProductState            ProductState
	ProductFingerprint      [sha256.Size]byte
	ProfileFingerprint      [sha256.Size]byte
	RegistrationFingerprint [sha256.Size]byte
	ComponentFingerprint    [sha256.Size]byte
	ComponentVersion        string
	FixtureState            TransportState
	FixtureFailureCase      tammyv1.SbrReadinessFixtureFailure
	StableCode              string
}

func (result HelperResult) Clone() HelperResult { return result }

type HelperPort interface {
	Execute(context.Context, HelperRequest) (HelperResult, error)
}

type AuditAction string

const (
	AuditCredentialImported            AuditAction = "CREDENTIAL_IMPORTED"
	AuditCredentialUnlocked            AuditAction = "CREDENTIAL_UNLOCKED"
	AuditCredentialUsed                AuditAction = "CREDENTIAL_USED"
	AuditCredentialFailed              AuditAction = "CREDENTIAL_FAILED"
	AuditCredentialExpired             AuditAction = "CREDENTIAL_EXPIRED"
	AuditCredentialReplaced            AuditAction = "CREDENTIAL_REPLACED"
	AuditCredentialRemoved             AuditAction = "CREDENTIAL_REMOVED"
	AuditCredentialSuspectedCompromise AuditAction = "CREDENTIAL_SUSPECTED_COMPROMISE"
	AuditProductIDChanged              AuditAction = "PRODUCT_ID_CHANGED"
	AuditProfileAccepted               AuditAction = "PROFILE_ACCEPTED"
	AuditProfileRejected               AuditAction = "PROFILE_REJECTED"
	AuditFixturePrepared               AuditAction = "HELPER_FIXTURE_PREPARED"
	AuditFixtureDispatching            AuditAction = "HELPER_FIXTURE_DISPATCHING"
	AuditFixtureCompleted              AuditAction = "HELPER_FIXTURE_COMPLETED"
	AuditFixtureUnknown                AuditAction = "HELPER_FIXTURE_UNKNOWN"
)

type AuditRecord struct {
	Action                AuditAction
	CredentialFingerprint [sha256.Size]byte
	ProfileFingerprint    [sha256.Size]byte
	ComponentFingerprint  [sha256.Size]byte
	StatusCode            string
}

type AuditPort interface {
	Record(context.Context, MutationExecutor, AuditRecord) error
}

func BuildAuditPayload(record AuditRecord) (*tammyv1.SbrAuditEvent, error) {
	actions := map[AuditAction]tammyv1.SbrAuditAction{
		AuditCredentialImported:            tammyv1.SbrAuditAction_SBR_AUDIT_ACTION_CREDENTIAL_IMPORTED,
		AuditCredentialUnlocked:            tammyv1.SbrAuditAction_SBR_AUDIT_ACTION_CREDENTIAL_UNLOCKED,
		AuditCredentialUsed:                tammyv1.SbrAuditAction_SBR_AUDIT_ACTION_CREDENTIAL_USED,
		AuditCredentialFailed:              tammyv1.SbrAuditAction_SBR_AUDIT_ACTION_CREDENTIAL_FAILED,
		AuditCredentialExpired:             tammyv1.SbrAuditAction_SBR_AUDIT_ACTION_CREDENTIAL_EXPIRED,
		AuditCredentialReplaced:            tammyv1.SbrAuditAction_SBR_AUDIT_ACTION_CREDENTIAL_REPLACED,
		AuditCredentialRemoved:             tammyv1.SbrAuditAction_SBR_AUDIT_ACTION_CREDENTIAL_REMOVED,
		AuditCredentialSuspectedCompromise: tammyv1.SbrAuditAction_SBR_AUDIT_ACTION_CREDENTIAL_SUSPECTED_COMPROMISE,
		AuditProductIDChanged:              tammyv1.SbrAuditAction_SBR_AUDIT_ACTION_PRODUCT_ID_CHANGED,
		AuditProfileAccepted:               tammyv1.SbrAuditAction_SBR_AUDIT_ACTION_PROFILE_ACCEPTED,
		AuditProfileRejected:               tammyv1.SbrAuditAction_SBR_AUDIT_ACTION_PROFILE_REJECTED,
		AuditFixturePrepared:               tammyv1.SbrAuditAction_SBR_AUDIT_ACTION_HELPER_FIXTURE_PREPARED,
		AuditFixtureDispatching:            tammyv1.SbrAuditAction_SBR_AUDIT_ACTION_HELPER_FIXTURE_DISPATCHING,
		AuditFixtureCompleted:              tammyv1.SbrAuditAction_SBR_AUDIT_ACTION_HELPER_FIXTURE_COMPLETED,
		AuditFixtureUnknown:                tammyv1.SbrAuditAction_SBR_AUDIT_ACTION_HELPER_FIXTURE_UNKNOWN,
	}
	action, ok := actions[record.Action]
	if !ok || record.StatusCode == "" || len(record.StatusCode) > 96 {
		return nil, ErrService
	}
	payload := &tammyv1.SbrAuditEvent{Action: action, StatusCode: record.StatusCode}
	if record.CredentialFingerprint != [sha256.Size]byte{} {
		payload.CredentialFingerprint = bytes.Clone(record.CredentialFingerprint[:])
	}
	if record.ProfileFingerprint != [sha256.Size]byte{} {
		payload.ProfileFingerprint = bytes.Clone(record.ProfileFingerprint[:])
	}
	if record.ComponentFingerprint != [sha256.Size]byte{} {
		payload.ComponentFingerprint = bytes.Clone(record.ComponentFingerprint[:])
	}
	return payload, nil
}

type discardAudit struct{}

func (discardAudit) Record(context.Context, MutationExecutor, AuditRecord) error { return nil }

type RedactedSQLAuditAppender struct{ now func() time.Time }

func NewRedactedSQLAuditAppender(now func() time.Time) (*RedactedSQLAuditAppender, error) {
	if now == nil {
		return nil, ErrService
	}
	return &RedactedSQLAuditAppender{now: now}, nil
}

func (appender *RedactedSQLAuditAppender) Record(ctx context.Context, executor MutationExecutor, record AuditRecord) error {
	if appender == nil || appender.now == nil || ctx == nil || executor == nil {
		return ErrService
	}
	payload, err := BuildAuditPayload(record)
	if err != nil {
		return err
	}
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(payload)
	if err != nil || len(encoded) == 0 {
		return ErrService
	}
	defer clear(encoded)
	optionalFingerprint := func(value [sha256.Size]byte) any {
		if value == [sha256.Size]byte{} {
			return nil
		}
		return value[:]
	}
	_, err = executor.ExecContext(ctx, `INSERT INTO sbr_audit_events_v1(action,status_code,credential_fingerprint,
profile_fingerprint,component_fingerprint,payload_proto,occurred_at) VALUES (?,?,?,?,?,?,?)`,
		int32(payload.Action), payload.StatusCode, optionalFingerprint(record.CredentialFingerprint),
		optionalFingerprint(record.ProfileFingerprint), optionalFingerprint(record.ComponentFingerprint), encoded,
		appender.now().UTC().Format("2006-01-02T15:04:05.000000000Z"))
	if err != nil {
		return ErrService
	}
	return nil
}

type serviceBinding struct {
	metadata           CredentialMetadata
	profile            RuntimeProfile
	state              tammyv1.MachineCredentialState
	product            ProductState
	productScope       [sha256.Size]byte
	productFingerprint [sha256.Size]byte
	bindingState       string
}

type pendingMutation struct {
	kind            MutationKind
	binding         OrganisationBinding
	result          HelperResult
	idempotencyKey  string
	semanticHash    [sha256.Size]byte
	effect          serviceBinding
	completionAudit AuditRecord
}

type commandResult struct {
	OperationID string
	ActorUserID string
	Kind        MutationKind
	Semantic    [sha256.Size]byte
	Completed   bool
	Credential  CredentialMetadata
	Product     ProductState
}

type ServiceStore interface {
	Current(context.Context, OrganisationBinding) (serviceBinding, bool)
	Command(context.Context, OrganisationBinding, string, [sha256.Size]byte) (commandResult, bool, error)
	Prepare(context.Context, string, string, MutationKind, OrganisationBinding, string, [sha256.Size]byte,
		func(context.Context, MutationExecutor) error) error
	Stage(context.Context, string, HelperResult) error
	Commit(context.Context, string, serviceBinding, AuditRecord, func(context.Context, MutationExecutor) error) error
	Finish(context.Context, string, func(context.Context, MutationExecutor, AuditRecord) error) error
	Abort(context.Context, string) error
	FinishAbort(context.Context, string) error
	ProductState(context.Context, OrganisationBinding, RuntimeProfile) ProductState
	SetProductState(context.Context, OrganisationBinding, RuntimeProfile, ProductState, [sha256.Size]byte, [sha256.Size]byte)
	PrepareFixture(context.Context, OrganisationBinding, [sha256.Size]byte, string, string, string, [sha256.Size]byte) (FixtureRecord, bool, error)
	ReserveFixtureDispatch(context.Context, FixtureRecord, string, func(context.Context, MutationExecutor) error) error
	ApplyFixture(context.Context, FixtureRecord, SimulatorCase, *[sha256.Size]byte) error
	FinishFixtureWithAudit(context.Context, FixtureRecord, SimulatorCase, *[sha256.Size]byte, AuditRecord,
		func(context.Context, MutationExecutor, AuditRecord) error) error
	ReserveUnlockDispatch(context.Context, HelperDispatchRecord, func(context.Context, MutationExecutor) error) error
	FinishUnlockDispatch(context.Context, HelperDispatchRecord, HelperDispatchState) error
}

type FixtureRecord struct {
	OperationID    string
	ActorUserID    string
	State          TransportState
	semantic       [sha256.Size]byte
	credential     [sha256.Size]byte
	idempotencyKey string
	bindingKey     string
	scopeKey       string
}

type memoryServiceStore struct {
	mu         sync.Mutex
	bindings   map[string]serviceBinding
	pending    map[string]pendingMutation
	products   map[string]ProductState
	fixtures   map[string]FixtureRecord
	dispatches map[string]HelperDispatchRecord
	commands   map[string]commandResult
}

func newMemoryServiceStore() *memoryServiceStore {
	return &memoryServiceStore{bindings: map[string]serviceBinding{}, pending: map[string]pendingMutation{}, products: map[string]ProductState{}, fixtures: map[string]FixtureRecord{}, dispatches: map[string]HelperDispatchRecord{}, commands: map[string]commandResult{}}
}

func organisationStoreKey(binding OrganisationBinding) string {
	return binding.OrganisationID + "\x00" + binding.CanonicalABN
}

func authenticatedProductScopeFingerprint(productIdentifier, serviceID string) [sha256.Size]byte {
	digest := sha256.New()
	_, _ = digest.Write([]byte("tammy.sbr.authenticated-product-scope.v1\x00"))
	var length [8]byte
	for _, value := range []string{productIdentifier, serviceID} {
		binary.BigEndian.PutUint64(length[:], uint64(len(value)))
		_, _ = digest.Write(length[:])
		_, _ = digest.Write([]byte(value))
	}
	var result [sha256.Size]byte
	copy(result[:], digest.Sum(nil))
	return result
}

func productStoreKey(binding OrganisationBinding, profile RuntimeProfile) string {
	return organisationStoreKey(binding) + "\x00" + profile.Environment.String() + "\x00" +
		hex.EncodeToString(profile.ProductScopeFingerprint[:]) + "\x00" + profile.ExpectedProductIdentifier + "\x00" + profile.ExpectedServiceID
}
func (store *memoryServiceStore) Current(_ context.Context, binding OrganisationBinding) (serviceBinding, bool) {
	store.mu.Lock()
	defer store.mu.Unlock()
	value, ok := store.bindings[organisationStoreKey(binding)]
	return value, ok
}
func commandStoreKey(binding OrganisationBinding, idempotencyKey string) string {
	return organisationStoreKey(binding) + "\x00" + idempotencyKey
}
func (store *memoryServiceStore) Command(_ context.Context, binding OrganisationBinding, idempotencyKey string, semantic [sha256.Size]byte) (commandResult, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	value, ok := store.commands[commandStoreKey(binding, idempotencyKey)]
	if ok && value.Semantic != semantic {
		return commandResult{}, false, ErrIdempotencyConflict
	}
	return value, ok, nil
}
func (store *memoryServiceStore) Prepare(ctx context.Context, operation, actorUserID string, kind MutationKind, binding OrganisationBinding,
	idempotencyKey string, semantic [sha256.Size]byte, reserve func(context.Context, MutationExecutor) error,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if reserve == nil {
		return ErrInvalid
	}
	key := commandStoreKey(binding, idempotencyKey)
	if existing, ok := store.commands[key]; ok {
		if existing.Semantic != semantic {
			return ErrIdempotencyConflict
		}
		return ErrConflict
	}
	if _, ok := store.pending[operation]; ok {
		return ErrConflict
	}
	if err := reserve(ctx, memoryMutationExecutor{}); err != nil {
		return err
	}
	store.pending[operation] = pendingMutation{kind: kind, binding: binding, idempotencyKey: idempotencyKey, semanticHash: semantic}
	store.commands[key] = commandResult{OperationID: operation, ActorUserID: actorUserID, Kind: kind, Semantic: semantic}
	return nil
}
func (store *memoryServiceStore) Stage(_ context.Context, operation string, result HelperResult) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	value, ok := store.pending[operation]
	if !ok {
		return ErrNotFound
	}
	value.result = result
	store.pending[operation] = value
	return nil
}
func (store *memoryServiceStore) Commit(ctx context.Context, operation string, binding serviceBinding, completionAudit AuditRecord,
	decision func(context.Context, MutationExecutor) error,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	pending, ok := store.pending[operation]
	if !ok || decision == nil {
		return ErrNotFound
	}
	if err := decision(ctx, memoryMutationExecutor{}); err != nil {
		return err
	}
	pending.effect = binding
	pending.completionAudit = completionAudit
	store.pending[operation] = pending
	return nil
}

func (store *memoryServiceStore) Finish(ctx context.Context, operation string,
	audit func(context.Context, MutationExecutor, AuditRecord) error,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	pending, ok := store.pending[operation]
	if !ok || audit == nil {
		return ErrNotFound
	}
	if err := audit(ctx, memoryMutationExecutor{}, pending.completionAudit); err != nil {
		return err
	}
	if pending.kind == MutationRemoveCredential {
		delete(store.bindings, organisationStoreKey(pending.binding))
	} else if pending.kind == MutationImportCredential || pending.kind == MutationReplaceCredential {
		store.bindings[organisationStoreKey(pending.binding)] = pending.effect
	}
	commandKey := commandStoreKey(pending.binding, pending.idempotencyKey)
	command := store.commands[commandKey]
	command.Completed = true
	command.Credential = pending.effect.metadata
	command.Product = pending.result.ProductState
	store.commands[commandKey] = command
	if pending.effect.product != 0 {
		store.products[productStoreKey(pending.binding, pending.effect.profile)] = pending.effect.product
	}
	delete(store.pending, operation)
	return nil
}

type memoryMutationExecutor struct{}

func (memoryMutationExecutor) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, ErrRepository
}
func (memoryMutationExecutor) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, ErrRepository
}
func (store *memoryServiceStore) Abort(_ context.Context, operation string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	delete(store.pending, operation)
	return nil
}
func (store *memoryServiceStore) FinishAbort(_ context.Context, operation string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	delete(store.pending, operation)
	return nil
}
func (store *memoryServiceStore) ProductState(_ context.Context, binding OrganisationBinding, profile RuntimeProfile) ProductState {
	store.mu.Lock()
	defer store.mu.Unlock()
	state := store.products[productStoreKey(binding, profile)]
	if state == 0 {
		return ProductMissing
	}
	return state
}
func (store *memoryServiceStore) SetProductState(_ context.Context, binding OrganisationBinding, profile RuntimeProfile, state ProductState, _ [sha256.Size]byte, _ [sha256.Size]byte) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.products[productStoreKey(binding, profile)] = state
}
func (store *memoryServiceStore) PrepareFixture(_ context.Context, binding OrganisationBinding, credential [sha256.Size]byte, operation, actorUserID, idempotency string, semantic [sha256.Size]byte) (FixtureRecord, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	key := organisationStoreKey(binding) + "\x00" + idempotency
	if original, ok := store.fixtures[key]; ok {
		if original.semantic != semantic {
			return FixtureRecord{}, false, ErrIdempotencyConflict
		}
		return original, true, nil
	}
	scope := organisationStoreKey(binding)
	for _, existing := range store.fixtures {
		if existing.scopeKey == scope && existing.semantic == semantic &&
			(existing.State == TransportDispatching || existing.State == TransportMaybeSent || existing.State == TransportUnknown) {
			return FixtureRecord{}, false, ErrUncertainTransport
		}
	}
	record := FixtureRecord{OperationID: operation, ActorUserID: actorUserID, State: TransportPrepared, semantic: semantic,
		credential: credential, idempotencyKey: idempotency, bindingKey: key, scopeKey: scope}
	store.fixtures[key] = record
	return record, false, nil
}
func (store *memoryServiceStore) ReserveFixtureDispatch(ctx context.Context, record FixtureRecord, actorUserID string,
	effect func(context.Context, MutationExecutor) error,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	current, ok := store.fixtures[record.bindingKey]
	if !ok || current.State != TransportPrepared {
		return ErrInvalidTransition
	}
	if current.ActorUserID != actorUserID {
		return ErrPermissionDenied
	}
	if effect == nil {
		return ErrInvalid
	}
	if err := effect(ctx, fakeMemoryExecutor{}); err != nil {
		return err
	}
	current.State = TransportDispatching
	store.fixtures[record.bindingKey] = current
	return nil
}

type fakeMemoryExecutor struct{}

func (fakeMemoryExecutor) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, nil
}
func (fakeMemoryExecutor) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, ErrRepository
}

func (store *memoryServiceStore) ReserveUnlockDispatch(ctx context.Context, record HelperDispatchRecord,
	effect func(context.Context, MutationExecutor) error,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	key := record.Key.OrganisationID + "\x00" + record.Key.CanonicalABN + "\x00" + record.IdempotencyKey
	if original, ok := store.dispatches[key]; ok {
		if original.ActorUserID != record.ActorUserID {
			return ErrPermissionDenied
		}
		if original.SemanticHash != record.SemanticHash {
			return ErrIdempotencyConflict
		}
		return ErrUncertainTransport
	}
	if effect == nil {
		return ErrInvalid
	}
	if err := effect(ctx, fakeMemoryExecutor{}); err != nil {
		return err
	}
	store.dispatches[key] = record
	return nil
}

func (store *memoryServiceStore) FinishUnlockDispatch(_ context.Context, record HelperDispatchRecord, state HelperDispatchState) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	key := record.Key.OrganisationID + "\x00" + record.Key.CanonicalABN + "\x00" + record.IdempotencyKey
	current, ok := store.dispatches[key]
	if !ok || current.State != HelperDispatching || !validHelperDispatchTerminal(state) {
		return ErrInvalidTransition
	}
	current.State = state
	store.dispatches[key] = current
	return nil
}
func (store *memoryServiceStore) ApplyFixture(_ context.Context, record FixtureRecord, value SimulatorCase, _ *[sha256.Size]byte) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	current, ok := store.fixtures[record.bindingKey]
	if !ok {
		return ErrNotFound
	}
	switch value {
	case SimulatorCasePreDispatchFailure:
		current.State = TransportNotStarted
	case SimulatorCaseUncertainWrite, SimulatorCaseHelperDeath, SimulatorCaseTimeout:
		current.State = TransportMaybeSent
	case SimulatorCaseSyntacticResponse:
		current.State = TransportResponseReceived
	case SimulatorCaseMalformedResponse:
		current.State = TransportFailed
	case SimulatorCaseAccepted:
		current.State = TransportAccepted
	default:
		return ErrInvalid
	}
	store.fixtures[record.bindingKey] = current
	return nil
}

func (store *memoryServiceStore) FinishFixtureWithAudit(ctx context.Context, record FixtureRecord, value SimulatorCase,
	result *[sha256.Size]byte, auditRecord AuditRecord,
	audit func(context.Context, MutationExecutor, AuditRecord) error,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	current, ok := store.fixtures[record.bindingKey]
	if !ok || audit == nil {
		return ErrNotFound
	}
	var next TransportState
	switch value {
	case SimulatorCasePreDispatchFailure:
		next = TransportNotStarted
	case SimulatorCaseUncertainWrite, SimulatorCaseHelperDeath, SimulatorCaseTimeout:
		next = TransportMaybeSent
	case SimulatorCaseMalformedResponse:
		next = TransportFailed
	case SimulatorCaseAccepted:
		next = TransportAccepted
	default:
		return ErrInvalid
	}
	if (next == TransportAccepted || next == TransportFailed) != (result != nil) {
		return ErrInvalid
	}
	if err := audit(ctx, fakeMemoryExecutor{}, auditRecord); err != nil {
		return err
	}
	current.State = next
	store.fixtures[record.bindingKey] = current
	return nil
}

type ServiceConfig struct {
	WorkspaceID     string
	Identity        IdentityPort
	Organisation    OrganisationPort
	Profiles        ProfilePort
	Helper          HelperPort
	Units           UnitOfWork
	Store           ServiceStore
	Audit           AuditPort
	Now             func() time.Time
	NewID           func() (string, error)
	InstallationKey []byte
}

type Service struct {
	workspaceID     string
	identity        IdentityPort
	organisation    OrganisationPort
	profiles        ProfilePort
	helper          HelperPort
	units           UnitOfWork
	store           ServiceStore
	audit           AuditPort
	now             func() time.Time
	newID           func() (string, error)
	installationKey [sha256.Size]byte
}

func NewService(config ServiceConfig) (*Service, error) {
	if !ids.IsCanonicalV7(config.WorkspaceID) || config.Identity == nil || config.Organisation == nil ||
		config.Profiles == nil || config.Helper == nil || config.Units == nil || config.Now == nil || config.NewID == nil ||
		config.Audit == nil || len(config.InstallationKey) != sha256.Size {
		return nil, ErrService
	}
	if config.Store == nil {
		config.Store = newMemoryServiceStore()
	}
	service := &Service{workspaceID: config.WorkspaceID, identity: config.Identity, organisation: config.Organisation,
		profiles: config.Profiles, helper: config.Helper, units: config.Units, store: config.Store, audit: config.Audit,
		now: config.Now, newID: config.NewID}
	copy(service.installationKey[:], config.InstallationKey)
	return service, nil
}

func DeriveOpaqueScope(installationKey []byte, workspaceID, organisationID, canonicalABN string) [sha256.Size]byte {
	if len(installationKey) != sha256.Size || !ids.IsCanonicalV7(workspaceID) || !ids.IsCanonicalV7(organisationID) || !abn.Valid(canonicalABN) {
		return [sha256.Size]byte{}
	}
	digest := hmac.New(sha256.New, installationKey)
	_, _ = digest.Write([]byte(workspaceID))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write([]byte(organisationID))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write([]byte(canonicalABN))
	var scope [sha256.Size]byte
	copy(scope[:], digest.Sum(nil))
	return scope
}

func (service *Service) authorize(ctx context.Context, authentication *tammyv1.AuthenticationContext, factor *tammyv1.FreshFactorContext, action authorisation.Action, purpose string) (OrganisationBinding, RuntimeProfile, error) {
	binding, profile, err := service.current(ctx)
	if err != nil {
		if authErr := service.authorizeCurrent(ctx, authentication, factor, action, purpose); authErr != nil {
			return binding, profile, authErr
		}
		if auditErr := service.auditProfile(ctx, profile, false); auditErr != nil {
			return binding, profile, auditErr
		}
		return binding, profile, err
	}
	if err := service.authorizeCurrent(ctx, authentication, factor, action, purpose); err != nil {
		return binding, profile, err
	}
	if err := service.auditProfile(ctx, profile, true); err != nil {
		return binding, profile, err
	}
	return binding, profile, nil
}

func (service *Service) current(ctx context.Context) (OrganisationBinding, RuntimeProfile, error) {
	var binding OrganisationBinding
	err := service.units.Inspect(ctx, func(queryCtx context.Context, executor QueryExecutor) error {
		var currentErr error
		binding, currentErr = service.organisation.Current(queryCtx, executor, service.now().UTC())
		return currentErr
	})
	if err != nil {
		return binding, RuntimeProfile{}, connect.NewError(connect.CodeFailedPrecondition, ErrService)
	}
	profile, err := service.profiles.Current(ctx, service.now().UTC())
	if err != nil || !validCurrent(binding, profile, service.now().UTC()) {
		_ = profile.Close()
		return binding, profile, connect.NewError(connect.CodeFailedPrecondition, ErrService)
	}
	if profile.lease == nil {
		profile = BindRuntimeProfileLease(profile, staticProfileLease{HelperPort: service.helper})
	}
	return binding, profile.Clone(), nil
}

type staticProfileLease struct{ HelperPort }

func (staticProfileLease) Close() error { return nil }

func (service *Service) auditProfile(ctx context.Context, profile RuntimeProfile, accepted bool) error {
	record := AuditRecord{Action: AuditProfileRejected, StatusCode: "SBR_PROFILE_REJECTED"}
	if accepted {
		record = AuditRecord{Action: AuditProfileAccepted, ProfileFingerprint: profile.ProfileFingerprint,
			ComponentFingerprint: profile.ComponentFingerprint, StatusCode: "SBR_PROFILE_ACCEPTED"}
	}
	if err := service.recordAudit(ctx, record); err != nil {
		return connect.NewError(connect.CodeInternal, ErrService)
	}
	return nil
}

func (service *Service) rejectProfileAfterAuthorization(ctx context.Context, authentication *tammyv1.AuthenticationContext,
	action authorisation.Action, profile RuntimeProfile, currentErr error,
) error {
	if err := service.validateCurrent(ctx, authentication, nil, action, ""); err != nil {
		return err
	}
	if err := service.auditProfile(ctx, profile, false); err != nil {
		return err
	}
	return currentErr
}

func (service *Service) authorizeCurrent(ctx context.Context, authentication *tammyv1.AuthenticationContext, factor *tammyv1.FreshFactorContext, action authorisation.Action, purpose string) error {
	err := service.units.Mutate(ctx, func(txctx context.Context, executor MutationExecutor) error {
		if err := service.identity.AuthorizeWithin(txctx, executor, authentication, action); err != nil {
			return err
		}
		if purpose != "" {
			if err := service.identity.ConsumeFreshFactorWithin(txctx, executor, authentication, factor, purpose); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return connect.NewError(connect.CodePermissionDenied, ErrService)
	}
	return nil
}

func (service *Service) validateCurrent(ctx context.Context, authentication *tammyv1.AuthenticationContext, factor *tammyv1.FreshFactorContext, action authorisation.Action, purpose string) error {
	err := service.units.Mutate(ctx, func(txctx context.Context, executor MutationExecutor) error {
		if err := service.identity.ValidateAuthorizationWithin(txctx, executor, authentication, action); err != nil {
			return err
		}
		if purpose != "" {
			return service.identity.ValidateFreshFactorWithin(txctx, executor, authentication, factor, purpose)
		}
		return nil
	})
	if err != nil {
		return connect.NewError(connect.CodePermissionDenied, ErrService)
	}
	return nil
}

func (service *Service) validateFreshFactor(ctx context.Context, authentication *tammyv1.AuthenticationContext,
	factor *tammyv1.FreshFactorContext, purpose string,
) error {
	err := service.units.Mutate(ctx, func(txctx context.Context, executor MutationExecutor) error {
		return service.identity.ValidateFreshFactorWithin(txctx, executor, authentication, factor, purpose)
	})
	if err != nil {
		return connect.NewError(connect.CodePermissionDenied, ErrService)
	}
	return nil
}

func (service *Service) authorizeMutationWithin(ctx context.Context, executor MutationExecutor, authentication *tammyv1.AuthenticationContext,
	factor *tammyv1.FreshFactorContext, action authorisation.Action, purpose string,
) error {
	if err := service.identity.AuthorizeWithin(ctx, executor, authentication, action); err != nil {
		return err
	}
	if purpose != "" {
		if err := service.identity.ConsumeFreshFactorWithin(ctx, executor, authentication, factor, purpose); err != nil {
			return err
		}
	}
	return nil
}

func (service *Service) recordAudit(ctx context.Context, record AuditRecord) error {
	return service.units.Mutate(ctx, func(txctx context.Context, executor MutationExecutor) error {
		return service.audit.Record(txctx, executor, record)
	})
}

func validCurrent(binding OrganisationBinding, profile RuntimeProfile, now time.Time) bool {
	valid := ids.IsCanonicalV7(binding.OrganisationID) && abn.Valid(binding.CanonicalABN) && binding.VerificationExpiresAt.After(now) &&
		(profile.Environment == tammyv1.SbrEnvironment_SBR_ENVIRONMENT_SIMULATOR || profile.Environment == tammyv1.SbrEnvironment_SBR_ENVIRONMENT_EVTE) &&
		profile.AuthenticatedUntil.After(now) && profile.ProfileFingerprint != [sha256.Size]byte{} &&
		profile.RegistrationFingerprint != [sha256.Size]byte{} && profile.ComponentFingerprint != [sha256.Size]byte{} &&
		len(profile.ComponentVersion) >= 1 && len(profile.ComponentVersion) <= 128
	if !valid {
		return false
	}
	if profile.Environment == tammyv1.SbrEnvironment_SBR_ENVIRONMENT_SIMULATOR {
		return profile.ExpectedProductIdentifier == "" && profile.ExpectedServiceID == "" &&
			profile.ProductScopeFingerprint == [sha256.Size]byte{}
	}
	return len(profile.ExpectedProductIdentifier) >= 1 && len(profile.ExpectedProductIdentifier) <= 128 &&
		len(profile.ExpectedServiceID) >= 1 && len(profile.ExpectedServiceID) <= 128 &&
		profile.ProductScopeFingerprint == authenticatedProductScopeFingerprint(profile.ExpectedProductIdentifier, profile.ExpectedServiceID)
}

func (service *Service) helperRequest(operation HelperOperation, binding OrganisationBinding, profile RuntimeProfile) HelperRequest {
	scope := DeriveOpaqueScope(service.installationKey[:], service.workspaceID, binding.OrganisationID, binding.CanonicalABN)
	return HelperRequest{Operation: operation, Environment: profile.Environment, WorkspaceID: service.workspaceID,
		OrganisationID: binding.OrganisationID, CanonicalABN: binding.CanonicalABN, OpaqueScope: scope[:], EndpointProfile: bytes.Clone(profile.EndpointProfile),
		ProfileFingerprint: profile.ProfileFingerprint, RegistrationFingerprint: profile.RegistrationFingerprint,
		ComponentFingerprint: profile.ComponentFingerprint, ComponentVersion: profile.ComponentVersion}
}

func (service *Service) GetSbrReadiness(ctx context.Context, request *connect.Request[tammyv1.GetSbrReadinessRequest]) (*connect.Response[tammyv1.GetSbrReadinessResponse], error) {
	if request == nil || request.Msg == nil || request.Msg.Authentication == nil {
		return nil, invalid()
	}
	binding, profile, err := service.authorize(ctx, request.Msg.Authentication, nil, authorisation.ActionInspectSBR, "")
	defer profile.Close()
	if err != nil {
		return nil, err
	}
	stored, ok := service.store.Current(ctx, binding)
	return connect.NewResponse(&tammyv1.GetSbrReadinessResponse{Readiness: service.readiness(ctx, binding, profile, stored, ok)}), nil
}

func (service *Service) GetMachineCredentialStatus(ctx context.Context, request *connect.Request[tammyv1.GetMachineCredentialStatusRequest]) (*connect.Response[tammyv1.GetMachineCredentialStatusResponse], error) {
	if request == nil || request.Msg == nil || request.Msg.Authentication == nil {
		return nil, invalid()
	}
	binding, profile, err := service.authorize(ctx, request.Msg.Authentication, nil, authorisation.ActionInspectSBR, "")
	defer profile.Close()
	if err != nil {
		return nil, err
	}
	stored, ok := service.store.Current(ctx, binding)
	if !ok {
		return connect.NewResponse(&tammyv1.GetMachineCredentialStatusResponse{CredentialStatus: missingCredential()}), nil
	}
	if !usableCredentialBinding(stored, binding, profile, service.now().UTC()) {
		return connect.NewResponse(&tammyv1.GetMachineCredentialStatusResponse{CredentialStatus: credentialProjection(stored.metadata)}), nil
	}
	helperRequest := service.helperRequest(HelperOperationStatus, binding, profile)
	requestID, idErr := service.newID()
	if idErr != nil || !ids.IsCanonicalV7(requestID) {
		return nil, connect.NewError(connect.CodeInternal, ErrService)
	}
	helperRequest.RequestID = requestID
	defer helperRequest.ClearSecrets()
	result, err := profile.Execute(ctx, helperRequest)
	if err != nil {
		if auditErr := service.recordAudit(ctx, AuditRecord{Action: AuditCredentialFailed, CredentialFingerprint: stored.metadata.Fingerprint, StatusCode: "SBR_HELPER_FAILED"}); auditErr != nil {
			return nil, connect.NewError(connect.CodeInternal, ErrService)
		}
		return nil, connect.NewError(connect.CodeUnavailable, ErrService)
	}
	if !validStatusHelperResponse(helperRequest, result, binding, profile, service.now().UTC()) || result.Credential.Fingerprint != stored.metadata.Fingerprint {
		return nil, connect.NewError(connect.CodeFailedPrecondition, ErrService)
	}
	return connect.NewResponse(&tammyv1.GetMachineCredentialStatusResponse{CredentialStatus: credentialProjection(result.Credential)}), nil
}

func (service *Service) ImportMachineCredential(ctx context.Context, request *connect.Request[tammyv1.ImportMachineCredentialRequest]) (*connect.Response[tammyv1.ImportMachineCredentialResponse], error) {
	if request == nil || request.Msg == nil {
		return nil, invalid()
	}
	path := strings.Clone(request.Msg.SelectedLocalPath)
	request.Msg.SelectedLocalPath = ""
	bookmark := bytes.Clone(request.Msg.SecurityScopedBookmark)
	clear(request.Msg.SecurityScopedBookmark)
	request.Msg.SecurityScopedBookmark = nil
	password := bytes.Clone(request.Msg.Password)
	clear(request.Msg.Password)
	request.Msg.Password = nil
	defer func() { clear(bookmark); clear(password) }()
	if request.Msg.CommandContext == nil || path == "" || len(path) > 4096 || len(password) > 1024 || len(bookmark) > 65536 {
		return nil, invalid()
	}
	metadata, err := service.credentialMutation(ctx, request.Msg.CommandContext, authorisation.ActionImportSBRMachineCredential, PurposeImportMachineCredential,
		MutationImportCredential, path, bookmark, password)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&tammyv1.ImportMachineCredentialResponse{CredentialStatus: credentialProjection(metadata)}), nil
}

func (service *Service) UnlockMachineCredential(ctx context.Context, request *connect.Request[tammyv1.UnlockMachineCredentialRequest]) (*connect.Response[tammyv1.UnlockMachineCredentialResponse], error) {
	if request == nil || request.Msg == nil {
		return nil, invalid()
	}
	password := bytes.Clone(request.Msg.Password)
	clear(request.Msg.Password)
	request.Msg.Password = nil
	defer clear(password)
	if request.Msg.CommandContext == nil || len(password) > 1024 {
		return nil, invalid()
	}
	binding, profile, err := service.current(ctx)
	defer profile.Close()
	if err != nil {
		return nil, service.rejectProfileAfterAuthorization(ctx, request.Msg.CommandContext.Authentication,
			authorisation.ActionUnlockSBRMachineCredential, profile, err)
	}
	stored, ok := service.store.Current(ctx, binding)
	if !ok || !usableCredentialBinding(stored, binding, profile, service.now().UTC()) {
		return nil, connect.NewError(connect.CodeFailedPrecondition, ErrService)
	}
	if err := service.validateCurrent(ctx, request.Msg.CommandContext.Authentication, request.Msg.CommandContext.FreshFactor,
		authorisation.ActionUnlockSBRMachineCredential, PurposeUnlockMachineCredential); err != nil {
		return nil, err
	}
	if err := service.auditProfile(ctx, profile, true); err != nil {
		return nil, err
	}
	operationID, idErr := service.newID()
	if idErr != nil || !ids.IsCanonicalV7(operationID) {
		return nil, connect.NewError(connect.CodeInternal, ErrService)
	}
	dispatch := HelperDispatchRecord{OperationID: operationID,
		ActorUserID: request.Msg.CommandContext.Authentication.GetActorUserId(),
		Key: BindingKey{WorkspaceID: service.workspaceID, OrganisationID: binding.OrganisationID,
			CanonicalABN: binding.CanonicalABN, SchemaVersion: 1, CredentialFingerprint: stored.metadata.Fingerprint},
		IdempotencyKey: request.Msg.CommandContext.IdempotencyKey,
		SemanticHash:   unlockSemanticHash(binding, profile, stored.metadata.Fingerprint), State: HelperDispatching,
		CreatedAt: service.now().UTC().Format("2006-01-02T15:04:05.000000000Z"),
		UpdatedAt: service.now().UTC().Format("2006-01-02T15:04:05.000000000Z")}
	if err := service.store.ReserveUnlockDispatch(ctx, dispatch, func(txctx context.Context, executor MutationExecutor) error {
		if err := service.identity.AuthorizeWithin(txctx, executor, request.Msg.CommandContext.Authentication,
			authorisation.ActionUnlockSBRMachineCredential); err != nil {
			return err
		}
		return service.identity.ConsumeFreshFactorWithin(txctx, executor, request.Msg.CommandContext.Authentication,
			request.Msg.CommandContext.FreshFactor, PurposeUnlockMachineCredential)
	}); err != nil {
		code := connect.CodePermissionDenied
		if errors.Is(err, ErrIdempotencyConflict) || errors.Is(err, ErrUncertainTransport) || errors.Is(err, ErrConflict) {
			code = connect.CodeAlreadyExists
		}
		return nil, connect.NewError(code, ErrService)
	}
	helperRequest := service.helperRequest(HelperOperationUnlock, binding, profile)
	helperRequest.RequestID, helperRequest.OperationID = operationID, operationID
	helperRequest.Password = bytes.Clone(password)
	defer helperRequest.ClearSecrets()
	result, err := profile.Execute(ctx, helperRequest)
	if err != nil || !validUnlockHelperResponse(helperRequest, result, binding, profile, service.now().UTC()) || result.Credential.Fingerprint != stored.metadata.Fingerprint {
		terminal := HelperDispatchFailed
		if err != nil {
			terminal = HelperDispatchUnknown
		}
		if finishErr := service.store.FinishUnlockDispatch(ctx, dispatch, terminal); finishErr != nil {
			return nil, connect.NewError(connect.CodeInternal, ErrService)
		}
		if auditErr := service.recordAudit(ctx, AuditRecord{Action: AuditCredentialFailed, CredentialFingerprint: stored.metadata.Fingerprint, StatusCode: "SBR_CREDENTIAL_UNLOCK_FAILED"}); auditErr != nil {
			return nil, connect.NewError(connect.CodeInternal, ErrService)
		}
		return nil, connect.NewError(connect.CodeFailedPrecondition, ErrService)
	}
	audit := AuditRecord{Action: AuditCredentialUnlocked, CredentialFingerprint: result.Credential.Fingerprint, StatusCode: "SBR_CREDENTIAL_UNLOCKED"}
	if err := service.store.FinishUnlockDispatch(ctx, dispatch, HelperDispatchCompleted); err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrService)
	}
	if err := service.recordAudit(ctx, audit); err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrService)
	}
	return connect.NewResponse(&tammyv1.UnlockMachineCredentialResponse{CredentialStatus: credentialProjection(result.Credential)}), nil
}

func (service *Service) ReplaceMachineCredential(ctx context.Context, request *connect.Request[tammyv1.ReplaceMachineCredentialRequest]) (*connect.Response[tammyv1.ReplaceMachineCredentialResponse], error) {
	if request == nil || request.Msg == nil {
		return nil, invalid()
	}
	path := strings.Clone(request.Msg.SelectedLocalPath)
	request.Msg.SelectedLocalPath = ""
	bookmark := bytes.Clone(request.Msg.SecurityScopedBookmark)
	clear(request.Msg.SecurityScopedBookmark)
	request.Msg.SecurityScopedBookmark = nil
	password := bytes.Clone(request.Msg.Password)
	clear(request.Msg.Password)
	request.Msg.Password = nil
	defer func() { clear(bookmark); clear(password) }()
	if request.Msg.CommandContext == nil || path == "" || len(path) > 4096 || len(password) > 1024 || len(bookmark) > 65536 {
		return nil, invalid()
	}
	metadata, err := service.credentialMutation(ctx, request.Msg.CommandContext, authorisation.ActionReplaceSBRMachineCredential, PurposeReplaceMachineCredential,
		MutationReplaceCredential, path, bookmark, password)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&tammyv1.ReplaceMachineCredentialResponse{CredentialStatus: credentialProjection(metadata)}), nil
}

func (service *Service) RemoveMachineCredential(ctx context.Context, request *connect.Request[tammyv1.RemoveMachineCredentialRequest]) (*connect.Response[tammyv1.RemoveMachineCredentialResponse], error) {
	if request == nil || request.Msg == nil || request.Msg.CommandContext == nil {
		return nil, invalid()
	}
	_, err := service.credentialMutation(ctx, request.Msg.CommandContext, authorisation.ActionRemoveSBRMachineCredential, PurposeRemoveMachineCredential,
		MutationRemoveCredential, "", nil, nil)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&tammyv1.RemoveMachineCredentialResponse{CredentialStatus: missingCredential()}), nil
}

func (service *Service) credentialMutation(ctx context.Context, command *tammyv1.CommandContext, action authorisation.Action, purpose string, kind MutationKind, path string, bookmark, password []byte) (CredentialMetadata, error) {
	binding, profile, err := service.current(ctx)
	defer profile.Close()
	if err != nil {
		return CredentialMetadata{}, service.rejectProfileAfterAuthorization(ctx, command.Authentication, action, profile, err)
	}
	if kind != MutationImportCredential {
		stored, ok := service.store.Current(ctx, binding)
		if !ok || !usableCredentialBinding(stored, binding, profile, service.now().UTC()) {
			return CredentialMetadata{}, connect.NewError(connect.CodeFailedPrecondition, ErrService)
		}
	}
	if err := service.validateCurrent(ctx, command.Authentication, nil, action, ""); err != nil {
		return CredentialMetadata{}, err
	}
	if err := service.auditProfile(ctx, profile, true); err != nil {
		return CredentialMetadata{}, err
	}
	semantic := mutationSemanticHash(kind, binding, profile, path, bookmark, password, nil, "", "")
	owned, replay, lookupErr := service.store.Command(ctx, binding, command.IdempotencyKey, semantic)
	if errors.Is(lookupErr, ErrIdempotencyConflict) {
		return CredentialMetadata{}, connect.NewError(connect.CodeAlreadyExists, ErrIdempotencyConflict)
	}
	if lookupErr != nil {
		return CredentialMetadata{}, connect.NewError(connect.CodeInternal, ErrService)
	}
	if replay {
		if owned.ActorUserID != command.Authentication.ActorUserId {
			return CredentialMetadata{}, connect.NewError(connect.CodePermissionDenied, ErrService)
		}
		if !owned.Completed {
			return CredentialMetadata{}, connect.NewError(connect.CodeUnavailable, ErrService)
		}
		return owned.Credential, nil
	}
	operationID, err := service.newID()
	if err != nil || !ids.IsCanonicalV7(operationID) {
		return CredentialMetadata{}, connect.NewError(connect.CodeInternal, ErrService)
	}
	if err := service.validateFreshFactor(ctx, command.Authentication, command.FreshFactor, purpose); err != nil {
		return CredentialMetadata{}, err
	}
	if err := service.store.Prepare(ctx, operationID, command.Authentication.ActorUserId, kind, binding, command.IdempotencyKey, semantic,
		func(reserveCtx context.Context, executor MutationExecutor) error {
			if err := service.authorizeMutationWithin(reserveCtx, executor, command.Authentication, command.FreshFactor, action, purpose); err != nil {
				return ErrPermissionDenied
			}
			return nil
		}); err != nil {
		if errors.Is(err, ErrPermissionDenied) {
			return CredentialMetadata{}, connect.NewError(connect.CodePermissionDenied, ErrService)
		}
		return CredentialMetadata{}, connect.NewError(connect.CodeAlreadyExists, ErrService)
	}
	helperRequest := service.helperRequest(HelperOperationPrepareMutation, binding, profile)
	helperRequest.RequestID, helperRequest.OperationID, helperRequest.MutationKind = operationID, operationID, kind
	helperRequest.SelectedLocalPath, helperRequest.Bookmark, helperRequest.Password = path, bytes.Clone(bookmark), bytes.Clone(password)
	defer helperRequest.ClearSecrets()
	result, helperErr := profile.Execute(ctx, helperRequest)
	if helperErr != nil || !validPreparedResponseEnvelope(helperRequest, result, profile) {
		var abortErr error
		if helperErr == nil {
			abortErr = service.rejectPreparedMutation(ctx, profile, operationID, helperRequest, result)
		} else {
			abortErr = service.store.Abort(ctx, operationID)
		}
		if abortErr != nil {
			return CredentialMetadata{}, connect.NewError(connect.CodeInternal, ErrService)
		}
		if auditErr := service.recordAudit(ctx, AuditRecord{Action: AuditCredentialFailed, StatusCode: "SBR_HELPER_FAILED"}); auditErr != nil {
			return CredentialMetadata{}, connect.NewError(connect.CodeInternal, ErrService)
		}
		return CredentialMetadata{}, connect.NewError(connect.CodeUnavailable, ErrService)
	}
	if !validPreparedCredentialMetadata(helperRequest, result, binding, profile, service.now().UTC()) {
		if abortErr := service.rejectPreparedMutation(ctx, profile, operationID, helperRequest, result); abortErr != nil {
			return CredentialMetadata{}, connect.NewError(connect.CodeInternal, ErrService)
		}
		action, code := AuditCredentialFailed, "SBR_CREDENTIAL_INVALID"
		if !result.Credential.ExpiresAt.After(service.now().UTC()) {
			action, code = AuditCredentialExpired, "SBR_CREDENTIAL_EXPIRED"
		}
		if auditErr := service.recordAudit(ctx, AuditRecord{Action: action, CredentialFingerprint: result.Credential.Fingerprint, StatusCode: code}); auditErr != nil {
			return CredentialMetadata{}, connect.NewError(connect.CodeInternal, ErrService)
		}
		return CredentialMetadata{}, connect.NewError(connect.CodeFailedPrecondition, ErrService)
	}
	if err := service.store.Stage(ctx, operationID, result); err != nil {
		if abortErr := service.abortHelper(ctx, profile, helperRequest, result.PendingID); abortErr != nil {
			return CredentialMetadata{}, connect.NewError(connect.CodeUnavailable, ErrService)
		}
		return CredentialMetadata{}, connect.NewError(connect.CodeInternal, ErrService)
	}
	stored := serviceBinding{metadata: result.Credential, profile: profile, state: tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT}
	auditAction := AuditCredentialImported
	if kind == MutationReplaceCredential {
		auditAction = AuditCredentialReplaced
	}
	if kind == MutationRemoveCredential {
		auditAction = AuditCredentialRemoved
	}
	auditRecord := AuditRecord{Action: auditAction, CredentialFingerprint: result.Credential.Fingerprint,
		ProfileFingerprint: profile.ProfileFingerprint, ComponentFingerprint: profile.ComponentFingerprint, StatusCode: string(auditAction)}
	if err := service.store.Commit(ctx, operationID, stored, auditRecord, func(context.Context, MutationExecutor) error {
		return nil
	}); err != nil {
		if abortErr := service.abortStagedMutation(ctx, profile, operationID, helperRequest, result.PendingID); abortErr != nil {
			return CredentialMetadata{}, connect.NewError(connect.CodeInternal, ErrService)
		}
		return CredentialMetadata{}, connect.NewError(connect.CodeInternal, ErrService)
	}
	commitRequest := service.helperRequest(HelperOperationCommitMutation, binding, profile)
	commitRequest.RequestID, commitRequest.OperationID, commitRequest.PendingID, commitRequest.MutationKind = operationID, operationID, result.PendingID, kind
	defer commitRequest.ClearSecrets()
	committed, err := profile.Execute(ctx, commitRequest)
	if err != nil || !validCommittedCredentialResponse(commitRequest, committed, result, profile) {
		return CredentialMetadata{}, connect.NewError(connect.CodeUnavailable, ErrService)
	}
	if err := service.store.Finish(ctx, operationID, func(auditCtx context.Context, executor MutationExecutor, record AuditRecord) error {
		return service.audit.Record(auditCtx, executor, record)
	}); err != nil {
		return CredentialMetadata{}, connect.NewError(connect.CodeInternal, ErrService)
	}
	return result.Credential, nil
}

func usableCredentialBinding(stored serviceBinding, binding OrganisationBinding, profile RuntimeProfile, now time.Time) bool {
	if stored.state != tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT ||
		(stored.bindingState != "" && stored.bindingState != "ACTIVE") ||
		stored.metadata.Fingerprint == [sha256.Size]byte{} || stored.metadata.CanonicalABN != binding.CanonicalABN ||
		!stored.metadata.ExpiresAt.After(now) {
		return false
	}
	return stored.profile.Environment == profile.Environment &&
		stored.profile.ProfileFingerprint == profile.ProfileFingerprint &&
		stored.profile.RegistrationFingerprint == profile.RegistrationFingerprint &&
		stored.profile.ComponentFingerprint == profile.ComponentFingerprint
}

func mutationSemanticHash(kind MutationKind, binding OrganisationBinding, profile RuntimeProfile, path string, bookmark, password, productID []byte, product, serviceID string) [sha256.Size]byte {
	digest := sha256.New()
	writeSemanticField := func(value []byte) {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(value)))
		_, _ = digest.Write(length[:])
		_, _ = digest.Write(value)
	}
	writeSemanticField([]byte("tammy.sbr.command.v1"))
	writeSemanticField([]byte(kind))
	writeSemanticField([]byte(binding.OrganisationID))
	writeSemanticField([]byte(binding.CanonicalABN))
	writeSemanticField(profile.ProfileFingerprint[:])
	writeSemanticField(profile.RegistrationFingerprint[:])
	writeSemanticField(profile.ComponentFingerprint[:])
	writeSemanticField([]byte(path))
	for _, transient := range [][]byte{bookmark, password, productID} {
		hash := sha256.Sum256(transient)
		writeSemanticField(hash[:])
	}
	writeSemanticField([]byte(product))
	writeSemanticField([]byte(serviceID))
	var result [sha256.Size]byte
	copy(result[:], digest.Sum(nil))
	return result
}

func validCredential(metadata CredentialMetadata, binding OrganisationBinding, now time.Time) bool {
	return metadata.Fingerprint != [sha256.Size]byte{} && metadata.CanonicalABN == binding.CanonicalABN &&
		metadata.ComponentVersion != "" && len(metadata.ComponentVersion) <= 128 && metadata.ExpiresAt.After(now)
}

func validPreparedResponseEnvelope(request HelperRequest, result HelperResult, profile RuntimeProfile) bool {
	if result.RequestID != request.RequestID || result.Outcome != HelperOutcomePending || result.ResultCode != HelperResultNone ||
		!ids.IsCanonicalV7(result.PendingID) || result.ProfileFingerprint != profile.ProfileFingerprint ||
		result.RegistrationFingerprint != profile.RegistrationFingerprint ||
		result.ComponentFingerprint != profile.ComponentFingerprint || result.ComponentVersion != profile.ComponentVersion {
		return false
	}
	return true
}

func validStatusHelperResponse(request HelperRequest, result HelperResult, binding OrganisationBinding, profile RuntimeProfile, now time.Time) bool {
	return validCredentialHelperResponse(request, result, binding, profile, now) &&
		(result.ResultCode == HelperResultReady || result.ResultCode == HelperResultCredentialLocked)
}

func validUnlockHelperResponse(request HelperRequest, result HelperResult, binding OrganisationBinding, profile RuntimeProfile, now time.Time) bool {
	return validCredentialHelperResponse(request, result, binding, profile, now) && result.ResultCode == HelperResultReady
}

func validCredentialHelperResponse(request HelperRequest, result HelperResult, binding OrganisationBinding, profile RuntimeProfile, now time.Time) bool {
	credential := result.Credential
	return ids.IsCanonicalV7(request.RequestID) && result.RequestID == request.RequestID && result.Outcome == HelperOutcomeOK &&
		result.PendingID == "" && result.StableCode == "" && result.ProfileFingerprint == profile.ProfileFingerprint &&
		result.RegistrationFingerprint == profile.RegistrationFingerprint && result.ComponentFingerprint == profile.ComponentFingerprint &&
		result.ComponentVersion == profile.ComponentVersion && result.ProductState == 0 && result.ProductFingerprint == [sha256.Size]byte{} &&
		result.FixtureState == "" && result.FixtureFailureCase == tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_UNSPECIFIED &&
		credential.Fingerprint != [sha256.Size]byte{} && credential.CanonicalABN == binding.CanonicalABN &&
		credential.ComponentVersion == profile.ComponentVersion && credential.State == tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT &&
		!credential.CreatedAt.IsZero() && !credential.CreatedAt.After(now.Add(5*time.Minute)) && credential.CreatedAt.Before(credential.ExpiresAt) &&
		credential.ExpiresAt.After(now)
}

func validPreparedCredentialMetadata(request HelperRequest, result HelperResult, binding OrganisationBinding, profile RuntimeProfile, now time.Time) bool {
	if request.MutationKind == MutationRemoveCredential {
		return result.Credential == (CredentialMetadata{})
	}
	return validCredential(result.Credential, binding, now) &&
		result.Credential.State == tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT &&
		result.Credential.ComponentVersion == profile.ComponentVersion &&
		(result.Credential.CreatedAt.IsZero() || (!result.Credential.CreatedAt.After(now.Add(5*time.Minute)) &&
			result.Credential.CreatedAt.Before(result.Credential.ExpiresAt)))
}

func (service *Service) abortHelper(ctx context.Context, profile RuntimeProfile, prepared HelperRequest, pendingID string) error {
	requestProfile := profile.Clone()
	requestProfile.Environment, requestProfile.EndpointProfile = prepared.Environment, bytes.Clone(prepared.EndpointProfile)
	request := service.helperRequest(HelperOperationAbortMutation, OrganisationBinding{OrganisationID: prepared.OrganisationID, CanonicalABN: prepared.CanonicalABN, VerificationExpiresAt: service.now().Add(time.Minute)}, requestProfile)
	request.OperationID, request.PendingID, request.MutationKind = prepared.OperationID, pendingID, prepared.MutationKind
	request.ProductIdentifier, request.ServiceIdentifier = prepared.ProductIdentifier, prepared.ServiceIdentifier
	request.RequestID = prepared.RequestID
	result, err := profile.Execute(ctx, request)
	request.ClearSecrets()
	if err != nil || !validClosedHelperResponse(request, result, HelperResultMutationAborted) {
		return ErrService
	}
	return nil
}

func (service *Service) rejectPreparedMutation(ctx context.Context, profile RuntimeProfile, operationID string, request HelperRequest, result HelperResult) error {
	if !ids.IsCanonicalV7(result.PendingID) {
		return service.store.Abort(ctx, operationID)
	}
	if err := service.store.Stage(ctx, operationID, result); err != nil {
		helperErr := service.abortHelper(ctx, profile, request, result.PendingID)
		storeErr := service.store.Abort(ctx, operationID)
		if helperErr != nil || storeErr != nil {
			return ErrService
		}
		return nil
	}
	if err := service.store.Abort(ctx, operationID); err != nil {
		return err
	}
	if err := service.abortHelper(ctx, profile, request, result.PendingID); err != nil {
		return err
	}
	return service.store.FinishAbort(ctx, operationID)
}

func (service *Service) abortStagedMutation(ctx context.Context, profile RuntimeProfile, operationID string, request HelperRequest, pendingID string) error {
	if err := service.store.Abort(ctx, operationID); err != nil {
		return err
	}
	if err := service.abortHelper(ctx, profile, request, pendingID); err != nil {
		return err
	}
	return service.store.FinishAbort(ctx, operationID)
}

func validClosedHelperResponse(request HelperRequest, result HelperResult, expected HelperResultCode) bool {
	return result.RequestID == request.RequestID && result.Outcome == HelperOutcomeOK &&
		result.ResultCode == expected && result.PendingID == "" && result.StableCode == ""
}

func validCommittedMutationEnvelope(request HelperRequest, result HelperResult, profile RuntimeProfile) bool {
	return (request.Operation == HelperOperationCommitMutation || request.Operation == HelperOperationReconcileMutation) &&
		ids.IsCanonicalV7(request.RequestID) && validClosedHelperResponse(request, result, HelperResultMutationCommitted) &&
		result.ProfileFingerprint == profile.ProfileFingerprint && result.RegistrationFingerprint == profile.RegistrationFingerprint &&
		result.ComponentFingerprint == profile.ComponentFingerprint && result.ComponentVersion == profile.ComponentVersion &&
		result.FixtureState == "" && result.FixtureFailureCase == tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_UNSPECIFIED
}

func validCommittedCredentialResponse(request HelperRequest, result, prepared HelperResult, profile RuntimeProfile) bool {
	if !validCommittedMutationEnvelope(request, result, profile) || result.ProductState != 0 ||
		result.ProductFingerprint != [sha256.Size]byte{} || request.ProductIdentifier != "" || request.ServiceIdentifier != "" {
		return false
	}
	if request.MutationKind == MutationRemoveCredential {
		return result.Credential == (CredentialMetadata{}) && prepared.Credential == (CredentialMetadata{})
	}
	return (request.MutationKind == MutationImportCredential || request.MutationKind == MutationReplaceCredential) &&
		result.Credential == prepared.Credential
}

func (service *Service) ImportSbrProductId(ctx context.Context, request *connect.Request[tammyv1.ImportSbrProductIdRequest]) (*connect.Response[tammyv1.ImportSbrProductIdResponse], error) {
	if request == nil || request.Msg == nil {
		return nil, invalid()
	}
	value := []byte(request.Msg.ProductIdValue)
	request.Msg.ProductIdValue = ""
	product, serviceID := strings.Clone(request.Msg.EvteProductIdentifier), strings.Clone(request.Msg.EvteServiceIdentifier)
	request.Msg.EvteProductIdentifier, request.Msg.EvteServiceIdentifier = "", ""
	defer clear(value)
	if request.Msg.CommandContext == nil || len(value) == 0 || len(value) > 1024 {
		return nil, invalid()
	}
	state, err := service.productMutation(ctx, request.Msg.CommandContext, PurposeImportProductID, MutationImportProductID, value, product, serviceID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&tammyv1.ImportSbrProductIdResponse{ProductIdState: productProjection(state)}), nil
}

func (service *Service) RemoveSbrProductId(ctx context.Context, request *connect.Request[tammyv1.RemoveSbrProductIdRequest]) (*connect.Response[tammyv1.RemoveSbrProductIdResponse], error) {
	if request == nil || request.Msg == nil {
		return nil, invalid()
	}
	product, serviceID := strings.Clone(request.Msg.EvteProductIdentifier), strings.Clone(request.Msg.EvteServiceIdentifier)
	request.Msg.EvteProductIdentifier, request.Msg.EvteServiceIdentifier = "", ""
	if request.Msg.CommandContext == nil {
		return nil, invalid()
	}
	state, err := service.productMutation(ctx, request.Msg.CommandContext, PurposeRemoveProductID, MutationRemoveProductID, nil, product, serviceID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&tammyv1.RemoveSbrProductIdResponse{ProductIdState: productProjection(state)}), nil
}

func (service *Service) productMutation(ctx context.Context, command *tammyv1.CommandContext, purpose string, kind MutationKind, value []byte, product, serviceID string) (ProductState, error) {
	if product == "" || serviceID == "" || len(product) > 128 || len(serviceID) > 128 {
		clear(value)
		return 0, invalid()
	}
	binding, profile, err := service.current(ctx)
	defer profile.Close()
	if err != nil {
		clear(value)
		return 0, service.rejectProfileAfterAuthorization(ctx, command.Authentication, authorisation.ActionManageSBRProductID, profile, err)
	}
	if profile.Environment != tammyv1.SbrEnvironment_SBR_ENVIRONMENT_EVTE {
		clear(value)
		return 0, connect.NewError(connect.CodeFailedPrecondition, ErrService)
	}
	if product != profile.ExpectedProductIdentifier || serviceID != profile.ExpectedServiceID ||
		profile.ProductScopeFingerprint != authenticatedProductScopeFingerprint(product, serviceID) {
		clear(value)
		return 0, connect.NewError(connect.CodeFailedPrecondition, ErrService)
	}
	stored, ok := service.store.Current(ctx, binding)
	if !ok || !usableCredentialBinding(stored, binding, profile, service.now().UTC()) {
		clear(value)
		return 0, connect.NewError(connect.CodeFailedPrecondition, ErrService)
	}
	if err := service.validateCurrent(ctx, command.Authentication, nil, authorisation.ActionManageSBRProductID, ""); err != nil {
		clear(value)
		return 0, err
	}
	if err := service.auditProfile(ctx, profile, true); err != nil {
		clear(value)
		return 0, err
	}
	semantic := mutationSemanticHash(kind, binding, profile, "", nil, nil, value, product, serviceID)
	owned, replay, lookupErr := service.store.Command(ctx, binding, command.IdempotencyKey, semantic)
	if errors.Is(lookupErr, ErrIdempotencyConflict) {
		clear(value)
		return 0, connect.NewError(connect.CodeAlreadyExists, ErrIdempotencyConflict)
	}
	if lookupErr != nil {
		clear(value)
		return 0, connect.NewError(connect.CodeInternal, ErrService)
	}
	if replay {
		clear(value)
		if owned.ActorUserID != command.Authentication.ActorUserId {
			return 0, connect.NewError(connect.CodePermissionDenied, ErrService)
		}
		if !owned.Completed {
			return 0, connect.NewError(connect.CodeUnavailable, ErrService)
		}
		return owned.Product, nil
	}
	operationID, err := service.newID()
	if err != nil {
		clear(value)
		return 0, connect.NewError(connect.CodeInternal, ErrService)
	}
	if err := service.validateFreshFactor(ctx, command.Authentication, command.FreshFactor, purpose); err != nil {
		clear(value)
		return 0, err
	}
	if err := service.store.Prepare(ctx, operationID, command.Authentication.ActorUserId, kind, binding, command.IdempotencyKey, semantic,
		func(reserveCtx context.Context, executor MutationExecutor) error {
			if err := service.authorizeMutationWithin(reserveCtx, executor, command.Authentication, command.FreshFactor,
				authorisation.ActionManageSBRProductID, purpose); err != nil {
				return ErrPermissionDenied
			}
			return nil
		}); err != nil {
		clear(value)
		if errors.Is(err, ErrPermissionDenied) {
			return 0, connect.NewError(connect.CodePermissionDenied, ErrService)
		}
		return 0, connect.NewError(connect.CodeAlreadyExists, ErrService)
	}
	helperRequest := service.helperRequest(HelperOperationPrepareMutation, binding, profile)
	helperRequest.RequestID, helperRequest.OperationID, helperRequest.MutationKind = operationID, operationID, kind
	helperRequest.ProductID = bytes.Clone(value)
	helperRequest.ProductIdentifier, helperRequest.ServiceIdentifier = product, serviceID
	clear(value)
	defer helperRequest.ClearSecrets()
	result, err := profile.Execute(ctx, helperRequest)
	if err != nil || !validPreparedProductResponse(helperRequest, result, profile) {
		var abortErr error
		if err == nil {
			abortErr = service.rejectPreparedMutation(ctx, profile, operationID, helperRequest, result)
		} else {
			abortErr = service.store.Abort(ctx, operationID)
		}
		if abortErr != nil {
			return 0, connect.NewError(connect.CodeInternal, ErrService)
		}
		return 0, connect.NewError(connect.CodeUnavailable, ErrService)
	}
	if err := service.store.Stage(ctx, operationID, result); err != nil {
		if abortErr := service.abortHelper(ctx, profile, helperRequest, result.PendingID); abortErr != nil {
			return 0, connect.NewError(connect.CodeUnavailable, ErrService)
		}
		return 0, connect.NewError(connect.CodeInternal, ErrService)
	}
	state := result.ProductState
	scopeFingerprint := profile.ProductScopeFingerprint
	auditRecord := AuditRecord{Action: AuditProductIDChanged, ProfileFingerprint: profile.ProfileFingerprint,
		ComponentFingerprint: profile.ComponentFingerprint, StatusCode: "SBR_PRODUCT_ID_STATE_CHANGED"}
	if err := service.store.Commit(ctx, operationID, serviceBinding{product: state, productScope: scopeFingerprint,
		productFingerprint: result.ProductFingerprint, profile: profile}, auditRecord, func(context.Context, MutationExecutor) error {
		return nil
	}); err != nil {
		if abortErr := service.abortStagedMutation(ctx, profile, operationID, helperRequest, result.PendingID); abortErr != nil {
			return 0, connect.NewError(connect.CodeInternal, ErrService)
		}
		return 0, connect.NewError(connect.CodeInternal, ErrService)
	}
	commit := service.helperRequest(HelperOperationCommitMutation, binding, profile)
	commit.RequestID, commit.OperationID, commit.PendingID, commit.MutationKind = operationID, operationID, result.PendingID, kind
	commit.ProductIdentifier, commit.ServiceIdentifier = product, serviceID
	committed, err := profile.Execute(ctx, commit)
	commit.ClearSecrets()
	if err != nil || !validCommittedProductResponse(commit, committed, result, profile) {
		return 0, connect.NewError(connect.CodeUnavailable, ErrService)
	}
	if err := service.store.Finish(ctx, operationID, func(auditCtx context.Context, executor MutationExecutor, record AuditRecord) error {
		return service.audit.Record(auditCtx, executor, record)
	}); err != nil {
		return 0, connect.NewError(connect.CodeInternal, ErrService)
	}
	return state, nil
}

func validPreparedProductResponse(request HelperRequest, result HelperResult, profile RuntimeProfile) bool {
	if !validPreparedResponseEnvelope(request, result, profile) || result.Credential != (CredentialMetadata{}) {
		return false
	}
	switch request.MutationKind {
	case MutationImportProductID:
		return (result.ProductState == ProductPresent || result.ProductState == ProductInaccessible) && result.ProductFingerprint != [sha256.Size]byte{}
	case MutationRemoveProductID:
		return result.ProductState == ProductMissing && result.ProductFingerprint == [sha256.Size]byte{}
	default:
		return false
	}
}

func validCommittedProductResponse(request HelperRequest, result, prepared HelperResult, profile RuntimeProfile) bool {
	return validCommittedMutationEnvelope(request, result, profile) && result.Credential == (CredentialMetadata{}) &&
		request.ProductIdentifier == profile.ExpectedProductIdentifier && request.ServiceIdentifier == profile.ExpectedServiceID &&
		profile.ProductScopeFingerprint == authenticatedProductScopeFingerprint(request.ProductIdentifier, request.ServiceIdentifier) &&
		result.ProductState == prepared.ProductState && result.ProductFingerprint == prepared.ProductFingerprint
}

func (service *Service) RunSbrReadinessFixture(ctx context.Context, request *connect.Request[tammyv1.RunSbrReadinessFixtureRequest]) (*connect.Response[tammyv1.RunSbrReadinessFixtureResponse], error) {
	if request == nil || request.Msg == nil || request.Msg.CommandContext == nil || request.Msg.FixtureId != ReadinessFixtureID || request.Msg.FailureCase == tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_UNKNOWN {
		return nil, invalid()
	}
	binding, profile, err := service.current(ctx)
	defer profile.Close()
	if err != nil {
		return nil, service.rejectProfileAfterAuthorization(ctx, request.Msg.CommandContext.Authentication,
			authorisation.ActionUseSBRMachineCredential, profile, err)
	}
	if profile.Environment != tammyv1.SbrEnvironment_SBR_ENVIRONMENT_SIMULATOR {
		return nil, connect.NewError(connect.CodeFailedPrecondition, ErrService)
	}
	stored, ok := service.store.Current(ctx, binding)
	if !ok || !usableCredentialBinding(stored, binding, profile, service.now().UTC()) {
		return nil, connect.NewError(connect.CodeFailedPrecondition, ErrService)
	}
	if err := service.validateCurrent(ctx, request.Msg.CommandContext.Authentication, nil,
		authorisation.ActionUseSBRMachineCredential, ""); err != nil {
		return nil, err
	}
	if err := service.auditProfile(ctx, profile, true); err != nil {
		return nil, err
	}
	operationID, idErr := service.newID()
	if idErr != nil || !ids.IsCanonicalV7(operationID) {
		return nil, connect.NewError(connect.CodeInternal, ErrService)
	}
	semantic := fixtureSemanticHash(request.Msg, profile, stored.metadata.Fingerprint)
	fixture, replay, prepareErr := service.store.PrepareFixture(ctx, binding, stored.metadata.Fingerprint, operationID,
		request.Msg.CommandContext.Authentication.GetActorUserId(),
		request.Msg.CommandContext.IdempotencyKey, semantic)
	if errors.Is(prepareErr, ErrIdempotencyConflict) {
		return nil, connect.NewError(connect.CodeAlreadyExists, ErrIdempotencyConflict)
	}
	if errors.Is(prepareErr, ErrUncertainTransport) {
		return nil, connect.NewError(connect.CodeFailedPrecondition, ErrUncertainTransport)
	}
	if prepareErr != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrService)
	}
	if replay {
		if fixture.ActorUserID != request.Msg.CommandContext.Authentication.GetActorUserId() {
			return nil, connect.NewError(connect.CodePermissionDenied, ErrService)
		}
		return service.fixtureResponse(ctx, request.Msg, binding, profile, stored, fixture.State), nil
	}
	if err := service.validateFreshFactor(ctx, request.Msg.CommandContext.Authentication,
		request.Msg.CommandContext.FreshFactor, PurposeUseMachineCredential); err != nil {
		return nil, err
	}
	if err := service.recordAudit(ctx, AuditRecord{Action: AuditFixturePrepared, CredentialFingerprint: stored.metadata.Fingerprint, StatusCode: "SBR_HELPER_FIXTURE_PREPARED"}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrService)
	}
	caseValue := simulatorCase(request.Msg.FailureCase)
	if caseValue == SimulatorCasePreDispatchFailure {
		audit := AuditRecord{Action: AuditFixtureCompleted, CredentialFingerprint: stored.metadata.Fingerprint,
			StatusCode: "SBR_HELPER_FIXTURE_NOT_STARTED"}
		if err := service.store.FinishFixtureWithAudit(ctx, fixture, caseValue, nil, audit,
			func(auditCtx context.Context, executor MutationExecutor, record AuditRecord) error {
				return service.audit.Record(auditCtx, executor, record)
			}); err != nil {
			return nil, connect.NewError(connect.CodeInternal, ErrService)
		}
		return service.fixtureResponse(ctx, request.Msg, binding, profile, stored, TransportNotStarted), nil
	}
	if err := service.store.ReserveFixtureDispatch(ctx, fixture, request.Msg.CommandContext.Authentication.GetActorUserId(),
		func(txctx context.Context, executor MutationExecutor) error {
			if err := service.identity.AuthorizeWithin(txctx, executor, request.Msg.CommandContext.Authentication,
				authorisation.ActionUseSBRMachineCredential); err != nil {
				return err
			}
			if err := service.identity.ConsumeFreshFactorWithin(txctx, executor, request.Msg.CommandContext.Authentication,
				request.Msg.CommandContext.FreshFactor, PurposeUseMachineCredential); err != nil {
				return err
			}
			return service.audit.Record(txctx, executor, AuditRecord{Action: AuditFixtureDispatching,
				CredentialFingerprint: stored.metadata.Fingerprint, StatusCode: "SBR_HELPER_FIXTURE_DISPATCHING"})
		}); err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, ErrService)
	}
	helperRequest := service.helperRequest(HelperOperationFixture, binding, profile)
	helperRequest.RequestID, helperRequest.OperationID = fixture.OperationID, fixture.OperationID
	helperRequest.FixtureFailureCase = request.Msg.FailureCase
	defer helperRequest.ClearSecrets()
	result, err := profile.Execute(ctx, helperRequest)
	if err != nil {
		failureCase := SimulatorCaseHelperDeath
		statusCode := "SBR_HELPER_FIXTURE_HELPER_DEATH"
		if errors.Is(err, ErrHelperDeadlineExpired) || errors.Is(err, context.DeadlineExceeded) {
			failureCase = SimulatorCaseTimeout
			statusCode = "SBR_HELPER_FIXTURE_TIMEOUT"
		}
		audit := AuditRecord{Action: AuditFixtureUnknown, CredentialFingerprint: stored.metadata.Fingerprint,
			StatusCode: statusCode}
		if finishErr := service.store.FinishFixtureWithAudit(ctx, fixture, failureCase, nil, audit,
			func(auditCtx context.Context, executor MutationExecutor, record AuditRecord) error {
				return service.audit.Record(auditCtx, executor, record)
			}); finishErr != nil {
			return nil, connect.NewError(connect.CodeInternal, ErrService)
		}
		return nil, connect.NewError(connect.CodeUnavailable, ErrService)
	}
	resultHash := fixtureResultHash(result)
	if !validFixtureHelperResponse(helperRequest, result) {
		audit := AuditRecord{Action: AuditFixtureCompleted, CredentialFingerprint: stored.metadata.Fingerprint,
			StatusCode: "SBR_HELPER_FIXTURE_REJECTED"}
		if finishErr := service.store.FinishFixtureWithAudit(ctx, fixture, SimulatorCaseMalformedResponse, &resultHash, audit,
			func(auditCtx context.Context, executor MutationExecutor, record AuditRecord) error {
				return service.audit.Record(auditCtx, executor, record)
			}); finishErr != nil {
			return nil, connect.NewError(connect.CodeInternal, ErrService)
		}
		return nil, connect.NewError(connect.CodeUnavailable, ErrService)
	}
	caseValue, ok = simulatorCaseForState(result.FixtureState)
	if !ok {
		return nil, connect.NewError(connect.CodeUnavailable, ErrService)
	}
	var hash *[sha256.Size]byte
	if caseValue == SimulatorCaseAccepted || caseValue == SimulatorCaseMalformedResponse {
		hash = &resultHash
	}
	finalAction := AuditFixtureCompleted
	if result.FixtureState == TransportUnknown || result.FixtureState == TransportMaybeSent {
		finalAction = AuditFixtureUnknown
	}
	audit := AuditRecord{Action: finalAction, CredentialFingerprint: stored.metadata.Fingerprint, StatusCode: string(finalAction)}
	if err := service.store.FinishFixtureWithAudit(ctx, fixture, caseValue, hash, audit,
		func(auditCtx context.Context, executor MutationExecutor, record AuditRecord) error {
			return service.audit.Record(auditCtx, executor, record)
		}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrService)
	}
	succeeded := result.FixtureState == TransportAccepted && caseValue == SimulatorCaseAccepted
	readiness := service.readiness(ctx, binding, profile, stored, true)
	return connect.NewResponse(&tammyv1.RunSbrReadinessFixtureResponse{Result: &tammyv1.SbrReadinessFixtureResult{FixtureId: ReadinessFixtureID, FailureCase: request.Msg.FailureCase, Succeeded: succeeded, Readiness: readiness}}), nil
}

func validFixtureHelperResponse(request HelperRequest, result HelperResult) bool {
	if result.RequestID != request.RequestID || result.Outcome != HelperOutcomeOK ||
		result.ResultCode != HelperResultFixtureSelected || result.PendingID != "" || result.StableCode != "" ||
		result.FixtureFailureCase != request.FixtureFailureCase {
		return false
	}
	want := TransportAccepted
	switch request.FixtureFailureCase {
	case tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_MAYBE_SENT,
		tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_HELPER_DEATH,
		tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_TIMEOUT:
		want = TransportMaybeSent
	case tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_MALFORMED_RESPONSE:
		want = TransportFailed
	case tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_UNSPECIFIED:
	default:
		return false
	}
	return result.FixtureState == want
}

func simulatorCaseForState(state TransportState) (SimulatorCase, bool) {
	switch state {
	case TransportAccepted:
		return SimulatorCaseAccepted, true
	case TransportFailed:
		return SimulatorCaseMalformedResponse, true
	case TransportMaybeSent:
		return SimulatorCaseUncertainWrite, true
	default:
		return "", false
	}
}

func fixtureResultHash(result HelperResult) [sha256.Size]byte {
	digest := sha256.New()
	_, _ = digest.Write([]byte("tammy.sbr.fixture-result.v1\x00"))
	_, _ = digest.Write([]byte(result.RequestID))
	_, _ = digest.Write([]byte{byte(result.Outcome), byte(result.ResultCode), byte(result.FixtureFailureCase)})
	_, _ = digest.Write([]byte(result.FixtureState))
	var value [sha256.Size]byte
	copy(value[:], digest.Sum(nil))
	return value
}

func (service *Service) fixtureResponse(ctx context.Context, request *tammyv1.RunSbrReadinessFixtureRequest, binding OrganisationBinding, profile RuntimeProfile, stored serviceBinding, state TransportState) *connect.Response[tammyv1.RunSbrReadinessFixtureResponse] {
	return connect.NewResponse(&tammyv1.RunSbrReadinessFixtureResponse{Result: &tammyv1.SbrReadinessFixtureResult{FixtureId: ReadinessFixtureID, FailureCase: request.FailureCase, Succeeded: state == TransportAccepted, Readiness: service.readiness(ctx, binding, profile, stored, true)}})
}

func fixtureSemanticHash(request *tammyv1.RunSbrReadinessFixtureRequest, profile RuntimeProfile, credential [sha256.Size]byte) [sha256.Size]byte {
	digest := sha256.New()
	_, _ = digest.Write([]byte("tammy.sbr.fixture.v1\x00"))
	_, _ = digest.Write([]byte(request.FixtureId))
	_, _ = digest.Write([]byte{byte(request.FailureCase)})
	_, _ = digest.Write(credential[:])
	_, _ = digest.Write(profile.ProfileFingerprint[:])
	_, _ = digest.Write(profile.ComponentFingerprint[:])
	var result [sha256.Size]byte
	copy(result[:], digest.Sum(nil))
	return result
}

func unlockSemanticHash(binding OrganisationBinding, profile RuntimeProfile, credential [sha256.Size]byte) [sha256.Size]byte {
	digest := sha256.New()
	_, _ = digest.Write([]byte("tammy.sbr.unlock.v1\x00"))
	_, _ = digest.Write([]byte(binding.OrganisationID))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write([]byte(binding.CanonicalABN))
	_, _ = digest.Write(credential[:])
	_, _ = digest.Write(profile.ProfileFingerprint[:])
	var result [sha256.Size]byte
	copy(result[:], digest.Sum(nil))
	return result
}

func simulatorCase(value tammyv1.SbrReadinessFixtureFailure) SimulatorCase {
	switch value {
	case tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_NOT_STARTED:
		return SimulatorCasePreDispatchFailure
	case tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_MAYBE_SENT:
		return SimulatorCaseUncertainWrite
	case tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_MALFORMED_RESPONSE:
		return SimulatorCaseMalformedResponse
	case tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_HELPER_DEATH:
		return SimulatorCaseHelperDeath
	case tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_TIMEOUT:
		return SimulatorCaseTimeout
	default:
		return SimulatorCaseAccepted
	}
}

func (service *Service) readiness(ctx context.Context, binding OrganisationBinding, profile RuntimeProfile, stored serviceBinding, present bool) *tammyv1.SbrReadiness {
	credentialState := tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_MISSING
	state := tammyv1.SbrReadinessState_SBR_READINESS_STATE_UNAVAILABLE
	codes := []string{"SBR_CREDENTIAL_MISSING"}
	fingerprint := ""
	productState := service.store.ProductState(ctx, binding, profile)
	if present && usableCredentialBinding(stored, binding, profile, service.now().UTC()) {
		credentialState = stored.state
		fingerprint = hex.EncodeToString(stored.metadata.Fingerprint[:])
		codes = nil
		if profile.Environment == tammyv1.SbrEnvironment_SBR_ENVIRONMENT_SIMULATOR {
			state = tammyv1.SbrReadinessState_SBR_READINESS_STATE_READY_FOR_SIMULATOR
		} else if productState == ProductPresent {
			state = tammyv1.SbrReadinessState_SBR_READINESS_STATE_READY_FOR_EVTE_PRE_CONFORMANCE
		} else {
			codes = []string{"SBR_PRODUCT_ID_MISSING"}
			if productState == ProductInaccessible {
				codes[0] = "SBR_PRODUCT_ID_INACCESSIBLE"
			}
		}
	} else if present {
		credentialState = stored.state
		fingerprint = hex.EncodeToString(stored.metadata.Fingerprint[:])
		codes = []string{"SBR_CREDENTIAL_REIMPORT_REQUIRED"}
		if !stored.metadata.ExpiresAt.After(service.now().UTC()) || stored.state == tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_EXPIRED {
			credentialState = tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_EXPIRED
			codes[0] = "SBR_CREDENTIAL_EXPIRED"
		}
	}
	return &tammyv1.SbrReadiness{Environment: profile.Environment, State: state, MachineCredentialState: credentialState,
		ProductIdState: productProjection(productState), ReadinessCodes: codes, CredentialFingerprint: fingerprint,
		ProfileFingerprint: hex.EncodeToString(profile.ProfileFingerprint[:]), ComponentFingerprint: hex.EncodeToString(profile.ComponentFingerprint[:])}
}

func credentialProjection(metadata CredentialMetadata) *tammyv1.MachineCredentialStatus {
	state := metadata.State
	if state == tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_MISSING {
		state = tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT
	}
	projection := &tammyv1.MachineCredentialStatus{State: state, Fingerprint: hex.EncodeToString(metadata.Fingerprint[:]), Issuer: bounded(metadata.Issuer, 512), Serial: bounded(metadata.Serial, 128), ComponentVersion: bounded(metadata.ComponentVersion, 128)}
	if !metadata.CreatedAt.IsZero() {
		projection.CreatedAt = timestamppb.New(metadata.CreatedAt.UTC())
	}
	if !metadata.ExpiresAt.IsZero() {
		projection.ExpiresAt = timestamppb.New(metadata.ExpiresAt.UTC())
	}
	return projection
}

func missingCredential() *tammyv1.MachineCredentialStatus {
	return &tammyv1.MachineCredentialStatus{State: tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_MISSING}
}
func productProjection(state ProductState) tammyv1.ProductIdState {
	switch state {
	case ProductPresent:
		return tammyv1.ProductIdState_PRODUCT_ID_STATE_PRESENT
	case ProductInaccessible:
		return tammyv1.ProductIdState_PRODUCT_ID_STATE_INACCESSIBLE
	default:
		return tammyv1.ProductIdState_PRODUCT_ID_STATE_MISSING
	}
}
func bounded(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if len(value) > maximum {
		return value[:maximum]
	}
	return value
}
func invalid() error { return connect.NewError(connect.CodeInvalidArgument, ErrService) }
