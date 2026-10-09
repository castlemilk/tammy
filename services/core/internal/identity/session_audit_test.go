package identity

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
	"google.golang.org/protobuf/proto"
)

type typedSessionAuditProbe struct {
	*recordingIdentityAudit
	operation string
	user      *tammyv1.User
	session   *tammyv1.Session
	failure   error
}

func (a *typedSessionAuditProbe) RecordSessionStarted(_ context.Context, _ workspace.MutationExecutor, operation string, user *tammyv1.User, session *tammyv1.Session) error {
	a.operation = operation
	a.user = proto.Clone(user).(*tammyv1.User)
	a.session = proto.Clone(session).(*tammyv1.Session)
	return a.failure
}

func TestSignInSuppliesActualTypedSessionAuditBeforeCommit(t *testing.T) {
	harness := newIdentityHarness(t)
	ctx := context.Background()
	admin, err := harness.service.BootstrapAdministrator(ctx, "admin@example.test", "Admin", []byte("admin-password-long-enough"))
	if err != nil {
		t.Fatal(err)
	}
	probe := &typedSessionAuditProbe{recordingIdentityAudit: harness.audit}
	harness.service.audit = probe
	response, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{Username: admin.Username, Password: secret("admin-password-long-enough")}))
	if err != nil {
		t.Fatal(err)
	}
	if probe.operation != "tammy.v1.IdentityService.SignIn" || probe.user == nil || probe.session == nil || !proto.Equal(probe.user, response.Msg.User) || !proto.Equal(probe.session, response.Msg.Session) {
		t.Fatalf("typed session audit omitted actual committed projections: operation=%q user=%v session=%v", probe.operation, probe.user, probe.session)
	}
	probe.failure = errors.New("typed session audit rejected")
	before, err := harness.repository.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if response, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{Username: admin.Username, Password: secret("admin-password-long-enough")})); !errors.Is(err, probe.failure) || response != nil {
		t.Fatalf("typed audit failure did not reject sign in: %v", err)
	}
	after, err := harness.repository.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Sessions) != len(before.Sessions) {
		t.Fatal("typed audit failure committed a session")
	}
}
