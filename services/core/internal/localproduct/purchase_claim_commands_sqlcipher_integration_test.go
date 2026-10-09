//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/documents"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func purchaseCivilNow() *tammyv1.CivilDate {
	now := time.Now().UTC()
	return &tammyv1.CivilDate{Year: int32(now.Year()), Month: int32(now.Month()), Day: int32(now.Day())}
}

func (server documentPageServer) reviewedReturnNotice(t *testing.T) *tammyv1.Document {
	t.Helper()
	repo, err := documents.NewRepository(server.module.db)
	if err != nil {
		t.Fatal(err)
	}
	original := []byte("Retained manual supporting notice: actual required-return due date 3 November 2026")
	digest := sha256.Sum256(original)
	candidate := &tammyv1.DocumentCandidate{SupplierName: "Australian Taxation Office", InvoiceNumber: "Required-return notice", DocumentDate: &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 1}, Subtotal: &tammyv1.Money{CurrencyCode: "AUD"}, Gst: &tammyv1.Money{CurrencyCode: "AUD"}, Total: &tammyv1.Money{CurrencyCode: "AUD"}}
	doc, err := repo.Create(context.Background(), "018f0000-0000-7000-8000-000000008052", server.authentication.ActorUserId, &tammyv1.Document{Id: "018f0000-0000-7000-8000-000000008051", OrganisationId: server.organisationID, Version: 1, Status: tammyv1.DocumentStatus_DOCUMENT_STATUS_NEEDS_REVIEW, SourceDisplayName: "return-notice.pdf", MimeType: "application/pdf", ByteLength: uint64(len(original)), Sha256: digest[:], Candidate: candidate, CreatedAt: timestamppb.New(time.Now().UTC())}, original)
	if err != nil {
		t.Fatal(err)
	}
	doc, err = repo.SaveReview(context.Background(), doc.Id, doc.Version, candidate, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestPurchaseCommandEvidenceDeferralAttributionAndLegacyThroughRealTLS(t *testing.T) {
	for _, scenario := range []struct {
		basis tammyv1.GstBasis
		role  tammyv1.Role
	}{
		{tammyv1.GstBasis_GST_BASIS_CASH, tammyv1.Role_ROLE_WORKSPACE_ADMIN},
		{tammyv1.GstBasis_GST_BASIS_NON_CASH, tammyv1.Role_ROLE_WORKSPACE_ADMIN},
		{tammyv1.GstBasis_GST_BASIS_CASH, tammyv1.Role_ROLE_BUSINESS_PREPARER},
		{tammyv1.GstBasis_GST_BASIS_NON_CASH, tammyv1.Role_ROLE_BUSINESS_PREPARER},
	} {
		basis := scenario.basis
		t.Run(basis.String()+"/"+scenario.role.String(), func(t *testing.T) {
			ctx := context.Background()
			server := newDocumentPageServerWithBasis(t, basis)
			if scenario.role == tammyv1.Role_ROLE_BUSINESS_PREPARER {
				server.authentication = server.userWithRole(t, scenario.role, 4)
			}
			doc := server.reviewedPurchase(t)
			postRequest := server.postRequest(t, doc)
			ready := proto.Clone(postRequest.Evidence).(*tammyv1.PurchaseEvidenceInput)
			postRequest.Evidence.Classification = tammyv1.PurchaseEvidenceClassification_PURCHASE_EVIDENCE_CLASSIFICATION_UNKNOWN
			postRequest.Evidence.Source, postRequest.Evidence.HeldDate = nil, nil
			posted, err := server.purchases.PostReviewedPurchase(ctx, brandingRequest(postRequest, server.capability))
			if err != nil {
				t.Fatal(err)
			}
			state := posted.Msg.Accounting
			assertPurchaseCommandOrdinaryAudit(t, server, postRequest.CommandContext.IdempotencyKey, posted.Msg, tammyv1.PurchaseAuditAction_PURCHASE_AUDIT_ACTION_POSTED)
			portion := &tammyv1.PurchaseGstSourcePortion{NormalEventDate: posted.Msg.Posting.InvoiceDate, EarliestOtherwiseEligiblePeriod: &tammyv1.PurchaseClaimPeriod{PeriodStart: &tammyv1.CivilDate{Year: 2026, Month: 7, Day: 1}, PeriodEnd: &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 30}}, RequiredReturnDueDate: &tammyv1.CivilDate{Year: 2026, Month: 11, Day: 3}, OrdinaryLastClaimDate: &tammyv1.CivilDate{Year: 2030, Month: 11, Day: 3}}
			want := int64(1000)
			if basis == tammyv1.GstBasis_GST_BASIS_CASH {
				paid, err := server.purchases.RecordPurchasePayment(ctx, brandingRequest(&tammyv1.RecordPurchasePaymentRequest{CommandContext: &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: "018f0000-0000-7000-8000-000000008053"}, OrganisationId: server.organisationID, DocumentId: doc.Id, ExpectedAccountingVersion: state.AccountingVersion, ActualPaymentDate: &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 30}, Amount: &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 5500}, BankAccountId: "018f0000-0000-7000-8000-000000000101", Reference: "Actual paid source requiring evidence"}, server.capability))
				if err != nil {
					t.Fatal(err)
				}
				state, want = paid.Msg.Accounting, 500
				assertPurchaseCommandOrdinaryAudit(t, server, "018f0000-0000-7000-8000-000000008053", paid.Msg, tammyv1.PurchaseAuditAction_PURCHASE_AUDIT_ACTION_PAYMENT_RECORDED)
				portion.NormalEventDate = paid.Msg.Payment.ActualPaymentDate
				portion.Source = &tammyv1.PurchaseGstSourcePortion_Payment{Payment: &tammyv1.PurchasePaymentPortionRef{PostingId: posted.Msg.Posting.Id, PostingHash: posted.Msg.Posting.SourceHash, PaymentId: paid.Msg.Payment.Id, PaymentHash: paid.Msg.Payment.SourceHash, AllocationSequence: 1}}
			} else {
				portion.Source = &tammyv1.PurchaseGstSourcePortion_Posting{Posting: &tammyv1.PurchasePostingPortionRef{PostingId: posted.Msg.Posting.Id, PostingHash: posted.Msg.Posting.SourceHash, AllocationSequence: 1}}
			}
			ready.HeldDate = &tammyv1.CivilDate{Year: 2026, Month: 10, Day: 1}
			evidenceRequest := &tammyv1.RecordPurchaseTaxEvidenceRequest{CommandContext: &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: "018f0000-0000-7000-8000-000000008054"}, OrganisationId: server.organisationID, DocumentId: doc.Id, ExpectedAccountingVersion: state.AccountingVersion, Evidence: ready}
			evidenced, err := server.purchases.RecordPurchaseTaxEvidence(ctx, brandingRequest(evidenceRequest, server.capability))
			if err != nil || evidenced.Msg.Accounting.SuspenseGst.MinorUnits != want || evidenced.Msg.Accounting.AttributedGst.MinorUnits != 0 {
				t.Fatalf("evidence moved suspense: %v %v", evidenced, err)
			}
			before := purchaseTLSCounts(t, server)
			assertPurchaseCommandOrdinaryAudit(t, server, evidenceRequest.CommandContext.IdempotencyKey, evidenced.Msg, tammyv1.PurchaseAuditAction_PURCHASE_AUDIT_ACTION_TAX_EVIDENCE_RECORDED)
			replayedEvidence, err := server.purchases.RecordPurchaseTaxEvidence(ctx, brandingRequest(evidenceRequest, server.capability))
			if err != nil || !proto.Equal(evidenced.Msg, replayedEvidence.Msg) {
				t.Fatalf("evidence exact replay: %v %v", replayedEvidence, err)
			}
			assertPurchaseTLSCounts(t, before, purchaseTLSCounts(t, server))
			portion.EvidenceId, portion.EvidenceHeldDate = evidenced.Msg.Evidence.Id, ready.HeldDate
			currentEvidence, err := server.purchases.GetPurchaseAccounting(ctx, brandingRequest(&tammyv1.GetPurchaseAccountingRequest{Authentication: server.authentication, OrganisationId: server.organisationID, DocumentId: doc.Id}, server.capability))
			if err != nil {
				t.Fatal(err)
			}
			field := currentEvidence.Msg.ProtoReflect().Descriptor().Fields().ByName("latest_evidence")
			if field == nil || !currentEvidence.Msg.ProtoReflect().Has(field) || !proto.Equal(currentEvidence.Msg.ProtoReflect().Get(field).Message().Interface(), evidenced.Msg.Evidence) {
				t.Fatal("authoritative read cannot recover the latest retained evidence ID and provenance")
			}
			notice := server.reviewedReturnNotice(t)
			portion.DueDateEvidence = &tammyv1.PurchaseRetainedSourceRef{SourceId: notice.Id, SourceVersion: notice.Version, SourceHash: notice.Sha256}
			period := &tammyv1.PurchaseClaimPeriod{PeriodStart: &tammyv1.CivilDate{Year: 2026, Month: 10, Day: 1}, PeriodEnd: &tammyv1.CivilDate{Year: 2026, Month: 12, Day: 31}}
			timing := &tammyv1.PurchaseClaimTimingReview{ReviewedAsOf: purchaseCivilNow(), AllowablePeriodConfirmed: true, OrdinaryTimeLimitConfirmed: true, EvidenceHeldConfirmed: true, Reason: "Manually reviewed actual required return notice, allowable period and deadline"}
			deferRequest := &tammyv1.DeferPurchaseGstClaimRequest{CommandContext: &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: "018f0000-0000-7000-8000-000000008055"}, OrganisationId: server.organisationID, DocumentId: doc.Id, ExpectedAccountingVersion: evidenced.Msg.Accounting.AccountingVersion, Portions: []*tammyv1.PurchaseGstSourcePortion{portion}, AffectedPeriod: period, TimingReview: timing, Reason: "Retain supported future claim for this selected period"}
			deferred, err := server.purchases.DeferPurchaseGstClaim(ctx, brandingRequest(deferRequest, server.capability))
			if err != nil || deferred.Msg.Deferral.DeferredGst.MinorUnits != want || deferred.Msg.Accounting.SuspenseGst.MinorUnits != want {
				t.Fatalf("real deferral: %v %v", deferred, err)
			}
			before = purchaseTLSCounts(t, server)
			assertPurchaseCommandOrdinaryAudit(t, server, deferRequest.CommandContext.IdempotencyKey, deferred.Msg, tammyv1.PurchaseAuditAction_PURCHASE_AUDIT_ACTION_GST_DEFERRED)
			replayedDeferral, err := server.purchases.DeferPurchaseGstClaim(ctx, brandingRequest(deferRequest, server.capability))
			if err != nil || !proto.Equal(deferred.Msg, replayedDeferral.Msg) {
				t.Fatalf("deferral exact replay: %v %v", replayedDeferral, err)
			}
			assertPurchaseTLSCounts(t, before, purchaseTLSCounts(t, server))
			currentDeferral, err := server.purchases.GetPurchaseAccounting(ctx, brandingRequest(&tammyv1.GetPurchaseAccountingRequest{Authentication: server.authentication, OrganisationId: server.organisationID, DocumentId: doc.Id}, server.capability))
			if err != nil || !proto.Equal(currentDeferral.Msg.LatestDeferral, deferred.Msg.Deferral) {
				t.Fatalf("authoritative read cannot recover the retained deferral ID and source portions: %v %v", currentDeferral, err)
			}
			claimRequest := &tammyv1.AttributePurchaseGstRequest{CommandContext: &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: "018f0000-0000-7000-8000-000000008056"}, OrganisationId: server.organisationID, DocumentId: doc.Id, ExpectedAccountingVersion: deferred.Msg.Accounting.AccountingVersion, Portions: []*tammyv1.PurchaseGstSourcePortion{portion}, ClaimPeriod: period, ReleasePostingDate: &tammyv1.CivilDate{Year: 2026, Month: 10, Day: 1}, TimingReview: timing, DeferralId: &deferred.Msg.Deferral.Id}
			claimed, err := server.purchases.AttributePurchaseGst(ctx, brandingRequest(claimRequest, server.capability))
			if err != nil || claimed.Msg.Accounting.SuspenseGst.MinorUnits != 0 || claimed.Msg.Accounting.AttributedGst.MinorUnits != want || len(claimed.Msg.Attributions) != 1 || claimed.Msg.Attributions[0].GetClosesDeferralId() != deferred.Msg.Deferral.Id {
				t.Fatalf("real release: %v %v", claimed, err)
			}
			before = purchaseTLSCounts(t, server)
			assertPurchaseCommandOrdinaryAudit(t, server, claimRequest.CommandContext.IdempotencyKey, claimed.Msg, tammyv1.PurchaseAuditAction_PURCHASE_AUDIT_ACTION_GST_ATTRIBUTED)
			replayedClaim, err := server.purchases.AttributePurchaseGst(ctx, brandingRequest(claimRequest, server.capability))
			if err != nil || !proto.Equal(claimed.Msg, replayedClaim.Msg) {
				t.Fatalf("claim exact replay: %v %v", replayedClaim, err)
			}
			assertPurchaseTLSCounts(t, before, purchaseTLSCounts(t, server))
			candidateBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(notice.Candidate)
			if err != nil {
				t.Fatal(err)
			}
			reviewedHash := sha256.Sum256(candidateBytes)
			legacyRequest := &tammyv1.ResolveLegacyPurchaseReviewRequest{CommandContext: &tammyv1.CommandContext{Authentication: server.authentication, IdempotencyKey: "018f0000-0000-7000-8000-000000008057"}, OrganisationId: server.organisationID, Review: &tammyv1.LegacyPurchaseReviewInput{Source: &tammyv1.LegacyPurchaseSourceRef{Source: &tammyv1.LegacyPurchaseSourceRef_Document{Document: &tammyv1.LegacyPurchaseDocumentRef{DocumentId: notice.Id, ReviewedVersion: notice.Version, OriginalContentHash: notice.Sha256, ReviewedSnapshotHash: reviewedHash[:]}}}, AffectedPeriod: &tammyv1.PurchaseClaimPeriod{PeriodStart: &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 1}, PeriodEnd: &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 30}}, ReportingBalanceDate: purchaseCivilNow(), Disposition: tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_NONCREDITABLE, SupportingEvidence: []*tammyv1.PurchaseRetainedSourceRef{portion.DueDateEvidence}, Reason: "This retained notice is supporting evidence, not a taxable acquisition"}}
			resolved, err := server.purchases.ResolveLegacyPurchaseReview(ctx, brandingRequest(legacyRequest, server.capability))
			if err != nil || resolved.Msg.Review.SourceRevision != 1 {
				t.Fatalf("real legacy review: %v %v", resolved, err)
			}
			before = purchaseTLSCounts(t, server)
			assertPurchaseCommandOrdinaryAudit(t, server, legacyRequest.CommandContext.IdempotencyKey, resolved.Msg, tammyv1.PurchaseAuditAction_PURCHASE_AUDIT_ACTION_LEGACY_REVIEW_RESOLVED)
			replayedLegacy, err := server.purchases.ResolveLegacyPurchaseReview(ctx, brandingRequest(legacyRequest, server.capability))
			if err != nil || !proto.Equal(resolved.Msg, replayedLegacy.Msg) {
				t.Fatalf("legacy exact replay: %v %v", replayedLegacy, err)
			}
			assertPurchaseTLSCounts(t, before, purchaseTLSCounts(t, server))
			for _, mutation := range []func() error{
				func() error {
					r := proto.Clone(evidenceRequest).(*tammyv1.RecordPurchaseTaxEvidenceRequest)
					r.Evidence.Reason += " changed"
					_, e := server.purchases.RecordPurchaseTaxEvidence(ctx, brandingRequest(r, server.capability))
					return e
				},
				func() error {
					r := proto.Clone(deferRequest).(*tammyv1.DeferPurchaseGstClaimRequest)
					r.Reason += " changed"
					_, e := server.purchases.DeferPurchaseGstClaim(ctx, brandingRequest(r, server.capability))
					return e
				},
				func() error {
					r := proto.Clone(claimRequest).(*tammyv1.AttributePurchaseGstRequest)
					r.TimingReview.Reason += " changed"
					_, e := server.purchases.AttributePurchaseGst(ctx, brandingRequest(r, server.capability))
					return e
				},
				func() error {
					r := proto.Clone(legacyRequest).(*tammyv1.ResolveLegacyPurchaseReviewRequest)
					r.Review.Reason += " changed"
					_, e := server.purchases.ResolveLegacyPurchaseReview(ctx, brandingRequest(r, server.capability))
					return e
				},
			} {
				if e := mutation(); connect.CodeOf(e) != connect.CodeAlreadyExists {
					t.Fatalf("changed retained payload: %v", e)
				}
				assertPurchaseTLSCounts(t, before, purchaseTLSCounts(t, server))
			}
			commands := []func(*tammyv1.AuthenticationContext) error{
				func(auth *tammyv1.AuthenticationContext) error {
					r := proto.Clone(postRequest).(*tammyv1.PostReviewedPurchaseRequest)
					r.CommandContext.Authentication = auth
					r.CommandContext.IdempotencyKey = "018f0000-0000-7000-8000-000000008061"
					_, e := server.purchases.PostReviewedPurchase(ctx, brandingRequest(r, server.capability))
					return e
				},
				func(auth *tammyv1.AuthenticationContext) error {
					_, e := server.purchases.RecordPurchasePayment(ctx, brandingRequest(&tammyv1.RecordPurchasePaymentRequest{CommandContext: &tammyv1.CommandContext{Authentication: auth, IdempotencyKey: "018f0000-0000-7000-8000-000000008062"}, OrganisationId: server.organisationID, DocumentId: doc.Id, ExpectedAccountingVersion: claimed.Msg.Accounting.AccountingVersion, ActualPaymentDate: &tammyv1.CivilDate{Year: 2026, Month: 10, Day: 1}, Amount: &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 1}, BankAccountId: "018f0000-0000-7000-8000-000000000101"}, server.capability))
					return e
				},
				func(auth *tammyv1.AuthenticationContext) error {
					r := proto.Clone(evidenceRequest).(*tammyv1.RecordPurchaseTaxEvidenceRequest)
					r.CommandContext.Authentication = auth
					r.CommandContext.IdempotencyKey = "018f0000-0000-7000-8000-000000008063"
					_, e := server.purchases.RecordPurchaseTaxEvidence(ctx, brandingRequest(r, server.capability))
					return e
				},
				func(auth *tammyv1.AuthenticationContext) error {
					r := proto.Clone(claimRequest).(*tammyv1.AttributePurchaseGstRequest)
					r.CommandContext.Authentication = auth
					r.CommandContext.IdempotencyKey = "018f0000-0000-7000-8000-000000008064"
					_, e := server.purchases.AttributePurchaseGst(ctx, brandingRequest(r, server.capability))
					return e
				},
				func(auth *tammyv1.AuthenticationContext) error {
					r := proto.Clone(deferRequest).(*tammyv1.DeferPurchaseGstClaimRequest)
					r.CommandContext.Authentication = auth
					r.CommandContext.IdempotencyKey = "018f0000-0000-7000-8000-000000008065"
					_, e := server.purchases.DeferPurchaseGstClaim(ctx, brandingRequest(r, server.capability))
					return e
				},
				func(auth *tammyv1.AuthenticationContext) error {
					r := proto.Clone(legacyRequest).(*tammyv1.ResolveLegacyPurchaseReviewRequest)
					r.CommandContext.Authentication = auth
					r.CommandContext.IdempotencyKey = "018f0000-0000-7000-8000-000000008066"
					_, e := server.purchases.ResolveLegacyPurchaseReview(ctx, brandingRequest(r, server.capability))
					return e
				},
			}
			for index, role := range []tammyv1.Role{tammyv1.Role_ROLE_BUSINESS_LODGER, tammyv1.Role_ROLE_AUDITOR} {
				adminServer := server
				// Activation intentionally invalidates other local sessions. Acquire
				// a real administrator session for each separate user creation.
				signedIn, err := server.identity.SignIn(ctx, brandingRequest(&tammyv1.SignInRequest{Username: "admin@tammy.local", Password: &tammyv1.SecretInput{Utf8: []byte("administrator-password-long-enough")}}, server.capability))
				if err != nil {
					t.Fatal(err)
				}
				adminServer.authentication = &tammyv1.AuthenticationContext{ActorUserId: signedIn.Msg.User.Id, SessionId: signedIn.Msg.Session.Id}
				auth := adminServer.userWithRole(t, role, index)
				if _, e := server.purchases.GetPurchaseAccounting(ctx, brandingRequest(&tammyv1.GetPurchaseAccountingRequest{Authentication: auth, OrganisationId: server.organisationID, DocumentId: doc.Id}, server.capability)); e != nil {
					t.Fatalf("read-only role cannot inspect accepted purchase: %v", e)
				}
				before = purchaseTLSCounts(t, server)
				for commandIndex, command := range commands {
					if e := command(auth); connect.CodeOf(e) != connect.CodePermissionDenied {
						t.Fatalf("role %v command %d admission: %v", role, commandIndex, e)
					}
					assertPurchaseTLSCounts(t, before, purchaseTLSCounts(t, server))
				}
			}
		})
	}
}
