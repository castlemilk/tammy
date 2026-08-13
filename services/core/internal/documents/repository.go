package documents

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	ErrRepository       = errors.New("documents: repository failure")
	ErrDocumentNotFound = errors.New("documents: document not found")
	ErrDocumentConflict = errors.New("documents: stale document version")
	ErrDuplicateSource  = errors.New("documents: source already retained")
)

// Repository is scoped to a caller-owned encrypted transaction.
type Repository struct{ executor app.CommandSQLExecutor }

func NewRepository(executor app.CommandSQLExecutor) (*Repository, error) {
	if executor == nil {
		return nil, ErrRepository
	}
	return &Repository{executor: executor}, nil
}

func (repository *Repository) Create(
	ctx context.Context,
	operationKey string,
	createdByUserID string,
	document *tammyv1.Document,
	original []byte,
) (*tammyv1.Document, error) {
	if repository == nil || repository.executor == nil || ctx == nil ||
		!ids.IsCanonicalV7(operationKey) || !ids.IsCanonicalV7(createdByUserID) || !validDocument(document, false) ||
		len(original) == 0 || len(original) > 10*1024*1024 || uint64(len(original)) != document.ByteLength {
		return nil, ErrRepository
	}
	digest := sha256.Sum256(original)
	if subtle.ConstantTimeCompare(digest[:], document.Sha256) != 1 {
		return nil, ErrRepository
	}
	if existing, err := repository.getByOperation(ctx, operationKey); err == nil {
		if sameImmutableDocument(existing, document) {
			return existing, nil
		}
		return nil, ErrDocumentConflict
	} else if !errors.Is(err, ErrDocumentNotFound) {
		return nil, err
	}
	date := nullableCivilDate(document.Candidate.DocumentDate)
	_, err := repository.executor.ExecContext(ctx, `
		INSERT INTO documents(
			id, operation_key, organisation_id, version, status, source_display_name,
			mime_type, byte_length, sha256, original_bytes, extracted_text,
			supplier_name, invoice_number, document_date, subtotal_minor, gst_minor,
			total_minor, created_by_user_id, created_at, reviewed_at
		) VALUES (?, ?, ?, 1, 'NEEDS_REVIEW', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)`,
		document.Id, operationKey, document.OrganisationId, document.SourceDisplayName,
		document.MimeType, document.ByteLength, document.Sha256, original, document.ExtractedText,
		document.Candidate.SupplierName, document.Candidate.InvoiceNumber, date,
		document.Candidate.Subtotal.MinorUnits, document.Candidate.Gst.MinorUnits,
		document.Candidate.Total.MinorUnits, createdByUserID,
		document.GetCreatedAt().AsTime().UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "documents.organisation_id, documents.sha256") {
			return nil, ErrDuplicateSource
		}
		return nil, fmt.Errorf("%w: insert document: %v", ErrRepository, err)
	}
	return proto.Clone(document).(*tammyv1.Document), nil
}

func (repository *Repository) Get(ctx context.Context, documentID string) (*tammyv1.Document, error) {
	if repository == nil || repository.executor == nil || ctx == nil || !ids.IsCanonicalV7(documentID) {
		return nil, ErrRepository
	}
	return repository.getOne(ctx, `WHERE id = ?`, documentID)
}

func (repository *Repository) getByOperation(ctx context.Context, operationKey string) (*tammyv1.Document, error) {
	return repository.getOne(ctx, `WHERE operation_key = ?`, operationKey)
}

func (repository *Repository) getOne(ctx context.Context, predicate string, value any) (*tammyv1.Document, error) {
	rows, err := repository.executor.QueryContext(ctx, documentSelect+" "+predicate, value)
	if err != nil {
		return nil, fmt.Errorf("%w: query document: %v", ErrRepository, err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("%w: read document: %v", ErrRepository, err)
		}
		return nil, ErrDocumentNotFound
	}
	document, err := scanDocument(rows)
	if err != nil || rows.Next() || rows.Err() != nil {
		return nil, ErrRepository
	}
	return document, nil
}

