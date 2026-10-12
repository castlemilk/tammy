//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/app"
	"github.com/tammyapp/tammy/services/core/internal/artefacts"
	"github.com/tammyapp/tammy/services/core/internal/audit"
	"github.com/tammyapp/tammy/services/core/internal/buildinfo"
	"github.com/tammyapp/tammy/services/core/internal/documents"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1/tammyv1connect"
	"github.com/tammyapp/tammy/services/core/internal/localproduct"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/migrations"
	"github.com/tammyapp/tammy/services/core/internal/transport"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type coverageAuditObservedModule struct {
	*localproduct.LedgerModule
	activation app.LocalWorkspaceActivation
}

func (module *coverageAuditObservedModule) Activate(activation app.LocalWorkspaceActivation) error {
	module.activation = activation
	return module.LedgerModule.Activate(activation)
}

type coverageAuditMirror struct {
	mu         sync.Mutex
	baselines  map[string]*tammyv1.AuditMirrorBaseline
	lifecycles map[string]*audit.InitialMirrorLifecycle
}

func newCoverageAuditMirror() *coverageAuditMirror {
	return &coverageAuditMirror{baselines: map[string]*tammyv1.AuditMirrorBaseline{}, lifecycles: map[string]*audit.InitialMirrorLifecycle{}}
}

func (store *coverageAuditMirror) Load(_ context.Context, workspaceID string) (*tammyv1.AuditMirrorBaseline, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	value := store.baselines[workspaceID]
	if value == nil {
		return nil, audit.ErrMirrorMissing
	}
	return proto.Clone(value).(*tammyv1.AuditMirrorBaseline), nil
}

func (store *coverageAuditMirror) CompareAndSwap(_ context.Context, expected, target *tammyv1.AuditMirrorBaseline) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if !proto.Equal(store.baselines[target.WorkspaceId], expected) {
		return audit.ErrMirrorConflict
	}
	store.baselines[target.WorkspaceId] = proto.Clone(target).(*tammyv1.AuditMirrorBaseline)
	return nil
}

func (store *coverageAuditMirror) LoadInitialMirrorLifecycle(_ context.Context, workspaceID string) (*audit.InitialMirrorLifecycle, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	value := store.lifecycles[workspaceID]
	if value == nil {
		return nil, audit.ErrMirrorMissing
	}
	copy := *value
	return &copy, nil
}

func (store *coverageAuditMirror) SaveInitialMirrorLifecycle(_ context.Context, value *audit.InitialMirrorLifecycle) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	copy := *value
	store.lifecycles[value.WorkspaceID] = &copy
	return nil
}

type coverageAuditServer struct {
	activation     app.LocalWorkspaceActivation
	purchases      tammyv1connect.PurchaseServiceClient
	authentication *tammyv1.AuthenticationContext
	organisationID string
	capability     string
}

func coverageAuditRequest[T any](value *T, capability string) *connect.Request[T] {
	request := connect.NewRequest(value)
	request.Header().Set(transport.CapabilityHeader, capability)
	return request
}

