//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"strconv"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/organisations"
	"github.com/tammyapp/tammy/services/core/internal/reporting"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRegenerateBASPreparationNativeSQLCipherCapturesConservesAndRechecksCurrentness(t *testing.T) {
	for _, basis := range []tammyv1.GstBasis{
		tammyv1.GstBasis_GST_BASIS_NON_CASH,
		tammyv1.GstBasis_GST_BASIS_CASH,
	} {
		t.Run(basis.String(), func(t *testing.T) {
			ctx := context.Background()
			_, tx, source, ports, _, _ := newBASSourceProducerNativeFixtureWithBasis(t, false, basis)
			invoice := seedBASSourcePostedInvoice(t, tx, source.OrganisationID, basis)
			if invoice == nil || invoice.Id == "" {
				t.Fatal("ordinary invoice/accounting writer returned no posted invoice")
			}
			actor := testUUID(9962) // retained owner created by the native SQLCipher fixture
			profileID, periodID := testUUID(20001), testUUID(20002)
			codec := newBASRepositoryCursorCodec(t)

			// Bind the capture to real immutable scope rows in the same SQLCipher
			// transaction as the ordinary invoice/accounting writers above.
			profiles, err := NewProfileRepository(tx, codec)
			if err != nil {
				t.Fatal(err)
			}
			profile := validBASProfile(source.OrganisationID, profileID, 1)
			profile.ActorUserId = actor
			profileRef, err := profiles.Append(ctx, profile, nil)
			if err != nil {
				t.Fatalf("retain exact BAS profile: %v", err)
			}
			periods, err := NewPeriodRepository(tx, codec)
			if err != nil {
				t.Fatal(err)
			}
			period := validBASPeriod(source.OrganisationID, periodID, profileRef, 1,
				testCivilDate(2026, 7, 1), testCivilDate(2026, 9, 30),
				tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JUL_SEP)
			period.ActorUserId = actor
			periodRef, err := periods.Append(ctx, period, nil)
			if err != nil {
				t.Fatalf("retain exact confirmed BAS period: %v", err)
			}

			period.ContentHash = bytes.Clone(periodRef.ContentHash)

			// 13:00 UTC is a Melbourne civil-date boundary; after the spring
			// transition it is midnight on the following local day.
			clock := time.Date(2026, 10, 4, 13, 0, 0, 0, time.UTC)
			nextID := uint64(21000)
			newID := func() (string, error) { nextID++; return testUUID(nextID), nil }
			callbacks, err := NewBASCommandCapture(tx, codec, func() time.Time { return clock }, newID)
			if err != nil || callbacks.Regenerate == nil || callbacks.CaptureCurrent == nil {
				t.Fatalf("construct shared command capture: %#v, %v", callbacks, err)
			}
			absent := func() *tammyv1.BasExpectedIdentity {
				return &tammyv1.BasExpectedIdentity{ExpectedState: &tammyv1.BasExpectedIdentity_Absent{Absent: true}}
			}
			request := &tammyv1.RegenerateBasPreparationRequest{
				CommandContext: &tammyv1.CommandContext{
					Authentication: &tammyv1.AuthenticationContext{ActorUserId: actor, SessionId: testUUID(20003)},
					IdempotencyKey: testUUID(20004),
				},
				OrganisationId: source.OrganisationID, FamilyExpectation: absent(), PreparationExpectation: absent(),
				Profile: profileRef, Period: periodRef,
			}
			first, err := callbacks.Regenerate(ctx, request, actor, timestamppb.New(clock))
			if err != nil || first == nil || first.Response == nil || first.Response.Preparation == nil || first.Response.Calculation == nil {
				t.Fatalf("first native source-backed regeneration = %#v, %v", first, err)
			}
			firstPrep, firstCalc := first.Response.Preparation, first.Response.Calculation
			wantG1, want1A := int64(13000), int64(1000)
			if basis == tammyv1.GstBasis_GST_BASIS_CASH {
				wantG1, want1A = 6500, 500 // only the Aug 15 receipt is in the confirmed quarter
			}
			if firstCalc.WorkpaperSnapshot == nil || firstCalc.Result == nil || firstCalc.CentWorkpaper == nil ||
				firstCalc.RevisionVector == nil || firstCalc.WorkpaperSnapshot.SalesG1.MinorUnits != wantG1 ||
				firstCalc.WorkpaperSnapshot.GstOnSales_1A.MinorUnits != want1A ||
				len(firstCalc.WorkpaperSnapshot.SalesSources) != 2 || len(firstCalc.Coverage) == 0 ||
				len(firstCalc.SourceSnapshotHash) != sha256.Size || len(firstCalc.CoverageManifestHash) != sha256.Size {
				t.Fatalf("nonempty income/coverage was not captured for %s: workpaper=%#v coverage=%d", basis,
					firstCalc.WorkpaperSnapshot, len(firstCalc.Coverage))
			}
			for _, line := range firstCalc.WorkpaperSnapshot.SalesSources {
				if line.InvoiceId != invoice.Id {
					t.Fatalf("workpaper income source points at %q, want writer-backed invoice %q", line.InvoiceId, invoice.Id)
				}
			}
			if g1 := traceForLabel(t, firstCalc.Result, tammyv1.BasFormLabel_BAS_FORM_LABEL_G1); g1.GetExactCentsNumerator() != formatBASInt(wantG1) ||
				g1.State == tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN {
				t.Fatalf("G1 calculation label lost mapped source amount: %#v", g1)
			}
			if oneA := traceForLabel(t, firstCalc.Result, tammyv1.BasFormLabel_BAS_FORM_LABEL_1A); oneA.GetExactCentsNumerator() != formatBASInt(want1A) ||
				oneA.State == tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN {
				t.Fatalf("1A calculation label lost mapped source amount: %#v", oneA)
			}

			preparations, err := NewPreparationRepository(tx, codec)
			if err != nil {
				t.Fatal(err)
			}
			firstOwner, err := preparations.GetExact(ctx, source.OrganisationID, first.Source)
			if err != nil || firstOwner == nil || firstOwner.SavedPaygInputs == nil {
				t.Fatalf("exact retained first preparation/input owner = %#v, %v", firstOwner, err)
			}
			calculations, err := NewCalculationRepository(tx, codec)
			if err != nil {
				t.Fatal(err)
			}
			firstStoredCalc, err := calculations.GetExact(ctx, source.OrganisationID, calculationReference(firstCalc))
			if err != nil || firstStoredCalc == nil || !proto.Equal(firstStoredCalc, firstCalc) {
				t.Fatalf("exact calculation reload = %#v, %v", firstStoredCalc, err)
			}
			workpapers, err := reporting.NewRepository(tx)
			if err != nil {
				t.Fatal(err)
			}
			storedWorkpaper, err := workpapers.GetExactBASDraft(ctx, source.OrganisationID, firstCalc.CentWorkpaper.Id)
			if err != nil || storedWorkpaper == nil || !proto.Equal(storedWorkpaper, firstCalc.WorkpaperSnapshot) {
				t.Fatalf("persisted workpaper differs from the calculation snapshot: %#v, %v", storedWorkpaper, err)
			}
			workpaperHash, err := reporting.BASWorkpaperContentHash(storedWorkpaper)
			calculationWorkpaperHash, calculationHashErr := reporting.BASWorkpaperContentHash(firstCalc.WorkpaperSnapshot)
			if err != nil || calculationHashErr != nil || len(workpaperHash) != sha256.Size ||
				!bytes.Equal(workpaperHash, calculationWorkpaperHash) ||
				!bytes.Equal(storedWorkpaper.SourceFingerprint, firstCalc.SourceSnapshotHash) ||
				!bytes.Equal(firstPrep.SourceSnapshotHash, firstCalc.SourceSnapshotHash) {
				t.Fatalf("workpaper/preparation/calculation source hash conservation failed: wp=%x calc=%x prep=%x hashErr=%v",
					storedWorkpaper.SourceFingerprint, firstCalc.SourceSnapshotHash, firstPrep.SourceSnapshotHash, err)
			}
			manifest, _, err := basCoverageManifest(ctx, firstStoredCalc.Coverage)
			if err != nil || !bytes.Equal(manifest, firstStoredCalc.CoverageManifestHash) {
				t.Fatalf("exact persisted coverage does not reproduce its manifest: %x, %x, %v", manifest, firstStoredCalc.CoverageManifestHash, err)
			}

			// Bump only the aggregate financial counter, the report-only case
			// deliberately excluded from the semantic source snapshot hash.
			var beforeFinancial uint64
			if err := tx.QueryRowContext(ctx, `SELECT financial_revision FROM financial_revisions WHERE id=1`).Scan(&beforeFinancial); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.ExecContext(ctx, `UPDATE financial_revisions SET financial_revision=financial_revision+1 WHERE id=1`); err != nil {
				t.Fatalf("advance report-only aggregate revision: %v", err)
			}
			secondRequest := proto.Clone(request).(*tammyv1.RegenerateBasPreparationRequest)
			secondRequest.CommandContext.IdempotencyKey = testUUID(20005)
			secondRequest.ExpectedVersion = firstPrep.Version
			secondRequest.FamilyExpectation = retainedBASIdentity(firstPrep.Family)
			secondRequest.PreparationExpectation = retainedBASIdentity(first.Source)
			second, err := callbacks.Regenerate(ctx, secondRequest, actor, timestamppb.New(clock))
			if err != nil || second == nil || second.Response == nil || second.Response.Calculation == nil {
				t.Fatalf("regeneration after report-only FinancialRevision change = %#v, %v", second, err)
			}
			secondPrep, secondCalc := second.Response.Preparation, second.Response.Calculation
			if secondCalc.RevisionVector == nil || firstCalc.RevisionVector == nil ||
				!bytes.Equal(secondCalc.SourceSnapshotHash, firstCalc.SourceSnapshotHash) ||
				secondCalc.RevisionVector.FinancialRevision != beforeFinancial+1 ||
				len(secondCalc.RevisionVectorHash) != sha256.Size || bytes.Equal(secondCalc.RevisionVectorHash, firstCalc.RevisionVectorHash) {
				t.Fatalf("report-only revision changed semantic source freshness or was not retained in audit vector: first=%#v second=%#v",
					firstCalc.RevisionVector, secondCalc.RevisionVector)
			}
			secondOwner, err := preparations.GetExact(ctx, source.OrganisationID, second.Source)
			if err != nil || secondOwner == nil {
				t.Fatalf("exact retained regenerated owner = %#v, %v", secondOwner, err)
			}
			live, err := callbacks.CaptureCurrent(ctx, secondOwner, secondCalc)
			if err != nil || !bytes.Equal(live.SourceSnapshotHash, secondPrep.SourceSnapshotHash) ||
				live.InputRevision != secondPrep.InputRevision || !bytes.Equal(live.InputHash, secondPrep.InputHash) ||
				live.RevisionVector.FinancialRevision != secondCalc.RevisionVector.FinancialRevision {
				t.Fatalf("fresh read-only current capture falsely staled same sources: capture=%#v prep=%#v err=%v",
					live, secondPrep, err)
			}

			// A newly retained draft invoice contributes no income to G1/1A, but
			// it changes the complete source census/fingerprint and must stale the
			// current capture rather than disappear because totals stayed fixed.
			seedSourceScalarInvoice(t, ctx, tx, source.OrganisationID, clock, testUUID(20006))
			updatedSource := source
			updatedSource.ConfirmedPeriod = period
			financialAfterDraft, err := CaptureBASFinancialSourceRecords(ctx, BASFinancialSourceRequest{
				Source: updatedSource, GstBasis: basis, AsOf: clock,
			}, BASBoundedSourcePorts{Invoices: ports.Invoices, Purchases: ports.Purchases, Accounting: ports.Accounting, Rules: ports.Rules})
			if err != nil || financialAfterDraft == nil || financialAfterDraft.Financial == nil ||
				financialAfterDraft.Financial.Invoices.G1GrossMinor != wantG1 ||
				financialAfterDraft.Financial.Invoices.GstOnSales1AMinor != want1A {
				t.Fatalf("draft source changed income totals: mapping=%#v, %v", financialAfterDraft, err)
			}
			stale, err := callbacks.CaptureCurrent(ctx, secondOwner, secondCalc)
			if err != nil || bytes.Equal(stale.SourceSnapshotHash, secondPrep.SourceSnapshotHash) {
				t.Fatalf("same-total draft invoice failed to stale source snapshot: capture=%#v err=%v", stale, err)
			}
		})
	}
}

