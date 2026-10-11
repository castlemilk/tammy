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
	if repository == nil || repository.executor == nil || ctx == nil ||
		!ids.IsCanonicalV7(organisationID) || journalID == "" {
		return nil, ErrJournalRepository
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrJournalRepository, err)
	}
	remainingBytes := int64(maxTaxFactReportingCaptureBytes)
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
		PayloadBytes: int64(maxTaxFactReportingCaptureBytes) - remainingBytes,
	}, nil
}
