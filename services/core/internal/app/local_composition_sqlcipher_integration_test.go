//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package app

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
	"github.com/tammyapp/tammy/services/core/internal/buildinfo"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	tammyv1connect "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1/tammyv1connect"
	"github.com/tammyapp/tammy/services/core/internal/transport"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
)

func TestLocalCompositionCreatesConfirmsAndAuthenticatesRealWorkspace(t *testing.T) {
	composition, err := NewLocalComposition(LocalCompositionConfig{
		Info: buildinfo.Info{Version: "local-integration"},
		Root: t.TempDir(),
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
}
