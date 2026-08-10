package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"reflect"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/authorisation"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/idempotency"
	"github.com/tammyapp/tammy/services/core/internal/platform/canonical"
	"google.golang.org/protobuf/proto"
)

var errCommandInjected = errors.New("injected command failure")

const (
	commandWorkspaceID = "018f0000-0000-7000-8000-000000000001"
	commandActorID     = "018f0000-0000-7000-8000-000000000002"
	commandSessionID   = "018f0000-0000-7000-8000-000000000003"
	commandOperationID = "018f0000-0000-7000-8000-000000000004"
)

type commandState struct {
	source  []string
	ledger  []string
	audit   []string
	results [][]byte
}

func cloneCommandState(state commandState) commandState {
	clone := commandState{source: append([]string(nil), state.source...), ledger: append([]string(nil), state.ledger...), audit: append([]string(nil), state.audit...)}
	for _, result := range state.results {
		clone.results = append(clone.results, append([]byte(nil), result...))
	}
	return clone
}

type commandRepositories struct{ state *commandState }

type commandTransaction struct {
	repositories commandRepositories
}

func (transaction *commandTransaction) TransactionID() string { return "tx-1" }
func (transaction *commandTransaction) Repositories() commandRepositories {
	return transaction.repositories
}
func (transaction *commandTransaction) IdempotencyExecutor() idempotency.Executor { return transaction }
func (transaction *commandTransaction) AuditExecutor() CommandSQLExecutor {
	return transaction
}
func (*commandTransaction) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errors.New("unexpected SQL in coordinator unit test")
}
func (*commandTransaction) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, errors.New("unexpected SQL in coordinator unit test")
}

type commandTransactions struct {
	state      commandState
	order      *[]string
	commits    int
	rollbacks  int
	beforeWork func()
}

func (transactions *commandTransactions) WorkspaceID() string { return commandWorkspaceID }
func (transactions *commandTransactions) Mutate(ctx context.Context, work func(context.Context, CommandTransaction[commandRepositories]) error) error {
	*transactions.order = append(*transactions.order, "begin")
	staged := cloneCommandState(transactions.state)
	transaction := &commandTransaction{repositories: commandRepositories{state: &staged}}
	if transactions.beforeWork != nil {
		transactions.beforeWork()
	}
	if err := work(ctx, transaction); err != nil {
		transactions.rollbacks++
		*transactions.order = append(*transactions.order, "rollback")
		return err
	}
	transactions.commits++
	transactions.state = staged
	*transactions.order = append(*transactions.order, "commit")
	return nil
}

type commandAuthorizer struct {
	order                  *[]string
	afterAuthorize         func()
	capturedAuthentication *tammyv1.AuthenticationContext
	actor                  *AuthorizedActor
}

func (authorizer *commandAuthorizer) Authorize(
	_ context.Context,
	_ CommandScope[commandRepositories],
	authentication *tammyv1.AuthenticationContext,
	_ authorisation.Action,
) (AuthorizedActor, error) {
	*authorizer.order = append(*authorizer.order, "authorize")
	authorizer.capturedAuthentication = authentication
	if authentication == nil || authentication.ActorUserId != commandActorID || authentication.SessionId != commandSessionID {
		return AuthorizedActor{}, errors.New("authorization denied")
	}
	if authorizer.afterAuthorize != nil {
		authorizer.afterAuthorize()
	}
	if authorizer.actor != nil {
		return *authorizer.actor, nil
	}
	return AuthorizedActor{WorkspaceID: commandWorkspaceID, UserID: commandActorID, SessionID: commandSessionID}, nil
}

type commandElector struct {
	order           *[]string
	replay          []byte
	calls           int
	observedRequest proto.Message
	mutateElect     func(proto.Message)
	mutateComplete  func(proto.Message)
}

