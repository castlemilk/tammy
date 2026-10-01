//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package invoices

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestInvoiceBrandingEncryptedPersistenceAndOwnership(t *testing.T) {
	db := invoiceDB(t)
	ctx := context.Background()
	repo, _ := NewRepository(db)
	initial, err := repo.GetTemplate(ctx, orgID)
	if err != nil || initial.Version != 0 || initial.Defaults.PaymentTermsDays != 14 {
		t.Fatalf("initial=%v err=%v", initial, err)
	}
	content := logoBytes(t, "image/png", 64, 32)
	digest := sha256.Sum256(content)
	logo := &tammyv1.InvoiceLogo{Id: "018f0000-0000-7000-8000-000000000050", OrganisationId: orgID, MimeType: "image/png", ByteLength: uint32(len(content)), Sha256: digest[:], Width: 64, Height: 32, CreatedAt: timestamppb.New(time.Now())}
	if err := repo.SaveLogo(ctx, logo, content, userID); err != nil {
		t.Fatal(err)
	}
	read, err := repo.GetLogo(ctx, orgID, logo.Id)
	if err != nil || !proto.Equal(read.Logo, logo) || !bytes.Equal(read.Content, content) {
		t.Fatalf("logo=%v err=%v", read, err)
	}
	if _, err := repo.GetLogo(ctx, "018f0000-0000-7000-8000-000000000999", logo.Id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-org logo=%v", err)
	}
	for _, statement := range []string{`UPDATE invoice_logo_assets_v1 SET content=x'01'`, `DELETE FROM invoice_logo_assets_v1`} {
		if _, err := db.ExecContext(ctx, statement); err == nil {
			t.Fatal("logo mutable")
		}
	}
	saved := &tammyv1.InvoiceTemplate{OrganisationId: orgID, Version: 1, Defaults: &tammyv1.InvoiceTemplateInput{SellerAddress: "Office", PaymentTermsDays: 0, LogoAssetId: &logo.Id}}
	if err := repo.SaveTemplate(ctx, saved, 0, time.Now()); err != nil {
		t.Fatal(err)
	}
	loaded, err := repo.GetTemplate(ctx, orgID)
	if err != nil || !proto.Equal(loaded, saved) {
		t.Fatalf("template=%v err=%v", loaded, err)
	}
	if err := repo.SaveTemplate(ctx, saved, 0, time.Now()); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale create=%v", err)
	}
	if _, err := repo.GetTemplate(ctx, "018f0000-0000-7000-8000-000000000999"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-org defaults=%v", err)
	}
	invoice := savedInvoice()
	invoice.LogoAssetId = &logo.Id
	if err := repo.Save(ctx, invoice, 0); err != nil {
		t.Fatal(err)
	}
	newTemplate := proto.Clone(saved).(*tammyv1.InvoiceTemplate)
	newTemplate.Version = 2
	newTemplate.Defaults.LogoAssetId = nil
	newTemplate.Defaults.SellerAddress = "Changed"
	if err := repo.SaveTemplate(ctx, newTemplate, 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	readInvoice, err := repo.Get(ctx, orgID, invoiceID)
	if err != nil || !proto.Equal(readInvoice, invoice) {
		t.Fatal("defaults changed saved invoice snapshot")
	}
	invoice.Version = 2
	invoice.State = tammyv1.InvoiceState_INVOICE_STATE_ISSUED
	if err := repo.Save(ctx, invoice, 1); err != nil {
		t.Fatal(err)
	}
	bad := savedInvoice()
	bad.Id = "018f0000-0000-7000-8000-000000000051"
	bad.Draft.Number = "INV-2"
	bad.LogoAssetId = ptr("018f0000-0000-7000-8000-000000000999")
	if err := repo.Save(ctx, bad, 0); !errors.Is(err, ErrLogoAsset) {
		t.Fatalf("foreign/missing logo=%v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM invoice_branding_command_audit_v1`); err != nil {
		t.Fatal(err)
	}
	if err := db.IntegrityCheck(ctx); err != nil {
		t.Fatal(err)
	}
}
