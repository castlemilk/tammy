//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package reporting

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	validate "buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"buf.build/go/protovalidate"
	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

func TestPurchaseBASLegacyCandidatesAreCompleteOutsideReconciled1B(t *testing.T) {
	for _, basis := range []tammyv1.GstBasis{tammyv1.GstBasis_GST_BASIS_CASH, tammyv1.GstBasis_GST_BASIS_NON_CASH} {
		t.Run(basis.String(), func(t *testing.T) {
			tx, repo := reportFixture(t, basis)
			for i := 0; i < 201; i++ {
				addReportPurchase(t, tx, 100+i, "2026-09-01", 110, 10)
			}
			addReportPurchase(t, tx, 500, "2026-10-01", 220, 20)
			// Reviewed undated legacy intake remains discoverable; no date is invented.
			reportExec(t, tx, `DROP TRIGGER documents_review_transition`)
			reportExec(t, tx, `UPDATE documents SET document_date=NULL WHERE id=?`, reportID(500))
			w, err := repo.CreateBASDraft(context.Background(), reportID(9000), reportID(9001), reportOrgID, reportDate("2026-10-01"), reportDate("2026-12-31"), time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC))
			if err != nil {
				t.Fatal(err)
			}
			if w.GstCredits_1B.MinorUnits != 0 || w.GetLegacyCandidateGst().GetMinorUnits() != 2030 || len(w.PurchaseReviews) != 202 || len(w.Sources) != 0 {
				t.Fatalf("legacy coverage or reconciled credit incorrect: %v", w)
			}
			var undated int
			for _, row := range w.PurchaseReviews {
				if row.Kind == tammyv1.BasPurchaseReviewKind_BAS_PURCHASE_REVIEW_KIND_UNDATED_LEGACY_DOCUMENT {
					undated++
					if row.InvoiceDate != nil {
						t.Fatal("undated legacy date invented")
					}
				}
				if row.Status != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNRESOLVED_LEGACY || row.LegacySource.GetDocument() == nil || len(row.SourceHash) != 32 {
					t.Fatal("legacy decision provenance incomplete")
				}
			}
			if undated != 1 {
				t.Fatalf("undated reviews=%d", undated)
			}
			loaded, err := repo.GetCurrentBASDraft(context.Background(), reportOrgID)
			if err != nil {
				t.Fatal(err)
			}
			if loaded.SourcesChanged || !proto.Equal(&tammyv1.BasWorkpaper{PurchaseReviews: w.PurchaseReviews, LegacyCandidateGst: w.LegacyCandidateGst}, &tammyv1.BasWorkpaper{PurchaseReviews: loaded.PurchaseReviews, LegacyCandidateGst: loaded.LegacyCandidateGst}) {
				t.Fatal("complete saved legacy review did not reload exactly")
			}
		})
	}
}

func TestPurchaseBASLegacySourceOverflowHasNoPartialSave(t *testing.T) {
	tx, repo := reportFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	for i := 0; i < 1001; i++ {
		addReportPurchase(t, tx, 100+i, "2026-09-01", 110, 10)
	}
	_, err := repo.CreateBASDraft(context.Background(), reportID(9000), reportID(9001), reportOrgID, reportDate("2026-10-01"), reportDate("2026-12-31"), time.Now().UTC())
	if !errors.Is(err, ErrSourceBound) {
		t.Fatalf("complete legacy overflow silently truncated: %v", err)
	}
	if got := purchaseSavedCount(t, tx); got != 0 {
		t.Fatalf("overflow saved %d partial workpapers", got)
	}
}

