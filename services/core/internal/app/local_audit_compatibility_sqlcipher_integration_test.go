//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/audit"
	"github.com/tammyapp/tammy/services/core/internal/buildinfo"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	tammyv1connect "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1/tammyv1connect"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testsupport"
	"github.com/tammyapp/tammy/services/core/internal/transport"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
)

func TestLocalCompositionAuthenticatesAndReadsUnauditedLegacyWorkspaceWithoutCreatingTrust(t *testing.T) {
	fixture, err := testsupport.CreateUnauditedLegacyWorkspace(context.Background(), filepath.Clean(t.TempDir()))
	if err != nil {
		t.Fatalf("create genuine pre-audit workspace fixture: %v", err)
	}
	mirror := newLocalTestAuditMirror()
	clients, capture, stop := startLocalAuditCompatibilityServer(t, fixture.Root, fixture.Anchors, mirror)
	t.Cleanup(stop)

	unlockLocalAuditCompatibilityWorkspace(t, clients.workspace, clients.capability, testsupport.LegacyWorkspacePassphrase)
	if capture.database == nil || capture.gate == nil || capture.workspaceID != fixture.WorkspaceID {
		t.Fatal("legacy workspace was not activated with its audit gate")
	}
	if capture.gate.Writable() {
		t.Fatal("unaudited legacy workspace unexpectedly received ordinary audit write coverage")
	}
	if _, err := mirror.Load(context.Background(), fixture.WorkspaceID); err != audit.ErrMirrorMissing {
		t.Fatalf("legacy activation fabricated an external baseline: %v", err)
	}

	signedIn, err := clients.identity.SignIn(context.Background(), localAuditCompatibilitySignIn(clients.capability))
	if err != nil || signedIn == nil || signedIn.Msg.User == nil || signedIn.Msg.Session == nil {
		t.Fatalf("legacy administrator could not authenticate: %#v, %v", signedIn, err)
	}
	read, err := clients.identity.GetCurrentUser(context.Background(), localAuditCompatibilityCurrentUser(clients.capability, signedIn.Msg.User.Id, signedIn.Msg.Session.Id))
	if err != nil || read == nil || read.Msg.User == nil || read.Msg.User.Id != signedIn.Msg.User.Id {
		t.Fatalf("authenticated legacy read failed: %#v, %v", read, err)
	}
	if capture.gate.Writable() {
		t.Fatal("legacy authentication fabricated ordinary audit coverage")
	}
	if _, err := audit.LoadChainHeader(context.Background(), capture.database, fixture.WorkspaceID, 0); err != audit.ErrChainNotFound {
		t.Fatalf("legacy authentication fabricated an historical chain: %v", err)
	}
	var ordinaryEvents, chainHeaders int
	if err := capture.database.QueryRowContext(context.Background(), `SELECT count(*) FROM audit_events_v1`).Scan(&ordinaryEvents); err != nil {
		t.Fatal(err)
	}
	if err := capture.database.QueryRowContext(context.Background(), `SELECT count(*) FROM audit_chain_headers_v1`).Scan(&chainHeaders); err != nil {
		t.Fatal(err)
	}
	if ordinaryEvents != 0 || chainHeaders != 0 {
		t.Fatalf("legacy flow fabricated historical audit rows: events=%d headers=%d", ordinaryEvents, chainHeaders)
	}
	if _, err := mirror.Load(context.Background(), fixture.WorkspaceID); err != audit.ErrMirrorMissing {
		t.Fatalf("legacy authentication fabricated an external baseline: %v", err)
	}
}

