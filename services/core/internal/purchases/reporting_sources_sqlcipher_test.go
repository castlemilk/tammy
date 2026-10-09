//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/reporting"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestPurchaseBASMixedAllocationTraceAndLegacyReplacement(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	accounts, _ := accounting.NewAccountRepository(tx)
	assetID, _ := f.id()
	privateID, _ := f.id()
	for _, account := range []*tammyv1.Account{
		{Id: assetID, OrganisationId: postingOrg, Version: 1, Code: "1501", Name: "Equipment", Type: tammyv1.AccountType_ACCOUNT_TYPE_ASSET, NormalBalance: tammyv1.NormalBalance_NORMAL_BALANCE_DEBIT, Status: tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE, Designation: tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY, ReportClassification: "custom.account", CashFlowClassification: "investing"},
		{Id: privateID, OrganisationId: postingOrg, Version: 1, Code: "3101", Name: "Drawings", Type: tammyv1.AccountType_ACCOUNT_TYPE_EQUITY, NormalBalance: tammyv1.NormalBalance_NORMAL_BALANCE_CREDIT, Status: tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE, Designation: tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY, ReportClassification: "custom.account", CashFlowClassification: "financing"},
	} {
		if err := accounts.Create(ctx, account, f.now); err != nil {
			t.Fatal(err)
		}
	}
	business := proto.Clone(f.request.Allocations[0]).(*tammyv1.PurchaseAllocationInput)
	business.Gross, business.SupplierGst, business.EligibleGst = postMoney(5500), postMoney(500), postMoney(400)
	business.NoncreditableReason = "Partly noncreditable cost"
	capital := proto.Clone(business).(*tammyv1.PurchaseAllocationInput)
	capital.AccountId, capital.Sequence = assetID, 2
	capital.Gross, capital.SupplierGst, capital.EligibleGst = postMoney(3300), postMoney(300), postMoney(300)
	capital.NoncreditableReason = ""
	capital.BasClassification = tammyv1.PurchaseBasClassification_PURCHASE_BAS_CLASSIFICATION_CAPITAL
	capital.CashFlowClassification = tammyv1.PurchaseCashFlowClassification_PURCHASE_CASH_FLOW_CLASSIFICATION_INVESTING
	private := proto.Clone(business).(*tammyv1.PurchaseAllocationInput)
	private.AccountId, private.Sequence = privateID, 3
	private.Gross, private.SupplierGst, private.EligibleGst = postMoney(2200), postMoney(200), postMoney(0)
	private.Use = tammyv1.PurchaseUse_PURCHASE_USE_PRIVATE
	private.BasClassification = tammyv1.PurchaseBasClassification_PURCHASE_BAS_CLASSIFICATION_EXCLUDED
	private.CashFlowClassification = tammyv1.PurchaseCashFlowClassification_PURCHASE_CASH_FLOW_CLASSIFICATION_FINANCING
	private.NoncreditableReason = "Private use"
	f.request.Allocations = []*tammyv1.PurchaseAllocationInput{business, capital, private}
	repo := f.repo(tx)
	posted, err := repo.PostReviewed(ctx, f.request)
	if err != nil {
		t.Fatal(err)
	}
	period := &tammyv1.PurchaseClaimPeriod{PeriodStart: postDate(1), PeriodEnd: postDate(31)}
	snapshot, err := repo.CaptureBAS(ctx, postingOrg, period, period.PeriodEnd, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Sources) != 3 || snapshot.LegacyCandidateGST.MinorUnits != 0 || len(snapshot.Reviews) != 0 {
		t.Fatalf("posted source was lost or double-counted as legacy: %+v", snapshot)
	}
	var claimed int64
	for i, input := range f.request.Allocations {
		line := snapshot.Sources[i]
		if line.PostingId != posted.Posting.Id || line.AllocationSequence != input.Sequence || line.Use != input.Use || line.BasClassification != input.BasClassification || line.CashFlowClassification != input.CashFlowClassification || !proto.Equal(line.AllocatedGross, input.Gross) || !proto.Equal(line.SupplierGst, input.SupplierGst) || !proto.Equal(line.EligibleGst, input.EligibleGst) || !proto.Equal(line.ClaimedGst, input.EligibleGst) {
			t.Fatalf("allocation %d trace changed: %v", i, line)
		}
		claimed += line.ClaimedGst.MinorUnits
	}
	if claimed != 700 || paymentBalance(t, tx, "1150") != claimed {
		t.Fatal("mixed allocation BAS does not reconcile GST control")
	}
}

func TestPurchaseBASDelayedAttributionUsesSelectedClaimPeriod(t *testing.T) {
	for _, basis := range []tammyv1.GstBasis{tammyv1.GstBasis_GST_BASIS_CASH, tammyv1.GstBasis_GST_BASIS_NON_CASH} {
		t.Run(basis.String(), func(t *testing.T) {
			ctx := context.Background()
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f, repo, state, portion := claimSetup(t, tx, basis)
			previous := &tammyv1.PurchaseClaimPeriod{PeriodStart: paymentDate(7, 1), PeriodEnd: paymentDate(9, 30)}
			before, err := repo.CaptureBAS(ctx, postingOrg, previous, previous.PeriodEnd, f.now)
			if err != nil {
				t.Fatal(err)
			}
			if len(before.Sources) != 0 || len(before.Reviews) == 0 || paymentBalance(t, tx, "1150") != 0 {
				t.Fatal("suspense entered prior-period reconciled credit")
			}
			claimed, err := attributeCommand(t, repo, claimRequest(t, f, state, portion))
			if err != nil {
				t.Fatal(err)
			}
			old, err := repo.CaptureBAS(ctx, postingOrg, previous, previous.PeriodEnd, f.now)
			if err != nil {
				t.Fatal(err)
			}
			selected, err := repo.CaptureBAS(ctx, postingOrg, claimPeriod(), claimPeriod().PeriodEnd, f.now)
			if err != nil {
				t.Fatal(err)
			}
			if len(old.Sources) != 0 || len(selected.Sources) != 1 {
				t.Fatal("delayed credit entered normal event period or duplicated")
			}
			line := selected.Sources[0]
			if !proto.Equal(line.ReleasePostingDate, claimed.Attributions[0].ReleasePostingDate) {
				t.Fatal("saved attribution lost its separate release posting date")
			}
			if line.AttributionId == nil || *line.AttributionId != claimed.Attributions[0].Id || !bytes.Equal(line.AttributionHash, claimed.Attributions[0].SourceHash) || !proto.Equal(line.ClaimPeriod, claimPeriod()) || !proto.Equal(line.NormalTaxEventDate, portion.NormalEventDate) || line.Evidence.SourceId != portion.EvidenceId || line.ClaimedGst.MinorUnits != paymentBalance(t, tx, "1150") {
				t.Fatalf("delayed provenance incomplete: %v", line)
			}
		})
	}
}

