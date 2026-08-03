package identity

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
	"github.com/tammyapp/tammy/services/core/internal/platform/faults"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
	"google.golang.org/protobuf/proto"
)

type identityHarness struct {
	service     *Service
	repository  Repository
	config      Config
	audit       *recordingIdentityAudit
	now         *time.Time
	attemptPath string
}

type recordingIdentityAudit struct {
	counts map[string]int
	fail   error
}

type testWorkspaceSessionLifecycle struct {
	within    func(context.Context, workspace.MutationExecutor) error
	committed func(context.Context) error
}

func (lifecycle *testWorkspaceSessionLifecycle) SessionStartedWithin(ctx context.Context, executor workspace.MutationExecutor, _ string) error {
	if lifecycle.within != nil {
		return lifecycle.within(ctx, executor)
	}
	return nil
}

func (lifecycle *testWorkspaceSessionLifecycle) SessionStartedCommitted(ctx context.Context) error {
	if lifecycle.committed != nil {
		return lifecycle.committed(ctx)
	}
	return nil
}

func (audit *recordingIdentityAudit) Record(_ context.Context, _ workspace.MutationExecutor, mutation, _ string) error {
	if audit.fail != nil {
		return audit.fail
	}
	audit.counts[mutation]++
	return nil
}

func newIdentityHarness(t *testing.T) identityHarness {
	t.Helper()
	now := time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)
	source := clock.Func(func() time.Time { return now })
	policy, err := workspace.NewPasswordPolicy(nil, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	generator, err := ids.NewGenerator(source, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	attemptPath := filepath.Join(t.TempDir(), "attempts.journal")
	journal, err := workspace.NewAttemptJournal(attemptPath, bytes.Repeat([]byte{7}, 32), source,
		"identity/test-service", workspace.NewMemoryAnchorStore())
	if err != nil {
		t.Fatal(err)
	}
	repository := NewMemoryRepository()
	audit := &recordingIdentityAudit{counts: make(map[string]int)}
	config := Config{
		Repository: repository, Passwords: policy, Clock: source,
		Random: rand.Reader, IDs: generator, Attempts: journal,
		FactorEncryptionKey: bytes.Repeat([]byte{9}, 32),
		Audit:               audit,
		SessionLifecycle:    &testWorkspaceSessionLifecycle{},
	}
	service, err := NewService(config)
	if err != nil {
		t.Fatal(err)
	}
	return identityHarness{service: service, repository: repository, config: config, audit: audit, now: &now, attemptPath: attemptPath}
}

func secret(value string) *tammyv1.SecretInput { return &tammyv1.SecretInput{Utf8: []byte(value)} }

func TestIdentityServiceActivationAndLockout(t *testing.T) {
	harness := newIdentityHarness(t)
	ctx := context.Background()
	admin, err := harness.service.BootstrapAdministrator(ctx, "admin@example.test", "Admin", []byte("admin-password-long-enough"))
	if err != nil {
		t.Fatal(err)
	}
	if admin.State != tammyv1.UserState_USER_STATE_ACTIVE || len(admin.Roles) != 1 || admin.Roles[0] != tammyv1.Role_ROLE_WORKSPACE_ADMIN {
		t.Fatalf("unexpected administrator: %v", admin)
	}
	for attempt := 0; attempt < 5; attempt++ {
		_, err = harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{Username: admin.Username, Password: secret("wrong-password-long-enough")}))
		if !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
			t.Fatalf("attempt %d returned %v", attempt+1, err)
		}
	}
	_, err = harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{Username: admin.Username, Password: secret("admin-password-long-enough")}))
	if !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("locked sign-in returned %v", err)
	}
	*harness.now = harness.now.Add(15 * time.Minute)
	response, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{Username: admin.Username, Password: secret("admin-password-long-enough")}))
	if err != nil {
		t.Fatal(err)
	}
	if response.Msg.Session.State != tammyv1.SessionState_SESSION_STATE_ACTIVE || !response.Msg.Session.ExpiresAt.AsTime().Equal(harness.now.Add(30*time.Minute)) {
		t.Fatalf("unexpected session: %v", response.Msg.Session)
	}
}

func TestSignInSessionLifecycleFailureRollsBackSessionAndAudit(t *testing.T) {
	harness := newIdentityHarness(t)
	ctx := context.Background()
	admin, err := harness.service.BootstrapAdministrator(ctx, "admin@example.test", "Admin", []byte("admin-password-long-enough"))
	if err != nil {
		t.Fatal(err)
	}
	lifecycleErr := errors.New("workspace authentication transition failed")
	lifecycle := harness.config.SessionLifecycle.(*testWorkspaceSessionLifecycle)
	lifecycle.within = func(context.Context, workspace.MutationExecutor) error { return lifecycleErr }
	response, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{Username: admin.Username, Password: secret("admin-password-long-enough")}))
	if !errors.Is(err, lifecycleErr) || response != nil {
		t.Fatalf("failed lifecycle returned response_present=%t err=%v", response != nil, err)
	}
	state, err := harness.repository.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Sessions) != 0 || harness.audit.counts["session_started"] != 0 {
		t.Fatalf("lifecycle rollback leaked sessions=%d audit=%d", len(state.Sessions), harness.audit.counts["session_started"])
	}
}

func TestSignInPersistsOneSessionAuditAndRunsPostCommitLifecycle(t *testing.T) {
	harness := newIdentityHarness(t)
	ctx := context.Background()
	admin, err := harness.service.BootstrapAdministrator(ctx, "admin@example.test", "Admin", []byte("admin-password-long-enough"))
	if err != nil {
		t.Fatal(err)
	}
	withinCalls := 0
	committedCalls := 0
	lifecycle := harness.config.SessionLifecycle.(*testWorkspaceSessionLifecycle)
	lifecycle.within = func(context.Context, workspace.MutationExecutor) error { withinCalls++; return nil }
	lifecycle.committed = func(context.Context) error { committedCalls++; return nil }
	response, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{Username: admin.Username, Password: secret("admin-password-long-enough")}))
	if err != nil {
		t.Fatal(err)
	}
	if response.Msg.Session.State != tammyv1.SessionState_SESSION_STATE_ACTIVE || withinCalls != 1 || committedCalls != 1 ||
		harness.audit.counts["session_started"] != 1 {
		t.Fatalf("session lifecycle: state=%v within=%d committed=%d audit=%d", response.Msg.Session.State, withinCalls, committedCalls,
			harness.audit.counts["session_started"])
	}
}

func TestSignInUsesOneFixedDummyVerifierForUnknownAndIneligibleUsers(t *testing.T) {
	harness := newIdentityHarness(t)
	ctx := context.Background()
	admin, err := harness.service.BootstrapAdministrator(ctx, "admin@example.test", "Admin", []byte("admin-password-long-enough"))
	if err != nil {
		t.Fatal(err)
	}
	if err := harness.service.Close(); err != nil {
		t.Fatal(err)
	}
	var seen []workspace.PasswordVerifier
	harness.config.PasswordVerify = func(secret []byte, verifier workspace.PasswordVerifier) bool {
		seen = append(seen, workspace.PasswordVerifier{PolicyVersion: verifier.PolicyVersion, MemoryKiB: verifier.MemoryKiB,
			Iterations: verifier.Iterations, Parallelism: verifier.Parallelism, Salt: append([]byte(nil), verifier.Salt...), Digest: append([]byte(nil), verifier.Digest...)})
		return false
	}
	harness.service, err = NewService(harness.config)
	if err != nil {
		t.Fatal(err)
	}
	for _, username := range []string{"missing-one@example.test", "missing-two@example.test"} {
		if _, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{Username: username, Password: secret("unknown-password-long-enough")})); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
			t.Fatalf("unknown sign-in returned %v", err)
		}
	}
	state, err := harness.repository.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	state.Users[admin.Id].State = tammyv1.UserState_USER_STATE_PENDING_ACTIVATION
	if err := harness.repository.Save(ctx, state); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{Username: admin.Username, Password: secret("admin-password-long-enough")})); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("ineligible sign-in returned %v", err)
	}
	if len(seen) != 3 {
		t.Fatalf("Argon2 verifier calls = %d, want exactly one per sign-in", len(seen))
	}
	for index := 1; index < len(seen); index++ {
		if !bytes.Equal(seen[0].Salt, seen[index].Salt) || !bytes.Equal(seen[0].Digest, seen[index].Digest) {
			t.Fatalf("dummy verifier changed between attempts %d and %d", 0, index)
		}
	}
}

