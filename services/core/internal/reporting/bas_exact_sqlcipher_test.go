//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package reporting

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"google.golang.org/protobuf/proto"
)

type failingBASPurchasePort struct{}

func (failingBASPurchasePort) CaptureBAS(context.Context, string, *tammyv1.PurchaseClaimPeriod, *tammyv1.CivilDate, time.Time) (*purchases.BASSnapshot, error) {
	return nil, errors.New("injected current source failure")
}

func TestBASSaveCapturedDraftRetainsExactBodyWithoutRecaptureAndRejectsUnbalancedAmounts(t *testing.T) {
	tx, repository := reportFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	ctx := context.Background()
	original, err := repository.CreateBASDraft(ctx, reportID(9310), reportID(9311), reportOrgID,
		reportDate("2026-07-01"), reportDate("2026-09-30"), time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	repository.purchasePort = failingBASPurchasePort{}
	want := proto.Clone(original).(*tammyv1.BasWorkpaper)
	want.Id = reportID(9312)
	before := purchaseSavedCount(t, tx)
	bad := proto.Clone(want).(*tammyv1.BasWorkpaper)
	bad.SalesG1.MinorUnits = 1
	if got, err := repository.SaveCapturedBASDraft(ctx, reportID(9313), BASDraftCapture{Workpaper: bad}); got != nil || err == nil {
		t.Fatalf("unbalanced source totals saved: %v, %v", got, err)
	}
	if purchaseSavedCount(t, tx) != before {
		t.Fatal("rejected capture wrote partial draft")
	}
	saved, err := repository.SaveCapturedBASDraft(ctx, reportID(9314), BASDraftCapture{Workpaper: want})
	if err != nil || !proto.Equal(saved, want) {
		t.Fatalf("captured draft save=%v,%v", saved, err)
	}
	exact, err := repository.GetExactBASDraft(ctx, reportOrgID, want.Id)
	if err != nil || !proto.Equal(exact, want) {
		t.Fatalf("captured exact read=%v,%v", exact, err)
	}
	firstHash, _ := BASWorkpaperContentHash(want)
	secondHash, _ := BASWorkpaperContentHash(exact)
	if len(firstHash) != 32 || !bytes.Equal(firstHash, secondHash) {
		t.Fatal("retained immutable workpaper hash changed")
	}
	if _, err := repository.GetExactBASDraft(ctx, reportID(9998), want.Id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong owner exact lookup=%v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("saver or exact reader ended caller transaction: %v", err)
	}
}

func TestBASExactReaderLoadsRetainedBodyWithoutCurrentSourceCapture(t *testing.T) {
	_, repository := reportFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	ctx := context.Background()
	created, err := repository.CreateBASDraft(ctx, reportID(9300), reportID(9301), reportOrgID,
		reportDate("2026-07-01"), reportDate("2026-09-30"), time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	repository.purchasePort = failingBASPurchasePort{}
	exact, err := repository.GetExactBASDraft(ctx, reportOrgID, created.Id)
	if err != nil || !proto.Equal(exact, created) {
		t.Fatalf("exact retained body=%v, %v", exact, err)
	}
	digest, err := BASWorkpaperContentHash(exact)
	if err != nil || len(digest) != 32 {
		t.Fatalf("whole-body digest length=%d err=%v", len(digest), err)
	}
	if _, err := repository.GetCurrentBASDraft(ctx, reportOrgID); err == nil {
		t.Fatal("current read did not attempt source capture; exact reader test would be vacuous")
	}
}