func (elector *commandElector) Elect(_ context.Context, _ idempotency.Executor, scope idempotency.Scope, request proto.Message) (idempotency.Election, error) {
	elector.calls++
	*elector.order = append(*elector.order, "elect")
	elector.observedRequest = proto.Clone(request)
	semantic, err := canonical.SemanticHashV1(request)
	if err != nil {
		return idempotency.Election{}, err
	}
	if elector.mutateElect != nil {
		elector.mutateElect(request)
	}
	decision := idempotency.DecisionExecute
	if elector.replay != nil {
		decision = idempotency.DecisionReplay
	}
	return idempotency.Election{Decision: decision, Scope: scope, HashVersion: semantic.Version,
		RequestType: string(request.ProtoReflect().Descriptor().FullName()), NormalizedHash: semantic.Sum,
		ResultType: "tammy.v1.CreateOrganisationResponse", ResultProto: append([]byte(nil), elector.replay...), Attempt: 1}, nil
}

func (elector *commandElector) Complete(
	_ context.Context,
	executor idempotency.Executor,
	_ idempotency.Election,
	result proto.Message,
	_ string,
) ([]byte, error) {
	*elector.order = append(*elector.order, "complete")
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(result)
	if err != nil {
		return nil, err
	}
	if elector.mutateComplete != nil {
		elector.mutateComplete(result)
	}
	transaction := executor.(*commandTransaction)
	transaction.repositories.state.results = append(transaction.repositories.state.results, append([]byte(nil), encoded...))
	return encoded, nil
}

type commandAuditor struct {
	order         *[]string
	calls         int
	capturedEvent *tammyv1.AuditEvent
	capturedBody  []byte
	mutate        func(*tammyv1.AuditEvent, []byte)
}

func (auditor *commandAuditor) Append(
	_ context.Context,
	executor CommandSQLExecutor,
	event *tammyv1.AuditEvent,
	payload []byte,
) error {
	*auditor.order = append(*auditor.order, "audit")
	auditor.calls++
	auditor.capturedEvent = event
	auditor.capturedBody = payload
	if auditor.mutate != nil {
		auditor.mutate(event, payload)
	}
	transaction := executor.(*commandTransaction)
	transaction.repositories.state.audit = append(transaction.repositories.state.audit, "event")
	return nil
}

type commandCheckpoints struct {
	order   *[]string
	failure string
}

func (checkpoints *commandCheckpoints) Check(stage string) error {
	*checkpoints.order = append(*checkpoints.order, stage)
	if stage == checkpoints.failure {
		return errCommandInjected
	}
	return nil
}

func TestCoordinatorExecutesAuthorizedCanonicalCommandInOneTransaction(t *testing.T) {
	coordinator, transactions, order := newCommandCoordinatorHarness(t, "")
	result, err := coordinator.Execute(context.Background(), validCommand())
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result == nil || result.(*tammyv1.CreateOrganisationResponse).Organisation == nil {
		t.Fatalf("Execute() result = %#v", result)
	}
	wantOrder := []string{"begin", "authorize", "elect", CheckpointAfterSourceSave,
		CheckpointAfterLedgerPost, "audit", CheckpointAfterAuditAppend,
		CheckpointAfterResultSerialization, "complete", "commit"}
	if !reflect.DeepEqual(*order, wantOrder) {
		t.Fatalf("order = %v, want %v", *order, wantOrder)
	}
	if transactions.commits != 1 || transactions.rollbacks != 0 ||
		!reflect.DeepEqual(transactions.state.source, []string{"source"}) ||
		!reflect.DeepEqual(transactions.state.ledger, []string{"ledger"}) ||
		!reflect.DeepEqual(transactions.state.audit, []string{"event"}) || len(transactions.state.results) != 1 {
		t.Fatalf("state=%#v commits=%d rollbacks=%d", transactions.state, transactions.commits, transactions.rollbacks)
	}
}

func TestCoordinatorRollsBackEveryOrdinaryCommandBoundary(t *testing.T) {
	for _, failure := range []string{
		CheckpointAfterSourceSave,
		CheckpointAfterLedgerPost,
		CheckpointAfterAuditAppend,
		CheckpointAfterResultSerialization,
	} {
		t.Run(failure, func(t *testing.T) {
			coordinator, transactions, _ := newCommandCoordinatorHarness(t, failure)
			result, err := coordinator.Execute(context.Background(), validCommand())
			if result != nil || !errors.Is(err, errCommandInjected) {
				t.Fatalf("Execute() = %#v, %v; want injected failure", result, err)
			}
			if transactions.commits != 0 || transactions.rollbacks != 1 || !reflect.DeepEqual(transactions.state, commandState{}) {
				t.Fatalf("state=%#v commits=%d rollbacks=%d", transactions.state, transactions.commits, transactions.rollbacks)
			}
		})
	}
}

