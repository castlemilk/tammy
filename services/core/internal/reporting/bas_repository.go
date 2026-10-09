package reporting

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"buf.build/go/protovalidate"
	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	ErrRepository  = errors.New("reporting: repository failure")
	ErrNotFound    = errors.New("reporting: BAS draft not found")
	ErrConflict    = errors.New("reporting: BAS draft conflict")
	ErrGSTBasis    = errors.New("reporting: GST basis history requires review")
	ErrSourceBound = errors.New("reporting: complete workpaper exceeds supported source or response capacity")
)

type PurchaseBASPort interface {
	CaptureBAS(context.Context, string, *tammyv1.PurchaseClaimPeriod, *tammyv1.CivilDate, time.Time) (*purchases.BASSnapshot, error)
}
type Repository struct {
	executor     app.CommandSQLExecutor
	purchasePort PurchaseBASPort
	now          func() time.Time
}

func NewRepository(executor app.CommandSQLExecutor, ports ...PurchaseBASPort) (*Repository, error) {
	return NewRepositoryWithClock(executor, time.Now, ports...)
}
func NewRepositoryWithClock(executor app.CommandSQLExecutor, now func() time.Time, ports ...PurchaseBASPort) (*Repository, error) {
	if executor == nil || now == nil || len(ports) > 1 {
		return nil, ErrRepository
	}
	var port PurchaseBASPort
	if len(ports) == 1 {
		port = ports[0]
		if port == nil {
			return nil, ErrRepository
		}
	} else {
		port, _ = purchases.NewRepository(executor, now, func() (string, error) { return "", ErrRepository })
	}
	return &Repository{executor: executor, purchasePort: port, now: now}, nil
}

