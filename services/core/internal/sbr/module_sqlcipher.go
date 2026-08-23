//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package sbr

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	tammyv1connect "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1/tammyv1connect"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/transport"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
)

type ModuleConfig struct {
	Helper          HelperPort
	Profiles        ProfilePort
	Audit           AuditPort
	InstallationKey []byte
}

type SbrModule struct {
	config ModuleConfig
	route  *sbrRoute
}

func NewSbrModule(config ModuleConfig) (*SbrModule, error) {
	if (config.Helper == nil) != (config.Profiles == nil) || (config.Helper == nil) != (config.Audit == nil) ||
		(config.Helper != nil && len(config.InstallationKey) != 0 && len(config.InstallationKey) != sha256.Size) {
		return nil, ErrService
	}
	return &SbrModule{config: config, route: &sbrRoute{}}, nil
}

func (module *SbrModule) HandlerFactories() []transport.GeneratedHandlerFactory {
	if module == nil || module.route == nil {
		return nil
	}
	return []transport.GeneratedHandlerFactory{module.route.factory}
}

func (module *SbrModule) Activate(activation app.LocalWorkspaceActivation) error {
	if module == nil || module.route == nil || activation.Database == nil || activation.Identity == nil || activation.Now == nil || activation.NewID == nil {
		return ErrService
	}
	if module.config.Helper == nil {
		return module.route.set(unavailableService{})
	}
	installationKey := activation.InstallationKey
	if len(installationKey) == 0 {
		installationKey = module.config.InstallationKey
	}
	if len(installationKey) != sha256.Size {
		return ErrService
	}
	repository, err := NewSQLCipherRepository(activation.Database)
	if err != nil {
		return err
	}
	recoveryStamp := activation.Now().UTC().Format("2006-01-02T15:04:05.000000000Z")
	if _, err := repository.RecoverSimulatorOrphans(context.Background(), recoveryStamp); err != nil {
		return err
	}
	if _, err := repository.RecoverHelperDispatchOrphans(context.Background(), recoveryStamp); err != nil {
		return err
	}
	if err := recoverDurableMutations(context.Background(), repository, activation.WorkspaceID, module.config.Profiles,
		module.config.Helper, module.config.Audit, installationKey, activation.Now); err != nil {
		return err
	}
	store := newSQLServiceStore(repository, activation.WorkspaceID, activation.Now, activation.NewID)
	service, err := NewService(ServiceConfig{WorkspaceID: activation.WorkspaceID, Identity: activation.Identity,
		Organisation: sqlOrganisationPort{}, Profiles: module.config.Profiles, Helper: module.config.Helper,
		Units: sqlUnitOfWork{database: activation.Database}, Store: store, Now: activation.Now, NewID: activation.NewID,
		Audit: module.config.Audit, InstallationKey: installationKey})
	if err != nil {
		return err
	}
	return module.route.set(service)
}

