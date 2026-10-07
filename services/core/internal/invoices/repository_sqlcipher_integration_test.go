//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package invoices

import (
	"context"
	"errors"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/organisations"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"path/filepath"
	"testing"
	"time"
)

const orgID = "018f0000-0000-7000-8000-000000000020"
const userID = "018f0000-0000-7000-8000-000000000010"
const invoiceID = "018f0000-0000-7000-8000-000000000030"
const sourceID = "018f0000-0000-7000-8000-000000000040"

func invoiceDB(t *testing.T) *sqlcipher.Database {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "workspace.db")
	key := make([]byte, 32)
	if _, err := sqlcipher.MigrateWorkspace(ctx, path, key, 12); err != nil {
		t.Fatal(err)
	}
	db, err := sqlcipher.Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, `INSERT INTO users(id,email,display_name,status,created_at,updated_at) VALUES (?, 'owner@example.test','Owner','ACTIVE','2026-09-30','2026-09-30')`, userID); err != nil {
		t.Fatal(err)
	}
	repo, _ := organisations.NewRepository(db)
	if err := repo.Create(ctx, &tammyv1.Organisation{Id: orgID, Version: 1, Abn: "51824753556", LegalName: "Tammy Business Pty Ltd", DisplayName: "Tammy", EntityType: "AU_PRIVATE_COMPANY", GstBasis: tammyv1.GstBasis_GST_BASIS_NON_CASH, GstReportingFrequency: tammyv1.GstReportingFrequency_GST_REPORTING_FREQUENCY_QUARTERLY, FinancialYearEndMonth: 6, OwnerUserId: userID, VerificationState: tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_UNVERIFIED, ActiveTaxRuleBundle: &tammyv1.SourceRef{Type: "rule_bundle", Id: invoiceID, Revision: 1, ContentHash: make([]byte, 32)}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	return db
}
func savedInvoice() *tammyv1.Invoice {
	v, _ := Calculate(validDraft())
	v.Id = invoiceID
	v.OrganisationId = orgID
	v.Version = 1
	v.State = tammyv1.InvoiceState_INVOICE_STATE_DRAFT
	v.Draft = validDraft()
	v.SellerName = "Tammy Business Pty Ltd"
	v.SellerAbn = "51824753556"
	v.CreatedAt = timestamppb.New(time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC))
	v.UpdatedAt = proto.Clone(v.CreatedAt).(*timestamppb.Timestamp)
	return v
}
func TestInvoiceRepositoryEncryptedPersistenceVersionUniquenessAndIssue(t *testing.T) {
	db := invoiceDB(t)
	ctx := context.Background()
	repo, _ := NewRepository(db)
	v := savedInvoice()
	if err := repo.Save(ctx, v, 0); err != nil {
		t.Fatal(err)
	}
	read, err := repo.Get(ctx, orgID, invoiceID)
	if err != nil || !proto.Equal(v, read) {
		t.Fatalf("read=%v err=%v", read, err)
	}
	if _, err := repo.Get(ctx, "018f0000-0000-7000-8000-000000000999", invoiceID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross organisation read: %v", err)
	}
	dupe := proto.Clone(v).(*tammyv1.Invoice)
	dupe.Id = "018f0000-0000-7000-8000-000000000031"
	if err := repo.Save(ctx, dupe, 0); !errors.Is(err, ErrNumberConflict) {
		t.Fatalf("number conflict=%v", err)
	}
	v.Version = 2
	v.Draft.Notes = "Updated"
	v.UpdatedAt = timestamppb.New(v.CreatedAt.AsTime().Add(time.Minute))
	if err := repo.Save(ctx, v, 1); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, v, 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale version=%v", err)
	}
	v.Version = 3
	v.State = tammyv1.InvoiceState_INVOICE_STATE_ISSUED
	v.UpdatedAt = timestamppb.New(v.CreatedAt.AsTime().Add(2 * time.Minute))
	if err := repo.Save(ctx, v, 2); err != nil {
		t.Fatal(err)
	}
	v.Version = 4
	v.Draft.Notes = "Forbidden mutation"
	if err := repo.Save(ctx, v, 3); !errors.Is(err, ErrConflict) {
		t.Fatalf("issued edit=%v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE invoices_v1 SET number='changed' WHERE id=?`, invoiceID); err == nil {
		t.Fatal("SQL changed issued invoice")
	}
	list, err := repo.List(ctx, orgID, 200)
	if err != nil || len(list) != 1 || list[0].Version != 3 {
		t.Fatalf("list=%v err=%v", list, err)
	}
	if err := db.Checkpoint(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.IntegrityCheck(ctx); err != nil {
		t.Fatal(err)
	}
}
func TestInvoiceRepositoryOnlyReferencesReviewedOrganisationDocuments(t *testing.T) {
	db := invoiceDB(t)
	ctx := context.Background()
	repo, _ := NewRepository(db)
	v := savedInvoice()
	v.Draft.Lines[0].SourceDocumentId = ptr(sourceID)
	if err := repo.Save(ctx, v, 0); !errors.Is(err, ErrSourceDocument) {
		t.Fatalf("missing source=%v", err)
	}
	_, err := db.ExecContext(ctx, `INSERT INTO documents(id,operation_key,organisation_id,version,status,source_display_name,mime_type,byte_length,sha256,original_bytes,extracted_text,supplier_name,invoice_number,subtotal_minor,gst_minor,total_minor,created_by_user_id,created_at,reviewed_at) VALUES (?, ?, ?, 1, 'NEEDS_REVIEW','cost.pdf','application/pdf',1,zeroblob(32),x'01','','Supplier','COST-1',100,10,110,?,'2026-09-30',NULL)`, sourceID, "018f0000-0000-7000-8000-000000000041", orgID, userID)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, v, 0); !errors.Is(err, ErrSourceDocument) {
		t.Fatalf("unreviewed source=%v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE documents SET status='REVIEWED',version=2,reviewed_at='2026-09-30' WHERE id=?`, sourceID); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, v, 0); err != nil {
		t.Fatalf("reviewed source=%v", err)
	}
}

func TestInvoiceRepositoryRejectsIssuedCreationAndCorruptRetainedTotals(t *testing.T) {
	db := invoiceDB(t)
	ctx := context.Background()
	repo, _ := NewRepository(db)
	v := savedInvoice()
	v.State = tammyv1.InvoiceState_INVOICE_STATE_ISSUED
	if err := repo.Save(ctx, v, 0); !errors.Is(err, ErrRepository) {
		t.Fatalf("created issued invoice=%v", err)
	}
	v.State = tammyv1.InvoiceState_INVOICE_STATE_DRAFT
	if err := repo.Save(ctx, v, 0); err != nil {
		t.Fatal(err)
	}
	v.TotalMinor++
	if err := repo.Save(ctx, v, 0); !errors.Is(err, ErrRepository) {
		t.Fatalf("corrupt total was accepted=%v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE invoices_v1 SET version=version+1,invoice_proto=x'01' WHERE id=?`, v.Id); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(ctx, orgID, invoiceID); !errors.Is(err, ErrRepository) {
		t.Fatalf("corrupt retained protobuf read=%v", err)
	}
}
