package idempotency

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
)

func TestElectorRequiresInjectedClockAndFreshObserverAndUsesWorkspaceLifetimeScope(t *testing.T) {
	if elector, err := NewElector(Config{}); !errors.Is(err, ErrInvalidElection) || elector != nil {
		t.Fatalf("missing clock elector=%v err=%v", elector, err)
	}
	configuredClock := clock.Func(func() time.Time {
		return time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)
	})
	if elector, err := NewElector(Config{Clock: configuredClock}); !errors.Is(err, ErrInvalidElection) || elector != nil {
		t.Fatalf("missing fresh observer elector=%v err=%v", elector, err)
	}
	elector, err := NewElector(Config{Clock: configuredClock, Observe: func(context.Context, Scope) (Record, error) {
		return Record{}, ErrRepository
	}})
	if err != nil || elector == nil {
		t.Fatalf("injected clock elector=%v err=%v", elector, err)
	}
	valid := Scope{WorkspaceID: "workspace", ActorUserID: "actor", RPCName: "tammy.v1.Service.Command", OperationKey: "operation"}
	if !validScope(valid) {
		t.Fatal("complete persistent-command scope was rejected")
	}
	for _, invalid := range []Scope{
		{ActorUserID: valid.ActorUserID, RPCName: valid.RPCName, OperationKey: valid.OperationKey},
		{WorkspaceID: valid.WorkspaceID, RPCName: valid.RPCName, OperationKey: valid.OperationKey},
		{WorkspaceID: valid.WorkspaceID, ActorUserID: valid.ActorUserID, OperationKey: valid.OperationKey},
		{WorkspaceID: valid.WorkspaceID, ActorUserID: valid.ActorUserID, RPCName: valid.RPCName},
	} {
		if validScope(invalid) {
			t.Fatalf("incomplete scope accepted: %#v", invalid)
		}
	}
}
