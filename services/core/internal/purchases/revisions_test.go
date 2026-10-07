package purchases

import (
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/revisions"
)

func TestPurchaseRevisionDomainsSelectOneReservation(t *testing.T) {
	for _, test := range []struct {
		command string
		journal bool
		want    revisions.Domains
	}{
		{"PostReviewedPurchase", true, revisions.Domains{Ledger: true, TaxSource: true}},
		{"PostReviewedPurchase", false, revisions.Domains{TaxSource: true}},
		{"RecordPurchasePayment", true, revisions.Domains{Ledger: true, Settlement: true, TaxSource: true}},
		{"RecordPurchaseTaxEvidence", false, revisions.Domains{TaxSource: true}},
		{"AttributePurchaseGst", true, revisions.Domains{Ledger: true, TaxSource: true}},
		{"DeferPurchaseGstClaim", false, revisions.Domains{TaxSource: true}},
		{"ResolveLegacyPurchaseReview", false, revisions.Domains{TaxSource: true}},
	} {
		domains, err := PurchaseRevisionDomains("tammy.v1.PurchaseService."+test.command, test.journal)
		if err != nil || domains != test.want {
			t.Errorf("%s journal=%t: %#v,%v", test.command, test.journal, domains, err)
		}
	}
	for _, test := range []struct {
		command string
		journal bool
	}{{"Unknown", false}, {"RecordPurchasePayment", false}, {"AttributePurchaseGst", false}, {"RecordPurchaseTaxEvidence", true}, {"DeferPurchaseGstClaim", true}, {"ResolveLegacyPurchaseReview", true}} {
		if _, err := PurchaseRevisionDomains("tammy.v1.PurchaseService."+test.command, test.journal); err == nil {
			t.Errorf("invalid revision selection admitted: %#v", test)
		}
	}
}
