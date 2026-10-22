//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"context"
	"database/sql"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
)

const purchaseMatureReplayLargeCellBytes = 9 * 1024 * 1024

type purchaseMatureReplayMutation string

const (
	purchaseMatureJournalForeignOwner purchaseMatureReplayMutation = "journal-owner"
	purchaseMatureJournalLargeMemo    purchaseMatureReplayMutation = "journal-description"
	purchaseMatureAccountForeignOwner purchaseMatureReplayMutation = "account-owner"
	purchaseMatureAccountLargeName    purchaseMatureReplayMutation = "account-name"
)

func purchaseMatureReplayIsJournalMutation(mutation purchaseMatureReplayMutation) bool {
	return mutation == purchaseMatureJournalForeignOwner || mutation == purchaseMatureJournalLargeMemo
}

func purchaseMatureReplayIsAccountMutation(mutation purchaseMatureReplayMutation) bool {
	return mutation == purchaseMatureAccountForeignOwner || mutation == purchaseMatureAccountLargeName
}

type purchaseMatureReplayProbe struct {
	app.CommandSQLExecutor
	t                *testing.T
	fixture          *postingFixture
	mutation         purchaseMatureReplayMutation
	targetDocumentID string
	targetJournalID  string
	targetAccountID  string

	documentReplayRead bool
	postingReplayRead  bool

	journalHeaderPreflightRead         bool
	journalLinesPreflightRead          bool
	journalHeaderRead                  bool
	journalLinesRead                   bool
	journalReportingBodyBeforeMutation bool
	ordinaryJournalBodyBeforeMutation  bool
	accountReportingReplaySeen         bool
	ordinaryAccountBodyBeforeMutation  bool
	mutated                            bool
	beforeRead                         runtime.MemStats

	journalHeaderReadsAfterMutation int
	journalLineReadsAfterMutation   int
	accountRowReadsAfterMutation    int
	otherBodyReadsAfterMutation     int
}

func (p *purchaseMatureReplayProbe) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	// Inject before an ordinary Get's body query so current code produces a real
	// late-owner/large-cell replay. On the bounded route, wait until the caller
	// has consumed both successful scalar preflight queries, then inject before
	// its single-row body query. QueryContext returns before Rows is scanned, so
	// mutating from the preflight branch itself would not be after preflight.
	if !p.mutated && purchaseMatureReplayIsJournalMutation(p.mutation) &&
		p.documentReplayRead && p.postingReplayRead {
		switch {
		case purchaseMatureReplayOrdinaryJournalHeaderBody(query, args, p.targetJournalID):
			p.ordinaryJournalBodyBeforeMutation = true
			p.mutateBeforeThisBodyQuery()
		case purchaseMatureReplayReportingJournalHeaderBody(query, args, p.targetJournalID, postingOrg):
			if !p.journalHeaderPreflightRead || !p.journalLinesPreflightRead {
				p.t.Fatalf("bounded journal replay reached its body before both own preflights: header/lines=%t/%t",
					p.journalHeaderPreflightRead, p.journalLinesPreflightRead)
			}
			p.journalReportingBodyBeforeMutation = true
			p.mutateBeforeThisBodyQuery()
		}
	}
	if !p.mutated && purchaseMatureReplayIsAccountMutation(p.mutation) &&
		p.documentReplayRead && p.postingReplayRead &&
		p.journalHeaderRead && p.journalLinesRead {
		switch {
		case purchaseMatureReplayAccountReportingQuery(query, args, p.targetAccountID, postingOrg):
			p.accountReportingReplaySeen = true
			p.mutateBeforeThisBodyQuery()
		case purchaseMatureReplayOrdinaryAccountBody(query, args, p.targetAccountID):
			p.ordinaryAccountBodyBeforeMutation = true
			p.mutateBeforeThisBodyQuery()
		}
	}

	rows, err := p.CommandSQLExecutor.QueryContext(ctx, query, args...)
	if err != nil || rows == nil {
		return rows, err
	}

	if purchaseReportingRootOwnerIsDocumentReplayQuery(query) &&
		purchaseMatureReplayHasArgs(args, p.targetDocumentID, postingOrg) {
		p.documentReplayRead = true
	}
	if purchaseModernOwnerAtomicIsPostingReplayQuery(query) &&
		purchaseModernOwnerAtomicTailArgsEqual(args, postingOrg, p.targetDocumentID) {
		p.postingReplayRead = true
	}
	if p.documentReplayRead && p.postingReplayRead &&
		purchaseMatureReplayJournalHeaderPreflight(query, args, p.targetJournalID, postingOrg) {
		p.journalHeaderPreflightRead = true
	}
	if p.documentReplayRead && p.postingReplayRead &&
		purchaseMatureReplayJournalLinesPreflight(query, args, p.targetJournalID, postingOrg) {
		p.journalLinesPreflightRead = true
	}
	if p.documentReplayRead && p.postingReplayRead &&
		purchaseMatureReplayJournalHeaderBody(query, args, p.targetJournalID) {
		p.journalHeaderRead = true
		if p.mutated {
			p.journalHeaderReadsAfterMutation++
		}
	}
	if p.documentReplayRead && p.postingReplayRead &&
		purchaseMatureReplayJournalLinesBody(query, args, p.targetJournalID) {
		p.journalLinesRead = true
		if p.mutated {
			p.journalLineReadsAfterMutation++
		}
	}
	if purchaseMatureReplayAccountBody(query, args, p.targetAccountID) && p.mutated {
		p.accountRowReadsAfterMutation++
	}
	if p.mutated && !purchaseMatureReplayJournalHeaderBody(query, args, p.targetJournalID) &&
		!purchaseMatureReplayJournalLinesBody(query, args, p.targetJournalID) &&
		!purchaseMatureReplayAccountBody(query, args, p.targetAccountID) &&
		purchaseCoverageColumnsContainBody(rows) {
		p.otherBodyReadsAfterMutation++
	}
	return rows, nil
}

