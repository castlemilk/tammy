package baspreparation

import (
	"bytes"
	"crypto/sha256"
	"testing"

	"buf.build/go/protovalidate"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

func TestBASRawSourceRefPreservesExactComponentTupleAndAbsentCompatibility(t *testing.T) {
	original := &tammyv1.BasRawSourceRef{Type: "purchase_payment_allocations_v1", Id: "opaque/payment/1", ComputedProofHash: bytes.Repeat([]byte{1}, 32),
		ProofScope: tammyv1.BasSourceProofScope_BAS_SOURCE_PROOF_SCOPE_SOURCE_ROW_PROJECTION}
	before, err := proto.MarshalOptions{Deterministic: true}.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	sequence, relatedSequence, relatedID := uint64(1), uint64(2), "opaque/posting/1"
	component := proto.Clone(original).(*tammyv1.BasRawSourceRef)
	component.ComponentSequence, component.RelatedId, component.RelatedSequence = &sequence, &relatedID, &relatedSequence
	if !validBASRawSourceRef(component) || protovalidate.Validate(component) != nil {
		t.Fatal("valid opaque component tuple rejected")
	}
	wire, err := proto.MarshalOptions{Deterministic: true}.Marshal(component)
	if err != nil {
		t.Fatal(err)
	}
	decoded := &tammyv1.BasRawSourceRef{}
	if proto.Unmarshal(wire, decoded) != nil || !proto.Equal(decoded, component) {
		t.Fatal("exact own and referenced sequences did not round trip")
	}
	beforeHash, componentHash := sha256.Sum256(before), sha256.Sum256(wire)
	if beforeHash == componentHash {
		t.Fatal("component identity was not committed")
	}
	other := proto.Clone(component).(*tammyv1.BasRawSourceRef)
	other.ComponentSequence, other.RelatedSequence = &relatedSequence, &sequence
	otherWire, _ := proto.MarshalOptions{Deterministic: true}.Marshal(other)
	if bytes.Equal(wire, otherWire) {
		t.Fatal("own and referenced sequences were coalesced")
	}
	decoded.ComponentSequence, decoded.RelatedId, decoded.RelatedSequence = nil, nil, nil
	after, _ := proto.MarshalOptions{Deterministic: true}.Marshal(decoded)
	if !bytes.Equal(before, after) {
		t.Fatal("absent additive component fields changed historical identity bytes")
	}
	for _, mutate := range []func(*tammyv1.BasRawSourceRef){
		func(v *tammyv1.BasRawSourceRef) { v.ComponentSequence = proto.Uint64(0) },
		func(v *tammyv1.BasRawSourceRef) { v.RelatedId = proto.String(" \t") },
		func(v *tammyv1.BasRawSourceRef) { v.RelatedSequence = proto.Uint64(0) },
		func(v *tammyv1.BasRawSourceRef) { v.RelatedId = nil },
	} {
		bad := proto.Clone(component).(*tammyv1.BasRawSourceRef)
		mutate(bad)
		if validBASRawSourceRef(bad) || protovalidate.Validate(bad) == nil {
			t.Fatal("malformed component tuple accepted")
		}
	}
}
