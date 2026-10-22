//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"context"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
)

// BAS history queries need a stable read snapshot without reserving SQLite's
// single writer slot. A long retained-proof read must not prevent the identity
// service from persisting a fresh-factor assertion or other authorized writes.
func TestBeginBASReadAllowsConcurrentWriter(t *testing.T) {
	server := newDocumentPageServer(t)
	cursors, err := paging.NewCodec(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	handler := &basPreparationHandler{
		database: server.activation.Database,
		identity: server.activation.Identity,
		cursors:  cursors,
	}
	// Open and pin the independent connection before starting the timed part;
	// SQLCipher key setup on a newly opened connection is outside the writer-slot
	// behavior this test is measuring.
	writer, err := server.activation.Database.BeginEncryptedTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("pre-open deferred writer transaction: %v", err)
	}
	defer func() { _ = writer.Rollback() }()
	readTx, err := handler.beginBASRead(context.Background(), server.authentication)
	if err != nil {
		t.Fatalf("begin BAS read: %v", err)
	}
	defer func() { _ = readTx.Rollback() }()

	writerStarted := make(chan struct{})
	writerFinished := make(chan error, 1)
	go func() {
		close(writerStarted)
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_, err := writer.ExecContext(ctx, `UPDATE user_roles SET role_code=role_code WHERE user_id=?`, server.authentication.ActorUserId)
		if err == nil {
			err = writer.Commit()
		} else {
			_ = writer.Rollback()
		}
		writerFinished <- err
	}()
	<-writerStarted

	select {
	case err := <-writerFinished:
		if err != nil {
			t.Fatalf("concurrent writer was admitted but failed: %v", err)
		}
	case <-time.After(time.Second):
		// Release before failing so a regression cannot leave a blocked writer
		// behind to stall cleanup or later tests.
		if err := readTx.Rollback(); err != nil {
			t.Fatalf("release BAS read after writer remained blocked: %v", err)
		}
		select {
		case err := <-writerFinished:
			if err != nil {
				t.Fatalf("writer failed after BAS read released: %v", err)
			}
		case <-time.After(7 * time.Second):
			t.Fatal("writer did not finish after BAS read released")
		}
		t.Fatal("BAS read reserved the SQLite writer slot and blocked an independent writer")
	}
}