func (p *purchaseMatureReplayProbe) mutateBeforeThisBodyQuery() {
	p.t.Helper()
	switch p.mutation {
	case purchaseMatureJournalForeignOwner:
		mutatePurchaseBASScalar(p.t, p.fixture, "journals",
			`UPDATE journals SET organisation_id=? WHERE id=?`, purchaseScalarRootOwnerForeignUUID, p.targetJournalID)
	case purchaseMatureJournalLargeMemo:
		// Measure hydration rather than the fixture mutation: create the 9 MiB
		// TEXT in SQLite and use CAST-to-BLOB length in production preflights.
		mutatePurchaseBASScalar(p.t, p.fixture, "journals",
			`UPDATE journals SET description=CAST(zeroblob(?) AS TEXT) WHERE id=?`,
			purchaseMatureReplayLargeCellBytes, p.targetJournalID)
	case purchaseMatureAccountForeignOwner:
		mutatePurchaseBASScalar(p.t, p.fixture, "accounts",
			`UPDATE accounts SET organisation_id=? WHERE id=?`, purchaseScalarRootOwnerForeignUUID, p.targetAccountID)
	case purchaseMatureAccountLargeName:
		mutatePurchaseBASScalar(p.t, p.fixture, "accounts",
			`UPDATE accounts SET name=CAST(zeroblob(?) AS TEXT) WHERE id=?`,
			purchaseMatureReplayLargeCellBytes, p.targetAccountID)
	default:
		p.t.Fatalf("unknown mature-replay mutation %q", p.mutation)
	}
	p.mutated = true
	runtime.GC()
	runtime.ReadMemStats(&p.beforeRead)
}