func newCoverageAuditServer(t *testing.T) coverageAuditServer {
	t.Helper()
	module, err := localproduct.NewLedgerModule()
	if err != nil {
		t.Fatal(err)
	}
	observed := &coverageAuditObservedModule{LedgerModule: module}
	composition, err := app.NewLocalComposition(app.LocalCompositionConfig{
		Info:           buildinfo.Info{Version: "purchase-coverage-audit-integration"},
		Root:           t.TempDir(),
		AttemptAnchors: workspace.NewMemoryAnchorStore(),
		AuditMirror:    newCoverageAuditMirror(),
		Modules:        []app.LocalWorkspaceModule{observed},
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
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		_ = composition.Close()
	})
	ready := server.Ready()
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(ready.CAPEM)) {
		t.Fatal("invalid local server CA")
	}
	httpClient := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, RootCAs: roots, ServerName: "127.0.0.1",
	}}}
	baseURL := fmt.Sprintf("https://127.0.0.1:%d", ready.Port)
	workspaceClient := tammyv1connect.NewWorkspaceServiceClient(httpClient, baseURL)
	identityClient := tammyv1connect.NewIdentityServiceClient(httpClient, baseURL)
	ctx := context.Background()
	setupID := "018f0000-0000-7000-8000-000000009001"
	created, err := workspaceClient.CreateWorkspace(ctx, coverageAuditRequest(&tammyv1.CreateWorkspaceRequest{
		SetupId: setupID, Destination: &tammyv1.ApprovedFileRef{CapabilityId: app.LocalWorkspaceDirectoryCapability},
		WorkspacePassphrase:   &tammyv1.SecretInput{Utf8: []byte("workspace-passphrase-long-enough")},
		AdministratorUsername: "coverage-admin@tammy.local", AdministratorDisplayName: "Coverage Admin",
		AdministratorPassword: &tammyv1.SecretInput{Utf8: []byte("administrator-password-long-enough")},
	}, ready.Capability))
	if err != nil {
		t.Fatal(err)
	}
	groups, err := workspace.ParseRecoveryGroups(created.Msg.RecoverySecret.Utf8)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workspaceClient.ConfirmRecovery(ctx, coverageAuditRequest(&tammyv1.ConfirmRecoveryRequest{
		SetupId: setupID, Confirmations: []*tammyv1.RecoveryGroupConfirmation{
			{GroupIndex: 0, Value: groups[0]}, {GroupIndex: 1, Value: groups[1]},
		},
	}, ready.Capability)); err != nil {
		t.Fatal(err)
	}
	if observed.activation.Database == nil || observed.activation.AuditAppender == nil || observed.activation.AuditGate == nil {
		t.Fatal("workspace activation did not install the real encrypted database and audit writer")
	}
	steps, err := migrations.All()
	if err != nil || len(steps) == 0 {
		t.Fatalf("load current application migrations: %v", err)
	}
	expectedVersion := int64(steps[len(steps)-1].Version)
	if version := postScalar(t, observed.activation.Database, `SELECT COALESCE(MAX(version),0) FROM schema_migrations`); version != expectedVersion {
		t.Fatalf("purchase audit fixture schema version=%d, want current native version %d", version, expectedVersion)
	}
	signedIn, err := identityClient.SignIn(ctx, coverageAuditRequest(&tammyv1.SignInRequest{
		Username: "coverage-admin@tammy.local", Password: &tammyv1.SecretInput{Utf8: []byte("administrator-password-long-enough")},
	}, ready.Capability))
	if err != nil {
		t.Fatal(err)
	}
	authentication := &tammyv1.AuthenticationContext{ActorUserId: signedIn.Msg.User.Id, SessionId: signedIn.Msg.Session.Id}
	bundle, err := artefacts.LoadAUGSTV1()
	if err != nil {
		t.Fatal(err)
	}
	organisation, err := tammyv1connect.NewOrganisationServiceClient(httpClient, baseURL).CreateOrganisation(ctx, coverageAuditRequest(&tammyv1.CreateOrganisationRequest{
		CommandContext: &tammyv1.CommandContext{IdempotencyKey: "018f0000-0000-7000-8000-000000009002", Authentication: authentication},
		Abn:            "51824753556", LegalName: "Coverage Audit Pty Ltd", DisplayName: "Coverage Audit", EntityType: "AU_PRIVATE_COMPANY",
		GstBasis:              tammyv1.GstBasis_GST_BASIS_NON_CASH,
		GstReportingFrequency: tammyv1.GstReportingFrequency_GST_REPORTING_FREQUENCY_QUARTERLY,
		FinancialYearEndMonth: 6, ActiveTaxRuleBundle: bundle.Source,
	}, ready.Capability))
	if err != nil {
		t.Fatal(err)
	}
	return coverageAuditServer{activation: observed.activation, purchases: tammyv1connect.NewPurchaseServiceClient(httpClient, baseURL),
		authentication: authentication, organisationID: organisation.Msg.Organisation.Id, capability: ready.Capability}
}

