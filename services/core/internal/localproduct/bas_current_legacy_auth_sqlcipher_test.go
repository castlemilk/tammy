//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

type barrierLegacyBASReadIdentity struct {
	app.LocalModuleIdentity
	preflightPassed chan struct{}
	resumeRead      chan struct{}
}

func (identity barrierLegacyBASReadIdentity) RequireActiveSessionReadOnly(ctx context.Context,
	authentication *tammyv1.AuthenticationContext) error {
	if err := identity.LocalModuleIdentity.RequireActiveSessionReadOnly(ctx, authentication); err != nil {
		return err
	}
	close(identity.preflightPassed)
	select {
	case <-identity.resumeRead:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestGetCurrentBASDraftRejectsSessionRevokedAfterPreflight(t *testing.T) {
	server := newDocumentPageServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tax := &taxHandler{
		database: server.activation.Database,
		identity: server.activation.Identity,
		now:      server.activation.Now,
		newID:    server.activation.NewID,
	}
	created, err := tax.CreateBasDraft(ctx, connect.NewRequest(&tammyv1.CreateBasDraftRequest{
		CommandContext: &tammyv1.CommandContext{
			Authentication: server.authentication,
			IdempotencyKey: "018f0000-0000-7000-8000-00000000e902",
		},
		OrganisationId: server.organisationID,
		PeriodStart:    &tammyv1.CivilDate{Year: 2026, Month: 7, Day: 1},
		PeriodEnd:      &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 30},
	}))
	if err != nil || created == nil || created.Msg == nil || created.Msg.Workpaper == nil {
		t.Fatalf("create retained workpaper through tax handler: response=%v err=%v", created, err)
	}

	barrier := barrierLegacyBASReadIdentity{
		LocalModuleIdentity: server.activation.Identity,
		preflightPassed:     make(chan struct{}),
		resumeRead:          make(chan struct{}),
	}
	var releaseOnce sync.Once
	releaseRead := func() {
		releaseOnce.Do(func() { close(barrier.resumeRead) })
	}
	tax.identity = barrier
	type readResult struct {
		response *connect.Response[tammyv1.GetCurrentBasDraftResponse]
		err      error
	}
	readFinished := make(chan readResult, 1)
	readStarted := true
	readJoined := false
	defer func() {
		releaseRead()
		if readStarted && !readJoined {
			select {
			case <-readFinished:
				readJoined = true
			case <-time.After(8 * time.Second):
				t.Error("current BAS read did not exit during bounded cleanup")
			}
		}
		cancel()
		if readStarted && !readJoined {
			select {
			case <-readFinished:
			case <-time.After(time.Second):
				t.Error("current BAS read remained blocked after context cancellation")
			}
		}
	}()
	go func() {
		response, err := tax.GetCurrentBasDraft(ctx, connect.NewRequest(&tammyv1.GetCurrentBasDraftRequest{
			Authentication: server.authentication,
			OrganisationId: server.organisationID,
		}))
		readFinished <- readResult{response: response, err: err}
	}()
	select {
	case <-barrier.preflightPassed:
	case <-time.After(5 * time.Second):
		t.Fatal("current BAS read did not reach the successful session preflight")
	}

	// Revoke the session in the real encrypted database while the handler is
	// paused between its initial preflight and the retained-workpaper read.
	revocation, err := server.activation.Database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatalf("begin session revocation: %v", err)
	}
	result, err := revocation.ExecContext(ctx, `UPDATE application_sessions
		SET state=?, ended_at=?, repository_version=repository_version+1
		WHERE id=? AND state=?`,
		int32(tammyv1.SessionState_SESSION_STATE_SIGNED_OUT), server.activation.Now().UTC().Format(time.RFC3339Nano),
		server.authentication.SessionId, int32(tammyv1.SessionState_SESSION_STATE_ACTIVE))
	if err != nil {
		_ = revocation.Rollback()
		t.Fatalf("revoke current session in SQLCipher: %v", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		_ = revocation.Rollback()
		t.Fatalf("session revocation changed %d rows: %v", rows, err)
	}
	if err := revocation.Commit(); err != nil {
		t.Fatalf("commit session revocation: %v", err)
	}
	releaseRead()

	select {
	case result := <-readFinished:
		readJoined = true
		if result.err == nil || result.response != nil || connect.CodeOf(result.err) != connect.CodePermissionDenied {
			t.Fatalf("current BAS read after session revocation = %v, %v; want permission-denied rejection", result.response, result.err)
		}
	case <-time.After(7 * time.Second):
		t.Fatal("current BAS read did not finish after session revocation")
	}
}