func TestCoordinatorOpensTransactionBeforeAuthAndRejectsUnknownFieldsBeforeElection(t *testing.T) {
	coordinator, transactions, order := newCommandCoordinatorHarness(t, "")
	command := validCommand()
	command.Request.ProtoReflect().SetUnknown([]byte{0x98, 0x06, 0x01})
	result, err := coordinator.Execute(context.Background(), command)
	if result != nil || !errors.Is(err, canonical.ErrUnknownFields) {
		t.Fatalf("Execute() = %#v, %v; want unknown-field rejection", result, err)
	}
	if !reflect.DeepEqual(*order, []string{"begin", "authorize", "rollback"}) || transactions.rollbacks != 1 {
		t.Fatalf("order=%v rollbacks=%d", *order, transactions.rollbacks)
	}
}

func TestCoordinatorReturnsCanonicalReplayWithoutDomainOrAuditWork(t *testing.T) {
	replayed := &tammyv1.CreateOrganisationResponse{Organisation: &tammyv1.Organisation{Id: commandWorkspaceID, Version: 1}}
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(replayed)
	if err != nil {
		t.Fatal(err)
	}
	coordinator, transactions, order := newCommandCoordinatorHarness(t, "")
	coordinator.elector.(*commandElector).replay = encoded
	result, err := coordinator.Execute(context.Background(), validCommand())
	if err != nil || !proto.Equal(result, replayed) {
		t.Fatalf("Execute() = %#v, %v; want canonical replay", result, err)
	}
	if !reflect.DeepEqual(*order, []string{"begin", "authorize", "elect", "commit"}) || transactions.commits != 1 ||
		!reflect.DeepEqual(transactions.state, commandState{}) {
		t.Fatalf("order=%v state=%#v commits=%d", *order, transactions.state, transactions.commits)
	}
}

func TestCoordinatorRejectsNestedUnknownFieldsInReplay(t *testing.T) {
	replayed := &tammyv1.CreateOrganisationResponse{Organisation: &tammyv1.Organisation{Id: commandWorkspaceID, Version: 1}}
	replayed.Organisation.ProtoReflect().SetUnknown([]byte{0x98, 0x06, 0x01})
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(replayed)
	if err != nil {
		t.Fatal(err)
	}
	coordinator, transactions, _ := newCommandCoordinatorHarness(t, "")
	coordinator.elector.(*commandElector).replay = encoded
	result, err := coordinator.Execute(context.Background(), validCommand())
	if result != nil || !errors.Is(err, canonical.ErrUnknownFields) {
		t.Fatalf("Execute() = %#v, %v; want recursive unknown-field rejection", result, err)
	}
	if transactions.commits != 0 || transactions.rollbacks != 1 {
		t.Fatalf("commits=%d rollbacks=%d, want rollback", transactions.commits, transactions.rollbacks)
	}
}

func TestCoordinatorOwnsRequestAndAuthenticationBeforeOpeningTransaction(t *testing.T) {
	coordinator, transactions, _ := newCommandCoordinatorHarness(t, "")
	command := validCommand()
	request := command.Request.(*tammyv1.CreateOrganisationRequest)
	transactions.beforeWork = func() {
		command.Authentication.ActorUserId = "018f0000-0000-7000-8000-000000000099"
		request.Abn = "53004085616"
	}

	result, err := coordinator.Execute(context.Background(), command)
	if err != nil || result == nil {
		t.Fatalf("Execute() = %#v, %v; caller mutation changed owned command", result, err)
	}
	elector := coordinator.elector.(*commandElector)
	observed := elector.observedRequest.(*tammyv1.CreateOrganisationRequest)
	if observed.Abn != "51824753556" {
		t.Fatalf("elector observed caller mutation %q", observed.Abn)
	}
	authorizer := coordinator.authorizer.(*commandAuthorizer)
	if captured := authorizer.capturedAuthentication; captured == nil || captured.ActorUserId != "" || captured.SessionId != "" {
		t.Fatalf("owned authentication retained after Execute: %#v", captured)
	}
}

