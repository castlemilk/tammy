//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package restore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"path/filepath"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
)

func TestPreRestoreArchiveMetadataSurvivesRestartWithSignedStableCursor(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "workspace.db")
	key := bytes.Repeat([]byte{0x31}, sqlcipher.KeySize)
	defer zeroBytes(key)
	_, err := sqlcipher.MigrateWorkspace(ctx, path, key, 4)
	if err != nil {
		t.Fatal(err)
	}
	database, err := sqlcipher.Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	workspaceID := "018f0000-0000-7000-8000-000000000001"
	createdAt := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	archiveIDs := []string{
		"018f0000-0000-7000-8000-000000000081",
		"018f0000-0000-7000-8000-000000000082",
	}
	operationIDs := []string{
		"018f0000-0000-7000-8000-000000000091",
		"018f0000-0000-7000-8000-000000000092",
	}
	for index, archiveID := range archiveIDs {
		record := PreRestoreArchiveRecord{WorkspaceID: workspaceID, ArchiveID: archiveID,
			OperationID: operationIDs[index], Version: 1,
			State:              tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_AVAILABLE,
			CreatedAt:          createdAt.Add(time.Duration(index) * time.Second),
			DeletionEligibleAt: createdAt.Add(time.Duration(index)*time.Second).AddDate(1, 0, 0),
			ContentHash:        bytes.Repeat([]byte{byte(index + 1)}, sha256.Size), SourceGeneration: uint64(5 + index),
			EncryptedByteLength: 4096 + uint64(index)}
		if err := PersistPreRestoreArchive(ctx, database, record); err != nil {
			t.Fatalf("persist archive %d: %v", index, err)
		}
	}
	codec, err := paging.NewCodec(bytes.Repeat([]byte{0x55}, 32))
	if err != nil {
		t.Fatal(err)
	}
	repository, err := NewPreRestoreArchiveRepository(database, codec)
	if err != nil {
		t.Fatal(err)
	}
	first, err := repository.List(ctx, PreRestoreArchiveList{WorkspaceID: workspaceID, PageSize: 1})
	if err != nil || len(first.Archives) != 1 || first.Page == nil || first.Page.NextCursor == nil {
		t.Fatalf("first page=%#v error=%v", first, err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	database, err = sqlcipher.Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	repository, err = NewPreRestoreArchiveRepository(database, codec)
	if err != nil {
		t.Fatal(err)
	}
	second, err := repository.List(ctx, PreRestoreArchiveList{WorkspaceID: workspaceID, PageSize: 1,
		Cursor: first.Page.NextCursor})
	if err != nil || len(second.Archives) != 1 || second.Archives[0].Id == first.Archives[0].Id ||
		second.Page == nil || second.Page.NextCursor != nil {
		t.Fatalf("second page=%#v error=%v", second, err)
	}
	got, err := repository.Get(ctx, workspaceID, second.Archives[0].Id)
	if err != nil || got.Version != 1 || got.State != tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_AVAILABLE ||
		len(got.ContentHash) != sha256.Size || got.SourceGeneration != 6 {
		t.Fatalf("Get()=%#v error=%v", got, err)
	}
	tampered := *first.Page.NextCursor
	tampered = tampered[:len(tampered)-1] + "A"
	if _, err := repository.List(ctx, PreRestoreArchiveList{WorkspaceID: workspaceID, PageSize: 1,
		Cursor: &tampered}); !errors.Is(err, ErrPreRestoreArchiveRepository) {
		t.Fatalf("tampered cursor error=%v", err)
	}
}
