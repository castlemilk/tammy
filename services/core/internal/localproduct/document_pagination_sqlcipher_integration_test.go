//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/app"
	"github.com/tammyapp/tammy/services/core/internal/artefacts"
	"github.com/tammyapp/tammy/services/core/internal/buildinfo"
	"github.com/tammyapp/tammy/services/core/internal/documents"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	tammyv1connect "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1/tammyv1connect"
	"github.com/tammyapp/tammy/services/core/internal/transport"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type documentPageServer struct {
	module                     *LedgerModule
	activation                 app.LocalWorkspaceActivation
	auditMirror                *purchaseTestAuditMirror
	client                     tammyv1connect.DocumentServiceClient
	purchases                  tammyv1connect.PurchaseServiceClient
	invoices                   tammyv1connect.InvoiceServiceClient
	basPreparation             tammyv1connect.BasPreparationServiceClient
	workspace                  tammyv1connect.WorkspaceServiceClient
	identity                   tammyv1connect.IdentityServiceClient
	authentication             *tammyv1.AuthenticationContext
	organisationID, capability string
	stop                       func()
}

type documentPageObservedModule struct {
	*LedgerModule
	activation app.LocalWorkspaceActivation
}

func (module *documentPageObservedModule) Activate(activation app.LocalWorkspaceActivation) error {
	module.activation = activation
	return module.LedgerModule.Activate(activation)
}