func TestCoordinatorDomainPhasesUseEntryOwnedRequest(t *testing.T) {
	coordinator, transactions, _ := newCommandCoordinatorHarness(t, "")
	command := validCommand()
	callerRequest := command.Request.(*tammyv1.CreateOrganisationRequest)
	transactions.beforeWork = func() {
		callerRequest.Abn = "53004085616"
	}
	command.SaveSource = func(_ context.Context, repositories commandRepositories, request proto.Message) error {
		owned := request.(*tammyv1.CreateOrganisationRequest)
		repositories.state.source = append(repositories.state.source, owned.Abn)
		callerRequest.Abn = "83080491935"
		return nil
	}
	command.PostLedger = func(_ context.Context, repositories commandRepositories, request proto.Message) error {
		owned := request.(*tammyv1.CreateOrganisationRequest)
		repositories.state.ledger = append(repositories.state.ledger, owned.Abn)
		return nil
	}
	command.BuildResult = func(_ context.Context, _ commandRepositories, request proto.Message) (CommandResult, error) {
		owned := request.(*tammyv1.CreateOrganisationRequest)
		result := &tammyv1.CreateOrganisationResponse{Organisation: &tammyv1.Organisation{
			Id: commandWorkspaceID, Version: 1, Abn: owned.Abn,
		}}
		return validCommandOutcomeForResult(result), nil
	}

	result, err := coordinator.Execute(context.Background(), command)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !reflect.DeepEqual(transactions.state.source, []string{"51824753556"}) ||
		!reflect.DeepEqual(transactions.state.ledger, []string{"51824753556"}) ||
		result.(*tammyv1.CreateOrganisationResponse).Organisation.Abn != "51824753556" {
		t.Fatalf("domain phases observed caller mutation: state=%#v result=%#v", transactions.state, result)
	}
}

func TestCoordinatorIgnoresCallerMutationBetweenAuthorizationAndElection(t *testing.T) {
	coordinator, _, _ := newCommandCoordinatorHarness(t, "")
	command := validCommand()
	request := command.Request.(*tammyv1.CreateOrganisationRequest)
	coordinator.authorizer.(*commandAuthorizer).afterAuthorize = func() {
		request.Abn = "53004085616"
	}

	result, err := coordinator.Execute(context.Background(), command)
	if err != nil || result == nil {
		t.Fatalf("Execute() = %#v, %v", result, err)
	}
	observed := coordinator.elector.(*commandElector).observedRequest.(*tammyv1.CreateOrganisationRequest)
	if observed.Abn != "51824753556" {
		t.Fatalf("elector observed between-callback caller mutation %q", observed.Abn)
	}
}

func TestCoordinatorRejectsElectorMutationWithoutMutatingCaller(t *testing.T) {
	coordinator, transactions, _ := newCommandCoordinatorHarness(t, "")
	command := validCommand()
	elector := coordinator.elector.(*commandElector)
	elector.mutateElect = func(message proto.Message) {
		message.(*tammyv1.CreateOrganisationRequest).Abn = "53004085616"
	}

	result, err := coordinator.Execute(context.Background(), command)
	if result != nil || !errors.Is(err, ErrCommand) {
		t.Fatalf("Execute() = %#v, %v; want mutation rejection", result, err)
	}
	if got := command.Request.(*tammyv1.CreateOrganisationRequest).Abn; got != "51824753556" {
		t.Fatalf("caller request mutated to %q", got)
	}
	if transactions.commits != 0 || transactions.rollbacks != 1 || !reflect.DeepEqual(transactions.state, commandState{}) {
		t.Fatalf("state=%#v commits=%d rollbacks=%d", transactions.state, transactions.commits, transactions.rollbacks)
	}
}

