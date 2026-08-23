package contracts_test

import (
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

func TestGetOrganisationCurrentVerificationIsStateExpiryOnly(t *testing.T) {
	file, err := protoregistry.GlobalFiles.FindFileByPath("tammy/v1/organisation.proto")
	if err != nil {
		t.Fatal(err)
	}
	summary := file.Messages().ByName("OrganisationVerificationSummary")
	if summary == nil || summary.Fields().Len() != 2 ||
		summary.Fields().ByName("state").Kind() != protoreflect.EnumKind ||
		summary.Fields().ByName("expires_at").Message().FullName() != "google.protobuf.Timestamp" {
		t.Fatalf("verification summary schema = %v", summary)
	}
	for _, forbidden := range []protoreflect.Name{"id", "organisation_id", "evidence_object_id", "source", "content_hash", "semantic_hash"} {
		if summary.Fields().ByName(forbidden) != nil {
			t.Fatalf("verification summary exposes %s", forbidden)
		}
	}
	response := file.Messages().ByName("GetOrganisationResponse")
	if got := response.Fields().ByName("current_verification").Message().FullName(); got != "tammy.v1.OrganisationVerificationSummary" {
		t.Fatalf("current_verification type = %s", got)
	}
}
