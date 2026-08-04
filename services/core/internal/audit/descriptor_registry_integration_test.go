//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

type oversizedDescriptorQueryGuard struct {
	Executor
	blobQueries         int
	rawTimestampQueries int
	createdAt           string
	oversizedCreatedAt  bool
}

func (guard *oversizedDescriptorQueryGuard) QueryContext(ctx context.Context, query string, arguments ...any) (*sql.Rows, error) {
	if strings.Contains(query, "SELECT descriptor_set,") {
		guard.blobQueries++
		return guard.Executor.QueryContext(ctx, query, arguments...)
	}
	if strings.Contains(query, "length(descriptor_set)") {
		if guard.oversizedCreatedAt {
			if !strings.Contains(query, "length(CAST(created_at AS BLOB)) = ?") {
				guard.rawTimestampQueries++
				return guard.Executor.QueryContext(ctx, `SELECT ?, ?`, 1, strings.Repeat("x", 1<<20))
			}
			return guard.Executor.QueryContext(ctx, `SELECT 1, 'unreachable' WHERE 0`)
		}
		return guard.Executor.QueryContext(ctx, `SELECT ?, ?`, maxEvidenceArchiveMember+1, guard.createdAt)
	}
	return guard.Executor.QueryContext(ctx, query, arguments...)
}

func TestDescriptorSetRegistryPersistsLoadsAndRejectsMutation(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	descriptors := testAuditDescriptorSet(t)
	wantFingerprint := sha256.Sum256(descriptors)
	createdAt := time.Date(2026, 8, 4, 9, 10, 11, 12, time.UTC)

	transaction, err := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := PersistDescriptorSet(ctx, transaction, descriptors, createdAt)
	if err != nil || fingerprint != wantFingerprint {
		t.Fatalf("persist fingerprint=%x err=%v, want %x", fingerprint, err, wantFingerprint)
	}
	descriptors[0] ^= 0xff
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadDescriptorSet(ctx, database, wantFingerprint)
	if err != nil || sha256.Sum256(loaded) != wantFingerprint {
		t.Fatalf("loaded descriptor hash=%x err=%v", sha256.Sum256(loaded), err)
	}
	loaded[0] ^= 0xff
	reloaded, err := LoadDescriptorSet(ctx, database, wantFingerprint)
	if err != nil || sha256.Sum256(reloaded) != wantFingerprint {
		t.Fatalf("registry load aliased caller bytes: hash=%x err=%v", sha256.Sum256(reloaded), err)
	}

	for _, statement := range []string{
		`UPDATE audit_descriptor_sets_v1 SET descriptor_set=x'01' WHERE fingerprint=?`,
		`DELETE FROM audit_descriptor_sets_v1 WHERE fingerprint=?`,
		`INSERT OR REPLACE INTO audit_descriptor_sets_v1(fingerprint, descriptor_set, created_at) VALUES (?, x'01', '2026-08-04T09:10:11.000000012Z')`,
	} {
		if _, err := database.ExecContext(ctx, statement, wantFingerprint[:]); err == nil {
			t.Fatalf("immutable descriptor registry accepted %q", statement)
		}
	}
	if _, err := LoadDescriptorSet(ctx, database, wantFingerprint); err != nil {
		t.Fatalf("failed mutations changed retained descriptor: %v", err)
	}

	for _, testCase := range []struct {
		name        string
		fingerprint any
		descriptor  []byte
		createdAt   string
	}{
		{name: "null fingerprint", fingerprint: nil, descriptor: []byte{1}, createdAt: "2026-08-04T09:10:11.000000012Z"},
		{name: "short fingerprint", fingerprint: bytes.Repeat([]byte{1}, sha256.Size-1), descriptor: []byte{1}, createdAt: "2026-08-04T09:10:11.000000012Z"},
		{name: "empty descriptor", fingerprint: bytes.Repeat([]byte{1}, sha256.Size), createdAt: "2026-08-04T09:10:11.000000012Z"},
		{name: "noncanonical timestamp", fingerprint: bytes.Repeat([]byte{2}, sha256.Size), descriptor: []byte{1}, createdAt: "2026-08-04 09:10:11Z"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := database.ExecContext(ctx, `INSERT INTO audit_descriptor_sets_v1(fingerprint, descriptor_set, created_at)
				VALUES (?, ?, ?)`, testCase.fingerprint, testCase.descriptor, testCase.createdAt); err == nil {
				t.Fatal("invalid descriptor registry row was accepted")
			}
		})
	}
}