func TestCoordinatorBindsAuthorizedActorToOwnedAuthentication(t *testing.T) {
	coordinator, transactions, _ := newCommandCoordinatorHarness(t, "")
	coordinator.authorizer.(*commandAuthorizer).actor = &AuthorizedActor{
		WorkspaceID: commandWorkspaceID,
		UserID:      "018f0000-0000-7000-8000-000000000099",
		SessionID:   commandSessionID,
	}

	result, err := coordinator.Execute(context.Background(), validCommand())
	if result != nil || !errors.Is(err, ErrCommand) {
		t.Fatalf("Execute() = %#v, %v; want actor/auth binding rejection", result, err)
	}
	if coordinator.elector.(*commandElector).calls != 0 || transactions.commits != 0 || transactions.rollbacks != 1 {
		t.Fatalf("elect=%d commits=%d rollbacks=%d", coordinator.elector.(*commandElector).calls, transactions.commits, transactions.rollbacks)
	}
}

func TestCoordinatorRejectsElectionMutationOfCommandMetadata(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*tammyv1.CreateOrganisationRequest)
	}{
		{name: "authentication", mutate: func(request *tammyv1.CreateOrganisationRequest) {
			request.CommandContext.Authentication.ActorUserId = "018f0000-0000-7000-8000-000000000099"
		}},
		{name: "idempotency key", mutate: func(request *tammyv1.CreateOrganisationRequest) {
			request.CommandContext.IdempotencyKey = "018f0000-0000-7000-8000-000000000099"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			coordinator, transactions, _ := newCommandCoordinatorHarness(t, "")
			coordinator.elector.(*commandElector).mutateElect = func(message proto.Message) {
				test.mutate(message.(*tammyv1.CreateOrganisationRequest))
			}

			result, err := coordinator.Execute(context.Background(), validCommand())
			if result != nil || !errors.Is(err, ErrCommand) {
				t.Fatalf("Execute() = %#v, %v; want metadata mutation rejection", result, err)
			}
			if transactions.commits != 0 || transactions.rollbacks != 1 || !reflect.DeepEqual(transactions.state, commandState{}) {
				t.Fatalf("state=%#v commits=%d rollbacks=%d", transactions.state, transactions.commits, transactions.rollbacks)
			}
		})
	}
}

func TestCoordinatorOwnsResultAcrossMaliciousCompletionMutation(t *testing.T) {
	coordinator, transactions, _ := newCommandCoordinatorHarness(t, "")
	command := validCommand()
	var built *tammyv1.CreateOrganisationResponse
	command.BuildResult = func(context.Context, commandRepositories, proto.Message) (CommandResult, error) {
		built = &tammyv1.CreateOrganisationResponse{Organisation: &tammyv1.Organisation{Id: commandWorkspaceID, Version: 1}}
		return validCommandOutcomeForResult(built), nil
	}
	coordinator.elector.(*commandElector).mutateComplete = func(message proto.Message) {
		message.(*tammyv1.CreateOrganisationResponse).Organisation.Version = 99
	}

	result, err := coordinator.Execute(context.Background(), command)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := result.(*tammyv1.CreateOrganisationResponse).Organisation.Version; got != 1 {
		t.Fatalf("returned result version = %d, want immutable 1", got)
	}
	if built.Organisation.Version != 1 {
		t.Fatalf("domain-owned result mutated to version %d", built.Organisation.Version)
	}
	want, err := proto.MarshalOptions{Deterministic: true}.Marshal(built)
	if err != nil {
		t.Fatal(err)
	}
	if len(transactions.state.results) != 1 || !reflect.DeepEqual(transactions.state.results[0], want) {
		t.Fatalf("persisted result = %x, want %x", transactions.state.results, want)
	}
}

