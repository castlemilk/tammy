package purchases

import (
	"context"
	"errors"
	"strings"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

func (r *Repository) reportingJournal(ctx context.Context, org, id string) (*tammyv1.Journal, error) {
	if !r.reportingRootOwners {
		repository, _ := accounting.NewJournalRepository(r.executor)
		return repository.Get(ctx, id)
	}
	proof, err := r.reportingJournalProof(ctx, org, id)
	if err != nil {
		return nil, err
	}
	return proof.Journal, nil
}

func (r *Repository) reportingJournalProof(ctx context.Context, org, id string) (*accounting.JournalReportingRecord, error) {
	repository, err := accounting.NewJournalRepository(r.executor)
	if err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	proof, err := repository.GetForReporting(ctx, org, id)
	if err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	if proof == nil || proof.Journal == nil {
		return nil, ErrRepository
	}
	return proof, nil
}

func (r *Repository) reportingAccount(ctx context.Context, org, id string) (*tammyv1.Account, error) {
	repository, _ := accounting.NewAccountRepository(r.executor)
	if !r.reportingRootOwners {
		return repository.Get(ctx, id)
	}
	account, err := repository.GetForReporting(ctx, org, id)
	if err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	return account, nil
}

func purchaseReportingControlIDReplayQuery(org string) postingReportingQuery {
	return postingReportingQuery{table: "control account IDs replay", alias: "a", columns: strings.Fields("id"),
		from: "accounts AS a", where: "a.organisation_id=? AND a.code IN ('2000','1150','1160','1170')", args: []any{org}, orderBy: "a.code,a.id",
		ownerGuard: purchaseReportingAccountReferenceGuard(org, nil, true)}
}
