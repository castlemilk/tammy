//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/png"
	"testing"

	"connectrpc.com/connect"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	tammyv1connect "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1/tammyv1connect"
	"github.com/tammyapp/tammy/services/core/internal/transport"
	"google.golang.org/protobuf/proto"
)

func brandingRequest[T any](message *T, capability string) *connect.Request[T] {
	request := connect.NewRequest(message)
	request.Header().Set(transport.CapabilityHeader, capability)
	return request
}

func assertInvoiceBrandingThroughRealServer(t *testing.T, module *LedgerModule, client tammyv1connect.InvoiceServiceClient, authentication *tammyv1.AuthenticationContext, organisationID, capability string) *tammyv1.InvoiceLogo {
	t.Helper()
	ctx := context.Background()
	initial, err := client.GetInvoiceTemplate(ctx, brandingRequest(&tammyv1.GetInvoiceTemplateRequest{Authentication: authentication, OrganisationId: organisationID}, capability))
	if err != nil || initial.Msg.Template.Version != 0 || initial.Msg.Template.Defaults.PaymentTermsDays != 14 {
		t.Fatalf("initial template=%v error=%v", initial, err)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 64, 32))); err != nil {
		t.Fatal(err)
	}
	// A legal ancillary chunk makes the image exactly 1 MiB. This proves the
	// authenticated protobuf envelope also fits through the real TLS server.
	content := encoded.Bytes()
	const maximumLogoBytes = 1048576
	chunk := make([]byte, maximumLogoBytes-len(content))
	binary.BigEndian.PutUint32(chunk[:4], uint32(len(chunk)-12))
	copy(chunk[4:8], "npAD")
	binary.BigEndian.PutUint32(chunk[len(chunk)-4:], crc32.ChecksumIEEE(chunk[4:len(chunk)-4]))
	content = append(append(append([]byte(nil), content[:33]...), chunk...), content[33:]...)
	upload := brandingRequest(&tammyv1.UploadInvoiceLogoRequest{CommandContext: &tammyv1.CommandContext{Authentication: authentication, IdempotencyKey: "018f0000-0000-7000-8000-000000000190"}, OrganisationId: organisationID, MimeType: "image/png", Content: content}, capability)
	unauthenticated := proto.Clone(upload.Msg).(*tammyv1.UploadInvoiceLogoRequest)
	unauthenticated.CommandContext.Authentication.SessionId = "018f0000-0000-7000-8000-000000000999"
	if _, err := client.UploadInvoiceLogo(ctx, brandingRequest(unauthenticated, capability)); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("unauthenticated logo upload=%v", err)
	}
	if _, err := module.db.ExecContext(ctx, `UPDATE user_roles SET role_code='auditor' WHERE user_id=? AND role_code='workspace_admin'`, authentication.ActorUserId); err != nil {
		t.Fatal(err)
	}
	if _, err := client.UploadInvoiceLogo(ctx, upload); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("auditor logo upload=%v", err)
	}
	if _, err := module.db.ExecContext(ctx, `UPDATE user_roles SET role_code='workspace_admin' WHERE user_id=? AND role_code='auditor'`, authentication.ActorUserId); err != nil {
		t.Fatal(err)
	}
	result, err := client.UploadInvoiceLogo(ctx, upload)
	if err != nil || result.Msg.Logo == nil || result.Msg.Logo.Width != 64 || result.Msg.Logo.Height != 32 {
		t.Fatalf("logo upload=%v error=%v", result, err)
	}
	if replay, err := client.UploadInvoiceLogo(ctx, upload); err != nil || !proto.Equal(replay.Msg, result.Msg) {
		t.Fatalf("logo replay=%v error=%v", replay, err)
	}
	changed := proto.Clone(upload.Msg).(*tammyv1.UploadInvoiceLogoRequest)
	encoded.Reset()
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 32, 16))); err != nil {
		t.Fatal(err)
	}
	changed.Content = encoded.Bytes()
	if _, err := client.UploadInvoiceLogo(ctx, brandingRequest(changed, capability)); connect.CodeOf(err) != connect.CodeAlreadyExists {
		t.Fatalf("logo replay conflict=%v", err)
	}
	get := &tammyv1.GetInvoiceLogoRequest{Authentication: authentication, OrganisationId: organisationID, LogoAssetId: result.Msg.Logo.Id}
	if read, err := client.GetInvoiceLogo(ctx, brandingRequest(get, capability)); err != nil || !bytes.Equal(read.Msg.Content, upload.Msg.Content) {
		t.Fatalf("logo read=%v error=%v", read, err)
	}
	foreign := proto.Clone(get).(*tammyv1.GetInvoiceLogoRequest)
	foreign.OrganisationId = "018f0000-0000-7000-8000-000000000999"
	if _, err := client.GetInvoiceLogo(ctx, brandingRequest(foreign, capability)); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("foreign logo=%v", err)
	}
	save := brandingRequest(&tammyv1.SaveInvoiceTemplateRequest{CommandContext: &tammyv1.CommandContext{Authentication: authentication, IdempotencyKey: "018f0000-0000-7000-8000-000000000191"}, OrganisationId: organisationID, Defaults: &tammyv1.InvoiceTemplateInput{SellerAddress: "Branding office", Notes: "Thank you", PaymentInstructions: "Bank transfer", PaymentTermsDays: 30, LogoAssetId: &result.Msg.Logo.Id}}, capability)
	saved, err := client.SaveInvoiceTemplate(ctx, save)
	if err != nil || saved.Msg.Template.Version != 1 || saved.Msg.Template.Defaults.GetLogoAssetId() != result.Msg.Logo.Id {
		t.Fatalf("save defaults=%v error=%v", saved, err)
	}
	if replay, err := client.SaveInvoiceTemplate(ctx, save); err != nil || !proto.Equal(replay.Msg, saved.Msg) {
		t.Fatalf("template replay=%v error=%v", replay, err)
	}
	stale := proto.Clone(save.Msg).(*tammyv1.SaveInvoiceTemplateRequest)
	stale.CommandContext.IdempotencyKey = "018f0000-0000-7000-8000-000000000192"
	if _, err := client.SaveInvoiceTemplate(ctx, brandingRequest(stale, capability)); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("stale defaults=%v", err)
	}
	conflict := proto.Clone(save.Msg).(*tammyv1.SaveInvoiceTemplateRequest)
	conflict.Defaults.Notes = "Changed"
	if _, err := client.SaveInvoiceTemplate(ctx, brandingRequest(conflict, capability)); connect.CodeOf(err) != connect.CodeAlreadyExists {
		t.Fatalf("template replay conflict=%v", err)
	}
	var auditCount, assetCount int
	if err := module.db.QueryRowContext(ctx, `SELECT count(*) FROM invoice_branding_command_audit_v1`).Scan(&auditCount); err != nil || auditCount != 2 {
		t.Fatalf("branding audit=%d error=%v", auditCount, err)
	}
	if err := module.db.QueryRowContext(ctx, `SELECT count(*) FROM invoice_logo_assets_v1`).Scan(&assetCount); err != nil || assetCount != 1 {
		t.Fatalf("logo assets=%d error=%v", assetCount, err)
	}
	if _, err := module.db.ExecContext(ctx, `DELETE FROM invoice_branding_command_audit_v1`); err == nil {
		t.Fatal("branding audit deletable")
	}
	return result.Msg.Logo
}