func TestCoordinatorRejectsAuditProvenanceMismatchBeforeAppend(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*tammyv1.AuditEvent)
	}{
		{name: "workspace", mutate: func(event *tammyv1.AuditEvent) {
			event.WorkspaceId = "018f0000-0000-7000-8000-000000000099"
		}},
		{name: "actor user", mutate: func(event *tammyv1.AuditEvent) {
			event.Actor.ActorUserId = "018f0000-0000-7000-8000-000000000099"
		}},
		{name: "actor session", mutate: func(event *tammyv1.AuditEvent) {
			event.Actor.SessionId = "018f0000-0000-7000-8000-000000000099"
		}},
		{name: "command type", mutate: func(event *tammyv1.AuditEvent) {
			event.CommandType = "tammy.v1.OrganisationService.UpdateOrganisation"
		}},
		{name: "idempotency key", mutate: func(event *tammyv1.AuditEvent) {
			wrong := "018f0000-0000-7000-8000-000000000099"
			event.IdempotencyKey = &wrong
		}},
		{name: "result type", mutate: func(event *tammyv1.AuditEvent) {
			event.Result.TypeName = "tammy.v1.UpdateOrganisationResponse"
		}},
		{name: "result digest", mutate: func(event *tammyv1.AuditEvent) {
			event.Result.DeterministicSha256[0] ^= 0xff
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			coordinator, transactions, _ := newCommandCoordinatorHarness(t, "")
			command := validCommand()
			command.BuildResult = func(context.Context, commandRepositories, proto.Message) (CommandResult, error) {
				outcome := validCommandOutcome()
				test.mutate(outcome.AuditEvent)
				return outcome, nil
			}

			result, err := coordinator.Execute(context.Background(), command)
			if result != nil || !errors.Is(err, ErrCommand) {
				t.Fatalf("Execute() = %#v, %v; want audit binding rejection", result, err)
			}
			if coordinator.auditor.(*commandAuditor).calls != 0 || transactions.commits != 0 || transactions.rollbacks != 1 ||
				!reflect.DeepEqual(transactions.state, commandState{}) {
				t.Fatalf("audits=%d state=%#v commits=%d rollbacks=%d", coordinator.auditor.(*commandAuditor).calls,
					transactions.state, transactions.commits, transactions.rollbacks)
			}
		})
	}
}

func TestCoordinatorOwnsAuditProvenanceAcrossMaliciousAuditorMutation(t *testing.T) {
	coordinator, transactions, _ := newCommandCoordinatorHarness(t, "")
	command := validCommand()
	want := validCommandOutcome()
	builtEvent := want.AuditEvent
	builtPayload := want.AuditPayload
	command.BuildResult = func(context.Context, commandRepositories, proto.Message) (CommandResult, error) {
		return want, nil
	}
	auditor := coordinator.auditor.(*commandAuditor)
	auditor.mutate = func(event *tammyv1.AuditEvent, payload []byte) {
		event.CommandType = "tampered"
		event.Actor.ActorUserId = "tampered"
		event.Result.DeterministicSha256[0] ^= 0xff
		payload[0] ^= 0xff
	}

	result, err := coordinator.Execute(context.Background(), command)
	if err != nil || result.(*tammyv1.CreateOrganisationResponse).Organisation.Version != 1 {
		t.Fatalf("Execute() = %#v, %v", result, err)
	}
	pristine := validCommandOutcome()
	if !proto.Equal(builtEvent, pristine.AuditEvent) || !reflect.DeepEqual(builtPayload, pristine.AuditPayload) {
		t.Fatalf("domain-owned audit provenance mutated: event=%#v payload=%x", builtEvent, builtPayload)
	}
	if transactions.commits != 1 || len(transactions.state.audit) != 1 || len(transactions.state.results) != 1 {
		t.Fatalf("state=%#v commits=%d", transactions.state, transactions.commits)
	}
}

func TestCoordinatorRejectsCommandsOutsideClosedOrdinaryClassification(t *testing.T) {
	for _, test := range []struct {
		name string
		rpc  string
	}{
		{name: "workspace unlock", rpc: "tammy.v1.WorkspaceService.UnlockWorkspace"},
		{name: "factor confirmation", rpc: "tammy.v1.IdentityService.ConfirmTOTP"},
		{name: "restore", rpc: "tammy.v1.WorkspaceService.RestoreWorkspace"},
		{name: "query", rpc: "tammy.v1.OrganisationService.GetOrganisation"},
		{name: "ownership transfer", rpc: "tammy.v1.WorkspaceService.TransferOwnership"},
		{name: "arbitrary valid shape", rpc: "tammy.v1.ArbitraryService.Mutate"},
	} {
		t.Run(test.name, func(t *testing.T) {
			coordinator, transactions, _ := newCommandCoordinatorHarness(t, "")
			command := validCommand()
			command.Operation = OrdinaryOperation(test.rpc)

			result, err := coordinator.Execute(context.Background(), command)
			if result != nil || !errors.Is(err, ErrCommand) {
				t.Fatalf("Execute() = %#v, %v; want closed-classification rejection", result, err)
			}
			if transactions.commits != 0 || transactions.rollbacks != 1 || !reflect.DeepEqual(transactions.state, commandState{}) {
				t.Fatalf("state=%#v commits=%d rollbacks=%d", transactions.state, transactions.commits, transactions.rollbacks)
			}
		})
	}
}

