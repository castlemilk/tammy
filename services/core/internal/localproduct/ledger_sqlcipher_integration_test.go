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
	"google.golang.org/protobuf/proto"
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
	bankingClient := tammyv1connect.NewBankingServiceClient(httpClient, baseURL)
	documentClient := tammyv1connect.NewDocumentServiceClient(httpClient, baseURL)
	taxClient := tammyv1connect.NewTaxServiceClient(httpClient, baseURL)
	overviewClient := tammyv1connect.NewOverviewServiceClient(httpClient, baseURL)

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
	createAccount := func(operationKey, code, name string, accountType tammyv1.AccountType, normal tammyv1.NormalBalance) *tammyv1.Account {
		t.Helper()
		request := connect.NewRequest(&tammyv1.CreateAccountRequest{
			CommandContext: &tammyv1.CommandContext{IdempotencyKey: operationKey, Authentication: authentication},
			OrganisationId: created.Msg.Organisation.Id,
			Code:           code, Name: name, Type: accountType, NormalBalance: normal,
			ReportClassification: "profit_loss.manual", CashFlowClassification: "noncash",
		})
		request.Header().Set(transport.CapabilityHeader, ready.Capability)
		response, createErr := accountingClient.CreateAccount(context.Background(), request)
		if createErr != nil || response.Msg.Account == nil {
			t.Fatalf("CreateAccount(%q) = %#v, %v", code, response, createErr)
		}
		return response.Msg.Account
	}
	expense := createAccount("018f0000-0000-7000-8000-000000000103", "6100", "Office expenses",
		tammyv1.AccountType_ACCOUNT_TYPE_EXPENSE, tammyv1.NormalBalance_NORMAL_BALANCE_DEBIT)
	equity := createAccount("018f0000-0000-7000-8000-000000000104", "3100", "Owner contributions",
		tammyv1.AccountType_ACCOUNT_TYPE_EQUITY, tammyv1.NormalBalance_NORMAL_BALANCE_CREDIT)
	post := connect.NewRequest(&tammyv1.PostManualJournalRequest{
		CommandContext: &tammyv1.CommandContext{IdempotencyKey: "018f0000-0000-7000-8000-000000000105", Authentication: authentication},
		OrganisationId: created.Msg.Organisation.Id,
		PostingDate:    &tammyv1.CivilDate{Year: 2026, Month: 8, Day: 10}, Memo: "Office supplies paid personally",
		Lines: []*tammyv1.ManualJournalLineInput{
			{ClientLineId: "018f0000-0000-7000-8000-000000000106", AccountId: expense.Id,
				Debit: &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 31900}, Credit: &tammyv1.Money{CurrencyCode: "AUD"}, Description: "Office supplies"},
			{ClientLineId: "018f0000-0000-7000-8000-000000000107", AccountId: equity.Id,
				Debit: &tammyv1.Money{CurrencyCode: "AUD"}, Credit: &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 31900}, Description: "Owner contribution"},
		},
	})
	post.Header().Set(transport.CapabilityHeader, ready.Capability)
	posted, err := accountingClient.PostManualJournal(context.Background(), post)
	if err != nil || posted.Msg.Journal == nil || posted.Msg.Journal.TotalDebits.MinorUnits != 31900 {
		t.Fatalf("PostManualJournal() = %#v, %v", posted, err)
	}
	trialRequest := connect.NewRequest(&tammyv1.GetTrialBalanceRequest{
		Authentication: authentication, OrganisationId: created.Msg.Organisation.Id,
		AsOfDate: &tammyv1.CivilDate{Year: 2026, Month: 8, Day: 10},
	})
	trialRequest.Header().Set(transport.CapabilityHeader, ready.Capability)
	trial, err := accountingClient.GetTrialBalance(context.Background(), trialRequest)
	if err != nil || trial.Msg.TotalDebits.MinorUnits != 31900 || trial.Msg.TotalCredits.MinorUnits != 31900 || len(trial.Msg.Lines) != 2 {
		t.Fatalf("GetTrialBalance() = %#v, %v", trial, err)
	}
	getJournal := connect.NewRequest(&tammyv1.GetJournalRequest{Authentication: authentication, JournalId: posted.Msg.Journal.Id})
	getJournal.Header().Set(transport.CapabilityHeader, ready.Capability)
	storedJournal, err := accountingClient.GetJournal(context.Background(), getJournal)
	if err != nil || storedJournal.Msg.Journal == nil || storedJournal.Msg.Journal.Memo != post.Msg.Memo {
		t.Fatalf("GetJournal() = %#v, %v", storedJournal, err)
	}
	listJournals := connect.NewRequest(&tammyv1.ListJournalsRequest{
		Authentication: authentication, OrganisationId: created.Msg.Organisation.Id,
		Page: &tammyv1.PageRequest{PageSize: 50},
	})
	listJournals.Header().Set(transport.CapabilityHeader, ready.Capability)
	journalPage, err := accountingClient.ListJournals(context.Background(), listJournals)
	if err != nil || len(journalPage.Msg.Journals) != 1 || journalPage.Msg.Journals[0].Id != posted.Msg.Journal.Id {
		t.Fatalf("ListJournals() = %#v, %v", journalPage, err)
	}

	invoiceBytes := []byte("%PDF-1.4\nTammy native-text fixture\n%%EOF")
	ingest := connect.NewRequest(&tammyv1.IngestDocumentRequest{
		CommandContext: &tammyv1.CommandContext{
			IdempotencyKey: "018f0000-0000-7000-8000-000000000108",
			Authentication: authentication,
		},
		OrganisationId:    created.Msg.Organisation.Id,
		SourceDisplayName: "officeworks-invoice.pdf",
		MimeType:          "application/pdf",
		Original:          invoiceBytes,
		ExtractedText:     "Officeworks Ltd Invoice INV-029847 Subtotal $290.00 GST $29.00 Total $319.00",
		Candidate: &tammyv1.DocumentCandidate{
			SupplierName:  "Officeworks Ltd",
			InvoiceNumber: "INV-029847",
			DocumentDate:  &tammyv1.CivilDate{Year: 2026, Month: 8, Day: 10},
			Subtotal:      &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 29000},
			Gst:           &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 2900},
			Total:         &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 31900},
		},
	})
	ingest.Header().Set(transport.CapabilityHeader, ready.Capability)
	retained, err := documentClient.IngestDocument(context.Background(), ingest)
	if err != nil || retained.Msg.Document == nil || retained.Msg.Document.Status != tammyv1.DocumentStatus_DOCUMENT_STATUS_NEEDS_REVIEW ||
		retained.Msg.Document.ByteLength != uint64(len(invoiceBytes)) || len(retained.Msg.Document.Sha256) != 32 {
		t.Fatalf("IngestDocument() = %#v, %v", retained, err)
	}
	listDocuments := connect.NewRequest(&tammyv1.ListDocumentsRequest{
		Authentication: authentication,
		OrganisationId: created.Msg.Organisation.Id,
		Page:           &tammyv1.PageRequest{PageSize: 50},
	})
	listDocuments.Header().Set(transport.CapabilityHeader, ready.Capability)
	documentPage, err := documentClient.ListDocuments(context.Background(), listDocuments)
	if err != nil || len(documentPage.Msg.Documents) != 1 || documentPage.Msg.Documents[0].Id != retained.Msg.Document.Id {
		t.Fatalf("ListDocuments() = %#v, %v", documentPage, err)
	}
	review := connect.NewRequest(&tammyv1.SaveDocumentReviewRequest{
		CommandContext: &tammyv1.CommandContext{
			IdempotencyKey: "018f0000-0000-7000-8000-000000000109",
			Authentication: authentication,
		},
		DocumentId:      retained.Msg.Document.Id,
		ExpectedVersion: retained.Msg.Document.Version,
		Candidate:       retained.Msg.Document.Candidate,
	})
	review.Header().Set(transport.CapabilityHeader, ready.Capability)
	reviewed, err := documentClient.SaveDocumentReview(context.Background(), review)
	if err != nil || reviewed.Msg.Document == nil || reviewed.Msg.Document.Status != tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED ||
		reviewed.Msg.Document.Version != 2 || reviewed.Msg.Document.ReviewedAt == nil {
		t.Fatalf("SaveDocumentReview() = %#v, %v", reviewed, err)
	}
	if _, err := documentClient.SaveDocumentReview(context.Background(), review); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("terminal SaveDocumentReview() error = %v; want failed precondition", err)
	}
	getDocument := connect.NewRequest(&tammyv1.GetDocumentRequest{
		Authentication: authentication,
		DocumentId:     retained.Msg.Document.Id,
	})
	getDocument.Header().Set(transport.CapabilityHeader, ready.Capability)
	storedDocument, err := documentClient.GetDocument(context.Background(), getDocument)
	if err != nil || storedDocument.Msg.Document == nil || storedDocument.Msg.Document.Candidate.InvoiceNumber != "INV-029847" ||
		storedDocument.Msg.Document.Status != tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED {
		t.Fatalf("GetDocument() = %#v, %v", storedDocument, err)
	}

	importStatement := connect.NewRequest(&tammyv1.ImportBankStatementRequest{
		CommandContext: &tammyv1.CommandContext{
			IdempotencyKey: "018f0000-0000-7000-8000-00000000010a",
			Authentication: authentication,
		},
		OrganisationId: created.Msg.Organisation.Id,
		OpeningBalance: &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 100000},
		Lines: []*tammyv1.BankStatementLineInput{{
			TransactionDate: &tammyv1.CivilDate{Year: 2026, Month: 8, Day: 10},
			Description:     "Officeworks INV-029847",
			Amount:          &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: -31900},
		}},
	})
	importStatement.Header().Set(transport.CapabilityHeader, ready.Capability)
	imported, err := bankingClient.ImportBankStatement(context.Background(), importStatement)
	if err != nil || imported.Msg.StatementImport == nil {
		t.Fatalf("ImportBankStatement() = %#v, %v", imported, err)
	}
	replayedImport, err := bankingClient.ImportBankStatement(context.Background(), importStatement)
	if err != nil || replayedImport.Msg.StatementImport == nil || replayedImport.Msg.StatementImport.Id != imported.Msg.StatementImport.Id {
		t.Fatalf("exact ImportBankStatement() replay = %#v, %v; want import %s", replayedImport, err, imported.Msg.StatementImport.Id)
	}
	changedImport := connect.NewRequest(proto.Clone(importStatement.Msg).(*tammyv1.ImportBankStatementRequest))
	changedImport.Msg.Lines[0].Description = "Changed same-count line"
	changedImport.Header().Set(transport.CapabilityHeader, ready.Capability)
	if _, err := bankingClient.ImportBankStatement(context.Background(), changedImport); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("changed ImportBankStatement() replay error = %v; want invalid argument", err)
	}
	listBankLines := connect.NewRequest(&tammyv1.ListBankStatementLinesRequest{
		Authentication: authentication,
		OrganisationId: created.Msg.Organisation.Id,
		Page:           &tammyv1.PageRequest{PageSize: 50},
	})
	listBankLines.Header().Set(transport.CapabilityHeader, ready.Capability)
	bankLines, err := bankingClient.ListBankStatementLines(context.Background(), listBankLines)
	if err != nil || len(bankLines.Msg.Lines) != 1 {
		t.Fatalf("ListBankStatementLines() = %#v, %v", bankLines, err)
	}
	matchBankLine := connect.NewRequest(&tammyv1.MatchBankStatementLineRequest{
		CommandContext: &tammyv1.CommandContext{
			IdempotencyKey: "018f0000-0000-7000-8000-00000000010b",
			Authentication: authentication,
		},
		LineId:          bankLines.Msg.Lines[0].Id,
		ExpectedVersion: bankLines.Msg.Lines[0].Version,
		MatchReference:  "Reviewed accounting source",
	})
	matchBankLine.Header().Set(transport.CapabilityHeader, ready.Capability)
	if _, err := bankingClient.MatchBankStatementLine(context.Background(), matchBankLine); err != nil {
		t.Fatalf("MatchBankStatementLine() error = %v", err)
	}
	if _, err := bankingClient.MatchBankStatementLine(context.Background(), matchBankLine); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("terminal MatchBankStatementLine() error = %v; want failed precondition", err)
	}
	reconcile := connect.NewRequest(&tammyv1.CompleteBankReconciliationRequest{
		CommandContext: &tammyv1.CommandContext{
			IdempotencyKey: "018f0000-0000-7000-8000-00000000010c",
			Authentication: authentication,
		},
		OrganisationId: created.Msg.Organisation.Id,
	})
	reconcile.Header().Set(transport.CapabilityHeader, ready.Capability)
	if _, err := bankingClient.CompleteBankReconciliation(context.Background(), reconcile); err != nil {
		t.Fatalf("CompleteBankReconciliation() error = %v", err)
	}
	if _, err := bankingClient.CompleteBankReconciliation(context.Background(), reconcile); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("terminal CompleteBankReconciliation() error = %v; want failed precondition", err)
	}

	createBAS := connect.NewRequest(&tammyv1.CreateBasDraftRequest{
		CommandContext: &tammyv1.CommandContext{
			IdempotencyKey: "018f0000-0000-7000-8000-00000000010d",
			Authentication: authentication,
		},
		OrganisationId: created.Msg.Organisation.Id,
		PeriodStart:    &tammyv1.CivilDate{Year: 2026, Month: 7, Day: 1},
		PeriodEnd:      &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 30},
	})
	createBAS.Header().Set(transport.CapabilityHeader, ready.Capability)
	if _, err := taxClient.CreateBasDraft(context.Background(), createBAS); err != nil {
		t.Fatalf("CreateBasDraft() error = %v", err)
	}

	attentionRequest := connect.NewRequest(&tammyv1.GetAttentionSummaryRequest{
		Authentication: authentication,
		OrganisationId: created.Msg.Organisation.Id,
		AsOfDate:       &tammyv1.CivilDate{Year: 2026, Month: 8, Day: 10},
		ReportingPeriod: &tammyv1.ReportingPeriod{
			StartDate: &tammyv1.CivilDate{Year: 2026, Month: 7, Day: 1},
			EndDate:   &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 30},
		},
	})
	attentionRequest.Header().Set(transport.CapabilityHeader, ready.Capability)
	attention, err := overviewClient.GetAttentionSummary(context.Background(), attentionRequest)
	if err != nil || attention.Msg.Revisions == nil {
		t.Fatalf("GetAttentionSummary() = %#v, %v", attention, err)
	}
	if attention.Msg.Revisions.BankingRevision != 3 || attention.Msg.Revisions.TaxSourceRevision != 3 ||
		attention.Msg.Revisions.FinancialRevision != 7 {
		t.Fatalf("revisions = %#v, want financial=7 banking=3 tax-source=3", attention.Msg.Revisions)
	}
}
