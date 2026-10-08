package purchases

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"errors"
	"math"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

const (
	lowValueRuleID            = "019a4210-0000-7000-8000-000000000075"
	lowValueRuleVersion       = "au-gst-tax-invoice-low-value-v1"
	lowValueRuleHash          = "11cc08f62cb53e5e896e0af18ad2056f1bf0a572a4cc2b98c342de6ba7398157"
	lowValueRuleEffectiveFrom = "2025-11-01"
	lowValueRuleType          = "AU_GST_PURCHASE_TAX_INVOICE_EXCEPTION"
)

//go:embed rules/au_gst_low_value_v1.json
var lowValueRuleBytes []byte

func embeddedLowValueRule() (*ReviewedExceptionRule, error) {
	digest := sha256.Sum256(lowValueRuleBytes)
	if hex.EncodeToString(digest[:]) != lowValueRuleHash {
		return nil, ErrRepository
	}
	return &ReviewedExceptionRule{Kind: tammyv1.PurchaseTaxInvoiceExceptionKind_PURCHASE_TAX_INVOICE_EXCEPTION_KIND_LOW_VALUE_ACQUISITION, Source: &tammyv1.PurchaseRetainedSourceRef{SourceId: lowValueRuleID, SourceVersion: 1, SourceHash: append([]byte(nil), digest[:]...)}}, nil
}

// RetainEvidenceRules installs the core-owned immutable rule without reserving
// an accounting revision. Composition calls this in its activation transaction.
// An existing row must match every byte and scalar; no client defines a rule.
func (r *Repository) RetainEvidenceRules(ctx context.Context) error {
	if r == nil || r.executor == nil || r.now == nil || ctx == nil {
		return ErrRepository
	}
	if _, err := embeddedLowValueRule(); err != nil {
		return err
	}
	_, err := r.LowValueExceptionRule(ctx)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	now := r.now().UTC()
	if now.IsZero() {
		return ErrRepository
	}
	_, err = r.executor.ExecContext(ctx, `INSERT INTO rule_bundles(id,organisation_id,bundle_type,version,semantic_sha256,rules_proto,effective_from,effective_to,retained_at) VALUES(?,NULL,?,?,?,?,?,NULL,?)`, lowValueRuleID, lowValueRuleType, lowValueRuleVersion, lowValueRuleHash, lowValueRuleBytes, lowValueRuleEffectiveFrom, now.Format(time.RFC3339Nano))
	if err != nil {
		return errors.Join(ErrRepository, err)
	}
	_, err = r.LowValueExceptionRule(ctx)
	return err
}

// LowValueExceptionRule is a read-only descriptor of an installed rule. It is
// provenance, not claim authority; ResolveEvidenceRule verifies source facts.
func (r *Repository) LowValueExceptionRule(ctx context.Context) (*ReviewedExceptionRule, error) {
	if r == nil || r.executor == nil || ctx == nil {
		return nil, ErrRepository
	}
	rule, err := embeddedLowValueRule()
	if err != nil {
		return nil, err
	}
	rows, err := r.executor.QueryContext(ctx, `SELECT organisation_id,bundle_type,version,semantic_sha256,rules_proto,effective_from,effective_to,retained_at FROM rule_bundles WHERE id=?`, lowValueRuleID)
	if err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	defer rows.Close()
	if !rows.Next() {
		if rows.Err() != nil {
			return nil, ErrRepository
		}
		return nil, sql.ErrNoRows
	}
	var org, end sql.NullString
	var kind, version, hash, effective, stamp string
	var body []byte
	if rows.Scan(&org, &kind, &version, &hash, &body, &effective, &end, &stamp) != nil || rows.Next() || rows.Err() != nil || org.Valid || end.Valid || kind != lowValueRuleType || version != lowValueRuleVersion || hash != lowValueRuleHash || effective != lowValueRuleEffectiveFrom || !bytes.Equal(body, lowValueRuleBytes) {
		return nil, ErrRepository
	}
	instant, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil || instant.IsZero() || instant.UTC().Format(time.RFC3339Nano) != stamp {
		return nil, ErrRepository
	}
	if err := rows.Close(); err != nil {
		return nil, ErrRepository
	}
	return rule, nil
}