func (server coverageAuditServer) reviewedDocument(t *testing.T) *tammyv1.Document {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	original := []byte("real reviewed purchase invoice used by the BAS coverage audit test")
	digest := sha256.Sum256(original)
	candidate := &tammyv1.DocumentCandidate{SupplierName: "Coverage Supplier", InvoiceNumber: "COVERAGE-AUDIT-1",
		DocumentDate: &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 1}, Subtotal: postMoney(10000), Gst: postMoney(1000), Total: postMoney(11000)}
	repository, err := documents.NewRepository(server.activation.Database)
	if err != nil {
		t.Fatal(err)
	}
	document, err := repository.Create(ctx, "018f0000-0000-7000-8000-000000009003", server.authentication.ActorUserId,
		&tammyv1.Document{Id: "018f0000-0000-7000-8000-000000009004", OrganisationId: server.organisationID, Version: 1,
			Status: tammyv1.DocumentStatus_DOCUMENT_STATUS_NEEDS_REVIEW, SourceDisplayName: "coverage-invoice.pdf", MimeType: "application/pdf",
			ByteLength: uint64(len(original)), Sha256: digest[:], Candidate: candidate, CreatedAt: timestamppb.New(now)}, original)
	if err != nil {
		t.Fatal(err)
	}
	reviewed, err := repository.SaveReview(ctx, document.Id, document.Version, candidate, now)
	if err != nil {
		t.Fatal(err)
	}
	return reviewed
}

func (server coverageAuditServer) postRequest(t *testing.T, document *tammyv1.Document) *tammyv1.PostReviewedPurchaseRequest {
	t.Helper()
	accountID := "018f0000-0000-7000-8000-000000009005"
	accounts, err := accounting.NewAccountRepository(server.activation.Database)
	if err != nil {
		t.Fatal(err)
	}
	if err := accounts.Create(context.Background(), &tammyv1.Account{Id: accountID, OrganisationId: server.organisationID, Version: 1,
		Code: "6101", Name: "Coverage purchase cost", Type: tammyv1.AccountType_ACCOUNT_TYPE_EXPENSE,
		NormalBalance: tammyv1.NormalBalance_NORMAL_BALANCE_DEBIT, Status: tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE,
		Designation: tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY, ReportClassification: "custom.account",
		CashFlowClassification: "operating"}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	return &tammyv1.PostReviewedPurchaseRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: "018f0000-0000-7000-8000-000000009006"},
		OrganisationId: server.organisationID, DocumentId: document.Id, ExpectedReviewedVersion: document.Version,
		Allocations: []*tammyv1.PurchaseAllocationInput{{Sequence: 1, AccountId: accountID, AccountVersion: 1,
			Gross: proto.Clone(document.Candidate.Total).(*tammyv1.Money), SupplierGst: proto.Clone(document.Candidate.Gst).(*tammyv1.Money),
			EligibleGst: proto.Clone(document.Candidate.Gst).(*tammyv1.Money), Use: tammyv1.PurchaseUse_PURCHASE_USE_BUSINESS,
			BasClassification:          tammyv1.PurchaseBasClassification_PURCHASE_BAS_CLASSIFICATION_NON_CAPITAL,
			CashFlowClassification:     tammyv1.PurchaseCashFlowClassification_PURCHASE_CASH_FLOW_CLASSIFICATION_OPERATING,
			EligibilityReviewConfirmed: true, EvidenceReviewConfirmed: true}},
		Evidence: &tammyv1.PurchaseEvidenceInput{Source: &tammyv1.PurchaseRetainedSourceRef{SourceId: document.Id,
			SourceVersion: document.Version, SourceHash: append([]byte(nil), document.Sha256...)},
			Classification: tammyv1.PurchaseEvidenceClassification_PURCHASE_EVIDENCE_CLASSIFICATION_TAX_INVOICE,
			HeldDate:       proto.Clone(document.Candidate.DocumentDate).(*tammyv1.CivilDate), RegistrationReviewConfirmed: true,
			BusinessUseReviewConfirmed: true, EvidenceReviewConfirmed: true, Reason: "Invoice checked against the retained original"},
		DuplicateReview: &tammyv1.PurchaseDuplicateReview{Decision: tammyv1.PurchaseDuplicateDecision_PURCHASE_DUPLICATE_DECISION_NO_MATCH,
			Reason: "No matching source found"},
		PriorAccountingReview: &tammyv1.PurchasePriorAccountingReview{
			Decision:                            tammyv1.PurchasePriorAccountingDecision_PURCHASE_PRIOR_ACCOUNTING_DECISION_NOT_PREVIOUSLY_ACCOUNTED_OR_CLAIMED,
			ReviewedExistingAccountingConfirmed: true, Reason: "Existing accounting and claim history reviewed"},
	}
}

