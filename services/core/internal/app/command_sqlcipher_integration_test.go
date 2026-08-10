//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/authorisation"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/idempotency"
	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/proto"
)

type sqlCommandRepositories struct{ executor CommandSQLExecutor }

type sqlCommandTransaction struct {
	*sqlcipher.Transaction
	repositories sqlCommandRepositories
	id           string
}

func (transaction *sqlCommandTransaction) TransactionID() string { return transaction.id }
func (transaction *sqlCommandTransaction) Repositories() sqlCommandRepositories {
	return transaction.repositories
}
func (transaction *sqlCommandTransaction) IdempotencyExecutor() idempotency.Executor {
	return transaction.Transaction
}
func (transaction *sqlCommandTransaction) AuditExecutor() CommandSQLExecutor {
	return transaction.Transaction
}

type sqlCommandStarter struct {
	database *sqlcipher.Database
	sequence int
}

func (starter *sqlCommandStarter) Begin(ctx context.Context) (OwnedCommandTransaction[sqlCommandRepositories], error) {
	raw, err := starter.database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, err
	}
	starter.sequence++
	return &sqlCommandTransaction{Transaction: raw, repositories: sqlCommandRepositories{executor: raw}, id: "sql-tx"}, nil
}

type sqlCommandAuthorizer struct{}

func (sqlCommandAuthorizer) Authorize(
	ctx context.Context,
	scope CommandScope[sqlCommandRepositories],
	authentication *tammyv1.AuthenticationContext,
	_ authorisation.Action,
) (AuthorizedActor, error) {
	if authentication == nil {
		return AuthorizedActor{}, ErrCommand
	}
	rows, err := scope.Repositories().executor.QueryContext(ctx,
		`SELECT count(*) FROM command_test_actors WHERE workspace_id=? AND actor_id=? AND session_id=?`,
		commandWorkspaceID, authentication.ActorUserId, authentication.SessionId)
	if err != nil {
		return AuthorizedActor{}, ErrCommand
	}
	defer rows.Close()
	var authenticated int
	if !rows.Next() || rows.Scan(&authenticated) != nil || rows.Next() || rows.Err() != nil || authenticated != 1 {
		return AuthorizedActor{}, ErrCommand
	}
	return AuthorizedActor{WorkspaceID: commandWorkspaceID, UserID: authentication.ActorUserId, SessionID: authentication.SessionId}, nil
}

type sqlCommandAuditor struct{}

func (sqlCommandAuditor) Append(ctx context.Context, executor CommandSQLExecutor, _ *tammyv1.AuditEvent, payload []byte) error {
	digest := sha256.Sum256(payload)
	_, err := executor.ExecContext(ctx, `INSERT INTO command_test_audit(payload_hash) VALUES(?)`, digest[:])
	return err
}

type sqlCommandCheckpoint struct{ failure string }

func (checkpoint *sqlCommandCheckpoint) Check(stage string) error {
	if checkpoint.failure == stage {
		return errCommandInjected
	}
	return nil
}

