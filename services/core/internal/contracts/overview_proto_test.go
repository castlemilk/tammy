package contracts_test

import (
	"bytes"
	"crypto/sha256"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	validate "buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"buf.build/go/protovalidate"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestOverviewDescriptorsExposeBoundedAttentionSummary(t *testing.T) {
	file, err := protoregistry.GlobalFiles.FindFileByPath("tammy/v1/overview.proto")
	if err != nil {
		t.Fatalf("overview descriptor missing: %v", err)
	}
	if file.Syntax() != protoreflect.Proto3 {
		t.Fatalf("overview syntax = %s, want proto3", file.Syntax())
	}

	service := file.Services().ByName("OverviewService")
	if service == nil {
		t.Fatal("tammy.v1.OverviewService descriptor missing")
	}
	if service.Methods().Len() != 1 {
		t.Fatalf("OverviewService method count = %d, want one read-only query", service.Methods().Len())
	}
	method := service.Methods().ByName("GetAttentionSummary")
	if method == nil {
		t.Fatal("OverviewService.GetAttentionSummary descriptor missing")
	}
	if method.IsStreamingClient() || method.IsStreamingServer() {
		t.Fatal("OverviewService.GetAttentionSummary must be unary")
	}
	if method.Input().FullName() != "tammy.v1.GetAttentionSummaryRequest" ||
		method.Output().FullName() != "tammy.v1.GetAttentionSummaryResponse" {
		t.Fatalf("unexpected GetAttentionSummary request/response: %s -> %s", method.Input().FullName(), method.Output().FullName())
	}

	request := method.Input()
	assertExactFields(t, request, map[protoreflect.Name]protoreflect.Kind{
		"authentication":   protoreflect.MessageKind,
		"organisation_id":  protoreflect.StringKind,
		"as_of_date":       protoreflect.MessageKind,
		"reporting_period": protoreflect.MessageKind,
	})
	assertRequiredMessage(t, request.Fields().ByName("authentication"), "tammy.v1.AuthenticationContext")
	assertUUIDv7Field(t, request.Fields().ByName("organisation_id"))
	assertRequiredMessage(t, request.Fields().ByName("as_of_date"), "tammy.v1.CivilDate")
	assertRequiredMessage(t, request.Fields().ByName("reporting_period"), "tammy.v1.ReportingPeriod")

	period := requireMessage(t, "tammy.v1.ReportingPeriod")
	assertExactFields(t, period, map[protoreflect.Name]protoreflect.Kind{
		"start_date": protoreflect.MessageKind,
		"end_date":   protoreflect.MessageKind,
	})
	assertRequiredMessage(t, period.Fields().ByName("start_date"), "tammy.v1.CivilDate")
	assertRequiredMessage(t, period.Fields().ByName("end_date"), "tammy.v1.CivilDate")

	response := method.Output()
	assertExactFields(t, response, map[protoreflect.Name]protoreflect.Kind{
		"documents_needing_review":             protoreflect.Uint32Kind,
		"documents_reviewed_in_period":         protoreflect.Uint32Kind,
		"banking_lines_needing_reconciliation": protoreflect.Uint32Kind,
		"banking_lines_unreconciled_in_period": protoreflect.Uint32Kind,
		"current_draft_bas_workpapers":         protoreflect.Uint32Kind,
		"bas_status":                           protoreflect.EnumKind,
		"attention_items":                      protoreflect.MessageKind,
		"revisions":                            protoreflect.MessageKind,
		"as_of_date":                           protoreflect.MessageKind,
		"reporting_period":                     protoreflect.MessageKind,
	})
	for _, name := range []protoreflect.Name{
		"documents_needing_review",
		"documents_reviewed_in_period",
		"banking_lines_needing_reconciliation",
		"banking_lines_unreconciled_in_period",
		"current_draft_bas_workpapers",
	} {
		assertBoundedCount(t, response.Fields().ByName(name), 1_000_000)
	}
	assertDefinedEnumField(t, response.Fields().ByName("bas_status"), "tammy.v1.BasAttentionStatus")
	assertRepeatedMessageBound(t, response.Fields().ByName("attention_items"), "tammy.v1.AttentionItem", 8)
	assertRequiredMessage(t, response.Fields().ByName("revisions"), "tammy.v1.AttentionRevisionVector")
	assertRequiredMessage(t, response.Fields().ByName("as_of_date"), "tammy.v1.CivilDate")
	assertRequiredMessage(t, response.Fields().ByName("reporting_period"), "tammy.v1.ReportingPeriod")

	item := requireMessage(t, "tammy.v1.AttentionItem")
	assertExactFields(t, item, map[protoreflect.Name]protoreflect.Kind{
		"kind":     protoreflect.EnumKind,
		"resource": protoreflect.MessageKind,
		"label":    protoreflect.StringKind,
	})
	assertDefinedEnumField(t, item.Fields().ByName("kind"), "tammy.v1.AttentionItemKind")
	assertRequiredMessage(t, item.Fields().ByName("resource"), "tammy.v1.SourceRef")
	labelRules := fieldRules(t, item.Fields().ByName("label")).GetString_()
	if labelRules == nil || labelRules.GetMinLen() != 1 || labelRules.GetMaxLen() != 160 {
		t.Fatalf("AttentionItem.label bounds = %v, want 1..160", labelRules)
	}

	revisions := requireMessage(t, "tammy.v1.AttentionRevisionVector")
	revisionFields := map[protoreflect.Name]protoreflect.Kind{
		"financial_revision":            protoreflect.Uint64Kind,
		"ledger_revision":               protoreflect.Uint64Kind,
		"settlement_revision":           protoreflect.Uint64Kind,
		"banking_revision":              protoreflect.Uint64Kind,
		"tax_source_revision":           protoreflect.Uint64Kind,
		"organisation_profile_revision": protoreflect.Uint64Kind,
		"rule_bundle_revision":          protoreflect.Uint64Kind,
	}
	assertExactFields(t, revisions, revisionFields)
	for name := range revisionFields {
		assertNonnegativeRevision(t, revisions.Fields().ByName(name))
	}

	assertEnumValues(t, "tammy.v1.AttentionItemKind", []protoreflect.Name{
		"ATTENTION_ITEM_KIND_UNSPECIFIED",
		"ATTENTION_ITEM_KIND_DOCUMENT_REVIEW",
		"ATTENTION_ITEM_KIND_BANKING_RECONCILIATION",
		"ATTENTION_ITEM_KIND_BAS_WORKPAPER",
	})
	assertEnumValues(t, "tammy.v1.BasAttentionStatus", []protoreflect.Name{
		"BAS_ATTENTION_STATUS_UNSPECIFIED",
		"BAS_ATTENTION_STATUS_NOT_CREATED",
		"BAS_ATTENTION_STATUS_DRAFT_NOT_LODGED",
		"BAS_ATTENTION_STATUS_OUTDATED",
	})
	assertEnumRejectsUnspecified(t, item.Fields().ByName("kind"))
	assertEnumRejectsUnspecified(t, response.Fields().ByName("bas_status"))

	for messageIndex := 0; messageIndex < file.Messages().Len(); messageIndex++ {
		message := file.Messages().Get(messageIndex)
		for fieldIndex := 0; fieldIndex < message.Fields().Len(); fieldIndex++ {
			field := message.Fields().Get(fieldIndex)
			if field.IsMap() || field.Kind() == protoreflect.FloatKind || field.Kind() == protoreflect.DoubleKind {
				t.Errorf("overview field %s uses a prohibited map or binary floating-point shape", field.FullName())
			}
			lowerName := strings.ToLower(string(field.Name()))
			for _, prohibited := range []string{"route", "path", "secret", "password", "passphrase", "token"} {
				if strings.Contains(lowerName, prohibited) {
					t.Errorf("overview field %s exposes prohibited renderer or secret data", field.FullName())
				}
			}
		}
	}
	t.Run("runtime accepts zero genesis revision vector", func(t *testing.T) {
		if err := protovalidate.Validate(validAttentionSummary(0)); err != nil {
			t.Fatalf("zero revision vector rejected: %v", err)
		}
	})
	t.Run("runtime accepts maximum uint64 revision vector", func(t *testing.T) {
		if err := protovalidate.Validate(validAttentionSummary(math.MaxUint64)); err != nil {
			t.Fatalf("maximum uint64 revision vector rejected: %v", err)
		}
	})
	t.Run("runtime rejects unspecified BAS status", func(t *testing.T) {
		response := validAttentionSummary(1)
		response.BasStatus = tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_UNSPECIFIED
		if err := protovalidate.Validate(response); err == nil {
			t.Fatal("unspecified BAS attention status passed runtime validation")
		}
	})
	t.Run("runtime accepts valid BAS status", func(t *testing.T) {
		if err := protovalidate.Validate(validAttentionSummary(1)); err != nil {
			t.Fatalf("valid BAS attention status rejected: %v", err)
		}
	})
	t.Run("runtime rejects unspecified attention item kind", func(t *testing.T) {
		item := validAttentionItem()
		item.Kind = tammyv1.AttentionItemKind_ATTENTION_ITEM_KIND_UNSPECIFIED
		if err := protovalidate.Validate(item); err == nil {
			t.Fatal("unspecified attention item kind passed runtime validation")
		}
	})
	t.Run("runtime accepts valid attention item kind", func(t *testing.T) {
		if err := protovalidate.Validate(validAttentionItem()); err != nil {
			t.Fatalf("valid attention item kind rejected: %v", err)
		}
	})
	assertOverviewCoverageContract(t)
}

