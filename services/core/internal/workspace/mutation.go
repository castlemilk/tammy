package workspace

import (
	"context"
	"database/sql"
)

// MutationExecutor is the common database contract used by workspace-owned
// units of work and their dependent domain services.
type MutationExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}