func TestPurchaseCoverageMatureJournalReplayUsesItsOwnedPreflightBeforeBody(t *testing.T) {
	for _, mutation := range []purchaseMatureReplayMutation{
		purchaseMatureJournalForeignOwner,
		purchaseMatureJournalLargeMemo,
	} {
		t.Run(string(mutation), func(t *testing.T) {
			ctx := context.Background()
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()

			f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
			posted := postReportingPurchase(t, f, "MATURE-JOURNAL-REPLAY", purchaseReportingDate(2026, 8, 10), 10000, 1000)
			if posted == nil || posted.Posting == nil || posted.Posting.JournalId == nil || posted.Journal == nil {
				t.Fatal("PostReviewed writer did not return the mature purchase journal")
			}
			journalID := *posted.Posting.JournalId

			baseline, _ := purchaseMatureReplayMeasuredCapture(t, f, ctx)
			if coverageJournal(t, baseline, journalID) == nil || len(baseline.Postings) != 1 {
				t.Fatal("writer-backed journal was absent from the complete baseline capture")
			}

			// Ordinary callers continue to use the complete, unscoped API. This
			// control intentionally does not exercise reporting mode.
			ordinaryProbe := &purchaseMatureOrdinaryReadProbe{CommandSQLExecutor: tx}
			ordinaryRepository, err := accounting.NewJournalRepository(ordinaryProbe)
			if err != nil {
				t.Fatal(err)
			}
			ordinary, err := ordinaryRepository.Get(ctx, journalID)
			if err != nil || ordinary == nil || ordinary.OrganisationId != postingOrg ||
				ordinary.Source != tammyv1.JournalSource_JOURNAL_SOURCE_PURCHASE || len(ordinary.Lines) != len(posted.Journal.Lines) {
				t.Fatalf("ordinary JournalRepository.Get() = %#v, %v; want its complete writer-backed record", ordinary, err)
			}
			if ordinaryProbe.reportingPreflights != 0 || ordinaryProbe.unscopedHeaderReads != 1 || ordinaryProbe.unscopedLineReads != 1 {
				t.Fatalf("ordinary Get crossed reporting boundary: preflights/header/lines=%d/%d/%d",
					ordinaryProbe.reportingPreflights, ordinaryProbe.unscopedHeaderReads, ordinaryProbe.unscopedLineReads)
			}

			probe := &purchaseMatureReplayProbe{
				CommandSQLExecutor: tx, t: t, fixture: f, mutation: mutation,
				targetDocumentID: posted.Posting.DocumentId, targetJournalID: journalID,
			}
			capture, err := f.repo(probe).CaptureCoverageForBASWithPayloadBudget(ctx, postingOrg, 8*1024*1024)
			var after runtime.MemStats
			runtime.ReadMemStats(&after)

			if !probe.documentReplayRead || !probe.postingReplayRead || !probe.mutated {
				t.Fatalf("late mutation missed mature capture replay: document/posting/mutated=%t/%t/%t",
					probe.documentReplayRead, probe.postingReplayRead, probe.mutated)
			}
			if probe.ordinaryJournalBodyBeforeMutation {
				// This is the expected native RED on the old replay path: Get has
				// no preflight, then hydrates its body/lines after injected drift.
				if mutation == purchaseMatureJournalForeignOwner && probe.journalLineReadsAfterMutation == 0 {
					t.Fatal("ordinary journal replay did not expose late owner drift to its later line read")
				}
				if mutation == purchaseMatureJournalLargeMemo {
					allocated := after.TotalAlloc - probe.beforeRead.TotalAlloc
					if allocated < 2*1024*1024 {
						t.Fatalf("ordinary journal replay did not hydrate the late 9 MiB description: allocated=%d", allocated)
					}
					t.Fatalf("ordinary journal replay hydrated the late 9 MiB description: allocated=%d lines=%d",
						allocated, probe.journalLineReadsAfterMutation)
				}
				t.Fatalf("mature capture still used ordinary JournalRepository.Get after mutation (header/lines/other bodies=%d/%d/%d)",
					probe.journalHeaderReadsAfterMutation, probe.journalLineReadsAfterMutation, probe.otherBodyReadsAfterMutation)
			}
			if capture != nil || err == nil ||
				(!errors.Is(err, purchases.ErrRepository) && !errors.Is(err, purchases.ErrSourceBound)) {
				t.Fatalf("mature journal replay returned capture=%t error=%v; want nil capture and a reporting failure", capture != nil, err)
			}
			if !probe.journalReportingBodyBeforeMutation || !probe.journalHeaderPreflightRead || !probe.journalLinesPreflightRead {
				t.Fatalf("bounded journal body did not follow both own preflights: body/header-preflight/line-preflight=%t/%t/%t",
					probe.journalReportingBodyBeforeMutation, probe.journalHeaderPreflightRead, probe.journalLinesPreflightRead)
			}
			if probe.journalHeaderReadsAfterMutation != 1 || probe.journalLineReadsAfterMutation != 0 ||
				probe.accountRowReadsAfterMutation != 0 || probe.otherBodyReadsAfterMutation != 0 {
				t.Fatalf("later body reads escaped early journal rejection: lines/accounts/other=%d/%d/%d",
					probe.journalLineReadsAfterMutation, probe.accountRowReadsAfterMutation, probe.otherBodyReadsAfterMutation)
			}
			if mutation == purchaseMatureJournalLargeMemo {
				allocated := after.TotalAlloc - probe.beforeRead.TotalAlloc
				if allocated >= 2*1024*1024 {
					t.Fatalf("oversized journal replay allocated %d bytes after its late mutation",
						allocated)
				}
			}
		})
	}
}