func TestNoncashSupplierMonthFixtureIsCanonicalAndExact(t *testing.T) {
	fixtureDescriptor := requireMessage(t, "tammy.v1.NoncashSupplierMonthFixture")
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve overview contract test path")
	}
	fixturePath := filepath.Join(filepath.Dir(sourceFile), "../../../../test/fixtures/walkthrough/noncash-supplier-month.pb.json")
	source, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read walkthrough fixture: %v", err)
	}
	fixture := &tammyv1.NoncashSupplierMonthFixture{}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(source, fixture); err != nil {
		t.Fatalf("decode canonical Protobuf JSON walkthrough fixture: %v", err)
	}
	if fixture.ProtoReflect().Descriptor().FullName() != fixtureDescriptor.FullName() {
		t.Fatalf("fixture type = %s, want %s", fixture.ProtoReflect().Descriptor().FullName(), fixtureDescriptor.FullName())
	}
	if err := protovalidate.Validate(fixture); err != nil {
		t.Fatalf("walkthrough fixture fails Protovalidate: %v", err)
	}
	normalized, err := (protojson.MarshalOptions{UseProtoNames: true, Indent: "  "}).Marshal(fixture)
	if err != nil {
		t.Fatalf("marshal walkthrough fixture: %v", err)
	}
	assertJSONEqual(t, source, normalized)

	want := expectedNoncashSupplierMonthFixture()
	if !proto.Equal(fixture, want) {
		wantJSON, _ := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(want)
		gotJSON, _ := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(fixture)
		t.Fatalf("walkthrough fixture semantic oracle mismatch\nwant: %s\n got: %s", wantJSON, gotJSON)
	}
	wantHash := retainedReviewContentHash()
	gotHash := fixture.GetAfterExtractionOverview().GetAttentionItems()[0].GetResource().GetContentHash()
	if !bytes.Equal(gotHash, wantHash) {
		t.Fatalf("retained-review SHA-256 = %x, want independently recomputed %x", gotHash, wantHash)
	}
}