func recoverDurableMutations(ctx context.Context, repository *SQLCipherRepository, workspaceID string,
	profiles ProfilePort, helper HelperPort, audit AuditPort, installationKey []byte, now func() time.Time,
) error {
	mutations, err := repository.ListRecoverableMutations(ctx, workspaceID)
	if err != nil {
		return err
	}
	if len(mutations) == 0 {
		return nil
	}
	profile, err := profiles.Current(ctx, now().UTC())
	if err != nil || profile.AuthenticatedUntil.Before(now().UTC()) {
		_ = profile.Close()
		return ErrService
	}
	if profile.lease == nil {
		profile = BindRuntimeProfileLease(profile, staticProfileLease{HelperPort: helper})
	}
	defer profile.Close()
	stamp := func() string { return now().UTC().Format("2006-01-02T15:04:05.000000000Z") }
	requestFor := func(operation HelperOperation, mutation Mutation) HelperRequest {
		scope := DeriveOpaqueScope(installationKey, workspaceID, mutation.Key.OrganisationID, mutation.Key.CanonicalABN)
		request := HelperRequest{Operation: operation, RequestID: mutation.OperationID, OperationID: mutation.OperationID, PendingID: mutation.PendingID,
			MutationKind: mutation.Kind, Environment: profile.Environment, WorkspaceID: workspaceID,
			OrganisationID: mutation.Key.OrganisationID, CanonicalABN: mutation.Key.CanonicalABN,
			OpaqueScope: scope[:], EndpointProfile: bytes.Clone(profile.EndpointProfile), ProfileFingerprint: profile.ProfileFingerprint,
			RegistrationFingerprint: profile.RegistrationFingerprint, ComponentFingerprint: profile.ComponentFingerprint,
			ComponentVersion: profile.ComponentVersion}
		if mutation.Kind == MutationImportProductID || mutation.Kind == MutationRemoveProductID {
			request.ProductIdentifier, request.ServiceIdentifier = profile.ExpectedProductIdentifier, profile.ExpectedServiceID
		}
		return request
	}
	for _, mutation := range mutations {
		reconcileRequest := requestFor(HelperOperationReconcileMutation, mutation)
		result, helperErr := profile.Execute(ctx, reconcileRequest)
		reconcileRequest.ClearSecrets()
		if helperErr != nil || !validRecoveryReconcileResponse(reconcileRequest, mutation, result, profile) {
			return ErrService
		}
		if (mutation.State == MutationCoreCommitted || mutation.State == MutationReconcileRequired) &&
			result.Outcome == HelperOutcomeOK && result.ResultCode == HelperResultMutationCommitted {
			effect, effectErr := repository.PendingMutationCommit(ctx, mutation)
			if effectErr != nil || !validRecoveryCommittedResponse(reconcileRequest, mutation, result, profile, effect) {
				return ErrService
			}
			if err := repository.FinalizeMutation(ctx, mutation.Key, mutation.OperationID, stamp(),
				func(auditCtx context.Context, executor MutationEffectExecutor, record AuditRecord) error {
					return audit.Record(auditCtx, executor, record)
				}); err != nil {
				return err
			}
			continue
		}
		if mutation.State == MutationPrepared && result.Outcome == HelperOutcomePending && ids.IsCanonicalV7(result.PendingID) {
			if mutation.Kind == MutationImportCredential {
				if result.Credential.Fingerprint == [sha256.Size]byte{} ||
					repository.MarkImportMutationStaged(ctx, mutation.Key, mutation.OperationID, result.PendingID,
						result.Credential.Fingerprint, stamp()) != nil {
					return ErrService
				}
			} else if err := repository.MarkMutationStaged(ctx, mutation.Key, mutation.OperationID, result.PendingID, stamp()); err != nil {
				return err
			}
			mutation, err = repository.GetMutation(ctx, mutation.Key, mutation.OperationID)
			if err != nil {
				return err
			}
		}
		action, err := repository.ReconcileMutation(ctx, mutation.Key, mutation.OperationID, stamp())
		if err != nil {
			return err
		}
		switch action {
		case ReconcileNone:
			continue
		case ReconcileAbort:
			current, currentErr := repository.GetMutation(ctx, mutation.Key, mutation.OperationID)
			if currentErr != nil {
				return currentErr
			}
			if current.State == MutationAborted {
				continue
			}
			if current.State == MutationAbortRequired {
				if err := repository.MarkMutationAbortDispatched(ctx, current.Key, current.OperationID, stamp()); err != nil {
					return err
				}
				current.State = MutationAborting
			}
			abortRequest := requestFor(HelperOperationAbortMutation, current)
			abortRequest.PendingID = current.PendingID
			ack, abortErr := profile.Execute(ctx, abortRequest)
			abortRequest.ClearSecrets()
			if abortErr != nil || !validRecoveryClosedResponse(abortRequest, ack, HelperResultMutationAborted, profile) {
				return ErrService
			}
			if err := repository.AcknowledgeMutationAbort(ctx, current.Key, current.OperationID, stamp()); err != nil {
				return err
			}
		case ReconcileCommit:
			current, currentErr := repository.GetMutation(ctx, mutation.Key, mutation.OperationID)
			if currentErr != nil {
				return currentErr
			}
			commitRequest := requestFor(HelperOperationCommitMutation, current)
			commitRequest.PendingID = current.PendingID
			ack, commitErr := profile.Execute(ctx, commitRequest)
			commitRequest.ClearSecrets()
			effect, effectErr := repository.PendingMutationCommit(ctx, current)
			if commitErr != nil || effectErr != nil || !validRecoveryCommittedResponse(commitRequest, current, ack, profile, effect) {
				return ErrService
			}
			if err := repository.FinalizeMutation(ctx, current.Key, current.OperationID, stamp(),
				func(auditCtx context.Context, executor MutationEffectExecutor, record AuditRecord) error {
					return audit.Record(auditCtx, executor, record)
				}); err != nil {
				return err
			}
		default:
			return ErrService
		}
	}
	return nil
}