// List returns a bounded stable page ordered newest first.
func (repository *Repository) List(ctx context.Context, organisationID string, limit int) ([]*tammyv1.Document, error) {
	if repository == nil || repository.executor == nil || ctx == nil ||
		!ids.IsCanonicalV7(organisationID) || limit < 1 || limit > 200 {
		return nil, ErrRepository
	}
	rows, err := repository.executor.QueryContext(ctx, documentSelect+`
		WHERE organisation_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`, organisationID, limit)
	if err != nil {
		return nil, fmt.Errorf("%w: list documents: %v", ErrRepository, err)
	}
	defer rows.Close()
	documents := make([]*tammyv1.Document, 0, limit)
	for rows.Next() {
		document, scanErr := scanDocument(rows)
		if scanErr != nil {
			return nil, ErrRepository
		}
		documents = append(documents, document)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: finish document list: %v", ErrRepository, err)
	}
	return documents, nil
}

func (repository *Repository) SaveReview(
	ctx context.Context,
	documentID string,
	expectedVersion uint64,
	candidate *tammyv1.DocumentCandidate,
	now time.Time,
) (*tammyv1.Document, error) {
	if repository == nil || repository.executor == nil || ctx == nil ||
		!ids.IsCanonicalV7(documentID) || expectedVersion == 0 || !validCandidate(candidate) || now.IsZero() {
		return nil, ErrRepository
	}
	result, err := repository.executor.ExecContext(ctx, `
		UPDATE documents SET version = version + 1, status = 'REVIEWED', supplier_name = ?,
			invoice_number = ?, document_date = ?, subtotal_minor = ?, gst_minor = ?, total_minor = ?, reviewed_at = ?
		WHERE id = ? AND version = ? AND status = 'NEEDS_REVIEW'`,
		candidate.SupplierName, candidate.InvoiceNumber, nullableCivilDate(candidate.DocumentDate),
		candidate.Subtotal.MinorUnits, candidate.Gst.MinorUnits, candidate.Total.MinorUnits,
		now.UTC().Format(time.RFC3339Nano), documentID, expectedVersion)
	if err != nil {
		return nil, fmt.Errorf("%w: save review: %v", ErrRepository, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, ErrRepository
	}
	if affected != 1 {
		return nil, ErrDocumentConflict
	}
	return repository.Get(ctx, documentID)
}

const documentSelect = `
	SELECT id, organisation_id, version, status, source_display_name, mime_type,
	       byte_length, sha256, extracted_text, supplier_name, invoice_number,
	       document_date, subtotal_minor, gst_minor, total_minor, created_at, reviewed_at
	FROM documents`

func scanDocument(scanner interface{ Scan(...any) error }) (*tammyv1.Document, error) {
	document := &tammyv1.Document{Candidate: &tammyv1.DocumentCandidate{
		Subtotal: audMoney(0), Gst: audMoney(0), Total: audMoney(0),
	}}
	var status, createdAt string
	var documentDate, reviewedAt sql.NullString
	if err := scanner.Scan(
		&document.Id, &document.OrganisationId, &document.Version, &status,
		&document.SourceDisplayName, &document.MimeType, &document.ByteLength, &document.Sha256,
		&document.ExtractedText, &document.Candidate.SupplierName, &document.Candidate.InvoiceNumber,
		&documentDate, &document.Candidate.Subtotal.MinorUnits, &document.Candidate.Gst.MinorUnits,
		&document.Candidate.Total.MinorUnits, &createdAt, &reviewedAt,
	); err != nil {
		return nil, err
	}
	document.Status = parseStatus(status)
	document.Candidate.DocumentDate = parseCivilDate(documentDate)
	created, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, err
	}
	document.CreatedAt = timestamppb.New(created)
	if reviewedAt.Valid {
		reviewed, parseErr := time.Parse(time.RFC3339Nano, reviewedAt.String)
		if parseErr != nil {
			return nil, parseErr
		}
		document.ReviewedAt = timestamppb.New(reviewed)
	}
	if !validDocument(document, document.Status == tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED) {
		return nil, ErrRepository
	}
	return document, nil
}

