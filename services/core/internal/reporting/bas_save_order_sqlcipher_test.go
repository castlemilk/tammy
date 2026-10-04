//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package reporting

import (
	"context"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
)

func TestCurrentBASDraftUsesSaveOrderInsteadOfClockOrRandomID(t *testing.T) {
	firstTime := time.Date(2026, 10, 4, 0, 0, 0, 900000000, time.UTC)
	for _, test := range []struct {
		name       string
		secondTime time.Time
	}{
		{"fractional timestamp prefix", firstTime.Add(10 * time.Millisecond)},
		{"equal clock and descending random IDs", firstTime},
		{"clock moves backwards", firstTime.Add(-time.Hour)},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, repository := reportFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
			ctx := context.Background()
			first, err := repository.CreateBASDraft(ctx, reportID(1900), reportID(1901), reportOrgID,
				reportDate("2026-07-01"), reportDate("2026-09-30"), firstTime)
			if err != nil {
				t.Fatal(err)
			}
			second, err := repository.CreateBASDraft(ctx, reportID(1800), reportID(1801), reportOrgID,
				reportDate("2026-10-01"), reportDate("2026-12-31"), test.secondTime)
			if err != nil {
				t.Fatal(err)
			}
			current, err := repository.GetCurrentBASDraft(ctx, reportOrgID)
			if err != nil {
				t.Fatal(err)
			}
			if current.Id != second.Id || civilDateString(current.PeriodStart) != "2026-10-01" {
				t.Fatalf("current draft = %s (%s), want most recently saved draft %s", current.Id, civilDateString(current.PeriodStart), second.Id)
			}
			replay, err := repository.CreateBASDraft(ctx, reportID(1900), reportID(1701), reportOrgID,
				reportDate("2026-07-01"), reportDate("2026-09-30"), firstTime.Add(time.Hour))
			if err != nil || replay.Id != first.Id {
				t.Fatalf("replay = %v, error = %v; want original saved draft", replay, err)
			}
			current, err = repository.GetCurrentBASDraft(ctx, reportOrgID)
			if err != nil || current.Id != second.Id {
				t.Fatalf("replaying an earlier save changed the current draft: %v, %v", current, err)
			}
		})
	}
}

func TestBASSaveOrderBackfillsLegacyPapersAndSurvivesVacuumAndRestart(t *testing.T) {
	ctx := context.Background()
	tx, _, database, path, key := reportFixtureAtVersion(t, tammyv1.GstBasis_GST_BASIS_NON_CASH, 10)
	// Deliberately leave gaps in hidden rowids so VACUUM must rewrite them.
	for _, paper := range []struct {
		rowID      int
		id         int
		start, end string
		createdAt  string
	}{
		{100, 1901, "2026-07-01", "2026-09-30", "2026-10-04T00:00:00.9Z"},
		{900, 1801, "2026-10-01", "2026-12-31", "2026-10-04T00:00:00.91Z"},
	} {
		reportExec(t, tx, `INSERT INTO bas_workpapers(rowid,id,operation_key,organisation_id,version,period_start,period_end,status,sales_g1_minor,gst_on_sales_1a_minor,gst_credits_1b_minor,net_gst_payable_minor,created_at)
			VALUES(?,?,?,?,1,?,?,'DRAFT_NOT_LODGED',0,0,0,0,?)`, paper.rowID, reportID(paper.id), reportID(paper.id-1), reportOrgID, paper.start, paper.end, paper.createdAt)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlcipher.MigrateWorkspace(ctx, path, key, 11); err != nil {
		t.Fatal(err)
	}
	database, err := sqlcipher.Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	repository, err := NewRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	assertCurrentBASPaper(t, repository, reportID(1801))
	var firstSequence, secondSequence int64
	if err := database.QueryRowContext(ctx, `SELECT sequence FROM bas_workpaper_save_order_v1 WHERE workpaper_id=?`, reportID(1901)).Scan(&firstSequence); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT sequence FROM bas_workpaper_save_order_v1 WHERE workpaper_id=?`, reportID(1801)).Scan(&secondSequence); err != nil {
		t.Fatal(err)
	}
	if firstSequence >= secondSequence {
		t.Fatalf("legacy save order = %d, %d; want insertion order", firstSequence, secondSequence)
	}
	for _, sql := range []string{
		`UPDATE bas_workpaper_save_order_v1 SET sequence=sequence+100`,
		`UPDATE bas_workpaper_save_order_v1 SET workpaper_id='018f0c65-7b00-7000-8000-000000000001'`,
		`DELETE FROM bas_workpaper_save_order_v1`,
	} {
		if _, err := database.ExecContext(ctx, sql); err == nil {
			t.Fatalf("retained save order was mutable: %s", sql)
		}
	}
	if _, err := database.ExecContext(ctx, `VACUUM`); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	database, err = sqlcipher.Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	repository, err = NewRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	assertCurrentBASPaper(t, repository, reportID(1801))
	var persistedSequence int64
	if err := database.QueryRowContext(ctx, `SELECT sequence FROM bas_workpaper_save_order_v1 WHERE workpaper_id=?`, reportID(1801)).Scan(&persistedSequence); err != nil || persistedSequence != secondSequence {
		t.Fatalf("save order changed across VACUUM and restart: %d, %v", persistedSequence, err)
	}
	transaction, err := database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	transactionRepository, err := NewRepository(transaction)
	if err != nil {
		t.Fatal(err)
	}
	third, err := transactionRepository.CreateBASDraft(ctx, reportID(1700), reportID(1701), reportOrgID,
		reportDate("2027-01-01"), reportDate("2027-03-31"), time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	assertCurrentBASPaper(t, transactionRepository, third.Id)
	if err := transaction.Rollback(); err != nil {
		t.Fatal(err)
	}
	assertCurrentBASPaper(t, repository, reportID(1801))
	var rolledBackCount int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM bas_workpaper_save_order_v1 WHERE workpaper_id=?`, third.Id).Scan(&rolledBackCount); err != nil || rolledBackCount != 0 {
		t.Fatalf("save-order evidence escaped the workpaper transaction: %d, %v", rolledBackCount, err)
	}
	transaction, err = database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	transactionRepository, err = NewRepository(transaction)
	if err != nil {
		t.Fatal(err)
	}
	third, err = transactionRepository.CreateBASDraft(ctx, reportID(1700), reportID(1701), reportOrgID,
		reportDate("2027-01-01"), reportDate("2027-03-31"), time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	database, err = sqlcipher.Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	repository, err = NewRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	assertCurrentBASPaper(t, repository, third.Id)
	replay, err := repository.CreateBASDraft(ctx, reportID(1900), reportID(1601), reportOrgID,
		reportDate("2026-07-01"), reportDate("2026-09-30"), time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC))
	if err != nil || replay.Id != reportID(1901) {
		t.Fatalf("legacy operation replay = %v, %v; want original paper", replay, err)
	}
	assertCurrentBASPaper(t, repository, third.Id)
	var retainedCount int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM bas_workpaper_save_order_v1`).Scan(&retainedCount); err != nil || retainedCount != 3 {
		t.Fatalf("committed save or replay created the wrong amount of retained evidence: %d, %v", retainedCount, err)
	}
}

func assertCurrentBASPaper(t *testing.T, repository *Repository, id string) {
	t.Helper()
	current, err := repository.GetCurrentBASDraft(context.Background(), reportOrgID)
	if err != nil || current.Id != id {
		t.Fatalf("current paper = %v, error = %v; want %s", current, err, id)
	}
}