func TestPurchaseCoverageMatureControlAccountReplayUsesOwnedMaskedQuery(t *testing.T) {
	for _, mutation := range []purchaseMatureReplayMutation{
		purchaseMatureAccountForeignOwner,
		purchaseMatureAccountLargeName,
	} {
		t.Run(string(mutation), func(t *testing.T) {
			ctx := context.Background()
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()

			f, _, state, portion := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
			paymentID := portion.GetPayment().GetPaymentId()
			journalID := purchaseBASScalarText(t, tx, `SELECT journal_id FROM purchase_payments_v1 WHERE id=?`, paymentID)
			cashAccountID := purchaseBASScalarText(t, tx, `SELECT id FROM accounts WHERE organisation_id=? AND code='2000'`, postingOrg)
			baseline, _ := purchaseMatureReplayMeasuredCapture(t, f, ctx)
			if len(baseline.Postings) != 1 || len(baseline.Postings[0].Payments) != 1 || coverageJournal(t, baseline, journalID) == nil {
				t.Fatal("writer-backed payment/control-account baseline is incomplete")
			}

			// Direct ordinary account lookup stays fully hydrated and unscoped.
			ordinaryProbe := &purchaseMatureOrdinaryReadProbe{CommandSQLExecutor: tx}
			ordinaryAccounts, err := accounting.NewAccountRepository(ordinaryProbe)
			if err != nil {
				t.Fatal(err)
			}
			ordinaryAccount, err := ordinaryAccounts.Get(ctx, cashAccountID)
			if err != nil || ordinaryAccount == nil || ordinaryAccount.OrganisationId != postingOrg || ordinaryAccount.Name == "" {
				t.Fatalf("ordinary AccountRepository.Get() = %#v, %v; want its complete ordinary account", ordinaryAccount, err)
			}
			if ordinaryProbe.accountReportingPreflights != 0 || ordinaryProbe.unscopedAccountReads != 1 {
				t.Fatalf("ordinary account Get crossed reporting boundary: preflights/unscoped-reads=%d/%d",
					ordinaryProbe.accountReportingPreflights, ordinaryProbe.unscopedAccountReads)
			}

			probe := &purchaseMatureReplayProbe{
				CommandSQLExecutor: tx, t: t, fixture: f, mutation: mutation,
				targetDocumentID: state.Posting.DocumentId,
				targetJournalID:  journalID, targetAccountID: cashAccountID,
			}
			capture, err := f.repo(probe).CaptureCoverageForBASWithPayloadBudget(ctx, postingOrg, 8*1024*1024)
			var after runtime.MemStats
			runtime.ReadMemStats(&after)

			if !probe.documentReplayRead || !probe.postingReplayRead || !probe.journalHeaderRead ||
				!probe.journalLinesRead || !probe.mutated {
				t.Fatalf("late account mutation missed mature journal replay: document/posting/header/lines/mutated=%t/%t/%t/%t/%t",
					probe.documentReplayRead, probe.postingReplayRead, probe.journalHeaderRead,
					probe.journalLinesRead, probe.mutated)
			}
			if probe.ordinaryAccountBodyBeforeMutation {
				if probe.accountRowReadsAfterMutation != 1 {
					t.Fatalf("ordinary account replay did not hydrate the mutated body exactly once: reads=%d", probe.accountRowReadsAfterMutation)
				}
				if mutation == purchaseMatureAccountLargeName {
					allocated := after.TotalAlloc - probe.beforeRead.TotalAlloc
					if allocated < 2*1024*1024 {
						t.Fatalf("ordinary account replay did not hydrate the late 9 MiB name: allocated=%d", allocated)
					}
					t.Fatalf("ordinary account replay hydrated the late 9 MiB name: allocated=%d",
						allocated)
				}
				t.Fatalf("mature payment capture still used ordinary AccountRepository.Get after mutation; body reads=%d",
					probe.accountRowReadsAfterMutation)
			}
			if capture != nil || err == nil ||
				(!errors.Is(err, purchases.ErrRepository) && !errors.Is(err, purchases.ErrSourceBound)) {
				t.Fatalf("mature account replay returned capture=%t error=%v; want nil capture and a reporting failure", capture != nil, err)
			}
			if !probe.accountReportingReplaySeen {
				t.Fatal("mature payment control-account replay did not use the same-statement BAS-owned masked query")
			}
			if probe.accountRowReadsAfterMutation != 1 || probe.otherBodyReadsAfterMutation != 0 {
				t.Fatalf("account rejection allowed later replay body reads: target-account/other=%d/%d",
					probe.accountRowReadsAfterMutation, probe.otherBodyReadsAfterMutation)
			}
			if mutation == purchaseMatureAccountLargeName {
				allocated := after.TotalAlloc - probe.beforeRead.TotalAlloc
				if allocated >= 2*1024*1024 {
					t.Fatalf("oversized account replay allocated %d bytes after its late mutation",
						allocated)
				}
			}
		})
	}
}