func validRecoveryReconcileResponse(request HelperRequest, mutation Mutation, result HelperResult, profile RuntimeProfile) bool {
	if result.RequestID != request.RequestID || result.StableCode != "" || !validRecoveryProfile(result, profile) {
		return false
	}
	if (mutation.State == MutationCoreCommitted || mutation.State == MutationReconcileRequired) &&
		result.Outcome == HelperOutcomeOK && result.ResultCode == HelperResultMutationCommitted && result.PendingID == "" {
		return true
	}
	if mutation.PendingID == "" && result.Outcome == HelperOutcomeOK {
		return result.ResultCode == HelperResultNotStarted && result.PendingID == ""
	}
	return result.Outcome == HelperOutcomePending && result.ResultCode == HelperResultRecoveryRequired &&
		ids.IsCanonicalV7(result.PendingID) && (mutation.PendingID == "" || result.PendingID == mutation.PendingID)
}

func validRecoveryClosedResponse(request HelperRequest, result HelperResult, expected HelperResultCode, profile RuntimeProfile) bool {
	return validClosedHelperResponse(request, result, expected) && validRecoveryProfile(result, profile)
}

func validRecoveryCommittedResponse(request HelperRequest, mutation Mutation, result HelperResult, profile RuntimeProfile,
	effect MutationCommit,
) bool {
	if effect.Command == nil {
		return false
	}
	switch mutation.Kind {
	case MutationImportCredential, MutationReplaceCredential, MutationRemoveCredential:
		return validCommittedCredentialResponse(request, result, HelperResult{Credential: effect.Command.Credential}, profile)
	case MutationImportProductID, MutationRemoveProductID:
		if effect.Product == nil || effect.Product.ExpectedProductIdentifier != profile.ExpectedProductIdentifier ||
			effect.Product.ExpectedServiceID != profile.ExpectedServiceID || effect.Product.ScopeFingerprint != profile.ProductScopeFingerprint ||
			request.ProductIdentifier != profile.ExpectedProductIdentifier || request.ServiceIdentifier != profile.ExpectedServiceID {
			return false
		}
		return validCommittedProductResponse(request, result, HelperResult{ProductState: effect.Product.State,
			ProductFingerprint: effect.Product.ProductFingerprint}, profile)
	default:
		return false
	}
}

func validRecoveryProfile(result HelperResult, profile RuntimeProfile) bool {
	return result.ProfileFingerprint == profile.ProfileFingerprint &&
		result.RegistrationFingerprint == profile.RegistrationFingerprint &&
		result.ComponentFingerprint == profile.ComponentFingerprint && result.ComponentVersion == profile.ComponentVersion
}

type sbrRoute struct {
	mu      sync.RWMutex
	options []connect.HandlerOption
	handler http.Handler
}

func (route *sbrRoute) factory(options ...connect.HandlerOption) (string, http.Handler) {
	route.mu.Lock()
	route.options = append([]connect.HandlerOption(nil), options...)
	route.mu.Unlock()
	return "/" + tammyv1connect.SbrServiceName + "/", route
}

func (route *sbrRoute) set(handler tammyv1connect.SbrServiceHandler) error {
	if route == nil || handler == nil {
		return ErrService
	}
	_, transportHandler := tammyv1connect.NewSbrServiceHandler(handler, append([]connect.HandlerOption(nil), route.options...)...)
	route.mu.Lock()
	route.handler = transportHandler
	route.mu.Unlock()
	return nil
}

func (route *sbrRoute) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	route.mu.RLock()
	handler := route.handler
	route.mu.RUnlock()
	if handler == nil {
		http.Error(response, "local SBR unavailable", http.StatusServiceUnavailable)
		return
	}
	handler.ServeHTTP(response, request)
}

type unavailableService struct {
	tammyv1connect.UnimplementedSbrServiceHandler
}