func TestOrdinaryOperationRegistryContainsOnlyPlannedMutationClasses(t *testing.T) {
	for _, test := range []struct {
		operation OrdinaryOperation
		rpc       string
		action    authorisation.Action
	}{
		{OrdinaryOperationCreateOrganisation, "tammy.v1.OrganisationService.CreateOrganisation", authorisation.ActionManageOrg},
		{OrdinaryOperationUpdateOrganisation, "tammy.v1.OrganisationService.UpdateOrganisation", authorisation.ActionManageOrg},
		{OrdinaryOperationRecordEntityVerification, "tammy.v1.OrganisationService.RecordEntityVerification", authorisation.ActionManageOrg},
		{OrdinaryOperationCreateAccount, "tammy.v1.AccountingService.CreateAccount", authorisation.ActionManageAccounts},
		{OrdinaryOperationUpdateAccount, "tammy.v1.AccountingService.UpdateAccount", authorisation.ActionManageAccounts},
		{OrdinaryOperationSetAccountStatus, "tammy.v1.AccountingService.SetAccountStatus", authorisation.ActionManageAccounts},
		{OrdinaryOperationPostOpeningConversion, "tammy.v1.AccountingService.PostOpeningConversion", authorisation.ActionPostAccounting},
		{OrdinaryOperationReplaceOpeningConversion, "tammy.v1.AccountingService.ReplaceOpeningConversion", authorisation.ActionPostAccounting},
		{OrdinaryOperationPostManualJournal, "tammy.v1.AccountingService.PostManualJournal", authorisation.ActionPostAccounting},
		{OrdinaryOperationReverseJournal, "tammy.v1.AccountingService.ReverseJournal", authorisation.ActionPostAccounting},
		{OrdinaryOperationClosePeriod, "tammy.v1.AccountingService.ClosePeriod", authorisation.ActionPostAccounting},
		{OrdinaryOperationReopenPeriod, "tammy.v1.AccountingService.ReopenPeriod", authorisation.ActionPostAccounting},
	} {
		definition, ok := ordinaryOperationDefinitionFor(test.operation)
		if !ok || definition.rpcName != test.rpc || definition.action != test.action ||
			definition.requestType == "" || definition.resultType == "" {
			t.Fatalf("operation %q = %#v, %t", test.operation, definition, ok)
		}
	}
}

func TestCoordinatorFailsClosedOnCancellationAndTypedNilComposition(t *testing.T) {
	coordinator, transactions, order := newCommandCoordinatorHarness(t, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := coordinator.Execute(ctx, validCommand()); result != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Execute() = %#v, %v", result, err)
	}
	if transactions.commits != 0 || transactions.rollbacks != 1 || !reflect.DeepEqual(*order, []string{"begin", "rollback"}) {
		t.Fatalf("cancel order=%v commits=%d rollbacks=%d", *order, transactions.commits, transactions.rollbacks)
	}

	var typedNilAuthorizer *commandAuthorizer
	if invalid, err := NewCoordinator(CoordinatorConfig[commandRepositories]{
		Transactions: transactions,
		Authorizer:   typedNilAuthorizer,
		Elector:      &commandElector{order: order},
		Auditor:      &commandAuditor{order: order},
	}); invalid != nil || !errors.Is(err, ErrCommand) {
		t.Fatalf("NewCoordinator(typed nil) = %#v, %v", invalid, err)
	}
}

