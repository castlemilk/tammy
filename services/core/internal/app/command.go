package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"errors"
	"reflect"

	"github.com/tammyapp/tammy/services/core/internal/authorisation"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/idempotency"
	"github.com/tammyapp/tammy/services/core/internal/platform/canonical"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
)

var (
	ErrCommand = errors.New("app: ordinary command failed")
)

const (
	CheckpointAfterSourceSave          = "after_source_save"
	CheckpointAfterLedgerPost          = "after_ledger_post"
	CheckpointAfterAuditAppend         = "after_audit_append"
	CheckpointAfterResultSerialization = "after_result_serialization"
)

// CommandSQLExecutor is a caller-owned transaction capability. It
// deliberately has no Commit or Rollback method.
type CommandSQLExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// CommandTransaction exposes typed repositories and the two narrow SQL
// capabilities required by idempotency and audit. The transaction owner alone
// can commit or roll back.
type CommandTransaction[Repositories any] interface {
	TransactionID() string
	Repositories() Repositories
	IdempotencyExecutor() idempotency.Executor
	AuditExecutor() CommandSQLExecutor
}

// CommandTransactions owns the outer SQLCipher transaction lifecycle.
type CommandTransactions[Repositories any] interface {
	WorkspaceID() string
	Mutate(context.Context, func(context.Context, CommandTransaction[Repositories]) error) error
}

// OwnedCommandTransaction is issued by the encrypted storage adapter. Only
// the application transaction owner receives its terminal capabilities.
type OwnedCommandTransaction[Repositories any] interface {
	CommandTransaction[Repositories]
	Commit() error
	Rollback() error
}

type CommandTransactionStarter[Repositories any] interface {
	Begin(context.Context) (OwnedCommandTransaction[Repositories], error)
}

type ownedCommandTransactions[Repositories any] struct {
	workspaceID string
	starter     CommandTransactionStarter[Repositories]
}

// NewCommandTransactions binds an exact workspace to an encrypted
// transaction starter. The returned port is the sole Commit/Rollback owner.
func NewCommandTransactions[Repositories any](
	workspaceID string,
	starter CommandTransactionStarter[Repositories],
) (CommandTransactions[Repositories], error) {
	if !ids.IsCanonicalV7(workspaceID) || nilInterface(starter) {
		return nil, ErrCommand
	}
	return &ownedCommandTransactions[Repositories]{workspaceID: workspaceID, starter: starter}, nil
}

func (transactions *ownedCommandTransactions[Repositories]) WorkspaceID() string {
	if transactions == nil {
		return ""
	}
	return transactions.workspaceID
}

func (transactions *ownedCommandTransactions[Repositories]) Mutate(
	ctx context.Context,
	work func(context.Context, CommandTransaction[Repositories]) error,
) (runErr error) {
	if transactions == nil || nilInterface(transactions.starter) || work == nil || ctx == nil {
		return ErrCommand
	}
	transaction, err := transactions.starter.Begin(ctx)
	if err != nil {
		return err
	}
	if nilInterface(transaction) {
		return ErrCommand
	}
	finished := false
	rollbackClaimed := false
	rollback := func() error {
		if rollbackClaimed {
			return nil
		}
		rollbackClaimed = true
		return transaction.Rollback()
	}
	defer func() {
		if !finished {
			_ = rollback()
		}
	}()
	if err := work(ctx, transaction); err != nil {
		rollbackErr := rollback()
		finished = true
		return errors.Join(err, rollbackErr)
	}
	if err := transaction.Commit(); err != nil {
		rollbackErr := rollback()
		finished = true
		return errors.Join(err, rollbackErr)
	}
	finished = true
	return nil
}

// CommandScope is the capability passed to actor authorization. It exposes no
// global database and no commit/rollback authority.
type CommandScope[Repositories any] interface {
	TransactionID() string
	Repositories() Repositories
}