func newDocumentPageServer(t *testing.T) documentPageServer {
	return newDocumentPageServerWithBasis(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
}

func newDocumentPageServerWithBasis(t *testing.T, basis tammyv1.GstBasis) documentPageServer {
	return newDocumentPageServerAtRoot(t, basis, t.TempDir(), workspace.NewMemoryAnchorStore(), newPurchaseTestAuditMirror(), true, "")
}

// A retained root has already completed recovery. Reopening it authenticates
// through the real unlock/session path while preserving its existing sources.
func newDocumentPageServerAtRoot(t *testing.T, basis tammyv1.GstBasis, root string, anchors workspace.AnchorStore, mirror *purchaseTestAuditMirror, createWorkspace bool, retainedOrganisation string) documentPageServer {
	t.Helper()
	module, err := NewLedgerModule()
	if err != nil {
		t.Fatal(err)
	}
	observed := &documentPageObservedModule{LedgerModule: module}
	composition, err := app.NewLocalComposition(app.LocalCompositionConfig{
		Info: buildinfo.Info{Version: "document-page-integration"}, Root: root,
		AttemptAnchors: anchors, AuditMirror: mirror, Modules: []app.LocalWorkspaceModule{observed},
	})
	if err != nil {
		t.Fatal(err)
	}
	server, err := transport.NewServer(composition.Registrar(), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = server.Shutdown(ctx)
			_ = composition.Close()
		})
	}
	t.Cleanup(stop)
	ready := server.Ready()
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(ready.CAPEM)) {
		t.Fatal("invalid server CA")
	}
	httpClient := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, RootCAs: roots, ServerName: "127.0.0.1"}}}
	baseURL := fmt.Sprintf("https://127.0.0.1:%d", ready.Port)
	workspaceClient := tammyv1connect.NewWorkspaceServiceClient(httpClient, baseURL)
	identityClient := tammyv1connect.NewIdentityServiceClient(httpClient, baseURL)
	ctx := context.Background()
	setupID := "018f0000-0000-7000-8000-000000000101"
	if createWorkspace {
		created, err := workspaceClient.CreateWorkspace(ctx, brandingRequest(&tammyv1.CreateWorkspaceRequest{
			SetupId: setupID, Destination: &tammyv1.ApprovedFileRef{CapabilityId: app.LocalWorkspaceDirectoryCapability},
			WorkspacePassphrase:   &tammyv1.SecretInput{Utf8: []byte("workspace-passphrase-long-enough")},
			AdministratorUsername: "admin@tammy.local", AdministratorDisplayName: "Tammy Admin",
			AdministratorPassword: &tammyv1.SecretInput{Utf8: []byte("administrator-password-long-enough")},
		}, ready.Capability))
		if err != nil {
			t.Fatal(err)
		}
		groups, err := workspace.ParseRecoveryGroups(created.Msg.RecoverySecret.Utf8)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := workspaceClient.ConfirmRecovery(ctx, brandingRequest(&tammyv1.ConfirmRecoveryRequest{SetupId: setupID,
			Confirmations: []*tammyv1.RecoveryGroupConfirmation{{GroupIndex: 0, Value: groups[0]}, {GroupIndex: 1, Value: groups[1]}},
		}, ready.Capability)); err != nil {
			t.Fatal(err)
		}
	} else {
		if _, err := workspaceClient.UnlockWorkspace(ctx, brandingRequest(&tammyv1.UnlockWorkspaceRequest{
			WorkspaceFile: &tammyv1.ApprovedFileRef{CapabilityId: app.LocalWorkspaceFileCapability},
			Proof:         &tammyv1.WorkspaceUnlockProof{Proof: &tammyv1.WorkspaceUnlockProof_Passphrase{Passphrase: &tammyv1.SecretInput{Utf8: []byte("workspace-passphrase-long-enough")}}},
		}, ready.Capability)); err != nil {
			t.Fatal(err)
		}
	}
	authenticated, err := identityClient.SignIn(ctx, brandingRequest(&tammyv1.SignInRequest{Username: "admin@tammy.local", Password: &tammyv1.SecretInput{Utf8: []byte("administrator-password-long-enough")}}, ready.Capability))
	if err != nil {
		t.Fatal(err)
	}
	authentication := &tammyv1.AuthenticationContext{ActorUserId: authenticated.Msg.User.Id, SessionId: authenticated.Msg.Session.Id}
	organisationID := retainedOrganisation
	if organisationID == "" {
		bundle, err := artefacts.LoadAUGSTV1()
		if err != nil {
			t.Fatal(err)
		}
		organisation, err := tammyv1connect.NewOrganisationServiceClient(httpClient, baseURL).CreateOrganisation(ctx, brandingRequest(&tammyv1.CreateOrganisationRequest{
			CommandContext: &tammyv1.CommandContext{IdempotencyKey: "018f0000-0000-7000-8000-000000000102", Authentication: authentication},
			Abn:            "51824753556", LegalName: "Tammy Business Pty Ltd", DisplayName: "Tammy",
			EntityType: "AU_PRIVATE_COMPANY", GstBasis: basis,
			GstReportingFrequency: tammyv1.GstReportingFrequency_GST_REPORTING_FREQUENCY_QUARTERLY, FinancialYearEndMonth: 6, ActiveTaxRuleBundle: bundle.Source,
		}, ready.Capability))
		if err != nil {
			t.Fatal(err)
		}
		organisationID = organisation.Msg.Organisation.Id
	}
	return documentPageServer{module: module, activation: observed.activation, auditMirror: mirror, client: tammyv1connect.NewDocumentServiceClient(httpClient, baseURL), purchases: tammyv1connect.NewPurchaseServiceClient(httpClient, baseURL), workspace: workspaceClient, identity: identityClient,
		basPreparation: tammyv1connect.NewBasPreparationServiceClient(httpClient, baseURL),
		invoices:       tammyv1connect.NewInvoiceServiceClient(httpClient, baseURL),
		authentication: authentication, organisationID: organisationID, capability: ready.Capability, stop: stop}
}

func (server documentPageServer) seed(t *testing.T, count int, extractedText string) []*tammyv1.Document {
	t.Helper()
	repository, err := documents.NewRepository(server.module.db)
	if err != nil {
		t.Fatal(err)
	}
	retained := make([]*tammyv1.Document, 0, count)
	instant := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for index := 0; index < count; index++ {
		original := []byte(fmt.Sprintf("document-page-original-%d", index))
		digest := sha256.Sum256(original)
		document := &tammyv1.Document{Id: fmt.Sprintf("018f0000-0000-7000-8000-%012x", index+0x1000), OrganisationId: server.organisationID,
			Version: 1, Status: tammyv1.DocumentStatus_DOCUMENT_STATUS_NEEDS_REVIEW, SourceDisplayName: fmt.Sprintf("purchase-%03d.pdf", index),
			MimeType: "application/pdf", ByteLength: uint64(len(original)), Sha256: digest[:], ExtractedText: extractedText,
			Candidate: &tammyv1.DocumentCandidate{SupplierName: "Supplier", Subtotal: &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 100}, Gst: &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 10}, Total: &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 110}},
			CreatedAt: timestamppb.New(instant)}
		saved, err := repository.Create(context.Background(), fmt.Sprintf("018f0000-0000-7000-8000-%012x", index+0x2000), server.authentication.ActorUserId, document, original)
		if err != nil {
			t.Fatal(err)
		}
		retained = append(retained, saved)
	}
	return retained
}

