package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/buildinfo"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	tammyv1connect "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1/tammyv1connect"
)

type compositionIdentityHandler struct {
	tammyv1connect.UnimplementedIdentityServiceHandler
}

type compositionAuditHandler struct {
	tammyv1connect.UnimplementedAuditServiceHandler
}

type compositionCloser struct {
	calls int
	err   error
}

func (closer *compositionCloser) Close() error {
	closer.calls++
	return closer.err
}

func TestBootCompositionRegistersOnlySystemAndLeavesFutureServicesNotFound(t *testing.T) {
	composition, err := NewBootComposition(buildinfo.Info{Version: "composition-test"})
	if err != nil {
		t.Fatalf("NewBootComposition() error = %v", err)
	}
	server := httptest.NewServer(mustCompositionHandler(t, composition))
	defer server.Close()

	client := tammyv1connect.NewSystemServiceClient(server.Client(), server.URL)
	response, err := client.GetDiagnostics(context.Background(), connect.NewRequest(&tammyv1.GetDiagnosticsRequest{}))
	if err != nil || response.Msg.CoreVersion != "composition-test" {
		t.Fatalf("GetDiagnostics() = %#v, %v", response, err)
	}
	for _, path := range []string{
		tammyv1connect.WorkspaceServiceGetWorkspaceStateProcedure,
		tammyv1connect.IdentityServiceGetSessionProcedure,
		tammyv1connect.OrganisationServiceGetOrganisationProcedure,
		tammyv1connect.AccountingServiceGetAccountProcedure,
		tammyv1connect.OverviewServiceGetAttentionSummaryProcedure,
		tammyv1connect.AuditServiceVerifyChainProcedure,
		"/undeclared.Service/Call",
	} {
		assertHTTPStatus(t, server.Client(), server.URL+path, http.StatusNotFound)
	}
}

func TestWorkspaceCompositionRegistersOnlyCompleteSuppliedGeneratedHandlers(t *testing.T) {
	closer := &compositionCloser{}
	composition, err := NewWorkspaceComposition(WorkspaceCompositionConfig{
		Info:      buildinfo.Info{Version: "workspace-composition-test"},
		Identity:  &compositionIdentityHandler{},
		Audit:     &compositionAuditHandler{},
		Resources: []ResourceCloser{closer},
	})
	if err != nil {
		t.Fatalf("NewWorkspaceComposition() error = %v", err)
	}
	server := httptest.NewServer(mustCompositionHandler(t, composition))
	defer server.Close()

	for _, path := range []string{
		tammyv1connect.SystemServiceGetDiagnosticsProcedure,
		tammyv1connect.IdentityServiceGetSessionProcedure,
		tammyv1connect.AuditServiceVerifyChainProcedure,
	} {
		assertHTTPStatusNot(t, server.Client(), server.URL+path, http.StatusNotFound)
	}
	for _, path := range []string{
		tammyv1connect.WorkspaceServiceGetWorkspaceStateProcedure,
		tammyv1connect.OrganisationServiceGetOrganisationProcedure,
		tammyv1connect.AccountingServiceGetAccountProcedure,
		tammyv1connect.OverviewServiceGetAttentionSummaryProcedure,
	} {
		assertHTTPStatus(t, server.Client(), server.URL+path, http.StatusNotFound)
	}
	if err := composition.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := composition.Close(); err != nil || closer.calls != 1 {
		t.Fatalf("second Close() = %v, calls=%d", err, closer.calls)
	}
}

func TestWorkspaceCompositionCannotOptIntoWorkspaceBeforeConcreteAggregateExists(t *testing.T) {
	configType := reflect.TypeOf(WorkspaceCompositionConfig{})
	if _, exposed := configType.FieldByName("Workspace"); exposed {
		t.Fatal("WorkspaceCompositionConfig exposes a generated handler escape hatch before a concrete aggregate exists")
	}

	closer := &compositionCloser{}
	composition, err := NewWorkspaceComposition(WorkspaceCompositionConfig{
		Info:      buildinfo.Info{Version: "workspace-omission-test"},
		Identity:  &compositionIdentityHandler{},
		Audit:     &compositionAuditHandler{},
		Resources: []ResourceCloser{closer},
	})
	if err != nil {
		t.Fatalf("NewWorkspaceComposition() error = %v", err)
	}
	defer func() { _ = composition.Close() }()
	server := httptest.NewServer(mustCompositionHandler(t, composition))
	defer server.Close()

	assertHTTPStatus(t, server.Client(), server.URL+tammyv1connect.WorkspaceServiceGetWorkspaceStateProcedure, http.StatusNotFound)
}

func TestWorkspaceCompositionFailsClosedOnPartialTypedNilOrUnownedRuntime(t *testing.T) {
	validIdentity := &compositionIdentityHandler{}
	validAudit := &compositionAuditHandler{}
	validCloser := &compositionCloser{}
	var typedNilIdentity *compositionIdentityHandler
	var typedNilAudit *compositionAuditHandler
	var typedNilCloser *compositionCloser
	tests := []WorkspaceCompositionConfig{
		{Info: buildinfo.Info{Version: "test"}, Audit: validAudit, Resources: []ResourceCloser{validCloser}},
		{Info: buildinfo.Info{Version: "test"}, Identity: validIdentity, Resources: []ResourceCloser{validCloser}},
		{Info: buildinfo.Info{Version: "test"}, Identity: typedNilIdentity, Audit: validAudit, Resources: []ResourceCloser{validCloser}},
		{Info: buildinfo.Info{Version: "test"}, Identity: validIdentity, Audit: typedNilAudit, Resources: []ResourceCloser{validCloser}},
		{Info: buildinfo.Info{Version: "test"}, Identity: validIdentity, Audit: validAudit},
		{Info: buildinfo.Info{Version: "test"}, Identity: validIdentity, Audit: validAudit, Resources: []ResourceCloser{typedNilCloser}},
	}
	for index, config := range tests {
		if composition, err := NewWorkspaceComposition(config); composition != nil || !errors.Is(err, ErrComposition) {
			t.Fatalf("case %d = %#v, %v; want ErrComposition", index, composition, err)
		}
	}
}

func mustCompositionHandler(t *testing.T, composition *Composition) http.Handler {
	t.Helper()
	if composition == nil || composition.Registrar() == nil {
		t.Fatal("composition returned no registrar")
	}
	handler, err := composition.Registrar().Handler()
	if err != nil {
		t.Fatalf("registrar.Handler() error = %v", err)
	}
	return handler
}

func assertHTTPStatus(t *testing.T, client *http.Client, url string, want int) {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/proto")
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("POST %s error = %v", url, err)
	}
	defer response.Body.Close()
	if response.StatusCode != want {
		t.Fatalf("POST %s status = %d, want %d", url, response.StatusCode, want)
	}
}

func assertHTTPStatusNot(t *testing.T, client *http.Client, url string, unwanted int) {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/proto")
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("POST %s error = %v", url, err)
	}
	defer response.Body.Close()
	if response.StatusCode == unwanted {
		t.Fatalf("POST %s status = %d, want registered route", url, response.StatusCode)
	}
}
