//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package app

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/audit"
	"github.com/tammyapp/tammy/services/core/internal/buildinfo"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	tammyv1connect "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1/tammyv1connect"
	"github.com/tammyapp/tammy/services/core/internal/storage/migrations"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/transport"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
)

type localMigrationCaptureModule struct {
	database    *sqlcipher.Database
	activations int
	failNext    int
}

func (module *localMigrationCaptureModule) HandlerFactories() []transport.GeneratedHandlerFactory {
	return []transport.GeneratedHandlerFactory{func(...connect.HandlerOption) (string, http.Handler) {
		return "/tammy.v1.MigrationCaptureService/", http.NotFoundHandler()
	}}
}

func (module *localMigrationCaptureModule) Activate(activation LocalWorkspaceActivation) error {
	if module.failNext > 0 {
		module.failNext--
		return errors.New("test: injected module activation failure")
	}
	module.database = activation.Database
	module.activations++
	return nil
}

func TestLocalAttemptAnchorIDsAreScopedToOneInstallation(t *testing.T) {
	firstMaster := bytes.Repeat([]byte{0x31}, 32)
	secondMaster := bytes.Repeat([]byte{0x32}, 32)
	workspaceID := localAttemptAnchorID(firstMaster, "workspace")

	if workspaceID != localAttemptAnchorID(firstMaster, "workspace") {
		t.Fatal("same installation and purpose produced different anchor IDs")
	}
	if workspaceID == localAttemptAnchorID(secondMaster, "workspace") {
		t.Fatal("different installations shared an anchor ID")
	}
	if workspaceID == localAttemptAnchorID(firstMaster, "identity") {
		t.Fatal("workspace and identity shared an anchor ID")
	}
}

