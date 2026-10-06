package contracts_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	validate "buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"buf.build/go/protovalidate"
	_ "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

var purchaseMethods = []string{"GetPurchaseAccounting", "PostReviewedPurchase", "RecordPurchasePayment", "ListPurchasePayments", "RecordPurchaseTaxEvidence", "AttributePurchaseGst", "DeferPurchaseGstClaim", "ResolveLegacyPurchaseReview"}

func purchaseDescriptor(t *testing.T, name string) protoreflect.MessageDescriptor {
	t.Helper()
	d, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName("tammy.v1." + name))
	if err != nil {
		t.Fatalf("purchase contract missing %s: %v", name, err)
	}
	return d.(protoreflect.MessageDescriptor)
}

// Populate real generated descriptors with deterministic, valid retained facts.
func purchaseFixture(d protoreflect.MessageDescriptor) *dynamicpb.Message {
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
			v = protoreflect.ValueOfMessage(purchaseFixture(f.Message()))
		case protoreflect.StringKind:
			s := "reviewed supporting facts"
			switch string(f.Name()) {
			case "currency_code":
				s = "AUD"
			case "purpose", "type":
				s = "purchase_review"
			default:
				if bytes.HasSuffix([]byte(f.Name()), []byte("_id")) || f.Name() == "id" || f.Name() == "idempotency_key" {
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
				m.Mutable(f).List().Append(protoreflect.ValueOfMessage(purchaseFixture(f.Message())))
			}
		} else {
			m.Set(f, v)
		}
	}
	return m
}

func TestPurchaseEightRPCDescriptorsAndRoundTrips(t *testing.T) {
	file, err := protoregistry.GlobalFiles.FindFileByPath("tammy/v1/purchases.proto")
	if err != nil {
		t.Fatalf("missing PurchaseService descriptor: %v", err)
	}
	s := file.Services().ByName("PurchaseService")
	if s == nil || s.Methods().Len() != 8 {
		t.Fatal("PurchaseService must expose exactly eight unary methods")
	}
	for i, name := range purchaseMethods {
		method := s.Methods().Get(i)
		if string(method.Name()) != name || method.IsStreamingClient() || method.IsStreamingServer() {
			t.Fatalf("unexpected purchase RPC %s", method.FullName())
		}
		for _, suffix := range []string{"Request", "Response"} {
			t.Run(name+suffix, func(t *testing.T) {
				d := purchaseDescriptor(t, name+suffix)
				m := purchaseFixture(d)
				if err := protovalidate.Validate(m); err != nil {
					t.Fatalf("valid retained facts rejected: %v", err)
				}
				b, err := proto.Marshal(m)
				if err != nil {
					t.Fatal(err)
				}
				out := dynamicpb.NewMessage(d)
				if err := proto.Unmarshal(b, out); err != nil {
					t.Fatal(err)
				}
				if !proto.Equal(m, out) {
					t.Fatal("purchase facts lost in binary roundtrip")
				}
			})
		}
	}
}

func TestPurchaseRejectsMissingAndUnknownReviewedDecisions(t *testing.T) {
	for _, name := range []string{"PurchaseAllocationInput", "PurchaseEvidenceInput", "PurchaseTaxInvoiceExceptionReview", "PurchaseDuplicateReview", "PurchasePriorAccountingReview", "PurchaseClaimTimingReview", "LegacyPurchaseReviewInput"} {
		d := purchaseDescriptor(t, name)
		if err := protovalidate.Validate(purchaseFixture(d)); err != nil {
			t.Fatalf("invalid test baseline for %s: %v", name, err)
		}
		for i := 0; i < d.Fields().Len(); i++ {
			f := d.Fields().Get(i)
			if f.Kind() != protoreflect.EnumKind && f.Kind() != protoreflect.BoolKind {
				continue
			}
			t.Run(string(f.FullName()), func(t *testing.T) {
				for _, unknown := range []bool{false, true} {
					m := purchaseFixture(d)
					if f.Kind() == protoreflect.EnumKind {
						v := protoreflect.EnumNumber(0)
						if unknown {
							v = 999
						}
						m.Set(f, protoreflect.ValueOfEnum(v))
					} else {
						m.Set(f, protoreflect.ValueOfBool(false))
					}
					if err := protovalidate.Validate(m); err == nil {
						t.Fatal("missing or unknown explicit review accepted")
					}
				}
			})
		}
	}
}

