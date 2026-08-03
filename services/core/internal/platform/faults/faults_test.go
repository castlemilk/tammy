package faults_test

import (
	"errors"
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
