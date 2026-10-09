//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"context"
	"fmt"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

func (server documentPageServer) postRequest(t *testing.T, document *tammyv1.Document) *tammyv1.PostReviewedPurchaseRequest {
	t.Helper()
	expense := &tammyv1.Account{Id: "018f0000-0000-7000-8000-000000008001", OrganisationId: server.organisationID, Version: 1, Code: "6101", Name: "Purchase cost", Type: tammyv1.AccountType_ACCOUNT_TYPE_EXPENSE, NormalBalance: tammyv1.NormalBalance_NORMAL_BALANCE_DEBIT, Status: tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE, Designation: tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY, ReportClassification: "custom.account", CashFlowClassification: "operating"}
	accounts, err := accounting.NewAccountRepository(server.module.db)
	if err != nil {
		t.Fatal(err)
	}
	if err := accounts.Create(context.Background(), expense, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	return &tammyv1.PostReviewedPurchaseRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: "018f0000-0000-7000-8000-000000008002"}, OrganisationId: server.organisationID, DocumentId: document.Id, ExpectedReviewedVersion: document.Version,
		Allocations:           []*tammyv1.PurchaseAllocationInput{{Sequence: 1, AccountId: expense.Id, AccountVersion: 1, Gross: proto.Clone(document.Candidate.Total).(*tammyv1.Money), SupplierGst: proto.Clone(document.Candidate.Gst).(*tammyv1.Money), EligibleGst: proto.Clone(document.Candidate.Gst).(*tammyv1.Money), Use: tammyv1.PurchaseUse_PURCHASE_USE_BUSINESS, BasClassification: tammyv1.PurchaseBasClassification_PURCHASE_BAS_CLASSIFICATION_NON_CAPITAL, CashFlowClassification: tammyv1.PurchaseCashFlowClassification_PURCHASE_CASH_FLOW_CLASSIFICATION_OPERATING, EligibilityReviewConfirmed: true, EvidenceReviewConfirmed: true}},
		Evidence:              &tammyv1.PurchaseEvidenceInput{Source: &tammyv1.PurchaseRetainedSourceRef{SourceId: document.Id, SourceVersion: document.Version, SourceHash: append([]byte(nil), document.Sha256...)}, Classification: tammyv1.PurchaseEvidenceClassification_PURCHASE_EVIDENCE_CLASSIFICATION_TAX_INVOICE, HeldDate: proto.Clone(document.Candidate.DocumentDate).(*tammyv1.CivilDate), RegistrationReviewConfirmed: true, BusinessUseReviewConfirmed: true, EvidenceReviewConfirmed: true, Reason: "Tax invoice reviewed against supplier original"},
		DuplicateReview:       &tammyv1.PurchaseDuplicateReview{Decision: tammyv1.PurchaseDuplicateDecision_PURCHASE_DUPLICATE_DECISION_NO_MATCH, Reason: "No matching transaction"},
		PriorAccountingReview: &tammyv1.PurchasePriorAccountingReview{Decision: tammyv1.PurchasePriorAccountingDecision_PURCHASE_PRIOR_ACCOUNTING_DECISION_NOT_PREVIOUSLY_ACCOUNTED_OR_CLAIMED, ReviewedExistingAccountingConfirmed: true, Reason: "Existing accounts and claim history checked"},
	}
}
func purchaseTLSCounts(t *testing.T, server documentPageServer) map[string]int64 {
	t.Helper()
	result := map[string]int64{}
	for _, table := range []string{"purchase_postings_v1", "purchase_payments_v1", "purchase_tax_evidence_v1", "purchase_gst_attributions_v1", "purchase_gst_deferrals_v1", "purchase_legacy_reviews_v1", "purchase_accounting_events_v1", "purchase_command_audit_v1", "audit_events_v1", "journals", "cash_flow_facts", "financial_revision_claims", "command_idempotency_v1"} {
		var count int64
		if err := server.module.db.QueryRowContext(context.Background(), "SELECT count(*) FROM "+table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		result[table] = count
	}
	return result
}
func assertPurchaseTLSCounts(t *testing.T, before, after map[string]int64) {
	t.Helper()
	for table, count := range before {
		if after[table] != count {
			t.Fatalf("rejected/replayed command changed %s from%d to%d", table, count, after[table])
		}
	}
}

func (server documentPageServer) userWithRole(t *testing.T, role tammyv1.Role, index int) *tammyv1.AuthenticationContext {
	t.Helper()
	username := fmt.Sprintf("purchase-role-%d@tammy.local", index)
	created, err := server.identity.CreateUser(context.Background(), brandingRequest(&tammyv1.CreateUserRequest{CommandContext: &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: fmt.Sprintf("018f0000-0000-7000-8000-%012x", index+0x9000)}, Username: username, DisplayName: "Purchase role", Roles: []tammyv1.Role{role}}, server.capability))
	if err != nil {
		t.Fatalf("create role user %v: %v", role, err)
	}
	activated, err := server.identity.ActivateUser(context.Background(), brandingRequest(&tammyv1.ActivateUserRequest{Username: username, ActivationCode: &tammyv1.SecretInput{Utf8: append([]byte(nil), created.Msg.ActivationCode.Utf8...)}, NewPassword: &tammyv1.SecretInput{Utf8: []byte("purchase-role-password-long-enough")}}, server.capability))
	if err != nil {
		t.Fatalf("activate role user %v: %v", role, err)
	}
	return &tammyv1.AuthenticationContext{ActorUserId: activated.Msg.User.Id, SessionId: activated.Msg.Session.Id}
}