// ResolveEvidenceRule reloads the complete acquisition and each support record
// from the owned encrypted transaction. The narrow implementation uses whole
// transaction net value; it cannot derive taxable-only value from allocations,
// accept a split transaction, or implement a specific Commissioner waiver.
func (r *Repository) ResolveEvidenceRule(ctx context.Context, org, acquisitionID string, review *tammyv1.PurchaseEvidenceInput) (*ReviewedExceptionRule, error) {
	if review == nil || review.Classification != tammyv1.PurchaseEvidenceClassification_PURCHASE_EVIDENCE_CLASSIFICATION_SUPPORTED_EXCEPTION {
		return nil, ValidatePurchaseEvidence(review, nil)
	}
	rule, err := r.LowValueExceptionRule(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUnsupportedException
	}
	if err != nil {
		return nil, err
	}
	if err := ValidatePurchaseEvidence(review, rule); err != nil {
		return nil, err
	}
	doc, err := r.document(ctx, org, acquisitionID)
	if err != nil {
		return nil, err
	}
	c := doc.Candidate
	if doc.Status != tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED || doc.ReviewedAt == nil || doc.ReviewedAt.CheckValid() != nil || c == nil || !validPurchaseMoney(c.Subtotal) || !validPurchaseMoney(c.Gst) || !validPurchaseMoney(c.Total) || c.Subtotal.MinorUnits > 7500 || purchaseDateString(c.DocumentDate) < lowValueRuleEffectiveFrom {
		return nil, ErrUnsupportedException
	}
	amount := c.Subtotal.MinorUnits
	if !addPurchaseAmount(&amount, c.Gst.MinorUnits) || amount != c.Total.MinorUnits {
		return nil, ErrUnsupportedException
	}
	seen := make(map[string]bool, len(review.ExceptionReview.SupportingRecords))
	for _, ref := range review.ExceptionReview.SupportingRecords {
		if ref.SourceVersion > math.MaxInt64 || seen[ref.SourceId] {
			return nil, ErrUnsupportedException
		}
		seen[ref.SourceId] = true
		support, err := r.document(ctx, org, ref.SourceId)
		if err != nil {
			return nil, err
		}
		if support.Status != tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED || support.ReviewedAt == nil || support.ReviewedAt.CheckValid() != nil || support.Version != ref.SourceVersion || !bytes.Equal(support.Sha256, ref.SourceHash) {
			return nil, ErrUnsupportedException
		}
	}
	if !seen[acquisitionID] || review.Source == nil || !seen[review.Source.SourceId] {
		return nil, ErrUnsupportedException
	}
	if err := r.verifyEvidenceSource(ctx, org, review); err != nil {
		return nil, err
	}
	return &ReviewedExceptionRule{Kind: rule.Kind, Source: proto.Clone(rule.Source).(*tammyv1.PurchaseRetainedSourceRef)}, nil
}

func (r *Repository) evidenceReadiness(ctx context.Context, org, documentID string, review *tammyv1.PurchaseEvidenceInput, eventDate, asOf *tammyv1.CivilDate) (EvidenceReadiness, error) {
	rule, err := r.ResolveEvidenceRule(ctx, org, documentID, review)
	if err != nil {
		return 0, err
	}
	return EvidenceReadinessFor(review, eventDate, asOf, rule)
}

func (r *Repository) evidenceOutcomes(ctx context.Context, p *tammyv1.PurchasePosting, review *tammyv1.PurchaseEvidenceInput, asOf time.Time) (*tammyv1.PurchaseAccountingReviewOutcomes, error) {
	rule, err := r.ResolveEvidenceRule(ctx, p.OrganisationId, p.DocumentId, review)
	if err != nil {
		return nil, err
	}
	return purchaseEvidenceOutcomes(review, p.InvoiceDate, asOf, rule)
}