func TestPurchaseBASAll201PaymentsRetainEveryOwnedSource(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	f.addDocument("Many payment supplier", "PAY-201", 2010, 201)
	repo := f.repo(tx)
	if _, err := repo.PostReviewed(ctx, f.request); err != nil {
		t.Fatal(err)
	}
	f.now = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	retained := map[string]*tammyv1.PurchasePayment{}
	for i := 0; i < 201; i++ {
		paid, err := repo.RecordPayment(ctx, paymentRequest(t, f, uint64(i+1), 11, paymentDate(10, 2)))
		if err != nil {
			t.Fatalf("payment %d: %v", i, err)
		}
		retained[paid.Payment.Id] = paid.Payment
	}
	snapshot, err := repo.CaptureBAS(ctx, postingOrg, claimPeriod(), claimPeriod().PeriodEnd, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Sources) != 201 {
		t.Fatalf("BAS used payment page cap: %d", len(snapshot.Sources))
	}
	var claimed int64
	for _, line := range snapshot.Sources {
		if line.PaymentId == nil {
			t.Fatal("payment source absent")
		}
		payment := retained[*line.PaymentId]
		if payment == nil || !bytes.Equal(line.PaymentHash, payment.SourceHash) || !proto.Equal(line.ActualPaymentDate, payment.ActualPaymentDate) || !proto.Equal(line.AllocatedGross, payment.Allocations[0].Gross) || line.ClaimedGst.MinorUnits != 1 {
			t.Fatalf("owned payment snapshot changed: %v", line)
		}
		delete(retained, *line.PaymentId)
		claimed += line.ClaimedGst.MinorUnits
	}
	if len(retained) != 0 || claimed != 201 || paymentBalance(t, tx, "1150") != claimed {
		t.Fatal("all-row payment claim conservation failed")
	}
}

func TestPurchaseFingerprintMutationsWithUnchangedEligibleTotals(t *testing.T) {
	for _, action := range []string{"payment", "evidence", "attribution", "deferral", "legacy decision", "new unresolved zero candidate", "new excluded zero candidate"} {
		t.Run(action, func(t *testing.T) {
			ctx := context.Background()
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f, purchaseRepo, state, portion := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
			var legacy *tammyv1.ResolveLegacyPurchaseReviewRequest
			if action == "legacy decision" {
				f.addDocument("Zero legacy", "LEGACY-ZERO", 0, 0)
				legacy = legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_NONCREDITABLE)
				legacy.Review.AffectedPeriod, legacy.Review.ReportingBalanceDate = claimPeriod(), claimPeriod().PeriodEnd
				f.now = time.Date(2026, 12, 31, 12, 0, 0, 0, time.UTC)
			}
			bas, err := reporting.NewRepositoryWithClock(tx, func() time.Time { return f.now }, purchaseRepo)
			if err != nil {
				t.Fatal(err)
			}
			key, _ := f.id()
			id, _ := f.id()
			saved, err := bas.CreateBASDraft(ctx, key, id, postingOrg, claimPeriod().PeriodStart, claimPeriod().PeriodEnd, f.now)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := bas.GetCurrentBASDraft(ctx, postingOrg)
			if err != nil || loaded.SourcesChanged || !proto.Equal(saved, loaded) {
				t.Fatalf("saving alone staled source fingerprint: %v", err)
			}
			original := proto.Clone(saved).(*tammyv1.BasWorkpaper)
			switch action {
			case "payment":
				_, err = purchaseRepo.RecordPayment(ctx, paymentRequest(t, f, state.AccountingVersion, 5500, paymentDate(10, 2)))
			case "evidence":
				review := proto.Clone(f.request.Evidence).(*tammyv1.PurchaseEvidenceInput)
				review.HeldDate = paymentDate(10, 1)
				review.Reason = "New evidence-only retained review"
				_, err = evidenceCommand(t, purchaseRepo, ctx, evidenceRequest(t, f, state.AccountingVersion, review))
			case "attribution":
				_, err = attributeCommand(t, purchaseRepo, claimRequest(t, f, state, portion))
			case "deferral":
				_, err = deferCommand(t, purchaseRepo, deferRequest(t, f, state, portion))
			case "legacy decision":
				_, err = legacyRepo(t, f, tx).ResolveLegacy(ctx, legacy)
			case "new unresolved zero candidate":
				f.addDocument("Zero new candidate", "NEW-ZERO", 0, 0)
			case "new excluded zero candidate":
				f.addDocument("Zero new excluded candidate", "NEW-EXCLUDED-ZERO", 0, 0)
				decision := legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_NONCREDITABLE)
				decision.Review.AffectedPeriod, decision.Review.ReportingBalanceDate = claimPeriod(), claimPeriod().PeriodEnd
				f.now = time.Date(2026, 12, 31, 12, 0, 0, 0, time.UTC)
				_, err = legacyRepo(t, f, tx).ResolveLegacy(ctx, decision)
			}
			if err != nil {
				t.Fatal(err)
			}
			current, err := bas.GetCurrentBASDraft(ctx, postingOrg)
			if err != nil || !current.SourcesChanged {
				t.Fatalf("%s did not stale saved draft without eligible-total changes: %v", action, err)
			}
			current.SourcesChanged = false
			if !proto.Equal(original, current) {
				t.Fatalf("%s rewrote retained saved figures or complete source bytes", action)
			}
			if action != "legacy decision" && action != "new unresolved zero candidate" && action != "new excluded zero candidate" {
				latest, err := purchaseRepo.Get(ctx, postingOrg, state.Posting.DocumentId)
				if err != nil || latest.EligibleGst.MinorUnits != state.EligibleGst.MinorUnits {
					t.Fatalf("test mutation changed eligible total: %v", err)
				}
			}
		})
	}
}