type commandScope[Repositories any] struct {
	transactionID string
	repositories  Repositories
}

func (scope commandScope[Repositories]) TransactionID() string { return scope.transactionID }
func (scope commandScope[Repositories]) Repositories() Repositories {
	return scope.repositories
}

// AuthorizedActor is the transaction-verified identity used for the
// idempotency scope. Request actor fields are never trusted directly.
type AuthorizedActor struct {
	WorkspaceID string
	UserID      string
	SessionID   string
}

type CommandAuthorizer[Repositories any] interface {
	Authorize(context.Context, CommandScope[Repositories], *tammyv1.AuthenticationContext, authorisation.Action) (AuthorizedActor, error)
}

type CommandElector interface {
	Elect(context.Context, idempotency.Executor, idempotency.Scope, proto.Message) (idempotency.Election, error)
	Complete(context.Context, idempotency.Executor, idempotency.Election, proto.Message, string) ([]byte, error)
}

type CommandAuditor interface {
	Append(context.Context, CommandSQLExecutor, *tammyv1.AuditEvent, []byte) error
}

type CommandCheckpoints interface {
	Check(string) error
}

type CoordinatorConfig[Repositories any] struct {
	Transactions CommandTransactions[Repositories]
	Authorizer   CommandAuthorizer[Repositories]
	Elector      CommandElector
	Auditor      CommandAuditor
	Checkpoints  CommandCheckpoints
}

type Coordinator[Repositories any] struct {
	transactions CommandTransactions[Repositories]
	authorizer   CommandAuthorizer[Repositories]
	elector      CommandElector
	auditor      CommandAuditor
	checkpoints  CommandCheckpoints
}

// CommandResult is produced by domain work before the coordinator appends the
// audit event and persists deterministic result bytes.
type CommandResult struct {
	Result       proto.Message
	AuditEvent   *tammyv1.AuditEvent
	AuditPayload []byte
	ResourceID   string
}

// OrdinaryOperation is a closed application-owned classification. Its
// registry deliberately excludes queries, security challenges, session and
// recovery actions, external-journal restore/backup work, ownership recovery,
// and audit export jobs.
type OrdinaryOperation string

const (
	OrdinaryOperationCreateOrganisation       OrdinaryOperation = "tammy.v1.OrganisationService.CreateOrganisation"
	OrdinaryOperationUpdateOrganisation       OrdinaryOperation = "tammy.v1.OrganisationService.UpdateOrganisation"
	OrdinaryOperationRecordEntityVerification OrdinaryOperation = "tammy.v1.OrganisationService.RecordEntityVerification"
	OrdinaryOperationCreateAccount            OrdinaryOperation = "tammy.v1.AccountingService.CreateAccount"
	OrdinaryOperationUpdateAccount            OrdinaryOperation = "tammy.v1.AccountingService.UpdateAccount"
	OrdinaryOperationSetAccountStatus         OrdinaryOperation = "tammy.v1.AccountingService.SetAccountStatus"
	OrdinaryOperationPostOpeningConversion    OrdinaryOperation = "tammy.v1.AccountingService.PostOpeningConversion"
	OrdinaryOperationReplaceOpeningConversion OrdinaryOperation = "tammy.v1.AccountingService.ReplaceOpeningConversion"
	OrdinaryOperationPostManualJournal        OrdinaryOperation = "tammy.v1.AccountingService.PostManualJournal"
	OrdinaryOperationReverseJournal           OrdinaryOperation = "tammy.v1.AccountingService.ReverseJournal"
	OrdinaryOperationClosePeriod              OrdinaryOperation = "tammy.v1.AccountingService.ClosePeriod"
	OrdinaryOperationReopenPeriod             OrdinaryOperation = "tammy.v1.AccountingService.ReopenPeriod"
)

type ordinaryOperationDefinition struct {
	rpcName     string
	action      authorisation.Action
	requestType string
	resultType  string
}