func TestPurchaseDelayedClaimRejectsMechanicalTimingViolations(t *testing.T) {
	d := purchaseDescriptor(t, "AttributePurchaseGstRequest")
	for _, name := range []string{"normal_event_date", "evidence_held_date"} {
		m := purchaseFixture(d)
		portion := m.Get(d.Fields().ByName("portions")).List().Get(0).Message()
		date := portion.Mutable(portion.Descriptor().Fields().ByName(protoreflect.Name(name))).Message()
		date.Set(date.Descriptor().Fields().ByName("year"), protoreflect.ValueOfInt32(2028))
		if err := protovalidate.Validate(m); err == nil {
			t.Errorf("mechanically impossible %s accepted despite explicit confirmations", name)
		}
	}
	for _, name := range []string{"required_return_due_date", "due_date_evidence", "ordinary_last_claim_date"} {
		m := purchaseFixture(d)
		portion := m.Get(d.Fields().ByName("portions")).List().Get(0).Message()
		portion.Clear(portion.Descriptor().Fields().ByName(protoreflect.Name(name)))
		if err := protovalidate.Validate(m); err == nil {
			t.Errorf("late claim accepted without retained %s", name)
		}
	}
}

func TestPurchaseUnpostedAccountingAndExactInt64Money(t *testing.T) {
	d := purchaseDescriptor(t, "PurchaseAccounting")
	m := purchaseFixture(d)
	m.Set(d.Fields().ByName("posted"), protoreflect.ValueOfBool(false))
	m.Clear(d.Fields().ByName("posting"))
	m.Set(d.Fields().ByName("accounting_version"), protoreflect.ValueOfUint64(0))
	m.Set(d.Fields().ByName("payment_count"), protoreflect.ValueOfUint64(0))
	if err := protovalidate.Validate(m); err != nil {
		t.Fatalf("unposted shallow state rejected: %v", err)
	}
	payment := purchaseFixture(purchaseDescriptor(t, "RecordPurchasePaymentRequest"))
	amount := payment.Mutable(payment.Descriptor().Fields().ByName("amount")).Message()
	amount.Set(amount.Descriptor().Fields().ByName("minor_units"), protoreflect.ValueOfInt64(9223372036854775807))
	if err := protovalidate.Validate(payment); err != nil {
		t.Fatalf("invented money ceiling rejected valid int64: %v", err)
	}
	encoded, err := proto.Marshal(payment)
	if err != nil {
		t.Fatal(err)
	}
	decoded := dynamicpb.NewMessage(payment.Descriptor())
	if err := proto.Unmarshal(encoded, decoded); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(payment, decoded) {
		t.Fatal("int64 precision lost")
	}
}

func TestPurchaseFreshKnownBasisHasZeroHistoryWatermark(t *testing.T) {
	d := purchaseDescriptor(t, "PurchasePosting")
	m := purchaseFixture(d)
	m.Set(d.Fields().ByName("gst_history_watermark"), protoreflect.ValueOfUint64(0))
	if err := protovalidate.Validate(m); err != nil {
		t.Fatalf("fresh known-basis profile version 1 must not invent an audit history row: %v", err)
	}
}