func TestCoordinatorUsesOneRealSQLCipherTransactionForWorkAuditResultAndReplay(t *testing.T) {
	database := newCommandSQLCipherDatabase(t)
	transactions, err := NewCommandTransactions(commandWorkspaceID, &sqlCommandStarter{database: database})
	if err != nil {
		t.Fatalf("NewCommandTransactions() error = %v", err)
	}
	observer, err := idempotency.NewObserver(database)
	if err != nil {
		t.Fatal(err)
	}
	elector, err := idempotency.NewElector(idempotency.Config{
		Clock: clock.NewFixed(time.Date(2026, 8, 10, 2, 0, 0, 0, time.UTC)), Observe: observer,
	})
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := &sqlCommandCheckpoint{}
	coordinator, err := NewCoordinator(CoordinatorConfig[sqlCommandRepositories]{
		Transactions: transactions, Authorizer: sqlCommandAuthorizer{}, Elector: elector,
		Auditor: sqlCommandAuditor{}, Checkpoints: checkpoint,
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}

	command := sqlOrdinaryCommand(commandOperationID, "51824753556")
	first, err := coordinator.Execute(context.Background(), command)
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	replayed, err := coordinator.Execute(context.Background(), command)
	if err != nil || !proto.Equal(first, replayed) {
		t.Fatalf("replay Execute() = %#v, %v", replayed, err)
	}
	assertCommandSQLCounts(t, database, 1, 1, 1, 1)

	changed := sqlOrdinaryCommand(commandOperationID, "53004085616")
	if result, err := coordinator.Execute(context.Background(), changed); result != nil || !errors.Is(err, idempotency.ErrConflict) {
		t.Fatalf("changed replay = %#v, %v; want conflict", result, err)
	}
	assertCommandSQLCounts(t, database, 1, 1, 1, 1)

	for index, failure := range []string{
		CheckpointAfterSourceSave,
		CheckpointAfterLedgerPost,
		CheckpointAfterAuditAppend,
		CheckpointAfterResultSerialization,
	} {
		checkpoint.failure = failure
		operationID := []string{
			"018f0000-0000-7000-8000-000000000011",
			"018f0000-0000-7000-8000-000000000012",
			"018f0000-0000-7000-8000-000000000013",
			"018f0000-0000-7000-8000-000000000014",
		}[index]
		if result, err := coordinator.Execute(context.Background(), sqlOrdinaryCommand(operationID, "51824753556")); result != nil || !errors.Is(err, errCommandInjected) {
			t.Fatalf("failure %s = %#v, %v", failure, result, err)
		}
		assertCommandSQLCounts(t, database, 1, 1, 1, 1)
	}
}

func newCommandSQLCipherDatabase(t *testing.T) *sqlcipher.Database {
	t.Helper()
	path := filepath.Join(t.TempDir(), "command.db")
	key := make([]byte, sqlcipher.KeySize)
	for index := range key {
		key[index] = byte(index + 11)
	}
	if _, err := sqlcipher.MigrateWorkspace(context.Background(), path, key, 4); err != nil {
		t.Fatalf("MigrateWorkspace() error = %v", err)
	}
	database, err := sqlcipher.Open(context.Background(), path, key)
	clear(key)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	for _, statement := range []string{
		`CREATE TABLE command_test_actors(workspace_id TEXT,actor_id TEXT,session_id TEXT)`,
		`CREATE TABLE command_test_source(value TEXT)`,
		`CREATE TABLE command_test_ledger(value TEXT)`,
		`CREATE TABLE command_test_audit(payload_hash BLOB)`,
		`INSERT INTO command_test_actors(workspace_id,actor_id,session_id) VALUES('018f0000-0000-7000-8000-000000000001','018f0000-0000-7000-8000-000000000002','018f0000-0000-7000-8000-000000000003')`,
	} {
		if _, err := database.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("fixture SQL error = %v", err)
		}
	}
	return database
}

func sqlOrdinaryCommand(operationID, abn string) OrdinaryCommand[sqlCommandRepositories] {
	return OrdinaryCommand[sqlCommandRepositories]{
		Operation:      OrdinaryOperationCreateOrganisation,
		OperationKey:   operationID,
		Authentication: &tammyv1.AuthenticationContext{ActorUserId: commandActorID, SessionId: commandSessionID},
		Request: &tammyv1.CreateOrganisationRequest{CommandContext: &tammyv1.CommandContext{
			IdempotencyKey: operationID,
			Authentication: &tammyv1.AuthenticationContext{ActorUserId: commandActorID, SessionId: commandSessionID},
		}, Abn: abn},
		NewResult: func() proto.Message {
			return &tammyv1.CreateOrganisationResponse{}
		},
		SaveSource: func(ctx context.Context, repositories sqlCommandRepositories, request proto.Message) error {
			owned := request.(*tammyv1.CreateOrganisationRequest)
			_, err := repositories.executor.ExecContext(ctx, `INSERT INTO command_test_source(value) VALUES(?)`, owned.Abn)
			return err
		},
		PostLedger: func(ctx context.Context, repositories sqlCommandRepositories, request proto.Message) error {
			owned := request.(*tammyv1.CreateOrganisationRequest)
			_, err := repositories.executor.ExecContext(ctx, `INSERT INTO command_test_ledger(value) VALUES(?)`, owned.Abn)
			return err
		},
		BuildResult: func(_ context.Context, _ sqlCommandRepositories, request proto.Message) (CommandResult, error) {
			owned := request.(*tammyv1.CreateOrganisationRequest)
			result := &tammyv1.CreateOrganisationResponse{
				Organisation: &tammyv1.Organisation{Id: commandWorkspaceID, Version: 1, Abn: owned.Abn},
			}
			return validCommandOutcomeForResultAndOperation(result, operationID, []byte(owned.Abn)), nil
		},
	}
}

func assertCommandSQLCounts(t *testing.T, database *sqlcipher.Database, source, ledger, audit, retained int) {
	t.Helper()
	for table, want := range map[string]int{
		"command_test_source":    source,
		"command_test_ledger":    ledger,
		"command_test_audit":     audit,
		"command_idempotency_v1": retained,
	} {
		var count int
		if err := database.QueryRowContext(context.Background(), `SELECT count(*) FROM `+table).Scan(&count); err != nil || count != want {
			t.Fatalf("%s count = %d, %v; want %d", table, count, err, want)
		}
	}
}
