package system

import (
	"context"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/buildinfo"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	tammyv1connect "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1/tammyv1connect"
)

// Service reports Tammy system information.
type Service struct {
	info buildinfo.Info
}

var _ tammyv1connect.SystemServiceHandler = (*Service)(nil)

// NewService constructs a system service with immutable build information.
func NewService(info buildinfo.Info) *Service {
	return &Service{info: info}
}

// GetDiagnostics returns current system diagnostic information.
func (s *Service) GetDiagnostics(
	_ context.Context,
	_ *connect.Request[tammyv1.GetDiagnosticsRequest],
) (*connect.Response[tammyv1.GetDiagnosticsResponse], error) {
	return connect.NewResponse(&tammyv1.GetDiagnosticsResponse{
		ApiVersion:      "tammy.v1",
		CoreVersion:     s.info.Version,
		RuntimeMode:     tammyv1.RuntimeMode_RUNTIME_MODE_OFFLINE,
		NetworkRequired: false,
	}), nil
}