func TestPersistentIdentityCommandsRollbackWithAuditFailure(t *testing.T) {
	t.Run("create user", func(t *testing.T) {
		harness := newIdentityHarness(t)
		ctx := context.Background()
		admin, err := harness.service.BootstrapAdministrator(ctx, "admin@example.test", "Admin", []byte("admin-password-long-enough"))
		if err != nil {
			t.Fatal(err)
		}
		signed, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{Username: admin.Username, Password: secret("admin-password-long-enough")}))
		if err != nil {
			t.Fatal(err)
		}
		request := &tammyv1.CreateUserRequest{CommandContext: &tammyv1.CommandContext{
			IdempotencyKey: "01890f3c-7b2e-7cc4-98c4-dc0c0c073971", Authentication: &tammyv1.AuthenticationContext{ActorUserId: admin.Id, SessionId: signed.Msg.Session.Id},
		}, Username: "new-user@example.test", DisplayName: "New User", Roles: []tammyv1.Role{tammyv1.Role_ROLE_AUDITOR}}
		auditErr := errors.New("audit unavailable")
		harness.audit.fail = auditErr
		if _, err := harness.service.CreateUser(ctx, connect.NewRequest(request)); !errors.Is(err, auditErr) {
			t.Fatalf("audit failure returned %v", err)
		}
		state, err := harness.repository.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(state.Users) != 1 || len(state.Idempotency) != 0 {
			t.Fatalf("create rollback leaked users=%d idempotency=%d", len(state.Users), len(state.Idempotency))
		}
		harness.audit.fail = nil
		created, err := harness.service.CreateUser(ctx, connect.NewRequest(proto.Clone(request).(*tammyv1.CreateUserRequest)))
		if err != nil {
			t.Fatal(err)
		}
		beforeReplay, err := harness.repository.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}
		beforeSession := beforeReplay.Sessions[signed.Msg.Session.Id]
		*harness.now = harness.now.Add(time.Minute)
		replayed, err := harness.service.CreateUser(ctx, connect.NewRequest(proto.Clone(request).(*tammyv1.CreateUserRequest)))
		if err != nil || replayed.Msg.User.Id != created.Msg.User.Id || harness.audit.counts["user_created"] != 1 {
			t.Fatalf("retry/replay result=%v audit=%d err=%v", replayed, harness.audit.counts["user_created"], err)
		}
		conflicting := proto.Clone(request).(*tammyv1.CreateUserRequest)
		conflicting.DisplayName = "Changed User"
		if _, err := harness.service.CreateUser(ctx, connect.NewRequest(conflicting)); !errors.Is(err, faults.New(faults.CodeIdempotencyConflict, nil)) {
			t.Fatalf("create conflict returned %v", err)
		}
		afterReplay, err := harness.repository.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}
		afterSession := afterReplay.Sessions[signed.Msg.Session.Id]
		if !afterSession.LastActive.Equal(beforeSession.LastActive) || !afterSession.ExpiresAt.Equal(beforeSession.ExpiresAt) ||
			len(afterReplay.Users) != len(beforeReplay.Users) || len(afterReplay.Idempotency) != len(beforeReplay.Idempotency) {
			t.Fatalf("create replay/conflict mutated session=%+v→%+v users=%d→%d idempotency=%d→%d", beforeSession, afterSession,
				len(beforeReplay.Users), len(afterReplay.Users), len(beforeReplay.Idempotency), len(afterReplay.Idempotency))
		}
	})

	t.Run("change password", func(t *testing.T) {
		harness := newIdentityHarness(t)
		ctx := context.Background()
		admin, err := harness.service.BootstrapAdministrator(ctx, "admin@example.test", "Admin", []byte("admin-password-long-enough"))
		if err != nil {
			t.Fatal(err)
		}
		signed, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{Username: admin.Username, Password: secret("admin-password-long-enough")}))
		if err != nil {
			t.Fatal(err)
		}
		request := &tammyv1.ChangePasswordRequest{CommandContext: &tammyv1.CommandContext{
			IdempotencyKey: "01890f3c-7b2e-7cc4-98c4-dc0c0c073972", Authentication: &tammyv1.AuthenticationContext{ActorUserId: admin.Id, SessionId: signed.Msg.Session.Id},
		}, ExpectedVersion: admin.Version, CurrentPassword: secret("admin-password-long-enough"), NewPassword: secret("replacement-password-long-enough")}
		auditErr := errors.New("audit unavailable")
		harness.audit.fail = auditErr
		if _, err := harness.service.ChangePassword(ctx, connect.NewRequest(request)); !errors.Is(err, auditErr) {
			t.Fatalf("audit failure returned %v", err)
		}
		state, err := harness.repository.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if state.Users[admin.Id].Version != admin.Version || state.Sessions[signed.Msg.Session.Id].State != tammyv1.SessionState_SESSION_STATE_ACTIVE || len(state.Idempotency) != 0 {
			t.Fatal("password audit failure committed user/session/idempotency state")
		}
		harness.audit.fail = nil
		changed, err := harness.service.ChangePassword(ctx, connect.NewRequest(proto.Clone(request).(*tammyv1.ChangePasswordRequest)))
		if err != nil || changed.Msg.User.Version != admin.Version+1 || harness.audit.counts["password_changed"] != 1 {
			t.Fatalf("retry result=%v audit=%d err=%v", changed, harness.audit.counts["password_changed"], err)
		}
		beforeReplay, err := harness.repository.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}
		beforeSession := beforeReplay.Sessions[signed.Msg.Session.Id]
		*harness.now = harness.now.Add(time.Minute)
		replayed, err := harness.service.ChangePassword(ctx, connect.NewRequest(proto.Clone(request).(*tammyv1.ChangePasswordRequest)))
		if err != nil || replayed.Msg.User.Version != changed.Msg.User.Version || harness.audit.counts["password_changed"] != 1 {
			t.Fatalf("replay result=%v audit=%d err=%v", replayed, harness.audit.counts["password_changed"], err)
		}
		conflicting := proto.Clone(request).(*tammyv1.ChangePasswordRequest)
		conflicting.NewPassword = secret("conflicting-password-long-enough")
		if _, err := harness.service.ChangePassword(ctx, connect.NewRequest(conflicting)); !errors.Is(err, faults.New(faults.CodeIdempotencyConflict, nil)) {
			t.Fatalf("password conflict returned %v", err)
		}
		afterReplay, err := harness.repository.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}
		afterSession := afterReplay.Sessions[signed.Msg.Session.Id]
		if !afterSession.LastActive.Equal(beforeSession.LastActive) || !afterSession.ExpiresAt.Equal(beforeSession.ExpiresAt) ||
			len(afterReplay.Idempotency) != len(beforeReplay.Idempotency) {
			t.Fatalf("password replay/conflict mutated session=%+v→%+v idempotency=%d→%d", beforeSession, afterSession,
				len(beforeReplay.Idempotency), len(afterReplay.Idempotency))
		}
	})

	t.Run("assign roles", func(t *testing.T) {
		harness := newIdentityHarness(t)
		ctx := context.Background()
		actor, err := harness.service.BootstrapAdministrator(ctx, "actor@example.test", "Actor", []byte("actor-password-long-enough"))
		if err != nil {
			t.Fatal(err)
		}
		target, err := harness.service.BootstrapAdministrator(ctx, "target@example.test", "Target", []byte("target-password-long-enough"))
		if err != nil {
			t.Fatal(err)
		}
		signed, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{Username: actor.Username, Password: secret("actor-password-long-enough")}))
		if err != nil {
			t.Fatal(err)
		}
		request := &tammyv1.AssignRolesRequest{CommandContext: &tammyv1.CommandContext{
			IdempotencyKey: "01890f3c-7b2e-7cc4-98c4-dc0c0c073973", Authentication: &tammyv1.AuthenticationContext{ActorUserId: actor.Id, SessionId: signed.Msg.Session.Id},
		}, UserId: target.Id, ExpectedVersion: target.Version, Roles: []tammyv1.Role{tammyv1.Role_ROLE_AUDITOR}}
		auditErr := errors.New("audit unavailable")
		harness.audit.fail = auditErr
		if _, err := harness.service.AssignRoles(ctx, connect.NewRequest(request)); !errors.Is(err, auditErr) {
			t.Fatalf("audit failure returned %v", err)
		}
		state, err := harness.repository.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if state.Users[target.Id].Version != target.Version || !hasRole(state.Users[target.Id].Roles, tammyv1.Role_ROLE_WORKSPACE_ADMIN) || len(state.Idempotency) != 0 {
			t.Fatal("role audit failure committed target/idempotency state")
		}
		harness.audit.fail = nil
		assigned, err := harness.service.AssignRoles(ctx, connect.NewRequest(proto.Clone(request).(*tammyv1.AssignRolesRequest)))
		if err != nil {
			t.Fatal(err)
		}
		beforeReplay, err := harness.repository.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}
		beforeSession := beforeReplay.Sessions[signed.Msg.Session.Id]
		*harness.now = harness.now.Add(time.Minute)
		replayed, err := harness.service.AssignRoles(ctx, connect.NewRequest(proto.Clone(request).(*tammyv1.AssignRolesRequest)))
		if err != nil || replayed.Msg.User.Version != assigned.Msg.User.Version || harness.audit.counts["roles_assigned"] != 1 {
			t.Fatalf("roles replay response=%v audit=%d err=%v", replayed, harness.audit.counts["roles_assigned"], err)
		}
		conflicting := proto.Clone(request).(*tammyv1.AssignRolesRequest)
		conflicting.Roles = []tammyv1.Role{tammyv1.Role_ROLE_BUSINESS_PREPARER}
		if _, err := harness.service.AssignRoles(ctx, connect.NewRequest(conflicting)); !errors.Is(err, faults.New(faults.CodeIdempotencyConflict, nil)) {
			t.Fatalf("roles conflict returned %v", err)
		}
		afterReplay, err := harness.repository.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}
		afterSession := afterReplay.Sessions[signed.Msg.Session.Id]
		if !afterSession.LastActive.Equal(beforeSession.LastActive) || !afterSession.ExpiresAt.Equal(beforeSession.ExpiresAt) ||
			afterReplay.Users[target.Id].Version != beforeReplay.Users[target.Id].Version || len(afterReplay.Idempotency) != len(beforeReplay.Idempotency) {
			t.Fatalf("roles replay/conflict mutated session=%+v→%+v target_version=%d→%d idempotency=%d→%d", beforeSession,
				afterSession, beforeReplay.Users[target.Id].Version, afterReplay.Users[target.Id].Version,
				len(beforeReplay.Idempotency), len(afterReplay.Idempotency))
		}
	})

	t.Run("sign out", func(t *testing.T) {
		harness := newIdentityHarness(t)
		ctx := context.Background()
		admin, err := harness.service.BootstrapAdministrator(ctx, "admin@example.test", "Admin", []byte("admin-password-long-enough"))
		if err != nil {
			t.Fatal(err)
		}
		signed, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{Username: admin.Username, Password: secret("admin-password-long-enough")}))
		if err != nil {
			t.Fatal(err)
		}
		harness.audit.fail = errors.New("audit unavailable")
		request := &tammyv1.SignOutRequest{Authentication: &tammyv1.AuthenticationContext{ActorUserId: admin.Id, SessionId: signed.Msg.Session.Id}}
		if _, err := harness.service.SignOut(ctx, connect.NewRequest(request)); !errors.Is(err, harness.audit.fail) {
			t.Fatalf("audit failure returned %v", err)
		}
		state, err := harness.repository.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if state.Sessions[signed.Msg.Session.Id].State != tammyv1.SessionState_SESSION_STATE_ACTIVE {
			t.Fatal("sign-out audit failure ended the session")
		}
	})
}