func unavailableResponse() error { return connect.NewError(connect.CodeFailedPrecondition, ErrService) }
func (unavailableService) GetSbrReadiness(context.Context, *connect.Request[tammyv1.GetSbrReadinessRequest]) (*connect.Response[tammyv1.GetSbrReadinessResponse], error) {
	return nil, unavailableResponse()
}
func (unavailableService) ImportMachineCredential(context.Context, *connect.Request[tammyv1.ImportMachineCredentialRequest]) (*connect.Response[tammyv1.ImportMachineCredentialResponse], error) {
	return nil, unavailableResponse()
}
func (unavailableService) GetMachineCredentialStatus(context.Context, *connect.Request[tammyv1.GetMachineCredentialStatusRequest]) (*connect.Response[tammyv1.GetMachineCredentialStatusResponse], error) {
	return nil, unavailableResponse()
}
func (unavailableService) UnlockMachineCredential(context.Context, *connect.Request[tammyv1.UnlockMachineCredentialRequest]) (*connect.Response[tammyv1.UnlockMachineCredentialResponse], error) {
	return nil, unavailableResponse()
}
func (unavailableService) ReplaceMachineCredential(context.Context, *connect.Request[tammyv1.ReplaceMachineCredentialRequest]) (*connect.Response[tammyv1.ReplaceMachineCredentialResponse], error) {
	return nil, unavailableResponse()
}
func (unavailableService) RemoveMachineCredential(context.Context, *connect.Request[tammyv1.RemoveMachineCredentialRequest]) (*connect.Response[tammyv1.RemoveMachineCredentialResponse], error) {
	return nil, unavailableResponse()
}
func (unavailableService) ImportSbrProductId(context.Context, *connect.Request[tammyv1.ImportSbrProductIdRequest]) (*connect.Response[tammyv1.ImportSbrProductIdResponse], error) {
	return nil, unavailableResponse()
}
func (unavailableService) RemoveSbrProductId(context.Context, *connect.Request[tammyv1.RemoveSbrProductIdRequest]) (*connect.Response[tammyv1.RemoveSbrProductIdResponse], error) {
	return nil, unavailableResponse()
}
func (unavailableService) RunSbrReadinessFixture(context.Context, *connect.Request[tammyv1.RunSbrReadinessFixtureRequest]) (*connect.Response[tammyv1.RunSbrReadinessFixtureResponse], error) {
	return nil, unavailableResponse()
}

type sqlUnitOfWork struct{ database *sqlcipher.Database }

func (unit sqlUnitOfWork) Inspect(ctx context.Context, work func(context.Context, QueryExecutor) error) error {
	if unit.database == nil || work == nil {
		return ErrService
	}
	return work(ctx, unit.database)
}

func (unit sqlUnitOfWork) Mutate(ctx context.Context, work func(context.Context, MutationExecutor) error) error {
	if unit.database == nil || work == nil {
		return ErrService
	}
	transaction, err := unit.database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = transaction.Rollback()
		}
	}()
	if err := work(ctx, transaction); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

type sqlOrganisationPort struct{}

func (sqlOrganisationPort) Current(ctx context.Context, executor QueryExecutor, now time.Time) (OrganisationBinding, error) {
	if executor == nil || now.IsZero() {
		return OrganisationBinding{}, ErrService
	}
	rows, err := executor.QueryContext(ctx, `SELECT o.id,o.abn,v.expires_at FROM organisations o
JOIN organisation_verifications v ON v.organisation_id=o.id
WHERE o.status='ACTIVE' AND o.verification_state='VERIFIED' AND v.state=2
AND v.expires_at>? AND NOT EXISTS (SELECT 1 FROM organisation_verifications successor WHERE successor.supersedes_verification_id=v.id)
ORDER BY v.recorded_at DESC LIMIT 2`, now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return OrganisationBinding{}, ErrService
	}
	defer rows.Close()
	var binding OrganisationBinding
	var expires string
	if !rows.Next() || rows.Scan(&binding.OrganisationID, &binding.CanonicalABN, &expires) != nil || rows.Next() || rows.Err() != nil {
		return OrganisationBinding{}, ErrService
	}
	binding.VerificationExpiresAt, err = time.Parse(time.RFC3339Nano, expires)
	if err != nil {
		return OrganisationBinding{}, ErrService
	}
	return binding, nil
}

type sqlServiceStore struct {
	repository  *SQLCipherRepository
	workspaceID string
	now         func() time.Time
	newID       func() (string, error)
}

