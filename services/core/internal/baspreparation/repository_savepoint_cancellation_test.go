//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/app"
)

func TestValidationRepositoryCancellationRollsBackOnlyItsSavepoint(t *testing.T) {
	workspace, org, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	fixture := seedValidationRepositoryFixture(t, tx, org)
	wantLegalName := writeCallerWorkBeforeRepositoryAppend(t, tx, org, "validation")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	executor := &cancelAfterRepositoryParentInsertExecutor{
		CommandSQLExecutor: tx,
		parentTable:        "bas_validations_v1",
		cancel:             cancel,
	}
	repository := newValidationRepositoryForTest(t, executor)
	ref, err := repository.Append(ctx, org, fixture.validation, fixture.ownerRef)
	if ref != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation during validation findings should abort append: ref=%v err=%v", ref, err)
	}
	if !executor.parentInserted {
		t.Fatal("test did not cancel after the validation row was inserted")
	}
	assertBoundedUncancelledSavepointCleanup(t, executor)
	assertBASRepositoryCount(t, tx, "bas_validations_v1", 0)
	assertBASRepositoryCount(t, tx, "bas_validation_findings_v1", 0)
	assertCallerWorkCanCommit(t, workspace.Database, tx, org, wantLegalName, "validation")
}

func TestCalculationRepositoryCancellationRollsBackOnlyItsSavepoint(t *testing.T) {
	workspace, org, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	value, inputsRef := seedCalculationRepositoryFixture(t, tx, org)
	wantLegalName := writeCallerWorkBeforeRepositoryAppend(t, tx, org, "calculation")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	executor := &cancelAfterRepositoryParentInsertExecutor{
		CommandSQLExecutor: tx,
		parentTable:        "bas_calculations_v1",
		cancel:             cancel,
	}
	repository := newCalculationRepositoryForTest(t, executor)
	ref, err := repository.Append(ctx, value, inputsRef)
	if ref != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation during calculation coverage should abort append: ref=%v err=%v", ref, err)
	}
	if !executor.parentInserted {
		t.Fatal("test did not cancel after the calculation row was inserted")
	}
	assertBoundedUncancelledSavepointCleanup(t, executor)
	assertBASRepositoryCount(t, tx, "bas_calculations_v1", 0)
	assertBASRepositoryCount(t, tx, "bas_calculation_coverage_v1", 0)
	assertCallerWorkCanCommit(t, workspace.Database, tx, org, wantLegalName, "calculation")
}

func TestValidationRepositoryReportsSavepointCleanupErrors(t *testing.T) {
	workspace, org, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	fixture := seedValidationRepositoryFixture(t, tx, org)
	primaryErr := errors.New("finding insert failed")
	cleanupErr := errors.New("savepoint rollback failed")
	executor := &failValidationFindingAndRollbackExecutor{
		CommandSQLExecutor: tx,
		primaryErr:         primaryErr,
		cleanupErr:         cleanupErr,
	}
	ref, err := newValidationRepositoryForTest(t, executor).Append(context.Background(), org, fixture.validation, fixture.ownerRef)
	if ref != nil || !errors.Is(err, primaryErr) || !errors.Is(err, cleanupErr) {
		t.Fatalf("append failure should preserve primary and cleanup errors: ref=%v err=%v", ref, err)
	}
	if !executor.parentInserted || !executor.rollbackAttempted {
		t.Fatalf("test did not exercise partial-write cleanup: parent=%t rollback=%t", executor.parentInserted, executor.rollbackAttempted)
	}
}

func writeCallerWorkBeforeRepositoryAppend(t *testing.T, tx interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, organisationID, label string) string {
	t.Helper()
	want := "before " + label + " savepoint"
	if _, err := tx.ExecContext(context.Background(), "UPDATE organisations SET legal_name=? WHERE id=?", want, organisationID); err != nil {
		t.Fatalf("write caller-owned work before %s append: %v", label, err)
	}
	return want
}