func TestPurchaseCommandRoleAdmissionIsInsideRealTransaction(t *testing.T) {
	for index, role := range []tammyv1.Role{tammyv1.Role_ROLE_BUSINESS_PREPARER, tammyv1.Role_ROLE_BUSINESS_LODGER, tammyv1.Role_ROLE_AUDITOR} {
		t.Run(role.String(), func(t *testing.T) {
			server := newDocumentPageServer(t)
			document := server.reviewedPurchase(t)
			request := server.postRequest(t, document)
			auth := server.userWithRole(t, role, index)
			query := &tammyv1.GetPurchaseAccountingRequest{Authentication: auth, OrganisationId: server.organisationID, DocumentId: document.Id}
			if _, err := server.purchases.GetPurchaseAccounting(context.Background(), brandingRequest(query, server.capability)); err != nil {
				t.Fatalf("active role cannot read owned purchase: %v", err)
			}
			request.CommandContext.Authentication = auth
			before := purchaseTLSCounts(t, server)
			result, err := server.purchases.PostReviewedPurchase(context.Background(), brandingRequest(request, server.capability))
			if role == tammyv1.Role_ROLE_BUSINESS_PREPARER {
				if err != nil || !result.Msg.Accounting.Posted {
					t.Fatalf("preparer accounting admission=%v error=%v", result, err)
				}
			} else {
				if connect.CodeOf(err) != connect.CodePermissionDenied {
					t.Fatalf("read-only role posted: %v", err)
				}
				assertPurchaseTLSCounts(t, before, purchaseTLSCounts(t, server))
			}
		})
	}
}

