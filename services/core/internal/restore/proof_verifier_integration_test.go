//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package restore

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
)

func TestTransactionalRestoreProofVerifierRejectsReplayInRealSQLCipherTransaction(t *testing.T) {
	ctx := context.Background()
	key := bytes.Repeat([]byte{0x45}, sqlcipher.KeySize)
	defer zeroBytes(key)
	database := createRestoreDatabaseFixture(t, ctx, filepath.Join(t.TempDir(), "workspace.db"), key, "proof-replay")
	defer database.Close()
	if _, err := database.ExecContext(ctx, `CREATE TABLE restore_proof_replays_test(
		replay_key TEXT PRIMARY KEY NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	transactions, err := NewSQLCipherRestoreAuthenticationTransactions(database)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 10, 10, 0, 0, 0, time.UTC)
	workspaceID := "018f0000-0000-7000-8000-000000000011"
	actorID := "018f0000-0000-7000-8000-000000000012"
	sessionID := "018f0000-0000-7000-8000-000000000013"
	replayKey := "018f0000-0000-7000-8000-000000000014"
	issuedAt := now.Add(-time.Minute)
	providerReplay := errors.New("provider duplicate replay detail")
	verifier, err := NewTransactionalRestoreProofVerifier(TransactionalRestoreProofVerifierConfig{
		Transactions: transactions, Now: func() time.Time { return now },
		Authenticator: restoreAuthenticatorPortFunc(func(ctx context.Context, executor RestoreAuthenticationExecutor,
			request RestoreAuthenticationRequest,
		) (AuthenticatedRestoreProof, error) {
			if !executor.Authenticated() {
				return AuthenticatedRestoreProof{}, providerReplay
			}
			if _, err := executor.ExecContext(ctx, `INSERT INTO restore_proof_replays_test(replay_key) VALUES(?)`,
				request.ReplayKey); err != nil {
				return AuthenticatedRestoreProof{}, providerReplay
			}
			return validAuthenticatedRestoreProof(RestoreAuthenticationModeNormal, workspaceID, actorID, sessionID,
				replayKey, "018f0000-0000-7000-8000-000000000015", issuedAt), nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	proof := &AdminTOTPProof{AdminUserID: actorID, Password: []byte("administrator-password"), TOTP: "123456",
		IssuedAt: issuedAt, ReplayKey: replayKey}
	if authorization, err := verifier.AuthorizeRestore(ctx, workspaceID, proof); err != nil || authorization == nil {
		t.Fatalf("first authorization=%#v error=%v", authorization, err)
	}
	if authorization, err := verifier.AuthorizeRestore(ctx, workspaceID, proof); authorization != nil ||
		!errors.Is(err, ErrRestoreAuthorization) || !errors.Is(err, ErrRestoreAuthenticationProvider) ||
		errors.Is(err, providerReplay) {
		t.Fatalf("replay authorization=%#v error=%v", authorization, err)
	}
	rows, err := database.QueryContext(ctx, `SELECT count(*) FROM restore_proof_replays_test WHERE replay_key=?`, replayKey)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var count int
	if !rows.Next() || rows.Scan(&count) != nil || count != 1 || rows.Next() || rows.Err() != nil {
		t.Fatalf("committed replay rows=%d", count)
	}
}
