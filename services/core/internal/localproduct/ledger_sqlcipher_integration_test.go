//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/app"
	"github.com/tammyapp/tammy/services/core/internal/artefacts"
	"github.com/tammyapp/tammy/services/core/internal/buildinfo"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	tammyv1connect "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1/tammyv1connect"
	"github.com/tammyapp/tammy/services/core/internal/transport"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
)

func TestLedgerModuleCreatesOrganisationAndInstallsAustralianChartThroughRealServer(t *testing.T) {
	module, err := NewLedgerModule()
	if err != nil {
		t.Fatal(err)
	}
	composition, err := app.NewLocalComposition(app.LocalCompositionConfig{
		Info:           buildinfo.Info{Version: "local-ledger-integration"},
		Root:           t.TempDir(),
		AttemptAnchors: workspace.NewMemoryAnchorStore(),
		Modules:        []app.LocalWorkspaceModule{module},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = composition.Close() })
	server, err := transport.NewServer(composition.Registrar(), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
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
	identityClient := tammyv1connect.NewIdentityServiceClient(httpClient, baseURL)
	organisationClient := tammyv1connect.NewOrganisationServiceClient(httpClient, baseURL)
	accountingClient := tammyv1connect.NewAccountingServiceClient(httpClient, baseURL)

	createWorkspace := connect.NewRequest(&tammyv1.CreateWorkspaceRequest{
		SetupId:                  "018f0000-0000-7000-8000-000000000101",
		Destination:              &tammyv1.ApprovedFileRef{CapabilityId: app.LocalWorkspaceDirectoryCapability},
		WorkspacePassphrase:      &tammyv1.SecretInput{Utf8: []byte("workspace-passphrase-long-enough")},
		AdministratorUsername:    "admin@tammy.local",
		AdministratorDisplayName: "Tammy Admin",
		AdministratorPassword:    &tammyv1.SecretInput{Utf8: []byte("administrator-password-long-enough")},
	})
	createWorkspace.Header().Set(transport.CapabilityHeader, ready.Capability)
	createdWorkspace, err := workspaceClient.CreateWorkspace(context.Background(), createWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	groups, err := workspace.ParseRecoveryGroups(createdWorkspace.Msg.RecoverySecret.Utf8)
	if err != nil {
		t.Fatal(err)
	}
	confirm := connect.NewRequest(&tammyv1.ConfirmRecoveryRequest{SetupId: createWorkspace.Msg.SetupId,
		Confirmations: []*tammyv1.RecoveryGroupConfirmation{{GroupIndex: 0, Value: groups[0]}, {GroupIndex: 1, Value: groups[1]}}})
	confirm.Header().Set(transport.CapabilityHeader, ready.Capability)
	if _, err := workspaceClient.ConfirmRecovery(context.Background(), confirm); err != nil {
		t.Fatal(err)
	}
	signIn := connect.NewRequest(&tammyv1.SignInRequest{Username: "admin@tammy.local",
		Password: &tammyv1.SecretInput{Utf8: []byte("administrator-password-long-enough")}})
	signIn.Header().Set(transport.CapabilityHeader, ready.Capability)
	authenticated, err := identityClient.SignIn(context.Background(), signIn)
	if err != nil {
		t.Fatal(err)
	}
	authentication := &tammyv1.AuthenticationContext{ActorUserId: authenticated.Msg.User.Id, SessionId: authenticated.Msg.Session.Id}
	bundle, err := artefacts.LoadAUGSTV1()
	if err != nil {
		t.Fatal(err)
	}
	createOrganisation := connect.NewRequest(&tammyv1.CreateOrganisationRequest{
		CommandContext: &tammyv1.CommandContext{IdempotencyKey: "018f0000-0000-7000-8000-000000000102", Authentication: authentication},
		Abn:            "51824753556", LegalName: "Tammy Business Pty Ltd", DisplayName: "Tammy Business",
		EntityType: "AU_PRIVATE_COMPANY", GstBasis: tammyv1.GstBasis_GST_BASIS_NON_CASH,
		GstReportingFrequency: tammyv1.GstReportingFrequency_GST_REPORTING_FREQUENCY_QUARTERLY,
		FinancialYearEndMonth: 6, ActiveTaxRuleBundle: bundle.Source,
	})
	createOrganisation.Header().Set(transport.CapabilityHeader, ready.Capability)
	created, err := organisationClient.CreateOrganisation(context.Background(), createOrganisation)
	if err != nil {
		t.Fatalf("CreateOrganisation() error = %v", err)
	}
	if created.Msg.Organisation == nil || created.Msg.Organisation.DisplayName != "Tammy Business" {
		t.Fatalf("CreateOrganisation() = %#v", created.Msg)
	}
	get := connect.NewRequest(&tammyv1.GetOrganisationRequest{Authentication: authentication, OrganisationId: created.Msg.Organisation.Id})
	get.Header().Set(transport.CapabilityHeader, ready.Capability)
	read, err := organisationClient.GetOrganisation(context.Background(), get)
	if err != nil || read.Msg.Organisation == nil || read.Msg.Organisation.Id != created.Msg.Organisation.Id {
		t.Fatalf("GetOrganisation() = %#v, %v", read, err)
	}
	if installed := module.InstalledAccountCount(context.Background()); installed != 12 {
		t.Fatalf("installed account count = %d, want 12", installed)
	}
	listAccounts := connect.NewRequest(&tammyv1.ListAccountsRequest{
		Authentication: authentication,
		OrganisationId: created.Msg.Organisation.Id,
		Page:           &tammyv1.PageRequest{PageSize: 50},
	})
	listAccounts.Header().Set(transport.CapabilityHeader, ready.Capability)
	chart, err := accountingClient.ListAccounts(context.Background(), listAccounts)
	if err != nil {
		t.Fatalf("ListAccounts() error = %v", err)
	}
	if len(chart.Msg.Accounts) != 12 || chart.Msg.Page == nil || chart.Msg.Page.ReturnedCount != 12 {
		t.Fatalf("ListAccounts() = %#v, want 12 installed accounts", chart.Msg)
	}
	for index := 1; index < len(chart.Msg.Accounts); index++ {
		if chart.Msg.Accounts[index-1].Code >= chart.Msg.Accounts[index].Code {
			t.Fatalf("accounts not sorted by code: %q then %q", chart.Msg.Accounts[index-1].Code, chart.Msg.Accounts[index].Code)
		}
	}
}
