package faults_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/platform/faults"
)

func TestFaultExposesStableCodeWithoutMetadataInError(t *testing.T) {
	fault := faults.New(faults.CodeStaleVersion, map[string]string{
		"resource_id": "01890f3c-7b2e-7cc4-98c4-dc0c0c07398f",
		"secret":      "must-not-appear",
	})
	if fault.Error() != "STALE_VERSION" || fault.Code() != faults.CodeStaleVersion {
		t.Fatalf("fault = %q/%q", fault.Error(), fault.Code())
	}
	if strings.Contains(fault.Error(), "must-not-appear") {
		t.Fatalf("fault error exposed metadata: %q", fault.Error())
	}
	metadata := fault.Metadata()
	metadata["resource_id"] = "mutated"
	if fault.Metadata()["resource_id"] == "mutated" {
		t.Fatal("fault metadata was not defensively copied")
	}
}

func TestFaultCodeVocabularyIsClosedAndUnknownValuesNormalizeToInternal(t *testing.T) {
	if kind := reflect.TypeOf(faults.CodeInternal).Kind(); kind == reflect.String {
		t.Fatalf("fault Code remains caller-controlled string kind: %s", kind)
	}
	unknown := faults.Code(255)
	fault := faults.New(unknown, map[string]string{"secret": "hostile-value"})
	if fault.Code() != faults.CodeInternal || fault.Error() != "INTERNAL" {
		t.Fatalf("unknown code exposed as %q/%v", fault.Error(), fault.Code())
	}
	if strings.Contains(fault.Error(), "hostile") || strings.Contains(fault.Error(), string(rune(255))) {
		t.Fatalf("unknown code leaked caller-controlled text: %q", fault.Error())
	}
}

func TestFaultSupportsTypedErrorMatchingAndCodeExtraction(t *testing.T) {
	fault := faults.New(faults.CodePermissionDenied, nil)
	wrapped := errors.Join(errors.New("outer"), fault)
	if !errors.Is(wrapped, faults.New(faults.CodePermissionDenied, nil)) {
		t.Fatal("fault code did not participate in errors.Is")
	}
	code, ok := faults.CodeOf(wrapped)
	if !ok || code != faults.CodePermissionDenied {
		t.Fatalf("CodeOf = %q, %t", code, ok)
	}
	if _, ok := faults.CodeOf(errors.New("ordinary")); ok {
		t.Fatal("ordinary error reported a fault code")
	}
}

func TestTypedNilFaultAndCodeExtractionAreSafe(t *testing.T) {
	var fault *faults.Fault
	if fault.Error() != "INTERNAL" || fault.Code() != faults.CodeInternal || fault.Metadata() != nil {
		t.Fatalf("typed-nil fault did not normalize safely")
	}
	if errors.Is(fault, faults.New(faults.CodeInternal, nil)) {
		t.Fatal("typed-nil fault matched a concrete internal fault")
	}
	if code, ok := faults.CodeOf(fault); ok || code != faults.CodeInternal {
		t.Fatalf("CodeOf(typed nil) = %v, %t; want INTERNAL, false", code, ok)
	}
}