func TestPurchaseZeroSupplierGSTAllowsUnspecifiedBasisOnly(t *testing.T) {
	d := purchaseDescriptor(t, "PurchasePosting")
	for _, supplierGST := range []int64{0, 1} {
		m := purchaseFixture(d)
		m.Set(d.Fields().ByName("gst_basis"), protoreflect.ValueOfEnum(0))
		for _, name := range []string{"supplier_gst", "eligible_gst"} {
			n := int64(0)
			if name == "supplier_gst" {
				n = supplierGST
			}
			money := m.Mutable(d.Fields().ByName(protoreflect.Name(name))).Message()
			money.Set(money.Descriptor().Fields().ByName("minor_units"), protoreflect.ValueOfInt64(n))
			allocation := m.Get(d.Fields().ByName("allocations")).List().Get(0).Message()
			allocated := allocation.Mutable(allocation.Descriptor().Fields().ByName(protoreflect.Name(name))).Message()
			allocated.Set(allocated.Descriptor().Fields().ByName("minor_units"), protoreflect.ValueOfInt64(n))
		}
		m.Set(d.Fields().ByName("gst_history_watermark"), protoreflect.ValueOfUint64(0))
		err := protovalidate.Validate(m)
		if supplierGST == 0 && err != nil {
			t.Fatalf("zero supplier GST must allow unspecified basis and genuine fresh history: %v", err)
		}
		if supplierGST > 0 && err == nil {
			t.Fatal("positive supplier GST needs a reviewed basis even when eligible GST is zero")
		}
	}
}

func TestPurchaseBASFullThousandRowProvenanceHasLinearSize(t *testing.T) {
	d := purchaseDescriptor(t, "BasWorkpaper")
	m := purchaseFixture(d)
	reviews := m.Mutable(d.Fields().ByName("purchase_reviews")).List()
	reviews.Truncate(0)
	for i := 0; i < 1000; i++ {
		review := purchaseFixture(d.Fields().ByName("purchase_reviews").Message())
		f := review.Descriptor().Fields().ByName("future_claim_obligation")
		future := purchaseFixture(f.Message())
		// Reproduce the previous multiplication if the old full deferral shape returns.
		if portions := future.Descriptor().Fields().ByName("portions"); portions != nil {
			list := future.Mutable(portions).List()
			for list.Len() < 200 {
				list.Append(protoreflect.ValueOfMessage(purchaseFixture(portions.Message())))
			}
		}
		review.Set(f, protoreflect.ValueOfMessage(future))
		reviews.Append(protoreflect.ValueOfMessage(review))
	}
	sources := m.Mutable(d.Fields().ByName("purchase_sources")).List()
	for sources.Len() < 1000 {
		sources.Append(protoreflect.ValueOfMessage(purchaseFixture(d.Fields().ByName("purchase_sources").Message())))
	}
	if err := protovalidate.Validate(m); err != nil {
		t.Fatalf("complete 1000-row sparse provenance must remain valid: %v", err)
	}
	actual := proto.Size(m)
	t.Logf("1000 purchase sources + 1000 individual future-claim reviews: %d encoded bytes", actual)
	if actual > 4*1024*1024 {
		t.Fatalf("sparse full-count provenance exceeds actual 4MiB transport guard: %d bytes", actual)
	}
	if actual > 16*1024*1024 {
		t.Fatalf("provenance exceeds 16MiB export guard: %d bytes", actual)
	}
	if got := d.Fields().ByName("purchase_reviews").Message().Fields().ByName("future_claim_obligation").Message().Name(); got != "BasPurchaseFutureClaim" {
		t.Fatalf("future claim must retain one portion, not nested command history: %s", got)
	}
}

func TestPurchaseBASSavedLegacySupportingFactsHaveGlobalBound(t *testing.T) {
	d := purchaseDescriptor(t, "BasWorkpaper")
	f := d.Fields().ByName("legacy_purchase_support")
	if f == nil {
		t.Fatal("saved legacy supporting facts need a globally bounded typed collection")
	}
	if f.Number() != 23 {
		t.Fatal("legacy support must append after existing workpaper fields")
	}
	m := purchaseFixture(d)
	list := m.Mutable(f).List()
	list.Truncate(0)
	for i := 0; i < 1000; i++ {
		list.Append(protoreflect.ValueOfMessage(purchaseFixture(f.Message())))
	}
	if err := protovalidate.Validate(m); err != nil {
		t.Fatalf("full 1000-row saved support must remain valid: %v", err)
	}
	list.Append(protoreflect.ValueOfMessage(purchaseFixture(f.Message())))
	if err := protovalidate.Validate(m); err == nil {
		t.Fatal("global nested supporting provenance overflow accepted")
	}
	legacy := purchaseDescriptor(t, "BasPurchaseLegacyReview")
	for i := 0; i < legacy.Fields().Len(); i++ {
		if legacy.Fields().Get(i).IsList() {
			t.Fatal("legacy review must not multiply supporting histories inside each review")
		}
	}
}

