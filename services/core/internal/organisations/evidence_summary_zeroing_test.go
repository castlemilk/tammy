//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package organisations

import (
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestCurrentVerificationSummaryClearsRetainedEvidence(t *testing.T) {
	content := []byte("sensitive-retained-evidence")
	record := VerificationRecord{
		Verification: &tammyv1.EntityVerification{
			OrganisationId: "018f0000-0000-7000-8000-000000000020",
			State:          tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_VERIFIED,
			ExpiresAt:      timestamppb.New(time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC)),
		},
		Evidence: &tammyv1.VerificationEvidence{Content: content},
	}

	summary, err := currentVerificationSummary(record, record.Verification.OrganisationId)
	if err != nil {
		t.Fatal(err)
	}
	if summary == nil || summary.State != record.Verification.State {
		t.Fatalf("summary = %#v", summary)
	}
	for index, value := range content {
		if value != 0 {
			t.Fatalf("evidence byte %d was not cleared", index)
		}
	}
}