func newSQLServiceStore(repository *SQLCipherRepository, workspaceID string, now func() time.Time, newID func() (string, error)) *sqlServiceStore {
	return &sqlServiceStore{repository: repository, workspaceID: workspaceID, now: now, newID: newID}
}
func (store *sqlServiceStore) scope(binding OrganisationBinding) BindingKey {
	return BindingKey{WorkspaceID: store.workspaceID, OrganisationID: binding.OrganisationID, CanonicalABN: binding.CanonicalABN, SchemaVersion: 1}
}
func (store *sqlServiceStore) Current(ctx context.Context, binding OrganisationBinding) (serviceBinding, bool) {
	scope := store.scope(binding)
	stored, err := store.repository.GetCurrentBinding(ctx, scope)
	if err != nil {
		return serviceBinding{}, false
	}
	profile, err := store.repository.GetAuthenticatedProfile(ctx, stored.Key, EnvironmentSimulator)
	if err != nil {
		profile, err = store.repository.GetAuthenticatedProfile(ctx, stored.Key, EnvironmentEVTE)
	}
	if err != nil {
		return serviceBinding{}, false
	}
	expires, err := time.Parse(time.RFC3339Nano, stored.ExpiresAt)
	if err != nil {
		return serviceBinding{}, false
	}
	credentialState := tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_PRESENT
	if stored.State != BindingActive {
		credentialState = tammyv1.MachineCredentialState_MACHINE_CREDENTIAL_STATE_INACCESSIBLE
	}
	metadata := CredentialMetadata{Fingerprint: stored.Key.CredentialFingerprint, CanonicalABN: stored.Key.CanonicalABN,
		ComponentVersion: stored.ComponentVersion, ExpiresAt: expires, State: credentialState}
	runtime := RuntimeProfile{ComponentVersion: stored.ComponentVersion, Conformance: profile.Conformance, ProfileFingerprint: profile.ProfileFingerprint,
		RegistrationFingerprint: profile.RegistrationFingerprint, ComponentFingerprint: profile.ComponentFingerprint}
	if profile.Environment == EnvironmentSimulator {
		runtime.Environment = tammyv1.SbrEnvironment_SBR_ENVIRONMENT_SIMULATOR
	} else {
		runtime.Environment = tammyv1.SbrEnvironment_SBR_ENVIRONMENT_EVTE
	}
	return serviceBinding{metadata: metadata, profile: runtime, state: metadata.State, bindingState: string(stored.State)}, true
}
func (store *sqlServiceStore) Command(ctx context.Context, binding OrganisationBinding, idempotencyKey string, semantic [sha256.Size]byte) (commandResult, bool, error) {
	record, err := store.repository.GetCommand(ctx, store.scope(binding), idempotencyKey)
	if errors.Is(err, ErrNotFound) {
		return commandResult{}, false, nil
	}
	if err != nil {
		return commandResult{}, false, err
	}
	if record.SemanticHash != semantic {
		return commandResult{}, false, ErrIdempotencyConflict
	}
	return commandResult{OperationID: record.OperationID, Kind: record.Kind, Semantic: record.SemanticHash,
		ActorUserID: record.ActorUserID, Completed: record.State == CommandCompleted, Credential: record.Credential, Product: record.Product}, true, nil
}
func (store *sqlServiceStore) Prepare(ctx context.Context, operation, actorUserID string, kind MutationKind, binding OrganisationBinding,
	idempotencyKey string, semantic [sha256.Size]byte, reserve func(context.Context, MutationExecutor) error,
) error {
	key := BindingKey{WorkspaceID: store.workspaceID, OrganisationID: binding.OrganisationID, CanonicalABN: binding.CanonicalABN, SchemaVersion: 1}
	if kind != MutationImportCredential {
		current, err := store.repository.GetCurrentBinding(ctx, key)
		if err != nil {
			return err
		}
		key = current.Key
	}
	metadataHash := sha256.Sum256([]byte(string(kind) + "\x00" + operation))
	stamp := store.now().UTC().Format("2006-01-02T15:04:05.000000000Z")
	command := CommandRecord{OperationID: operation, ActorUserID: actorUserID, Scope: store.scope(binding),
		IdempotencyKey: idempotencyKey, SemanticHash: semantic, Kind: kind, State: CommandPrepared,
		CreatedAt: stamp, UpdatedAt: stamp}
	mutation := Mutation{OperationID: operation, Key: key, Kind: kind, State: MutationPrepared,
		MetadataHash: metadataHash, CreatedAt: stamp, UpdatedAt: stamp}
	if err := store.repository.ReserveCommandMutation(ctx, command, mutation,
		func(reserveCtx context.Context, executor MutationEffectExecutor) error {
			return reserve(reserveCtx, executor)
		}); err != nil {
		return err
	}
	return nil
}
func (store *sqlServiceStore) Stage(ctx context.Context, operation string, result HelperResult) error {
	command, err := store.repository.GetCommandByOperation(ctx, store.workspaceID, operation)
	if err != nil {
		return err
	}
	pending, err := store.repository.GetMutation(ctx, command.Scope, operation)
	if err != nil {
		return err
	}
	stamp := store.now().UTC().Format("2006-01-02T15:04:05.000000000Z")
	if pending.Kind == MutationImportCredential {
		err = store.repository.MarkImportMutationStaged(ctx, pending.Key, operation, result.PendingID, result.Credential.Fingerprint, stamp)
	} else {
		err = store.repository.MarkMutationStaged(ctx, pending.Key, operation, result.PendingID, stamp)
	}
	return err
}
func (store *sqlServiceStore) Commit(ctx context.Context, operation string, value serviceBinding, completionAudit AuditRecord,
	decisionEffect func(context.Context, MutationExecutor) error,
) error {
	if decisionEffect == nil {
		return ErrInvalid
	}
	command, err := store.repository.GetCommandByOperation(ctx, store.workspaceID, operation)
	if err != nil {
		return err
	}
	pending, err := store.repository.GetMutation(ctx, command.Scope, operation)
	if err != nil {
		return err
	}
	commit := MutationCommit{CompletionAudit: completionAudit, Decision: func(decisionCtx context.Context, executor MutationEffectExecutor) error {
		return decisionEffect(decisionCtx, executor)
	}}
	stamp := store.now().UTC().Format("2006-01-02T15:04:05.000000000Z")
	commit.Command = &CommandCompletion{Scope: command.Scope, Credential: value.metadata, Product: value.product, UpdatedAt: stamp}
	if pending.Kind == MutationImportCredential || pending.Kind == MutationReplaceCredential {
		key := pending.Key
		key.CredentialFingerprint = value.metadata.Fingerprint
		subject := sha256.Sum256([]byte(value.metadata.Issuer + "\x00" + value.metadata.Serial))
		commit.NewBinding = &Binding{Key: key, ComponentVersion: value.metadata.ComponentVersion, SubjectHash: subject, ExpiresAt: value.metadata.ExpiresAt.UTC().Format("2006-01-02T15:04:05.000000000Z"), State: BindingActive, Revision: 1, UpdatedAt: store.now().UTC().Format("2006-01-02T15:04:05.000000000Z")}
		environment, conformance := EnvironmentEVTE, runtimeConformance(value.profile)
		readiness := ReadinessReadyForEVTEPreConformance
		if value.profile.Environment == tammyv1.SbrEnvironment_SBR_ENVIRONMENT_SIMULATOR {
			environment, conformance, readiness = EnvironmentSimulator, ConformanceSimulator, ReadinessReadyForSimulator
		} else if conformance == ConformancePost {
			readiness = ReadinessReadyForEVTEPostConformance
		}
		commit.Profile = &AuthenticatedProfile{Key: key, Environment: environment, ProfileFingerprint: value.profile.ProfileFingerprint, RegistrationFingerprint: value.profile.RegistrationFingerprint, ComponentFingerprint: value.profile.ComponentFingerprint, Conformance: conformance}
		transitionID, err := store.newID()
		if err != nil {
			return err
		}
		commit.Readiness = &ReadinessTransition{TransitionID: transitionID, Key: key, State: readiness, ReasonCode: "SBR_PROFILE_ACCEPTED", OccurredAt: store.now().UTC().Format("2006-01-02T15:04:05.000000000Z")}
	}
	if value.product != 0 {
		environment := EnvironmentEVTE
		if value.profile.Environment == tammyv1.SbrEnvironment_SBR_ENVIRONMENT_SIMULATOR {
			environment = EnvironmentSimulator
		}
		commit.Product = &ProductRecord{Key: pending.Key, Environment: environment,
			ScopeFingerprint: value.productScope, ExpectedProductIdentifier: value.profile.ExpectedProductIdentifier,
			ExpectedServiceID: value.profile.ExpectedServiceID, State: value.product, ProductFingerprint: value.productFingerprint,
			Revision: 1, UpdatedAt: stamp}
	}
	return store.repository.CommitMutation(ctx, pending.Key, operation, commit)
}
func (store *sqlServiceStore) Finish(ctx context.Context, operation string,
	audit func(context.Context, MutationExecutor, AuditRecord) error,
) error {
	if audit == nil {
		return ErrInvalid
	}
	command, err := store.repository.GetCommandByOperation(ctx, store.workspaceID, operation)
	if err != nil {
		return err
	}
	pending, err := store.repository.GetMutation(ctx, command.Scope, operation)
	if err != nil {
		return err
	}
	return store.repository.FinalizeMutation(ctx, pending.Key, operation,
		store.now().UTC().Format("2006-01-02T15:04:05.000000000Z"),
		func(auditCtx context.Context, executor MutationEffectExecutor, record AuditRecord) error {
			return audit(auditCtx, executor, record)
		})
}
func (store *sqlServiceStore) Abort(ctx context.Context, operation string) error {
	command, err := store.repository.GetCommandByOperation(ctx, store.workspaceID, operation)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	pending, err := store.repository.GetMutation(ctx, command.Scope, operation)
	if err != nil {
		return err
	}
	return store.repository.AbortMutation(ctx, pending.Key, operation, store.now().UTC().Format("2006-01-02T15:04:05.000000000Z"))
}
func (store *sqlServiceStore) FinishAbort(ctx context.Context, operation string) error {
	command, err := store.repository.GetCommandByOperation(ctx, store.workspaceID, operation)
	if err != nil {
		return err
	}
	pending, err := store.repository.GetMutation(ctx, command.Scope, operation)
	if err != nil {
		return err
	}
	return store.repository.AcknowledgeMutationAbort(ctx, pending.Key, operation, store.now().UTC().Format("2006-01-02T15:04:05.000000000Z"))
}
func (store *sqlServiceStore) ProductState(ctx context.Context, binding OrganisationBinding, profile RuntimeProfile) ProductState {
	if profile.Environment != tammyv1.SbrEnvironment_SBR_ENVIRONMENT_EVTE ||
		profile.ProductScopeFingerprint == [sha256.Size]byte{} || profile.ExpectedProductIdentifier == "" || profile.ExpectedServiceID == "" {
		return ProductMissing
	}
	current, err := store.repository.GetCurrentBinding(ctx, store.scope(binding))
	if err != nil {
		return ProductMissing
	}
	record, err := store.repository.GetProductState(ctx, current.Key, EnvironmentEVTE, profile.ProductScopeFingerprint,
		profile.ExpectedProductIdentifier, profile.ExpectedServiceID)
	if errors.Is(err, ErrNotFound) {
		return ProductMissing
	}
	if err != nil {
		return ProductInaccessible
	}
	return record.State
}
func (store *sqlServiceStore) SetProductState(ctx context.Context, binding OrganisationBinding, profile RuntimeProfile,
	state ProductState, scopeFingerprint, productFingerprint [sha256.Size]byte,
) {
	if profile.Environment != tammyv1.SbrEnvironment_SBR_ENVIRONMENT_EVTE ||
		scopeFingerprint != profile.ProductScopeFingerprint || profile.ExpectedProductIdentifier == "" || profile.ExpectedServiceID == "" {
		return
	}
	current, err := store.repository.GetCurrentBinding(ctx, store.scope(binding))
	if err != nil {
		return
	}
	_ = store.repository.PutProductState(ctx, ProductRecord{Key: current.Key, Environment: EnvironmentEVTE,
		ScopeFingerprint: scopeFingerprint, ExpectedProductIdentifier: profile.ExpectedProductIdentifier,
		ExpectedServiceID: profile.ExpectedServiceID, State: state, ProductFingerprint: productFingerprint, Revision: 1,
		UpdatedAt: store.now().UTC().Format("2006-01-02T15:04:05.000000000Z")})
}