// Fill every permitted optional fact and collection, with four-byte UTF-8 at each
// bounded text field. Item caps are not substitutes for encoded-byte guards.
func purchaseMaxBASFixture(t *testing.T, d protoreflect.MessageDescriptor) *dynamicpb.Message {
	t.Helper()
	m := purchaseFixture(d)
	for i := 0; i < d.Fields().Len(); i++ {
		f := d.Fields().Get(i)
		if oneof := f.ContainingOneof(); oneof != nil && f != oneof.Fields().Get(0) {
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
					list.Append(protoreflect.ValueOfMessage(purchaseMaxBASFixture(t, f.Message())))
				} else {
					max := rules.GetRepeated().GetItems().GetString().GetMaxLen()
					list.Append(protoreflect.ValueOfString(strings.Repeat("😀", int(max))))
				}
			}
		} else if f.Kind() == protoreflect.MessageKind {
			m.Set(f, protoreflect.ValueOfMessage(purchaseMaxBASFixture(t, f.Message())))
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
			default:
				t.Fatalf("unhandled maximal BAS optional %s", f.FullName())
			}
		} else if f.Kind() == protoreflect.Int64Kind && f.Name() == "minor_units" {
			m.Set(f, protoreflect.ValueOfInt64(9223372036854775807))
		}
	}
	return m
}

func TestPurchaseBASMaximalUnicodeProvenanceRequiresActualByteGuard(t *testing.T) {
	m := purchaseMaxBASFixture(t, purchaseDescriptor(t, "BasWorkpaper"))
	if err := protovalidate.Validate(m); err != nil {
		t.Fatalf("schema-permitted complete maximal provenance rejected: %v", err)
	}
	actual := proto.Size(m)
	t.Logf("all collections at their caps, all optional retained facts, max four-byte Unicode text: %d encoded bytes", actual)
	if actual <= 4*1024*1024 {
		t.Fatal("boundary fixture must demonstrate an item-valid workpaper exceeding the 4MiB RPC/IPC budget")
	}
	if actual > 16*1024*1024 {
		t.Fatalf("linear saved provenance unexpectedly exceeds 16MiB before export rendering: %d", actual)
	}
	// Tasks10/13's source capture and bridge/core adapters must reject this exact
	// complete body using proto.Size >4MiB; Task15 separately enforces actual
	// rendered CSV/PDF output <=16MiB. Neither
	// boundary may drop rows or substitute export limits for RPC admission.
	for _, name := range []string{"purchase_sources", "purchase_reviews", "legacy_purchase_support"} {
		if m.Get(m.Descriptor().Fields().ByName(protoreflect.Name(name))).List().Len() != 1000 {
			t.Fatalf("maximal fixture lost complete %s", name)
		}
	}
}

func TestPurchaseBASOptionalSourceIdentitiesNeedTheirHashes(t *testing.T) {
	d := purchaseDescriptor(t, "BasPurchaseSourceLine")
	for _, name := range []string{"payment", "attribution"} {
		m := purchaseFixture(d)
		m.Set(d.Fields().ByName(protoreflect.Name(name+"_id")), protoreflect.ValueOfString("018f0c4a-7b9d-7abc-8def-0123456789ab"))
		if err := protovalidate.Validate(m); err == nil {
			t.Errorf("%s identity accepted without immutable source hash", name)
		}
	}
}