func assertCallerWorkCanCommit(t *testing.T, database interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, tx interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	Commit() error
}, organisationID, want, label string) {
	t.Helper()
	var got string
	rows, err := tx.QueryContext(context.Background(), "SELECT legal_name FROM organisations WHERE id=?", organisationID)
	if err != nil {
		t.Fatalf("read caller-owned work after %s append: %v", label, err)
	}
	if !rows.Next() {
		_ = rows.Close()
		t.Fatalf("caller-owned organisation disappeared after %s append: %v", label, rows.Err())
	}
	if err := rows.Scan(&got); err != nil {
		_ = rows.Close()
		t.Fatalf("scan caller-owned work after %s append: %v", label, err)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatalf("finish reading caller-owned work after %s append: %v", label, err)
	}
	if got != want {
		t.Fatalf("caller-owned work after %s append: legal_name=%q want=%q", label, got, want)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit caller transaction after %s append failure: %v", label, err)
	}
	if err := database.QueryRowContext(context.Background(), "SELECT legal_name FROM organisations WHERE id=?", organisationID).Scan(&got); err != nil || got != want {
		t.Fatalf("committed caller-owned work after %s append failure: legal_name=%q err=%v", label, got, err)
	}
}

type cancelAfterRepositoryParentInsertExecutor struct {
	app.CommandSQLExecutor
	parentTable    string
	cancel         context.CancelFunc
	parentInserted bool
	cleanupCalls   int
	liveCleanup    int
	boundedCleanup int
}

func (executor *cancelAfterRepositoryParentInsertExecutor) ExecContext(ctx context.Context, query string, arguments ...any) (sql.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result, err := executor.CommandSQLExecutor.ExecContext(ctx, query, arguments...)
	if err == nil && strings.Contains(strings.ToLower(query), "insert into "+executor.parentTable) {
		executor.parentInserted = true
		executor.cancel()
	}
	if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(query)), "ROLLBACK TO SAVEPOINT") ||
		strings.HasPrefix(strings.ToUpper(strings.TrimSpace(query)), "RELEASE SAVEPOINT") {
		executor.cleanupCalls++
		if ctx.Err() == nil {
			executor.liveCleanup++
		}
		if _, ok := ctx.Deadline(); ok {
			executor.boundedCleanup++
		}
	}
	return result, err
}

func (executor *cancelAfterRepositoryParentInsertExecutor) QueryContext(ctx context.Context, query string, arguments ...any) (*sql.Rows, error) {
	return executor.CommandSQLExecutor.QueryContext(ctx, query, arguments...)
}

func assertBoundedUncancelledSavepointCleanup(t *testing.T, executor *cancelAfterRepositoryParentInsertExecutor) {
	t.Helper()
	if executor.cleanupCalls != 2 || executor.liveCleanup != 2 || executor.boundedCleanup != 2 {
		t.Fatalf("savepoint cleanup should use a bounded live context for rollback and release: calls=%d live=%d bounded=%d",
			executor.cleanupCalls, executor.liveCleanup, executor.boundedCleanup)
	}
}

type failValidationFindingAndRollbackExecutor struct {
	app.CommandSQLExecutor
	primaryErr        error
	cleanupErr        error
	parentInserted    bool
	rollbackAttempted bool
}

func (executor *failValidationFindingAndRollbackExecutor) ExecContext(ctx context.Context, query string, arguments ...any) (sql.Result, error) {
	upper := strings.ToUpper(strings.TrimSpace(query))
	if strings.HasPrefix(upper, "ROLLBACK TO SAVEPOINT") {
		executor.rollbackAttempted = true
		return nil, executor.cleanupErr
	}
	if strings.Contains(upper, "INSERT INTO BAS_VALIDATION_FINDINGS_V1") {
		return nil, executor.primaryErr
	}
	result, err := executor.CommandSQLExecutor.ExecContext(ctx, query, arguments...)
	if err == nil && strings.Contains(upper, "INSERT INTO BAS_VALIDATIONS_V1") {
		executor.parentInserted = true
	}
	return result, err
}

func (executor *failValidationFindingAndRollbackExecutor) QueryContext(ctx context.Context, query string, arguments ...any) (*sql.Rows, error) {
	return executor.CommandSQLExecutor.QueryContext(ctx, query, arguments...)
}

var _ app.CommandSQLExecutor = (*cancelAfterRepositoryParentInsertExecutor)(nil)
var _ app.CommandSQLExecutor = (*failValidationFindingAndRollbackExecutor)(nil)
