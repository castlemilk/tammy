//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"bytes"
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/app"
	"github.com/tammyapp/tammy/services/core/internal/documents"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

func (server documentPageServer) reviewedPurchase(t *testing.T) *tammyv1.Document {
	t.Helper()
	document := server.seed(t, 1, "Reviewed supplier tax invoice")[0]
	candidate := proto.Clone(document.Candidate).(*tammyv1.DocumentCandidate)
	candidate.InvoiceNumber = "TLS-PURCHASE-1"
	candidate.DocumentDate = &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 1}
	candidate.Subtotal.MinorUnits = 10000
	candidate.Gst.MinorUnits = 1000
	candidate.Total.MinorUnits = 11000
	repo, err := documents.NewRepository(server.module.db)
	if err != nil {
		t.Fatal(err)
	}
	reviewed, err := repo.SaveReview(context.Background(), document.Id, document.Version, candidate, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	return reviewed
}

type revokingPurchaseIdentity struct {
	app.LocalModuleIdentity
	revoke func() error
}

func (identity revokingPurchaseIdentity) RequireActiveSessionReadOnly(ctx context.Context, auth *tammyv1.AuthenticationContext) error {
	if err := identity.LocalModuleIdentity.RequireActiveSessionReadOnly(ctx, auth); err != nil {
		return err
	}
	return identity.revoke()
}

func TestPurchaseQueryThroughRealTLSRejectsSessionRevokedAfterPreflight(t *testing.T) {
	for _, query := range []string{"accounting", "payments"} {
		t.Run(query, func(t *testing.T) {
			server := newDocumentPageServer(t)
			document := server.reviewedPurchase(t)
			activation := server.activation
			activation.Identity = revokingPurchaseIdentity{LocalModuleIdentity: activation.Identity, revoke: func() error {
				_, err := server.identity.SignOut(context.Background(), brandingRequest(&tammyv1.SignOutRequest{Authentication: server.authentication}, server.capability))
				return err
			}}
			if err := activatePurchases(activation, server.module.purchaseRoute); err != nil {
				t.Fatal(err)
			}
			var err error
			if query == "accounting" {
				_, err = server.purchases.GetPurchaseAccounting(context.Background(), brandingRequest(&tammyv1.GetPurchaseAccountingRequest{Authentication: server.authentication, OrganisationId: server.organisationID, DocumentId: document.Id}, server.capability))
			} else {
				_, err = server.purchases.ListPurchasePayments(context.Background(), brandingRequest(&tammyv1.ListPurchasePaymentsRequest{Authentication: server.authentication, OrganisationId: server.organisationID, DocumentId: document.Id, Page: &tammyv1.PageRequest{PageSize: 50}}, server.capability))
			}
			if connect.CodeOf(err) != connect.CodeUnauthenticated {
				t.Fatalf("revoked session read after preflight: %v", err)
			}
		})
	}
}

func TestPurchaseQueryThroughRealTLSRequiresActiveSessionAndOrganisation(t *testing.T) {
	server := newDocumentPageServer(t)
	document := server.reviewedPurchase(t)
	request := &tammyv1.GetPurchaseAccountingRequest{Authentication: server.authentication, OrganisationId: server.organisationID, DocumentId: document.Id}
	result, err := server.purchases.GetPurchaseAccounting(context.Background(), brandingRequest(request, server.capability))
	if err != nil || result.Msg.Accounting == nil || result.Msg.Accounting.Posted || result.Msg.Accounting.Outstanding.MinorUnits != 11000 {
		t.Fatalf("reviewed purchase query must show unposted authoritative state: %v error=%v", result, err)
	}
	for name, mutate := range map[string]func(*tammyv1.GetPurchaseAccountingRequest){
		"foreign organisation": func(r *tammyv1.GetPurchaseAccountingRequest) {
			r.OrganisationId = "018f0000-0000-7000-8000-000000009999"
		},
		"foreign document": func(r *tammyv1.GetPurchaseAccountingRequest) { r.DocumentId = "018f0000-0000-7000-8000-000000009999" },
		"expired session": func(r *tammyv1.GetPurchaseAccountingRequest) {
			r.Authentication.SessionId = "018f0000-0000-7000-8000-000000009999"
		},
		"malformed organisation": func(r *tammyv1.GetPurchaseAccountingRequest) { r.OrganisationId = "invalid" },
		"malformed document":     func(r *tammyv1.GetPurchaseAccountingRequest) { r.DocumentId = "invalid" },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := proto.Clone(request).(*tammyv1.GetPurchaseAccountingRequest)
			mutate(invalid)
			want := connect.CodeNotFound
			if name == "expired session" {
				want = connect.CodeUnauthenticated
			}
			if name == "malformed organisation" || name == "malformed document" {
				want = connect.CodeInvalidArgument
			}
			if _, err := server.purchases.GetPurchaseAccounting(context.Background(), brandingRequest(invalid, server.capability)); connect.CodeOf(err) != want {
				t.Fatalf("scope/admission error=%v want=%v", err, want)
			}
		})
	}
	page, err := server.purchases.ListPurchasePayments(context.Background(), brandingRequest(&tammyv1.ListPurchasePaymentsRequest{Authentication: server.authentication, OrganisationId: server.organisationID, DocumentId: document.Id, Page: &tammyv1.PageRequest{PageSize: 50}}, server.capability))
	if err != nil || len(page.Msg.Payments) != 0 || page.Msg.Page == nil || page.Msg.Page.ReturnedCount != 0 || page.Msg.Page.NextCursor != nil {
		t.Fatalf("unposted payment history=%v error=%v", page, err)
	}
}