func TestPurchaseEveryRequiredRequestFactRejectsOmission(t *testing.T) {
	for _, method := range purchaseMethods {
		d := purchaseDescriptor(t, method+"Request")
		for i := 0; i < d.Fields().Len(); i++ {
			f := d.Fields().Get(i)
			if f.HasOptionalKeyword() {
				continue
			}
			if f.Kind() == protoreflect.StringKind && f.Name() == "reference" {
				continue
			}
			m := purchaseFixture(d)
			m.Clear(f)
			if err := protovalidate.Validate(m); err == nil {
				t.Errorf("missing request fact accepted: %s", f.FullName())
			}
		}
	}
}

func TestPurchaseMissingEvidenceAndSupportedExceptionFacts(t *testing.T) {
	d := purchaseDescriptor(t, "PurchaseEvidenceInput")
	unknown := purchaseFixture(d)
	unknown.Clear(d.Fields().ByName("source"))
	unknown.Clear(d.Fields().ByName("held_date"))
	if err := protovalidate.Validate(unknown); err != nil {
		t.Fatalf("unknown evidence must not invent a held date or source: %v", err)
	}
	classification := d.Fields().ByName("classification")
	for _, known := range []protoreflect.EnumNumber{2, 3} {
		m := proto.Clone(unknown).(*dynamicpb.Message)
		m.Set(classification, protoreflect.ValueOfEnum(known))
		if err := protovalidate.Validate(m); err == nil {
			t.Fatal("known evidence accepted without retained held date/source")
		}
	}
	exception := purchaseFixture(d)
	exception.Set(classification, protoreflect.ValueOfEnum(3))
	field := d.Fields().ByName("exception_review")
	if field == nil {
		t.Fatal("supported exception must carry typed reviewed facts and retained rule provenance")
	}
	exception.Set(d.Fields().ByName("source"), protoreflect.ValueOfMessage(purchaseFixture(d.Fields().ByName("source").Message())))
	exception.Set(d.Fields().ByName("held_date"), protoreflect.ValueOfMessage(purchaseFixture(d.Fields().ByName("held_date").Message())))
	if err := protovalidate.Validate(exception); err == nil {
		t.Fatal("exception accepted using only enum and reason")
	}
	exception.Set(field, protoreflect.ValueOfMessage(purchaseFixture(field.Message())))
	if err := protovalidate.Validate(exception); err != nil {
		t.Fatalf("supported evidenced exception rejected: %v", err)
	}
}

func TestPurchaseRetainedContractsAndLegacyBASShape(t *testing.T) {
	contracts := map[string][]string{
		"PurchasePosting":          {"id", "organisation_id", "document_id", "reviewed_version", "original_content_hash", "reviewed_snapshot_hash", "supplier_name", "invoice_number", "invoice_date", "gst_basis", "gst_profile_version", "gst_history_watermark", "gross", "supplier_gst", "eligible_gst", "allocations", "duplicate_review", "prior_accounting_review", "journal_id", "financial_revision", "actor_user_id", "posted_at", "source_hash"},
		"PurchaseGstSourcePortion": {"posting", "payment", "normal_event_date", "earliest_otherwise_eligible_period", "required_return_due_date", "due_date_evidence", "ordinary_last_claim_date", "evidence_id", "evidence_held_date"},
		"BasPurchaseSourceLine":    {"posting_id", "posting_hash", "document_id", "payment_id", "payment_hash", "attribution_id", "attribution_hash", "allocation_sequence", "use", "bas_classification", "cash_flow_classification", "invoice_date", "actual_payment_date", "normal_tax_event_date", "claim_period", "allocated_gross", "supplier_gst", "eligible_gst", "claimed_gst", "evidence", "source_revision", "source_hash"},
		"BasPurchaseReview":        {"kind", "legacy_source", "status", "affected_period", "legacy_review", "future_claim_obligation", "reason", "source_revision", "source_hash"},
	}
	for name, fields := range contracts {
		d := purchaseDescriptor(t, name)
		for _, name := range fields {
			if d.Fields().ByName(protoreflect.Name(name)) == nil {
				t.Errorf("missing retained contract field %s.%s", d.Name(), name)
			}
		}
	}
	bas := purchaseDescriptor(t, "BasWorkpaper")
	legacy := purchaseFixture(bas)
	for _, name := range []string{"purchase_sources", "purchase_reviews", "legacy_candidate_gst", "purchase_source_format", "legacy_purchase_support"} {
		legacy.Clear(bas.Fields().ByName(protoreflect.Name(name)))
	}
	if err := protovalidate.Validate(legacy); err != nil {
		t.Fatalf("legacy BAS shape rejected: %v", err)
	}
	want := []string{"id", "organisation_id", "version", "period_start", "period_end", "status", "sales_g1", "gst_on_sales_1a", "gst_credits_1b", "net_gst_payable", "sources", "created_at", "gst_basis", "sales_sources", "source_fingerprint", "sources_changed", "review_warnings", "excluded_cash_purchase_count"}
	for i, name := range want {
		if f := bas.Fields().ByNumber(protoreflect.FieldNumber(i + 1)); f == nil || string(f.Name()) != name {
			t.Fatalf("legacy BAS field %d changed", i+1)
		}
	}
	for _, name := range []string{"PostReviewedPurchaseRequest", "RecordPurchasePaymentRequest"} {
		d := purchaseDescriptor(t, name)
		for _, late := range []string{"required_return_due_date", "due_date_evidence", "ordinary_last_claim_date", "timing_review"} {
			if d.Fields().ByName(protoreflect.Name(late)) != nil {
				t.Errorf("ordinary request %s requires delayed claim detail %s", name, late)
			}
		}
	}
}

