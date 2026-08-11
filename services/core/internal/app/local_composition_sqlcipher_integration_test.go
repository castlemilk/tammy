//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/buildinfo"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	tammyv1connect "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1/tammyv1connect"
	"github.com/tammyapp/tammy/services/core/internal/transport"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
)

func TestLocalCompositionCreatesConfirmsAndAuthenticatesRealWorkspace(t *testing.T) {
	composition, err := NewLocalComposition(LocalCompositionConfig{
		Info:           buildinfo.Info{Version: "local-integration"},
		Root:           t.TempDir(),
		AttemptAnchors: workspace.NewMemoryAnchorStore(),
	})
	if err != nil {
		t.Fatalf("NewLocalComposition() error = %v", err)
	}
	t.Cleanup(func() { _ = composition.Close() })

	server, err := transport.NewServer(composition.Registrar(), io.Discard)
	if err != nil {
		t.Fatalf("transport.NewServer() error = %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatalf("server.Start() error = %v", err)
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
	overviewClient := tammyv1connect.NewOverviewServiceClient(httpClient, baseURL)
	reportingCapabilityClient := tammyv1connect.NewReportingCapabilityServiceClient(httpClient, baseURL)
	assertLocalReportingCapability(t, reportingCapabilityClient, ready.Capability, "local-integration")

	createRequest := connect.NewRequest(&tammyv1.CreateWorkspaceRequest{
		SetupId:                  "01900f3c-7b2e-7cc4-98c4-dc0c0c073991",
		Destination:              &tammyv1.ApprovedFileRef{CapabilityId: LocalWorkspaceDirectoryCapability},
		WorkspacePassphrase:      &tammyv1.SecretInput{Utf8: []byte("workspace-passphrase-long-enough")},
		AdministratorUsername:    "admin@tammy.local",
		AdministratorDisplayName: "Tammy Admin",
		AdministratorPassword:    &tammyv1.SecretInput{Utf8: []byte("administrator-password-long-enough")},
	})
	createRequest.Header().Set(transport.CapabilityHeader, ready.Capability)
	created, err := workspaceClient.CreateWorkspace(context.Background(), createRequest)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	if created.Msg.Workspace == nil || created.Msg.RecoverySecret == nil {
		t.Fatalf("CreateWorkspace() = %#v", created.Msg)
	}
	assertLocalReportingCapability(t, reportingCapabilityClient, ready.Capability, "local-integration")
	groups, err := workspace.ParseRecoveryGroups(created.Msg.RecoverySecret.Utf8)
	if err != nil {
		t.Fatal(err)
	}
	confirmRequest := connect.NewRequest(&tammyv1.ConfirmRecoveryRequest{
		SetupId: createRequest.Msg.SetupId,
		Confirmations: []*tammyv1.RecoveryGroupConfirmation{
			{GroupIndex: 0, Value: groups[0]},
			{GroupIndex: 1, Value: groups[1]},
		},
	})
	confirmRequest.Header().Set(transport.CapabilityHeader, ready.Capability)
	if _, err := workspaceClient.ConfirmRecovery(context.Background(), confirmRequest); err != nil {
		t.Fatalf("ConfirmRecovery() error = %v", err)
	}

	signInRequest := connect.NewRequest(&tammyv1.SignInRequest{
		Username: "admin@tammy.local",
		Password: &tammyv1.SecretInput{Utf8: []byte("administrator-password-long-enough")},
	})
	signInRequest.Header().Set(transport.CapabilityHeader, ready.Capability)
	signedIn, err := identityClient.SignIn(context.Background(), signInRequest)
	if err != nil {
		t.Fatalf("SignIn() error = %v", err)
	}
	if signedIn.Msg.User == nil || signedIn.Msg.Session == nil || signedIn.Msg.User.Username != "admin@tammy.local" {
		t.Fatalf("SignIn() = %#v", signedIn.Msg)
	}
	overviewRequest := connect.NewRequest(&tammyv1.GetAttentionSummaryRequest{
		Authentication: &tammyv1.AuthenticationContext{
			ActorUserId: signedIn.Msg.User.Id,
			SessionId:   signedIn.Msg.Session.Id,
		},
		OrganisationId: created.Msg.Workspace.Id,
		AsOfDate:       &tammyv1.CivilDate{Year: 2026, Month: 8, Day: 10},
		ReportingPeriod: &tammyv1.ReportingPeriod{
			StartDate: &tammyv1.CivilDate{Year: 2026, Month: 7, Day: 1},
			EndDate:   &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 30},
		},
	})
	overviewRequest.Header().Set(transport.CapabilityHeader, ready.Capability)
	summary, err := overviewClient.GetAttentionSummary(context.Background(), overviewRequest)
	if err != nil {
		t.Fatalf("GetAttentionSummary() error = %v", err)
	}
	if summary.Msg.BasStatus != tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_NOT_CREATED ||
		summary.Msg.Revisions == nil || summary.Msg.Revisions.FinancialRevision != 0 {
		t.Fatalf("GetAttentionSummary() = %#v", summary.Msg)
	}
}

func assertLocalReportingCapability(
	t *testing.T,
	client tammyv1connect.ReportingCapabilityServiceClient,
	capabilityHeader string,
	appVersion string,
) {
	t.Helper()
	request := connect.NewRequest(&tammyv1.GetReportingCapabilityRequest{
		Report:     tammyv1.ReportKind_REPORT_KIND_GST_WORKPAPER,
		TaxYear:    2024,
		EntityType: tammyv1.ReportingEntityType_REPORTING_ENTITY_TYPE_AU_BUSINESS,
	})
	request.Header().Set(transport.CapabilityHeader, capabilityHeader)
	response, err := client.GetReportingCapability(context.Background(), request)
	if err != nil {
		t.Fatalf("GetReportingCapability() error = %v", err)
	}
	got := response.Msg.GetCapability()
	if got == nil || got.GetReport() != request.Msg.GetReport() || got.GetTaxYear() != request.Msg.GetTaxYear() ||
		got.GetEntityType() != request.Msg.GetEntityType() ||
		got.GetStatus() != tammyv1.ReportingCapabilityStatus_REPORTING_CAPABILITY_STATUS_AVAILABLE ||
		got.GetAppVersion() != appVersion {
		t.Fatalf("GetReportingCapability() = %#v", got)
	}
}

func TestLocalCompositionReopensAndAuthenticatesExistingWorkspaceAfterRestart(t *testing.T) {
	root := t.TempDir()
	anchors := workspace.NewMemoryAnchorStore()
	first, stopFirst := startLocalCompositionTestServer(t, root, anchors)
	t.Cleanup(stopFirst)

	createRequest := connect.NewRequest(&tammyv1.CreateWorkspaceRequest{
		SetupId:                  "01900f3c-7b2e-7cc4-98c4-dc0c0c0739a1",
		Destination:              &tammyv1.ApprovedFileRef{CapabilityId: LocalWorkspaceDirectoryCapability},
		WorkspacePassphrase:      &tammyv1.SecretInput{Utf8: []byte("workspace-passphrase-long-enough")},
		AdministratorUsername:    "admin@tammy.local",
		AdministratorDisplayName: "Tammy Admin",
		AdministratorPassword:    &tammyv1.SecretInput{Utf8: []byte("administrator-password-long-enough")},
	})
	createRequest.Header().Set(transport.CapabilityHeader, first.capability)
	created, err := first.workspace.CreateWorkspace(context.Background(), createRequest)
	if err != nil || created.Msg.Workspace == nil || created.Msg.RecoverySecret == nil {
		t.Fatalf("CreateWorkspace() = %#v, %v", created, err)
	}
	groups, err := workspace.ParseRecoveryGroups(created.Msg.RecoverySecret.Utf8)
	if err != nil {
		t.Fatal(err)
	}
	confirm := connect.NewRequest(&tammyv1.ConfirmRecoveryRequest{
		SetupId: createRequest.Msg.SetupId,
		Confirmations: []*tammyv1.RecoveryGroupConfirmation{
			{GroupIndex: 0, Value: groups[0]},
			{GroupIndex: 1, Value: groups[1]},
		},
	})
	confirm.Header().Set(transport.CapabilityHeader, first.capability)
	if _, err := first.workspace.ConfirmRecovery(context.Background(), confirm); err != nil {
		t.Fatalf("ConfirmRecovery() error = %v", err)
	}

	stopFirst()
	second, stopSecond := startLocalCompositionTestServer(t, root, anchors)
	t.Cleanup(stopSecond)
	unlock := connect.NewRequest(&tammyv1.UnlockWorkspaceRequest{
		WorkspaceFile: &tammyv1.ApprovedFileRef{CapabilityId: LocalWorkspaceFileCapability},
		Proof: &tammyv1.WorkspaceUnlockProof{Proof: &tammyv1.WorkspaceUnlockProof_Passphrase{
			Passphrase: &tammyv1.SecretInput{Utf8: []byte("workspace-passphrase-long-enough")},
		}},
	})
	unlock.Header().Set(transport.CapabilityHeader, second.capability)
	opened, err := second.workspace.UnlockWorkspace(context.Background(), unlock)
	if err != nil {
		t.Fatalf("UnlockWorkspace() error = %v", err)
	}
	if opened.Msg.Workspace == nil || opened.Msg.Workspace.Id != created.Msg.Workspace.Id {
		t.Fatalf("UnlockWorkspace() = %#v", opened.Msg)
	}
	signIn := connect.NewRequest(&tammyv1.SignInRequest{
		Username: "admin@tammy.local",
		Password: &tammyv1.SecretInput{Utf8: []byte("administrator-password-long-enough")},
	})
	signIn.Header().Set(transport.CapabilityHeader, second.capability)
	authenticated, err := second.identity.SignIn(context.Background(), signIn)
	if err != nil {
		t.Fatalf("SignIn() after restart error = %v", err)
	}
	if authenticated.Msg.User == nil || authenticated.Msg.Session == nil {
		t.Fatalf("SignIn() after restart = %#v", authenticated.Msg)
	}
}

type localCompositionTestClients struct {
	capability string
	identity   tammyv1connect.IdentityServiceClient
	workspace  tammyv1connect.WorkspaceServiceClient
}

func startLocalCompositionTestServer(t *testing.T, root string, anchors workspace.AnchorStore) (localCompositionTestClients, func()) {
	t.Helper()
	composition, err := NewLocalComposition(LocalCompositionConfig{
		Info:           buildinfo.Info{Version: "local-restart-integration"},
		Root:           root,
		AttemptAnchors: anchors,
	})
	if err != nil {
		t.Fatalf("NewLocalComposition() error = %v", err)
	}
	server, err := transport.NewServer(composition.Registrar(), io.Discard)
	if err != nil {
		_ = composition.Close()
		t.Fatalf("transport.NewServer() error = %v", err)
	}
	if err := server.Start(); err != nil {
		_ = composition.Close()
		t.Fatalf("server.Start() error = %v", err)
	}
	ready := server.Ready()
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(ready.CAPEM)) {
		t.Fatal("invalid server CA")
	}
	httpClient := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, RootCAs: roots, ServerName: "127.0.0.1",
	}}}
	baseURL := fmt.Sprintf("https://127.0.0.1:%d", ready.Port)
	clients := localCompositionTestClients{
		capability: ready.Capability,
		identity:   tammyv1connect.NewIdentityServiceClient(httpClient, baseURL),
		workspace:  tammyv1connect.NewWorkspaceServiceClient(httpClient, baseURL),
	}
	var once sync.Once
	return clients, func() {
		once.Do(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = server.Shutdown(ctx)
			_ = composition.Close()
		})
	}
}
