//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/app"
	"github.com/tammyapp/tammy/services/core/internal/buildinfo"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	tammyv1connect "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1/tammyv1connect"
	"github.com/tammyapp/tammy/services/core/internal/transport"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
	"google.golang.org/protobuf/proto"
)

func assertHistoricalPrefixUpgradeAfterRestart(t *testing.T, root string, anchors workspace.AnchorStore, auditMirror *purchaseTestAuditMirror,
	previous *app.Composition, previousServer *transport.Server, previousModule *LedgerModule,
	invoice *tammyv1.Invoice, workpaper *tammyv1.BasWorkpaper,
) {
	t.Helper()
	ctx := context.Background()
	var version int
	shutdownContext, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := previousServer.Shutdown(shutdownContext); err != nil {
		t.Fatal(err)
	}
	if err := previous.Close(); err != nil {
		t.Fatal(err)
	}
	workpaper = replaceWorkspaceWithHistoricalPrefix(t, ctx, root, []byte("workspace-passphrase-long-enough"), 11, workpaper)
	path := filepath.Join(root, "core", "workspace", "tammy-workspace.db")
	lockedBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	module, err := NewLedgerModule()
	if err != nil {
		t.Fatal(err)
	}
	composition, err := app.NewLocalComposition(app.LocalCompositionConfig{
		Info: buildinfo.Info{Version: "workspace-upgrade-integration"}, Root: root,
		AttemptAnchors: anchors, AuditMirror: auditMirror, Modules: []app.LocalWorkspaceModule{module},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = composition.Close() })
	assertLockedWorkspaceUnchanged(t, module, path, lockedBytes)
	server, err := transport.NewServer(composition.Registrar(), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stopContext, stopCancel := context.WithTimeout(ctx, 3*time.Second)
		defer stopCancel()
		_ = server.Shutdown(stopContext)
	})
	ready := server.Ready()
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(ready.CAPEM)) {
		t.Fatal("invalid server CA")
	}
	httpClient := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, RootCAs: roots, ServerName: "127.0.0.1",
	}}}
	baseURL := fmt.Sprintf("https://127.0.0.1:%d", ready.Port)
	workspaceClient := tammyv1connect.NewWorkspaceServiceClient(httpClient, baseURL)
	unlock := func(passphrase string) error {
		request := brandingRequest(&tammyv1.UnlockWorkspaceRequest{
			WorkspaceFile: &tammyv1.ApprovedFileRef{CapabilityId: app.LocalWorkspaceFileCapability},
			Proof: &tammyv1.WorkspaceUnlockProof{Proof: &tammyv1.WorkspaceUnlockProof_Passphrase{
				Passphrase: &tammyv1.SecretInput{Utf8: []byte(passphrase)},
			}},
		}, ready.Capability)
		_, err := workspaceClient.UnlockWorkspace(ctx, request)
		return err
	}
	if err := unlock("incorrect-workspace-passphrase"); err == nil {
		t.Fatal("incorrect passphrase admitted the workspace")
	}
	assertLockedWorkspaceUnchanged(t, module, path, lockedBytes)
	if err := unlock("workspace-passphrase-long-enough"); err != nil {
		t.Fatal(err)
	}
	if err := module.db.QueryRowContext(ctx, `SELECT max(version) FROM schema_migrations`).Scan(&version); err != nil || version != 15 {
		t.Fatalf("unlocked schema version = %d, %v; want 15", version, err)
	}
	assertHistoricalFixtureIntegrity(t, ctx, module.db)
	for _, table := range []string{"purchase_postings_v1", "purchase_posting_allocations_v1", "purchase_payments_v1", "purchase_payment_allocations_v1", "purchase_tax_evidence_v1", "purchase_gst_attributions_v1", "purchase_gst_deferrals_v1", "purchase_gst_deferral_portions_v1", "purchase_legacy_reviews_v1", "purchase_accounting_events_v1", "purchase_command_audit_v1", "bas_workpaper_purchase_sources_v1", "bas_workpaper_purchase_reviews_v1", "bas_workpaper_purchase_support_v1", "bas_workpaper_purchase_metadata_v1", "bas_profile_versions_v1", "bas_period_versions_v1", "bas_family_identities_v1", "bas_family_state_v1", "bas_preparation_versions_v1", "bas_calculations_v1", "bas_calculation_coverage_v1", "bas_calculation_raw_coverage_v1", "bas_payg_input_versions_v1", "bas_validations_v1", "bas_frozen_bas_v1", "bas_family_events_v1"} {
		var count int
		if err := module.db.QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("upgrade created %s rows: %d, %v", table, count, err)
		}
	}
	identityClient := tammyv1connect.NewIdentityServiceClient(httpClient, baseURL)
	signedIn, err := identityClient.SignIn(ctx, brandingRequest(&tammyv1.SignInRequest{
		Username: "admin@tammy.local", Password: &tammyv1.SecretInput{Utf8: []byte("administrator-password-long-enough")},
	}, ready.Capability))
	if err != nil {
		t.Fatal(err)
	}
	auth := &tammyv1.AuthenticationContext{ActorUserId: signedIn.Msg.User.Id, SessionId: signedIn.Msg.Session.Id}
	invoiceClient := tammyv1connect.NewInvoiceServiceClient(httpClient, baseURL)
	retainedInvoice, err := invoiceClient.GetInvoice(ctx, brandingRequest(&tammyv1.GetInvoiceRequest{
		Authentication: auth, OrganisationId: invoice.OrganisationId, InvoiceId: invoice.Id,
	}, ready.Capability))
	if err != nil || !proto.Equal(retainedInvoice.Msg.Invoice, invoice) {
		t.Fatalf("issued invoice changed across upgrade: %v, %v", retainedInvoice, err)
	}
	accounting, err := invoiceClient.GetInvoiceAccounting(ctx, brandingRequest(&tammyv1.GetInvoiceAccountingRequest{
		Authentication: auth, OrganisationId: invoice.OrganisationId, InvoiceId: invoice.Id,
	}, ready.Capability))
	if err != nil || !accounting.Msg.Accounting.Posted || accounting.Msg.Accounting.ReceivedMinor != 16050 || accounting.Msg.Accounting.OutstandingMinor != 22450 {
		t.Fatalf("invoice journal or payments changed across upgrade: %v, %v", accounting, err)
	}
	taxClient := tammyv1connect.NewTaxServiceClient(httpClient, baseURL)
	getRequest := &tammyv1.GetCurrentBasDraftRequest{Authentication: auth, OrganisationId: invoice.OrganisationId}
	retained, err := taxClient.GetCurrentBasDraft(ctx, brandingRequest(getRequest, ready.Capability))
	if err != nil {
		t.Fatalf("read historical saved BAS after upgrade: %v", err)
	}
	unchanged := proto.Clone(retained.Msg.Workpaper).(*tammyv1.BasWorkpaper)
	unchanged.SourcesChanged = workpaper.SourcesChanged
	if !proto.Equal(unchanged, workpaper) {
		t.Fatalf("saved BAS figures or sources changed across upgrade: %v, %v", retained, err)
	}
	regenerated, err := taxClient.CreateBasDraft(ctx, brandingRequest(&tammyv1.CreateBasDraftRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: auth, IdempotencyKey: "018f0000-0000-7000-8000-000000000f11"},
		OrganisationId: invoice.OrganisationId, PeriodStart: &tammyv1.CivilDate{Year: 2026, Month: 10, Day: 1},
		PeriodEnd: &tammyv1.CivilDate{Year: 2026, Month: 12, Day: 31},
	}, ready.Capability))
	if err != nil || regenerated.Msg.Workpaper.Id == workpaper.Id {
		t.Fatalf("new draft after upgrade = %v, %v", regenerated, err)
	}
	current, err := taxClient.GetCurrentBasDraft(ctx, brandingRequest(getRequest, ready.Capability))
	if err != nil || !proto.Equal(current.Msg.Workpaper, regenerated.Msg.Workpaper) {
		t.Fatalf("current draft after upgrade = %v, %v", current, err)
	}
}

func assertLockedWorkspaceUnchanged(t *testing.T, module *LedgerModule, path string, expected []byte) {
	t.Helper()
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, expected) || module.db != nil {
		t.Fatalf("locked or failed-authentication startup mutated or admitted the workspace: %v", err)
	}
}
