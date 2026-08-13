// Package sales owns customer opening-item projections.
package sales

import (
	"context"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

type OpeningReceivablePort interface {
	RecordOpeningReceivable(context.Context, string, *tammyv1.OpeningBalanceInput, time.Time) error
}
