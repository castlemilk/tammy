package banking

import (
	"context"
	"errors"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
)

var ErrOpeningRepository = errors.New("banking: opening repository failure")

type OpeningRepository struct{ executor app.CommandSQLExecutor }

func NewOpeningRepository(executor app.CommandSQLExecutor) (*OpeningRepository, error) {
	if executor == nil {
		return nil, ErrOpeningRepository
	}
	return &OpeningRepository{executor: executor}, nil
}

func (repository *OpeningRepository) RecordOpeningFinancialAccount(ctx context.Context, conversionID string,
	input *tammyv1.OpeningBalanceInput, now time.Time,
) error {
	if repository == nil || repository.executor == nil || ctx == nil || !ids.IsCanonicalV7(conversionID) || input == nil ||
		!ids.IsCanonicalV7(input.ClientLineId) || now.IsZero() ||
		input.Kind != tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_FINANCIAL_ACCOUNT {
		return ErrOpeningRepository
	}
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(input)
	if err != nil || len(encoded) == 0 || len(encoded) > 1<<20 {
		return ErrOpeningRepository
	}
	if _, err := repository.executor.ExecContext(ctx, `INSERT INTO banking_opening_accounts(id, conversion_id, retained_input_proto, created_at) VALUES(?,?,?,?)`,
		input.ClientLineId, conversionID, encoded, now.UTC().Format(time.RFC3339Nano)); err != nil {
		return ErrOpeningRepository
	}
	return nil
}
