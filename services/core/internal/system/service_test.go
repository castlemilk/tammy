package system_test

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/buildinfo"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/system"
)

func TestService_GetDiagnostics(t *testing.T) {
	tests := []struct {
		name string
		info buildinfo.Info
	}{
		{
			name: "reports offline core diagnostics",
			info: buildinfo.Info{Version: "0.1.0-test"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := system.NewService(tt.info)
			got, err := service.GetDiagnostics(
				context.Background(),
				connect.NewRequest(&tammyv1.GetDiagnosticsRequest{}),
			)
			if err != nil {
				t.Fatalf("GetDiagnostics() error = %v", err)
			}
			if got.Msg.GetApiVersion() != "tammy.v1" ||
				got.Msg.GetCoreVersion() != "0.1.0-test" ||
				got.Msg.GetRuntimeMode() != tammyv1.RuntimeMode_RUNTIME_MODE_OFFLINE ||
				got.Msg.GetNetworkRequired() {
				t.Fatalf("unexpected diagnostics: %+v", got.Msg)
			}
		})
	}
}
