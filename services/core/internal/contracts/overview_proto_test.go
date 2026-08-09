package contracts_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	validate "buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	_ "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
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
	assertExactFields(t, revisions, map[protoreflect.Name]protoreflect.Kind{
		"financial_revision":            protoreflect.Uint64Kind,
		"ledger_revision":               protoreflect.Uint64Kind,
		"settlement_revision":           protoreflect.Uint64Kind,
		"banking_revision":              protoreflect.Uint64Kind,
		"tax_source_revision":           protoreflect.Uint64Kind,
		"organisation_profile_revision": protoreflect.Uint64Kind,
		"rule_bundle_revision":          protoreflect.Uint64Kind,
	})

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
	message := dynamicpb.NewMessage(fixtureDescriptor)
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(source, message); err != nil {
		t.Fatalf("decode canonical Protobuf JSON walkthrough fixture: %v", err)
	}
	normalized, err := (protojson.MarshalOptions{UseProtoNames: true, Indent: "  "}).Marshal(message)
	if err != nil {
		t.Fatalf("marshal walkthrough fixture: %v", err)
	}
	assertJSONEqual(t, source, normalized)

	var fixture map[string]any
	decoder := json.NewDecoder(bytes.NewReader(source))
	decoder.UseNumber()
	if err := decoder.Decode(&fixture); err != nil {
		t.Fatal(err)
	}
	assertFixtureMoney(t, fixture, "opening_balance", "100000")
	assertFixtureMoney(t, fixture, "bill_net", "29000")
	assertFixtureMoney(t, fixture, "bill_gross", "31900")
	assertFixtureMoney(t, fixture, "bill_gst", "2900")
	assertFixtureMoney(t, fixture, "statement_withdrawal", "-31900")
	assertFixtureMoney(t, fixture, "payment_amount", "31900")
	assertFixtureMoney(t, fixture, "closing_bank_balance", "68100")
	assertFixtureMoney(t, fixture, "trial_balance_total_debits", "100000")
	assertFixtureMoney(t, fixture, "trial_balance_total_credits", "100000")
	assertFixtureMoney(t, fixture, "bas_g1", "0")
	assertFixtureMoney(t, fixture, "bas_1a", "0")
	assertFixtureMoney(t, fixture, "bas_1b", "2900")
	assertFixtureMoney(t, fixture, "bas_net_refundable", "2900")
	assertFixtureCivilDate(t, fixture, "opening_date", 2024, 4, 30)
	assertFixtureCivilDate(t, fixture, "bill_issue_date", 2024, 5, 12)
	assertFixtureCivilDate(t, fixture, "statement_start_date", 2024, 5, 1)
	assertFixtureCivilDate(t, fixture, "statement_end_date", 2024, 5, 31)
	assertFixtureCivilDate(t, fixture, "payment_date", 2024, 5, 15)
	if fixture["gst_basis"] != "GST_BASIS_NON_CASH" {
		t.Errorf("gst_basis = %v, want GST_BASIS_NON_CASH", fixture["gst_basis"])
	}
	if fixture["gst_reporting_frequency"] != "GST_REPORTING_FREQUENCY_QUARTERLY" {
		t.Errorf("gst_reporting_frequency = %v, want quarterly", fixture["gst_reporting_frequency"])
	}
	if fixture["synthetic_abn"] != "99000000000" {
		t.Errorf("synthetic_abn = %v, want the fixed valid synthetic ABN", fixture["synthetic_abn"])
	}
	period := fixture["bas_period"].(map[string]any)
	assertFixtureCivilDate(t, period, "start_date", 2024, 4, 1)
	assertFixtureCivilDate(t, period, "end_date", 2024, 6, 30)
	afterExtraction := fixture["after_extraction_overview"].(map[string]any)
	finalOverview := fixture["final_overview"].(map[string]any)
	assertFixtureCount(t, afterExtraction, "documents_needing_review", 1)
	assertFixtureCount(t, finalOverview, "documents_needing_review", 0)
	assertFixtureCount(t, finalOverview, "documents_reviewed_in_period", 1)
	assertFixtureCount(t, finalOverview, "banking_lines_needing_reconciliation", 0)
	assertFixtureCount(t, finalOverview, "banking_lines_unreconciled_in_period", 0)
	assertFixtureCount(t, finalOverview, "current_draft_bas_workpapers", 1)
	items := afterExtraction["attention_items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["kind"] != "ATTENTION_ITEM_KIND_DOCUMENT_REVIEW" {
		t.Errorf("after-extraction attention items = %v, want one typed document review", items)
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

func assertFixtureMoney(t *testing.T, fixture map[string]any, field, wantMinorUnits string) {
	t.Helper()
	money, ok := fixture[field].(map[string]any)
	if !ok {
		t.Fatalf("fixture %s is missing or not Money", field)
	}
	minorUnits := money["minor_units"]
	if minorUnits == nil {
		minorUnits = "0"
	}
	if money["currency_code"] != "AUD" || minorUnits != wantMinorUnits {
		t.Errorf("fixture %s = %v, want AUD %s minor units", field, money, wantMinorUnits)
	}
}

func assertFixtureCivilDate(t *testing.T, fixture map[string]any, field string, year, month, day int) {
	t.Helper()
	date, ok := fixture[field].(map[string]any)
	if !ok {
		t.Fatalf("fixture %s is missing or not CivilDate", field)
	}
	if date["year"].(json.Number).String() != strconv.Itoa(year) ||
		date["month"].(json.Number).String() != strconv.Itoa(month) ||
		date["day"].(json.Number).String() != strconv.Itoa(day) {
		t.Errorf("fixture %s = %v, want %04d-%02d-%02d", field, date, year, month, day)
	}
}

func assertFixtureCount(t *testing.T, fixture map[string]any, field string, want int) {
	t.Helper()
	value, ok := fixture[field].(json.Number)
	if !ok || value.String() != strconv.Itoa(want) {
		t.Errorf("fixture %s count = %v, want %d", field, fixture[field], want)
	}
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
