//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"connectrpc.com/connect"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

// The legacy BAS current-workpaper read is a retained snapshot read. It must
// finish from WAL while an independent writer owns SQLite's single writer
// reservation; it must not request BEGIN IMMEDIATE itself.
func TestGetCurrentBASDraftReadsWhileWriterIsHeld(t *testing.T) {
	server := newDocumentPageServer(t)
	ctx := context.Background()
	tax := &taxHandler{
		database: server.activation.Database,
		identity: server.activation.Identity,
		now:      server.activation.Now,
		newID:    server.activation.NewID,
	}
	created, err := tax.CreateBasDraft(ctx, connect.NewRequest(&tammyv1.CreateBasDraftRequest{
		CommandContext: &tammyv1.CommandContext{
			Authentication: server.authentication,
			IdempotencyKey: "018f0000-0000-7000-8000-00000000e901",
		},
		OrganisationId: server.organisationID,
		PeriodStart:    &tammyv1.CivilDate{Year: 2026, Month: 7, Day: 1},
		PeriodEnd:      &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 30},
	}))
	if err != nil || created == nil || created.Msg == nil || created.Msg.Workpaper == nil {
		t.Fatalf("create retained workpaper through tax handler: response=%v err=%v", created, err)
	}

	// Warm a deferred reader before measuring the competing read. The SQLCipher
	// connection setup cost is outside the timing window, while the held
	// BEGIN IMMEDIATE below still proves the handler itself does not wait for the
	// writer reservation.
	warmReader, err := server.activation.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin warm deferred reader: %v", err)
	}
	var warmedRoleVersion int64
	if err := warmReader.QueryRowContext(ctx,
		`SELECT repository_version FROM user_roles WHERE user_id=?`, server.authentication.ActorUserId).Scan(&warmedRoleVersion); err != nil {
		_ = warmReader.Rollback()
		t.Fatalf("warm deferred reader connection: %v", err)
	}

	writer, err := server.activation.Database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		_ = warmReader.Rollback()
		t.Fatalf("begin independent writer: %v", err)
	}
	defer func() { _ = writer.Rollback() }()
	if _, err := writer.ExecContext(ctx,
		`UPDATE user_roles SET role_code=role_code WHERE user_id=?`, server.authentication.ActorUserId); err != nil {
		_ = warmReader.Rollback()
		t.Fatalf("hold real SQLCipher writer slot: %v", err)
	}
	if err := warmReader.Rollback(); err != nil {
		t.Fatalf("release warm deferred reader after writer acquired slot: %v", err)
	}

	type readResult struct {
		response *connect.Response[tammyv1.GetCurrentBasDraftResponse]
		err      error
	}
	readFinished := make(chan readResult, 1)
	go func() {
		response, err := tax.GetCurrentBasDraft(ctx, connect.NewRequest(&tammyv1.GetCurrentBasDraftRequest{
			Authentication: server.authentication,
			OrganisationId: server.organisationID,
		}))
		readFinished <- readResult{response: response, err: err}
	}()

	select {
	case result := <-readFinished:
		if result.err != nil || result.response == nil || result.response.Msg == nil ||
			!proto.Equal(result.response.Msg.Workpaper, created.Msg.Workpaper) {
			t.Fatalf("current workpaper read while writer held = %v, %v; want exact retained workpaper %v",
				result.response, result.err, created.Msg.Workpaper)
		}
	case <-time.After(time.Second):
		// Always release the writer before failing so the old implementation's
		// busy wait cannot leak into subsequent tests.
		if err := writer.Rollback(); err != nil {
			t.Fatalf("release writer after current BAS read remained blocked: %v", err)
		}
		select {
		case result := <-readFinished:
			if result.err != nil || result.response == nil || result.response.Msg == nil ||
				!proto.Equal(result.response.Msg.Workpaper, created.Msg.Workpaper) {
				t.Fatalf("current workpaper read after writer release = %v, %v; want exact retained workpaper %v",
					result.response, result.err, created.Msg.Workpaper)
			}
		case <-time.After(7 * time.Second):
			t.Fatal("current BAS read did not finish after writer released")
		}
		t.Fatal("GetCurrentBasDraft reserved the SQLite writer slot instead of reading the retained workpaper from WAL")
	}
}