func requireMessage(t *testing.T, name protoreflect.FullName) protoreflect.MessageDescriptor {
	t.Helper()
	descriptor, err := protoregistry.GlobalFiles.FindDescriptorByName(name)
	if err != nil {
		t.Fatalf("find %s descriptor: %v", name, err)
	}
	message, ok := descriptor.(protoreflect.MessageDescriptor)
	if !ok {
		t.Fatalf("%s is not a message", name)
	}
	return message
}

func assertExactFields(t *testing.T, message protoreflect.MessageDescriptor, want map[protoreflect.Name]protoreflect.Kind) {
	t.Helper()
	if message.Fields().Len() != len(want) {
		t.Errorf("%s field count = %d, want %d", message.FullName(), message.Fields().Len(), len(want))
	}
	for name, kind := range want {
		field := message.Fields().ByName(name)
		if field == nil {
			t.Errorf("%s.%s descriptor missing", message.FullName(), name)
			continue
		}
		if field.Kind() != kind {
			t.Errorf("%s kind = %s, want %s", field.FullName(), field.Kind(), kind)
		}
	}
}

func fieldRules(t *testing.T, field protoreflect.FieldDescriptor) *validate.FieldRules {
	t.Helper()
	if field == nil {
		t.Fatal("field descriptor missing")
	}
	options, ok := field.Options().(*descriptorpb.FieldOptions)
	if !ok || !proto.HasExtension(options, validate.E_Field) {
		t.Fatalf("%s validation rules missing", field.FullName())
	}
	rules, ok := proto.GetExtension(options, validate.E_Field).(*validate.FieldRules)
	if !ok {
		t.Fatalf("%s validation rules have unexpected type", field.FullName())
	}
	return rules
}

