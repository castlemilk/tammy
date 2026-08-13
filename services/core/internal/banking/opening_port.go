// Package banking owns financial-account opening projections.
package banking

import (
	"context"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

type OpeningFinancialAccountPort interface {
	RecordOpeningFinancialAccount(context.Context, string, *tammyv1.OpeningBalanceInput, time.Time) error
}