func TestIdentityServiceCreateAndActivateUser(t *testing.T) {
	harness := newIdentityHarness(t)
	ctx := context.Background()
	admin, err := harness.service.BootstrapAdministrator(ctx, "admin@example.test", "Admin", []byte("admin-password-long-enough"))
	if err != nil {
		t.Fatal(err)
	}
	signedIn, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{Username: admin.Username, Password: secret("admin-password-long-enough")}))
	if err != nil {
		t.Fatal(err)
	}
	authentication := &tammyv1.AuthenticationContext{ActorUserId: admin.Id, SessionId: signedIn.Msg.Session.Id}
	created, err := harness.service.CreateUser(ctx, connect.NewRequest(&tammyv1.CreateUserRequest{
		CommandContext: &tammyv1.CommandContext{IdempotencyKey: "01890f3c-7b2e-7cc4-98c4-dc0c0c073901", Authentication: authentication},
		Username:       "lodger@example.test", DisplayName: "Lodger", Roles: []tammyv1.Role{tammyv1.Role_ROLE_BUSINESS_LODGER},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if created.Msg.User.State != tammyv1.UserState_USER_STATE_PENDING_ACTIVATION ||
		!created.Msg.ExpiresAt.AsTime().Equal(harness.now.Add(24*time.Hour)) || len(created.Msg.ActivationCode.Utf8) == 0 {
		t.Fatalf("unexpected pending user: %v", created.Msg)
	}
	withinCalls := 0
	committedCalls := 0
	lifecycle := harness.config.SessionLifecycle.(*testWorkspaceSessionLifecycle)
	lifecycle.within = func(context.Context, workspace.MutationExecutor) error { withinCalls++; return nil }
	lifecycle.committed = func(context.Context) error { committedCalls++; return nil }
	activated, err := harness.service.ActivateUser(ctx, connect.NewRequest(&tammyv1.ActivateUserRequest{
		Username: created.Msg.User.Username, ActivationCode: &tammyv1.SecretInput{Utf8: append([]byte(nil), created.Msg.ActivationCode.Utf8...)},
		NewPassword: secret("lodger-password-long-enough"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if activated.Msg.User.State != tammyv1.UserState_USER_STATE_ACTIVE || activated.Msg.Session.UserId != created.Msg.User.Id {
		t.Fatalf("unexpected activation: %v", activated.Msg)
	}
	if withinCalls != 1 || committedCalls != 1 || harness.audit.counts["user_activated"] != 1 {
		t.Fatalf("activation lifecycle within=%d committed=%d audit=%d", withinCalls, committedCalls, harness.audit.counts["user_activated"])
	}
	replayed, err := harness.service.ActivateUser(ctx, connect.NewRequest(&tammyv1.ActivateUserRequest{
		Username: created.Msg.User.Username, ActivationCode: &tammyv1.SecretInput{Utf8: append([]byte(nil), created.Msg.ActivationCode.Utf8...)},
		NewPassword: secret("lodger-password-long-enough"),
	}))
	if err != nil || replayed.Msg.Session.Id != activated.Msg.Session.Id || replayed.Msg.User.Version != activated.Msg.User.Version {
		t.Fatalf("terminal activation challenge: %v", err)
	}
	if withinCalls != 1 || committedCalls != 1 || harness.audit.counts["user_activated"] != 1 {
		t.Fatalf("activation replay duplicated lifecycle within=%d committed=%d audit=%d", withinCalls, committedCalls,
			harness.audit.counts["user_activated"])
	}
}

func TestActivateUserLifecycleOrAuditFailureRollsBack(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		configure func(identityHarness, error)
	}{
		{name: "workspace lifecycle", configure: func(harness identityHarness, injected error) {
			harness.config.SessionLifecycle.(*testWorkspaceSessionLifecycle).within = func(context.Context, workspace.MutationExecutor) error {
				return injected
			}
		}},
		{name: "identity audit", configure: func(harness identityHarness, injected error) { harness.audit.fail = injected }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			harness := newIdentityHarness(t)
			ctx := context.Background()
			admin, err := harness.service.BootstrapAdministrator(ctx, "admin@example.test", "Admin", []byte("admin-password-long-enough"))
			if err != nil {
				t.Fatal(err)
			}
			signedIn, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{
				Username: admin.Username, Password: secret("admin-password-long-enough"),
			}))
			if err != nil {
				t.Fatal(err)
			}
			created, err := harness.service.CreateUser(ctx, connect.NewRequest(&tammyv1.CreateUserRequest{
				CommandContext: &tammyv1.CommandContext{IdempotencyKey: "01890f3c-7b2e-7cc4-98c4-dc0c0c073990", Authentication: &tammyv1.AuthenticationContext{
					ActorUserId: admin.Id, SessionId: signedIn.Msg.Session.Id,
				}},
				Username: "rollback@example.test", DisplayName: "Rollback", Roles: []tammyv1.Role{tammyv1.Role_ROLE_AUDITOR},
			}))
			if err != nil {
				t.Fatal(err)
			}
			injected := errors.New("injected activation dependency failure")
			testCase.configure(harness, injected)
			response, err := harness.service.ActivateUser(ctx, connect.NewRequest(&tammyv1.ActivateUserRequest{
				Username: created.Msg.User.Username, ActivationCode: secret(string(created.Msg.ActivationCode.Utf8)),
				NewPassword: secret("rollback-password-long-enough"),
			}))
			if !errors.Is(err, injected) || response != nil {
				t.Fatalf("failed activation response_present=%t err=%v", response != nil, err)
			}
			state, err := harness.repository.Load(ctx)
			if err != nil {
				t.Fatal(err)
			}
			target := state.Users[created.Msg.User.Id]
			if target == nil || target.State != tammyv1.UserState_USER_STATE_PENDING_ACTIVATION || target.ActivationSessionID != "" ||
				state.Sessions[signedIn.Msg.Session.Id].State != tammyv1.SessionState_SESSION_STATE_ACTIVE ||
				harness.audit.counts["user_activated"] != 0 {
				t.Fatalf("activation failure leaked target=%+v admin_session=%v activation_audit=%d", target,
					state.Sessions[signedIn.Msg.Session.Id].State, harness.audit.counts["user_activated"])
			}
		})
	}
}

func TestIdentityServicePendingActivationExpiresAfterTwentyFourHours(t *testing.T) {
	harness := newIdentityHarness(t)
	ctx := context.Background()
	admin, err := harness.service.BootstrapAdministrator(ctx, "admin@example.test", "Admin", []byte("admin-password-long-enough"))
	if err != nil {
		t.Fatal(err)
	}
	signedIn, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{
		Username: admin.Username, Password: secret("admin-password-long-enough"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	created, err := harness.service.CreateUser(ctx, connect.NewRequest(&tammyv1.CreateUserRequest{
		CommandContext: &tammyv1.CommandContext{IdempotencyKey: "01890f3c-7b2e-7cc4-98c4-dc0c0c073935", Authentication: &tammyv1.AuthenticationContext{
			ActorUserId: admin.Id, SessionId: signedIn.Msg.Session.Id,
		}},
		Username: "expired@example.test", DisplayName: "Expired", Roles: []tammyv1.Role{tammyv1.Role_ROLE_AUDITOR},
	}))
	if err != nil {
		t.Fatal(err)
	}
	*harness.now = harness.now.Add(24 * time.Hour)
	if _, err := harness.service.ActivateUser(ctx, connect.NewRequest(&tammyv1.ActivateUserRequest{
		Username: created.Msg.User.Username, ActivationCode: &tammyv1.SecretInput{Utf8: append([]byte(nil), created.Msg.ActivationCode.Utf8...)},
		NewPassword: secret("expired-user-password-long-enough"),
	})); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("expired activation returned %v", err)
	}
}

func TestIdentityServiceChangePasswordEnvelopeAndHistory(t *testing.T) {
	harness := newIdentityHarness(t)
	ctx := context.Background()
	admin, err := harness.service.BootstrapAdministrator(ctx, "admin@example.test", "Admin", []byte("admin-password-long-enough"))
	if err != nil {
		t.Fatal(err)
	}
	signedIn, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{Username: admin.Username, Password: secret("admin-password-long-enough")}))
	if err != nil {
		t.Fatal(err)
	}
	request := &tammyv1.ChangePasswordRequest{
		CommandContext:  &tammyv1.CommandContext{IdempotencyKey: "01890f3c-7b2e-7cc4-98c4-dc0c0c073902", Authentication: &tammyv1.AuthenticationContext{ActorUserId: admin.Id, SessionId: signedIn.Msg.Session.Id}},
		ExpectedVersion: admin.Version, CurrentPassword: secret("admin-password-long-enough"), NewPassword: secret("replacement-password-long-enough"),
	}
	changed, err := harness.service.ChangePassword(ctx, connect.NewRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	if changed.Msg.InvalidatedSession.State != tammyv1.SessionState_SESSION_STATE_INVALIDATED || changed.Msg.User.Version != admin.Version+1 {
		t.Fatalf("unexpected change result: %v", changed.Msg)
	}
	replayed, err := harness.service.ChangePassword(ctx, connect.NewRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Msg.InvalidatedSession.Id != changed.Msg.InvalidatedSession.Id {
		t.Fatal("exact replay did not return retained result")
	}
	unauthenticatedReplay := proto.Clone(request).(*tammyv1.ChangePasswordRequest)
	unauthenticatedReplay.CommandContext.Authentication = nil
	if _, err := harness.service.ChangePassword(ctx, connect.NewRequest(unauthenticatedReplay)); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("unauthenticated password replay returned %v", err)
	}
	resigned, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{Username: admin.Username, Password: secret("replacement-password-long-enough")}))
	if err != nil {
		t.Fatal(err)
	}
	secondRequest := &tammyv1.ChangePasswordRequest{
		CommandContext: &tammyv1.CommandContext{IdempotencyKey: "01890f3c-7b2e-7cc4-98c4-dc0c0c073936", Authentication: &tammyv1.AuthenticationContext{
			ActorUserId: admin.Id, SessionId: resigned.Msg.Session.Id,
		}},
		ExpectedVersion: changed.Msg.User.Version, CurrentPassword: secret("replacement-password-long-enough"),
		NewPassword: secret("second-replacement-password-long-enough"),
	}
	secondChanged, err := harness.service.ChangePassword(ctx, connect.NewRequest(secondRequest))
	if err != nil {
		t.Fatal(err)
	}
	olderReplay, err := harness.service.ChangePassword(ctx, connect.NewRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	if olderReplay.Msg.User.Version != changed.Msg.User.Version || olderReplay.Msg.User.Version == secondChanged.Msg.User.Version {
		t.Fatalf("older replay returned mutable current user: %v", olderReplay.Msg.User)
	}
	conflicting := proto.Clone(request).(*tammyv1.ChangePasswordRequest)
	conflicting.NewPassword = secret("different-password-long-enough")
	if _, err := harness.service.ChangePassword(ctx, connect.NewRequest(conflicting)); !errors.Is(err, faults.New(faults.CodeIdempotencyConflict, nil)) {
		t.Fatalf("changed replay returned %v", err)
	}
	resigned, err = harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{Username: admin.Username, Password: secret("second-replacement-password-long-enough")}))
	if err != nil {
		t.Fatal(err)
	}
	reuse := &tammyv1.ChangePasswordRequest{
		CommandContext:  &tammyv1.CommandContext{IdempotencyKey: "01890f3c-7b2e-7cc4-98c4-dc0c0c073903", Authentication: &tammyv1.AuthenticationContext{ActorUserId: admin.Id, SessionId: resigned.Msg.Session.Id}},
		ExpectedVersion: secondChanged.Msg.User.Version, CurrentPassword: secret("second-replacement-password-long-enough"), NewPassword: secret("admin-password-long-enough"),
	}
	if _, err := harness.service.ChangePassword(ctx, connect.NewRequest(reuse)); !errors.Is(err, faults.New(faults.CodeValidation, nil)) {
		t.Fatalf("password history reuse returned %v", err)
	}
}

