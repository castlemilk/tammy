package contracts_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	_ "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

var basQueries = []string{
	"GetBasObligationsProfile", "ListBasObligationsProfiles", "GetBasCalendar", "ListBasPeriods",
	"GetBasPreparation", "ListBasPreparations", "GetBasCalculation", "ListBasCalculations",
	"GetFrozenBas", "ListFrozenBas", "GetBasExternalObservation", "ListBasExternalObservations",
	"GetBasExternalDisputeResolution", "ListBasExternalDisputeResolutions", "ListBasExportEvents",
}

var basCommands = []string{
	"SetBasObligationsProfile", "ConfirmBasPeriod", "SaveBasPaygInputs", "RegenerateBasPreparation",
	"ValidateBasPreparation", "AcknowledgeBasWarnings", "FreezeBasForManualHandoff", "CreateBasReplacement",
	"CreateBasRevision", "RecordExternalBasObservation", "CorrectExternalBasObservation", "ResolveExternalBasDispute",
	"ReplaceRejectedBasAttempt", "RecordBasExportEvent",
}

func basFile(t *testing.T) protoreflect.FileDescriptor {
	t.Helper()
	file, err := protoregistry.GlobalFiles.FindFileByPath("tammy/v1/bas_preparation.proto")
	if err != nil {
		t.Fatalf("BAS preparation descriptor missing: %v", err)
	}
	return file
}

func TestBasPreparationServiceIsLocalScopedAndExact(t *testing.T) {
	file := basFile(t)
	service := file.Services().ByName("BasPreparationService")
	wantMethods := append(append([]string(nil), basQueries...), basCommands...)
	if service == nil || service.Methods().Len() != len(wantMethods) {
		t.Fatalf("expected one local BAS service with %d unary methods", len(wantMethods))
	}
	for i, name := range wantMethods {
		method := service.Methods().Get(i)
		if string(method.Name()) != name || method.IsStreamingClient() || method.IsStreamingServer() || method.Input().Name() != protoreflect.Name(name+"Request") || method.Output().Name() != protoreflect.Name(name+"Response") {
			t.Fatalf("unexpected local BAS method: %s", method.FullName())
		}
		request := method.Input()
		organisation := request.Fields().ByName("organisation_id")
		if organisation == nil || fieldRules(t, organisation).GetString().GetPattern() != canonicalUUIDv7Pattern {
			t.Fatalf("%s must identify the exact organisation", method.FullName())
		}
		contextName, contextType := "authentication", "tammy.v1.AuthenticationContext"
		if i >= len(basQueries) {
			contextName, contextType = "command_context", "tammy.v1.CommandContext"
			version := request.Fields().ByName("expected_version")
			if version == nil || version.Kind() != protoreflect.Uint64Kind {
				t.Fatalf("%s must bind the expected aggregate version", method.FullName())
			}
		}
		context := request.Fields().ByName(protoreflect.Name(contextName))
		if context == nil || context.Kind() != protoreflect.MessageKind || string(context.Message().FullName()) != contextType || !fieldRules(t, context).GetRequired() {
			t.Fatalf("%s must require shared %s", method.FullName(), contextType)
		}
	}
}

func TestBasPreparationContractsAreTypedBoundedAndDoNotCarryTransportAuthority(t *testing.T) {
	file := basFile(t)
	for i := 0; i < file.Messages().Len(); i++ {
		message := file.Messages().Get(i)
		for j := 0; j < message.Fields().Len(); j++ {
			field := message.Fields().Get(j)
			if field.IsMap() || (field.IsList() && fieldRules(t, field).GetRepeated().GetMaxItems() == 0) {
				t.Errorf("%s must be typed and bounded", field.FullName())
			}
			if field.Kind() == protoreflect.MessageKind {
				name := string(field.Message().FullName())
				if name == "google.protobuf.Any" || name == "google.protobuf.Struct" || strings.Contains(name, "SubmissionReceipt") || strings.Contains(name, "Sbr") {
					t.Errorf("%s carries dynamic or official transport authority", field.FullName())
				}
			}
			if field.Kind() == protoreflect.FloatKind || field.Kind() == protoreflect.DoubleKind {
				t.Errorf("%s must use exact integer/rational arithmetic", field.FullName())
			}
			if field.Kind() == protoreflect.StringKind && !field.IsList() && fieldRules(t, field).GetString().GetMaxLen() == 0 && fieldRules(t, field).GetString().GetPattern() == "" {
				t.Errorf("%s must bound text or use a bounded canonical pattern", field.FullName())
			}
			if field.Kind() == protoreflect.EnumKind && !field.IsList() && !fieldRules(t, field).GetEnum().GetDefinedOnly() {
				t.Errorf("%s must reject undefined enum values", field.FullName())
			}
			if field.Kind() == protoreflect.BytesKind && fieldRules(t, field).GetBytes().GetLen() != 32 {
				t.Errorf("%s must reference retained evidence by SHA256, without raw evidence bytes", field.FullName())
			}
		}
	}
}