func TestLocalCompositionCreatesConfirmsAndAuthenticatesRealWorkspace(t *testing.T) {
	steps, err := migrations.All()
	if err != nil {
		t.Fatal(err)
	}
	latestMigrationVersion := steps[len(steps)-1].Version
	if localMigrationTarget != latestMigrationVersion {
		t.Fatalf("localMigrationTarget = %d, want latest embedded migration %d", localMigrationTarget, latestMigrationVersion)
	}
	migrationCapture := &localMigrationCaptureModule{}
	composition, err := NewLocalComposition(LocalCompositionConfig{
		Info:           buildinfo.Info{Version: "local-integration"},
		Root:           t.TempDir(),
		AttemptAnchors: workspace.NewMemoryAnchorStore(),
		AuditMirror:    newLocalTestAuditMirror(),
		Modules:        []LocalWorkspaceModule{migrationCapture},
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
	if migrationCapture.database == nil {
		t.Fatal("local workspace module was not activated")
	}
	var migrationVersion int
	if err := migrationCapture.database.QueryRowContext(context.Background(),
		`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&migrationVersion); err != nil {
		t.Fatalf("read migration version: %v", err)
	}
	if migrationVersion != int(latestMigrationVersion) {
		t.Fatalf("migration version = %d, want latest embedded migration %d", migrationVersion, latestMigrationVersion)
	}
	for _, table := range []string{
		"sbr_credential_bindings_v1",
		"sbr_authenticated_profiles_v1",
		"sbr_readiness_transitions_v1",
		"sbr_mutations_v1",
		"sbr_idempotency_v1",
		"sbr_simulator_transports_v1",
		"sbr_commands_v1",
		"sbr_product_states_v1",
		"sbr_audit_events_v1",
		"invoices_v1",
		"invoice_logo_assets_v1",
		"invoice_templates_v1",
		"invoice_branding_command_audit_v1",
		"bas_calculation_coverage_v1",
		"bas_calculation_raw_coverage_v1",
	} {
		var count int
		if err := migrationCapture.database.QueryRowContext(context.Background(),
			`SELECT count(*) FROM sqlite_schema WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
			t.Fatalf("inspect %s: %v", table, err)
		}
		if count != 1 {
			t.Fatalf("table %s count = %d, want 1", table, count)
		}
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
	if summary.Msg.BasStatus != tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_ACTION_NEEDED ||
		summary.Msg.BasPreparation != nil || summary.Msg.BasFamily != nil || summary.Msg.BasFrozen != nil ||
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
	mirror := newLocalTestAuditMirror()
	first, stopFirst := startLocalCompositionTestServer(t, root, anchors, mirror)
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
	second, stopSecond := startLocalCompositionTestServer(t, root, anchors, mirror)
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
	if second.capture.database == nil {
		t.Fatal("reopened workspace did not activate its database")
	}
	stored, err := audit.LoadStoredEvents(context.Background(), second.capture.database, created.Msg.Workspace.Id, 1, 3, 4)
	if err != nil || len(stored) != 2 {
		t.Fatalf("pending creation lock/unlock events = %v, %v", stored, err)
	}
	for index, want := range []struct {
		reason string
		from   tammyv1.WorkspaceState
		to     tammyv1.WorkspaceState
	}{
		{"LOCK", tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED, tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED},
		{"UNLOCK", tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED, tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED},
	} {
		transition := stored[index].Event.GetPayload().GetWorkspaceStateChanged()
		if transition == nil || transition.ReasonCode != want.reason || transition.FromState != want.from || transition.ToState != want.to {
			t.Fatalf("pending creation transition %d = %v, want %+v", index, transition, want)
		}
	}
	if _, err := mirror.Load(context.Background(), created.Msg.Workspace.Id); err != audit.ErrMirrorMissing {
		t.Fatalf("pre-sign-in lock/unlock established a mirror: %v", err)
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
	overview   tammyv1connect.OverviewServiceClient
	workspace  tammyv1connect.WorkspaceServiceClient
	capture    *localMigrationCaptureModule
}

func localTestRequest[T any](capability string, message *T) *connect.Request[T] {
	request := connect.NewRequest(message)
	request.Header().Set(transport.CapabilityHeader, capability)
	return request
}

func localTestCreateRequest(setupID string) *tammyv1.CreateWorkspaceRequest {
	return &tammyv1.CreateWorkspaceRequest{
		SetupId:               setupID,
		Destination:           &tammyv1.ApprovedFileRef{CapabilityId: LocalWorkspaceDirectoryCapability},
		WorkspacePassphrase:   &tammyv1.SecretInput{Utf8: []byte("workspace-passphrase-long-enough")},
		AdministratorUsername: "admin@tammy.local", AdministratorDisplayName: "Tammy Admin",
		AdministratorPassword: &tammyv1.SecretInput{Utf8: []byte("administrator-password-long-enough")},
	}
}

func TestLocalCompositionRejectedAdministratorInputLeavesSetupRetryable(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		password   []byte
		change     func(*tammyv1.CreateWorkspaceRequest)
		restart    bool
		newSetupID bool
	}{
		{name: "too short", password: bytes.Repeat([]byte("a"), 14)},
		{name: "too long after restart", password: bytes.Repeat([]byte("a"), 129), restart: true},
		{name: "non-printable with fresh setup ID", password: []byte("administrator-password\x00"), newSetupID: true},
		{name: "invalid UTF-8 after restart with fresh setup ID", password: append([]byte("administrator-password"), 0xff), restart: true, newSetupID: true},
		{name: "leading username whitespace", change: func(request *tammyv1.CreateWorkspaceRequest) {
			request.AdministratorUsername = " admin@tammy.local"
		}},
		{name: "trailing username whitespace after restart", restart: true, change: func(request *tammyv1.CreateWorkspaceRequest) {
			request.AdministratorUsername = "admin@tammy.local\t"
		}},
		{name: "blank username with fresh setup ID", newSetupID: true, change: func(request *tammyv1.CreateWorkspaceRequest) {
			request.AdministratorUsername = " \t\u00a0"
		}},
		{name: "empty username", change: func(request *tammyv1.CreateWorkspaceRequest) {
			request.AdministratorUsername = ""
		}},
		{name: "overlong username", change: func(request *tammyv1.CreateWorkspaceRequest) {
			request.AdministratorUsername = strings.Repeat("a", 129)
		}},
		{name: "empty display name", change: func(request *tammyv1.CreateWorkspaceRequest) {
			request.AdministratorDisplayName = ""
		}},
		{name: "overlong display name", change: func(request *tammyv1.CreateWorkspaceRequest) {
			request.AdministratorDisplayName = strings.Repeat("a", 129)
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			anchors := workspace.NewMemoryAnchorStore()
			mirror := newLocalTestAuditMirror()
			server, stop := startLocalCompositionTestServer(t, root, anchors, mirror)
			t.Cleanup(stop)
			const setupID = "01900f3c-7b2e-7cc4-98c4-dc0c0c0739b8"
			request := localTestCreateRequest(setupID)
			if testCase.password != nil {
				request.AdministratorPassword.Utf8 = append([]byte(nil), testCase.password...)
			}
			if testCase.change != nil {
				testCase.change(request)
			}
			response, err := server.workspace.CreateWorkspace(context.Background(), localTestRequest(server.capability, request))
			var connectErr *connect.Error
			if response != nil || !errors.As(err, &connectErr) || connectErr.Message() != "VALIDATION" {
				t.Fatalf("invalid administrator input: response present=%t, err=%v", response != nil, err)
			}
			entries, err := os.ReadDir(filepath.Join(root, "core", "workspace"))
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Errorf("rejected setup left %d workspace files", len(entries))
			}
			if _, err := os.Lstat(filepath.Join(root, "core", "workspace-catalogue.enc")); !os.IsNotExist(err) {
				t.Errorf("rejected setup reserved a catalogue record: %v", err)
			}
			if server.capture.database != nil || server.capture.activations != 0 {
				t.Error("rejected setup activated workspace modules")
			}
			if testCase.restart {
				stop()
				server, stop = startLocalCompositionTestServer(t, root, anchors, mirror)
				t.Cleanup(stop)
			}
			retrySetupID := setupID
			if testCase.newSetupID {
				retrySetupID = "01900f3c-7b2e-7cc4-98c4-dc0c0c0739b9"
			}
			created, groups := server.createWorkspace(t, retrySetupID)
			if err := server.confirmRecovery(retrySetupID, groups); err != nil {
				t.Fatalf("ConfirmRecovery() after corrected setup: %v", err)
			}
			if _, err := server.signIn(); err != nil {
				t.Fatalf("SignIn() after corrected setup: %v", err)
			}
			if state := server.workspaceState(t, created.Workspace.Id); state != tammyv1.WorkspaceState_WORKSPACE_STATE_AUTHENTICATED {
				t.Fatalf("corrected setup workspace state = %s, want AUTHENTICATED", state)
			}
		})
	}
}

func (clients localCompositionTestClients) createWorkspace(t *testing.T, setupID string) (*tammyv1.CreateWorkspaceResponse, [][]byte) {
	t.Helper()
	created, err := clients.workspace.CreateWorkspace(context.Background(), localTestRequest(clients.capability, localTestCreateRequest(setupID)))
	if err != nil || created.Msg.Workspace == nil || created.Msg.RecoverySecret == nil {
		t.Fatalf("CreateWorkspace() = %#v, %v", created, err)
	}
	groups, err := workspace.ParseRecoveryGroups(created.Msg.RecoverySecret.Utf8)
	if err != nil {
		t.Fatal(err)
	}
	return created.Msg, groups
}

func (clients localCompositionTestClients) confirmRecovery(setupID string, groups [][]byte) error {
	_, err := clients.workspace.ConfirmRecovery(context.Background(), localTestRequest(clients.capability, &tammyv1.ConfirmRecoveryRequest{
		SetupId:       setupID,
		Confirmations: []*tammyv1.RecoveryGroupConfirmation{{GroupIndex: 0, Value: groups[0]}, {GroupIndex: 1, Value: groups[1]}},
	}))
	return err
}

func (clients localCompositionTestClients) unlock() (*connect.Response[tammyv1.UnlockWorkspaceResponse], error) {
	return clients.workspace.UnlockWorkspace(context.Background(), localTestRequest(clients.capability, &tammyv1.UnlockWorkspaceRequest{
		WorkspaceFile: &tammyv1.ApprovedFileRef{CapabilityId: LocalWorkspaceFileCapability},
		Proof: &tammyv1.WorkspaceUnlockProof{Proof: &tammyv1.WorkspaceUnlockProof_Passphrase{
			Passphrase: &tammyv1.SecretInput{Utf8: []byte("workspace-passphrase-long-enough")},
		}},
	}))
}

func (clients localCompositionTestClients) signIn() (*tammyv1.SignInResponse, error) {
	signedIn, err := clients.identity.SignIn(context.Background(), localTestRequest(clients.capability, &tammyv1.SignInRequest{
		Username: "admin@tammy.local", Password: &tammyv1.SecretInput{Utf8: []byte("administrator-password-long-enough")},
	}))
	if err != nil {
		return nil, err
	}
	if signedIn.Msg.User == nil || signedIn.Msg.Session == nil {
		return nil, fmt.Errorf("SignIn() = %#v", signedIn.Msg)
	}
	return signedIn.Msg, nil
}

func (clients localCompositionTestClients) workspaceState(t *testing.T, workspaceID string) tammyv1.WorkspaceState {
	t.Helper()
	got, err := clients.workspace.GetWorkspaceState(context.Background(),
		localTestRequest(clients.capability, &tammyv1.GetWorkspaceStateRequest{WorkspaceId: &workspaceID}))
	if err != nil || got.Msg.Workspace == nil {
		t.Fatalf("GetWorkspaceState() = %#v, %v", got, err)
	}
	return got.Msg.Workspace.State
}

// readWithSession proves the session still authorises a read through the
// identity bridge (overview) and through the identity route (current user).
func (clients localCompositionTestClients) readWithSession(workspaceID string, signedIn *tammyv1.SignInResponse) error {
	authentication := &tammyv1.AuthenticationContext{ActorUserId: signedIn.User.Id, SessionId: signedIn.Session.Id}
	if _, err := clients.overview.GetAttentionSummary(context.Background(), localTestRequest(clients.capability, &tammyv1.GetAttentionSummaryRequest{
		Authentication: authentication,
		OrganisationId: workspaceID,
		AsOfDate:       &tammyv1.CivilDate{Year: 2026, Month: 8, Day: 10},
		ReportingPeriod: &tammyv1.ReportingPeriod{
			StartDate: &tammyv1.CivilDate{Year: 2026, Month: 7, Day: 1},
			EndDate:   &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 30},
		},
	})); err != nil {
		return fmt.Errorf("GetAttentionSummary(): %w", err)
	}
	current, err := clients.identity.GetCurrentUser(context.Background(),
		localTestRequest(clients.capability, &tammyv1.GetCurrentUserRequest{Authentication: authentication}))
	if err != nil {
		return fmt.Errorf("GetCurrentUser(): %w", err)
	}
	if current.Msg.User.GetId() != signedIn.User.Id {
		return fmt.Errorf("GetCurrentUser() = %#v, want user %s", current.Msg.User, signedIn.User.Id)
	}
	return nil
}

