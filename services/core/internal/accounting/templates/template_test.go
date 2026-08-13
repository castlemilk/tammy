package templates_test

import (
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

func TestAUSmallBusinessV1ContainsEveryRequiredProtectedAccount(t *testing.T) {
	template, err := accounting.LoadAUSmallBusinessV1()
	if err != nil {
		t.Fatal(err)
	}
	required := map[string]bool{
		"bank": false, "accounts_receivable": false, "accounts_payable": false,
		"gst_receivable_current": false, "gst_payable_current": false,
		"gst_input_deferred": false, "gst_output_deferred": false,
		"gst_evidence_suspense": false, "gst_adjustment": false,
		"current_year_earnings": false, "retained_earnings": false, "opening_equity": false,
	}
	codes := make(map[string]struct{}, len(template.Accounts))
	for _, account := range template.Accounts {
		if _, duplicate := codes[account.Code]; duplicate {
			t.Fatalf("duplicate code %q", account.Code)
		}
		codes[account.Code] = struct{}{}
		if _, tracked := required[account.Key]; tracked {
			required[account.Key] = true
			if account.Designation == tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY {
				t.Fatalf("required account %q is ordinary", account.Key)
			}
		}
	}
	for key, found := range required {
		if !found {
			t.Errorf("missing required account %q", key)
		}
	}
}