func TestPurchaseValidatesEveryNewDateIdentityHashAndBound(t *testing.T) {
	file, err := protoregistry.GlobalFiles.FindFileByPath("tammy/v1/purchases.proto")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < file.Messages().Len(); i++ {
		d := file.Messages().Get(i)
		for j := 0; j < d.Fields().Len(); j++ {
			f := d.Fields().Get(j)
			if f.IsList() {
				continue
			}
			if f.Kind() == protoreflect.MessageKind && f.Message().FullName() == "tammy.v1.CivilDate" {
				t.Run(string(f.FullName()), func(t *testing.T) {
					for _, parts := range [][3]int32{{2023, 2, 29}, {2100, 2, 29}, {2024, 4, 31}} {
						m := purchaseFixture(d)
						date := purchaseFixture(f.Message())
						for k, n := range []string{"year", "month", "day"} {
							date.Set(date.Descriptor().Fields().ByName(protoreflect.Name(n)), protoreflect.ValueOfInt32(parts[k]))
						}
						m.Set(f, protoreflect.ValueOfMessage(date))
						if err := protovalidate.Validate(m); err == nil {
							t.Fatal("nonexistent calendar date accepted")
						}
					}
				})
			} else if f.Kind() == protoreflect.BytesKind || (f.Kind() == protoreflect.StringKind && (bytes.HasSuffix([]byte(f.Name()), []byte("_id")) || f.Name() == "id")) {
				m := purchaseFixture(d)
				if f.Kind() == protoreflect.BytesKind {
					m.Set(f, protoreflect.ValueOfBytes([]byte{1}))
				} else {
					m.Set(f, protoreflect.ValueOfString("00000000-0000-4000-8000-000000000000"))
				}
				if err := protovalidate.Validate(m); err == nil {
					t.Errorf("malformed retained identity/hash accepted: %s", f.FullName())
				}
			}
		}
	}
	for _, tc := range []struct {
		name, field string
		max         int
	}{{"PostReviewedPurchaseRequest", "allocations", 50}, {"PurchasePosting", "allocations", 50}, {"ListPurchasePaymentsResponse", "payments", 200}, {"BasWorkpaper", "purchase_sources", 1000}, {"BasWorkpaper", "purchase_reviews", 1000}} {
		d := purchaseDescriptor(t, tc.name)
		m := purchaseFixture(d)
		f := d.Fields().ByName(protoreflect.Name(tc.field))
		if f == nil {
			t.Fatalf("missing bounded field %s.%s", tc.name, tc.field)
		}
		l := m.Mutable(f).List()
		for l.Len() <= tc.max {
			l.Append(protoreflect.ValueOfMessage(purchaseFixture(f.Message())))
		}
		if err := protovalidate.Validate(m); err == nil {
			t.Errorf("overflow accepted: %s.%s", tc.name, tc.field)
		}
	}
}