func TestPurchaseBASOpeningWithoutDocumentRemainsVisibleOutside1B(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	id := legacyOpeningFixture(t, f)
	repo := f.repo(tx)
	period := &tammyv1.PurchaseClaimPeriod{PeriodStart: postDate(1), PeriodEnd: postDate(31)}
	snapshot, err := repo.CaptureBAS(ctx, postingOrg, period, period.PeriodEnd, f.now)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, row := range snapshot.Reviews {
		ref := row.LegacySource.GetOpeningPayable()
		if ref == nil || ref.OpeningPayableId != id {
			continue
		}
		found = true
		if row.Kind != tammyv1.BasPurchaseReviewKind_BAS_PURCHASE_REVIEW_KIND_OPENING_PAYABLE || row.Status != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNRESOLVED_LEGACY || row.UnclaimedGst.MinorUnits != 1000 || len(ref.SourceHash) != 32 || ref.SourceVersion == 0 || row.PostingId != nil {
			t.Fatalf("opening provenance incomplete: %v", row)
		}
	}
	if !found || len(snapshot.Sources) != 0 || snapshot.LegacyCandidateGST.MinorUnits != 2000 {
		t.Fatalf("opening disappeared or contributed credit: %s", fmt.Sprint(snapshot))
	}
}

func TestPurchaseFingerprintOpeningConversionMetadataChangesWithoutGSTChange(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	legacyOpeningFixture(t, f)
	repo := f.repo(tx)
	period := &tammyv1.PurchaseClaimPeriod{PeriodStart: postDate(1), PeriodEnd: postDate(31)}
	before, err := repo.CaptureBAS(ctx, postingOrg, period, period.PeriodEnd, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `DROP TRIGGER posted_opening_conversion_update_guard`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `DROP TRIGGER opening_conversion_transition_guard`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE opening_conversions SET source_sha256=?`, strings.Repeat("b", 64)); err != nil {
		t.Fatal(err)
	}
	after, err := repo.CaptureBAS(ctx, postingOrg, period, period.PeriodEnd, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(before.LegacyCandidateGST, after.LegacyCandidateGST) {
		t.Fatal("metadata mutation changed provisional GST")
	}
	if bytes.Equal(before.Fingerprint, after.Fingerprint) {
		t.Fatal("opening conversion provenance changed without staling fingerprint")
	}
}

func TestPurchaseFingerprintRetainedEvidenceRuleMetadata(t *testing.T) {
	for _, mutation := range []string{"install", "retained stamp", "corrupt rule body"} {
		t.Run(mutation, func(t *testing.T) {
			ctx := context.Background()
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
			repo := f.repo(tx)
			if mutation != "install" {
				if err := repo.RetainEvidenceRules(ctx); err != nil {
					t.Fatal(err)
				}
			}
			period := &tammyv1.PurchaseClaimPeriod{PeriodStart: postDate(1), PeriodEnd: postDate(31)}
			before, err := repo.CaptureBAS(ctx, postingOrg, period, period.PeriodEnd, f.now)
			if err != nil {
				t.Fatal(err)
			}
			if mutation == "install" {
				err = repo.RetainEvidenceRules(ctx)
			} else {
				if _, err = tx.ExecContext(ctx, `DROP TRIGGER rule_bundles_immutable_update`); err != nil {
					t.Fatal(err)
				}
				if mutation == "retained stamp" {
					_, err = tx.ExecContext(ctx, `UPDATE rule_bundles SET retained_at='2026-08-10T12:00:00.000000001Z'`)
				} else {
					_, err = tx.ExecContext(ctx, `UPDATE rule_bundles SET rules_proto=?`, []byte("damaged retained rule"))
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			after, err := repo.CaptureBAS(ctx, postingOrg, period, period.PeriodEnd, f.now)
			if mutation == "corrupt rule body" {
				if err == nil {
					t.Fatal("damaged retained GST rule yielded a complete BAS snapshot")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !proto.Equal(before.LegacyCandidateGST, after.LegacyCandidateGST) {
				t.Fatal("rule metadata changed GST total")
			}
			if bytes.Equal(before.Fingerprint, after.Fingerprint) {
				t.Fatalf("%s did not stale retained rule provenance", mutation)
			}
		})
	}
}

func TestPurchaseBASCorruptOwnedSourceHasNoPartialSave(t *testing.T) {
	for _, source := range []string{"posted", "legacy original bytes"} {
		t.Run(source, func(t *testing.T) {
			ctx := context.Background()
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
			purchaseRepo := f.repo(tx)
			if source == "posted" {
				if _, err := purchaseRepo.PostReviewed(ctx, f.request); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, `DROP TRIGGER purchase_postings_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				_, err = tx.ExecContext(ctx, `UPDATE purchase_postings_v1 SET source_hash=?`, make([]byte, 32))
			} else {
				if _, err := tx.ExecContext(ctx, `DROP TRIGGER documents_immutable_source`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, `DROP TRIGGER documents_review_transition`); err != nil {
					t.Fatal(err)
				}
				_, err = tx.ExecContext(ctx, `UPDATE documents SET original_bytes=? WHERE id=?`, bytes.Repeat([]byte{0}, int(f.document.ByteLength)), f.document.Id)
			}
			if err != nil {
				t.Fatal(err)
			}
			bas, err := reporting.NewRepositoryWithClock(tx, func() time.Time { return f.now }, purchaseRepo)
			if err != nil {
				t.Fatal(err)
			}
			key, _ := f.id()
			id, _ := f.id()
			_, err = bas.CreateBASDraft(ctx, key, id, postingOrg, postDate(1), postDate(31), f.now)
			if err == nil || postScalar(t, tx, `SELECT count(*) FROM bas_workpapers`) != 0 {
				t.Fatalf("%s corruption served a partial draft: %v", source, err)
			}
		})
	}
}