// Ordinary repository controls intentionally assert the legacy APIs remain
// complete and unscoped; reporting-only calls are exercised by CaptureCoverage.
type purchaseMatureOrdinaryReadProbe struct {
	app.CommandSQLExecutor
	reportingPreflights        int
	accountReportingPreflights int
	unscopedHeaderReads        int
	unscopedLineReads          int
	unscopedAccountReads       int
}

func (p *purchaseMatureOrdinaryReadProbe) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	compact := purchaseMatureReplayCompact(query)
	journalHeaderPreflight := strings.Contains(compact, "fromjournalswhereid=?andorganisation_id=?") &&
		strings.Contains(compact, "count(*)") && strings.Contains(compact, "total(")
	journalLinesPreflight := strings.Contains(compact, "fromjournal_linesasline") &&
		strings.Contains(compact, "count(*)") && strings.Contains(compact, "total(payload_bytes)") &&
		strings.Contains(compact, "exists(select1fromjournalswhereid=line.journal_idandorganisation_id=?)")
	if journalHeaderPreflight || journalLinesPreflight {
		p.reportingPreflights++
	}
	if strings.Contains(query, "/* BAS account reporting replay */") ||
		strings.Contains(compact, "fromaccounts") && strings.Contains(compact, "length(cast(") {
		p.accountReportingPreflights++
	}
	if purchaseMatureReplayOrdinaryJournalHeaderBody(query, args, "") {
		p.unscopedHeaderReads++
	}
	if purchaseMatureReplayOrdinaryJournalLinesBody(query, args, "") {
		p.unscopedLineReads++
	}
	if purchaseMatureReplayOrdinaryAccountBody(query, args, "") {
		p.unscopedAccountReads++
	}
	return p.CommandSQLExecutor.QueryContext(ctx, query, args...)
}

func purchaseMatureReplayCompact(query string) string {
	return strings.ToLower(strings.Join(strings.Fields(query), ""))
}