func TestBasPreparationListsUseSharedBoundedPagination(t *testing.T) {
	service := basFile(t).Services().ByName("BasPreparationService")
	if service == nil {
		t.Fatal("missing BAS preparation service")
	}
	for _, name := range basQueries {
		if !strings.HasPrefix(name, "List") {
			continue
		}
		method := service.Methods().ByName(protoreflect.Name(name))
		if method == nil {
			t.Fatalf("missing scoped list %s", name)
		}
		page := method.Input().Fields().ByName("page")
		if page == nil || page.Kind() != protoreflect.MessageKind || page.Message().FullName() != "tammy.v1.PageRequest" || !fieldRules(t, page).GetRequired() {
			t.Errorf("%s must require shared authenticated page request", name)
		}
		info := method.Output().Fields().ByName("page")
		if info == nil || info.Kind() != protoreflect.MessageKind || info.Message().FullName() != "tammy.v1.PageInfo" || !fieldRules(t, info).GetRequired() {
			t.Errorf("%s must return shared page information", name)
		}
		items := 0
		for i := 0; i < method.Output().Fields().Len(); i++ {
			field := method.Output().Fields().Get(i)
			if field.IsList() {
				items++
				if fieldRules(t, field).GetRepeated().GetMaxItems() != 200 {
					t.Errorf("%s must admit at most 200 retained history items", field.FullName())
				}
			}
		}
		if items != 1 {
			t.Errorf("%s should page one retained history collection", name)
		}
	}
}

func TestBasPreparationStatesKeepExternalRecordsLocallyUnverified(t *testing.T) {
	basFile(t)
	assertEnumValues(t, "tammy.v1.BasPreparationState", []protoreflect.Name{
		"BAS_PREPARATION_STATE_UNSPECIFIED", "BAS_PREPARATION_STATE_COLLECTING", "BAS_PREPARATION_STATE_BLOCKED",
		"BAS_PREPARATION_STATE_REVIEW_READY", "BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF",
		"BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED", "BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION",
		"BAS_PREPARATION_STATE_SUPERSEDED_AFTER_RECORDED_REJECTION", "BAS_PREPARATION_STATE_SUPERSEDED_BY_CONFIRMED_REVISION",
	})
	assertEnumValues(t, "tammy.v1.BasExternalTrust", []protoreflect.Name{
		"BAS_EXTERNAL_TRUST_UNSPECIFIED", "BAS_EXTERNAL_TRUST_USER_RECORDED_UNVERIFIED",
	})
}

func TestBasPreparationFixturePinsOnlyApprovedLifecycleEdges(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve BAS fixture path")
	}
	body, err := os.ReadFile(filepath.Join(filepath.Dir(source), "../../../../test/fixtures/bas-preparation/transitions.pb.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture transitionFixtureDocument
	if err := json.Unmarshal(body, &fixture); err != nil {
		t.Fatal(err)
	}
	// This list is an independent oracle from the approved lifecycle table. Guards
	// are evaluated by the domain; fixture membership alone grants no authority.
	want := map[string]bool{
		"COLLECTING->BLOCKED": false, "COLLECTING->REVIEW_READY": false,
		"BLOCKED->COLLECTING": false, "BLOCKED->REVIEW_READY": false,
		"REVIEW_READY->COLLECTING": false, "REVIEW_READY->BLOCKED": false,
		"REVIEW_READY->FROZEN_FOR_MANUAL_HANDOFF":                          false,
		"FROZEN_FOR_MANUAL_HANDOFF->EXTERNAL_CONFIRMATION_RECORDED":        false,
		"FROZEN_FOR_MANUAL_HANDOFF->SUPERSEDED_BEFORE_CONFIRMATION":        false,
		"FROZEN_FOR_MANUAL_HANDOFF->SUPERSEDED_AFTER_RECORDED_REJECTION":   false,
		"EXTERNAL_CONFIRMATION_RECORDED->SUPERSEDED_BY_CONFIRMED_REVISION": false,
	}
	if fixture.SchemaVersion != 1 || len(fixture.Transitions) != len(want) {
		t.Fatalf("unexpected BAS transition fixture version/count: %d/%d", fixture.SchemaVersion, len(fixture.Transitions))
	}
	for _, edge := range fixture.Transitions {
		parts := strings.Split(edge.Transition, "->")
		if edge.Enum != "tammy.v1.BasPreparationState" || len(parts) != 2 || !strings.HasPrefix(parts[0], "BAS_PREPARATION_STATE_") || !strings.HasPrefix(parts[1], "BAS_PREPARATION_STATE_") {
			t.Fatalf("unexpected BAS lifecycle edge: %+v", edge)
		}
		key := strings.TrimPrefix(parts[0], "BAS_PREPARATION_STATE_") + "->" + strings.TrimPrefix(parts[1], "BAS_PREPARATION_STATE_")
		seen, exists := want[key]
		if !exists || seen {
			t.Fatalf("unapproved or duplicate BAS lifecycle edge: %s", key)
		}
		want[key] = true
	}
}