func TestPurchaseBASExact1000OwnedSourcesAnd1001Overflow(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	purchaseRepo := f.repo(tx)
	for i := 0; i < 20; i++ {
		if i > 0 {
			f.addDocument("Bound supplier", fmt.Sprintf("SOURCE-BOUND-%d", i), 10000, 1000)
		}
		allocation := proto.Clone(f.request.Allocations[0]).(*tammyv1.PurchaseAllocationInput)
		f.request.Allocations = nil
		for n := 1; n <= 50; n++ {
			line := proto.Clone(allocation).(*tammyv1.PurchaseAllocationInput)
			line.Sequence = uint32(n)
			line.Gross, line.SupplierGst, line.EligibleGst = postMoney(220), postMoney(20), postMoney(20)
			f.request.Allocations = append(f.request.Allocations, line)
		}
		if _, err := purchaseRepo.PostReviewed(ctx, f.request); err != nil {
			t.Fatalf("posting %d: %v", i, err)
		}
	}
	bas, err := reporting.NewRepositoryWithClock(tx, func() time.Time { return f.now }, purchaseRepo)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := f.id()
	id, _ := f.id()
	w, err := bas.CreateBASDraft(ctx, key, id, postingOrg, postDate(1), postDate(31), f.now)
	if err != nil {
		t.Fatal(err)
	}
	if len(w.PurchaseSources) != 1000 || w.GstCredits_1B.MinorUnits != 20000 || len(w.Sources) != 0 || w.LegacyCandidateGst.MinorUnits != 0 || paymentBalance(t, tx, "1150") != 20000 {
		t.Fatal("exact source limit lost owned credit/provenance")
	}
	loaded, err := bas.GetCurrentBASDraft(ctx, postingOrg)
	if err != nil || !proto.Equal(w, loaded) {
		t.Fatalf("exact source capacity did not reload every row: %v", err)
	}
	f.addDocument("Overflow supplier", "SOURCE-BOUND-OVERFLOW", 10, 1)
	if _, err := purchaseRepo.PostReviewed(ctx, f.request); err != nil {
		t.Fatal(err)
	}
	key, _ = f.id()
	id, _ = f.id()
	_, err = bas.CreateBASDraft(ctx, key, id, postingOrg, postDate(1), postDate(31), f.now)
	if !errors.Is(err, reporting.ErrSourceBound) || postScalar(t, tx, `SELECT count(*) FROM bas_workpapers`) != 1 {
		t.Fatalf("1001 owned source rows saved partial workpaper: %v", err)
	}
	retained, err := bas.GetCurrentBASDraft(ctx, postingOrg)
	if err != nil || !retained.SourcesChanged || len(retained.PurchaseSources) != 1000 || retained.GstCredits_1B.MinorUnits != 20000 {
		t.Fatalf("unsupported current volume lost old complete snapshot: %v", err)
	}
}

func TestPurchaseBASExact1000SupportLinksAnd1001Overflow(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	request := legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_NONCREDITABLE)
	for i := 1; i < 200; i++ {
		f.addDocument("Supporting statement", fmt.Sprintf("SUPPORT-%d", i), 0, 0)
		request.Review.SupportingEvidence = append(request.Review.SupportingEvidence, &tammyv1.PurchaseRetainedSourceRef{SourceId: f.document.Id, SourceVersion: f.document.Version, SourceHash: append([]byte(nil), f.document.Sha256...)})
	}
	legacy := legacyRepo(t, f, tx)
	for i := 0; i < 5; i++ {
		if i > 0 {
			request = electNextLegacy(t, f, request)
		}
		request.Review.Reason = fmt.Sprintf("Reviewed retained supporting statement batch %d", i+1)
		if _, err := legacy.ResolveLegacy(ctx, request); err != nil {
			t.Fatalf("legacy review %d: %v", i, err)
		}
	}
	purchaseRepo := f.repo(tx)
	bas, err := reporting.NewRepositoryWithClock(tx, func() time.Time { return f.now }, purchaseRepo)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := f.id()
	id, _ := f.id()
	w, err := bas.CreateBASDraft(ctx, key, id, postingOrg, postDate(1), postDate(31), f.now)
	if err != nil {
		t.Fatal(err)
	}
	if len(w.LegacyPurchaseSupport) != 1000 || w.GstCredits_1B.MinorUnits != 0 {
		t.Fatal("exact global support bound lost legacy decision provenance")
	}
	loaded, err := bas.GetCurrentBASDraft(ctx, postingOrg)
	if err != nil || !proto.Equal(w, loaded) {
		t.Fatalf("support links did not reload exactly: %v", err)
	}
	request = electNextLegacy(t, f, request)
	request.Review.SupportingEvidence = request.Review.SupportingEvidence[:1]
	if _, err := legacy.ResolveLegacy(ctx, request); err != nil {
		t.Fatal(err)
	}
	key, _ = f.id()
	id, _ = f.id()
	_, err = bas.CreateBASDraft(ctx, key, id, postingOrg, postDate(1), postDate(31), f.now)
	if !errors.Is(err, reporting.ErrSourceBound) || postScalar(t, tx, `SELECT count(*) FROM bas_workpapers`) != 1 {
		t.Fatalf("1001 support rows saved a partial workpaper: %v", err)
	}
}