func TestPurchasePositiveAUDPaymentAndZeroGSTAllocation(t *testing.T) {
	d := purchaseDescriptor(t, "RecordPurchasePaymentRequest")
	f := d.Fields().ByName("amount")
	for _, tc := range []struct {
		currency string
		amount   int64
	}{{"AUD", 0}, {"AUD", -1}, {"USD", 1}} {
		m := purchaseFixture(d)
		money := m.Mutable(f).Message()
		money.Set(money.Descriptor().Fields().ByName("currency_code"), protoreflect.ValueOfString(tc.currency))
		money.Set(money.Descriptor().Fields().ByName("minor_units"), protoreflect.ValueOfInt64(tc.amount))
		if err := protovalidate.Validate(m); err == nil {
			t.Fatal("invalid payment accepted")
		}
	}
	allocation := purchaseFixture(purchaseDescriptor(t, "PurchaseAllocationInput"))
	for _, n := range []string{"supplier_gst", "eligible_gst"} {
		money := allocation.Mutable(allocation.Descriptor().Fields().ByName(protoreflect.Name(n))).Message()
		money.Set(money.Descriptor().Fields().ByName("minor_units"), protoreflect.ValueOfInt64(0))
	}
	if err := protovalidate.Validate(allocation); err != nil {
		t.Fatalf("zero GST rejected: %v", err)
	}
}

func TestPurchaseInputAndShallowProjectionDoNotEmbedEvidenceBytes(t *testing.T) {
	post := purchaseDescriptor(t, "PostReviewedPurchaseRequest")
	for _, n := range []string{"document", "snapshot_bytes", "original_content", "claim_amount"} {
		if post.Fields().ByName(protoreflect.Name(n)) != nil {
			t.Errorf("renderer supplied trusted retained content: %s", n)
		}
	}
	accounting := purchaseDescriptor(t, "PurchaseAccounting")
	for i := 0; i < accounting.Fields().Len(); i++ {
		if accounting.Fields().Get(i).IsList() {
			t.Fatal("accounting projection must not embed unbounded history")
		}
	}
	posting := purchaseFixture(purchaseDescriptor(t, "PurchasePosting"))
	if posting.Has(posting.Descriptor().Fields().ByName("journal_id")) {
		t.Fatal("zero posting fixture must permit absent journal")
	}
	for _, name := range []string{"gross", "supplier_gst", "eligible_gst"} {
		money := posting.Mutable(posting.Descriptor().Fields().ByName(protoreflect.Name(name))).Message()
		money.Set(money.Descriptor().Fields().ByName("minor_units"), protoreflect.ValueOfInt64(0))
		allocation := posting.Get(posting.Descriptor().Fields().ByName("allocations")).List().Get(0).Message()
		allocated := allocation.Mutable(allocation.Descriptor().Fields().ByName(protoreflect.Name(name))).Message()
		allocated.Set(allocated.Descriptor().Fields().ByName("minor_units"), protoreflect.ValueOfInt64(0))
	}
	if err := protovalidate.Validate(posting); err != nil {
		t.Fatalf("zero purchase without journal rejected: %v", err)
	}
	legacy := purchaseFixture(purchaseDescriptor(t, "LegacyPurchaseSourceRef"))
	f := legacy.Descriptor().Fields().ByName("opening_payable")
	legacy.Set(f, protoreflect.ValueOfMessage(purchaseFixture(f.Message())))
	if err := protovalidate.Validate(legacy); err != nil {
		t.Fatalf("opening source without document rejected: %v", err)
	}
}
