package contracts_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	_ "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

type companyEOFYTransitionFixture struct {
	SchemaVersion int                         `json:"schemaVersion"`
	Transitions   []companyEOFYTransitionEdge `json:"transitions"`
}

type companyEOFYTransitionEdge struct {
	Enum       string `json:"enum"`
	Transition string `json:"transition"`
}

func TestCompanyEOFYLifecycleEnumsHaveExactNames(t *testing.T) {
	tests := []struct {
		name   protoreflect.FullName
		values []string
	}{
		{"tammy.v1.FinancialCloseState", []string{"FINANCIAL_CLOSE_STATE_UNSPECIFIED", "FINANCIAL_CLOSE_STATE_COLLECTING", "FINANCIAL_CLOSE_STATE_BLOCKED", "FINANCIAL_CLOSE_STATE_REVIEW_READY", "FINANCIAL_CLOSE_STATE_FROZEN"}},
		{"tammy.v1.CompanyReturnState", []string{"COMPANY_RETURN_STATE_UNSPECIFIED", "COMPANY_RETURN_STATE_COLLECTING", "COMPANY_RETURN_STATE_BLOCKED", "COMPANY_RETURN_STATE_REVIEW_READY", "COMPANY_RETURN_STATE_DECLARED", "COMPANY_RETURN_STATE_PRELODGE_PENDING", "COMPANY_RETURN_STATE_PRELODGE_REVIEW", "COMPANY_RETURN_STATE_READY_TO_LODGE", "COMPANY_RETURN_STATE_PRELODGE_OUTCOME_UNKNOWN", "COMPANY_RETURN_STATE_LODGE_PENDING", "COMPANY_RETURN_STATE_DELIVERED", "COMPANY_RETURN_STATE_LODGE_REJECTED", "COMPANY_RETURN_STATE_LODGE_OUTCOME_UNKNOWN", "COMPANY_RETURN_STATE_REPLACED", "COMPANY_RETURN_STATE_SUPERSEDED_BY_AMENDMENT"}},
		{"tammy.v1.CompanyReturnRelationshipKind", []string{"COMPANY_RETURN_RELATIONSHIP_KIND_UNSPECIFIED", "COMPANY_RETURN_RELATIONSHIP_KIND_ORIGINAL", "COMPANY_RETURN_RELATIONSHIP_KIND_REPLACEMENT", "COMPANY_RETURN_RELATIONSHIP_KIND_AMENDMENT"}},
		{"tammy.v1.CompanyReturnOperationType", []string{"COMPANY_RETURN_OPERATION_TYPE_UNSPECIFIED", "COMPANY_RETURN_OPERATION_TYPE_PRELODGE", "COMPANY_RETURN_OPERATION_TYPE_LODGE", "COMPANY_RETURN_OPERATION_TYPE_STATUS", "COMPANY_RETURN_OPERATION_TYPE_RECONCILE"}},
		{"tammy.v1.CompanyReturnOperationOutcome", []string{"COMPANY_RETURN_OPERATION_OUTCOME_UNSPECIFIED", "COMPANY_RETURN_OPERATION_OUTCOME_SUCCESS", "COMPANY_RETURN_OPERATION_OUTCOME_WARNINGS", "COMPANY_RETURN_OPERATION_OUTCOME_REJECTED", "COMPANY_RETURN_OPERATION_OUTCOME_OUTCOME_UNKNOWN"}},
		{"tammy.v1.CompanyReturnAttemptState", []string{"COMPANY_RETURN_ATTEMPT_STATE_UNSPECIFIED", "COMPANY_RETURN_ATTEMPT_STATE_PREPARED", "COMPANY_RETURN_ATTEMPT_STATE_DISPATCHING", "COMPANY_RETURN_ATTEMPT_STATE_NOT_DISPATCHED", "COMPANY_RETURN_ATTEMPT_STATE_RESULT_RECORDED", "COMPANY_RETURN_ATTEMPT_STATE_OUTCOME_UNKNOWN", "COMPANY_RETURN_ATTEMPT_STATE_COMMITTED", "COMPANY_RETURN_ATTEMPT_STATE_ABORTED"}},
	}

	for _, test := range tests {
		t.Run(string(test.name), func(t *testing.T) {
			descriptor, err := protoregistry.GlobalFiles.FindDescriptorByName(test.name)
			if err != nil {
				t.Fatalf("enum %s missing: %v", test.name, err)
			}
			enum, ok := descriptor.(protoreflect.EnumDescriptor)
			if !ok {
				t.Fatalf("descriptor %s is not an enum", test.name)
			}
			if enum.Values().Len() != len(test.values) {
				t.Fatalf("%s value count = %d, want %d", test.name, enum.Values().Len(), len(test.values))
			}
			for index, want := range test.values {
				if got := string(enum.Values().Get(index).Name()); got != want {
					t.Errorf("%s value %d = %q, want %q", test.name, index, got, want)
				}
			}
		})
	}
}