func (store *sqlServiceStore) LookupFixture(ctx context.Context, binding OrganisationBinding, credential [sha256.Size]byte,
	idempotency string,
) (FixtureRecord, bool, error) {
	key := store.scope(binding)
	key.CredentialFingerprint = credential
	stored, err := store.repository.GetSimulatorTransportByIdempotency(ctx, key, idempotency)
	if errors.Is(err, ErrNotFound) {
		return FixtureRecord{}, false, nil
	}
	if err != nil {
		return FixtureRecord{}, false, err
	}
	return fixtureRecordFromTransport(binding, stored), true, nil
}

func (store *sqlServiceStore) PrepareFixture(ctx context.Context, binding OrganisationBinding, credential [sha256.Size]byte, operation, actorUserID, idempotency string, semantic [sha256.Size]byte) (FixtureRecord, bool, error) {
	key := store.scope(binding)
	key.CredentialFingerprint = credential
	stamp := store.now().UTC().Format("2006-01-02T15:04:05.000000000Z")
	stored, replay, err := store.repository.PrepareSimulatorTransport(ctx, SimulatorTransport{OperationID: operation, ActorUserID: actorUserID, Key: key,
		IdempotencyKey: idempotency, SemanticHash: semantic, State: TransportPrepared, CreatedAt: stamp, UpdatedAt: stamp})
	record := fixtureRecordFromTransport(binding, stored)
	if err != nil {
		return record, false, err
	}
	return record, replay, nil
}