// OrdinaryCommand describes transaction-local phases. SaveSource and
// PostLedger receive typed repositories only, which lets each module keep raw
// SQL and external I/O out of the coordinator surface.
type OrdinaryCommand[Repositories any] struct {
	Operation      OrdinaryOperation
	OperationKey   string
	Authentication *tammyv1.AuthenticationContext
	Request        proto.Message
	NewResult      func() proto.Message
	SaveSource     func(context.Context, Repositories, proto.Message) error
	PostLedger     func(context.Context, Repositories, proto.Message) error
	BuildResult    func(context.Context, Repositories, proto.Message) (CommandResult, error)
}

func NewCoordinator[Repositories any](config CoordinatorConfig[Repositories]) (*Coordinator[Repositories], error) {
	if nilInterface(config.Transactions) || !ids.IsCanonicalV7(config.Transactions.WorkspaceID()) ||
		nilInterface(config.Authorizer) || nilInterface(config.Elector) || nilInterface(config.Auditor) ||
		config.Checkpoints != nil && nilInterface(config.Checkpoints) {
		return nil, ErrCommand
	}
	return &Coordinator[Repositories]{transactions: config.Transactions, authorizer: config.Authorizer,
		elector: config.Elector, auditor: config.Auditor, checkpoints: config.Checkpoints}, nil
}