func TestPurchaseCommandThroughRealTLSCommitsOnceAndRollsBackAuditFailure(t *testing.T) {
	for _, basis := range []tammyv1.GstBasis{tammyv1.GstBasis_GST_BASIS_CASH, tammyv1.GstBasis_GST_BASIS_NON_CASH} {
		t.Run(basis.String(), func(t *testing.T) {
			server := newDocumentPageServerWithBasis(t, basis)
			document := server.reviewedPurchase(t)
			request := server.postRequest(t, document)
			ctx := context.Background()
			posted, err := server.purchases.PostReviewedPurchase(ctx, brandingRequest(request, server.capability))
			if err != nil || posted.Msg.Accounting.AccountingVersion != 1 || !posted.Msg.Accounting.Posted || posted.Msg.Journal == nil {
				t.Fatalf("actual posting=%v error=%v", posted, err)
			}
			before := purchaseTLSCounts(t, server)
			replay, err := server.purchases.PostReviewedPurchase(ctx, brandingRequest(request, server.capability))
			if err != nil || !proto.Equal(posted.Msg, replay.Msg) {
				t.Fatalf("exact posting replay=%v error=%v", replay, err)
			}
			assertPurchaseTLSCounts(t, before, purchaseTLSCounts(t, server))
			changed := proto.Clone(request).(*tammyv1.PostReviewedPurchaseRequest)
			changed.PriorAccountingReview.Reason = "Changed review under same key"
			if _, err := server.purchases.PostReviewedPurchase(ctx, brandingRequest(changed, server.capability)); connect.CodeOf(err) != connect.CodeAlreadyExists {
				t.Fatalf("changed posting key=%v", err)
			}
			changed = proto.Clone(request).(*tammyv1.PostReviewedPurchaseRequest)
			changed.CommandContext.IdempotencyKey = "018f0000-0000-7000-8000-000000008003"
			if _, err := server.purchases.PostReviewedPurchase(ctx, brandingRequest(changed, server.capability)); connect.CodeOf(err) != connect.CodeFailedPrecondition {
				t.Fatalf("new-key duplicate posting=%v", err)
			}
			assertPurchaseTLSCounts(t, before, purchaseTLSCounts(t, server))
			payment := &tammyv1.RecordPurchasePaymentRequest{CommandContext: &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: "018f0000-0000-7000-8000-000000008004"}, OrganisationId: server.organisationID, DocumentId: document.Id, ExpectedAccountingVersion: 1, ActualPaymentDate: &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 1}, Amount: &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 5500}, BankAccountId: "018f0000-0000-7000-8000-000000000101", Reference: "Actual first payment"}
			// Reject after sources, journal, cash-flow and revisions were written.
			if _, err := server.module.db.ExecContext(ctx, `CREATE TRIGGER fail_purchase_audit BEFORE INSERT ON purchase_command_audit_v1 BEGIN SELECT RAISE(ABORT,'private injected audit failure'); END`); err != nil {
				t.Fatal(err)
			}
			if _, err := server.purchases.RecordPurchasePayment(ctx, brandingRequest(payment, server.capability)); connect.CodeOf(err) != connect.CodeInternal {
				t.Fatalf("audit failure=%v", err)
			}
			assertPurchaseTLSCounts(t, before, purchaseTLSCounts(t, server))
			if _, err := server.module.db.ExecContext(ctx, `DROP TRIGGER fail_purchase_audit`); err != nil {
				t.Fatal(err)
			}
			// Reject the final result commitment after the retained audit insert.
			if _, err := server.module.db.ExecContext(ctx, `CREATE TRIGGER fail_purchase_result BEFORE UPDATE ON command_idempotency_v1 WHEN NEW.outcome='COMMITTED' BEGIN SELECT RAISE(ABORT,'private injected result failure'); END`); err != nil {
				t.Fatal(err)
			}
			if _, err := server.purchases.RecordPurchasePayment(ctx, brandingRequest(payment, server.capability)); connect.CodeOf(err) != connect.CodeInternal {
				t.Fatalf("result commitment failure=%v", err)
			}
			assertPurchaseTLSCounts(t, before, purchaseTLSCounts(t, server))
			if _, err := server.module.db.ExecContext(ctx, `DROP TRIGGER fail_purchase_result`); err != nil {
				t.Fatal(err)
			}
			first, err := server.purchases.RecordPurchasePayment(ctx, brandingRequest(payment, server.capability))
			if err != nil || first.Msg.Accounting.Paid.MinorUnits != 5500 || first.Msg.Accounting.Outstanding.MinorUnits != 5500 {
				t.Fatalf("actual first payment=%v error=%v", first, err)
			}
			before = purchaseTLSCounts(t, server)
			invalidPayment := proto.Clone(payment).(*tammyv1.RecordPurchasePaymentRequest)
			invalidPayment.CommandContext.IdempotencyKey = "018f0000-0000-7000-8000-000000008006"
			invalidPayment.ExpectedAccountingVersion = 2
			invalidPayment.Amount.MinorUnits = 5501
			if _, err := server.purchases.RecordPurchasePayment(ctx, brandingRequest(invalidPayment, server.capability)); connect.CodeOf(err) != connect.CodeInvalidArgument {
				t.Fatalf("overpayment corrective error=%v", err)
			}
			assertPurchaseTLSCounts(t, before, purchaseTLSCounts(t, server))
			repaid, err := server.purchases.RecordPurchasePayment(ctx, brandingRequest(payment, server.capability))
			if err != nil || !proto.Equal(first.Msg, repaid.Msg) {
				t.Fatalf("exact payment replay=%v error=%v", repaid, err)
			}
			assertPurchaseTLSCounts(t, before, purchaseTLSCounts(t, server))
			changedPayment := proto.Clone(payment).(*tammyv1.RecordPurchasePaymentRequest)
			changedPayment.Reference = "Changed payment reference"
			if _, err := server.purchases.RecordPurchasePayment(ctx, brandingRequest(changedPayment, server.capability)); connect.CodeOf(err) != connect.CodeAlreadyExists {
				t.Fatalf("changed payment key=%v", err)
			}
			secondRequest := proto.Clone(payment).(*tammyv1.RecordPurchasePaymentRequest)
			secondRequest.CommandContext.IdempotencyKey = "018f0000-0000-7000-8000-000000008005"
			secondRequest.ExpectedAccountingVersion = 2
			secondRequest.ActualPaymentDate = &tammyv1.CivilDate{Year: 2026, Month: 10, Day: 1}
			second, err := server.purchases.RecordPurchasePayment(ctx, brandingRequest(secondRequest, server.capability))
			if err != nil || second.Msg.Accounting.Paid.MinorUnits != 11000 || second.Msg.Accounting.Outstanding.MinorUnits != 0 || second.Msg.Accounting.AttributedGst.MinorUnits != 1000 || second.Msg.Accounting.DeferredGst.MinorUnits != 0 || second.Msg.Accounting.SuspenseGst.MinorUnits != 0 {
				t.Fatalf("actual full settlement=%v error=%v", second, err)
			}
			var cost, bank, payable int64
			for code, dest := range map[string]*int64{"6101": &cost, "1000": &bank, "2000": &payable} {
				if err := server.module.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(l.debit_minor-l.credit_minor),0) FROM journal_lines l JOIN accounts a ON a.id=l.account_id AND a.organisation_id=? WHERE a.code=?`, server.organisationID, code).Scan(dest); err != nil {
					t.Fatal(err)
				}
			}
			if cost != 10000 || bank != -11000 || payable != 0 {
				t.Fatalf("actual ledger cost=%d bank=%d payable=%d", cost, bank, payable)
			}
			var audit int64
			if err := server.module.db.QueryRowContext(ctx, `SELECT count(*) FROM purchase_command_audit_v1 WHERE organisation_id=?`, server.organisationID).Scan(&audit); err != nil || audit != 3 {
				t.Fatalf("one retained audit per accepted command=%d,error=%v", audit, err)
			}
			get, err := server.purchases.GetPurchaseAccounting(ctx, brandingRequest(&tammyv1.GetPurchaseAccountingRequest{Authentication: server.authentication, OrganisationId: server.organisationID, DocumentId: document.Id}, server.capability))
			if err != nil || !proto.Equal(get.Msg.Accounting, second.Msg.Accounting) {
				t.Fatalf("authoritative read=%v error=%v", get, err)
			}
			pageRequest := &tammyv1.ListPurchasePaymentsRequest{Authentication: server.authentication, OrganisationId: server.organisationID, DocumentId: document.Id, Page: &tammyv1.PageRequest{PageSize: 1}}
			page, err := server.purchases.ListPurchasePayments(ctx, brandingRequest(pageRequest, server.capability))
			if err != nil || len(page.Msg.Payments) != 1 || page.Msg.Page.GetNextCursor() == "" {
				t.Fatalf("signed payment first page=%v error=%v", page, err)
			}
			cursor := page.Msg.Page.GetNextCursor()
			pageRequest.Page.Cursor = &cursor
			last, err := server.purchases.ListPurchasePayments(ctx, brandingRequest(pageRequest, server.capability))
			if err != nil || len(last.Msg.Payments) != 1 || last.Msg.Payments[0].Id == page.Msg.Payments[0].Id || last.Msg.Page.NextCursor != nil {
				t.Fatalf("payment continuation=%v error=%v", last, err)
			}
			for name, mutate := range map[string]func(*tammyv1.ListPurchasePaymentsRequest){"tampered": func(r *tammyv1.ListPurchasePaymentsRequest) { x := cursor + "="; r.Page.Cursor = &x }, "foreign org": func(r *tammyv1.ListPurchasePaymentsRequest) {
				r.OrganisationId = "018f0000-0000-7000-8000-000000009999"
			}, "foreign document": func(r *tammyv1.ListPurchasePaymentsRequest) { r.DocumentId = "018f0000-0000-7000-8000-000000009999" }, "changed limit": func(r *tammyv1.ListPurchasePaymentsRequest) { r.Page.PageSize = 2 }} {
				t.Run(name, func(t *testing.T) {
					bad := proto.Clone(pageRequest).(*tammyv1.ListPurchasePaymentsRequest)
					mutate(bad)
					if _, err := server.purchases.ListPurchasePayments(ctx, brandingRequest(bad, server.capability)); connect.CodeOf(err) != connect.CodeInvalidArgument {
						t.Fatalf("cursor accepted %s: %v", name, err)
					}
				})
			}
			before = purchaseTLSCounts(t, server)
			if _, err := server.workspace.LockWorkspace(ctx, brandingRequest(&tammyv1.LockWorkspaceRequest{Authentication: server.authentication}, server.capability)); err != nil {
				t.Fatal(err)
			}
			if _, err := server.purchases.RecordPurchasePayment(ctx, brandingRequest(secondRequest, server.capability)); connect.CodeOf(err) != connect.CodeInternal {
				t.Fatalf("locked module did not report unavailable: %v", err)
			}
			if _, err := server.workspace.UnlockWorkspace(ctx, brandingRequest(&tammyv1.UnlockWorkspaceRequest{
				WorkspaceFile: &tammyv1.ApprovedFileRef{CapabilityId: app.LocalWorkspaceFileCapability},
				Proof:         &tammyv1.WorkspaceUnlockProof{Proof: &tammyv1.WorkspaceUnlockProof_Passphrase{Passphrase: &tammyv1.SecretInput{Utf8: []byte("workspace-passphrase-long-enough")}}},
			}, server.capability)); err != nil {
				t.Fatal(err)
			}
			signedIn, err := server.identity.SignIn(ctx, brandingRequest(&tammyv1.SignInRequest{Username: "admin@tammy.local", Password: &tammyv1.SecretInput{Utf8: []byte("administrator-password-long-enough")}}, server.capability))
			if err != nil {
				t.Fatal(err)
			}
			freshAuth := &tammyv1.AuthenticationContext{ActorUserId: signedIn.Msg.User.Id, SessionId: signedIn.Msg.Session.Id}
			afterSecurity := purchaseTLSCounts(t, server)
			if afterSecurity["audit_events_v1"] != before["audit_events_v1"]+4 {
				t.Fatalf("lock, unlock, workspace session and actual sign-in audit events = %d; prior %d", afterSecurity["audit_events_v1"], before["audit_events_v1"])
			}
			before["audit_events_v1"] = afterSecurity["audit_events_v1"]
			assertPurchaseTLSCounts(t, before, afterSecurity)
			before = afterSecurity
			secondRequest.CommandContext.Authentication = freshAuth
			restarted, err := server.purchases.RecordPurchasePayment(ctx, brandingRequest(secondRequest, server.capability))
			if err != nil || !proto.Equal(second.Msg, restarted.Msg) {
				t.Fatalf("reopened encrypted workspace exact retry=%v error=%v", restarted, err)
			}
			assertPurchaseTLSCounts(t, before, purchaseTLSCounts(t, server))
			pageRequest.Authentication = freshAuth
			if _, err := server.purchases.ListPurchasePayments(ctx, brandingRequest(pageRequest, server.capability)); connect.CodeOf(err) != connect.CodeInvalidArgument {
				t.Fatalf("cursor crossed local session: %v", err)
			}
		})
	}
}
