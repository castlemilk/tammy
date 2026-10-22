package organisations

import (
	"context"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

// ReadPort is the narrow organisation projection exposed to dependent modules.
// It intentionally carries no raw SQL or write capability.
type ReadPort interface {
	GetOrganisation(context.Context, string) (*tammyv1.Organisation, error)
}

func (repository *Repository) GetOrganisation(ctx context.Context, organisationID string) (*tammyv1.Organisation, error) {
	return repository.Get(ctx, organisationID)
}
