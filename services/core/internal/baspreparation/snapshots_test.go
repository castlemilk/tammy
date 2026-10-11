package baspreparation

import (
	"bytes"
	"crypto/sha256"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

func TestSnapshotProfileSealHashesWholeRetainedBodyWithoutChangingCaller(t *testing.T) {
	input := obligationsProfile(testUUID(1))
	input.ContentHash = nil
	input.Profile.Gst.Evidence = []*tammyv1.SourceRef{sourceRef("document", testUUID(1001), 3, 45)}
	before := proto.Clone(input).(*tammyv1.BasObligationsProfile)
	wire, hash, err := sealProfile(input)
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(input, before) {
		t.Fatal("sealing must not mutate the service-owned input")
	}
	canonical, err := (proto.MarshalOptions{Deterministic: true}).Marshal(before)
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(canonical)
	if !bytes.Equal(hash, want[:]) {
		t.Fatal("the digest must bind the complete body with only its own hash absent")
	}
	exact := &tammyv1.SourceRef{Type: "bas_obligations_profile", Id: input.ProfileId, Revision: input.Revision, ContentHash: bytes.Clone(hash)}
	retained, err := openProfile(wire, input.OrganisationId, exact)
	if err != nil || !bytes.Equal(retained.GetContentHash(), hash) || !proto.Equal(retained.GetProfile(), input.Profile) {
		t.Fatalf("exact retained profile read = %v, %v", retained, err)
	}
	retained.Profile.Gst.Evidence[0].ContentHash[0] ^= 0xff
	retained.ContentHash[0] ^= 0xff
	again, err := openProfile(wire, input.OrganisationId, exact)
	if err != nil || !bytes.Equal(again.ContentHash, hash) || !proto.Equal(again.Profile.Gst.Evidence[0], input.Profile.Gst.Evidence[0]) {
		t.Fatal("read results must not alias the encrypted row, caller key, or another read")
	}
}

func TestSnapshotPreparationExactHistoricalReadRejectsWrongScopeVersionAndHash(t *testing.T) {
	input := readyReviewInput().Preparation
	input.ContentHash = nil
	wire, hash, err := sealPreparation(input)
	if err != nil {
		t.Fatal(err)
	}
	exact := &tammyv1.SourceRef{Type: "bas_preparation", Id: input.PreparationId, Revision: input.Version, ContentHash: hash}
	for _, test := range []struct {
		name   string
		org    string
		change func(*tammyv1.SourceRef)
	}{
		{name: "other organisation", org: testUUID(1002)},
		{name: "other ID", change: func(ref *tammyv1.SourceRef) { ref.Id = testUUID(1003) }},
		{name: "other version", change: func(ref *tammyv1.SourceRef) { ref.Revision++ }},
		{name: "other hash", change: func(ref *tammyv1.SourceRef) { ref.ContentHash[0] ^= 0xff }},
		{name: "other type", change: func(ref *tammyv1.SourceRef) { ref.Type = "bas_calculation" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			org := input.OrganisationId
			if test.org != "" {
				org = test.org
			}
			ref := proto.Clone(exact).(*tammyv1.SourceRef)
			if test.change != nil {
				test.change(ref)
			}
			if value, err := openPreparation(wire, org, ref); err == nil || value != nil {
				t.Fatal("a retained read must fail closed for another exact identity or organisation")
			}
		})
	}
	retained, err := openPreparation(wire, input.OrganisationId, exact)
	if err != nil || retained.Version != input.Version {
		t.Fatalf("historical preparation must remain readable without consulting a current head: %v", err)
	}
}

func TestSnapshotOpenRejectsCorruptNoncanonicalOrMalformedBody(t *testing.T) {
	input := readyReviewInput().Preparation
	input.ContentHash = nil
	wire, hash, err := sealPreparation(input)
	if err != nil {
		t.Fatal(err)
	}
	exact := &tammyv1.SourceRef{Type: "bas_preparation", Id: input.PreparationId, Revision: input.Version, ContentHash: hash}
	changed := &tammyv1.BasPreparation{}
	if err := proto.Unmarshal(wire, changed); err != nil {
		t.Fatal(err)
	}
	changed.InputHash[0] ^= 0xff
	tampered, err := (proto.MarshalOptions{Deterministic: true}).Marshal(changed)
	if err != nil {
		t.Fatal(err)
	}
	// A duplicate scalar field decodes successfully but is not the retained canonical encoding.
	duplicateVersion := append(bytes.Clone(wire), byte(3<<3), byte(input.Version))
	for _, test := range []struct {
		name string
		wire []byte
	}{
		{"changed stable input", tampered}, {"duplicate scalar", duplicateVersion},
		{"truncated body", wire[:len(wire)-1]}, {"empty body", nil},
		{"oversized body", make([]byte, 4*1024*1024+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if value, err := openPreparation(test.wire, input.OrganisationId, exact); err == nil || value != nil {
				t.Fatal("corrupt or noncanonical retained data must not produce a partial snapshot")
			}
		})
	}
}

func TestSnapshotSealRejectsMismatchedOwnHashAndInvalidRetainedPayload(t *testing.T) {
	input := readyReviewInput().Preparation
	input.ContentHash = nil
	wire, hash, err := sealPreparation(input)
	if err != nil {
		t.Fatal(err)
	}
	input.ContentHash = bytes.Clone(hash)
	if again, got, err := sealPreparation(input); err != nil || !bytes.Equal(again, wire) || !bytes.Equal(got, hash) {
		t.Fatal("an already correctly sealed immutable body must retain its exact bytes")
	}
	input.ContentHash[0] ^= 0xff
	if _, _, err := sealPreparation(input); err == nil {
		t.Fatal("sealing must not silently repair an inconsistent retained own hash")
	}
	input.ContentHash = nil
	input.CreatedAt = nil
	if _, _, err := sealPreparation(input); err == nil {
		t.Fatal("missing required retained metadata must fail generated validation")
	}
}

func TestSnapshotFrozenHashPreservesEveryNestedCalculationAndSourceHash(t *testing.T) {
	input := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE).Attempt.Frozen
	input.ContentHash = nil
	before := proto.Clone(input).(*tammyv1.BasFrozenBas)
	wire, hash, err := sealFrozen(input)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := (proto.MarshalOptions{Deterministic: true}).Marshal(before)
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(canonical)
	if !bytes.Equal(hash, want[:]) || !proto.Equal(input, before) {
		t.Fatal("hashing the frozen parent must clear only its own hash and preserve nested identities")
	}
	exact := &tammyv1.SourceRef{Type: "bas_frozen", Id: input.FrozenId, Revision: 1, ContentHash: hash}
	retained, err := openFrozen(wire, input.OrganisationId, exact)
	if err != nil || !proto.Equal(retained.CalculationSnapshot, input.CalculationSnapshot) {
		t.Fatalf("frozen read lost the exact nested calculation: %v", err)
	}
}