func TestPurchaseBASRetainedSupportReloadsCanonicalOrderWithTiedUUIDTimes(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	request := legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_NONCREDITABLE)
	legacy := legacyRepo(t, f, tx)
	// Canonical UUIDv7 randomness within a tied millisecond does not promise
	// identifier order. Source history still follows accounting version.
	f.next = 90000
	if _, err := legacy.ResolveLegacy(ctx, request); err != nil {
		t.Fatal(err)
	}
	f.next = 80000
	request = electNextLegacy(t, f, request)
	request.Review.Reason = "Second retained review of the same noncreditable source"
	if _, err := legacy.ResolveLegacy(ctx, request); err != nil {
		t.Fatal(err)
	}
	bas, err := reporting.NewRepositoryWithClock(tx, func() time.Time { return f.now }, f.repo(tx))
	if err != nil {
		t.Fatal(err)
	}
	key, _ := f.id()
	id, _ := f.id()
	w, err := bas.CreateBASDraft(ctx, key, id, postingOrg, postDate(1), postDate(31), f.now)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := bas.GetCurrentBASDraft(ctx, postingOrg)
	if err != nil || loaded.SourcesChanged || !proto.Equal(w, loaded) {
		t.Fatalf("tied UUID identities changed complete support order on reload: %v", err)
	}
}

func TestPurchaseBASOutstandingDeferralRetainsWithdrawnEvidenceReview(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f, repo, state, portion := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	deferred, err := deferCommand(t, repo, deferRequest(t, f, state, portion))
	if err != nil {
		t.Fatal(err)
	}
	withdrawClaimEvidence(t, f, repo, deferred.Accounting)
	snapshot, err := repo.CaptureBAS(ctx, postingOrg, claimPeriod(), claimPeriod().PeriodEnd, f.now)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, row := range snapshot.Reviews {
		if row.DeferralId == nil || *row.DeferralId != deferred.Deferral.Id {
			continue
		}
		found = true
		if row.Status != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_NEEDS_EVIDENCE || row.UnclaimedGst.MinorUnits != 500 || !proto.Equal(row.FutureClaimObligation.Portion, portion) || row.FutureClaimObligation.AllocatedDeferredGst.MinorUnits != 500 {
			t.Fatalf("outstanding future credit ignored withdrawn evidence: %v", row)
		}
	}
	if !found || len(snapshot.Sources) != 0 || paymentBalance(t, tx, "1150") != 0 {
		t.Fatal("withdrawn future claim lost history or became credit")
	}
}

func TestPurchaseBASHistoricalClaimRetainsCurrentWithdrawnEvidenceReview(t *testing.T) {
	for _, basis := range []tammyv1.GstBasis{tammyv1.GstBasis_GST_BASIS_CASH, tammyv1.GstBasis_GST_BASIS_NON_CASH} {
		t.Run(basis.String(), func(t *testing.T) {
			ctx := context.Background()
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f, repo, state, portion := claimSetup(t, tx, basis)
			claimed, err := attributeCommand(t, repo, claimRequest(t, f, state, portion))
			if err != nil {
				t.Fatal(err)
			}
			before, err := repo.CaptureBAS(ctx, postingOrg, claimPeriod(), claimPeriod().PeriodEnd, f.now)
			if err != nil || len(before.Sources) != 1 {
				t.Fatalf("original claim capture: %v", err)
			}
			current := withdrawClaimEvidence(t, f, repo, claimed.Accounting)
			latest := purchaseBASLatestRetainedEvidence(t, tx, f.document.Id, current.AccountingVersion)
			snapshot, err := repo.CaptureBAS(ctx, postingOrg, claimPeriod(), claimPeriod().PeriodEnd, f.now)
			if err != nil {
				t.Fatal(err)
			}
			if len(snapshot.Sources) != 1 || !proto.Equal(before.Sources[0], snapshot.Sources[0]) || current.SuspenseGst.MinorUnits != 0 || paymentBalance(t, tx, "1150") != claimed.Accounting.AttributedGst.MinorUnits {
				t.Fatal("later evidence withdrawal rewrote accepted credit provenance or accounting")
			}
			var found bool
			for _, row := range snapshot.Reviews {
				if row.PostingId != nil && *row.PostingId == state.Posting.Id && row.Status == tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_NEEDS_EVIDENCE && row.UnclaimedGst.MinorUnits == 0 && row.SourceRevision == current.AccountingVersion && row.Evidence != nil && bytes.Equal(row.SourceHash, latest.SourceHash) && bytes.Equal(row.Evidence.SourceHash, latest.SourceHash) && row.Evidence.SourceId == latest.Id && row.Evidence.SourceVersion == latest.AccountingVersion {
					found = true
				}
			}
			if !found {
				t.Fatal("accepted credit hid the latest withdrawn evidence review from complete BAS coverage")
			}
		})
	}
}