func purchaseAuditRetentionState(t *testing.T, executor app.CommandSQLExecutor) map[string]any {
	t.Helper()
	state := reportingStateCounts(t, executor)
	for _, query := range []string{
		`SELECT count(*) FROM purchase_command_audit_v1`,
		`SELECT count(*) FROM audit_events_v1`,
		`SELECT count(*) FROM command_idempotency_v1`,
		`SELECT count(*) FROM purchase_accounting_events_v1`,
	} {
		state[query] = postScalar(t, executor, query)
	}
	return state
}

type purchaseCoverageAuditProbe struct {
	app.CommandSQLExecutor
	bodyReads int
}

func (probe *purchaseCoverageAuditProbe) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	rows, err := probe.CommandSQLExecutor.QueryContext(ctx, query, args...)
	if err != nil || rows == nil {
		return rows, err
	}
	columns, err := rows.Columns()
	if err == nil {
		for _, column := range columns {
			name := strings.ToLower(strings.TrimSpace(column))
			if name == "posting_proto" || name == "payment_proto" || name == "journal_proto" || name == "document_proto" ||
				name == "candidate_proto" || name == "review_proto" || name == "response_proto" || name == "result_proto" ||
				name == "outcome_proto" || name == "event_proto" || name == "claim_proto" || name == "evidence_proto" ||
				name == "allocation_proto" || name == "attribution_proto" || name == "deferral_proto" || name == "portion_proto" ||
				name == "payload_proto" || name == "retained_input_proto" || name == "source_proto" || name == "original_bytes" ||
				name == "extracted_text" || strings.HasSuffix(name, "_proto") || strings.HasSuffix(name, "_original_bytes") || strings.HasSuffix(name, "_extracted_text") {
				probe.bodyReads++
				break
			}
		}
	}
	return rows, err
}

