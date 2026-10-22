//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/baspreparation"
)

func TestBASCommandErrorsPreserveCapacityAndConflictClassification(t *testing.T) {
	for _, test := range []struct {
		err  error
		code connect.Code
	}{
		{errors.Join(baspreparation.ErrInvalidCommand, baspreparation.ErrSourceBound), connect.CodeResourceExhausted},
		{baspreparation.ErrSnapshotBound, connect.CodeResourceExhausted},
		{errors.Join(baspreparation.ErrInvalidCommand, baspreparation.ErrCommandConflict), connect.CodeFailedPrecondition},
	} {
		if err := basPreparationConnectError(test.err); connect.CodeOf(err) != test.code {
			t.Fatalf("command error=%v mapped to %v; want %v", test.err, err, test.code)
		}
	}
}