func assertRequiredMessage(t *testing.T, field protoreflect.FieldDescriptor, want protoreflect.FullName) {
	t.Helper()
	if field == nil || field.Kind() != protoreflect.MessageKind || field.Message().FullName() != want {
		t.Fatalf("required field has descriptor %v, want %s", field, want)
	}
	if !fieldRules(t, field).GetRequired() {
		t.Fatalf("%s must carry buf.validate required", field.FullName())
	}
}

func assertUUIDv7Field(t *testing.T, field protoreflect.FieldDescriptor) {
	t.Helper()
	if got := fieldRules(t, field).GetString_().GetPattern(); got != canonicalUUIDv7Pattern {
		t.Fatalf("%s UUIDv7 pattern = %q, want %q", field.FullName(), got, canonicalUUIDv7Pattern)
	}
}

func assertBoundedCount(t *testing.T, field protoreflect.FieldDescriptor, max uint32) {
	t.Helper()
	rules := fieldRules(t, field).GetUint32()
	if rules == nil || rules.GetLte() != max {
		t.Fatalf("%s upper bound = %v, want %d", field.FullName(), rules, max)
	}
}

func assertNonnegativeRevision(t *testing.T, field protoreflect.FieldDescriptor) {
	t.Helper()
	rules := fieldRules(t, field).GetUint64()
	if rules == nil {
		t.Errorf("%s is missing explicit nonnegative uint64 validation", field.FullName())
		return
	}
	gte := rules.ProtoReflect().Descriptor().Fields().ByName("gte")
	if gte == nil || !rules.ProtoReflect().Has(gte) || rules.GetGte() != 0 {
		t.Errorf("%s lower bound = %v, want explicit gte 0 for revision genesis compatibility", field.FullName(), rules)
	}
}

func assertEnumRejectsUnspecified(t *testing.T, field protoreflect.FieldDescriptor) {
	t.Helper()
	rules := fieldRules(t, field).GetEnum()
	if rules == nil || !rules.GetDefinedOnly() {
		t.Errorf("%s must reject undefined enum values", field.FullName())
		return
	}
	if notIn := rules.GetNotIn(); len(notIn) != 1 || notIn[0] != 0 {
		t.Errorf("%s enum not_in = %v, want [0]", field.FullName(), notIn)
	}
}

func assertRepeatedMessageBound(t *testing.T, field protoreflect.FieldDescriptor, want protoreflect.FullName, max uint64) {
	t.Helper()
	if field == nil || !field.IsList() || field.Kind() != protoreflect.MessageKind || field.Message().FullName() != want {
		t.Fatalf("bounded repeated field has descriptor %v, want repeated %s", field, want)
	}
	if got := fieldRules(t, field).GetRepeated().GetMaxItems(); got != max {
		t.Fatalf("%s max_items = %d, want %d", field.FullName(), got, max)
	}
}

