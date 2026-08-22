// Package evte contains the intentionally unavailable EVTE component boundary.
package evte

import (
	"context"

	"github.com/tammyapp/tammy/services/sbr-helper/internal/protocol"
)

// Adapter has no endpoint or runtime implementation configuration. A signed,
// approved component will replace it in a later registration-gated bundle.
type Adapter struct{}

func (Adapter) Execute(ctx context.Context, request protocol.Request) protocol.Response {
	if ctx.Err() != nil {
		return protocol.NewErrorResponse(request.RequestID, protocol.StableErrorHelperUnavailable)
	}
	return protocol.NewErrorResponse(request.RequestID, protocol.StableErrorComponentUnavailable)
}
