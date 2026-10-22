//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/audit"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
)

type fixedCopyProjectionFactoryFunc func(context.Context, *sqlcipher.Database) (FixedCopyProjectionSet, error)

func (function fixedCopyProjectionFactoryFunc) OpenFixedCopy(
	ctx context.Context,
	database *sqlcipher.Database,
) (FixedCopyProjectionSet, error) {
	return function(ctx, database)
}

type fixedCopyProjectionSet struct {
	header  func(context.Context, SnapshotRequest) ([]byte, error)
	sources func(context.Context) ([]ProjectionSourceRegistration, error)
	close   func() error
}

func (set *fixedCopyProjectionSet) SnapshotHeader(ctx context.Context, request SnapshotRequest) ([]byte, error) {
	return set.header(ctx, request)
}

func (set *fixedCopyProjectionSet) ProjectionSources(ctx context.Context) ([]ProjectionSourceRegistration, error) {
	return set.sources(ctx)
}

func (set *fixedCopyProjectionSet) Close() error {
	return set.close()
}

func TestSQLCipherSnapshotSourceProjectsLineageAndProvidersFromFixedCopy(t *testing.T) {
	ctx := context.Background()
	const workspaceID = "018f0000-0000-7000-8000-000000000051"
	key := make([]byte, sqlcipher.KeySize)
	for index := range key {
		key[index] = byte(index + 1)
	}
	defer zero(key)
	livePath := t.TempDir() + "/live.db"
	if _, err := sqlcipher.MigrateWorkspace(ctx, livePath, key, 3); err != nil {
		t.Fatal(err)
	}
	live, err := sqlcipher.Open(ctx, livePath, key)
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	firstSalt := bytes.Repeat([]byte{0x21}, sha256.Size)
	firstGenesis, err := audit.Genesis(workspaceID, firstSalt)
	if err != nil {
		t.Fatal(err)
	}
	createdAt := time.Date(2026, 8, 9, 1, 0, 0, 0, time.UTC)
	root, _, err := audit.GenerateSigningKey(workspaceID, bytes.Repeat([]byte{0x41}, 32), createdAt,
		bytes.NewReader(bytes.Repeat([]byte{0x61}, 128)))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := live.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := audit.InitializeChain(ctx, tx, audit.ChainHeader{WorkspaceID: workspaceID, Generation: 1,
		ChainSalt: firstSalt, GenesisHash: firstGenesis, CurrentHead: firstGenesis, CreatedAt: createdAt}); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := audit.PersistSigningKey(ctx, tx, root); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := audit.InitializeSigningKeyState(ctx, tx, root); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE backup_projection_state(revision INTEGER NOT NULL)`); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO backup_projection_state(revision) VALUES(1)`); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	readRevision := func(ctx context.Context, reader SQLExecutor) (int, error) {
		rows, err := reader.QueryContext(ctx, `SELECT revision FROM backup_projection_state`)
		if err != nil {
			return 0, err
		}
		defer rows.Close()
		var revision int
		if !rows.Next() || rows.Scan(&revision) != nil || rows.Next() || rows.Err() != nil {
			return 0, ErrService
		}
		return revision, nil
	}
	registry, err := NewProviderRegistry([]ProviderRegistration{{Name: "rules", Version: 1,
		Provider: providerFunc(func(_ context.Context, _ SnapshotReader, request SnapshotRequest) (Projection, error) {
			if request.AuditGeneration != 1 || !bytes.Equal(request.AuditHead, firstGenesis[:]) {
				t.Fatalf("provider request generation/head = %d/%x, want copied generation 1", request.AuditGeneration, request.AuditHead)
			}
			return Projection{Objects: []Object{{Path: "rules/current.pb", Bytes: []byte("1")}}}, nil
		})}})
	if err != nil {
		t.Fatal(err)
	}
	secondSalt := bytes.Repeat([]byte{0x22}, sha256.Size)
	secondGenesis, err := audit.Genesis(workspaceID, secondSalt)
	if err != nil {
		t.Fatal(err)
	}
	stagingDirectory := t.TempDir()
	stagingAuthenticationKey := testSnapshotStagingAuthenticationKey()
	source, err := NewSQLCipherSnapshotSource(SQLCipherSnapshotSourceConfig{
		Live: live,
		Key:  key,
		Staging: SQLCipherStagedCaptureConfig{Directory: stagingDirectory, AuthenticationKey: stagingAuthenticationKey,
			NewID: func() (string, error) { return "018f0000-0000-7000-8000-000000000052", nil },
			hooks: &captureHooks{afterBackup: func() error {
				if _, err := live.ExecContext(ctx, `UPDATE backup_projection_state SET revision=2`); err != nil {
					return err
				}
				return audit.InitializeChain(ctx, live, audit.ChainHeader{WorkspaceID: workspaceID, Generation: 2,
					ChainSalt: secondSalt, GenesisHash: secondGenesis, CurrentHead: secondGenesis,
					CreatedAt: createdAt.Add(time.Hour)})
			}},
		},
		Projections: fixedCopyProjectionFactoryFunc(func(_ context.Context, staged *sqlcipher.Database) (FixedCopyProjectionSet, error) {
			opened := true
			set := &fixedCopyProjectionSet{}
			set.header = func(ctx context.Context, _ SnapshotRequest) ([]byte, error) {
				if !opened {
					return nil, ErrService
				}
				revision, err := readRevision(ctx, staged)
				return []byte("header-" + strconv.Itoa(revision)), err
			}
			set.sources = func(context.Context) ([]ProjectionSourceRegistration, error) {
				return []ProjectionSourceRegistration{{Name: "rules", Version: 1,
					Source: projectionSourceFunc(func(ctx context.Context, _ SnapshotRequest) (Projection, error) {
						if !opened {
							return Projection{}, ErrService
						}
						revision, err := readRevision(ctx, staged)
						return Projection{Objects: []Object{{Path: "rules/current.pb", Bytes: []byte(strconv.Itoa(revision))}}}, err
					})}}, nil
			}
			set.close = func() error { opened = false; return nil }
			return set, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	captured, err := source.ConsistentSnapshot(ctx, workspaceID, registry)
	if err != nil {
		t.Fatal(err)
	}
	if captured.Lineage.Generation != 1 || captured.Lineage.Sequence != 0 ||
		!bytes.Equal(captured.Lineage.Head, firstGenesis[:]) || captured.Lineage.SigningKeyID != root.KeyID ||
		captured.Lineage.SigningKeyEpoch != 1 || !bytes.Equal(captured.Lineage.PublicKey, root.PublicKey) ||
		len(captured.Lineage.Root) != sha256.Size {
		t.Fatalf("captured lineage = %#v", captured.Lineage)
	}
	if string(captured.Workspace.Header) != "header-1" || len(captured.ProviderObjects) != 1 ||
		string(captured.ProviderObjects[0].Bytes) != "1" {
		t.Fatalf("fixed-copy header/objects = %q/%#v", captured.Workspace.Header, captured.ProviderObjects)
	}
	liveHeader, err := audit.LoadChainHeader(ctx, live, workspaceID, 0)
	if err != nil || liveHeader.Generation != 2 {
		t.Fatalf("live header after capture = %#v, %v", liveHeader, err)
	}
	restoredPath := t.TempDir() + "/captured.db"
	if err := os.WriteFile(restoredPath, captured.Workspace.Database, 0o600); err != nil {
		t.Fatal(err)
	}
	staged, err := sqlcipher.Open(ctx, restoredPath, key)
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Close()
	stagedHeader, err := audit.LoadChainHeader(ctx, staged, workspaceID, 0)
	if err != nil || stagedHeader.Generation != 1 || stagedHeader.CurrentHead != firstGenesis {
		t.Fatalf("captured database header = %#v, %v", stagedHeader, err)
	}
}