func fixtureRecordFromTransport(binding OrganisationBinding, stored SimulatorTransport) FixtureRecord {
	return FixtureRecord{OperationID: stored.OperationID, ActorUserID: stored.ActorUserID, State: stored.State,
		semantic: stored.SemanticHash, credential: stored.Key.CredentialFingerprint, idempotencyKey: stored.IdempotencyKey,
		bindingKey: organisationStoreKey(binding), scopeKey: organisationStoreKey(binding)}
}
func (store *sqlServiceStore) fixtureKey(ctx context.Context, record FixtureRecord) (BindingKey, error) {
	parts := strings.Split(record.bindingKey, "\x00")
	if len(parts) != 2 {
		return BindingKey{}, ErrInvalid
	}
	current, err := store.repository.GetCurrentBinding(ctx, BindingKey{WorkspaceID: store.workspaceID, OrganisationID: parts[0], CanonicalABN: parts[1], SchemaVersion: 1})
	if err != nil {
		return BindingKey{}, err
	}
	return current.Key, nil
}
func (store *sqlServiceStore) ReserveFixtureDispatch(ctx context.Context, record FixtureRecord, actorUserID string,
	effect func(context.Context, MutationExecutor) error,
) error {
	key, err := store.fixtureKey(ctx, record)
	if err != nil {
		return err
	}
	transport := SimulatorTransport{OperationID: record.OperationID, ActorUserID: record.ActorUserID, Key: key,
		IdempotencyKey: record.idempotencyKey, SemanticHash: record.semantic, State: TransportPrepared,
		CreatedAt: store.now().UTC().Format("2006-01-02T15:04:05.000000000Z"), UpdatedAt: store.now().UTC().Format("2006-01-02T15:04:05.000000000Z")}
	return store.repository.ReserveSimulatorDispatch(ctx, transport, actorUserID,
		store.now().UTC().Format("2006-01-02T15:04:05.000000000Z"),
		func(txctx context.Context, executor MutationEffectExecutor) error { return effect(txctx, executor) })
}
func (store *sqlServiceStore) ApplyFixture(ctx context.Context, record FixtureRecord, value SimulatorCase, result *[sha256.Size]byte) error {
	key, err := store.fixtureKey(ctx, record)
	if err != nil {
		return err
	}
	return store.repository.ApplySimulatorCase(ctx, key, record.OperationID, value, result, store.now().UTC().Format("2006-01-02T15:04:05.000000000Z"))
}