// Execute runs one ordinary command in one caller-owned transaction. Security
// challenges and restore orchestration intentionally use separate paths.
func (coordinator *Coordinator[Repositories]) Execute(
	ctx context.Context,
	command OrdinaryCommand[Repositories],
) (proto.Message, error) {
	if coordinator == nil || nilInterface(coordinator.transactions) || nilInterface(coordinator.authorizer) ||
		nilInterface(coordinator.elector) || nilInterface(coordinator.auditor) || ctx == nil ||
		nilInterface(command.Request) || !command.Request.ProtoReflect().IsValid() || command.Authentication == nil {
		return nil, ErrCommand
	}
	ownedRequest := proto.Clone(command.Request)
	ownedAuthentication, ok := proto.Clone(command.Authentication).(*tammyv1.AuthenticationContext)
	if nilInterface(ownedRequest) || !ownedRequest.ProtoReflect().IsValid() || !ok || ownedAuthentication == nil {
		return nil, ErrCommand
	}
	defer proto.Reset(ownedRequest)
	defer proto.Reset(ownedAuthentication)

	var completed proto.Message
	err := coordinator.transactions.Mutate(ctx, func(transactionContext context.Context, transaction CommandTransaction[Repositories]) error {
		if transactionContext == nil || nilInterface(transaction) || transaction.TransactionID() == "" ||
			nilInterface(transaction.IdempotencyExecutor()) || nilInterface(transaction.AuditExecutor()) {
			return ErrCommand
		}
		if err := transactionContext.Err(); err != nil {
			return err
		}
		operation, known := ordinaryOperationDefinitionFor(command.Operation)
		if !known {
			return ErrCommand
		}
		scope := commandScope[Repositories]{transactionID: transaction.TransactionID(), repositories: transaction.Repositories()}
		actor, err := coordinator.authorizer.Authorize(transactionContext, scope, ownedAuthentication, operation.action)
		if err != nil {
			return err
		}
		if actor.WorkspaceID != coordinator.transactions.WorkspaceID() || !ids.IsCanonicalV7(actor.WorkspaceID) ||
			!ids.IsCanonicalV7(actor.UserID) || !ids.IsCanonicalV7(actor.SessionID) ||
			!matchesAuthorizedActor(ownedAuthentication, actor) {
			return ErrCommand
		}
		if !ids.IsCanonicalV7(command.OperationKey) || command.NewResult == nil || command.BuildResult == nil ||
			string(ownedRequest.ProtoReflect().Descriptor().FullName()) != operation.requestType ||
			!matchesCommandContext(ownedRequest, ownedAuthentication, command.OperationKey) {
			return ErrCommand
		}
		semantic, err := canonical.SemanticHashV1(ownedRequest)
		if err != nil {
			return err
		}
		expectedResult := command.NewResult()
		if nilInterface(expectedResult) || !expectedResult.ProtoReflect().IsValid() {
			return ErrCommand
		}
		if string(expectedResult.ProtoReflect().Descriptor().FullName()) != operation.resultType {
			return ErrCommand
		}
		electionScope := idempotency.Scope{WorkspaceID: actor.WorkspaceID, ActorUserID: actor.UserID,
			RPCName: operation.rpcName, OperationKey: command.OperationKey}
		election, err := coordinator.elector.Elect(transactionContext, transaction.IdempotencyExecutor(), electionScope, ownedRequest)
		if err != nil {
			return err
		}
		if !matchesOwnedCommand(ownedRequest, ownedAuthentication, command.OperationKey, actor, semantic) {
			return ErrCommand
		}
		if election.Scope != electionScope || election.HashVersion != semantic.Version ||
			election.RequestType != string(ownedRequest.ProtoReflect().Descriptor().FullName()) || election.NormalizedHash != semantic.Sum {
			return ErrCommand
		}
		switch election.Decision {
		case idempotency.DecisionReplay:
			if election.ResultType != string(expectedResult.ProtoReflect().Descriptor().FullName()) || len(election.ResultProto) == 0 {
				return ErrCommand
			}
			if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(election.ResultProto, expectedResult); err != nil ||
				len(expectedResult.ProtoReflect().GetUnknown()) != 0 {
				return ErrCommand
			}
			if _, err := canonical.NormalizedJSON(expectedResult); err != nil {
				return err
			}
			canonicalResult, err := proto.MarshalOptions{Deterministic: true}.Marshal(expectedResult)
			if err != nil || !bytes.Equal(canonicalResult, election.ResultProto) {
				return ErrCommand
			}
			completed = proto.Clone(expectedResult)
			return nil
		case idempotency.DecisionExecute:
		default:
			return ErrCommand
		}

		repositories := transaction.Repositories()
		if command.SaveSource != nil {
			if err := command.SaveSource(transactionContext, repositories, ownedRequest); err != nil {
				return err
			}
			if !matchesOwnedCommand(ownedRequest, ownedAuthentication, command.OperationKey, actor, semantic) {
				return ErrCommand
			}
			if err := coordinator.check(CheckpointAfterSourceSave); err != nil {
				return err
			}
		}
		if command.PostLedger != nil {
			if err := command.PostLedger(transactionContext, repositories, ownedRequest); err != nil {
				return err
			}
			if !matchesOwnedCommand(ownedRequest, ownedAuthentication, command.OperationKey, actor, semantic) {
				return ErrCommand
			}
			if err := coordinator.check(CheckpointAfterLedgerPost); err != nil {
				return err
			}
		}
		outcome, err := command.BuildResult(transactionContext, repositories, ownedRequest)
		if err != nil {
			return err
		}
		if !matchesOwnedCommand(ownedRequest, ownedAuthentication, command.OperationKey, actor, semantic) {
			return ErrCommand
		}
		if nilInterface(outcome.Result) || outcome.AuditEvent == nil || len(outcome.AuditPayload) == 0 ||
			outcome.Result.ProtoReflect().Descriptor().FullName() != expectedResult.ProtoReflect().Descriptor().FullName() {
			return ErrCommand
		}
		ownedResult := proto.Clone(outcome.Result)
		ownedAuditEvent, ok := proto.Clone(outcome.AuditEvent).(*tammyv1.AuditEvent)
		ownedAuditPayload := append([]byte(nil), outcome.AuditPayload...)
		if nilInterface(ownedResult) || !ownedResult.ProtoReflect().IsValid() ||
			ownedResult.ProtoReflect().Descriptor().FullName() != expectedResult.ProtoReflect().Descriptor().FullName() ||
			!ok || ownedAuditEvent == nil || len(ownedAuditPayload) == 0 {
			return ErrCommand
		}
		defer proto.Reset(ownedResult)
		defer proto.Reset(ownedAuditEvent)
		defer clear(ownedAuditPayload)
		if _, err := canonical.NormalizedJSON(ownedResult); err != nil {
			return err
		}
		encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(ownedResult)
		if err != nil {
			return ErrCommand
		}
		if _, err := canonical.NormalizedJSON(ownedAuditEvent); err != nil {
			return err
		}
		if !matchesAuditProvenance(ownedAuditEvent, ownedAuthentication, actor, operation.rpcName,
			command.OperationKey, ownedResult, encoded) {
			return ErrCommand
		}
		if err := coordinator.auditor.Append(transactionContext, transaction.AuditExecutor(), ownedAuditEvent,
			ownedAuditPayload); err != nil {
			return err
		}
		if err := coordinator.check(CheckpointAfterAuditAppend); err != nil {
			return err
		}
		if err := coordinator.check(CheckpointAfterResultSerialization); err != nil {
			return err
		}
		completionResult := proto.Clone(ownedResult)
		if nilInterface(completionResult) || !completionResult.ProtoReflect().IsValid() ||
			completionResult.ProtoReflect().Descriptor().FullName() != expectedResult.ProtoReflect().Descriptor().FullName() {
			return ErrCommand
		}
		defer proto.Reset(completionResult)
		persisted, err := coordinator.elector.Complete(transactionContext, transaction.IdempotencyExecutor(), election,
			completionResult, outcome.ResourceID)
		if err != nil {
			return err
		}
		if !bytes.Equal(encoded, persisted) {
			return ErrCommand
		}
		completed = proto.Clone(ownedResult)
		return nil
	})
	if err != nil {
		return nil, errors.Join(ErrCommand, err)
	}
	if nilInterface(completed) {
		return nil, ErrCommand
	}
	return completed, nil
}