func TestDescriptorSetRegistryRejectsNoncanonicalAndMalformedSets(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	canonicalSet := testAuditDescriptorSet(t)
	noncanonicalSet := reverseDescriptorFiles(t, canonicalSet)
	createdAt := time.Date(2026, 8, 4, 9, 10, 11, 12, time.UTC)

	transaction, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if _, err := PersistDescriptorSet(ctx, transaction, noncanonicalSet, createdAt); !errors.Is(err, ErrDescriptorSetRegistry) {
		t.Fatalf("noncanonical persist error=%v, want ErrDescriptorSetRegistry", err)
	}
	if _, err := PersistDescriptorSet(ctx, transaction, []byte("not a descriptor set"), createdAt); !errors.Is(err, ErrDescriptorSetRegistry) {
		t.Fatalf("malformed persist error=%v, want ErrDescriptorSetRegistry", err)
	}
	_ = transaction.Rollback()

	malformed := []byte("not a descriptor set")
	malformedFingerprint := sha256.Sum256(malformed)
	if _, err := database.ExecContext(ctx, `INSERT INTO audit_descriptor_sets_v1(fingerprint, descriptor_set, created_at)
		VALUES (?, ?, ?)`, malformedFingerprint[:], malformed, formatTimestamp(createdAt)); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDescriptorSet(ctx, database, malformedFingerprint); !errors.Is(err, ErrDescriptorSetRegistry) {
		t.Fatalf("malformed load error=%v, want ErrDescriptorSetRegistry", err)
	}
}

func TestDescriptorSetRegistryRejectsOversizedLengthBeforeBlobQuery(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	guard := &oversizedDescriptorQueryGuard{
		Executor:  database,
		createdAt: formatTimestamp(time.Date(2026, 8, 4, 9, 10, 11, 12, time.UTC)),
	}

	if _, err := LoadDescriptorSet(ctx, guard, sha256.Sum256([]byte("oversized descriptor"))); !errors.Is(err, ErrDescriptorSetRegistry) {
		t.Fatalf("oversized descriptor error=%v, want ErrDescriptorSetRegistry", err)
	}
	if guard.blobQueries != 0 {
		t.Fatalf("oversized descriptor executed %d BLOB queries, want 0", guard.blobQueries)
	}
}

func TestDescriptorSetRegistryRejectsOversizedTimestampBeforeRawTextOrBlobQuery(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	guard := &oversizedDescriptorQueryGuard{
		Executor:           database,
		createdAt:          formatTimestamp(time.Date(2026, 8, 4, 9, 10, 11, 12, time.UTC)),
		oversizedCreatedAt: true,
	}

	if _, err := LoadDescriptorSet(ctx, guard, sha256.Sum256([]byte("oversized descriptor timestamp"))); !errors.Is(err, ErrDescriptorSetRegistry) {
		t.Fatalf("oversized descriptor timestamp error=%v, want ErrDescriptorSetRegistry", err)
	}
	if guard.rawTimestampQueries != 0 {
		t.Fatalf("oversized descriptor timestamp executed %d raw timestamp queries, want 0", guard.rawTimestampQueries)
	}
	if guard.blobQueries != 0 {
		t.Fatalf("oversized descriptor timestamp executed %d BLOB queries, want 0", guard.blobQueries)
	}
}

func TestOversizedDescriptorQueryGuardCountsBlobProjectionBeforeMetadataPatterns(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	guard := &oversizedDescriptorQueryGuard{
		Executor:  database,
		createdAt: formatTimestamp(time.Date(2026, 8, 4, 9, 10, 11, 12, time.UTC)),
	}

	rows, err := guard.QueryContext(ctx, `SELECT descriptor_set, created_at
		FROM audit_descriptor_sets_v1
		WHERE fingerprint = ? AND length(descriptor_set) = ? AND created_at = ?`,
		[]byte("fingerprint"), 1, guard.createdAt)
	if err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	if guard.blobQueries != 1 {
		t.Fatalf("BLOB projection count=%d, want 1", guard.blobQueries)
	}
}