func TestPurchaseQueryPresentsVerifiedOwnedSemanticDuplicateMatches(t *testing.T) {
	ctx := context.Background()
	server := newDocumentPageServer(t)
	docs := server.seed(t, 2, "Distinct retained originals for matching supplier invoices")
	documentsRepo, err := documents.NewRepository(server.module.db)
	if err != nil {
		t.Fatal(err)
	}
	for index, doc := range docs {
		candidate := proto.Clone(doc.Candidate).(*tammyv1.DocumentCandidate)
		candidate.InvoiceNumber = "MATCH-001"
		candidate.DocumentDate = &tammyv1.CivilDate{Year: 2026, Month: 9, Day: int32(index + 1)}
		candidate.Subtotal.MinorUnits = int64(index+1) * 10000
		candidate.Gst.MinorUnits = int64(index+1) * 1000
		candidate.Total.MinorUnits = int64(index+1) * 11000
		docs[index], err = documentsRepo.SaveReview(ctx, doc.Id, doc.Version, candidate, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
	}
	posted, err := server.purchases.PostReviewedPurchase(ctx, brandingRequest(server.postRequest(t, docs[0]), server.capability))
	if err != nil {
		t.Fatal(err)
	}
	before := purchaseTLSCounts(t, server)
	result, err := server.purchases.GetPurchaseAccounting(ctx, brandingRequest(&tammyv1.GetPurchaseAccountingRequest{Authentication: server.authentication, OrganisationId: server.organisationID, DocumentId: docs[1].Id}, server.capability))
	if err != nil || result.Msg.Accounting.Posted {
		t.Fatalf("unposted reviewed source query=%v error=%v", result, err)
	}
	field := result.Msg.ProtoReflect().Descriptor().Fields().ByName("duplicate_candidates")
	if field == nil || result.Msg.ProtoReflect().Get(field).List().Len() != 1 {
		t.Fatal("core-discovered duplicate match is unavailable for user selection")
	}
	candidate := result.Msg.ProtoReflect().Get(field).List().Get(0).Message()
	postingField := candidate.Descriptor().Fields().ByName("posting")
	ref := candidate.Get(postingField).Message().Interface().(*tammyv1.SourceRef)
	if ref.Type != "purchase_posting" || ref.Id != posted.Msg.Posting.Id || ref.Revision != 1 || !bytes.Equal(ref.ContentHash, posted.Msg.Posting.SourceHash) {
		t.Fatalf("selected owned posting provenance=%v", ref)
	}
	if candidate.Get(candidate.Descriptor().Fields().ByName("document_id")).String() != docs[0].Id || candidate.Get(candidate.Descriptor().Fields().ByName("gross")).Message().Interface().(*tammyv1.Money).MinorUnits != 11000 {
		t.Fatal("duplicate match lost retained document or exact original amount")
	}
	assertPurchaseTLSCounts(t, before, purchaseTLSCounts(t, server))
}
