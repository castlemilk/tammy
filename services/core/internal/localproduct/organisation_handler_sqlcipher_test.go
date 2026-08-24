//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

func TestOrganisationHandlerDoesNotLeaveEntityVerificationUnimplemented(t *testing.T) {
	_, err := (&organisationHandler{}).RecordEntityVerification(
		context.Background(),
		connect.NewRequest(&tammyv1.RecordEntityVerificationRequest{}),
	)
	if connect.CodeOf(err) == connect.CodeUnimplemented {
		t.Fatal("RecordEntityVerification inherited the generated unimplemented handler")
	}
}
