package contracts_test

import (
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	tammyv1connect "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1/tammyv1connect"
)

func TestSystemContract(t *testing.T) {
	if tammyv1connect.SystemServiceName != "tammy.v1.SystemService" {
		t.Fatalf("unexpected service name %q", tammyv1connect.SystemServiceName)
	}
	response := &tammyv1.GetDiagnosticsResponse{
		ApiVersion:      "tammy.v1",
		CoreVersion:     "test",
		RuntimeMode:     tammyv1.RuntimeMode_RUNTIME_MODE_OFFLINE,
		NetworkRequired: false,
	}
	if response.GetRuntimeMode() != tammyv1.RuntimeMode_RUNTIME_MODE_OFFLINE {
		t.Fatal("offline runtime enum is not usable")
	}
}