func TestPurchaseBASAutomaticCreditRetainsCurrentWithdrawnEvidenceReview(t *testing.T) {
	for _, basis := range []tammyv1.GstBasis{tammyv1.GstBasis_GST_BASIS_CASH, tammyv1.GstBasis_GST_BASIS_NON_CASH} {
		t.Run(basis.String(), func(t *testing.T) {
			ctx := context.Background()
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f := newPostingFixture(t, tx, basis)
			repo := f.repo(tx)
			posted, err := repo.PostReviewed(ctx, f.request)
			if err != nil {
				t.Fatal(err)
			}
			state := posted.Accounting
			if basis == tammyv1.GstBasis_GST_BASIS_CASH {
				paid, err := repo.RecordPayment(ctx, paymentRequest(t, f, state.AccountingVersion, 11000, paymentDate(9, 30)))
				if err != nil {
					t.Fatal(err)
				}
				state = paid.Accounting
			}
			period := &tammyv1.PurchaseClaimPeriod{PeriodStart: paymentDate(7, 1), PeriodEnd: paymentDate(9, 30)}
			before, err := repo.CaptureBAS(ctx, postingOrg, period, period.PeriodEnd, f.now)
			if err != nil || len(before.Sources) != 1 {
				t.Fatalf("original automatic capture: %v", err)
			}
			current := withdrawClaimEvidence(t, f, repo, state)
			latest := purchaseBASLatestRetainedEvidence(t, tx, f.document.Id, current.AccountingVersion)
			snapshot, err := repo.CaptureBAS(ctx, postingOrg, period, period.PeriodEnd, f.now)
			if err != nil {
				t.Fatal(err)
			}
			if len(snapshot.Sources) != 1 || !proto.Equal(before.Sources[0], snapshot.Sources[0]) || current.SuspenseGst.MinorUnits != 0 || paymentBalance(t, tx, "1150") != 1000 {
				t.Fatal("evidence withdrawal rewrote accepted automatic credit")
			}
			var found bool
			for _, row := range snapshot.Reviews {
				if row.PostingId != nil && *row.PostingId == state.Posting.Id && row.Status == tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_NEEDS_EVIDENCE && row.UnclaimedGst.MinorUnits == 0 && row.SourceRevision == current.AccountingVersion && row.Evidence != nil && bytes.Equal(row.SourceHash, latest.SourceHash) && bytes.Equal(row.Evidence.SourceHash, latest.SourceHash) && row.Evidence.SourceId == latest.Id && row.Evidence.SourceVersion == latest.AccountingVersion {
					found = true
				}
			}
			if !found {
				t.Fatal("automatic credit hid current withdrawn evidence review")
			}
		})
	}
}

func purchaseBASLatestRetainedEvidence(t *testing.T, executor app.CommandSQLExecutor, document string, version uint64) *tammyv1.PurchaseTaxEvidence {
	t.Helper()
	rows, err := executor.QueryContext(context.Background(), `SELECT evidence_proto FROM purchase_tax_evidence_v1 WHERE organisation_id=? AND document_id=? AND accounting_version=?`, postingOrg, document, version)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var content []byte
	if !rows.Next() || rows.Scan(&content) != nil || rows.Next() || rows.Err() != nil {
		t.Fatal("missing unique latest evidence")
	}
	retained := &tammyv1.PurchaseTaxEvidence{}
	if err := proto.Unmarshal(content, retained); err != nil {
		t.Fatal(err)
	}
	return retained
}

func TestPurchaseBASRetainsAllocationAccountSupplierMetadata(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	repo := f.repo(tx)
	posted, err := repo.PostReviewed(ctx, f.request)
	if err != nil {
		t.Fatal(err)
	}
	period := &tammyv1.PurchaseClaimPeriod{PeriodStart: postDate(1), PeriodEnd: postDate(31)}
	snapshot, err := repo.CaptureBAS(ctx, postingOrg, period, period.PeriodEnd, f.now)
	if err != nil || len(snapshot.Sources) != 1 {
		t.Fatalf("capture metadata: %v", err)
	}
	row := snapshot.Sources[0].ProtoReflect()
	for name, expected := range map[string]string{"account_id": posted.Posting.Allocations[0].AccountId, "supplier_name": posted.Posting.SupplierName, "invoice_number": posted.Posting.InvoiceNumber} {
		field := row.Descriptor().Fields().ByName(protoreflect.Name(name))
		if field == nil || !row.Has(field) || row.Get(field).String() != expected {
			t.Fatalf("saved source omitted %s provenance", name)
		}
	}
	field := row.Descriptor().Fields().ByName("account_version")
	if field == nil || !row.Has(field) || row.Get(field).Uint() != posted.Posting.Allocations[0].AccountVersion {
		t.Fatal("saved source omitted retained account version")
	}
}

func TestPurchaseBASDeferralResolvesOnlyItsAffectedPeriod(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f, repo, state, portion := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	deferred, err := deferCommand(t, repo, deferRequest(t, f, state, portion))
	if err != nil {
		t.Fatal(err)
	}
	for _, period := range []*tammyv1.PurchaseClaimPeriod{
		{PeriodStart: paymentDate(7, 1), PeriodEnd: paymentDate(9, 30)},
		{PeriodStart: &tammyv1.CivilDate{Year: 2027, Month: 1, Day: 1}, PeriodEnd: &tammyv1.CivilDate{Year: 2027, Month: 3, Day: 31}},
	} {
		snapshot, err := repo.CaptureBAS(ctx, postingOrg, period, period.PeriodEnd, f.now)
		if err != nil {
			t.Fatal(err)
		}
		var found bool
		for _, row := range snapshot.Reviews {
			if row.DeferralId == nil || *row.DeferralId != deferred.Deferral.Id {
				continue
			}
			found = true
			if row.Status != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_NEEDS_TIMING_REVIEW || row.UnclaimedGst.MinorUnits != 500 || !proto.Equal(row.FutureClaimObligation.AffectedPeriod, claimPeriod()) {
				t.Fatalf("deferral in %v incorrectly cleared selected period %v: %v", claimPeriod(), period, row)
			}
		}
		if !found {
			t.Fatal("future claim no longer discoverable outside deferred period")
		}
	}
}

