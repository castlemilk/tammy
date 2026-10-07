//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

func paymentPageOptions(t *testing.T, limit int) purchases.PageOptions {
	t.Helper()
	codec, err := paging.NewCodec(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	return purchases.PageOptions{Limit: limit, EncodeCursor: func(position string) (string, error) {
		return codec.Encode(paging.Cursor{Snapshot: "purchase-payments-v1", Position: position, QueryHash: sha256.Sum256([]byte("retained payment fixture"))})
	}}
}

func assertPaymentPage(t *testing.T, repo *purchases.Repository, document string, options purchases.PageOptions) purchases.PaymentPage {
	t.Helper()
	page, err := repo.ListPage(context.Background(), postingOrg, document, options)
	if err != nil {
		t.Fatal(err)
	}
	response := &tammyv1.ListPurchasePaymentsResponse{Payments: page.Payments, Page: &tammyv1.PageInfo{ReturnedCount: uint32(len(page.Payments))}}
	if page.NextPosition != "" {
		token, err := options.EncodeCursor(page.NextPosition)
		if err != nil {
			t.Fatal(err)
		}
		response.Page.NextCursor = &token
	}
	frame, err := anypb.New(response)
	if err != nil || proto.Size(frame) > 4*1024*1024 {
		t.Fatalf("actual payment response Any frame size=%d error=%v", proto.Size(frame), err)
	}
	return page
}

func paymentPageFixture(t *testing.T, executor app.CommandSQLExecutor, gross int64) (*postingFixture, *purchases.Repository) {
	t.Helper()
	f := newPostingFixture(t, executor, tammyv1.GstBasis_GST_BASIS_CASH)
	f.addDocument("History supplier", "PAY-HISTORY", gross, 0)
	repo := f.repo(executor)
	if _, err := repo.PostReviewed(context.Background(), f.request); err != nil {
		t.Fatal(err)
	}
	f.now = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	return f, repo
}

func TestPaymentPageAll205PaymentsReachableWithTiedTimesAndAllRowBalance(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f, repo := paymentPageFixture(t, tx, 205)
	retained := make([]*tammyv1.PurchasePayment, 0, 205)
	for i := 0; i < 205; i++ {
		response, err := repo.RecordPayment(context.Background(), paymentRequest(t, f, uint64(i+1), 1, paymentDate(10, 2)))
		if err != nil {
			t.Fatalf("payment %d: %v", i, err)
		}
		retained = append(retained, response.Payment)
	}
	options := paymentPageOptions(t, 100)
	seen := map[string]bool{}
	offset := len(retained) - 1
	for index, want := range []int{100, 100, 5} {
		page := assertPaymentPage(t, repo, f.document.Id, options)
		if len(page.Payments) != want {
			t.Fatalf("page %d count=%d want=%d", index, len(page.Payments), want)
		}
		for _, payment := range page.Payments {
			if seen[payment.Id] || !proto.Equal(payment, retained[offset]) {
				t.Fatal("tied timestamp history skipped/repeated/changed a source")
			}
			seen[payment.Id] = true
			offset--
		}
		if (page.NextPosition != "") != (index < 2) {
			t.Fatal("lookahead produced wrong continuation")
		}
		options.Position = page.NextPosition
		state, err := repo.Get(context.Background(), postingOrg, f.document.Id)
		if err != nil || state.Paid.MinorUnits != 205 || state.Outstanding.MinorUnits != 0 || state.PaymentCount != 205 || state.AccountingVersion != 206 {
			t.Fatalf("page affected all-row balance: %v %v", state, err)
		}
	}
	if len(seen) != 205 || paymentBalance(t, tx, "1000") != -205 || paymentBalance(t, tx, "2000") != 0 {
		t.Fatal("history/full settlement conservation failed")
	}
	before := postScalar(t, tx, `SELECT count(*) FROM financial_revision_claims`)
	if _, err := repo.RecordPayment(context.Background(), paymentRequest(t, f, 206, 1, paymentDate(10, 2))); err == nil {
		t.Fatal("fully settled source accepted overpayment")
	}
	if postScalar(t, tx, `SELECT count(*) FROM financial_revision_claims`) != before {
		t.Fatal("overpayment mutated revision")
	}
}

func TestPaymentPageChronologicalNanosecondsAndRetainedAnchors(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f, repo := paymentPageFixture(t, tx, 10)
	instant := f.now
	var retained []*tammyv1.PurchasePayment
	for i, delta := range []time.Duration{0, 100 * time.Millisecond, 100*time.Millisecond + time.Nanosecond, 110 * time.Millisecond} {
		f.now = instant.Add(delta)
		response, err := repo.RecordPayment(context.Background(), paymentRequest(t, f, uint64(i+1), 1, paymentDate(10, 2)))
		if err != nil {
			t.Fatal(err)
		}
		retained = append(retained, response.Payment)
	}
	options := paymentPageOptions(t, 3)
	first := assertPaymentPage(t, repo, f.document.Id, options)
	if len(first.Payments) != 3 || first.NextPosition == "" {
		t.Fatal("fractional first page missing")
	}
	for i, payment := range first.Payments {
		if !proto.Equal(payment, retained[3-i]) {
			t.Fatal("raw RFC3339Nano text was used as chronological order")
		}
	}
	f.now = instant.Add(110*time.Millisecond + time.Nanosecond)
	newer, err := repo.RecordPayment(context.Background(), paymentRequest(t, f, 5, 1, paymentDate(10, 2)))
	if err != nil {
		t.Fatal(err)
	}
	options.Position = first.NextPosition
	last := assertPaymentPage(t, repo, f.document.Id, options)
	if len(last.Payments) != 1 || !proto.Equal(last.Payments[0], retained[0]) || last.NextPosition != "" {
		t.Fatal("new payment changed retained continuation")
	}
	options.Position = ""
	refreshed := assertPaymentPage(t, repo, f.document.Id, options)
	if !proto.Equal(refreshed.Payments[0], newer.Payment) {
		t.Fatal("refresh did not include later nanosecond")
	}
}

func TestPaymentPageRejectsUnboundMissingAndChangedAnchorsAndInvalidLimits(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f, repo := paymentPageFixture(t, tx, 10)
	for i := 0; i < 3; i++ {
		if _, err := repo.RecordPayment(context.Background(), paymentRequest(t, f, uint64(i+1), 1, paymentDate(10, 2))); err != nil {
			t.Fatal(err)
		}
	}
	options := paymentPageOptions(t, 1)
	first := assertPaymentPage(t, repo, f.document.Id, options)
	for _, test := range []struct {
		name     string
		limit    int
		position string
		encoder  func(string) (string, error)
	}{
		{"zero limit", 0, "", options.EncodeCursor}, {"negative limit", -1, "", options.EncodeCursor}, {"overflow limit", 201, "", options.EncodeCursor}, {"malformed", 1, "invalid", options.EncodeCursor}, {"padding", 1, first.NextPosition + "=", options.EncodeCursor}, {"missing encoder", 1, "", nil}, {"encoder failure", 1, "", func(string) (string, error) { return "", errors.New("sign failure") }}, {"empty encoder", 1, "", func(string) (string, error) { return "", nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := purchases.PageOptions{Position: test.position, Limit: test.limit, EncodeCursor: test.encoder}
			if _, err := repo.ListPage(context.Background(), postingOrg, f.document.Id, request); err == nil {
				t.Fatal("invalid page accepted")
			}
		})
	}
	options.Position = first.NextPosition
	otherDoc := f.document.Id
	f.addDocument("Other supplier", "OTHER", 10, 0)
	if _, err := repo.PostReviewed(context.Background(), f.request); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ListPage(context.Background(), postingOrg, f.document.Id, options); err == nil {
		t.Fatal("another posting anchor accepted")
	}
	if _, err := repo.ListPage(context.Background(), "018f0000-0000-7000-8000-000000009998", otherDoc, options); err == nil {
		t.Fatal("foreign organisation anchor accepted")
	}
	raw, err := base64.RawURLEncoding.DecodeString(first.NextPosition)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(string(raw), first.Payments[0].Id, "018f0000-0000-7000-8000-000000009997", 1)
	options.Position = base64.RawURLEncoding.EncodeToString([]byte(changed))
	if _, err := repo.ListPage(context.Background(), postingOrg, otherDoc, options); err == nil {
		t.Fatal("missing retained anchor accepted")
	}
	if _, err := tx.ExecContext(context.Background(), `DROP TRIGGER purchase_payments_v1_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(context.Background(), `UPDATE purchase_payments_v1 SET created_at='2026-10-07T12:00:01Z' WHERE id=?`, first.Payments[0].Id); err != nil {
		t.Fatal(err)
	}
	options.Position = first.NextPosition
	if _, err := repo.ListPage(context.Background(), postingOrg, otherDoc, options); err == nil {
		t.Fatal("changed retained anchor accepted")
	}
}

func TestPaymentPageByteBudgetIncludesActualCursorAndAnyFrame(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f, repo := paymentPageFixture(t, tx, 10)
	for i := 0; i < 3; i++ {
		if _, err := repo.RecordPayment(context.Background(), paymentRequest(t, f, uint64(i+1), 1, paymentDate(10, 2))); err != nil {
			t.Fatal(err)
		}
	}
	options := paymentPageOptions(t, 3)
	ordinary := assertPaymentPage(t, repo, f.document.Id, options)
	if len(ordinary.Payments) != 3 || ordinary.NextPosition != "" {
		t.Fatal("terminal page added a needless token")
	}
	options.Limit = 2
	options.EncodeCursor = func(string) (string, error) {
		// The ordinary page above uses the real signing codec. This adversarial
		// adapter result establishes that the response counts returned token
		// bytes, including its field tags and the outer protobuf Any frame.
		return strings.Repeat("x", 4*1024*1024), nil
	}
	if _, err := repo.ListPage(context.Background(), postingOrg, f.document.Id, options); err == nil {
		t.Fatal("page accepted an actual signed token beyond frame budget")
	}
}

func TestPurchasePaymentMaximumExactAmountAndStaleFinalCommand(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	ctx := context.Background()
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f, repo := paymentPageFixture(t, tx, math.MaxInt64)
	first, err := repo.RecordPayment(ctx, paymentRequest(t, f, 1, math.MaxInt64-1, paymentDate(9, 30)))
	if err != nil {
		t.Fatal(err)
	}
	if first.Accounting.Outstanding.MinorUnits != 1 || first.Payment.Allocations[0].Gross.MinorUnits != math.MaxInt64-1 {
		t.Fatal("large exact payment rounded/overflowed")
	}
	final := paymentRequest(t, f, 2, 1, paymentDate(10, 2))
	stale := paymentRequest(t, f, 2, 1, paymentDate(10, 2))
	last, err := repo.RecordPayment(ctx, final)
	if err != nil {
		t.Fatal(err)
	}
	if last.Accounting.Paid.MinorUnits != math.MaxInt64 || last.Accounting.Outstanding.MinorUnits != 0 || paymentBalance(t, tx, "1000") != -math.MaxInt64 {
		t.Fatal("maximum settlement did not conserve exact amount")
	}
	before := postScalar(t, tx, `SELECT count(*) FROM financial_revision_claims`)
	if _, err := repo.RecordPayment(ctx, stale); !errors.Is(err, purchases.ErrConflict) {
		t.Fatalf("serialized stale final command=%v", err)
	}
	if postScalar(t, tx, `SELECT count(*) FROM financial_revision_claims`) != before {
		t.Fatal("stale final payment reserved revision")
	}
}

func TestPaymentPageExactAnyWireLimitAndOneByteOverflow(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 12)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f, repo := paymentPageFixture(t, tx, 10)
	for i := 0; i < 2; i++ {
		if _, err := repo.RecordPayment(ctx, paymentRequest(t, f, uint64(i+1), 1, paymentDate(10, 2))); err != nil {
			t.Fatal(err)
		}
	}
	options := paymentPageOptions(t, 1)
	ordinary := assertPaymentPage(t, repo, f.document.Id, options)
	wireSize := func(length int) int {
		token := strings.Repeat("x", length)
		frame, err := anypb.New(&tammyv1.ListPurchasePaymentsResponse{Payments: ordinary.Payments, Page: &tammyv1.PageInfo{ReturnedCount: 1, NextCursor: &token}})
		if err != nil {
			t.Fatal(err)
		}
		return proto.Size(frame)
	}
	low, high := 0, 4*1024*1024
	for low < high {
		mid := low + (high-low+1)/2
		if wireSize(mid) <= 4*1024*1024 {
			low = mid
		} else {
			high = mid - 1
		}
	}
	if wireSize(low) != 4*1024*1024 || wireSize(low+1) != 4*1024*1024+1 {
		t.Fatal("fixture did not establish exact wire boundary")
	}
	options.EncodeCursor = func(string) (string, error) { return strings.Repeat("x", low), nil }
	accepted := assertPaymentPage(t, repo, f.document.Id, options)
	if len(accepted.Payments) != 1 || accepted.NextPosition == "" {
		t.Fatal("exact-limit response rejected")
	}
	options.EncodeCursor = func(string) (string, error) { return strings.Repeat("x", low+1), nil }
	if _, err := repo.ListPage(ctx, postingOrg, f.document.Id, options); err == nil {
		t.Fatal("one-byte wire overflow accepted")
	}
}