func (server documentPageServer) list(t *testing.T, request *tammyv1.ListDocumentsRequest) *tammyv1.ListDocumentsResponse {
	t.Helper()
	response, err := server.client.ListDocuments(context.Background(), brandingRequest(request, server.capability))
	if err != nil {
		t.Fatal(err)
	}
	if response.Msg.Page == nil || int(response.Msg.Page.ReturnedCount) != len(response.Msg.Documents) {
		t.Fatalf("invalid page info: %v", response.Msg.Page)
	}
	frame, err := anypb.New(response.Msg)
	if err != nil {
		t.Fatal(err)
	}
	if size := proto.Size(frame); size > 4*1024*1024 {
		t.Fatalf("encoded IPC response is %d bytes, exceeds 4 MiB", size)
	}
	return response.Msg
}

func TestDocumentPageThroughRealTLSBindsCursorAndRetainsKeyset(t *testing.T) {
	server := newDocumentPageServer(t)
	retained := server.seed(t, 205, "")
	request := &tammyv1.ListDocumentsRequest{Authentication: server.authentication, OrganisationId: server.organisationID, Page: &tammyv1.PageRequest{PageSize: 75}}
	first := server.list(t, request)
	if len(first.Documents) != 75 || first.Page.GetNextCursor() == "" {
		t.Fatalf("first page count=%d next=%q", len(first.Documents), first.Page.GetNextCursor())
	}
	if first.Documents[0].Id != retained[204].Id || first.Documents[74].Id != retained[130].Id {
		t.Fatal("timestamp ties are not ordered by descending ID")
	}
	// Ingest through the actual authenticated RPC after reading the first page.
	newOriginal := []byte("new document ingested between page reads")
	ingested, err := server.client.IngestDocument(context.Background(), brandingRequest(&tammyv1.IngestDocumentRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: "018f0000-0000-7000-8000-000000009999"},
		OrganisationId: server.organisationID, SourceDisplayName: "new.pdf", MimeType: "application/pdf", Original: newOriginal,
		Candidate: proto.Clone(retained[0].Candidate).(*tammyv1.DocumentCandidate),
	}, server.capability))
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for _, document := range first.Documents {
		seen[document.Id] = true
	}
	lastCursor := first.Page.GetNextCursor()
	for page := 1; ; page++ {
		request.Page.Cursor = &lastCursor
		result := server.list(t, request)
		wantCount := 75
		if page == 2 {
			wantCount = 55
		}
		if len(result.Documents) != wantCount {
			t.Fatalf("page %d count=%d want=%d", page, len(result.Documents), wantCount)
		}
		for _, document := range result.Documents {
			if seen[document.Id] {
				t.Fatalf("duplicate document %s", document.Id)
			}
			seen[document.Id] = true
		}
		if result.Page.GetNextCursor() == "" {
			break
		}
		lastCursor = result.Page.GetNextCursor()
	}
	if len(seen) != 205 || seen[ingested.Msg.Document.Id] {
		t.Fatalf("retained traversal has %d sources or included new ingestion", len(seen))
	}
	request.Page.Cursor = nil
	refreshed := server.list(t, request)
	if refreshed.Documents[0].Id != ingested.Msg.Document.Id {
		t.Fatal("refresh did not discover new ingestion")
	}
	for name, mutate := range map[string]func(*tammyv1.ListDocumentsRequest){
		"foreign organisation": func(v *tammyv1.ListDocumentsRequest) { v.OrganisationId = "018f0000-0000-7000-8000-000000000999" },
		"changed limit":        func(v *tammyv1.ListDocumentsRequest) { v.Page.PageSize = 76 },
		"invalid limit":        func(v *tammyv1.ListDocumentsRequest) { v.Page.PageSize = 201 },
		"changed review filter": func(v *tammyv1.ListDocumentsRequest) {
			status := tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED
			v.Status = &status
		},
		"changed accounting filter": func(v *tammyv1.ListDocumentsRequest) {
			filter := tammyv1.DocumentAccountingFilter_DOCUMENT_ACCOUNTING_FILTER_UNPOSTED
			v.Accounting = &filter
		},
		"forged": func(v *tammyv1.ListDocumentsRequest) {
			forged := "document-page-v1|" + server.organisationID + "|2026-10-01T12:00:00Z|" + retained[130].Id
			v.Page.Cursor = &forged
		},
		"tampered": func(v *tammyv1.ListDocumentsRequest) {
			token := []byte(v.Page.GetCursor())
			if token[10] == 'A' {
				token[10] = 'B'
			} else {
				token[10] = 'A'
			}
			altered := string(token)
			v.Page.Cursor = &altered
		},
	} {
		t.Run(name, func(t *testing.T) {
			changed := proto.Clone(request).(*tammyv1.ListDocumentsRequest)
			cursor := first.Page.GetNextCursor()
			changed.Page.Cursor = &cursor
			mutate(changed)
			if _, err := server.client.ListDocuments(context.Background(), brandingRequest(changed, server.capability)); connect.CodeOf(err) != connect.CodeInvalidArgument {
				t.Fatalf("cursor accepted: %v", err)
			}
		})
	}
	// A new valid session for the same actor must not reuse the old session's cursor.
	signedIn, err := server.identity.SignIn(context.Background(), brandingRequest(&tammyv1.SignInRequest{Username: "admin@tammy.local", Password: &tammyv1.SecretInput{Utf8: []byte("administrator-password-long-enough")}}, server.capability))
	if err != nil {
		t.Fatal(err)
	}
	changed := proto.Clone(request).(*tammyv1.ListDocumentsRequest)
	changed.Authentication = &tammyv1.AuthenticationContext{ActorUserId: signedIn.Msg.User.Id, SessionId: signedIn.Msg.Session.Id}
	cursor := first.Page.GetNextCursor()
	changed.Page.Cursor = &cursor
	if _, err := server.client.ListDocuments(context.Background(), brandingRequest(changed, server.capability)); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("foreign session cursor accepted: %v", err)
	}
	// A separately activated reader can read this organisation, but cannot
	// continue a page retained for another authenticated principal.
	createdUser, err := server.identity.CreateUser(context.Background(), brandingRequest(&tammyv1.CreateUserRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: changed.Authentication, IdempotencyKey: "018f0000-0000-7000-8000-000000009998"},
		Username:       "document-reader@tammy.local", DisplayName: "Document Reader", Roles: []tammyv1.Role{tammyv1.Role_ROLE_AUDITOR},
	}, server.capability))
	if err != nil {
		t.Fatal(err)
	}
	activated, err := server.identity.ActivateUser(context.Background(), brandingRequest(&tammyv1.ActivateUserRequest{
		Username: createdUser.Msg.User.Username, ActivationCode: &tammyv1.SecretInput{Utf8: append([]byte(nil), createdUser.Msg.ActivationCode.Utf8...)},
		NewPassword: &tammyv1.SecretInput{Utf8: []byte("document-reader-password-long-enough")},
	}, server.capability))
	if err != nil {
		t.Fatal(err)
	}
	changed.Authentication = &tammyv1.AuthenticationContext{ActorUserId: activated.Msg.User.Id, SessionId: activated.Msg.Session.Id}
	changed.Page.Cursor = nil
	readerPage := server.list(t, changed)
	if len(readerPage.Documents) != 75 || readerPage.Page.GetNextCursor() == "" {
		t.Fatal("valid different reader could not acquire their own page")
	}
	changed.Page.Cursor = &cursor
	if _, err := server.client.ListDocuments(context.Background(), brandingRequest(changed, server.capability)); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("foreign actor cursor accepted: %v", err)
	}
}

func TestDocumentPageThroughRealTLSFitsIPCEnvelopeWithLargeText(t *testing.T) {
	server := newDocumentPageServer(t)
	server.seed(t, 7, strings.Repeat("界", 1024*1024/3))
	request := &tammyv1.ListDocumentsRequest{Authentication: server.authentication, OrganisationId: server.organisationID, Page: &tammyv1.PageRequest{PageSize: 200}}
	seen := map[string]bool{}
	for {
		page := server.list(t, request)
		if len(page.Documents) == 0 {
			t.Fatal("byte budget stalled on a nonempty retained page")
		}
		for _, document := range page.Documents {
			if seen[document.Id] {
				t.Fatal("duplicated document at byte boundary")
			}
			seen[document.Id] = true
		}
		if page.Page.GetNextCursor() == "" {
			break
		}
		cursor := page.Page.GetNextCursor()
		request.Page.Cursor = &cursor
	}
	if len(seen) != 7 {
		t.Fatalf("large text pagination returned %d documents", len(seen))
	}
}