func matchesCommandContext(request proto.Message, authentication *tammyv1.AuthenticationContext, operationKey string) bool {
	if nilInterface(request) || authentication == nil || !ids.IsCanonicalV7(operationKey) {
		return false
	}
	message := request.ProtoReflect()
	field := message.Descriptor().Fields().ByName("command_context")
	if field == nil || field.IsList() || field.IsMap() || field.Message() == nil ||
		field.Message().FullName() != "tammy.v1.CommandContext" || !message.Has(field) {
		return false
	}
	commandContext, ok := message.Get(field).Message().Interface().(*tammyv1.CommandContext)
	return ok && commandContext != nil && commandContext.IdempotencyKey == operationKey &&
		commandContext.Authentication != nil && proto.Equal(commandContext.Authentication, authentication)
}

func matchesAuthorizedActor(authentication *tammyv1.AuthenticationContext, actor AuthorizedActor) bool {
	return authentication != nil && authentication.ActorUserId == actor.UserID && authentication.SessionId == actor.SessionID
}

func matchesAuditProvenance(event *tammyv1.AuditEvent, authentication *tammyv1.AuthenticationContext,
	actor AuthorizedActor, rpcName, operationKey string, result proto.Message, encoded []byte,
) bool {
	if event == nil || authentication == nil || nilInterface(result) || event.WorkspaceId != actor.WorkspaceID ||
		!proto.Equal(event.Actor, authentication) || event.CommandType != rpcName || event.IdempotencyKey == nil ||
		event.GetIdempotencyKey() != operationKey || event.Result == nil ||
		event.Result.TypeName != string(result.ProtoReflect().Descriptor().FullName()) || len(encoded) == 0 {
		return false
	}
	digest := sha256.Sum256(encoded)
	return len(event.Result.DeterministicSha256) == sha256.Size &&
		subtle.ConstantTimeCompare(event.Result.DeterministicSha256, digest[:]) == 1
}