func TestIdentityServiceTOTPEnrolAssertDisableEnvelopes(t *testing.T) {
	harness := newIdentityHarness(t)
	ctx := context.Background()
	admin, err := harness.service.BootstrapAdministrator(ctx, "admin@example.test", "Admin", []byte("admin-password-long-enough"))
	if err != nil {
		t.Fatal(err)
	}
	signedIn, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{Username: admin.Username, Password: secret("admin-password-long-enough")}))
	if err != nil {
		t.Fatal(err)
	}
	auth := &tammyv1.AuthenticationContext{ActorUserId: admin.Id, SessionId: signedIn.Msg.Session.Id}
	enrolRequest := &tammyv1.EnrolTOTPRequest{CommandContext: &tammyv1.CommandContext{
		IdempotencyKey: "01890f3c-7b2e-7cc4-98c4-dc0c0c073904", Authentication: auth,
	}, CurrentPassword: secret("admin-password-long-enough")}
	enrolled, err := harness.service.EnrolTOTP(ctx, connect.NewRequest(enrolRequest))
	if err != nil {
		t.Fatal(err)
	}
	if harness.audit.counts["totp_enrolled"] != 1 {
		t.Fatalf("enrol audit count = %d", harness.audit.counts["totp_enrolled"])
	}
	beforeEnrolReplay, err := harness.repository.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	beforeEnrolSession := beforeEnrolReplay.Sessions[signedIn.Msg.Session.Id]
	*harness.now = harness.now.Add(time.Minute)
	replayedEnrol, err := harness.service.EnrolTOTP(ctx, connect.NewRequest(enrolRequest))
	if err != nil {
		t.Fatal(err)
	}
	if replayedEnrol.Msg.Factor.Id != enrolled.Msg.Factor.Id || !bytes.Equal(replayedEnrol.Msg.ProvisioningSecret.Utf8, enrolled.Msg.ProvisioningSecret.Utf8) {
		t.Fatal("enrol replay created different factor material")
	}
	afterEnrolReplay, err := harness.repository.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	afterEnrolSession := afterEnrolReplay.Sessions[signedIn.Msg.Session.Id]
	if !afterEnrolSession.LastActive.Equal(beforeEnrolSession.LastActive) || !afterEnrolSession.ExpiresAt.Equal(beforeEnrolSession.ExpiresAt) {
		t.Fatalf("enrol replay touched session last_active=%v→%v expires=%v→%v", beforeEnrolSession.LastActive,
			afterEnrolSession.LastActive, beforeEnrolSession.ExpiresAt, afterEnrolSession.ExpiresAt)
	}
	unauthenticatedEnrol := proto.Clone(enrolRequest).(*tammyv1.EnrolTOTPRequest)
	unauthenticatedEnrol.CommandContext.Authentication = nil
	if _, err := harness.service.EnrolTOTP(ctx, connect.NewRequest(unauthenticatedEnrol)); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("unauthenticated enrol replay returned %v", err)
	}
	wrongSessionEnrol := proto.Clone(enrolRequest).(*tammyv1.EnrolTOTPRequest)
	wrongSessionEnrol.CommandContext.Authentication.SessionId = "01890f3c-7b2e-7cc4-98c4-dc0c0c073949"
	if _, err := harness.service.EnrolTOTP(ctx, connect.NewRequest(wrongSessionEnrol)); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("wrong-session enrol replay returned %v", err)
	}
	changedEnrol := proto.Clone(enrolRequest).(*tammyv1.EnrolTOTPRequest)
	changedEnrol.CurrentPassword = secret("different-password-long-enough")
	if _, err := harness.service.EnrolTOTP(ctx, connect.NewRequest(changedEnrol)); !errors.Is(err, faults.New(faults.CodeIdempotencyConflict, nil)) {
		t.Fatalf("changed enrol replay returned %v", err)
	}
	afterEnrolConflict, err := harness.repository.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	conflictSession := afterEnrolConflict.Sessions[signedIn.Msg.Session.Id]
	if !conflictSession.LastActive.Equal(beforeEnrolSession.LastActive) || !conflictSession.ExpiresAt.Equal(beforeEnrolSession.ExpiresAt) {
		t.Fatalf("enrol conflict touched session last_active=%v expires=%v", conflictSession.LastActive, conflictSession.ExpiresAt)
	}
	totpSecret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(string(enrolled.Msg.ProvisioningSecret.Utf8))
	if err != nil {
		t.Fatal(err)
	}
	code := TOTPCode(totpSecret, *harness.now)
	confirmed, err := harness.service.ConfirmTOTP(ctx, connect.NewRequest(&tammyv1.ConfirmTOTPRequest{
		Authentication: auth, FactorId: enrolled.Msg.Factor.Id, Code: &tammyv1.TotpCodeInput{Value: code},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.Msg.Factor.State != tammyv1.FactorState_FACTOR_STATE_ENABLED {
		t.Fatal("factor was not enabled")
	}
	afterConfirmationReplay, err := harness.service.EnrolTOTP(ctx, connect.NewRequest(enrolRequest))
	if err != nil {
		t.Fatal(err)
	}
	if afterConfirmationReplay.Msg.Factor.State != tammyv1.FactorState_FACTOR_STATE_PENDING_CONFIRMATION {
		t.Fatalf("enrol replay returned mutable factor: %v", afterConfirmationReplay.Msg.Factor)
	}
	if harness.audit.counts["totp_enrolled"] != 1 {
		t.Fatalf("enrol replay audit count = %d", harness.audit.counts["totp_enrolled"])
	}
	terminal, err := harness.service.ConfirmTOTP(ctx, connect.NewRequest(&tammyv1.ConfirmTOTPRequest{
		Authentication: auth, FactorId: enrolled.Msg.Factor.Id, Code: &tammyv1.TotpCodeInput{Value: code},
	}))
	if err != nil || terminal.Msg.Factor.Id != confirmed.Msg.Factor.Id || terminal.Msg.Factor.Version != confirmed.Msg.Factor.Version {
		t.Fatalf("terminal confirmation challenge: %v", err)
	}
	*harness.now = harness.now.Add(30 * time.Second)
	asserted, err := harness.service.AssertTOTP(ctx, connect.NewRequest(&tammyv1.AssertTOTPRequest{
		Authentication: auth, Code: &tammyv1.TotpCodeInput{Value: TOTPCode(totpSecret, *harness.now)}, Purpose: "disable_totp",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := harness.service.AssertTOTP(ctx, connect.NewRequest(&tammyv1.AssertTOTPRequest{
		Authentication: auth, Code: &tammyv1.TotpCodeInput{Value: TOTPCode(totpSecret, *harness.now)}, Purpose: "disable_totp",
	})); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("assertion replay returned %v", err)
	}
	disableRequest := &tammyv1.DisableTOTPRequest{CommandContext: &tammyv1.CommandContext{
		IdempotencyKey: "01890f3c-7b2e-7cc4-98c4-dc0c0c073905", Authentication: auth, FreshFactor: asserted.Msg.FreshFactor,
	}, FactorId: enrolled.Msg.Factor.Id, ExpectedVersion: confirmed.Msg.Factor.Version, CurrentPassword: secret("admin-password-long-enough")}
	disabled, err := harness.service.DisableTOTP(ctx, connect.NewRequest(disableRequest))
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Msg.Factor.State != tammyv1.FactorState_FACTOR_STATE_DISABLED {
		t.Fatal("factor was not disabled")
	}
	if harness.audit.counts["totp_disabled"] != 1 {
		t.Fatalf("disable audit count = %d", harness.audit.counts["totp_disabled"])
	}
	stateAfterDisable, err := harness.repository.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if retainedFactor := stateAfterDisable.Factors[disabled.Msg.Factor.Id]; retainedFactor == nil || len(retainedFactor.EncryptedSecret) != 0 {
		t.Fatalf("disabled factor retained encrypted seed: %+v", retainedFactor)
	}
	beforeDisableSession := stateAfterDisable.Sessions[signedIn.Msg.Session.Id]
	*harness.now = harness.now.Add(time.Minute)
	replayedDisable, err := harness.service.DisableTOTP(ctx, connect.NewRequest(disableRequest))
	if err != nil || replayedDisable.Msg.Factor.Id != disabled.Msg.Factor.Id {
		t.Fatalf("disable replay: %v", err)
	}
	if harness.audit.counts["totp_disabled"] != 1 {
		t.Fatalf("disable replay audit count = %d", harness.audit.counts["totp_disabled"])
	}
	afterDisableReplay, err := harness.repository.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	afterDisableSession := afterDisableReplay.Sessions[signedIn.Msg.Session.Id]
	if !afterDisableSession.LastActive.Equal(beforeDisableSession.LastActive) || !afterDisableSession.ExpiresAt.Equal(beforeDisableSession.ExpiresAt) {
		t.Fatalf("disable replay touched session last_active=%v→%v expires=%v→%v", beforeDisableSession.LastActive,
			afterDisableSession.LastActive, beforeDisableSession.ExpiresAt, afterDisableSession.ExpiresAt)
	}
	changedDisable := proto.Clone(disableRequest).(*tammyv1.DisableTOTPRequest)
	changedDisable.CurrentPassword = secret("different-password-long-enough")
	if _, err := harness.service.DisableTOTP(ctx, connect.NewRequest(changedDisable)); !errors.Is(err, faults.New(faults.CodeIdempotencyConflict, nil)) {
		t.Fatalf("changed disable replay returned %v", err)
	}
	afterDisableConflict, err := harness.repository.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	conflictDisableSession := afterDisableConflict.Sessions[signedIn.Msg.Session.Id]
	if !conflictDisableSession.LastActive.Equal(beforeDisableSession.LastActive) || !conflictDisableSession.ExpiresAt.Equal(beforeDisableSession.ExpiresAt) {
		t.Fatalf("disable conflict touched session last_active=%v expires=%v", conflictDisableSession.LastActive, conflictDisableSession.ExpiresAt)
	}
	unauthenticatedDisable := proto.Clone(disableRequest).(*tammyv1.DisableTOTPRequest)
	unauthenticatedDisable.CommandContext.Authentication = nil
	if _, err := harness.service.DisableTOTP(ctx, connect.NewRequest(unauthenticatedDisable)); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("unauthenticated disable replay returned %v", err)
	}
}

func TestIdentityServiceTOTPConfirmationCooldown(t *testing.T) {
	harness := newIdentityHarness(t)
	ctx := context.Background()
	admin, err := harness.service.BootstrapAdministrator(ctx, "admin@example.test", "Admin", []byte("admin-password-long-enough"))
	if err != nil {
		t.Fatal(err)
	}
	signedIn, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{
		Username: admin.Username, Password: secret("admin-password-long-enough"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	auth := &tammyv1.AuthenticationContext{ActorUserId: admin.Id, SessionId: signedIn.Msg.Session.Id}
	enrolled, err := harness.service.EnrolTOTP(ctx, connect.NewRequest(&tammyv1.EnrolTOTPRequest{
		CommandContext:  &tammyv1.CommandContext{IdempotencyKey: "01890f3c-7b2e-7cc4-98c4-dc0c0c073930", Authentication: auth},
		CurrentPassword: secret("admin-password-long-enough"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	secretBytes, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(string(enrolled.Msg.ProvisioningSecret.Utf8))
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 5; attempt++ {
		_, err := harness.service.ConfirmTOTP(ctx, connect.NewRequest(&tammyv1.ConfirmTOTPRequest{
			Authentication: auth, FactorId: enrolled.Msg.Factor.Id, Code: &tammyv1.TotpCodeInput{Value: "not-a-code"},
		}))
		if !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
			t.Fatalf("attempt %d returned %v", attempt+1, err)
		}
	}
	if _, err := harness.service.ConfirmTOTP(ctx, connect.NewRequest(&tammyv1.ConfirmTOTPRequest{
		Authentication: auth, FactorId: enrolled.Msg.Factor.Id,
		Code: &tammyv1.TotpCodeInput{Value: TOTPCode(secretBytes, *harness.now)},
	})); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("valid code during cooldown returned %v", err)
	}
	*harness.now = harness.now.Add(15 * time.Minute)
	confirmed, err := harness.service.ConfirmTOTP(ctx, connect.NewRequest(&tammyv1.ConfirmTOTPRequest{
		Authentication: auth, FactorId: enrolled.Msg.Factor.Id,
		Code: &tammyv1.TotpCodeInput{Value: TOTPCode(secretBytes, *harness.now)},
	}))
	if err != nil || confirmed.Msg.Factor.State != tammyv1.FactorState_FACTOR_STATE_ENABLED {
		t.Fatalf("confirmation after cooldown: %v", err)
	}
}

func TestResetUserAuthenticationFreshFactor(t *testing.T) {
	harness := newIdentityHarness(t)
	ctx := context.Background()
	admin, err := harness.service.BootstrapAdministrator(ctx, "admin@example.test", "Admin", []byte("admin-password-long-enough"))
	if err != nil {
		t.Fatal(err)
	}
	signedIn, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{Username: admin.Username, Password: secret("admin-password-long-enough")}))
	if err != nil {
		t.Fatal(err)
	}
	auth := &tammyv1.AuthenticationContext{ActorUserId: admin.Id, SessionId: signedIn.Msg.Session.Id}
	totpSecret := enrolConfirmedFactor(t, harness, auth, "admin-password-long-enough", "01890f3c-7b2e-7cc4-98c4-dc0c0c073906")
	created, err := harness.service.CreateUser(ctx, connect.NewRequest(&tammyv1.CreateUserRequest{
		CommandContext: &tammyv1.CommandContext{IdempotencyKey: "01890f3c-7b2e-7cc4-98c4-dc0c0c073907", Authentication: auth},
		Username:       "preparer@example.test", DisplayName: "Preparer", Roles: []tammyv1.Role{tammyv1.Role_ROLE_BUSINESS_PREPARER},
	}))
	if err != nil {
		t.Fatal(err)
	}
	activated, err := harness.service.ActivateUser(ctx, connect.NewRequest(&tammyv1.ActivateUserRequest{
		Username: created.Msg.User.Username, ActivationCode: &tammyv1.SecretInput{Utf8: created.Msg.ActivationCode.Utf8},
		NewPassword: secret("preparer-password-long-enough"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	resigned, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{Username: admin.Username, Password: secret("admin-password-long-enough")}))
	if err != nil {
		t.Fatal(err)
	}
	auth.SessionId = resigned.Msg.Session.Id
	base := &tammyv1.ResetUserAuthenticationRequest{CommandContext: &tammyv1.CommandContext{
		IdempotencyKey: "01890f3c-7b2e-7cc4-98c4-dc0c0c073908", Authentication: auth,
	}, UserId: activated.Msg.User.Id, ExpectedVersion: activated.Msg.User.Version}
	t.Run("missing factor", func(t *testing.T) {
		if _, err := harness.service.ResetUserAuthentication(ctx, connect.NewRequest(proto.Clone(base).(*tammyv1.ResetUserAuthenticationRequest))); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
			t.Fatalf("missing factor returned %v", err)
		}
	})
	*harness.now = harness.now.Add(30 * time.Second)
	stale, err := harness.service.AssertTOTP(ctx, connect.NewRequest(&tammyv1.AssertTOTPRequest{Authentication: auth,
		Code: &tammyv1.TotpCodeInput{Value: TOTPCode(totpSecret, *harness.now)}, Purpose: "reset_user_authentication"}))
	if err != nil {
		t.Fatal(err)
	}
	*harness.now = harness.now.Add(5 * time.Minute)
	t.Run("stale factor", func(t *testing.T) {
		request := proto.Clone(base).(*tammyv1.ResetUserAuthenticationRequest)
		request.CommandContext.FreshFactor = stale.Msg.FreshFactor
		if _, err := harness.service.ResetUserAuthentication(ctx, connect.NewRequest(request)); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
			t.Fatalf("stale factor returned %v", err)
		}
	})
	*harness.now = harness.now.Add(30 * time.Second)
	wrongPurpose, err := harness.service.AssertTOTP(ctx, connect.NewRequest(&tammyv1.AssertTOTPRequest{Authentication: auth,
		Code: &tammyv1.TotpCodeInput{Value: TOTPCode(totpSecret, *harness.now)}, Purpose: "change_passphrase"}))
	if err != nil {
		t.Fatal(err)
	}
	t.Run("marker reserved for another action", func(t *testing.T) {
		request := proto.Clone(base).(*tammyv1.ResetUserAuthenticationRequest)
		request.CommandContext.FreshFactor = wrongPurpose.Msg.FreshFactor
		if _, err := harness.service.ResetUserAuthentication(ctx, connect.NewRequest(request)); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
			t.Fatalf("wrong-purpose factor returned %v", err)
		}
	})
	*harness.now = harness.now.Add(30 * time.Second)
	fresh, err := harness.service.AssertTOTP(ctx, connect.NewRequest(&tammyv1.AssertTOTPRequest{Authentication: auth,
		Code: &tammyv1.TotpCodeInput{Value: TOTPCode(totpSecret, *harness.now)}, Purpose: "reset_user_authentication"}))
	if err != nil {
		t.Fatal(err)
	}
	var resetResponse *tammyv1.ResetUserAuthenticationResponse
	var successfulResetRequest *tammyv1.ResetUserAuthenticationRequest
	t.Run("newly asserted factor", func(t *testing.T) {
		request := proto.Clone(base).(*tammyv1.ResetUserAuthenticationRequest)
		request.CommandContext.FreshFactor = fresh.Msg.FreshFactor
		response, err := harness.service.ResetUserAuthentication(ctx, connect.NewRequest(request))
		if err != nil {
			t.Fatal(err)
		}
		if response.Msg.User.State != tammyv1.UserState_USER_STATE_PENDING_ACTIVATION || len(response.Msg.ActivationCode.Utf8) == 0 {
			t.Fatalf("unexpected reset: %v", response.Msg)
		}
		resetResponse = response.Msg
		successfulResetRequest = request
	})
	beforeResetReplay, err := harness.repository.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	beforeResetSession := beforeResetReplay.Sessions[auth.SessionId]
	*harness.now = harness.now.Add(time.Minute)
	replayedReset, err := harness.service.ResetUserAuthentication(ctx, connect.NewRequest(proto.Clone(successfulResetRequest).(*tammyv1.ResetUserAuthenticationRequest)))
	if err != nil || replayedReset.Msg.User.Version != resetResponse.User.Version || harness.audit.counts["user_authentication_reset"] != 1 {
		t.Fatalf("reset replay response=%v audit=%d err=%v", replayedReset, harness.audit.counts["user_authentication_reset"], err)
	}
	conflictingReset := proto.Clone(successfulResetRequest).(*tammyv1.ResetUserAuthenticationRequest)
	conflictingReset.ExpectedVersion++
	if _, err := harness.service.ResetUserAuthentication(ctx, connect.NewRequest(conflictingReset)); !errors.Is(err, faults.New(faults.CodeIdempotencyConflict, nil)) {
		t.Fatalf("reset conflict returned %v", err)
	}
	afterResetReplay, err := harness.repository.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	afterResetSession := afterResetReplay.Sessions[auth.SessionId]
	if !afterResetSession.LastActive.Equal(beforeResetSession.LastActive) || !afterResetSession.ExpiresAt.Equal(beforeResetSession.ExpiresAt) ||
		afterResetReplay.Users[resetResponse.User.Id].Version != beforeResetReplay.Users[resetResponse.User.Id].Version ||
		len(afterResetReplay.Idempotency) != len(beforeResetReplay.Idempotency) {
		t.Fatalf("reset replay/conflict mutated session=%+v→%+v user_version=%d→%d idempotency=%d→%d", beforeResetSession,
			afterResetSession, beforeResetReplay.Users[resetResponse.User.Id].Version, afterResetReplay.Users[resetResponse.User.Id].Version,
			len(beforeResetReplay.Idempotency), len(afterResetReplay.Idempotency))
	}
	t.Run("unauthenticated exact replay", func(t *testing.T) {
		request := proto.Clone(base).(*tammyv1.ResetUserAuthenticationRequest)
		request.CommandContext.Authentication = nil
		request.CommandContext.FreshFactor = fresh.Msg.FreshFactor
		if _, err := harness.service.ResetUserAuthentication(ctx, connect.NewRequest(request)); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
			t.Fatalf("unauthenticated retained reset returned %v", err)
		}
	})
	t.Run("prior password rejected on reactivation", func(t *testing.T) {
		if _, err := harness.service.ActivateUser(ctx, connect.NewRequest(&tammyv1.ActivateUserRequest{
			Username:       resetResponse.User.Username,
			ActivationCode: &tammyv1.SecretInput{Utf8: append([]byte(nil), resetResponse.ActivationCode.Utf8...)},
			NewPassword:    secret("preparer-password-long-enough"),
		})); !errors.Is(err, faults.New(faults.CodeValidation, nil)) {
			t.Fatalf("prior password activation returned %v", err)
		}
	})
}

func enrolConfirmedFactor(t *testing.T, harness identityHarness, auth *tammyv1.AuthenticationContext, password, operationKey string) []byte {
	t.Helper()
	enrolled, err := harness.service.EnrolTOTP(context.Background(), connect.NewRequest(&tammyv1.EnrolTOTPRequest{
		CommandContext: &tammyv1.CommandContext{IdempotencyKey: operationKey, Authentication: auth}, CurrentPassword: secret(password),
	}))
	if err != nil {
		t.Fatal(err)
	}
	secretBytes, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(string(enrolled.Msg.ProvisioningSecret.Utf8))
	if err != nil {
		t.Fatal(err)
	}
	_, err = harness.service.ConfirmTOTP(context.Background(), connect.NewRequest(&tammyv1.ConfirmTOTPRequest{
		Authentication: auth, FactorId: enrolled.Msg.Factor.Id, Code: &tammyv1.TotpCodeInput{Value: TOTPCode(secretBytes, *harness.now)},
	}))
	if err != nil {
		t.Fatal(err)
	}
	return secretBytes
}

func TestIdentityServiceFreshFactorLookupPurposeAndSingleUse(t *testing.T) {
	harness := newIdentityHarness(t)
	ctx := context.Background()
	admin, err := harness.service.BootstrapAdministrator(ctx, "admin@example.test", "Admin", []byte("admin-password-long-enough"))
	if err != nil {
		t.Fatal(err)
	}
	signed, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{
		Username: admin.Username, Password: secret("admin-password-long-enough"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	authentication := &tammyv1.AuthenticationContext{ActorUserId: admin.Id, SessionId: signed.Msg.Session.Id}
	totpSecret := enrolConfirmedFactor(t, harness, authentication, "admin-password-long-enough", "01890f3c-7b2e-7cc4-98c4-dc0c0c073947")
	*harness.now = harness.now.Add(30 * time.Second)
	asserted, err := harness.service.AssertTOTP(ctx, connect.NewRequest(&tammyv1.AssertTOTPRequest{
		Authentication: authentication,
		Code:           &tammyv1.TotpCodeInput{Value: TOTPCode(totpSecret, *harness.now)},
		Purpose:        "ownership_transfer",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := harness.service.ConsumeFreshFactor(ctx, nil, asserted.Msg.FreshFactor, "ownership_transfer"); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("unauthenticated consumption returned %v", err)
	}
	if err := harness.service.ConsumeFreshFactor(ctx, authentication, asserted.Msg.FreshFactor, "change_passphrase"); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("wrong-purpose consumption returned %v", err)
	}
	if err := harness.service.ConsumeFreshFactor(ctx, authentication, asserted.Msg.FreshFactor, "ownership_transfer"); err != nil {
		t.Fatalf("fresh assertion was not accepted: %v", err)
	}
	if err := harness.service.ConsumeFreshFactor(ctx, authentication, asserted.Msg.FreshFactor, "ownership_transfer"); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("consumed assertion replay returned %v", err)
	}

	*harness.now = harness.now.Add(30 * time.Second)
	stale, err := harness.service.AssertTOTP(ctx, connect.NewRequest(&tammyv1.AssertTOTPRequest{
		Authentication: authentication,
		Code:           &tammyv1.TotpCodeInput{Value: TOTPCode(totpSecret, *harness.now)},
		Purpose:        "ownership_transfer",
	}))
	if err != nil {
		t.Fatal(err)
	}
	*harness.now = harness.now.Add(5 * time.Minute)
	if err := harness.service.ConsumeFreshFactor(ctx, authentication, stale.Msg.FreshFactor, "ownership_transfer"); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("stale assertion consumption returned %v", err)
	}
}

func TestIdentityServiceNewAssertionSupersedesOlderMarker(t *testing.T) {
	harness := newIdentityHarness(t)
	ctx := context.Background()
	admin, err := harness.service.BootstrapAdministrator(ctx, "admin@example.test", "Admin", []byte("admin-password-long-enough"))
	if err != nil {
		t.Fatal(err)
	}
	signed, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{
		Username: admin.Username, Password: secret("admin-password-long-enough"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	authentication := &tammyv1.AuthenticationContext{ActorUserId: admin.Id, SessionId: signed.Msg.Session.Id}
	totpSecret := enrolConfirmedFactor(t, harness, authentication, "admin-password-long-enough", "01890f3c-7b2e-7cc4-98c4-dc0c0c073950")
	*harness.now = harness.now.Add(30 * time.Second)
	older, err := harness.service.AssertTOTP(ctx, connect.NewRequest(&tammyv1.AssertTOTPRequest{
		Authentication: authentication, Code: &tammyv1.TotpCodeInput{Value: TOTPCode(totpSecret, *harness.now)}, Purpose: "ownership_transfer",
	}))
	if err != nil {
		t.Fatal(err)
	}
	*harness.now = harness.now.Add(30 * time.Second)
	newer, err := harness.service.AssertTOTP(ctx, connect.NewRequest(&tammyv1.AssertTOTPRequest{
		Authentication: authentication, Code: &tammyv1.TotpCodeInput{Value: TOTPCode(totpSecret, *harness.now)}, Purpose: "change_passphrase",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := harness.service.ConsumeFreshFactor(ctx, authentication, older.Msg.FreshFactor, "ownership_transfer"); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("superseded assertion returned %v", err)
	}
	if err := harness.service.ConsumeFreshFactor(ctx, authentication, newer.Msg.FreshFactor, "change_passphrase"); err != nil {
		t.Fatalf("newest assertion returned %v", err)
	}
}

func TestIdentityServiceFailedAssertionFailsClosedWhenJournalUnavailable(t *testing.T) {
	harness := newIdentityHarness(t)
	ctx := context.Background()
	admin, err := harness.service.BootstrapAdministrator(ctx, "admin@example.test", "Admin", []byte("admin-password-long-enough"))
	if err != nil {
		t.Fatal(err)
	}
	signed, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{
		Username: admin.Username, Password: secret("admin-password-long-enough"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	authentication := &tammyv1.AuthenticationContext{ActorUserId: admin.Id, SessionId: signed.Msg.Session.Id}
	totpSecret := enrolConfirmedFactor(t, harness, authentication, "admin-password-long-enough", "01890f3c-7b2e-7cc4-98c4-dc0c0c073951")
	*harness.now = harness.now.Add(30 * time.Second)
	validCode := TOTPCode(totpSecret, *harness.now)
	invalidCode := validCode[:5] + string([]byte{'0' + (validCode[5]-'0'+1)%10})
	harness.config.Attempts.Close()
	if _, err := harness.service.AssertTOTP(ctx, connect.NewRequest(&tammyv1.AssertTOTPRequest{
		Authentication: authentication, Code: &tammyv1.TotpCodeInput{Value: invalidCode}, Purpose: "ownership_transfer",
	})); !errors.Is(err, workspace.ErrAttemptPolicy) {
		t.Fatalf("unavailable assertion journal returned %v", err)
	}
}

func TestIdentityServiceSignInFailsClosedWhenJournalUnavailable(t *testing.T) {
	harness := newIdentityHarness(t)
	ctx := context.Background()
	admin, err := harness.service.BootstrapAdministrator(ctx, "admin@example.test", "Admin", []byte("admin-password-long-enough"))
	if err != nil {
		t.Fatal(err)
	}
	harness.config.Attempts.Close()
	if _, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{
		Username: admin.Username, Password: secret("wrong-password-long-enough"),
	})); !errors.Is(err, workspace.ErrAttemptPolicy) {
		t.Fatalf("unavailable sign-in journal returned %v", err)
	}
}

func TestIdentitySuccessfulChallengesDoNotCommitWhenSuccessJournalWriteFails(t *testing.T) {
	t.Run("sign in", func(t *testing.T) {
		harness := newIdentityHarness(t)
		admin, err := harness.service.BootstrapAdministrator(context.Background(), "admin@example.test", "Admin", []byte("admin-password-long-enough"))
		if err != nil {
			t.Fatal(err)
		}
		failIdentityAttemptJournalWrites(t, harness)
		if _, err := harness.service.SignIn(context.Background(), connect.NewRequest(&tammyv1.SignInRequest{
			Username: admin.Username, Password: secret("admin-password-long-enough"),
		})); err == nil {
			t.Fatal("sign in succeeded without success-attempt persistence")
		}
		state, err := harness.repository.Load(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(state.Sessions) != 0 {
			t.Fatalf("journal failure committed %d sessions", len(state.Sessions))
		}
	})

	t.Run("activate user", func(t *testing.T) {
		harness := newIdentityHarness(t)
		ctx := context.Background()
		admin, err := harness.service.BootstrapAdministrator(ctx, "admin@example.test", "Admin", []byte("admin-password-long-enough"))
		if err != nil {
			t.Fatal(err)
		}
		signed, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{Username: admin.Username, Password: secret("admin-password-long-enough")}))
		if err != nil {
			t.Fatal(err)
		}
		created, err := harness.service.CreateUser(ctx, connect.NewRequest(&tammyv1.CreateUserRequest{
			CommandContext: &tammyv1.CommandContext{IdempotencyKey: "01890f3c-7b2e-7cc4-98c4-dc0c0c073979", Authentication: &tammyv1.AuthenticationContext{
				ActorUserId: admin.Id, SessionId: signed.Msg.Session.Id,
			}},
			Username: "journal-activation@example.test", DisplayName: "Journal Activation", Roles: []tammyv1.Role{tammyv1.Role_ROLE_AUDITOR},
		}))
		if err != nil {
			t.Fatal(err)
		}
		request := &tammyv1.ActivateUserRequest{Username: created.Msg.User.Username,
			ActivationCode: &tammyv1.SecretInput{Utf8: append([]byte(nil), created.Msg.ActivationCode.Utf8...)},
			NewPassword:    secret("activated-password-long-enough")}
		failIdentityAttemptJournalWrites(t, harness)
		if _, err := harness.service.ActivateUser(ctx, connect.NewRequest(request)); err == nil {
			t.Fatal("activation succeeded without success-attempt persistence")
		}
		state, err := harness.repository.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}
		target := state.Users[created.Msg.User.Id]
		if target == nil || target.State != tammyv1.UserState_USER_STATE_PENDING_ACTIVATION || target.ActivationSessionID != "" {
			if target == nil {
				t.Fatal("journal failure removed pending user")
			}
			t.Fatalf("journal failure committed activation: state=%v activation_session=%t", target.State, target.ActivationSessionID != "")
		}
		if response, err := harness.service.ActivateUser(ctx, connect.NewRequest(request)); err == nil || response != nil {
			t.Fatalf("failed activation replay returned response_present=%t err=%v", response != nil, err)
		}
	})

	t.Run("confirm totp", func(t *testing.T) {
		harness := newIdentityHarness(t)
		ctx := context.Background()
		admin, err := harness.service.BootstrapAdministrator(ctx, "admin@example.test", "Admin", []byte("admin-password-long-enough"))
		if err != nil {
			t.Fatal(err)
		}
		signed, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{Username: admin.Username, Password: secret("admin-password-long-enough")}))
		if err != nil {
			t.Fatal(err)
		}
		authentication := &tammyv1.AuthenticationContext{ActorUserId: admin.Id, SessionId: signed.Msg.Session.Id}
		enrolled, err := harness.service.EnrolTOTP(ctx, connect.NewRequest(&tammyv1.EnrolTOTPRequest{
			CommandContext:  &tammyv1.CommandContext{IdempotencyKey: "01890f3c-7b2e-7cc4-98c4-dc0c0c073980", Authentication: authentication},
			CurrentPassword: secret("admin-password-long-enough"),
		}))
		if err != nil {
			t.Fatal(err)
		}
		secretBytes, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(string(enrolled.Msg.ProvisioningSecret.Utf8))
		if err != nil {
			t.Fatal(err)
		}
		defer workspace.Zero(secretBytes)
		request := &tammyv1.ConfirmTOTPRequest{Authentication: authentication, FactorId: enrolled.Msg.Factor.Id,
			Code: &tammyv1.TotpCodeInput{Value: TOTPCode(secretBytes, *harness.now)}}
		failIdentityAttemptJournalWrites(t, harness)
		if _, err := harness.service.ConfirmTOTP(ctx, connect.NewRequest(request)); err == nil {
			t.Fatal("TOTP confirmation succeeded without success-attempt persistence")
		}
		state, err := harness.repository.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}
		factor := state.Factors[enrolled.Msg.Factor.Id]
		if factor == nil || factor.State != tammyv1.FactorState_FACTOR_STATE_PENDING_CONFIRMATION || factor.Version != 1 || factor.LastCounter != -1 {
			if factor == nil {
				t.Fatal("journal failure removed pending factor")
			}
			t.Fatalf("journal failure committed factor confirmation: state=%v version=%d counter=%d", factor.State, factor.Version, factor.LastCounter)
		}
		if response, err := harness.service.ConfirmTOTP(ctx, connect.NewRequest(request)); err == nil || response != nil {
			t.Fatalf("failed confirmation replay returned response_present=%t err=%v", response != nil, err)
		}
		if harness.audit.counts["totp_enrolled"] != 1 {
			t.Fatalf("confirmation changed audit count to %d", harness.audit.counts["totp_enrolled"])
		}
	})

	t.Run("assert totp", func(t *testing.T) {
		harness := newIdentityHarness(t)
		ctx := context.Background()
		admin, err := harness.service.BootstrapAdministrator(ctx, "admin@example.test", "Admin", []byte("admin-password-long-enough"))
		if err != nil {
			t.Fatal(err)
		}
		signed, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{Username: admin.Username, Password: secret("admin-password-long-enough")}))
		if err != nil {
			t.Fatal(err)
		}
		authentication := &tammyv1.AuthenticationContext{ActorUserId: admin.Id, SessionId: signed.Msg.Session.Id}
		secretBytes := enrolConfirmedFactor(t, harness, authentication, "admin-password-long-enough", "01890f3c-7b2e-7cc4-98c4-dc0c0c073981")
		defer workspace.Zero(secretBytes)
		state, err := harness.repository.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var initialCounter int64
		for _, factor := range state.Factors {
			initialCounter = factor.LastCounter
		}
		*harness.now = harness.now.Add(30 * time.Second)
		request := &tammyv1.AssertTOTPRequest{Authentication: authentication,
			Code: &tammyv1.TotpCodeInput{Value: TOTPCode(secretBytes, *harness.now)}, Purpose: "change_passphrase"}
		failIdentityAttemptJournalWrites(t, harness)
		if _, err := harness.service.AssertTOTP(ctx, connect.NewRequest(request)); err == nil {
			t.Fatal("TOTP assertion succeeded without success-attempt persistence")
		}
		state, err = harness.repository.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(state.Assertions) != 0 {
			t.Fatalf("journal failure committed %d assertions", len(state.Assertions))
		}
		for _, factor := range state.Factors {
			if factor.LastCounter != initialCounter {
				t.Fatalf("journal failure advanced counter from %d to %d", initialCounter, factor.LastCounter)
			}
		}
	})
}

func failIdentityAttemptJournalWrites(t *testing.T, harness identityHarness) {
	t.Helper()
	if harness.attemptPath == "" {
		t.Fatal("attempt journal path unavailable")
	}
	if _, err := os.Lstat(harness.attemptPath); os.IsNotExist(err) {
		if err := os.WriteFile(harness.attemptPath, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	} else if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(harness.attemptPath, 0o400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(harness.attemptPath, 0o600) })
}

func TestIdentityServiceLastAdministratorProtection(t *testing.T) {
	harness := newIdentityHarness(t)
	ctx := context.Background()
	admin, err := harness.service.BootstrapAdministrator(ctx, "admin@example.test", "Admin", []byte("admin-password-long-enough"))
	if err != nil {
		t.Fatal(err)
	}
	signed, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{Username: admin.Username, Password: secret("admin-password-long-enough")}))
	if err != nil {
		t.Fatal(err)
	}
	auth := &tammyv1.AuthenticationContext{ActorUserId: admin.Id, SessionId: signed.Msg.Session.Id}
	request := &tammyv1.AssignRolesRequest{CommandContext: &tammyv1.CommandContext{
		IdempotencyKey: "01890f3c-7b2e-7cc4-98c4-dc0c0c073909", Authentication: auth,
	}, UserId: admin.Id, ExpectedVersion: admin.Version, Roles: []tammyv1.Role{tammyv1.Role_ROLE_AUDITOR}}
	if _, err := harness.service.AssignRoles(ctx, connect.NewRequest(request)); !errors.Is(err, faults.New(faults.CodeValidation, nil)) {
		t.Fatalf("last administrator removal returned %v", err)
	}
}

