package baspreparation

import (
	"context"
	"errors"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/app"
)

const repositorySavepointCleanupTimeout = 5 * time.Second

// rollbackRepositorySavepoint cleans up one repository's partial writes while
// leaving the caller-owned transaction and its earlier work intact. Request
// cancellation must not prevent rollback, but cleanup remains time-bounded.
func rollbackRepositorySavepoint(ctx context.Context, executor app.CommandSQLExecutor, savepoint string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), repositorySavepointCleanupTimeout)
	defer cancel()

	_, rollbackErr := executor.ExecContext(cleanupCtx, "ROLLBACK TO SAVEPOINT "+savepoint)
	_, releaseErr := executor.ExecContext(cleanupCtx, "RELEASE SAVEPOINT "+savepoint)
	return errors.Join(rollbackErr, releaseErr)
}