func (store *sqlServiceStore) FinishFixtureWithAudit(ctx context.Context, record FixtureRecord, value SimulatorCase,
	result *[sha256.Size]byte, auditRecord AuditRecord,
	audit func(context.Context, MutationExecutor, AuditRecord) error,
) error {
	if audit == nil {
		return ErrInvalid
	}
	key, err := store.fixtureKey(ctx, record)
	if err != nil {
		return err
	}
	return store.repository.FinishSimulatorTransportWithAudit(ctx, key, record.OperationID, value, result,
		store.now().UTC().Format("2006-01-02T15:04:05.000000000Z"), auditRecord,
		func(auditCtx context.Context, executor MutationEffectExecutor, stored AuditRecord) error {
			return audit(auditCtx, executor, stored)
		})
}

func (store *sqlServiceStore) ReserveUnlockDispatch(ctx context.Context, record HelperDispatchRecord,
	effect func(context.Context, MutationExecutor) error,
) error {
	return store.repository.ReserveUnlockDispatch(ctx, record,
		func(txctx context.Context, executor MutationEffectExecutor) error { return effect(txctx, executor) })
}

func (store *sqlServiceStore) FinishUnlockDispatch(ctx context.Context, record HelperDispatchRecord, state HelperDispatchState) error {
	return store.repository.FinishHelperDispatch(ctx, record, state,
		store.now().UTC().Format("2006-01-02T15:04:05.000000000Z"))
}

var _ app.LocalWorkspaceModule = (*SbrModule)(nil)
var _ tammyv1connect.SbrServiceHandler = (*Service)(nil)
var _ tammyv1connect.SbrServiceHandler = unavailableService{}
var _ workspace.MutationExecutor = (*sqlcipher.Transaction)(nil)
var _ = errors.Is