func TestPurchaseBASLaterPeriodDeferralClearsItsOwnDecisionWithoutDoubleFutureCredit(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f, repo, state, portion := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	first, err := deferCommand(t, repo, deferRequest(t, f, state, portion))
	if err != nil {
		t.Fatal(err)
	}
	f.now = time.Date(2027, 1, 7, 12, 0, 0, 0, time.UTC)
	period := &tammyv1.PurchaseClaimPeriod{PeriodStart: &tammyv1.CivilDate{Year: 2027, Month: 1, Day: 1}, PeriodEnd: &tammyv1.CivilDate{Year: 2027, Month: 3, Day: 31}}
	request := deferRequest(t, f, first.Accounting, portion)
	request.AffectedPeriod = period
	request.TimingReview.ReviewedAsOf = &tammyv1.CivilDate{Year: 2027, Month: 1, Day: 7}
	second, err := deferCommand(t, repo, request)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := repo.CaptureBAS(ctx, postingOrg, period, period.PeriodEnd, f.now)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	var unclaimed int64
	for _, row := range snapshot.Reviews {
		if row.DeferralId == nil {
			continue
		}
		count++
		unclaimed += row.UnclaimedGst.MinorUnits
		if row.Status != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR || row.FutureClaimObligation.AllocatedDeferredGst.MinorUnits != 500 {
			t.Fatalf("supported same-period deferral did not clear its own decision: %v", row)
		}
		if *row.DeferralId == second.Deferral.Id && row.UnclaimedGst.MinorUnits != 500 {
			t.Fatal("selected period lost its supported future extent")
		}
	}
	if count != 2 || unclaimed != 500 || paymentBalance(t, tx, "1170") != 500 {
		t.Fatal("period-specific decisions duplicated physical future GST or discarded history")
	}
}