func TestIdentityServiceBreakGlassAdministratorReset(t *testing.T) {
	harness := newIdentityHarness(t)
	ctx := context.Background()
	admin, err := harness.service.BootstrapAdministrator(ctx, "admin@example.test", "Admin", []byte("admin-password-long-enough"))
	if err != nil {
		t.Fatal(err)
	}
	signed, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{
		Username: admin.Username, Password: secret("admin-password-long-enough"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	auth := &tammyv1.AuthenticationContext{ActorUserId: admin.Id, SessionId: signed.Msg.Session.Id}
	_ = enrolConfirmedFactor(t, harness, auth, "admin-password-long-enough", "01890f3c-7b2e-7cc4-98c4-dc0c0c073931")
	recovered, err := harness.service.BreakGlassResetAdministrator(ctx, "01890f3c-7b2e-7cc4-98c4-dc0c0c073932",
		admin.Username, []byte("recovered-admin-password-long-enough"))
	if err != nil {
		t.Fatal(err)
	}
	if recovered.State != tammyv1.UserState_USER_STATE_ACTIVE || recovered.Version != admin.Version+1 || recovered.FactorState != nil {
		t.Fatalf("unexpected recovered administrator: %v", recovered)
	}
	if _, err := harness.service.GetSession(ctx, connect.NewRequest(&tammyv1.GetSessionRequest{Authentication: auth})); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("pre-recovery session returned %v", err)
	}
	replayed, err := harness.service.BreakGlassResetAdministrator(ctx, "01890f3c-7b2e-7cc4-98c4-dc0c0c073932",
		admin.Username, []byte("recovered-admin-password-long-enough"))
	if err != nil || replayed.Version != recovered.Version {
		t.Fatalf("exact recovery replay: %v", err)
	}
	if _, err := harness.service.BreakGlassResetAdministrator(ctx, "01890f3c-7b2e-7cc4-98c4-dc0c0c073932",
		admin.Username, []byte("different-admin-password-long-enough")); !errors.Is(err, faults.New(faults.CodeIdempotencyConflict, nil)) {
		t.Fatalf("changed recovery replay returned %v", err)
	}
	if _, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{
		Username: admin.Username, Password: secret("admin-password-long-enough"),
	})); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("old administrator password returned %v", err)
	}
	if _, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{
		Username: admin.Username, Password: secret("recovered-admin-password-long-enough"),
	})); err != nil {
		t.Fatal(err)
	}
}

