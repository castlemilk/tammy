//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package reporting

import (
	"bytes"
	"context"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/proto"
)

func TestPurchaseFingerprintMigrationPreservesOldDirectCreditSnapshotBytes(t *testing.T) {
	ctx := context.Background()
	tx, repo, database, path, key := reportFixtureAtVersion(t, tammyv1.GstBasis_GST_BASIS_NON_CASH, 11)
	addReportPurchase(t, tx, 100, "2026-10-01", 1100, 100)
	saved, err := repo.CreateBASDraft(ctx, reportID(9000), reportID(9001), reportOrgID, reportDate("2026-10-01"), reportDate("2026-12-31"), time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if saved.PurchaseSourceFormat != nil || saved.GstCredits_1B.MinorUnits != 100 || len(saved.Sources) != 1 || len(saved.PurchaseReviews) != 0 {
		t.Fatal("pre-migration direct credit fixture did not retain old format")
	}
	original, err := proto.MarshalOptions{Deterministic: true}.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlcipher.MigrateWorkspace(ctx, path, key, 12); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlcipher.Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	read, err := reopened.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer read.Rollback()
	modern, err := NewRepositoryWithClock(read, func() time.Time { return time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := modern.GetCurrentBASDraft(ctx, reportOrgID)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.SourcesChanged || loaded.PurchaseSourceFormat != nil || loaded.GstCredits_1B.MinorUnits != 100 || len(loaded.Sources) != 1 || loaded.LegacyCandidateGst != nil || len(loaded.PurchaseReviews) != 0 {
		t.Fatal("migration backfilled or rewrote saved pre-format figures")
	}
	loaded.SourcesChanged = false
	actual, err := proto.MarshalOptions{Deterministic: true}.Marshal(loaded)
	if err != nil || !bytes.Equal(original, actual) {
		t.Fatalf("migration changed retained workpaper/source bytes: %v", err)
	}
	if got := purchaseSnapshotMetadataCount(t, read); got != 0 {
		t.Fatalf("migration backfilled %d new-format snapshots", got)
	}
}

func purchaseSnapshotMetadataCount(t *testing.T, tx *sqlcipher.Transaction) int64 {
	t.Helper()
	var count int64
	if querySingle(tx, context.Background(), `SELECT count(*) FROM bas_workpaper_purchase_metadata_v1`, nil, &count) != nil {
		t.Fatal("read purchase metadata count")
	}
	return count
}