func TestPurchaseBASExact1000LegacyReviewLimitSavesAndReloadsAllRows(t *testing.T) {
	tx, repo := reportFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	for i := 0; i < 1000; i++ {
		addReportPurchase(t, tx, 100+i, "2026-09-01", 110, 10)
	}
	w, err := repo.CreateBASDraft(context.Background(), reportID(9000), reportID(9001), reportOrgID, reportDate("2026-10-01"), reportDate("2026-12-31"), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(w.PurchaseReviews) != 1000 || w.LegacyCandidateGst.MinorUnits != 10000 || w.GstCredits_1B.MinorUnits != 0 {
		t.Fatal("exact review capacity lost complete candidates")
	}
	loaded, err := repo.GetCurrentBASDraft(context.Background(), reportOrgID)
	if err != nil || !proto.Equal(w, loaded) {
		t.Fatalf("exact limit did not reload complete snapshot: %v", err)
	}
}

func TestPurchaseBASSourceReviewAndSupportOverflowFailBeforeAnySave(t *testing.T) {
	for _, collection := range []string{"sources", "reviews", "support"} {
		t.Run(collection, func(t *testing.T) {
			tx, _ := reportFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
			snapshot := &purchases.BASSnapshot{LegacyCandidateGST: aud(0), Fingerprint: bytes.Repeat([]byte{42}, 32)}
			for i := 0; i < 1001; i++ {
				switch collection {
				case "sources":
					snapshot.Sources = append(snapshot.Sources, &tammyv1.BasPurchaseSourceLine{ClaimedGst: aud(0)})
				case "reviews":
					snapshot.Reviews = append(snapshot.Reviews, &tammyv1.BasPurchaseReview{})
				case "support":
					snapshot.LegacySupport = append(snapshot.LegacySupport, &tammyv1.BasPurchaseLegacySupport{})
				}
			}
			repo, err := NewRepository(tx, maximalPurchasePort{snapshot})
			if err != nil {
				t.Fatal(err)
			}
			_, err = repo.CreateBASDraft(context.Background(), reportID(9000), reportID(9001), reportOrgID, reportDate("2026-10-01"), reportDate("2026-12-31"), time.Now().UTC())
			if !errors.Is(err, ErrSourceBound) || purchaseSavedCount(t, tx) != 0 {
				t.Fatalf("%s overflow wrote a partial draft: %v", collection, err)
			}
		})
	}
}

func TestPurchaseBASSavedSnapshotRejectsInvalidMoneyEvenWhenRehashed(t *testing.T) {
	tx, repo := reportFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	addReportPurchase(t, tx, 100, "2026-09-01", 110, 10)
	w, err := repo.CreateBASDraft(context.Background(), reportID(9000), reportID(9001), reportOrgID, reportDate("2026-10-01"), reportDate("2026-12-31"), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	row := proto.Clone(w.PurchaseReviews[0]).(*tammyv1.BasPurchaseReview)
	row.UnclaimedGst.CurrencyCode = "CAD"
	content, err := proto.MarshalOptions{Deterministic: true}.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(content)
	reportExec(t, tx, `DROP TRIGGER bas_workpaper_purchase_reviews_v1_no_update`)
	reportExec(t, tx, `UPDATE bas_workpaper_purchase_reviews_v1 SET snapshot_proto=?,source_hash=? WHERE workpaper_id=?`, content, hash[:], w.Id)
	_, err = repo.GetCurrentBASDraft(context.Background(), reportOrgID)
	if !errors.Is(err, ErrRepository) {
		t.Fatalf("self-rehashed invalid retained money was served: %v", err)
	}
}

func TestPurchaseBASInvalidSnapshotMoneyFailsBeforeAnySave(t *testing.T) {
	tx, base := reportFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	addReportPurchase(t, tx, 100, "2026-09-01", 110, 10)
	period := &tammyv1.PurchaseClaimPeriod{PeriodStart: reportDate("2026-10-01"), PeriodEnd: reportDate("2026-12-31")}
	snapshot, err := base.purchasePort.CaptureBAS(context.Background(), reportOrgID, period, period.PeriodEnd, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Reviews[0].UnclaimedGst.CurrencyCode = "CAD"
	repo, err := NewRepository(tx, maximalPurchasePort{snapshot})
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.CreateBASDraft(context.Background(), reportID(9000), reportID(9001), reportOrgID, period.PeriodStart, period.PeriodEnd, time.Now().UTC())
	if !errors.Is(err, ErrRepository) || purchaseSavedCount(t, tx) != 0 {
		t.Fatalf("invalid money admitted a saved snapshot: %v", err)
	}
}

func purchaseSavedCount(t *testing.T, tx app.CommandSQLExecutor) int64 {
	t.Helper()
	var count int64
	if querySingle(tx, context.Background(), `SELECT count(*) FROM bas_workpapers`, nil, &count) != nil {
		t.Fatal("read saved workpaper count")
	}
	return count
}

type maximalPurchasePort struct{ snapshot *purchases.BASSnapshot }

func (p maximalPurchasePort) CaptureBAS(context.Context, string, *tammyv1.PurchaseClaimPeriod, *tammyv1.CivilDate, time.Time) (*purchases.BASSnapshot, error) {
	return p.snapshot, nil
}

func TestPurchaseBASMaximalUnicodeSnapshotFailsBeforeSave(t *testing.T) {
	fixture := purchaseMaximalFixture(t, (&tammyv1.BasWorkpaper{}).ProtoReflect().Descriptor())
	if err := protovalidate.Validate(fixture); err != nil {
		t.Fatalf("item-valid maximal fixture rejected: %v", err)
	}
	older := proto.Clone(fixture).ProtoReflect()
	for i := 0; i < older.Get(older.Descriptor().Fields().ByName("purchase_sources")).List().Len(); i++ {
		line := older.Get(older.Descriptor().Fields().ByName("purchase_sources")).List().Get(i).Message()
		for _, name := range []protoreflect.Name{"account_id", "account_version", "supplier_name", "invoice_number", "release_posting_date"} {
			line.Clear(line.Descriptor().Fields().ByName(name))
		}
	}
	reviews := older.Get(older.Descriptor().Fields().ByName("purchase_reviews")).List()
	for i := 0; i < reviews.Len(); i++ {
		row := reviews.Get(i).Message()
		row.Clear(row.Descriptor().Fields().ByName("document_id"))
	}
	if size := proto.Size(older.Interface()); size != 13060275 {
		t.Fatalf("original Task-one maximal Unicode bytes changed: %d", size)
	}
	if size := proto.Size(fixture); size <= 4*1024*1024 || size > 16*1024*1024 {
		t.Fatalf("additive maximal fixture is outside intended RPC/export boundaries: %d", size)
	}
	t.Logf("maximal Unicode snapshot including optional export provenance: %d encoded bytes", proto.Size(fixture))
	full := &tammyv1.BasWorkpaper{}
	encoded, _ := proto.Marshal(fixture)
	if proto.Unmarshal(encoded, full) != nil {
		t.Fatal("decode maximal fixture")
	}
	if basResponseFits(full) {
		t.Fatal("actual complete maximal workpaper crossed 4MiB response limit")
	}
	tx, _ := reportFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	// Keep financial summary arithmetic valid, leaving every complete retained
	// purchase fact and maximum Unicode field for the actual byte guard.
	for _, line := range full.PurchaseSources {
		line.ClaimedGst = aud(0)
	}
	port := maximalPurchasePort{&purchases.BASSnapshot{Sources: full.PurchaseSources, Reviews: full.PurchaseReviews, LegacySupport: full.LegacyPurchaseSupport, LegacyCandidateGST: aud(0), Fingerprint: bytes.Repeat([]byte{42}, 32)}}
	repo, err := NewRepository(tx, port)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.CreateBASDraft(context.Background(), reportID(9000), reportID(9001), reportOrgID, reportDate("2026-10-01"), reportDate("2026-12-31"), time.Now().UTC())
	if !errors.Is(err, ErrSourceBound) {
		t.Fatalf("oversized complete purchase snapshot bypassed response byte guard: %v", err)
	}
	if got := purchaseSavedCount(t, tx); got != 0 {
		t.Fatalf("byte overflow saved %d partial workpapers", got)
	}
}

// Same generated-descriptor fixture as Task one's contract test: all optional
// facts, every collection at its documented cap, maximum four-byte Unicode.
func purchaseBaseFixture(d protoreflect.MessageDescriptor) *dynamicpb.Message {
	m := dynamicpb.NewMessage(d)
	for i := 0; i < d.Fields().Len(); i++ {
		f := d.Fields().Get(i)
		if f.HasOptionalKeyword() && !(d.Name() == "PurchaseAccounting" && f.Name() == "posting") {
			continue
		}
		if o := f.ContainingOneof(); o != nil && f != o.Fields().Get(0) {
			continue
		}
		var v protoreflect.Value
		switch f.Kind() {
		case protoreflect.MessageKind:
			v = protoreflect.ValueOfMessage(purchaseBaseFixture(f.Message()))
		case protoreflect.StringKind:
			s := "reviewed supporting facts"
			switch string(f.Name()) {
			case "currency_code":
				s = "AUD"
			case "purpose", "type":
				s = "purchase_review"
			default:
				if strings.HasSuffix(string(f.Name()), "_id") || f.Name() == "id" || f.Name() == "idempotency_key" {
					s = "018f0c4a-7b9d-7abc-8def-0123456789ab"
				}
			}
			v = protoreflect.ValueOfString(s)
		case protoreflect.BytesKind:
			v = protoreflect.ValueOfBytes(bytes.Repeat([]byte{42}, 32))
		case protoreflect.EnumKind:
			v = protoreflect.ValueOfEnum(f.Enum().Values().Get(1).Number())
		case protoreflect.BoolKind:
			v = protoreflect.ValueOfBool(true)
		case protoreflect.Int32Kind:
			n := int32(1)
			switch f.Name() {
			case "year":
				n = 2024
			case "month":
				n = 2
			case "day":
				n = 29
			}
			v = protoreflect.ValueOfInt32(n)
		case protoreflect.Int64Kind:
			v = protoreflect.ValueOfInt64(1)
		case protoreflect.Uint32Kind:
			v = protoreflect.ValueOfUint32(1)
		case protoreflect.Uint64Kind:
			v = protoreflect.ValueOfUint64(1)
		default:
			panic(fmt.Sprintf("unhandled purchase field %s", f.FullName()))
		}
		if f.IsList() {
			m.Mutable(f).List().Append(v)
			if d.Name() == "Journal" && f.Name() == "lines" {
				m.Mutable(f).List().Append(protoreflect.ValueOfMessage(purchaseBaseFixture(f.Message())))
			}
		} else {
			m.Set(f, v)
		}
	}
	return m
}
func purchaseMaximalFixture(t *testing.T, d protoreflect.MessageDescriptor) *dynamicpb.Message {
	t.Helper()
	m := purchaseBaseFixture(d)
	for i := 0; i < d.Fields().Len(); i++ {
		f := d.Fields().Get(i)
		if o := f.ContainingOneof(); o != nil && f != o.Fields().Get(0) {
			continue
		}
		rules := &validate.FieldRules{}
		options := f.Options().(*descriptorpb.FieldOptions)
		if proto.HasExtension(options, validate.E_Field) {
			rules = proto.GetExtension(options, validate.E_Field).(*validate.FieldRules)
		}
		if f.IsList() {
			list := m.Mutable(f).List()
			list.Truncate(0)
			count := int(rules.GetRepeated().GetMaxItems())
			for j := 0; j < count; j++ {
				if f.Kind() == protoreflect.MessageKind {
					list.Append(protoreflect.ValueOfMessage(purchaseMaximalFixture(t, f.Message())))
				} else {
					max := rules.GetRepeated().GetItems().GetString().GetMaxLen()
					list.Append(protoreflect.ValueOfString(strings.Repeat("😀", int(max))))
				}
			}
		} else if f.Kind() == protoreflect.MessageKind {
			m.Set(f, protoreflect.ValueOfMessage(purchaseMaximalFixture(t, f.Message())))
		} else if f.Kind() == protoreflect.StringKind {
			if max := rules.GetString().GetMaxLen(); max > 0 && rules.GetString().GetPattern() == "" {
				m.Set(f, protoreflect.ValueOfString(strings.Repeat("😀", int(max))))
			} else if f.HasOptionalKeyword() {
				m.Set(f, protoreflect.ValueOfString("018f0c4a-7b9d-7abc-8def-0123456789ab"))
			}
		} else if f.HasOptionalKeyword() {
			switch f.Kind() {
			case protoreflect.BytesKind:
				m.Set(f, protoreflect.ValueOfBytes(bytes.Repeat([]byte{42}, 32)))
			case protoreflect.EnumKind:
				m.Set(f, protoreflect.ValueOfEnum(f.Enum().Values().Get(1).Number()))
			case protoreflect.Uint64Kind:
				m.Set(f, protoreflect.ValueOfUint64(1))
			default:
				t.Fatalf("unhandled maximal optional %s", f.FullName())
			}
		} else if f.Kind() == protoreflect.Int64Kind && f.Name() == "minor_units" {
			m.Set(f, protoreflect.ValueOfInt64(9223372036854775807))
		}
	}
	return m
}