func TestCompanyEOFYProtoDependencyDirectionIsAcyclic(t *testing.T) {
	tax, err := protoregistry.GlobalFiles.FindFileByPath("tammy/v1/company_tax.proto")
	if err != nil {
		t.Fatalf("company tax descriptor missing: %v", err)
	}
	for index := range tax.Imports().Len() {
		if tax.Imports().Get(index).Path() == "tammy/v1/company_return_submission.proto" {
			t.Fatal("company_tax.proto must not import company_return_submission.proto")
		}
	}

	submission, err := protoregistry.GlobalFiles.FindFileByPath("tammy/v1/company_return_submission.proto")
	if err != nil {
		t.Fatalf("company return submission descriptor missing: %v", err)
	}
	foundTax := false
	for index := range submission.Imports().Len() {
		foundTax = foundTax || submission.Imports().Get(index).Path() == "tammy/v1/company_tax.proto"
	}
	if !foundTax {
		t.Fatal("company_return_submission.proto must import company_tax.proto")
	}
}

func TestCompanyEOFYTransitionFixturesMatchExactAuthority(t *testing.T) {
	assertCompanyEOFYTransitionFixture(t, "reporting", map[string][]string{
		"tammy.v1.FinancialCloseState": {
			"FINANCIAL_CLOSE_STATE_BLOCKED->FINANCIAL_CLOSE_STATE_COLLECTING",
			"FINANCIAL_CLOSE_STATE_BLOCKED->FINANCIAL_CLOSE_STATE_REVIEW_READY",
			"FINANCIAL_CLOSE_STATE_COLLECTING->FINANCIAL_CLOSE_STATE_BLOCKED",
			"FINANCIAL_CLOSE_STATE_COLLECTING->FINANCIAL_CLOSE_STATE_REVIEW_READY",
			"FINANCIAL_CLOSE_STATE_FROZEN->FINANCIAL_CLOSE_STATE_COLLECTING",
			"FINANCIAL_CLOSE_STATE_REVIEW_READY->FINANCIAL_CLOSE_STATE_BLOCKED",
			"FINANCIAL_CLOSE_STATE_REVIEW_READY->FINANCIAL_CLOSE_STATE_COLLECTING",
			"FINANCIAL_CLOSE_STATE_REVIEW_READY->FINANCIAL_CLOSE_STATE_FROZEN",
		},
	})
	assertCompanyEOFYTransitionFixture(t, "tax", map[string][]string{
		"tammy.v1.CompanyReturnAttemptState": {
			"COMPANY_RETURN_ATTEMPT_STATE_DISPATCHING->COMPANY_RETURN_ATTEMPT_STATE_NOT_DISPATCHED",
			"COMPANY_RETURN_ATTEMPT_STATE_DISPATCHING->COMPANY_RETURN_ATTEMPT_STATE_OUTCOME_UNKNOWN",
			"COMPANY_RETURN_ATTEMPT_STATE_DISPATCHING->COMPANY_RETURN_ATTEMPT_STATE_RESULT_RECORDED",
			"COMPANY_RETURN_ATTEMPT_STATE_NOT_DISPATCHED->COMPANY_RETURN_ATTEMPT_STATE_ABORTED",
			"COMPANY_RETURN_ATTEMPT_STATE_NOT_DISPATCHED->COMPANY_RETURN_ATTEMPT_STATE_PREPARED",
			"COMPANY_RETURN_ATTEMPT_STATE_OUTCOME_UNKNOWN->COMPANY_RETURN_ATTEMPT_STATE_RESULT_RECORDED",
			"COMPANY_RETURN_ATTEMPT_STATE_PREPARED->COMPANY_RETURN_ATTEMPT_STATE_ABORTED",
			"COMPANY_RETURN_ATTEMPT_STATE_PREPARED->COMPANY_RETURN_ATTEMPT_STATE_DISPATCHING",
			"COMPANY_RETURN_ATTEMPT_STATE_RESULT_RECORDED->COMPANY_RETURN_ATTEMPT_STATE_COMMITTED",
		},
		"tammy.v1.CompanyReturnState": {
			"COMPANY_RETURN_STATE_BLOCKED->COMPANY_RETURN_STATE_COLLECTING",
			"COMPANY_RETURN_STATE_BLOCKED->COMPANY_RETURN_STATE_REVIEW_READY",
			"COMPANY_RETURN_STATE_COLLECTING->COMPANY_RETURN_STATE_BLOCKED",
			"COMPANY_RETURN_STATE_COLLECTING->COMPANY_RETURN_STATE_REVIEW_READY",
			"COMPANY_RETURN_STATE_DECLARED->COMPANY_RETURN_STATE_PRELODGE_PENDING",
			"COMPANY_RETURN_STATE_DECLARED->COMPANY_RETURN_STATE_REPLACED",
			"COMPANY_RETURN_STATE_DELIVERED->COMPANY_RETURN_STATE_SUPERSEDED_BY_AMENDMENT",
			"COMPANY_RETURN_STATE_LODGE_OUTCOME_UNKNOWN->COMPANY_RETURN_STATE_DELIVERED",
			"COMPANY_RETURN_STATE_LODGE_OUTCOME_UNKNOWN->COMPANY_RETURN_STATE_LODGE_REJECTED",
			"COMPANY_RETURN_STATE_LODGE_PENDING->COMPANY_RETURN_STATE_DELIVERED",
			"COMPANY_RETURN_STATE_LODGE_PENDING->COMPANY_RETURN_STATE_LODGE_OUTCOME_UNKNOWN",
			"COMPANY_RETURN_STATE_LODGE_PENDING->COMPANY_RETURN_STATE_LODGE_REJECTED",
			"COMPANY_RETURN_STATE_LODGE_PENDING->COMPANY_RETURN_STATE_READY_TO_LODGE",
			"COMPANY_RETURN_STATE_LODGE_REJECTED->COMPANY_RETURN_STATE_REPLACED",
			"COMPANY_RETURN_STATE_PRELODGE_OUTCOME_UNKNOWN->COMPANY_RETURN_STATE_BLOCKED",
			"COMPANY_RETURN_STATE_PRELODGE_OUTCOME_UNKNOWN->COMPANY_RETURN_STATE_DECLARED",
			"COMPANY_RETURN_STATE_PRELODGE_OUTCOME_UNKNOWN->COMPANY_RETURN_STATE_PRELODGE_REVIEW",
			"COMPANY_RETURN_STATE_PRELODGE_OUTCOME_UNKNOWN->COMPANY_RETURN_STATE_READY_TO_LODGE",
			"COMPANY_RETURN_STATE_PRELODGE_PENDING->COMPANY_RETURN_STATE_BLOCKED",
			"COMPANY_RETURN_STATE_PRELODGE_PENDING->COMPANY_RETURN_STATE_DECLARED",
			"COMPANY_RETURN_STATE_PRELODGE_PENDING->COMPANY_RETURN_STATE_PRELODGE_OUTCOME_UNKNOWN",
			"COMPANY_RETURN_STATE_PRELODGE_PENDING->COMPANY_RETURN_STATE_PRELODGE_REVIEW",
			"COMPANY_RETURN_STATE_PRELODGE_PENDING->COMPANY_RETURN_STATE_READY_TO_LODGE",
			"COMPANY_RETURN_STATE_PRELODGE_REVIEW->COMPANY_RETURN_STATE_DECLARED",
			"COMPANY_RETURN_STATE_PRELODGE_REVIEW->COMPANY_RETURN_STATE_REPLACED",
			"COMPANY_RETURN_STATE_READY_TO_LODGE->COMPANY_RETURN_STATE_LODGE_PENDING",
			"COMPANY_RETURN_STATE_READY_TO_LODGE->COMPANY_RETURN_STATE_REPLACED",
			"COMPANY_RETURN_STATE_REVIEW_READY->COMPANY_RETURN_STATE_BLOCKED",
			"COMPANY_RETURN_STATE_REVIEW_READY->COMPANY_RETURN_STATE_COLLECTING",
			"COMPANY_RETURN_STATE_REVIEW_READY->COMPANY_RETURN_STATE_DECLARED",
		},
	})
}

