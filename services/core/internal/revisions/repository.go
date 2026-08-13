// Package revisions owns the monotonic financial projection revision vector.
package revisions

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

var (
	ErrInvalidOperation = errors.New("revisions: canonical operation key is required")
	ErrNoDomain         = errors.New("revisions: at least one domain must be selected")
	ErrReplayConflict   = errors.New("revisions: operation key reused with different domains")
)

// Domains identifies projections changed by one financial unit of work.
type Domains struct {
	Ledger              bool
	Settlement          bool
	Banking             bool
	TaxSource           bool
	OrganisationProfile bool
	RuleBundle          bool
}

// Snapshot is the persisted monotonic revision vector.
type Snapshot struct {
	Financial           uint64
	Ledger              uint64
	Settlement          uint64
	Banking             uint64
	TaxSource           uint64
	OrganisationProfile uint64
	RuleBundle          uint64
	UpdatedAt           time.Time
}

// Repository is transaction-scoped. Construct exactly one for a unit of work;
// Bump rejects a second attempt so financial_revision changes exactly once.
type Repository struct {
	exec     func(context.Context, string, ...any) (sql.Result, error)
	queryRow func(context.Context, string, ...any) RowScanner
	clock    clock.Clock
}

func New(tx *sql.Tx, source clock.Clock) (*Repository, error) {
	if tx == nil || source == nil {
		return nil, errors.New("revisions: transaction and clock are required")
	}
	return NewWithExecutor(
		tx.ExecContext,
		func(ctx context.Context, query string, arguments ...any) RowScanner {
			return tx.QueryRowContext(ctx, query, arguments...)
		},
		source,
	)
}

// NewWithExecutor scopes revisions to another authenticated transaction
// capability without exposing its underlying database/sql transaction.
func NewWithExecutor(
	exec func(context.Context, string, ...any) (sql.Result, error),
	queryRow func(context.Context, string, ...any) RowScanner,
	source clock.Clock,
) (*Repository, error) {
	if exec == nil || queryRow == nil || source == nil {
		return nil, errors.New("revisions: transaction and clock are required")
	}
	return &Repository{exec: exec, queryRow: queryRow, clock: source}, nil
}

func (repository *Repository) Current(ctx context.Context) (Snapshot, error) {
	return scanSnapshot(repository.queryRow(ctx, `
		SELECT financial_revision, ledger_revision, settlement_revision,
		       banking_revision, tax_source_revision,
		       organisation_profile_revision, rule_bundle_revision, updated_at
		FROM financial_revisions WHERE id = 1`))
}

