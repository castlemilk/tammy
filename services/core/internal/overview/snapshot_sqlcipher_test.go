//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package overview

import (
	"context"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"path/filepath"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
)

func TestSQLCipherSnapshotPortReadsBlankWorkspaceRevisionVector(t *testing.T) {
	path := filepath.Join(t.TempDir(), "overview.db")
	key := make([]byte, sqlcipher.KeySize)
	for index := range key {
		key[index] = byte(index + 1)
	}
	if _, err := sqlcipher.MigrateWorkspace(context.Background(), path, key, 9); err != nil {
		t.Fatal(err)
	}
	database, err := sqlcipher.Open(context.Background(), path, key)
	clear(key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	port, err := NewSQLCipherSnapshotPort(database, overviewOrganisationID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := port.Attention(context.Background(), overviewOrganisationID, &tammyv1.ReportingPeriod{StartDate: &tammyv1.CivilDate{Year: 2026, Month: 7, Day: 1}, EndDate: &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 30}})
	if err != nil {
		t.Fatalf("Attention() error = %v", err)
	}
	if snapshot.Revisions != (RevisionVector{}) {
		t.Fatalf("revisions = %#v", snapshot.Revisions)
	}
}
