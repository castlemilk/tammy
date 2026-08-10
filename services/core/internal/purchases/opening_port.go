// Package purchases owns supplier opening-item projections.
package purchases

import (
	"context"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

type OpeningPayablePort interface {
	RecordOpeningPayable(context.Context, string, *tammyv1.OpeningBalanceInput, time.Time) error
}