func assertCompanyEOFYTransitionFixture(t *testing.T, slice string, want map[string][]string) {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve company EOFY transition fixture path")
	}
	fixturePath := filepath.Join(filepath.Dir(sourceFile), "../../../../test/fixtures", slice, "transitions.pb.json")
	source, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read %s transition fixture: %v", slice, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(source))
	decoder.DisallowUnknownFields()
	var fixture companyEOFYTransitionFixture
	if err := decoder.Decode(&fixture); err != nil {
		t.Fatalf("decode %s transition fixture: %v", slice, err)
	}
	if fixture.SchemaVersion != 1 {
		t.Fatalf("%s transition fixture schemaVersion = %d, want 1", slice, fixture.SchemaVersion)
	}

	got := make([]string, 0, len(fixture.Transitions))
	for _, transition := range fixture.Transitions {
		id := transition.Enum + "." + transition.Transition
		got = append(got, id)
		descriptor, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(transition.Enum))
		if err != nil {
			t.Fatalf("find transition enum %q: %v", transition.Enum, err)
		}
		enum, ok := descriptor.(protoreflect.EnumDescriptor)
		if !ok {
			t.Fatalf("transition descriptor %q is not an enum", transition.Enum)
		}
		values := strings.Split(transition.Transition, "->")
		if len(values) != 2 {
			t.Fatalf("transition %q must contain exactly one ->", id)
		}
		for _, value := range values {
			if enum.Values().ByName(protoreflect.Name(value)) == nil {
				t.Fatalf("transition %q references unknown enum value %q", id, value)
			}
			if strings.HasSuffix(value, "_UNSPECIFIED") {
				t.Fatalf("transition %q references an unspecified sentinel", id)
			}
		}
	}
	if !sort.StringsAreSorted(got) {
		t.Fatalf("%s transition fixture is not sorted by fully-qualified transition ID", slice)
	}

	wantIDs := make([]string, 0, len(fixture.Transitions))
	for enum, transitions := range want {
		for _, transition := range transitions {
			wantIDs = append(wantIDs, enum+"."+transition)
		}
	}
	sort.Strings(wantIDs)
	if strings.Join(got, "\n") != strings.Join(wantIDs, "\n") {
		t.Fatalf("%s transition fixture differs from exact lifecycle authority\ngot:\n%s\nwant:\n%s", slice, strings.Join(got, "\n"), strings.Join(wantIDs, "\n"))
	}
}