func TestIdentityServiceIdleAndOSLockExpiry(t *testing.T) {
	harness := newIdentityHarness(t)
	ctx := context.Background()
	locks := 0
	harness.service.onWorkspaceLock = func() { locks++ }
	admin, err := harness.service.BootstrapAdministrator(ctx, "admin@example.test", "Admin", []byte("admin-password-long-enough"))
	if err != nil {
		t.Fatal(err)
	}
	signed, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{Username: admin.Username, Password: secret("admin-password-long-enough")}))
	if err != nil {
		t.Fatal(err)
	}
	auth := &tammyv1.AuthenticationContext{ActorUserId: admin.Id, SessionId: signed.Msg.Session.Id}
	*harness.now = harness.now.Add(30 * time.Minute)
	if err := harness.service.ExpireIdle(ctx); err != nil {
		t.Fatal(err)
	}
	if locks != 1 {
		t.Fatalf("idle expiry lock callbacks = %d", locks)
	}
	if _, err := harness.service.GetSession(ctx, connect.NewRequest(&tammyv1.GetSessionRequest{Authentication: auth})); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("expired session returned %v", err)
	}
	signed, err = harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{Username: admin.Username, Password: secret("admin-password-long-enough")}))
	if err != nil {
		t.Fatal(err)
	}
	if err := harness.service.HandleOSLock(ctx); err != nil {
		t.Fatal(err)
	}
	if locks != 2 {
		t.Fatalf("OS lock callbacks = %d", locks)
	}
}