func purchaseMatureReplayHasArgs(args []any, expected ...string) bool {
	for _, want := range expected {
		found := false
		for _, arg := range args {
			value, ok := arg.(string)
			if ok && value == want {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func purchaseMatureReplayJournalHeaderPreflight(query string, args []any, journalID, org string) bool {
	compact := purchaseMatureReplayCompact(query)
	return strings.Contains(compact, "fromjournalswhereid=?andorganisation_id=?") &&
		strings.Contains(compact, "count(*)") && strings.Contains(compact, "total(") &&
		purchaseMatureReplayHasArgs(args, journalID, org)
}

func purchaseMatureReplayJournalLinesPreflight(query string, args []any, journalID, org string) bool {
	compact := purchaseMatureReplayCompact(query)
	return strings.Contains(compact, "fromjournal_linesasline") &&
		strings.Contains(compact, "count(*)") && strings.Contains(compact, "total(payload_bytes)") &&
		strings.Contains(compact, "exists(select1fromjournalswhereid=line.journal_idandorganisation_id=?)") &&
		purchaseMatureReplayHasArgs(args, journalID, org)
}

func purchaseMatureReplayJournalHeaderBody(query string, args []any, journalID string) bool {
	compact := purchaseMatureReplayCompact(query)
	return (strings.Contains(query, "/* tax fact reporting journal header body */") ||
		strings.HasPrefix(compact, "selectid,organisation_id,version,source_type,source_kind") &&
			strings.Contains(compact, "fromjournalswhereid=?")) && purchaseMatureReplayHasArgs(args, journalID)
}

func purchaseMatureReplayOrdinaryJournalHeaderBody(query string, args []any, journalID string) bool {
	compact := purchaseMatureReplayCompact(query)
	if !strings.HasPrefix(compact, "selectid,organisation_id,version,source_type,source_kind,state,journal_date,description") ||
		!strings.Contains(compact, "fromjournalswhereid=?") || strings.Contains(compact, "organisation_id=?") {
		return false
	}
	return journalID == "" || purchaseMatureReplayHasArgs(args, journalID)
}

func purchaseMatureReplayReportingJournalHeaderBody(query string, args []any, journalID, org string) bool {
	compact := purchaseMatureReplayCompact(query)
	return (strings.Contains(query, "/* tax fact reporting journal header body */") ||
		strings.HasPrefix(compact, "selectid,organisation_id,version,source_type,source_kind") &&
			strings.Contains(compact, "fromjournalswhereid=?andorganisation_id=?")) &&
		purchaseMatureReplayHasArgs(args, journalID, org)
}

func purchaseMatureReplayJournalLinesBody(query string, args []any, journalID string) bool {
	compact := purchaseMatureReplayCompact(query)
	return (strings.Contains(query, "/* tax fact reporting journal lines body */") ||
		strings.HasPrefix(compact, "selectid,journal_id,line_number,account_id,debit_minor,credit_minor") &&
			strings.Contains(compact, "fromjournal_lineswherejournal_id=?")) && purchaseMatureReplayHasArgs(args, journalID)
}

func purchaseMatureReplayOrdinaryJournalLinesBody(query string, args []any, journalID string) bool {
	compact := purchaseMatureReplayCompact(query)
	if !strings.HasPrefix(compact, "selectid,journal_id,line_number,account_id,debit_minor,credit_minor") ||
		!strings.Contains(compact, "fromjournal_lineswherejournal_id=?") || strings.Contains(compact, "limit?") {
		return false
	}
	return journalID == "" || purchaseMatureReplayHasArgs(args, journalID)
}

func purchaseMatureReplayAccountReportingQuery(query string, args []any, accountID, org string) bool {
	return strings.Contains(query, "/* BAS account reporting replay */") &&
		strings.Contains(purchaseMatureReplayCompact(query), "fromaccounts") &&
		purchaseMatureReplayHasArgs(args, accountID, org)
}

func purchaseMatureReplayAccountBody(query string, args []any, accountID string) bool {
	compact := purchaseMatureReplayCompact(query)
	if strings.Contains(query, "/* BAS account reporting replay */") {
		return purchaseMatureReplayHasArgs(args, accountID)
	}
	return strings.HasPrefix(compact, "selectid,organisation_id,version,code,name,account_type") &&
		strings.Contains(compact, "fromaccountswhereid=?") && purchaseMatureReplayHasArgs(args, accountID)
}

func purchaseMatureReplayOrdinaryAccountBody(query string, args []any, accountID string) bool {
	compact := purchaseMatureReplayCompact(query)
	if strings.Contains(query, "/* BAS account reporting replay */") ||
		!strings.HasPrefix(compact, "selectid,organisation_id,version,code,name,account_type") ||
		!strings.Contains(compact, "fromaccountswhereid=?") || strings.Contains(compact, "organisation_id=?") {
		return false
	}
	return accountID == "" || purchaseMatureReplayHasArgs(args, accountID)
}

func purchaseCoverageColumnsContainBody(rows *sql.Rows) bool {
	columns, err := rows.Columns()
	if err != nil {
		return false
	}
	for _, column := range columns {
		if purchaseCoverageBodyResultColumn(column) {
			return true
		}
	}
	return false
}

func purchaseMatureReplayMeasuredCapture(t *testing.T, f *postingFixture, ctx context.Context) (*purchases.PurchaseCoverageReportingCapture, uint64) {
	t.Helper()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	capture, err := f.repo(f.executor).CaptureCoverageForBAS(ctx, postingOrg)
	runtime.ReadMemStats(&after)
	if err != nil || capture == nil {
		t.Fatalf("baseline writer-backed coverage capture=%v err=%v", capture != nil, err)
	}
	return capture, after.TotalAlloc - before.TotalAlloc
}

func TestPurchaseCoverageMatureControlIDReplayMasksLateOversizedID(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f, _, state, _ := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	purchaseMatureReplayMeasuredCapture(t, f, ctx)
	accountID := purchaseBASScalarText(t, tx, `SELECT id FROM accounts WHERE organisation_id=? AND code='2000'`, postingOrg)
	probe := &purchaseReferenceQueryProbe{CommandSQLExecutor: tx, t: t, requireDocumentAndPosting: true,
		replayDocumentID: state.Posting.DocumentId, measureAllocation: true,
		matchTarget: func(query string, _ []any) bool {
			return strings.Contains(query, "/* BAS purchase owner body control account IDs replay */")
		}}
	probe.mutate = func() {
		mutatePurchaseBASScalar(t, f, "accounts", `UPDATE accounts SET id=CAST(zeroblob(?) AS TEXT) WHERE id=?`, purchaseMatureReplayLargeCellBytes, accountID)
	}
	capture, err := f.repo(probe).CaptureCoverageForBAS(ctx, postingOrg)
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	if !probe.mutated || capture != nil || !errors.Is(err, purchases.ErrRepository) || probe.laterBodyQueries != 0 || !tx.Authenticated() {
		t.Fatalf("late control ID mutation: mutated=%t nil=%t err=%v later=%d", probe.mutated, capture == nil, err, probe.laterBodyQueries)
	}
	if allocated := after.TotalAlloc - probe.beforeRead.TotalAlloc; allocated >= 2*1024*1024 {
		t.Fatalf("control-ID replay copied the late 9 MiB identity: %d Go bytes", allocated)
	}
}

const purchaseGetJournalProofLateCellBytes = 9 * 1024 * 1024

type purchaseGetJournalProofReuseProbe struct {
	app.CommandSQLExecutor
	t             *testing.T
	fixture       *postingFixture
	journalID     string
	documentRead  bool
	postingRead   bool
	headerProofs  int
	lineProofs    int
	metadataReads int
	mutated       bool
	beforeRead    runtime.MemStats
}

func (probe *purchaseGetJournalProofReuseProbe) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	compact := purchaseReportingRootOwnerCompactSQL(query)
	metadataRead := strings.HasPrefix(compact,
		"selectsource_id,source_revisionfromjournalswhereid=?andorganisation_id=?")
	if metadataRead {
		probe.metadataReads++
		if !probe.documentRead || !probe.postingRead || probe.headerProofs == 0 || probe.lineProofs == 0 {
			probe.t.Fatalf("redundant source metadata read preceded the complete bounded journal proof: document/posting/header/lines=%t/%t/%d/%d",
				probe.documentRead, probe.postingRead, probe.headerProofs, probe.lineProofs)
		}
		probe.mutated = true
		mutatePurchaseBASScalar(probe.t, probe.fixture, "journals",
			`UPDATE journals SET source_id=CAST(zeroblob(?) AS TEXT),source_revision=zeroblob(?) WHERE id=?`,
			purchaseGetJournalProofLateCellBytes, purchaseGetJournalProofLateCellBytes, probe.journalID)
		runtime.GC()
		runtime.ReadMemStats(&probe.beforeRead)
	}

	rows, err := probe.CommandSQLExecutor.QueryContext(ctx, query, args...)
	if err != nil || rows == nil {
		return rows, err
	}
	if purchaseReportingRootOwnerIsDocumentReplayQuery(query) {
		probe.documentRead = true
	}
	if purchaseModernOwnerAtomicIsPostingReplayQuery(query) &&
		purchaseModernOwnerAtomicTailArgsEqual(args, postingOrg, probe.fixture.document.Id) {
		probe.postingRead = true
	}
	if probe.documentRead && probe.postingRead {
		if purchaseMatureReplayJournalHeaderBody(query, args, probe.journalID) {
			probe.headerProofs++
		}
		if purchaseMatureReplayJournalLinesBody(query, args, probe.journalID) {
			probe.lineProofs++
		}
	}
	return rows, nil
}

func TestPurchaseReportingGetReusesBoundedJournalSourceProof(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	posted := postReportingPurchase(t, f, "GET-JOURNAL-PROOF-REUSE",
		purchaseReportingDate(2026, 8, 10), 10000, 1000)
	if posted == nil || posted.Posting == nil || posted.Posting.JournalId == nil {
		t.Fatal("PostReviewed writer did not return a purchase journal")
	}
	probe := &purchaseGetJournalProofReuseProbe{
		CommandSQLExecutor: tx, t: t, fixture: f,
		journalID: *posted.Posting.JournalId,
	}
	capture, err := f.repo(probe).CapturePostingsForReporting(ctx, postingOrg)
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	if probe.metadataReads != 0 || probe.mutated {
		allocated := uint64(0)
		if probe.mutated {
			allocated = after.TotalAlloc - probe.beforeRead.TotalAlloc
			if allocated < 2*1024*1024 {
				t.Fatalf("late metadata mutation did not demonstrate the oversized scan: allocated=%d capture=%v err=%v", allocated, capture, err)
			}
		}
		t.Fatalf("reporting Get re-read source metadata after its full journal proof: reads=%d mutated=%t allocation=%d capture=%v err=%v",
			probe.metadataReads, probe.mutated, allocated, capture, err)
	}
	if err != nil || capture == nil || len(capture.Postings) != 1 || len(capture.Journals) != 1 {
		t.Fatalf("writer-backed reporting capture=%#v err=%v", capture, err)
	}
	if !probe.documentRead || !probe.postingRead || probe.headerProofs == 0 || probe.lineProofs == 0 {
		t.Fatalf("test missed the reported posting's complete journal proof: document/posting/header/lines=%t/%t/%d/%d",
			probe.documentRead, probe.postingRead, probe.headerProofs, probe.lineProofs)
	}
	if proof := capture.Journals[0]; proof == nil || proof.Journal == nil || proof.Journal.Id != probe.journalID ||
		proof.SourceID != posted.Posting.Id || proof.SourceRevision != 1 {
		t.Fatalf("capture lost the exact writer journal provenance: %#v", proof)
	}
	if capture.Postings[0].Posting == nil || capture.Postings[0].Posting.Id != posted.Posting.Id || !tx.Authenticated() {
		t.Fatal("successful proof reuse lost the selected posting or caller transaction")
	}
}
