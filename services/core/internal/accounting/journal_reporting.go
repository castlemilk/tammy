package accounting

import (
	"context"
	"crypto/sha256"
	"errors"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

// JournalReportingRecord is the full retained parent proof for a module-owned
// invoice, receipt or purchase source, including journals with no tax facts.
// Raw source/state metadata remains visible when its protobuf enum has no
// corresponding value. ContentHash commits all raw nullable SQL fields and
// the complete ordered protobuf journal, not just its monetary totals.
type JournalReportingRecord struct {
	Journal        *tammyv1.Journal
	SourceType     string
	SourceKind     int64
	SourceID       string
	SourceRevision uint64
	CreatedAt      string
	State          string
	ContentHash    []byte
	// PayloadBytes uses the accounting reporting retained-payload budget.
	// Consumers charge this once per distinct journal against their aggregate
	// capture budget. It does not measure total Go heap allocation.
	PayloadBytes int64
}

// GetForReporting reads complete journal provenance through the caller's
// executor, applying the same line and payload bounds as TaxFact capture.
// It does not interpret GST eligibility, mutate data or end the transaction.
// Retained legacy journal identities are accepted; organisation IDs must be
// canonical workspace identities. Missing and foreign parents return no proof.
func (repository *JournalRepository) GetForReporting(
	ctx context.Context, organisationID, journalID string,
) (*JournalReportingRecord, error) {
	return repository.GetForReportingWithPayloadBudget(ctx, organisationID, journalID,
		maxTaxFactReportingCaptureBytes)
}

// PreflightForReportingWithPayloadBudget returns the retained parent and line
// byte charge without loading their bodies. Aggregate readers can reserve all
// unique parents before hydrating any source. The caller must still obtain and
// verify the full proof through GetForReportingWithPayloadBudget, using the same
// transaction. Missing and foreign parents return no size.
func (repository *JournalRepository) PreflightForReportingWithPayloadBudget(
	ctx context.Context, organisationID, journalID string, maximumPayloadBytes int64,
) (int64, error) {
	if repository == nil || repository.executor == nil || ctx == nil ||
		!ids.IsCanonicalV7(organisationID) || journalID == "" || maximumPayloadBytes < 0 {
		return 0, ErrJournalRepository
	}
	if err := ctx.Err(); err != nil {
		return 0, errors.Join(ErrJournalRepository, err)
	}
	if maximumPayloadBytes > maxTaxFactReportingCaptureBytes {
		maximumPayloadBytes = maxTaxFactReportingCaptureBytes
	}
	remainingBytes := maximumPayloadBytes
	if err := repository.preflightTaxFactReportingJournal(ctx, journalID, organisationID, &remainingBytes); err != nil {
		var bound *TaxFactReportingBoundError
		if errors.As(err, &bound) && bound.Resource == "capture bytes" {
			err = &TaxFactReportingBoundError{Resource: bound.Resource, Limit: int(maximumPayloadBytes)}
		}
		return 0, errors.Join(ErrJournalRepository, err, ctx.Err())
	}
	if err := ctx.Err(); err != nil {
		return 0, errors.Join(ErrJournalRepository, err)
	}
	payloadBytes := maximumPayloadBytes - remainingBytes
	// An owned parent always has a positive header-row charge. Both private
	// size queries are organisation-scoped, so zero means no owned parent.
	if payloadBytes <= 0 {
		return 0, ErrJournalRepository
	}
	return payloadBytes, nil
}

// GetForReportingWithPayloadBudget checks the complete parent's retained bytes
// against the caller's remaining capture budget before loading journal data.
// The ordinary accounting limit still applies when the caller supplies more.
// PayloadBytes reports actual retained bytes, independently of the budget.
func (repository *JournalRepository) GetForReportingWithPayloadBudget(
	ctx context.Context, organisationID, journalID string, maximumPayloadBytes int64,
) (*JournalReportingRecord, error) {
	if repository == nil || repository.executor == nil || ctx == nil ||
		!ids.IsCanonicalV7(organisationID) || journalID == "" || maximumPayloadBytes < 0 {
		return nil, ErrJournalRepository
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrJournalRepository, err)
	}
	if maximumPayloadBytes > maxTaxFactReportingCaptureBytes {
		maximumPayloadBytes = maxTaxFactReportingCaptureBytes
	}
	remainingBytes := maximumPayloadBytes
	if err := repository.preflightTaxFactReportingJournal(ctx, journalID, organisationID, &remainingBytes); err != nil {
		return nil, err
	}
	snapshot, err := repository.loadTaxFactReportingJournal(ctx, journalID, organisationID)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrJournalRepository, err)
	}
	digest := sha256.New()
	hashTaxFactReportingFrame(digest, []byte("tammy/accounting/journal-reporting/v1"))
	hashTaxFactReportingFrame(digest, []byte(organisationID))
	hashTaxFactReportingFrame(digest, []byte(journalID))
	hashTaxFactReportingFrame(digest, snapshot.journalProto)
	hashTaxFactReportingFrame(digest, snapshot.raw)
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrJournalRepository, err)
	}
	return &JournalReportingRecord{
		Journal: snapshot.journal, SourceType: snapshot.sourceType, SourceKind: snapshot.sourceKind,
		SourceID: snapshot.sourceID, SourceRevision: snapshot.sourceRev,
		CreatedAt: snapshot.createdAt, State: snapshot.state, ContentHash: digest.Sum(nil),
		PayloadBytes: maximumPayloadBytes - remainingBytes,
	}, nil
}