func TestMovedAuditedWorkspaceCanAuthenticateAndLockWithoutEstablishingTrust(t *testing.T) {
	root := filepath.Clean(t.TempDir())
	anchors := workspace.NewMemoryAnchorStore()
	firstMirror := newLocalTestAuditMirror()
	first, _, stopFirst := startLocalAuditCompatibilityServer(t, root, anchors, firstMirror)
	createRequest := connect.NewRequest(&tammyv1.CreateWorkspaceRequest{
		SetupId:               "01900f3c-7b2e-7cc4-98c4-dc0c0c070002",
		Destination:           &tammyv1.ApprovedFileRef{CapabilityId: LocalWorkspaceDirectoryCapability},
		WorkspacePassphrase:   &tammyv1.SecretInput{Utf8: []byte("workspace-passphrase-long-enough")},
		AdministratorUsername: "admin@tammy.local", AdministratorDisplayName: "Tammy Admin",
		AdministratorPassword: &tammyv1.SecretInput{Utf8: []byte("administrator-password-long-enough")},
	})
	createRequest.Header().Set(transport.CapabilityHeader, first.capability)
	created, err := first.workspace.CreateWorkspace(context.Background(), createRequest)
	if err != nil || created.Msg.Workspace == nil || created.Msg.RecoverySecret == nil {
		t.Fatalf("create audited workspace: %#v, %v", created, err)
	}
	groups, err := workspace.ParseRecoveryGroups(created.Msg.RecoverySecret.Utf8)
	if err != nil {
		t.Fatal(err)
	}
	confirm := connect.NewRequest(&tammyv1.ConfirmRecoveryRequest{SetupId: createRequest.Msg.SetupId,
		Confirmations: []*tammyv1.RecoveryGroupConfirmation{{GroupIndex: 0, Value: groups[0]}, {GroupIndex: 1, Value: groups[1]}},
	})
	confirm.Header().Set(transport.CapabilityHeader, first.capability)
	if _, err := first.workspace.ConfirmRecovery(context.Background(), confirm); err != nil {
		t.Fatalf("confirm recovery: %v", err)
	}
	signedIn, err := first.identity.SignIn(context.Background(), localAuditCompatibilitySignIn(first.capability))
	if err != nil || signedIn == nil || signedIn.Msg.User == nil || signedIn.Msg.Session == nil {
		t.Fatalf("initial administrator sign-in: %#v, %v", signedIn, err)
	}
	stopFirst()
	if _, err := firstMirror.Load(context.Background(), created.Msg.Workspace.Id); err != nil {
		t.Fatalf("original installation did not establish its baseline: %v", err)
	}

	// The workspace file and authenticated installation header remain intact;
	// the new installation simply has no local baseline or creation marker.
	movedMirror := newLocalTestAuditMirror()
	moved, capture, stopMoved := startLocalAuditCompatibilityServer(t, root, anchors, movedMirror)
	t.Cleanup(stopMoved)
	unlockLocalAuditCompatibilityWorkspace(t, moved.workspace, moved.capability, "workspace-passphrase-long-enough")
	if capture.gate == nil || capture.gate.Writable() || !capture.gate.EvidenceExportAllowed() {
		t.Fatalf("moved audited workspace trust state: gate=%v evidence=%v", capture.gate != nil && capture.gate.Writable(), capture.gate != nil && capture.gate.EvidenceExportAllowed())
	}
	header, err := audit.LoadChainHeader(context.Background(), capture.database, created.Msg.Workspace.Id, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := movedMirror.Load(context.Background(), created.Msg.Workspace.Id); err != audit.ErrMirrorMissing {
		t.Fatalf("moved workspace activation fabricated a local baseline: %v", err)
	}

	movedSignIn, err := moved.identity.SignIn(context.Background(), localAuditCompatibilitySignIn(moved.capability))
	if err != nil || movedSignIn == nil || movedSignIn.Msg.User == nil || movedSignIn.Msg.Session == nil {
		t.Fatalf("moved workspace authentication failed: %#v, %v", movedSignIn, err)
	}
	read, err := moved.identity.GetCurrentUser(context.Background(), localAuditCompatibilityCurrentUser(moved.capability,
		movedSignIn.Msg.User.Id, movedSignIn.Msg.Session.Id))
	if err != nil || read == nil || read.Msg.User == nil || read.Msg.User.Id != movedSignIn.Msg.User.Id {
		t.Fatalf("moved workspace authenticated read failed: %#v, %v", read, err)
	}
	if _, err := moved.workspace.LockWorkspace(context.Background(), localAuditCompatibilityLock(moved.capability)); err != nil {
		t.Fatalf("moved workspace security lock failed: %v", err)
	}
	unlockLocalAuditCompatibilityWorkspace(t, moved.workspace, moved.capability, "workspace-passphrase-long-enough")
	if capture.gate.Writable() || !capture.gate.EvidenceExportAllowed() {
		t.Fatal("security LOCK/UNLOCK established ordinary write trust for a moved workspace")
	}
	after, err := audit.LoadChainHeader(context.Background(), capture.database, created.Msg.Workspace.Id, 0)
	if err != nil || after.CurrentSequence != header.CurrentSequence || after.CurrentHead != header.CurrentHead {
		t.Fatalf("moved security transitions changed the untrusted historical chain: before=%+v after=%+v err=%v", header, after, err)
	}
	if _, err := movedMirror.Load(context.Background(), created.Msg.Workspace.Id); err != audit.ErrMirrorMissing {
		t.Fatalf("security LOCK/UNLOCK fabricated a moved-workspace baseline: %v", err)
	}
}

type localAuditCompatibilityCapture struct {
	database    *sqlcipher.Database
	workspaceID string
	gate        *audit.WriteGate
}

func (capture *localAuditCompatibilityCapture) HandlerFactories() []transport.GeneratedHandlerFactory {
	return []transport.GeneratedHandlerFactory{func(...connect.HandlerOption) (string, http.Handler) {
		return "/tammy.v1.MigrationCaptureService/", http.NotFoundHandler()
	}}
}

func (capture *localAuditCompatibilityCapture) Activate(activation LocalWorkspaceActivation) error {
	capture.database = activation.Database
	capture.workspaceID = activation.WorkspaceID
	capture.gate = activation.AuditGate
	return nil
}

func startLocalAuditCompatibilityServer(t *testing.T, root string, anchors workspace.AnchorStore, mirror audit.MirrorStore) (localCompositionTestClients, *localAuditCompatibilityCapture, func()) {
	t.Helper()
	capture := &localAuditCompatibilityCapture{}
	composition, err := NewLocalComposition(LocalCompositionConfig{Info: buildinfo.Info{Version: "audit-compatibility-integration"},
		Root: root, AttemptAnchors: anchors, AuditMirror: mirror, Modules: []LocalWorkspaceModule{capture}})
	if err != nil {
		t.Fatalf("NewLocalComposition(): %v", err)
	}
	server, err := transport.NewServer(composition.Registrar(), io.Discard)
	if err != nil {
		_ = composition.Close()
		t.Fatalf("transport.NewServer(): %v", err)
	}
	if err := server.Start(); err != nil {
		_ = composition.Close()
		t.Fatalf("server.Start(): %v", err)
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
	clients := localCompositionTestClients{capability: ready.Capability,
		identity:  tammyv1connect.NewIdentityServiceClient(httpClient, baseURL),
		workspace: tammyv1connect.NewWorkspaceServiceClient(httpClient, baseURL)}
	var once sync.Once
	stop := func() {
		once.Do(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = server.Shutdown(ctx)
			_ = composition.Close()
		})
	}
	return clients, capture, stop
}

func unlockLocalAuditCompatibilityWorkspace(t *testing.T, client tammyv1connect.WorkspaceServiceClient, capability, passphrase string) {
	t.Helper()
	request := connect.NewRequest(&tammyv1.UnlockWorkspaceRequest{
		WorkspaceFile: &tammyv1.ApprovedFileRef{CapabilityId: LocalWorkspaceFileCapability},
		Proof:         &tammyv1.WorkspaceUnlockProof{Proof: &tammyv1.WorkspaceUnlockProof_Passphrase{Passphrase: &tammyv1.SecretInput{Utf8: []byte(passphrase)}}},
	})
	request.Header().Set(transport.CapabilityHeader, capability)
	if _, err := client.UnlockWorkspace(context.Background(), request); err != nil {
		t.Fatalf("UnlockWorkspace(): %v", err)
	}
}

func localAuditCompatibilitySignIn(capability string) *connect.Request[tammyv1.SignInRequest] {
	request := connect.NewRequest(&tammyv1.SignInRequest{Username: "admin@tammy.local",
		Password: &tammyv1.SecretInput{Utf8: []byte("administrator-password-long-enough")}})
	request.Header().Set(transport.CapabilityHeader, capability)
	return request
}

func localAuditCompatibilityCurrentUser(capability, userID, sessionID string) *connect.Request[tammyv1.GetCurrentUserRequest] {
	request := connect.NewRequest(&tammyv1.GetCurrentUserRequest{Authentication: &tammyv1.AuthenticationContext{ActorUserId: userID, SessionId: sessionID}})
	request.Header().Set(transport.CapabilityHeader, capability)
	return request
}

func localAuditCompatibilityLock(capability string) *connect.Request[tammyv1.LockWorkspaceRequest] {
	request := connect.NewRequest(&tammyv1.LockWorkspaceRequest{})
	request.Header().Set(transport.CapabilityHeader, capability)
	return request
}
