package evte

import (
	"context"
	"reflect"
	"testing"

	"github.com/tammyapp/tammy/services/sbr-helper/internal/protocol"
)

func TestAdapterIsPermanentlyUnavailableAndHasNoRuntimeConfiguration(t *testing.T) {
	adapter := Adapter{}
	request := protocol.Request{RequestID: "018bcfe5-6800-7000-8000-000000000001"}
	response := adapter.Execute(context.Background(), request)
	if response.RequestID != request.RequestID || response.Outcome != protocol.OutcomeError || response.StableErrorCode != protocol.StableErrorComponentUnavailable {
		t.Fatalf("response = %#v", response)
	}
	if reflect.TypeOf(adapter).NumField() != 0 {
		t.Fatal("unavailable EVTE adapter must not expose endpoint or implementation switches")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	response = adapter.Execute(ctx, request)
	if response.StableErrorCode != protocol.StableErrorHelperUnavailable {
		t.Fatalf("cancelled response = %#v", response)
	}
}