func TestBASWorkspaceAsOfUsesMelbourneCivilDateAcrossSpringDSTBoundary(t *testing.T) {
	location, err := time.LoadLocation(organisations.WorkspaceTimezone)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		instant time.Time
		wantY   int
		wantM   time.Month
		wantD   int
		wantUTC int
	}{
		{time.Date(2026, 10, 3, 13, 0, 0, 0, time.UTC), 2026, time.October, 3, 10 * 60 * 60},
		{time.Date(2026, 10, 4, 13, 0, 0, 0, time.UTC), 2026, time.October, 5, 11 * 60 * 60},
	} {
		got, err := basWorkspaceAsOf(tc.instant)
		if err != nil {
			t.Fatalf("workspace as-of %s: %v", tc.instant, err)
		}
		local := tc.instant.In(location)
		if got.Location().String() != organisations.WorkspaceTimezone ||
			got.Year() != tc.wantY || got.Month() != tc.wantM || got.Day() != tc.wantD ||
			!got.Equal(tc.instant) || got.Year() != local.Year() || got.Month() != local.Month() || got.Day() != local.Day() {
			t.Fatalf("workspace as-of %s = %s; want local %04d-%02d-%02d", tc.instant, got,
				tc.wantY, tc.wantM, tc.wantD)
		}
		_, offset := got.Zone()
		if offset != tc.wantUTC {
			t.Fatalf("workspace offset at %s = %d; want %d", got, offset, tc.wantUTC)
		}
	}
}

func retainedBASIdentity(ref *tammyv1.SourceRef) *tammyv1.BasExpectedIdentity {
	if ref == nil {
		return nil
	}
	version := ref.Revision
	return &tammyv1.BasExpectedIdentity{
		ExpectedState:   &tammyv1.BasExpectedIdentity_Retained{Retained: proto.Clone(ref).(*tammyv1.SourceRef)},
		ExpectedVersion: &version,
	}
}

func formatBASInt(value int64) string {
	return strconv.FormatInt(value, 10)
}
