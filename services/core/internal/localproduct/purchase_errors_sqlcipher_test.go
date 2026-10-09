//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/invoices"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"google.golang.org/protobuf/proto"
)

func TestPurchaseCommandCorrectiveErrorsHaveSafeBoundedDetails(t *testing.T) {
	for _, test := range []struct {
		err         error
		code        string
		remediation string
	}{
		{err: app.ErrLocalAuditCoverageUnavailable, code: "PURCHASE_AUDIT_COVERAGE_UNAVAILABLE", remediation: "Use a workspace with verified audit coverage; this app cannot establish coverage for this workspace."},
		{err: purchases.ErrPayment, code: "PURCHASE_PAYMENT_INVALID"},
		{err: purchases.ErrConflict, code: "PURCHASE_STALE"},
		{err: purchases.ErrDuplicatePurchase, code: "PURCHASE_DUPLICATE"},
		{err: purchases.ErrPriorAccounting, code: "PURCHASE_PRIOR_ACCOUNTING"},
		{err: purchases.ErrUnsupportedException, code: "PURCHASE_EVIDENCE_UNSUPPORTED"},
		{err: accounting.ErrClosedPeriod, code: "PURCHASE_PERIOD_CLOSED"},
		{err: invoices.ErrGSTBasisHistory, code: "PURCHASE_GST_BASIS_CHANGED"},
		{err: purchases.ErrClaimBalance, code: "PURCHASE_CLAIM_BALANCE_UNAVAILABLE"},
	} {
		t.Run(test.code, func(t *testing.T) {
			err := purchaseError(errors.Join(test.err, errors.New("private SQL /Users/private/secret.pdf")), func() (string, error) { return "018f0000-0000-7000-8000-000000008099", nil })
			var failure *connect.Error
			if !errors.As(err, &failure) {
				t.Fatal("missing connect error")
			}
			if len(failure.Details()) != 1 {
				t.Fatalf("corrective error has no typed guidance: %v", err)
			}
			if test.err == purchases.ErrClaimBalance && failure.Code() != connect.CodeFailedPrecondition {
				t.Fatalf("unavailable claim balance reported as %v", failure.Code())
			}
			value, e := failure.Details()[0].Value()
			if e != nil {
				t.Fatal(e)
			}
			detail, ok := value.(*tammyv1.ValidationErrorDetail)
			if !ok || detail.Context == nil || detail.Context.Code != test.code || detail.Context.SafeSummary == "" || detail.Context.Remediation == "" || detail.Context.Retry != tammyv1.RetryClassification_RETRY_CLASSIFICATION_AFTER_USER_ACTION || proto.Size(detail) > 1024 {
				t.Fatalf("invalid safe guidance: %v", value)
			}
			if failure.Message() != detail.Context.SafeSummary {
				t.Fatalf("error summary does not match bounded detail: %v", failure.Message())
			}
			if test.remediation != "" && detail.Context.Remediation != test.remediation {
				t.Fatalf("remediation=%q, want %q", detail.Context.Remediation, test.remediation)
			}
		})
	}
	if err := purchaseError(errors.New("private SQL /Users/private/secret.pdf")); err.Error() != "internal: local product: ledger module unavailable" {
		t.Fatalf("internal failure leaked: %v", err)
	}
}