// assertLocalAuditTrail compares the stored audit chain, in sequence order,
// with "REASON FROM->TO" workspace transitions and "USER REASON" user changes.
func assertLocalAuditTrail(t *testing.T, database *sqlcipher.Database, workspaceID string, want ...string) {
	t.Helper()
	header, err := audit.LoadChainHeader(context.Background(), database, workspaceID, 0)
	if err != nil {
		t.Fatalf("LoadChainHeader() error = %v", err)
	}
	stored, err := audit.LoadStoredEvents(context.Background(), database, workspaceID, header.Generation, 1, header.CurrentSequence)
	if err != nil {
		t.Fatalf("LoadStoredEvents() error = %v", err)
	}
	state := func(value tammyv1.WorkspaceState) string {
		return strings.TrimPrefix(value.String(), "WORKSPACE_STATE_")
	}
	got := make([]string, 0, len(stored))
	for index, event := range stored {
		if event.Event.GetSequence() != uint64(index+1) {
			t.Fatalf("audit event %d has sequence %d", index+1, event.Event.GetSequence())
		}
		payload := event.Event.GetPayload()
		switch {
		case payload.GetWorkspaceStateChanged() != nil:
			transition := payload.GetWorkspaceStateChanged()
			got = append(got, fmt.Sprintf("%s %s->%s", transition.ReasonCode, state(transition.FromState), state(transition.ToState)))
		case payload.GetUserStateChanged() != nil:
			got = append(got, "USER "+payload.GetUserStateChanged().ReasonCode)
		default:
			got = append(got, event.Event.GetType().String())
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("audit trail = %q, want %q", got, want)
	}
}

func startLocalCompositionTestServer(t *testing.T, root string, anchors workspace.AnchorStore, mirror audit.MirrorStore) (localCompositionTestClients, func()) {
	t.Helper()
	capture := &localMigrationCaptureModule{}
	composition, err := NewLocalComposition(LocalCompositionConfig{
		Info:           buildinfo.Info{Version: "local-restart-integration"},
		Root:           root,
		AttemptAnchors: anchors,
		AuditMirror:    mirror,
		Modules:        []LocalWorkspaceModule{capture},
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
		overview:   tammyv1connect.NewOverviewServiceClient(httpClient, baseURL),
		workspace:  tammyv1connect.NewWorkspaceServiceClient(httpClient, baseURL),
		capture:    capture,
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

func TestLocalCompositionRestartDuringRecoveryPreservesPendingSetup(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	anchors := workspace.NewMemoryAnchorStore()
	mirror := newLocalTestAuditMirror()
	first, stopFirst := startLocalCompositionTestServer(t, root, anchors, mirror)
	t.Cleanup(stopFirst)
	const setupID = "01900f3c-7b2e-7cc4-98c4-dc0c0c0739b1"
	created, groups := first.createWorkspace(t, setupID)
	workspaceID := created.Workspace.Id
	stopFirst()

	second, stopSecond := startLocalCompositionTestServer(t, root, anchors, mirror)
	t.Cleanup(stopSecond)
	// The first core's shutdown must leave the unfinished setup pending, not locked.
	if state := second.workspaceState(t, workspaceID); state != tammyv1.WorkspaceState_WORKSPACE_STATE_PENDING_RECOVERY {
		t.Fatalf("GetWorkspaceState() after restart = %s, want PENDING_RECOVERY", state)
	}
	if second.capture.database != nil {
		t.Fatal("workspace modules were bound before recovery confirmation")
	}
	if err := second.confirmRecovery(setupID, groups); err != nil {
		t.Fatalf("ConfirmRecovery() after restart: %v", err)
	}
	if second.capture.database == nil {
		t.Fatal("ConfirmRecovery() after restart did not bind the workspace modules")
	}
	// No LOCK may sit between creation and confirmation.
	assertLocalAuditTrail(t, second.capture.database, workspaceID,
		"CREATE UNSPECIFIED->PENDING_RECOVERY",
		"RECOVERY_CONFIRMATION PENDING_RECOVERY->UNAUTHENTICATED")
	signedIn, err := second.signIn()
	if err != nil {
		t.Fatalf("SignIn() after pending recovery: %v", err)
	}
	if err := second.readWithSession(workspaceID, signedIn); err != nil {
		t.Fatalf("session after restarted confirmation: %v", err)
	}
	if _, err := mirror.Load(context.Background(), workspaceID); err != nil {
		t.Fatalf("first sign-in did not establish the audit mirror: %v", err)
	}
	stopSecond()

	// Startup normalisation reports any closed workspace as LOCKED, so only an
	// unlock and sign-in prove the third core accepts the restarted audit chain.
	third, stopThird := startLocalCompositionTestServer(t, root, anchors, mirror)
	t.Cleanup(stopThird)
	unlocked, err := third.unlock()
	if err != nil || unlocked.Msg.Workspace == nil || unlocked.Msg.Workspace.Id != workspaceID {
		t.Fatalf("UnlockWorkspace() after normal close = %#v, %v", unlocked, err)
	}
	if _, err := third.signIn(); err != nil {
		t.Fatalf("SignIn() after unlock: %v", err)
	}
	// Once the mirror is established, LOCK records no prior state.
	assertLocalAuditTrail(t, third.capture.database, workspaceID,
		"CREATE UNSPECIFIED->PENDING_RECOVERY",
		"RECOVERY_CONFIRMATION PENDING_RECOVERY->UNAUTHENTICATED",
		"SESSION_STARTED UNAUTHENTICATED->AUTHENTICATED", "USER SIGN_IN",
		"LOCK UNSPECIFIED->LOCKED",
		"UNLOCK LOCKED->UNAUTHENTICATED",
		"SESSION_STARTED UNAUTHENTICATED->AUTHENTICATED", "USER SIGN_IN")
}

// Replaying CreateWorkspace while setup is pending, or ConfirmRecovery after
// sign-in, activates the database that is already bound. Such a replay must
// not bind the modules again or rebuild identity, which would end the session.
func TestLocalCompositionSetupReplaysKeepBindingAndSession(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server, stop := startLocalCompositionTestServer(t, root, workspace.NewMemoryAnchorStore(), newLocalTestAuditMirror())
	t.Cleanup(stop)
	const setupID = "01900f3c-7b2e-7cc4-98c4-dc0c0c0739c1"
	created, groups := server.createWorkspace(t, setupID)
	database := server.capture.database
	if database == nil || server.capture.activations != 1 {
		t.Fatalf("CreateWorkspace() activations = %d, database bound = %t", server.capture.activations, database != nil)
	}
	assertBoundOnce := func(step string) {
		t.Helper()
		if server.capture.activations != 1 || server.capture.database != database {
			t.Fatalf("%s bound the workspace modules again (activations = %d)", step, server.capture.activations)
		}
	}
	replayed, err := server.workspace.CreateWorkspace(context.Background(), localTestRequest(server.capability, localTestCreateRequest(setupID)))
	if err != nil || replayed.Msg.Workspace == nil || replayed.Msg.Workspace.Id != created.Workspace.Id {
		t.Fatalf("CreateWorkspace() replay while pending = %#v, %v", replayed, err)
	}
	assertBoundOnce("CreateWorkspace() replay")
	if err := server.confirmRecovery(setupID, groups); err != nil {
		t.Fatalf("ConfirmRecovery() error = %v", err)
	}
	assertBoundOnce("ConfirmRecovery()")
	signedIn, err := server.signIn()
	if err != nil {
		t.Fatalf("SignIn() error = %v", err)
	}
	if err := server.confirmRecovery(setupID, groups); err != nil {
		t.Fatalf("ConfirmRecovery() replay error = %v", err)
	}
	assertBoundOnce("ConfirmRecovery() replay")
	if err := server.readWithSession(created.Workspace.Id, signedIn); err != nil {
		t.Fatalf("session after setup replays: %v", err)
	}
}

// A restarted confirmation can commit and still fail to bind a module. Its
// replay binds the same database again and must keep the identity service, so a
// session started in between survives.
func TestLocalCompositionActivationRetryKeepsIdentitySessions(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	anchors := workspace.NewMemoryAnchorStore()
	mirror := newLocalTestAuditMirror()
	first, stopFirst := startLocalCompositionTestServer(t, root, anchors, mirror)
	t.Cleanup(stopFirst)
	const setupID = "01900f3c-7b2e-7cc4-98c4-dc0c0c0739d1"
	created, groups := first.createWorkspace(t, setupID)
	stopFirst()

	second, stopSecond := startLocalCompositionTestServer(t, root, anchors, mirror)
	t.Cleanup(stopSecond)
	second.capture.failNext = 1
	if err := second.confirmRecovery(setupID, groups); err == nil {
		t.Fatal("ConfirmRecovery() succeeded although module activation failed")
	}
	if second.capture.database != nil {
		t.Fatal("failed activation bound the workspace modules")
	}
	signedIn, err := second.signIn()
	if err != nil {
		t.Fatalf("SignIn() after failed activation: %v", err)
	}
	if err := second.confirmRecovery(setupID, groups); err != nil {
		t.Fatalf("ConfirmRecovery() replay after failed activation: %v", err)
	}
	if second.capture.database == nil || second.capture.activations != 1 {
		t.Fatalf("replay did not finish binding the workspace modules (activations = %d)", second.capture.activations)
	}
	if err := second.readWithSession(created.Workspace.Id, signedIn); err != nil {
		t.Fatalf("session after activation retry: %v", err)
	}
}

func TestLocalCompositionRejectedAdministratorPasswordLeavesNoDatabaseAndRetrySucceeds(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server, stop := startLocalCompositionTestServer(t, root, workspace.NewMemoryAnchorStore(), newLocalTestAuditMirror())
	t.Cleanup(stop)
	invalid := localTestCreateRequest("01900f3c-7b2e-7cc4-98c4-dc0c0c0739d1")
	invalid.AdministratorPassword = &tammyv1.SecretInput{Utf8: []byte("abcdefghijklmn")}
	if _, err := server.workspace.CreateWorkspace(context.Background(), localTestRequest(server.capability, invalid)); err == nil {
		t.Fatal("CreateWorkspace() accepted a rejected administrator password")
	}
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr == nil && !entry.IsDir() && entry.Name() == "tammy-workspace.db" {
			return errors.New("database left behind at " + path)
		}
		return walkErr
	}); err != nil {
		t.Fatal(err)
	}
	server.createWorkspace(t, "01900f3c-7b2e-7cc4-98c4-dc0c0c0739d2")
}
