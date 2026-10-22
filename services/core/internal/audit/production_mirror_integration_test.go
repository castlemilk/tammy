//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
)

func TestSQLMirrorVerifierAuthenticatesPagedSnapshotAndReturnsPredecessors(t *testing.T) {
	database := newEncryptedAuditDatabase(t)
	header := seedProductionMirrorChain(t, database, uint64(StoredEventPageSizeLimit)+1)
	transactions := &auditServiceTransactions{database: database, workspaceID: header.WorkspaceID}

	verifier, err := NewSQLMirrorVerifier(transactions)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := verifier.VerifyFull(context.Background(), header.WorkspaceID, header.Generation)
	if err != nil {
		t.Fatal(err)
	}
	if !verified.Valid || verified.Baseline == nil || verified.Baseline.WorkspaceId != header.WorkspaceID ||
		verified.Baseline.Generation != header.Generation || verified.Baseline.Sequence != header.CurrentSequence ||
		string(verified.Baseline.Head) != string(header.CurrentHead[:]) {
		t.Fatalf("verified chain=%#v, want authenticated terminal baseline", verified)
	}
	if len(verified.Heads) != int(header.CurrentSequence)+1 || string(verified.Heads[0]) != string(header.GenesisHash[:]) ||
		string(verified.Heads[header.CurrentSequence]) != string(header.CurrentHead[:]) {
		t.Fatalf("predecessor evidence=%d heads, want genesis through terminal", len(verified.Heads))
	}
}

func TestSQLMirrorVerifierFailsClosedOnMissingOrMutatedRows(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		trigger  string
		mutation string
	}{
		{name: "missing", trigger: `DROP TRIGGER audit_events_v1_no_delete`,
			mutation: `DELETE FROM audit_events_v1 WHERE sequence=1`},
		{name: "mutated", trigger: `DROP TRIGGER audit_events_v1_no_update`, mutation: `UPDATE audit_events_v1
			SET canonical_event=randomblob(length(canonical_event)) WHERE sequence=1`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			database := newEncryptedAuditDatabase(t)
			header := seedProductionMirrorChain(t, database, 1)
			if _, err := database.ExecContext(context.Background(), testCase.trigger); err != nil {
				t.Fatal(err)
			}
			if _, err := database.ExecContext(context.Background(), testCase.mutation); err != nil {
				t.Fatal(err)
			}
			verifier, err := NewSQLMirrorVerifier(&auditServiceTransactions{database: database, workspaceID: header.WorkspaceID})
			if err != nil {
				t.Fatal(err)
			}
			verified, err := verifier.VerifyFull(context.Background(), header.WorkspaceID, header.Generation)
			if !errors.Is(err, ErrMirrorInvalid) || verified.Valid || verified.Baseline != nil || len(verified.Heads) != 0 {
				t.Fatalf("verified=%#v error=%v, want closed invalid result", verified, err)
			}
		})
	}
}

func seedProductionMirrorChain(t *testing.T, database *sqlcipher.Database, count uint64) ChainHeader {
	t.Helper()
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	salt := bytes.Repeat([]byte{0x23}, sha256.Size)
	genesis, err := Genesis(workspaceID, salt)
	if err != nil {
		t.Fatal(err)
	}
	if err := InitializeChain(context.Background(), database, ChainHeader{WorkspaceID: workspaceID, Generation: 1,
		ChainSalt: salt, GenesisHash: genesis, CurrentHead: genesis,
		CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	for sequence := uint64(1); sequence <= count; sequence++ {
		event, payload := integrationAuditEvent(fmt.Sprintf("01890f60-4d6d-7c12-8f02-%012x", sequence))
		if _, err := appendStoredEventForTest(context.Background(), database, event, payload); err != nil {
			t.Fatalf("append event %d: %v", sequence, err)
		}
	}
	header, err := LoadChainHeader(context.Background(), database, workspaceID, 1)
	if err != nil {
		t.Fatal(err)
	}
	return header
}
