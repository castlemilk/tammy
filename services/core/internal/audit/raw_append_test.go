//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package audit

import (
	"context"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

// appendStoredEventForTest seeds audit fixtures without exposing a production
// constructor that can bypass mirror reconciliation.
func appendStoredEventForTest(ctx context.Context, executor Executor, event *tammyv1.AuditEvent, payload []byte) (StoredEvent, error) {
	return (&Appender{}).append(ctx, executor, event, payload, false, 0)
}