func assertDefinedEnumField(t *testing.T, field protoreflect.FieldDescriptor, want protoreflect.FullName) {
	t.Helper()
	if field == nil || field.Kind() != protoreflect.EnumKind || field.Enum().FullName() != want {
		t.Fatalf("enum field has descriptor %v, want %s", field, want)
	}
	if !fieldRules(t, field).GetEnum().GetDefinedOnly() {
		t.Fatalf("%s must reject undefined enum values", field.FullName())
	}
}

func assertEnumValues(t *testing.T, name protoreflect.FullName, want []protoreflect.Name) {
	t.Helper()
	descriptor, err := protoregistry.GlobalFiles.FindDescriptorByName(name)
	if err != nil {
		t.Fatalf("find %s descriptor: %v", name, err)
	}
	enum, ok := descriptor.(protoreflect.EnumDescriptor)
	if !ok {
		t.Fatalf("%s is not an enum", name)
	}
	if enum.Values().Len() != len(want) {
		t.Fatalf("%s value count = %d, want %d", name, enum.Values().Len(), len(want))
	}
	for index, value := range want {
		if enum.Values().Get(index).Name() != value || enum.Values().Get(index).Number() != protoreflect.EnumNumber(index) {
			t.Errorf("%s value %d = %s/%d, want %s/%d", name, index, enum.Values().Get(index).Name(), enum.Values().Get(index).Number(), value, index)
		}
	}
}

func validAttentionSummary(revision uint64) *tammyv1.GetAttentionSummaryResponse {
	return &tammyv1.GetAttentionSummaryResponse{
		BasStatus: tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_NOT_CREATED,
		Revisions: &tammyv1.AttentionRevisionVector{
			FinancialRevision: revision, LedgerRevision: revision, SettlementRevision: revision,
			BankingRevision: revision, TaxSourceRevision: revision,
			OrganisationProfileRevision: revision, RuleBundleRevision: revision,
		},
		AsOfDate: date(2024, 6, 30),
		ReportingPeriod: &tammyv1.ReportingPeriod{
			StartDate: date(2024, 4, 1),
			EndDate:   date(2024, 6, 30),
		},
	}
}

func validAttentionItem() *tammyv1.AttentionItem {
	return &tammyv1.AttentionItem{
		Kind: tammyv1.AttentionItemKind_ATTENTION_ITEM_KIND_DOCUMENT_REVIEW,
		Resource: &tammyv1.SourceRef{
			Type: "document_review", Id: "018f0000-0000-7000-8000-000000000101",
			Revision: 1, ContentHash: retainedReviewContentHash(),
		},
		Label: "Review Paper & Co supplier invoice",
	}
}