func (repository *Repository) CreateBASDraft(
	ctx context.Context,
	operationKey, workpaperID, organisationID string,
	periodStart, periodEnd *tammyv1.CivilDate,
	now time.Time,
) (*tammyv1.BasWorkpaper, error) {
	if repository == nil || repository.executor == nil || ctx == nil || !ids.IsCanonicalV7(operationKey) ||
		!ids.IsCanonicalV7(workpaperID) || !ids.IsCanonicalV7(organisationID) || !validDate(periodStart) || !validDate(periodEnd) || now.IsZero() {
		return nil, ErrRepository
	}
	start, end := civilDateString(periodStart), civilDateString(periodEnd)
	if start > end {
		return nil, ErrRepository
	}
	if existing, err := repository.getByOperation(ctx, operationKey); err == nil {
		if existing.OrganisationId == organisationID && civilDateString(existing.PeriodStart) == start && civilDateString(existing.PeriodEnd) == end {
			return existing, nil
		}
		return nil, ErrConflict
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	captured, err := repository.collectBASSources(ctx, organisationID, start, end, now)
	if err != nil {
		return nil, err
	}
	var sales, gst, credits int64
	for _, source := range captured.sales {
		if !addBASAmount(&sales, source.Gross.MinorUnits) || !addBASAmount(&gst, source.Gst.MinorUnits) {
			return nil, ErrRepository
		}
	}
	purchases := captured.purchases
	if captured.basis == tammyv1.GstBasis_GST_BASIS_CASH {
		purchases = nil
	}
	for _, source := range purchases {
		if !addBASAmount(&credits, source.GstCredit.MinorUnits) {
			return nil, ErrRepository
		}
	}
	if captured.ownedPurchases != nil {
		for _, source := range captured.ownedPurchases.Sources {
			if !addBASAmount(&credits, source.ClaimedGst.MinorUnits) {
				return nil, ErrRepository
			}
		}
	}
	workpaper := &tammyv1.BasWorkpaper{Id: workpaperID, OrganisationId: organisationID, Version: 1, PeriodStart: periodStart, PeriodEnd: periodEnd, Status: tammyv1.BasWorkpaperStatus_BAS_WORKPAPER_STATUS_DRAFT_NOT_LODGED, SalesG1: aud(sales), GstOnSales_1A: aud(gst), GstCredits_1B: aud(credits), NetGstPayable: aud(gst - credits), Sources: purchases, CreatedAt: timestamppb.New(now), GstBasis: captured.basis, SalesSources: captured.sales, SourceFingerprint: captured.fingerprint, ReviewWarnings: captured.warnings, ExcludedCashPurchaseCount: captured.excludedCashPurchases}
	if captured.ownedPurchases != nil {
		format := tammyv1.BasPurchaseSourceFormat_BAS_PURCHASE_SOURCE_FORMAT_RECONCILED_V1
		workpaper.PurchaseSourceFormat = &format
		workpaper.PurchaseSources = captured.ownedPurchases.Sources
		workpaper.PurchaseReviews = captured.ownedPurchases.Reviews
		workpaper.LegacyPurchaseSupport = captured.ownedPurchases.LegacySupport
		workpaper.LegacyCandidateGst = captured.ownedPurchases.LegacyCandidateGST
	}
	if !basResponseFits(workpaper) {
		return nil, ErrSourceBound
	}
	if protovalidate.Validate(workpaper) != nil {
		return nil, ErrRepository
	}
	warningsJSON, err := json.Marshal(captured.warnings)
	if err != nil {
		return nil, ErrRepository
	}
	createdAt := now.UTC().Format(time.RFC3339Nano)
	if _, err := repository.executor.ExecContext(ctx, `
		INSERT INTO bas_workpapers(
			id, operation_key, organisation_id, version, period_start, period_end, status,
			sales_g1_minor, gst_on_sales_1a_minor, gst_credits_1b_minor, net_gst_payable_minor, created_at,gst_basis,source_fingerprint,review_warnings_json,excluded_cash_purchase_count
		) VALUES (?, ?, ?, 1, ?, ?, 'DRAFT_NOT_LODGED', ?, ?, ?, ?, ?,?,?,?,?)`,
		workpaperID, operationKey, organisationID, start, end, sales, gst, credits, gst-credits, createdAt, int32(captured.basis), captured.fingerprint, string(warningsJSON), captured.excludedCashPurchases); err != nil {
		return nil, ErrRepository
	}
	for index, source := range purchases {
		if _, err := repository.executor.ExecContext(ctx, `
			INSERT INTO bas_workpaper_sources(
				workpaper_id, sequence, document_id, document_version, supplier_name,
				invoice_number, document_date, gross_minor, gst_credit_minor
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, workpaperID, index+1, source.DocumentId,
			captured.purchaseVersions[index], source.SupplierName, source.InvoiceNumber, civilDateString(source.DocumentDate),
			source.Gross.MinorUnits, source.GstCredit.MinorUnits); err != nil {
			return nil, ErrRepository
		}
	}
	for index, source := range captured.sales {
		if _, err := repository.executor.ExecContext(ctx, `INSERT INTO bas_workpaper_sales_sources_v1(workpaper_id,sequence,invoice_id,invoice_number,customer_name,tax_date,gross_minor,gst_minor,payment_id,source_revision,source_hash) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, workpaperID, index+1, source.InvoiceId, source.InvoiceNumber, source.CustomerName, civilDateString(source.TaxDate), source.Gross.MinorUnits, source.Gst.MinorUnits, source.PaymentId, source.SourceRevision, source.SourceHash); err != nil {
			return nil, ErrRepository
		}
	}
	if captured.ownedPurchases != nil {
		if err := repository.savePurchaseSnapshot(ctx, workpaper); err != nil {
			return nil, err
		}
	}
	return workpaper, nil
}

func (repository *Repository) GetCurrentBASDraft(ctx context.Context, organisationID string) (*tammyv1.BasWorkpaper, error) {
	if repository == nil || repository.executor == nil || ctx == nil || !ids.IsCanonicalV7(organisationID) {
		return nil, ErrRepository
	}
	return repository.getOne(ctx, `WHERE id = (
		SELECT save_order.workpaper_id FROM bas_workpaper_save_order_v1 AS save_order
		JOIN bas_workpapers AS saved ON saved.id = save_order.workpaper_id
		WHERE saved.organisation_id = ? ORDER BY save_order.sequence DESC LIMIT 1
	)`, organisationID)
}

func (repository *Repository) getByOperation(ctx context.Context, operationKey string) (*tammyv1.BasWorkpaper, error) {
	return repository.getOne(ctx, `WHERE operation_key = ?`, operationKey)
}

func (repository *Repository) getOne(ctx context.Context, predicate string, value any) (*tammyv1.BasWorkpaper, error) {
	workpaper := &tammyv1.BasWorkpaper{
		SalesG1: aud(0), GstOnSales_1A: aud(0), GstCredits_1B: aud(0), NetGstPayable: aud(0),
		Status: tammyv1.BasWorkpaperStatus_BAS_WORKPAPER_STATUS_DRAFT_NOT_LODGED,
	}
	var start, end, createdAt, status, warningsJSON string
	var basis int32
	err := querySingle(repository.executor, ctx, `
		SELECT id, organisation_id, version, period_start, period_end, status,
		       sales_g1_minor, gst_on_sales_1a_minor, gst_credits_1b_minor,
		       net_gst_payable_minor, created_at,gst_basis,source_fingerprint,review_warnings_json,excluded_cash_purchase_count FROM bas_workpapers `+predicate, []any{value},
		&workpaper.Id, &workpaper.OrganisationId, &workpaper.Version, &start, &end, &status,
		&workpaper.SalesG1.MinorUnits, &workpaper.GstOnSales_1A.MinorUnits,
		&workpaper.GstCredits_1B.MinorUnits, &workpaper.NetGstPayable.MinorUnits, &createdAt, &basis, &workpaper.SourceFingerprint, &warningsJSON, &workpaper.ExcludedCashPurchaseCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil || status != "DRAFT_NOT_LODGED" {
		return nil, ErrRepository
	}
	workpaper.PeriodStart, workpaper.PeriodEnd = parseCivilDate(start), parseCivilDate(end)
	created, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, ErrRepository
	}
	workpaper.CreatedAt = timestamppb.New(created)
	workpaper.GstBasis = tammyv1.GstBasis(basis)
	if workpaper.PeriodStart == nil || workpaper.PeriodEnd == nil || json.Unmarshal([]byte(warningsJSON), &workpaper.ReviewWarnings) != nil {
		return nil, ErrRepository
	}
	rows, err := repository.executor.QueryContext(ctx, `
		SELECT document_id, supplier_name, invoice_number, document_date, gross_minor, gst_credit_minor
		FROM bas_workpaper_sources WHERE workpaper_id = ? ORDER BY sequence`, workpaper.Id)
	if err != nil {
		return nil, ErrRepository
	}
	for rows.Next() {
		line := &tammyv1.BasSourceLine{Gross: aud(0), GstCredit: aud(0)}
		var date string
		if err := rows.Scan(&line.DocumentId, &line.SupplierName, &line.InvoiceNumber, &date, &line.Gross.MinorUnits, &line.GstCredit.MinorUnits); err != nil {
			rows.Close()
			return nil, ErrRepository
		}
		line.DocumentDate = parseCivilDate(date)
		workpaper.Sources = append(workpaper.Sources, line)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, ErrRepository
	}
	rows.Close()
	salesRows, err := repository.executor.QueryContext(ctx, `SELECT invoice_id,invoice_number,customer_name,tax_date,gross_minor,gst_minor,payment_id,source_revision,source_hash FROM bas_workpaper_sales_sources_v1 WHERE workpaper_id=? ORDER BY sequence`, workpaper.Id)
	if err != nil {
		return nil, ErrRepository
	}
	for salesRows.Next() {
		source := &tammyv1.BasSalesSourceLine{Gross: aud(0), Gst: aud(0)}
		var date string
		if err := salesRows.Scan(&source.InvoiceId, &source.InvoiceNumber, &source.CustomerName, &date, &source.Gross.MinorUnits, &source.Gst.MinorUnits, &source.PaymentId, &source.SourceRevision, &source.SourceHash); err != nil {
			salesRows.Close()
			return nil, ErrRepository
		}
		source.TaxDate = parseCivilDate(date)
		if source.TaxDate == nil {
			salesRows.Close()
			return nil, ErrRepository
		}
		workpaper.SalesSources = append(workpaper.SalesSources, source)
	}
	if err := salesRows.Err(); err != nil {
		salesRows.Close()
		return nil, ErrRepository
	}
	salesRows.Close()
	if err := repository.loadPurchaseSnapshot(ctx, workpaper); err != nil {
		return nil, err
	}
	if !basResponseFits(workpaper) {
		return nil, ErrSourceBound
	}
	current, err := repository.collectBASSources(ctx, workpaper.OrganisationId, start, end, repository.now().UTC())
	if errors.Is(err, ErrGSTBasis) {
		workpaper.SourcesChanged = true
		workpaper.ReviewWarnings = append(workpaper.ReviewWarnings, "GST basis history needs review before regenerating this workpaper. Saved figures have been retained.")
	} else if errors.Is(err, ErrSourceBound) {
		workpaper.SourcesChanged = true
		workpaper.ReviewWarnings = append(workpaper.ReviewWarnings, "The complete current source set exceeds supported workpaper capacity. Saved figures have been retained; complete preparation requires supported volume.")
	} else if err != nil {
		return nil, err
	} else {
		workpaper.SourcesChanged = len(workpaper.SourceFingerprint) != 32 || !bytes.Equal(workpaper.SourceFingerprint, current.fingerprint)
	}
	if len(workpaper.SourceFingerprint) == 0 {
		workpaper.ReviewWarnings = append(workpaper.ReviewWarnings, "This older workpaper has no complete sales fingerprint. Regenerate it to include linked invoice income.")
	}
	if !basResponseFits(workpaper) {
		return nil, ErrSourceBound
	}
	return workpaper, nil
}

func addBASAmount(total *int64, value int64) bool {
	if value < 0 || value > math.MaxInt64-*total {
		return false
	}
	*total += value
	return true
}
func basResponseFits(w *tammyv1.BasWorkpaper) bool {
	if w == nil || len(w.Sources) > 1000 || len(w.SalesSources) > 1000 || len(w.PurchaseSources) > 1000 || len(w.PurchaseReviews) > 1000 || len(w.LegacyPurchaseSupport) > 1000 || len(w.ReviewWarnings) > 10 {
		return false
	}
	for _, warning := range w.ReviewWarnings {
		if len([]rune(warning)) > 500 {
			return false
		}
	}
	for _, response := range []proto.Message{&tammyv1.CreateBasDraftResponse{Workpaper: w}, &tammyv1.GetCurrentBasDraftResponse{Workpaper: w}} {
		content, err := anypb.New(response)
		if err != nil || proto.Size(content) > 4*1024*1024 {
			return false
		}
	}
	return true
}

// source_hash in the saved tables commits the exact snapshot bytes. The owned
// business source hash remains inside the retained protobuf SourceHash field.
// This covers classifications and review prose as well as indexed scalars.
func savedPurchaseBytes(value proto.Message) ([]byte, []byte, error) {
	content, err := proto.MarshalOptions{Deterministic: true}.Marshal(value)
	if err != nil {
		return nil, nil, ErrRepository
	}
	digest := sha256.Sum256(content)
	return content, digest[:], nil
}
func (r *Repository) savePurchaseSnapshot(ctx context.Context, w *tammyv1.BasWorkpaper) error {
	if _, err := r.executor.ExecContext(ctx, `INSERT INTO bas_workpaper_purchase_metadata_v1(workpaper_id,organisation_id,purchase_source_format,legacy_candidate_gst_minor,currency_code) VALUES(?,?,'RECONCILED_V1',?,'AUD')`, w.Id, w.OrganisationId, w.LegacyCandidateGst.MinorUnits); err != nil {
		return ErrRepository
	}
	for i, s := range w.PurchaseSources {
		content, hash, err := savedPurchaseBytes(s)
		if err != nil || len(content) > 32768 {
			return ErrSourceBound
		}
		if _, err := r.executor.ExecContext(ctx, `INSERT INTO bas_workpaper_purchase_sources_v1(workpaper_id,sequence,organisation_id,posting_id,document_id,allocation_sequence,payment_id,attribution_id,claim_period_start,claim_period_end,gross_minor,supplier_gst_minor,eligible_gst_minor,claimed_gst_minor,currency_code,source_revision,source_hash,snapshot_proto) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,'AUD',?,?,?)`, w.Id, i+1, w.OrganisationId, s.PostingId, s.DocumentId, s.AllocationSequence, s.PaymentId, s.AttributionId, civilDateString(s.ClaimPeriod.PeriodStart), civilDateString(s.ClaimPeriod.PeriodEnd), s.AllocatedGross.MinorUnits, s.SupplierGst.MinorUnits, s.EligibleGst.MinorUnits, s.ClaimedGst.MinorUnits, s.SourceRevision, hash, content); err != nil {
			return ErrRepository
		}
	}
	for i, s := range w.PurchaseReviews {
		content, hash, err := savedPurchaseBytes(s)
		if err != nil || len(content) > 65536 {
			return ErrSourceBound
		}
		kind := "POSTING"
		var document, opening, legacy any
		if s.DocumentId != nil {
			document = *s.DocumentId
		}
		if ref := s.LegacySource.GetDocument(); ref != nil {
			if s.DocumentId != nil && *s.DocumentId != ref.DocumentId {
				return ErrRepository
			}
			kind = "DOCUMENT"
			document = ref.DocumentId
		}
		if ref := s.LegacySource.GetOpeningPayable(); ref != nil {
			kind = "OPENING_PAYABLE"
			opening = ref.OpeningPayableId
		}
		if s.LegacyReview != nil {
			legacy = s.LegacyReview.Id
		}
		if _, err := r.executor.ExecContext(ctx, `INSERT INTO bas_workpaper_purchase_reviews_v1(workpaper_id,sequence,organisation_id,kind,status,source_kind,posting_id,document_id,opening_payable_id,payment_id,deferral_id,legacy_review_id,affected_period_start,affected_period_end,eligible_gst_minor,unclaimed_gst_minor,currency_code,source_revision,source_hash,snapshot_proto) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,'AUD',?,?,?)`, w.Id, i+1, w.OrganisationId, s.Kind, s.Status, kind, s.PostingId, document, opening, s.PaymentId, s.DeferralId, legacy, civilDateString(s.AffectedPeriod.PeriodStart), civilDateString(s.AffectedPeriod.PeriodEnd), s.EligibleGst.MinorUnits, s.UnclaimedGst.MinorUnits, s.SourceRevision, hash, content); err != nil {
			return ErrRepository
		}
	}
	for _, s := range w.LegacyPurchaseSupport {
		content, hash, err := savedPurchaseBytes(s)
		if err != nil || len(content) > 2048 {
			return ErrSourceBound
		}
		if _, err := r.executor.ExecContext(ctx, `INSERT INTO bas_workpaper_purchase_support_v1(workpaper_id,organisation_id,review_id,kind,sequence,source_id,source_version,source_hash,snapshot_proto) VALUES(?,?,?,?,?,?,?,?,?)`, w.Id, w.OrganisationId, s.ReviewId, s.Kind, s.Sequence, s.Source.SourceId, s.Source.SourceVersion, hash, content); err != nil {
			return ErrRepository
		}
	}
	return nil
}
func savedPurchaseMessage(value proto.Message, content, hash []byte) error {
	digest := sha256.Sum256(content)
	if len(hash) != 32 || !bytes.Equal(hash, digest[:]) || proto.Unmarshal(content, value) != nil {
		return ErrRepository
	}
	canonical, err := proto.MarshalOptions{Deterministic: true}.Marshal(value)
	if err != nil || !bytes.Equal(content, canonical) || protovalidate.Validate(value) != nil {
		return ErrRepository
	}
	return nil
}
func (r *Repository) loadPurchaseSnapshot(ctx context.Context, w *tammyv1.BasWorkpaper) error {
	var present int
	if querySingle(r.executor, ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='bas_workpaper_purchase_metadata_v1'`, nil, &present) != nil {
		return ErrRepository
	}
	if present == 0 {
		return nil
	}
	var format, currency string
	var candidate int64
	err := querySingle(r.executor, ctx, `SELECT purchase_source_format,legacy_candidate_gst_minor,currency_code FROM bas_workpaper_purchase_metadata_v1 WHERE workpaper_id=? AND organisation_id=?`, []any{w.Id, w.OrganisationId}, &format, &candidate, &currency)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil || format != "RECONCILED_V1" || currency != "AUD" || candidate < 0 {
		return ErrRepository
	}
	f := tammyv1.BasPurchaseSourceFormat_BAS_PURCHASE_SOURCE_FORMAT_RECONCILED_V1
	w.PurchaseSourceFormat = &f
	w.LegacyCandidateGst = aud(candidate)
	rows, err := r.executor.QueryContext(ctx, `SELECT sequence,posting_id,document_id,allocation_sequence,payment_id,attribution_id,claim_period_start,claim_period_end,gross_minor,supplier_gst_minor,eligible_gst_minor,claimed_gst_minor,source_revision,source_hash,snapshot_proto FROM bas_workpaper_purchase_sources_v1 WHERE workpaper_id=? AND organisation_id=? ORDER BY sequence`, w.Id, w.OrganisationId)
	if err != nil {
		return ErrRepository
	}
	for rows.Next() {
		var sequence int
		var posting, document, start, end string
		var allocation uint32
		var payment, attribution *string
		var gross, supplier, eligible, claimed int64
		var revision uint64
		var hash, content []byte
		if rows.Scan(&sequence, &posting, &document, &allocation, &payment, &attribution, &start, &end, &gross, &supplier, &eligible, &claimed, &revision, &hash, &content) != nil {
			_ = rows.Close()
			return ErrRepository
		}
		value := &tammyv1.BasPurchaseSourceLine{}
		if sequence != len(w.PurchaseSources)+1 || len(w.PurchaseSources) >= 1000 || len(content) > 32768 || savedPurchaseMessage(value, content, hash) != nil || value.PostingId != posting || value.DocumentId != document || value.AllocationSequence != allocation || !sameNullableString(value.PaymentId, payment) || !sameNullableString(value.AttributionId, attribution) || value.ClaimPeriod == nil || civilDateString(value.ClaimPeriod.PeriodStart) != start || civilDateString(value.ClaimPeriod.PeriodEnd) != end || value.GetAllocatedGross().GetMinorUnits() != gross || value.GetSupplierGst().GetMinorUnits() != supplier || value.GetEligibleGst().GetMinorUnits() != eligible || value.GetClaimedGst().GetMinorUnits() != claimed || value.SourceRevision != revision {
			_ = rows.Close()
			return ErrRepository
		}
		w.PurchaseSources = append(w.PurchaseSources, value)
	}
	if errors.Join(rows.Err(), rows.Close()) != nil {
		return ErrRepository
	}
	rows, err = r.executor.QueryContext(ctx, `SELECT sequence,kind,status,source_kind,posting_id,document_id,opening_payable_id,payment_id,deferral_id,legacy_review_id,affected_period_start,affected_period_end,eligible_gst_minor,unclaimed_gst_minor,source_revision,source_hash,snapshot_proto FROM bas_workpaper_purchase_reviews_v1 WHERE workpaper_id=? AND organisation_id=? ORDER BY sequence`, w.Id, w.OrganisationId)
	if err != nil {
		return ErrRepository
	}
	for rows.Next() {
		var sequence int
		var kind, status int32
		var sourceKind, start, end string
		var posting, document, opening, payment, deferral, legacy *string
		var eligible, unclaimed int64
		var revision uint64
		var hash, content []byte
		if rows.Scan(&sequence, &kind, &status, &sourceKind, &posting, &document, &opening, &payment, &deferral, &legacy, &start, &end, &eligible, &unclaimed, &revision, &hash, &content) != nil {
			_ = rows.Close()
			return ErrRepository
		}
		value := &tammyv1.BasPurchaseReview{}
		if sequence != len(w.PurchaseReviews)+1 || len(w.PurchaseReviews) >= 1000 || len(content) > 65536 || savedPurchaseMessage(value, content, hash) != nil || int32(value.Kind) != kind || int32(value.Status) != status || !sameNullableString(value.PostingId, posting) || !sameNullableString(value.PaymentId, payment) || !sameNullableString(value.DeferralId, deferral) || value.AffectedPeriod == nil || civilDateString(value.AffectedPeriod.PeriodStart) != start || civilDateString(value.AffectedPeriod.PeriodEnd) != end || value.GetEligibleGst().GetMinorUnits() != eligible || value.GetUnclaimedGst().GetMinorUnits() != unclaimed || value.SourceRevision != revision {
			_ = rows.Close()
			return ErrRepository
		}
		actualKind := "POSTING"
		var actualDocument, actualOpening, actualLegacy *string
		actualDocument = value.DocumentId
		if ref := value.LegacySource.GetDocument(); ref != nil {
			if value.DocumentId != nil && *value.DocumentId != ref.DocumentId {
				_ = rows.Close()
				return ErrRepository
			}
			actualKind = "DOCUMENT"
			id := ref.DocumentId
			actualDocument = &id
		}
		if ref := value.LegacySource.GetOpeningPayable(); ref != nil {
			actualKind = "OPENING_PAYABLE"
			id := ref.OpeningPayableId
			actualOpening = &id
		}
		if value.LegacyReview != nil {
			id := value.LegacyReview.Id
			actualLegacy = &id
		}
		if actualKind != sourceKind || !sameNullableString(actualDocument, document) || !sameNullableString(actualOpening, opening) || !sameNullableString(actualLegacy, legacy) {
			_ = rows.Close()
			return ErrRepository
		}
		w.PurchaseReviews = append(w.PurchaseReviews, value)
	}
	if errors.Join(rows.Err(), rows.Close()) != nil {
		return ErrRepository
	}
	rows, err = r.executor.QueryContext(ctx, `SELECT review_id,kind,sequence,source_id,source_version,source_hash,snapshot_proto FROM bas_workpaper_purchase_support_v1 WHERE workpaper_id=? AND organisation_id=? ORDER BY review_id,kind,sequence`, w.Id, w.OrganisationId)
	if err != nil {
		return ErrRepository
	}
	for rows.Next() {
		var review, id string
		var kind int32
		var sequence uint32
		var revision uint64
		var hash, content []byte
		if rows.Scan(&review, &kind, &sequence, &id, &revision, &hash, &content) != nil {
			_ = rows.Close()
			return ErrRepository
		}
		value := &tammyv1.BasPurchaseLegacySupport{}
		if len(w.LegacyPurchaseSupport) >= 1000 || len(content) > 2048 || savedPurchaseMessage(value, content, hash) != nil || value.ReviewId != review || int32(value.Kind) != kind || value.Sequence != sequence || value.Source == nil || value.Source.SourceId != id || value.Source.SourceVersion != revision {
			_ = rows.Close()
			return ErrRepository
		}
		w.LegacyPurchaseSupport = append(w.LegacyPurchaseSupport, value)
	}
	if errors.Join(rows.Err(), rows.Close()) != nil {
		return ErrRepository
	}
	return nil
}
func sameNullableString(a, b *string) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func validDate(value *tammyv1.CivilDate) bool {
	if value == nil || value.Year < 1 || value.Month < 1 || value.Month > 12 || value.Day < 1 || value.Day > 31 {
		return false
	}
	_, err := time.Parse("2006-01-02", civilDateString(value))
	return err == nil
}

func civilDateString(value *tammyv1.CivilDate) string {
	return fmt.Sprintf("%04d-%02d-%02d", value.Year, value.Month, value.Day)
}

func parseCivilDate(value string) *tammyv1.CivilDate {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return nil
	}
	return &tammyv1.CivilDate{Year: int32(parsed.Year()), Month: int32(parsed.Month()), Day: int32(parsed.Day())}
}

func aud(minor int64) *tammyv1.Money { return &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: minor} }

func querySingle(executor app.CommandSQLExecutor, ctx context.Context, query string, args []any, destinations ...any) error {
	rows, err := executor.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	if err := rows.Scan(destinations...); err != nil {
		return err
	}
	if rows.Next() {
		return ErrRepository
	}
	return rows.Err()
}