func matchesOwnedCommand(request proto.Message, authentication *tammyv1.AuthenticationContext, operationKey string,
	actor AuthorizedActor, expected canonical.SemanticHash,
) bool {
	if !matchesCommandContext(request, authentication, operationKey) || !matchesAuthorizedActor(authentication, actor) {
		return false
	}
	current, err := canonical.SemanticHashV1(request)
	return err == nil && current == expected
}

func ordinaryOperationDefinitionFor(operation OrdinaryOperation) (ordinaryOperationDefinition, bool) {
	definition := ordinaryOperationDefinition{rpcName: string(operation)}
	switch operation {
	case OrdinaryOperationCreateOrganisation:
		definition.action = authorisation.ActionManageOrg
		definition.requestType = "tammy.v1.CreateOrganisationRequest"
		definition.resultType = "tammy.v1.CreateOrganisationResponse"
	case OrdinaryOperationUpdateOrganisation:
		definition.action = authorisation.ActionManageOrg
		definition.requestType = "tammy.v1.UpdateOrganisationRequest"
		definition.resultType = "tammy.v1.UpdateOrganisationResponse"
	case OrdinaryOperationRecordEntityVerification:
		definition.action = authorisation.ActionManageOrg
		definition.requestType = "tammy.v1.RecordEntityVerificationRequest"
		definition.resultType = "tammy.v1.RecordEntityVerificationResponse"
	case OrdinaryOperationCreateAccount:
		definition.action = authorisation.ActionManageAccounts
		definition.requestType = "tammy.v1.CreateAccountRequest"
		definition.resultType = "tammy.v1.CreateAccountResponse"
	case OrdinaryOperationUpdateAccount:
		definition.action = authorisation.ActionManageAccounts
		definition.requestType = "tammy.v1.UpdateAccountRequest"
		definition.resultType = "tammy.v1.UpdateAccountResponse"
	case OrdinaryOperationSetAccountStatus:
		definition.action = authorisation.ActionManageAccounts
		definition.requestType = "tammy.v1.SetAccountStatusRequest"
		definition.resultType = "tammy.v1.SetAccountStatusResponse"
	case OrdinaryOperationPostOpeningConversion:
		definition.action = authorisation.ActionPostAccounting
		definition.requestType = "tammy.v1.PostOpeningConversionRequest"
		definition.resultType = "tammy.v1.PostOpeningConversionResponse"
	case OrdinaryOperationReplaceOpeningConversion:
		definition.action = authorisation.ActionPostAccounting
		definition.requestType = "tammy.v1.ReplaceOpeningConversionRequest"
		definition.resultType = "tammy.v1.ReplaceOpeningConversionResponse"
	case OrdinaryOperationPostManualJournal:
		definition.action = authorisation.ActionPostAccounting
		definition.requestType = "tammy.v1.PostManualJournalRequest"
		definition.resultType = "tammy.v1.PostManualJournalResponse"
	case OrdinaryOperationReverseJournal:
		definition.action = authorisation.ActionPostAccounting
		definition.requestType = "tammy.v1.ReverseJournalRequest"
		definition.resultType = "tammy.v1.ReverseJournalResponse"
	case OrdinaryOperationClosePeriod:
		definition.action = authorisation.ActionPostAccounting
		definition.requestType = "tammy.v1.ClosePeriodRequest"
		definition.resultType = "tammy.v1.ClosePeriodResponse"
	case OrdinaryOperationReopenPeriod:
		definition.action = authorisation.ActionPostAccounting
		definition.requestType = "tammy.v1.ReopenPeriodRequest"
		definition.resultType = "tammy.v1.ReopenPeriodResponse"
	default:
		return ordinaryOperationDefinition{}, false
	}
	return definition, true
}

func (coordinator *Coordinator[Repositories]) check(stage string) error {
	if coordinator.checkpoints == nil {
		return nil
	}
	return coordinator.checkpoints.Check(stage)
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