func TestCoordinatorRejectsDetachedAuthenticationAndOperationMetadata(t *testing.T) {
	coordinator, transactions, _ := newCommandCoordinatorHarness(t, "")
	for _, mutate := range []func(*tammyv1.CreateOrganisationRequest){
		func(request *tammyv1.CreateOrganisationRequest) {
			request.CommandContext.Authentication.ActorUserId = "018f0000-0000-7000-8000-000000000099"
		},
		func(request *tammyv1.CreateOrganisationRequest) {
			request.CommandContext.IdempotencyKey = "018f0000-0000-7000-8000-000000000099"
		},
	} {
		command := validCommand()
		mutate(command.Request.(*tammyv1.CreateOrganisationRequest))
		if result, err := coordinator.Execute(context.Background(), command); result != nil || !errors.Is(err, ErrCommand) {
			t.Fatalf("detached metadata Execute() = %#v, %v", result, err)
		}
	}
	if transactions.commits != 0 || transactions.rollbacks != 2 || !reflect.DeepEqual(transactions.state, commandState{}) {
		t.Fatalf("state=%#v commits=%d rollbacks=%d", transactions.state, transactions.commits, transactions.rollbacks)
	}
}

func newCommandCoordinatorHarness(t *testing.T, failure string) (*Coordinator[commandRepositories], *commandTransactions, *[]string) {
	t.Helper()
	order := &[]string{}
	transactions := &commandTransactions{order: order}
	coordinator, err := NewCoordinator(CoordinatorConfig[commandRepositories]{
		Transactions: transactions,
		Authorizer:   &commandAuthorizer{order: order},
		Elector:      &commandElector{order: order},
		Auditor:      &commandAuditor{order: order},
		Checkpoints:  &commandCheckpoints{order: order, failure: failure},
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	return coordinator, transactions, order
}

func validCommand() OrdinaryCommand[commandRepositories] {
	return OrdinaryCommand[commandRepositories]{
		Operation:    OrdinaryOperationCreateOrganisation,
		OperationKey: commandOperationID,
		Authentication: &tammyv1.AuthenticationContext{
			ActorUserId: commandActorID,
			SessionId:   commandSessionID,
		},
		Request: &tammyv1.CreateOrganisationRequest{CommandContext: &tammyv1.CommandContext{
			IdempotencyKey: commandOperationID,
			Authentication: &tammyv1.AuthenticationContext{ActorUserId: commandActorID, SessionId: commandSessionID},
		}, Abn: "51824753556"},
		NewResult: func() proto.Message {
			return &tammyv1.CreateOrganisationResponse{}
		},
		SaveSource: func(_ context.Context, repositories commandRepositories, _ proto.Message) error {
			repositories.state.source = append(repositories.state.source, "source")
			return nil
		},
		PostLedger: func(_ context.Context, repositories commandRepositories, _ proto.Message) error {
			repositories.state.ledger = append(repositories.state.ledger, "ledger")
			return nil
		},
		BuildResult: func(context.Context, commandRepositories, proto.Message) (CommandResult, error) {
			return validCommandOutcome(), nil
		},
	}
}

func validCommandOutcome() CommandResult {
	result := &tammyv1.CreateOrganisationResponse{
		Organisation: &tammyv1.Organisation{Id: commandWorkspaceID, Version: 1},
	}
	return validCommandOutcomeForResult(result)
}

func validCommandOutcomeForResult(result *tammyv1.CreateOrganisationResponse) CommandResult {
	return validCommandOutcomeForResultAndOperation(result, commandOperationID, []byte{1})
}

func validCommandOutcomeForResultAndOperation(result *tammyv1.CreateOrganisationResponse, operationID string, payload []byte) CommandResult {
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(result)
	if err != nil {
		panic(err)
	}
	digest := sha256.Sum256(encoded)
	idempotencyKey := operationID
	return CommandResult{Result: result, AuditEvent: &tammyv1.AuditEvent{
		WorkspaceId:    commandWorkspaceID,
		Actor:          &tammyv1.AuthenticationContext{ActorUserId: commandActorID, SessionId: commandSessionID},
		CommandType:    "tammy.v1.OrganisationService.CreateOrganisation",
		IdempotencyKey: &idempotencyKey,
		Result: &tammyv1.AuditResultMetadata{TypeName: "tammy.v1.CreateOrganisationResponse",
			DeterministicSha256: digest[:], OutcomeCode: "SUCCESS"},
	}, AuditPayload: append([]byte(nil), payload...), ResourceID: commandWorkspaceID}
}