func purchaseAuditMoneySnapshot(t *testing.T, executor app.CommandSQLExecutor, organisationID string) [3]int64 {
	t.Helper()
	rows, err := executor.QueryContext(context.Background(), `SELECT count(*),COALESCE(sum(l.debit_minor),0),COALESCE(sum(l.credit_minor),0)
		FROM journal_lines l JOIN journals j ON j.id=l.journal_id WHERE j.organisation_id=?`, organisationID)
	if err != nil {
		t.Fatal(err)
	}
	var result [3]int64
	if !rows.Next() || rows.Scan(&result[0], &result[1], &result[2]) != nil || rows.Next() || rows.Err() != nil {
		_ = rows.Close()
		t.Fatal("read purchase journal money snapshot")
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	return result
}

func purchaseAuditCapturedRow(t *testing.T, capture *purchases.PurchaseCoverageReportingCapture, operationKey string) purchases.PurchaseReportingRetainedRow {
	t.Helper()
	for _, row := range capture.Rows {
		if row.Table != "purchase_command_audit_v1" {
			continue
		}
		value, ok := retainedValue(row, "operation_key")
		if ok && value.Kind == "TEXT" && value.Text == operationKey {
			return row
		}
	}
	t.Fatalf("coverage capture omitted purchase audit operation %s", operationKey)
	return purchases.PurchaseReportingRetainedRow{}
}

func assertPurchaseAuditRow(t *testing.T, executor app.CommandSQLExecutor, row purchases.PurchaseReportingRetainedRow, operationKey, organisationID, workspaceID, actorID, sessionID, commandType string, result proto.Message, payload proto.Message, occurredAfter time.Time) int64 {
	t.Helper()
	wantColumns := []string{"operation_key", "organisation_id", "workspace_id", "actor_user_id", "session_id", "command_type", "result_sha256", "payload_proto", "occurred_at"}
	if row.Table != "purchase_command_audit_v1" || !reflect.DeepEqual(row.Columns, wantColumns) || len(row.Values) != len(wantColumns) {
		t.Fatalf("purchase audit row schema=%v values=%d", row.Columns, len(row.Values))
	}
	textColumns := map[string]string{"operation_key": operationKey, "organisation_id": organisationID, "workspace_id": workspaceID,
		"actor_user_id": actorID, "session_id": sessionID, "command_type": commandType}
	for column, expected := range textColumns {
		value, ok := retainedValue(row, column)
		if !ok || value.Kind != "TEXT" || value.Text != expected {
			t.Fatalf("purchase audit %s=%+v, want %q", column, value, expected)
		}
	}
	encodedResult, err := proto.MarshalOptions{Deterministic: true}.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	resultDigest := sha256.Sum256(encodedResult)
	hash, ok := retainedValue(row, "result_sha256")
	if !ok || hash.Kind != "BLOB" || !bytes.Equal(hash.Bytes, resultDigest[:]) {
		t.Fatal("purchase audit result hash does not commit the actual public command response")
	}
	encodedPayload, err := proto.MarshalOptions{Deterministic: true}.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	retainedPayload, ok := retainedValue(row, "payload_proto")
	if !ok || retainedPayload.Kind != "BLOB" || !bytes.Equal(retainedPayload.Bytes, encodedPayload) {
		t.Fatal("purchase audit typed payload does not match the actual public command source")
	}
	occurredAt, ok := retainedValue(row, "occurred_at")
	if !ok || occurredAt.Kind != "TEXT" {
		t.Fatalf("purchase audit time has invalid SQL kind: %+v", occurredAt)
	}
	actualRows, err := executor.QueryContext(context.Background(), `SELECT occurred_at FROM purchase_command_audit_v1 WHERE operation_key=?`, operationKey)
	if err != nil {
		t.Fatal(err)
	}
	var actualOccurredAt string
	if !actualRows.Next() || actualRows.Scan(&actualOccurredAt) != nil || actualRows.Next() || actualRows.Err() != nil {
		_ = actualRows.Close()
		t.Fatal("read exact stored purchase audit timestamp")
	}
	if err := actualRows.Close(); err != nil {
		t.Fatal(err)
	}
	if occurredAt.Text != actualOccurredAt {
		t.Fatalf("capture changed occurred_at from %q to %q", actualOccurredAt, occurredAt.Text)
	}
	parsed, err := time.Parse(time.RFC3339Nano, occurredAt.Text)
	if err != nil || parsed.Before(occurredAfter) || parsed.After(time.Now().UTC().Add(time.Minute)) {
		t.Fatalf("purchase audit time=%q, parse error=%v", occurredAt.Text, err)
	}
	return int64(len(retainedPayload.Bytes))
}

func assertOrdinaryPurchaseAuditWritten(t *testing.T, executor app.CommandSQLExecutor, workspaceID string, authentication *tammyv1.AuthenticationContext, organisationID, operationKey string, result proto.Message, action tammyv1.PurchaseAuditAction) {
	t.Helper()
	ctx := context.Background()
	rows, err := executor.QueryContext(ctx, `SELECT workspace_id,generation,sequence FROM audit_events_v1 WHERE idempotency_key=? AND event_type=?`, operationKey, tammyv1.AuditEventType_AUDIT_EVENT_TYPE_PURCHASE_ACCOUNTING_CHANGED)
	if err != nil {
		t.Fatal(err)
	}
	var storedWorkspace string
	var generation, sequence uint64
	if !rows.Next() || rows.Scan(&storedWorkspace, &generation, &sequence) != nil || rows.Next() || rows.Err() != nil {
		_ = rows.Close()
		t.Fatalf("purchase command %s has no unique ordinary purchase audit event", operationKey)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if storedWorkspace != workspaceID {
		t.Fatalf("ordinary audit workspace=%s, want %s", storedWorkspace, workspaceID)
	}
	stored, err := audit.LoadStoredEvents(ctx, executor, workspaceID, generation, sequence, sequence)
	if err != nil || len(stored) != 1 {
		t.Fatalf("load ordinary purchase audit event: stored=%d err=%v", len(stored), err)
	}
	event := stored[0].Event
	body := event.GetPayload().GetPurchaseAccountingChanged()
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	if body == nil || body.Action != action || event.GetOrganisationId() != organisationID || !proto.Equal(event.Actor, authentication) ||
		event.GetIdempotencyKey() != operationKey || event.GetCommandId() != operationKey || event.Result == nil ||
		event.Result.TypeName != string(result.ProtoReflect().Descriptor().FullName()) || !bytes.Equal(event.Result.DeterministicSha256, digest[:]) {
		t.Fatalf("ordinary audit does not commit actual purchase command: %v", event)
	}
}

func TestPurchaseCoverageRetainsActualImmutableCommandAuditWithinSharedBudget(t *testing.T) {
	ctx := context.Background()
	started := time.Now().UTC()
	server := newCoverageAuditServer(t)
	document := server.reviewedDocument(t)
	request := server.postRequest(t, document)
	posted, err := server.purchases.PostReviewedPurchase(ctx, coverageAuditRequest(request, server.capability))
	if err != nil || posted.Msg == nil || posted.Msg.Posting == nil || !posted.Msg.Accounting.GetPosted() {
		t.Fatalf("public reviewed purchase command: response=%v err=%v", posted, err)
	}

	repository, err := purchases.NewRepository(server.activation.Database, server.activation.Now, server.activation.NewID)
	if err != nil {
		t.Fatal(err)
	}
	beforeState := purchaseAuditRetentionState(t, server.activation.Database)
	beforeCapture, err := repository.CaptureCoverageForBAS(ctx, server.organisationID)
	if err != nil || beforeCapture == nil {
		t.Fatalf("capture after actual purchase audit append: capture=%v err=%v", beforeCapture, err)
	}
	if after := purchaseAuditRetentionState(t, server.activation.Database); !reflect.DeepEqual(beforeState, after) {
		t.Fatalf("capture wrote database rows or revisions: before=%v after=%v", beforeState, after)
	}
	firstRow := purchaseAuditCapturedRow(t, beforeCapture, request.CommandContext.IdempotencyKey)
	firstPayloadBytes := assertPurchaseAuditRow(t, server.activation.Database, firstRow, request.CommandContext.IdempotencyKey, server.organisationID,
		server.activation.WorkspaceID, server.authentication.ActorUserId, server.authentication.SessionId,
		"tammy.v1.PurchaseService.PostReviewedPurchase", posted.Msg, posted.Msg.Posting, started)
	assertOrdinaryPurchaseAuditWritten(t, server.activation.Database, server.activation.WorkspaceID, server.authentication,
		server.organisationID, request.CommandContext.IdempotencyKey, posted.Msg, tammyv1.PurchaseAuditAction_PURCHASE_AUDIT_ACTION_POSTED)

	beforeMoney := purchaseAuditMoneySnapshot(t, server.activation.Database, server.organisationID)
	evidenceRequest := &tammyv1.RecordPurchaseTaxEvidenceRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: "018f0000-0000-7000-8000-000000009007"},
		OrganisationId: server.organisationID, DocumentId: document.Id, ExpectedAccountingVersion: posted.Msg.Accounting.AccountingVersion,
		Evidence: proto.Clone(request.Evidence).(*tammyv1.PurchaseEvidenceInput),
	}
	evidenced, err := server.purchases.RecordPurchaseTaxEvidence(ctx, coverageAuditRequest(evidenceRequest, server.capability))
	if err != nil || evidenced.Msg == nil || evidenced.Msg.Evidence == nil {
		t.Fatalf("public purchase evidence command: response=%v err=%v", evidenced, err)
	}
	if afterMoney := purchaseAuditMoneySnapshot(t, server.activation.Database, server.organisationID); afterMoney != beforeMoney {
		t.Fatalf("source-only evidence audit changed journal money: before=%v after=%v", beforeMoney, afterMoney)
	}

	afterState := purchaseAuditRetentionState(t, server.activation.Database)
	afterCapture, err := repository.CaptureCoverageForBAS(ctx, server.organisationID)
	if err != nil || afterCapture == nil {
		t.Fatalf("capture after actual evidence audit append: capture=%v err=%v", afterCapture, err)
	}
	if after := purchaseAuditRetentionState(t, server.activation.Database); !reflect.DeepEqual(afterState, after) {
		t.Fatalf("capture after evidence audit wrote database rows or revisions: before=%v after=%v", afterState, after)
	}
	if bytes.Equal(beforeCapture.Fingerprint, afterCapture.Fingerprint) {
		t.Fatal("purchase coverage fingerprint did not change after an actual no-money-change audit append")
	}
	firstRow = purchaseAuditCapturedRow(t, afterCapture, request.CommandContext.IdempotencyKey)
	secondRow := purchaseAuditCapturedRow(t, afterCapture, evidenceRequest.CommandContext.IdempotencyKey)
	firstPayloadBytes = assertPurchaseAuditRow(t, server.activation.Database, firstRow, request.CommandContext.IdempotencyKey, server.organisationID,
		server.activation.WorkspaceID, server.authentication.ActorUserId, server.authentication.SessionId,
		"tammy.v1.PurchaseService.PostReviewedPurchase", posted.Msg, posted.Msg.Posting, started)
	secondPayloadBytes := assertPurchaseAuditRow(t, server.activation.Database, secondRow, evidenceRequest.CommandContext.IdempotencyKey, server.organisationID,
		server.activation.WorkspaceID, server.authentication.ActorUserId, server.authentication.SessionId,
		"tammy.v1.PurchaseService.RecordPurchaseTaxEvidence", evidenced.Msg, evidenced.Msg.Evidence, started)
	assertOrdinaryPurchaseAuditWritten(t, server.activation.Database, server.activation.WorkspaceID, server.authentication,
		server.organisationID, evidenceRequest.CommandContext.IdempotencyKey, evidenced.Msg,
		tammyv1.PurchaseAuditAction_PURCHASE_AUDIT_ACTION_TAX_EVIDENCE_RECORDED)

	raw := &purchases.PostingReportingCapture{Rows: afterCapture.Rows}
	assertRawSchemaComplete(t, server.activation.Database, raw, "purchase_command_audit_v1")
	assertEveryCapturedTableHasCompleteSchema(t, server.activation.Database, raw)
	if got := postScalar(t, server.activation.Database, `SELECT count(*) FROM purchase_command_audit_v1 WHERE organisation_id=?`, server.organisationID); got != 2 {
		t.Fatalf("retained purchase command audit row count=%d, want 2", got)
	}

	auditPayloadBytes := firstPayloadBytes + secondPayloadBytes
	if afterCapture.PayloadBytes <= auditPayloadBytes {
		t.Fatalf("capture payload total %d does not include room for audit rows totaling %d bytes", afterCapture.PayloadBytes, auditPayloadBytes)
	}
	budget := afterCapture.PayloadBytes - auditPayloadBytes
	probe := &purchaseCoverageAuditProbe{CommandSQLExecutor: server.activation.Database}
	partial, budgetErr := repositoryWithCoverageProbe(t, probe, server.activation).CaptureCoverageForBASWithPayloadBudget(ctx, server.organisationID, budget)
	var bound *purchases.SourceBoundError
	if !errors.As(budgetErr, &bound) || bound.Resource != "capture bytes" || int64(bound.Limit) != budget {
		t.Fatalf("audit-payload budget error=%v, want capture-bytes bound at %d", budgetErr, budget)
	}
	if partial != nil || probe.bodyReads != 0 {
		t.Fatalf("audit-payload budget failure returned partial=%v after %d hydrated body queries", partial != nil, probe.bodyReads)
	}
	if after := purchaseAuditRetentionState(t, server.activation.Database); !reflect.DeepEqual(afterState, after) {
		t.Fatalf("bounded preflight wrote database rows or revisions: before=%v after=%v", afterState, after)
	}
	exact, err := repository.CaptureCoverageForBASWithPayloadBudget(ctx, server.organisationID, afterCapture.PayloadBytes)
	if err != nil || exact == nil || !bytes.Equal(exact.Fingerprint, afterCapture.Fingerprint) {
		t.Fatalf("exact shared payload budget capture=%v err=%v", exact, err)
	}
	if after := purchaseAuditRetentionState(t, server.activation.Database); !reflect.DeepEqual(afterState, after) {
		t.Fatalf("exact-budget capture wrote database rows or revisions: before=%v after=%v", afterState, after)
	}
}

func repositoryWithCoverageProbe(t *testing.T, executor app.CommandSQLExecutor, activation app.LocalWorkspaceActivation) *purchases.Repository {
	t.Helper()
	repository, err := purchases.NewRepository(executor, activation.Now, activation.NewID)
	if err != nil {
		t.Fatal(err)
	}
	return repository
}

var _ audit.MirrorStore = (*coverageAuditMirror)(nil)
var _ audit.InitialMirrorLifecycleStore = (*coverageAuditMirror)(nil)
var _ app.LocalWorkspaceModule = (*coverageAuditObservedModule)(nil)