func TestPurchaseBASNewOnTimeEvidenceStillRequiresExplicitSuspenseClaim(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	ready := proto.Clone(f.request.Evidence).(*tammyv1.PurchaseEvidenceInput)
	f.request.Evidence.Classification = tammyv1.PurchaseEvidenceClassification_PURCHASE_EVIDENCE_CLASSIFICATION_UNKNOWN
	f.request.Evidence.Source, f.request.Evidence.HeldDate = nil, nil
	repo := f.repo(tx)
	posted, err := repo.PostReviewed(ctx, f.request)
	if err != nil {
		t.Fatal(err)
	}
	evidenced, err := evidenceCommand(t, repo, ctx, evidenceRequest(t, f, posted.Accounting.AccountingVersion, ready))
	if err != nil {
		t.Fatal(err)
	}
	if evidenced.Accounting.SuspenseGst.MinorUnits != 1000 || paymentBalance(t, tx, "1150") != 0 {
		t.Fatal("evidence-only append moved financial GST")
	}
	period := &tammyv1.PurchaseClaimPeriod{PeriodStart: postDate(1), PeriodEnd: postDate(31)}
	snapshot, err := repo.CaptureBAS(ctx, postingOrg, period, period.PeriodEnd, f.now)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, row := range snapshot.Reviews {
		if row.PostingId == nil {
			continue
		}
		found = true
		documentField := row.ProtoReflect().Descriptor().Fields().ByName("document_id")
		if documentField == nil || row.ProtoReflect().Get(documentField).String() != f.request.DocumentId {
			t.Fatal("unresolved posted purchase has no retained document route")
		}
		if row.Kind != tammyv1.BasPurchaseReviewKind_BAS_PURCHASE_REVIEW_KIND_CLAIM_TIMING || row.Status != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_NEEDS_TIMING_REVIEW || row.UnclaimedGst.MinorUnits != 1000 {
			t.Fatalf("supported evidence cleared unresolved suspense claim: %v", row)
		}
	}
	if !found || len(snapshot.Sources) != 0 {
		t.Fatal("unclaimed suspense disappeared or was reported in1B")
	}
	bas, err := reporting.NewRepositoryWithClock(tx, func() time.Time { return f.now }, repo)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := f.id()
	id, _ := f.id()
	w, err := bas.CreateBASDraft(ctx, key, id, postingOrg, period.PeriodStart, period.PeriodEnd, f.now)
	if err != nil {
		t.Fatal(err)
	}
	var indexedDocument *string
	if err := tx.QueryRowContext(ctx, `SELECT document_id FROM bas_workpaper_purchase_reviews_v1 WHERE workpaper_id=? AND posting_id=?`, w.Id, posted.Accounting.Posting.Id).Scan(&indexedDocument); err != nil || indexedDocument == nil || *indexedDocument != f.request.DocumentId {
		t.Fatalf("saved posting review does not index its retained document route: %v", err)
	}
	loaded, err := bas.GetCurrentBASDraft(ctx, postingOrg)
	if err != nil || !proto.Equal(w, loaded) {
		t.Fatalf("saved posting review did not retain its exact document route: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_workpaper_purchase_reviews_v1_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE bas_workpaper_purchase_reviews_v1 SET document_id=NULL WHERE workpaper_id=? AND posting_id=?`, w.Id, posted.Accounting.Posting.Id); err != nil {
		t.Fatal(err)
	}
	if _, err := bas.GetCurrentBASDraft(ctx, postingOrg); !errors.Is(err, reporting.ErrRepository) {
		t.Fatalf("saved review accepted a document index inconsistent with retained facts: %v", err)
	}
}

func TestPurchaseBASActualPaymentPeriodReconcilesOwnedCredits(t *testing.T) {
	for _, basis := range []tammyv1.GstBasis{tammyv1.GstBasis_GST_BASIS_CASH, tammyv1.GstBasis_GST_BASIS_NON_CASH} {
		t.Run(basis.String(), func(t *testing.T) {
			ctx := context.Background()
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f := newPostingFixture(t, tx, basis)
			repo := f.repo(tx)
			posted, err := repo.PostReviewed(ctx, f.request)
			if err != nil {
				t.Fatal(err)
			}
			f.now = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
			for i, date := range []*tammyv1.CivilDate{paymentDate(9, 30), paymentDate(10, 2)} {
				if _, err := repo.RecordPayment(ctx, paymentRequest(t, f, uint64(i+1), 5500, date)); err != nil {
					t.Fatal(err)
				}
			}
			bas, err := reporting.NewRepository(tx)
			if err != nil {
				t.Fatal(err)
			}
			for _, month := range []int32{8, 9, 10} {
				key, _ := f.id()
				id, _ := f.id()
				end := int32(31)
				if month == 9 {
					end = 30
				}
				wp, err := bas.CreateBASDraft(ctx, key, id, postingOrg, paymentDate(month, 1), paymentDate(month, end), f.now)
				if err != nil {
					t.Fatal(err)
				}
				want := int64(0)
				if basis == tammyv1.GstBasis_GST_BASIS_CASH && month >= 9 {
					want = 500
				}
				if basis == tammyv1.GstBasis_GST_BASIS_NON_CASH && month == 8 {
					want = 1000
				}
				if wp.GstCredits_1B.MinorUnits != want {
					t.Fatalf("month %d reconciled 1B=%d want %d", month, wp.GstCredits_1B.MinorUnits, want)
				}
				if wp.GetPurchaseSourceFormat() != tammyv1.BasPurchaseSourceFormat_BAS_PURCHASE_SOURCE_FORMAT_RECONCILED_V1 || wp.LegacyCandidateGst == nil || len(wp.Sources) != 0 {
					t.Fatalf("new draft did not retain reconciled purchase format: %v", wp)
				}
				if want > 0 {
					if len(wp.PurchaseSources) != 1 {
						t.Fatalf("month %d source count=%d want 1", month, len(wp.PurchaseSources))
					}
					s := wp.PurchaseSources[0]
					if s.PostingId != posted.Accounting.Posting.Id || s.DocumentId != f.document.Id || s.AllocationSequence != 1 || s.ClaimedGst.MinorUnits != want || s.InvoiceDate.Month != 8 || len(s.PostingHash) != 32 || len(s.SourceHash) != 32 || s.Evidence == nil {
						t.Fatalf("incomplete retained source: %v", s)
					}
					if basis == tammyv1.GstBasis_GST_BASIS_CASH && (s.PaymentId == nil || s.ActualPaymentDate == nil || s.ActualPaymentDate.Month != month || s.NormalTaxEventDate.Month != month) {
						t.Fatalf("actual cash event was replaced by invoice date: %v", s)
					}
					if basis == tammyv1.GstBasis_GST_BASIS_NON_CASH && (s.PaymentId != nil || s.ActualPaymentDate != nil) {
						t.Fatal("non-cash credit duplicated settlement provenance")
					}
				}
				loaded, err := bas.GetCurrentBASDraft(ctx, postingOrg)
				if err != nil {
					t.Fatal(err)
				}
				if loaded.GstCredits_1B.MinorUnits != want || len(loaded.PurchaseSources) != len(wp.PurchaseSources) {
					t.Fatal("saved typed purchase sources did not reload")
				}
			}
			if got := paymentBalance(t, tx, "1150"); got != 1000 {
				t.Fatalf("owned GST control balance=%d want 1000", got)
			}
		})
	}
}

func TestPurchaseBASPartialDeferralDoesNotHideOtherSuspense(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f, repo, state, portion := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	second, err := repo.RecordPayment(ctx, paymentRequest(t, f, state.AccountingVersion, 5500, paymentDate(9, 29)))
	if err != nil {
		t.Fatal(err)
	}
	deferred, err := deferCommand(t, repo, deferRequest(t, f, second.Accounting, portion))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := repo.CaptureBAS(ctx, postingOrg, claimPeriod(), claimPeriod().PeriodEnd, f.now)
	if err != nil {
		t.Fatal(err)
	}
	var future, unresolved int64
	for _, row := range snapshot.Reviews {
		if row.PostingId == nil {
			continue
		}
		if row.Kind == tammyv1.BasPurchaseReviewKind_BAS_PURCHASE_REVIEW_KIND_DEFERRED_FUTURE_CLAIM {
			future += row.UnclaimedGst.MinorUnits
			if row.FutureClaimObligation.DeferralId != deferred.Deferral.Id || row.FutureClaimObligation.AllocatedDeferredGst.MinorUnits != 500 {
				t.Fatal("future claim lost exact individual amount")
			}
		} else if row.Kind == tammyv1.BasPurchaseReviewKind_BAS_PURCHASE_REVIEW_KIND_CLAIM_TIMING || row.Kind == tammyv1.BasPurchaseReviewKind_BAS_PURCHASE_REVIEW_KIND_MISSING_EVIDENCE {
			unresolved += row.UnclaimedGst.MinorUnits
		}
	}
	if future != 500 || unresolved != 500 {
		t.Fatalf("partial deferral hid suspense: future=%d unresolved=%d want500/500", future, unresolved)
	}
}

func TestPurchaseBASDeferralHistoryRemainsWithoutPhantomExpiredCredit(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f, repo, state, portion := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	deferred, err := deferCommand(t, repo, deferRequest(t, f, state, portion))
	if err != nil {
		t.Fatal(err)
	}
	request := claimRequest(t, f, deferred.Accounting, portion)
	id := deferred.Deferral.Id
	request.DeferralId = &id
	if _, err := attributeCommand(t, repo, request); err != nil {
		t.Fatal(err)
	}
	snapshot, err := repo.CaptureBAS(ctx, postingOrg, claimPeriod(), claimPeriod().PeriodEnd, time.Date(2030, 11, 4, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, row := range snapshot.Reviews {
		if row.DeferralId != nil && *row.DeferralId == id {
			found = true
			if row.UnclaimedGst.MinorUnits != 0 || row.Status != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR {
				t.Fatal("consumed deferral created phantom expired credit")
			}
			if !proto.Equal(row.FutureClaimObligation.Portion, portion) || row.FutureClaimObligation.AllocatedDeferredGst.MinorUnits != 500 {
				t.Fatal("deferral history lost original allocated portion")
			}
		}
	}
	if !found {
		t.Fatal("complete BAS capture discarded retained deferral history after attribution")
	}
}
