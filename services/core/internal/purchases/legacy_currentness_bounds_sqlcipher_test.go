//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
)

type legacyCurrentnessJournalProbe struct {
	app.CommandSQLExecutor
	journalIDs []string
}

func (probe *legacyCurrentnessJournalProbe) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	// Get hydrates financial header fields here. The bounded closure selector
	// reads only identities, versions and reciprocal links through another query.
	if strings.Contains(strings.Join(strings.Fields(query), " "), "FROM journals WHERE id = ?") {
		if len(args) == 1 {
			if id, ok := args[0].(string); ok {
				probe.journalIDs = append(probe.journalIDs, id)
			}
		}
	}
	return probe.CommandSQLExecutor.QueryContext(ctx, query, args...)
}

func TestLegacyCurrentnessAccepts1000JournalClosureAndRejects1001BeforeClosureBodies(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	original, cashID := seedLegacyReversalLinkedJournal(t, f, f.document.Id, false)
	terminal := original
	expectedIDs := []string{original.Id}
	// Every node and reciprocal link is made by the real accounting writer.
	// An even-length chain ends in a cash debit, so explicit unpaid evidence
	// is not contradicted by any current cash-credit journal.
	for len(expectedIDs) < 1000 {
		_, terminal = reverseLegacyReversalLinkedJournal(t, f, terminal)
		expectedIDs = append(expectedIDs, terminal.Id)
	}
	if got := postScalar(t, tx, `SELECT SUM(debit_minor-credit_minor) FROM journal_lines WHERE journal_id=? AND account_id=?`, terminal.Id, cashID); got != 11000 {
		t.Fatalf("1,000-node chain terminal cash movement=%d, want debit 11000", got)
	}
	request := legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_CONFIRMED_UNPAID_THROUGH_BALANCE)
	probe := &legacyCurrentnessJournalProbe{CommandSQLExecutor: tx}
	resolved, err := legacyRepo(t, f, probe).ResolveLegacy(ctx, request)
	if err != nil || resolved == nil || resolved.Review == nil {
		t.Fatalf("exact 1,000-node owner closure failed: review=%v err=%v", resolved, err)
	}
	sort.Strings(expectedIDs)
	if !reflect.DeepEqual(probe.journalIDs, expectedIDs) {
		t.Fatalf("closure-body read order/count differs from all 1,000 exact sorted identities: got=%d want=%d", len(probe.journalIDs), len(expectedIDs))
	}

	_, terminal = reverseLegacyReversalLinkedJournal(t, f, terminal)
	if postScalar(t, tx, `SELECT count(*) FROM journals`) != 1001 ||
		postScalar(t, tx, `SELECT count(*) FROM tax_facts`) != 0 ||
		postScalar(t, tx, `SELECT SUM(debit_minor-credit_minor) FROM journal_lines WHERE journal_id=? AND account_id=?`, terminal.Id, cashID) != -11000 {
		t.Fatal("real 1,001-node no-tax-fact chain did not end in the expected current cash credit")
	}
	overflowRequest := electNextLegacy(t, f, request)
	overflowRequest.Review.Reason = "Recheck retained unpaid evidence after a further journal reversal"
	counts := coverageStateCounts(t, tx)
	changes := postScalar(t, tx, "SELECT total_changes()")
	probe.journalIDs = nil
	overflow, err := legacyRepo(t, f, probe).ResolveLegacy(ctx, overflowRequest)
	if overflow != nil || !errors.Is(err, purchases.ErrSourceBound) || len(probe.journalIDs) != 0 {
		t.Fatalf("1,001-node closure did not fail before closure bodies: result=%v err=%v bodyReads=%d", overflow, err, len(probe.journalIDs))
	}
	if !tx.Authenticated() || postScalar(t, tx, "SELECT total_changes()") != changes || !reflect.DeepEqual(counts, coverageStateCounts(t, tx)) {
		t.Fatal("rejected closure changed retained state or ended the caller transaction")
	}
}