func validDocument(document *tammyv1.Document, reviewed bool) bool {
	if document == nil || !ids.IsCanonicalV7(document.Id) || !ids.IsCanonicalV7(document.OrganisationId) ||
		document.Version == 0 || document.SourceDisplayName == "" || strings.TrimSpace(document.SourceDisplayName) != document.SourceDisplayName ||
		len(document.SourceDisplayName) > 255 || !validMime(document.MimeType) || document.ByteLength < 1 ||
		document.ByteLength > 10*1024*1024 || len(document.Sha256) != 32 || len(document.ExtractedText) > 1024*1024 ||
		!validCandidate(document.Candidate) || document.CreatedAt == nil || document.CreatedAt.CheckValid() != nil {
		return false
	}
	if reviewed {
		return document.Status == tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED &&
			document.ReviewedAt != nil && document.ReviewedAt.CheckValid() == nil
	}
	return document.Status == tammyv1.DocumentStatus_DOCUMENT_STATUS_NEEDS_REVIEW && document.ReviewedAt == nil
}

func validCandidate(candidate *tammyv1.DocumentCandidate) bool {
	return candidate != nil && len(candidate.SupplierName) <= 256 && len(candidate.InvoiceNumber) <= 128 &&
		strings.TrimSpace(candidate.SupplierName) == candidate.SupplierName &&
		strings.TrimSpace(candidate.InvoiceNumber) == candidate.InvoiceNumber && validCivilDate(candidate.DocumentDate) &&
		validMoney(candidate.Subtotal) && validMoney(candidate.Gst) && validMoney(candidate.Total) &&
		candidate.Subtotal.MinorUnits >= 0 && candidate.Gst.MinorUnits >= 0 && candidate.Total.MinorUnits >= 0
}

func validCivilDate(value *tammyv1.CivilDate) bool {
	if value == nil {
		return true
	}
	date := time.Date(int(value.Year), time.Month(value.Month), int(value.Day), 0, 0, 0, 0, time.UTC)
	return int32(date.Year()) == value.Year && int32(date.Month()) == value.Month && int32(date.Day()) == value.Day
}

func validMoney(value *tammyv1.Money) bool { return value != nil && value.CurrencyCode == "AUD" }

func validMime(value string) bool {
	return value == "application/pdf" || value == "image/png" || value == "image/jpeg"
}

func nullableCivilDate(value *tammyv1.CivilDate) any {
	if value == nil {
		return nil
	}
	return fmt.Sprintf("%04d-%02d-%02d", value.Year, value.Month, value.Day)
}

func parseCivilDate(value sql.NullString) *tammyv1.CivilDate {
	if !value.Valid {
		return nil
	}
	date, err := time.Parse("2006-01-02", value.String)
	if err != nil {
		return nil
	}
	return &tammyv1.CivilDate{Year: int32(date.Year()), Month: int32(date.Month()), Day: int32(date.Day())}
}

func audMoney(minor int64) *tammyv1.Money {
	return &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: minor}
}

func parseStatus(value string) tammyv1.DocumentStatus {
	if value == "NEEDS_REVIEW" {
		return tammyv1.DocumentStatus_DOCUMENT_STATUS_NEEDS_REVIEW
	}
	if value == "REVIEWED" {
		return tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED
	}
	return tammyv1.DocumentStatus_DOCUMENT_STATUS_UNSPECIFIED
}

func sameImmutableDocument(left, right *tammyv1.Document) bool {
	return left != nil && right != nil && left.OrganisationId == right.OrganisationId &&
		left.SourceDisplayName == right.SourceDisplayName && left.MimeType == right.MimeType &&
		left.ByteLength == right.ByteLength && subtle.ConstantTimeCompare(left.Sha256, right.Sha256) == 1 &&
		left.ExtractedText == right.ExtractedText && proto.Equal(left.Candidate, right.Candidate)
}