func TestIdentityServiceWorkspaceLockCallbackMayInvalidateSessions(t *testing.T) {
	harness := newIdentityHarness(t)
	ctx := context.Background()
	admin, err := harness.service.BootstrapAdministrator(ctx, "admin@example.test", "Admin", []byte("admin-password-long-enough"))
	if err != nil {
		t.Fatal(err)
	}
	signed, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{
		Username: admin.Username, Password: secret("admin-password-long-enough"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	authentication := &tammyv1.AuthenticationContext{ActorUserId: admin.Id, SessionId: signed.Msg.Session.Id}
	callbackResult := make(chan error, 1)
	harness.service.onWorkspaceLock = func() {
		callbackResult <- harness.service.InvalidateAllSessions(ctx)
	}
	signOutResult := make(chan error, 1)
	go func() {
		_, signOutErr := harness.service.SignOut(ctx, connect.NewRequest(&tammyv1.SignOutRequest{Authentication: authentication}))
		signOutResult <- signOutErr
	}()
	select {
	case err := <-signOutResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("workspace-lock callback deadlocked identity session invalidation")
	}
	if err := <-callbackResult; err != nil {
		t.Fatal(err)
	}
}

func TestIdentityServiceCloseInvalidatesPersistedSessions(t *testing.T) {
	harness := newIdentityHarness(t)
	ctx := context.Background()
	admin, err := harness.service.BootstrapAdministrator(ctx, "admin@example.test", "Admin", []byte("admin-password-long-enough"))
	if err != nil {
		t.Fatal(err)
	}
	signed, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{
		Username: admin.Username, Password: secret("admin-password-long-enough"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	auth := &tammyv1.AuthenticationContext{ActorUserId: admin.Id, SessionId: signed.Msg.Session.Id}
	harness.service.Close()
	restarted, err := NewService(harness.config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.GetSession(ctx, connect.NewRequest(&tammyv1.GetSessionRequest{Authentication: auth})); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("session survived core exit: %v", err)
	}
}

func TestIdentityServiceStartupInvalidatesSessionsAfterAbruptRestart(t *testing.T) {
	harness := newIdentityHarness(t)
	ctx := context.Background()
	admin, err := harness.service.BootstrapAdministrator(ctx, "admin@example.test", "Admin", []byte("admin-password-long-enough"))
	if err != nil {
		t.Fatal(err)
	}
	signed, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{
		Username: admin.Username, Password: secret("admin-password-long-enough"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	authentication := &tammyv1.AuthenticationContext{ActorUserId: admin.Id, SessionId: signed.Msg.Session.Id}
	restarted, err := NewService(harness.config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.GetSession(ctx, connect.NewRequest(&tammyv1.GetSessionRequest{Authentication: authentication})); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("session survived abrupt restart: %v", err)
	}
}

func TestIdentityServiceRoleQueries(t *testing.T) {
	harness := newIdentityHarness(t)
	ctx := context.Background()
	admin, err := harness.service.BootstrapAdministrator(ctx, "admin@example.test", "Admin", []byte("admin-password-long-enough"))
	if err != nil {
		t.Fatal(err)
	}
	signed, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{Username: admin.Username, Password: secret("admin-password-long-enough")}))
	if err != nil {
		t.Fatal(err)
	}
	auth := &tammyv1.AuthenticationContext{ActorUserId: admin.Id, SessionId: signed.Msg.Session.Id}
	current, err := harness.service.GetCurrentUser(ctx, connect.NewRequest(&tammyv1.GetCurrentUserRequest{Authentication: auth}))
	if err != nil || current.Msg.User.Id != admin.Id {
		t.Fatalf("current user: %v", err)
	}
	listed, err := harness.service.ListUsers(ctx, connect.NewRequest(&tammyv1.ListUsersRequest{Authentication: auth, Page: &tammyv1.PageRequest{PageSize: 20}}))
	if err != nil || len(listed.Msg.Users) != 1 || listed.Msg.Users[0].Roles[0] != tammyv1.Role_ROLE_WORKSPACE_ADMIN {
		t.Fatalf("list users: %v", err)
	}
}