func expectedNoncashSupplierMonthFixture() *tammyv1.NoncashSupplierMonthFixture {
	return &tammyv1.NoncashSupplierMonthFixture{
		OrganisationName:         "Tammy Demo Pty Ltd",
		SyntheticAbn:             "99000000000",
		CurrencyCode:             "AUD",
		GstBasis:                 tammyv1.GstBasis_GST_BASIS_NON_CASH,
		GstReportingFrequency:    tammyv1.GstReportingFrequency_GST_REPORTING_FREQUENCY_QUARTERLY,
		FinancialYearEndMonth:    6,
		BasPeriod:                &tammyv1.ReportingPeriod{StartDate: date(2024, 4, 1), EndDate: date(2024, 6, 30)},
		PrimaryBankName:          "Business Bank",
		OpeningDate:              date(2024, 4, 30),
		OpeningBalance:           aud(100000),
		SupplierName:             "Paper & Co Supplies Pty Ltd",
		SourceDocumentName:       "paper-and-co-sup-1001.pdf",
		SupplierReference:        "SUP-1001",
		BillIssueDate:            date(2024, 5, 12),
		BillTaxExclusive:         true,
		BillNet:                  aud(29000),
		BillGst:                  aud(2900),
		BillGross:                aud(31900),
		StatementStartDate:       date(2024, 5, 1),
		StatementEndDate:         date(2024, 5, 31),
		PaymentDate:              date(2024, 5, 15),
		StatementWithdrawal:      aud(-31900),
		PaymentAmount:            aud(31900),
		ClosingBankBalance:       aud(68100),
		TrialBalanceTotalDebits:  aud(100000),
		TrialBalanceTotalCredits: aud(100000),
		BasG1:                    aud(0),
		Bas_1A:                   aud(0),
		Bas_1B:                   aud(2900),
		BasNetRefundable:         aud(2900),
		AfterExtractionOverview: &tammyv1.WalkthroughOverviewOracle{
			DocumentsNeedingReview:            proto.Uint32(1),
			DocumentsReviewedInPeriod:         proto.Uint32(0),
			BankingLinesNeedingReconciliation: proto.Uint32(0),
			BankingLinesUnreconciledInPeriod:  proto.Uint32(0),
			CurrentDraftBasWorkpapers:         proto.Uint32(0),
			BasStatus:                         tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_NOT_CREATED,
			AttentionItems:                    []*tammyv1.AttentionItem{validAttentionItem()},
		},
		FinalOverview: &tammyv1.WalkthroughOverviewOracle{
			DocumentsNeedingReview:            proto.Uint32(0),
			DocumentsReviewedInPeriod:         proto.Uint32(1),
			BankingLinesNeedingReconciliation: proto.Uint32(0),
			BankingLinesUnreconciledInPeriod:  proto.Uint32(0),
			CurrentDraftBasWorkpapers:         proto.Uint32(1),
			BasStatus:                         tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_DRAFT_NOT_LODGED,
		},
	}
}

func date(year, month, day int32) *tammyv1.CivilDate {
	return &tammyv1.CivilDate{Year: year, Month: month, Day: day}
}

func aud(minorUnits int64) *tammyv1.Money {
	return &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: minorUnits}
}

func retainedReviewContentHash() []byte {
	// Exact v1 preimage: domain, version, source filename, supplier reference,
	// issue date, currency, net, GST, and gross minor units, joined by NUL bytes.
	preimage := strings.Join([]string{
		"tammy.walkthrough.retained-review", "v1", "paper-and-co-sup-1001.pdf", "SUP-1001",
		"2024-05-12", "AUD", "29000", "2900", "31900",
	}, "\x00")
	digest := sha256.Sum256([]byte(preimage))
	return digest[:]
}

func assertOverviewCoverageContract(t *testing.T) {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve overview contract test path")
	}
	coveragePath := filepath.Join(filepath.Dir(sourceFile), "../../../../test/e2e/coverage.yaml")
	source, err := os.ReadFile(coveragePath)
	if err != nil {
		t.Fatalf("read E2E coverage: %v", err)
	}
	const header = "  tammy.v1.OverviewService.GetAttentionSummary:\n"
	start := bytes.Index(source, []byte(header))
	if start < 0 {
		t.Fatal("OverviewService.GetAttentionSummary coverage row missing")
	}
	block := source[start+len(header):]
	if end := bytes.Index(block, []byte("\n  tammy.v1.")); end >= 0 {
		block = block[:end]
	}
	wantFragments := []string{
		"    stage: declared_future\n    preload: getAttentionSummary\n    cases: []",
		"    projections:\n      - attention_counts\n      - typed_attention_items\n      - financial_revision\n      - module_revisions\n      - reporting_period",
		"    roles:\n      workspace_admin: planned_allowed\n      business_preparer: planned_allowed\n      business_lodger: planned_allowed\n      auditor: planned_allowed",
		"    principalFailures:\n      - AUTHENTICATION_REQUIRED\n      - PERMISSION_DENIED\n      - INVALID_ORGANISATION\n      - INVALID_DATE\n      - INVALID_PERIOD\n      - REVISION_SNAPSHOT_UNAVAILABLE",
		"    list:\n      states:\n        - empty\n        - populated",
		"    idempotency:\n      mode: query\n      outcomes:\n        - not_applicable",
	}
	for _, fragment := range wantFragments {
		if !bytes.Contains(block, []byte(fragment)) {
			t.Errorf("Overview coverage row is missing exact contract fragment:\n%s", fragment)
		}
	}
}