// Bump increments once for operationKey. Exact replays, including through a
// separately constructed Repository, return the retained vector without mutation.
func (repository *Repository) Bump(ctx context.Context, operationKey string, domains Domains) (Snapshot, bool, error) {
	if !domains.any() {
		return Snapshot{}, false, ErrNoDomain
	}
	if !ids.IsCanonicalV7(operationKey) {
		return Snapshot{}, false, ErrInvalidOperation
	}
	domainMask := domains.mask()
	retained, retainedMask, found, err := repository.claim(ctx, operationKey)
	if err != nil {
		return Snapshot{}, false, err
	}
	if found {
		if retainedMask != domainMask {
			return Snapshot{}, true, ErrReplayConflict
		}
		return retained, true, nil
	}
	if _, err := repository.exec(ctx, `SAVEPOINT financial_revision_bump`); err != nil {
		return Snapshot{}, false, fmt.Errorf("revisions: begin bump: %w", err)
	}
	fail := func(bumpErr error) (Snapshot, bool, error) {
		_, rollbackErr := repository.exec(context.WithoutCancel(ctx), `ROLLBACK TO financial_revision_bump`)
		_, releaseErr := repository.exec(context.WithoutCancel(ctx), `RELEASE financial_revision_bump`)
		return Snapshot{}, false, errors.Join(bumpErr, rollbackErr, releaseErr)
	}
	instant := repository.clock.Now().UTC().Format(time.RFC3339Nano)
	snapshot, err := scanSnapshot(repository.queryRow(ctx, `
		UPDATE financial_revisions
		SET financial_revision = financial_revision + 1,
		    ledger_revision = ledger_revision + ?,
		    settlement_revision = settlement_revision + ?,
		    banking_revision = banking_revision + ?,
		    tax_source_revision = tax_source_revision + ?,
		    organisation_profile_revision = organisation_profile_revision + ?,
		    rule_bundle_revision = rule_bundle_revision + ?,
		    updated_at = ?
		WHERE id = 1
		RETURNING financial_revision, ledger_revision, settlement_revision,
		          banking_revision, tax_source_revision,
		          organisation_profile_revision, rule_bundle_revision, updated_at`,
		boolInt(domains.Ledger), boolInt(domains.Settlement), boolInt(domains.Banking),
		boolInt(domains.TaxSource), boolInt(domains.OrganisationProfile),
		boolInt(domains.RuleBundle), instant))
	if err != nil {
		return fail(err)
	}
	if _, err := repository.exec(ctx, `
		INSERT INTO financial_revision_claims(
			operation_key, domain_mask, financial_revision, ledger_revision,
			settlement_revision, banking_revision, tax_source_revision,
			organisation_profile_revision, rule_bundle_revision, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		operationKey, domainMask, snapshot.Financial, snapshot.Ledger,
		snapshot.Settlement, snapshot.Banking, snapshot.TaxSource,
		snapshot.OrganisationProfile, snapshot.RuleBundle,
		snapshot.UpdatedAt.UTC().Format(time.RFC3339Nano)); err != nil {
		return fail(fmt.Errorf("revisions: retain unit-of-work claim: %w", err))
	}
	if _, err := repository.exec(ctx, `RELEASE financial_revision_bump`); err != nil {
		return fail(fmt.Errorf("revisions: commit bump: %w", err))
	}
	return snapshot, false, nil
}

func (domains Domains) any() bool {
	return domains.Ledger || domains.Settlement || domains.Banking || domains.TaxSource ||
		domains.OrganisationProfile || domains.RuleBundle
}

func (domains Domains) mask() uint8 {
	var mask uint8
	for index, selected := range [...]bool{
		domains.Ledger, domains.Settlement, domains.Banking, domains.TaxSource,
		domains.OrganisationProfile, domains.RuleBundle,
	} {
		if selected {
			mask |= 1 << index
		}
	}
	return mask
}

func (repository *Repository) claim(ctx context.Context, operationKey string) (Snapshot, uint8, bool, error) {
	var snapshot Snapshot
	var domainMask uint8
	var updatedAt string
	err := repository.queryRow(ctx, `
		SELECT domain_mask, financial_revision, ledger_revision, settlement_revision,
		       banking_revision, tax_source_revision,
		       organisation_profile_revision, rule_bundle_revision, updated_at
		FROM financial_revision_claims WHERE operation_key = ?`, operationKey).Scan(
		&domainMask, &snapshot.Financial, &snapshot.Ledger, &snapshot.Settlement,
		&snapshot.Banking, &snapshot.TaxSource, &snapshot.OrganisationProfile,
		&snapshot.RuleBundle, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Snapshot{}, 0, false, nil
	}
	if err != nil {
		return Snapshot{}, 0, false, fmt.Errorf("revisions: read unit-of-work claim: %w", err)
	}
	parsed, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return Snapshot{}, 0, false, fmt.Errorf("revisions: invalid retained claim time: %w", err)
	}
	snapshot.UpdatedAt = parsed
	return snapshot, domainMask, true, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

type RowScanner interface {
	Scan(...any) error
}

func scanSnapshot(row RowScanner) (Snapshot, error) {
	var snapshot Snapshot
	var updatedAt string
	if err := row.Scan(
		&snapshot.Financial,
		&snapshot.Ledger,
		&snapshot.Settlement,
		&snapshot.Banking,
		&snapshot.TaxSource,
		&snapshot.OrganisationProfile,
		&snapshot.RuleBundle,
		&updatedAt,
	); err != nil {
		return Snapshot{}, fmt.Errorf("revisions: read vector: %w", err)
	}
	parsed, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return Snapshot{}, fmt.Errorf("revisions: invalid persisted time: %w", err)
	}
	snapshot.UpdatedAt = parsed
	return snapshot, nil
}
