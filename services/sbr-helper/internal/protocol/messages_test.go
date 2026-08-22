package protocol

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protowire"
)

var protocolNow = time.UnixMilli(1_700_000_000_000)

const (
	testRequestID   = "018bcfe5-6800-7000-8000-000000000001"
	testOperationID = "018bcfe5-6800-7000-8000-000000000002"
	testWorkspaceID = "018bcfe5-6800-7000-8000-000000000003"
	testOrgID       = "018bcfe5-6800-7000-8000-000000000004"
	testPendingID   = "018bcfe5-6800-7000-8000-000000000005"
)

func baseRequest(operation Operation) Request {
	return Request{
		ProtocolVersion: ProtocolVersion,
		RequestID:       testRequestID,
		Operation:       operation,
		DeadlineMillis:  protocolNow.Add(time.Minute).UnixMilli(),
		Environment:     EnvironmentSimulator,
		WorkspaceID:     testWorkspaceID,
		OrganisationID:  testOrgID,
		CanonicalABN:    "51824753556",
		OpaqueScope:     bytes.Repeat([]byte{0x5a}, 32),
	}
}

func TestProtocolRequestOperationAndMutationNumericValues(t *testing.T) {
	if OperationStatus != 1 || OperationUnlock != 2 || OperationFixture != 3 ||
		OperationPrepareMutation != 4 || OperationCommitMutation != 5 ||
		OperationAbortMutation != 6 || OperationReconcileMutation != 7 {
		t.Fatal("request operation numbers changed")
	}
	if MutationImportCredential != 1 || MutationReplaceCredential != 2 ||
		MutationRemoveCredential != 3 || MutationImportProductID != 4 || MutationRemoveProductID != 5 {
		t.Fatal("mutation kind numbers changed")
	}
	if EnvironmentSimulator != 1 || EnvironmentEVTE != 2 {
		t.Fatal("environment numbers changed")
	}
	if SimulatorAccepted != 1 || SimulatorNotStarted != 2 || SimulatorMaybeSent != 3 ||
		SimulatorMalformedResponse != 4 || SimulatorHelperDeath != 5 || SimulatorTimeout != 6 || SimulatorUnknown != 7 {
		t.Fatal("simulator case numbers changed")
	}
	if OutcomeOK != 1 || OutcomeError != 2 || OutcomePending != 3 {
		t.Fatal("outcome numbers changed")
	}
	if ResultReady != 1 || ResultCredentialLocked != 2 || ResultRegistrationRequired != 3 ||
		ResultMutationCommitted != 4 || ResultMutationAborted != 5 || ResultRecoveryRequired != 6 || ResultFixtureSelected != 7 {
		t.Fatal("redacted result numbers changed")
	}
}

func TestProtocolRequestFieldNumbersAreLocked(t *testing.T) {
	fields := []protowire.Number{
		requestFieldVersion, requestFieldRequestID, requestFieldOperation, requestFieldDeadline,
		requestFieldEnvironment, requestFieldWorkspaceID, requestFieldOrganisationID, requestFieldABN,
		requestFieldOpaqueScope, requestFieldOperationID, requestFieldMutationKind, requestFieldSelectedPath,
		requestFieldBookmark, requestFieldPassword, requestFieldProductID, requestFieldProductScope,
		requestFieldServiceID, requestFieldEndpointProfile, requestFieldSimulatorCase,
	}
	for index, field := range fields {
		if want := protowire.Number(index + 1); field != want {
			t.Fatalf("field %d = %d, want %d", index, field, want)
		}
	}
}

func TestProtocolResponseFieldNumbersAreLocked(t *testing.T) {
	fields := []protowire.Number{
		responseFieldRequestID, responseFieldOutcome, responseFieldRedactedResult,
		responseFieldStableErrorCode, responseFieldPendingItemID,
	}
	for index, field := range fields {
		if want := protowire.Number(index + 1); field != want {
			t.Fatalf("response field %d = %d, want %d", index, field, want)
		}
	}
	responseType := reflect.TypeOf(Response{})
	wantNames := []string{"RequestID", "Outcome", "RedactedResult", "StableErrorCode", "PendingItemID"}
	if responseType.NumField() != len(wantNames) {
		t.Fatalf("response fields = %d, want exact five-field response", responseType.NumField())
	}
	for index, want := range wantNames {
		if got := responseType.Field(index).Name; got != want {
			t.Fatalf("response field %d = %s, want %s", index, got, want)
		}
	}
}

func TestProtocolRequestCombinationMatrix(t *testing.T) {
	credentialImport := baseRequest(OperationPrepareMutation)
	credentialImport.OperationID = testOperationID
	credentialImport.MutationKind = MutationImportCredential
	credentialImport.SelectedLocalPath = "/tmp/credential.p12"
	credentialImport.Bookmark = []byte("bookmark")
	credentialImport.TransientPassword = []byte{}

	productImport := baseRequest(OperationPrepareMutation)
	productImport.OperationID = testOperationID
	productImport.MutationKind = MutationImportProductID
	productImport.TransientProductID = []byte("secret-product-id")
	productImport.ProductScope = "PAYROLL"
	productImport.ServiceID = "SBR_GST"

	fixture := baseRequest(OperationFixture)
	fixture.WorkspaceID, fixture.OrganisationID, fixture.CanonicalABN, fixture.OpaqueScope = "", "", "", nil
	fixture.SimulatorCase = SimulatorAccepted

	evteStatus := baseRequest(OperationStatus)
	evteStatus.Environment = EnvironmentEVTE
	evteStatus.EndpointProfile = []byte("signed-endpoint-profile")

	tests := []struct {
		name    string
		request Request
		valid   bool
	}{
		{name: "status simulator", request: baseRequest(OperationStatus), valid: true},
		{name: "unlock simulator", request: baseRequest(OperationUnlock), valid: true},
		{name: "fixture accepted", request: fixture, valid: true},
		{name: "fixture unknown reserved for response recovery", request: withFixtureCase(fixture, SimulatorUnknown)},
		{name: "evte status authenticated profile", request: evteStatus, valid: true},
		{name: "evte status missing profile", request: withEndpoint(evteStatus, nil)},
		{name: "simulator rejects endpoint", request: withEndpoint(baseRequest(OperationStatus), []byte("profile"))},
		{name: "credential import", request: credentialImport, valid: true},
		{name: "credential import missing path", request: withPath(credentialImport, "")},
		{name: "credential path only on prepare import", request: mutationRequest(OperationCommitMutation, MutationImportCredential, func(r *Request) { r.SelectedLocalPath = "/tmp/a" })},
		{name: "product import", request: productImport, valid: true},
		{name: "product import missing secret", request: withProductID(productImport, nil)},
		{name: "product remove", request: mutationRequest(OperationPrepareMutation, MutationRemoveProductID, nil), valid: true},
		{name: "product commit retains service scope", request: mutationRequest(OperationCommitMutation, MutationImportProductID, nil), valid: true},
		{name: "credential remove", request: mutationRequest(OperationPrepareMutation, MutationRemoveCredential, nil), valid: true},
		{name: "reconcile credential", request: mutationRequest(OperationReconcileMutation, MutationReplaceCredential, nil), valid: true},
		{name: "read rejects operation id", request: withOperationID(baseRequest(OperationStatus), testOperationID)},
		{name: "mutation requires operation id", request: withOperationID(mutationRequest(OperationAbortMutation, MutationRemoveCredential, nil), "")},
		{name: "mutation requires kind", request: withMutationKind(mutationRequest(OperationAbortMutation, MutationRemoveCredential, nil), 0)},
		{name: "fixture rejects scope", request: withWorkspace(fixture, testWorkspaceID)},
		{name: "product fields reject credential mutation", request: mutationRequest(OperationPrepareMutation, MutationRemoveCredential, func(r *Request) { r.ProductScope = "PAYROLL" })},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := EncodeRequest(tt.request, protocolNow)
			if tt.valid {
				if err != nil {
					t.Fatalf("EncodeRequest: %v", err)
				}
				decoded, err := DecodeRequest(encoded, protocolNow)
				if err != nil {
					t.Fatalf("DecodeRequest: %v", err)
				}
				again, err := EncodeRequest(decoded, protocolNow)
				if err != nil || !bytes.Equal(again, encoded) {
					t.Fatalf("canonical round trip = %x, %v; want %x", again, err, encoded)
				}
				return
			}
			assertProtocolError(t, err, "REQUEST_INVALID")
		})
	}
}

func TestProtocolProductAndServiceIdentifiersMatchPublicContract(t *testing.T) {
	request := mutationRequest(OperationPrepareMutation, MutationImportProductID, nil)
	request.ProductScope = "pay.roll/v2:demo-product"
	request.ServiceID = "sbr.gst-service-v1"
	if _, err := EncodeRequest(request, protocolNow); err != nil {
		t.Fatalf("legitimate public identifiers rejected: %v", err)
	}
	request.ProductScope = strings.Repeat("é", 128)
	request.ServiceID = "service"
	if _, err := EncodeRequest(request, protocolNow); err != nil {
		t.Fatalf("128-rune identifier rejected: %v", err)
	}
	for _, invalid := range []string{"bad\u0085scope", "bad\x00scope", strings.Repeat("é", 129)} {
		request.ProductScope = invalid
		if _, err := EncodeRequest(request, protocolNow); err == nil {
			t.Fatalf("invalid public identifier accepted: %q", invalid)
		}
	}
}

func TestProtocolRequestRejectsInvalidIdentifiersBoundsAndDeadlines(t *testing.T) {
	valid := baseRequest(OperationStatus)
	tests := []Request{
		withVersion(valid, 2),
		withRequestID(valid, "018BCFE5-6800-7000-8000-000000000001"),
		withRequestID(valid, "00000000-0000-0000-0000-000000000000"),
		withRequestID(valid, "018bcfe5-6800-6000-8000-000000000001"),
		withOperation(valid, 99),
		withEnvironment(valid, 3),
		withDeadline(valid, protocolNow.UnixMilli()),
		withDeadline(valid, protocolNow.Add(5*time.Minute+time.Millisecond).UnixMilli()),
		withWorkspace(valid, "not-a-uuid"),
		withOrganisation(valid, "018bcfe5-6800-7000-7000-000000000004"),
		withABN(valid, "51 824 753 556"),
		withABN(valid, "51824753557"),
		withScope(valid, bytes.Repeat([]byte{1}, 31)),
	}
	for i, request := range tests {
		if _, err := EncodeRequest(request, protocolNow); err == nil {
			t.Fatalf("invalid request %d accepted", i)
		} else {
			assertProtocolError(t, err, "REQUEST_INVALID")
		}
	}

	tooLongPath := "/" + strings.Repeat("a", maxPathBytes)
	request := mutationRequest(OperationPrepareMutation, MutationImportCredential, func(r *Request) { r.SelectedLocalPath = tooLongPath })
	assertProtocolErrorFromEncode(t, request)
	request = mutationRequest(OperationPrepareMutation, MutationImportCredential, func(r *Request) { r.SelectedLocalPath = "/tmp/a\n" })
	assertProtocolErrorFromEncode(t, request)
	request = mutationRequest(OperationPrepareMutation, MutationImportCredential, func(r *Request) { r.SelectedLocalPath = "/tmp/a\u0085b" })
	assertProtocolErrorFromEncode(t, request)
	request = mutationRequest(OperationPrepareMutation, MutationImportCredential, func(r *Request) { r.SelectedLocalPath = "/tmp/a\x00b" })
	assertProtocolErrorFromEncode(t, request)
	request = mutationRequest(OperationPrepareMutation, MutationImportCredential, func(r *Request) { r.SelectedLocalPath = "relative.p12" })
	assertProtocolErrorFromEncode(t, request)
	request = mutationRequest(OperationPrepareMutation, MutationImportCredential, func(r *Request) { r.SelectedLocalPath = "/tmp/café/証明.p12" })
	if _, err := EncodeRequest(request, protocolNow); err != nil {
		t.Fatalf("valid Unicode path rejected: %v", err)
	}
	request = mutationRequest(OperationPrepareMutation, MutationImportCredential, func(r *Request) { r.SelectedLocalPath = "/tmp/a"; r.Bookmark = make([]byte, maxBookmarkBytes+1) })
	assertProtocolErrorFromEncode(t, request)
	request = mutationRequest(OperationPrepareMutation, MutationImportCredential, func(r *Request) { r.SelectedLocalPath = "/tmp/a"; r.TransientPassword = make([]byte, maxSecretBytes+1) })
	assertProtocolErrorFromEncode(t, request)
}

func TestProtocolRequestRejectsNonCanonicalWireForms(t *testing.T) {
	request := baseRequest(OperationStatus)
	canonical, err := EncodeRequest(request, protocolNow)
	if err != nil {
		t.Fatal(err)
	}

	_, _, tag1End := protowire.ConsumeField(canonical)
	if tag1End < 0 {
		t.Fatal("failed to locate first field")
	}
	duplicate := append(append([]byte{}, canonical[:tag1End]...), canonical...)
	unknown := append(append([]byte{}, canonical...), protowire.AppendTag(nil, 30, protowire.VarintType)...)
	unknown = protowire.AppendVarint(unknown, 1)
	wrongWire := append([]byte{0x0a, 0x01, 0x01}, canonical[tag1End:]...)
	nonMinimalVersion := append([]byte{0x08, 0x81, 0x00}, canonical[tag1End:]...)
	nonMinimalLength := append(append([]byte{}, canonical[:tag1End]...), 0x12, 0xa4, 0x00)
	nonMinimalLength = append(nonMinimalLength, canonical[tag1End+2:]...)
	reordered := append(append([]byte{}, canonical[tag1End:]...), canonical[:tag1End]...)
	malformedLength := []byte{0x08, 0x01, 0x12, 0x05, 'x'}
	trailingMalformed := append(append([]byte{}, canonical...), 0x80)

	for name, encoded := range map[string][]byte{
		"duplicate": duplicate, "unknown": unknown, "wrong wire": wrongWire,
		"non-minimal": nonMinimalVersion, "reordered": reordered,
		"non-minimal length": nonMinimalLength,
		"malformed length":   malformedLength, "trailing malformed": trailingMalformed,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := DecodeRequest(encoded, protocolNow)
			assertProtocolError(t, err, "REQUEST_INVALID")
		})
	}
}

func TestProtocolRequestDecodeOwnsAllBytes(t *testing.T) {
	request := mutationRequest(OperationPrepareMutation, MutationImportCredential, func(r *Request) {
		r.SelectedLocalPath = "/tmp/credential.p12"
		r.Bookmark = []byte("bookmark")
		r.TransientPassword = []byte("password")
	})
	encoded, err := EncodeRequest(request, protocolNow)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeRequest(encoded, protocolNow)
	if err != nil {
		t.Fatal(err)
	}
	before := cloneRequest(decoded)
	for i := range encoded {
		encoded[i] ^= 0xff
	}
	request.OpaqueScope[0] ^= 0xff
	request.Bookmark[0] ^= 0xff
	request.TransientPassword[0] ^= 0xff
	if !requestsEqual(decoded, before) {
		t.Fatal("decoded request aliases encoded or source bytes")
	}
}

func TestProtocolRequestOwnsProductAndEndpointBytes(t *testing.T) {
	requests := []Request{
		mutationRequest(OperationPrepareMutation, MutationImportProductID, nil),
		withEndpoint(withEnvironment(baseRequest(OperationStatus), EnvironmentEVTE), []byte("signed-profile")),
	}
	for index, request := range requests {
		encoded, err := EncodeRequest(request, protocolNow)
		if err != nil {
			t.Fatalf("case %d EncodeRequest: %v", index, err)
		}
		encodedBefore := bytes.Clone(encoded)
		decoded, err := DecodeRequest(encoded, protocolNow)
		if err != nil {
			t.Fatalf("case %d DecodeRequest: %v", index, err)
		}
		decodedBefore := cloneRequest(decoded)
		if request.TransientProductID != nil {
			request.TransientProductID[0] ^= 0xff
		}
		if request.EndpointProfile != nil {
			request.EndpointProfile[0] ^= 0xff
		}
		request.OpaqueScope[0] ^= 0xff
		if !bytes.Equal(encoded, encodedBefore) {
			t.Fatalf("case %d encoded request aliases source", index)
		}
		for i := range encoded {
			encoded[i] ^= 0xff
		}
		if !requestsEqual(decoded, decodedBefore) {
			t.Fatalf("case %d decoded request aliases wire bytes", index)
		}
	}
}

func TestProtocolRequestClearSecretsZerosAndReleasesSensitiveMaterial(t *testing.T) {
	request := mutationRequest(OperationPrepareMutation, MutationImportCredential, func(r *Request) {
		r.SelectedLocalPath = "/tmp/credential.p12"
		r.Bookmark = []byte("bookmark-secret")
		r.TransientPassword = []byte("password-secret")
		r.TransientProductID = []byte("product-secret")
		r.EndpointProfile = []byte("endpoint-secret")
	})
	backings := [][]byte{request.OpaqueScope, request.Bookmark, request.TransientPassword, request.TransientProductID, request.EndpointProfile}
	request.ClearSecrets()
	for _, backing := range backings {
		for _, value := range backing {
			if value != 0 {
				t.Fatalf("secret backing was not zeroed: %x", backing)
			}
		}
	}
	if request.OpaqueScope != nil || request.Bookmark != nil || request.TransientPassword != nil || request.TransientProductID != nil || request.EndpointProfile != nil || request.SelectedLocalPath != "" {
		t.Fatalf("request retained sensitive fields after ClearSecrets: %#v", request)
	}
}

func TestProtocolDecodeRequestClearsPartialSecretsAndCanonicalScratchOnError(t *testing.T) {
	request := mutationRequest(OperationPrepareMutation, MutationImportCredential, func(r *Request) {
		r.SelectedLocalPath = "/tmp/credential.p12"
		r.Bookmark = []byte("bookmark-secret")
		r.TransientPassword = []byte("password-secret")
	})
	canonical, err := EncodeRequest(request, protocolNow)
	if err != nil {
		t.Fatal(err)
	}
	// Re-encode the version varint non-minimally while preserving semantics.
	nonMinimal := append([]byte{0x08, 0x81, 0x00}, canonical[2:]...)
	lateUnknown := append(append([]byte{}, canonical...), protowire.AppendTag(nil, 30, protowire.VarintType)...)
	lateUnknown = protowire.AppendVarint(lateUnknown, 1)
	for name, input := range map[string][]byte{"noncanonical": nonMinimal, "late unknown": lateUnknown} {
		t.Run(name, func(t *testing.T) {
			inputBefore := bytes.Clone(input)
			observed := false
			_, err := decodeRequest(input, protocolNow, func(buffers [][]byte, scratch []byte) {
				observed = true
				for _, buffer := range buffers {
					for _, value := range buffer {
						if value != 0 {
							t.Fatalf("partial secret was not cleared: %x", buffer)
						}
					}
				}
				for _, value := range scratch {
					if value != 0 {
						t.Fatalf("canonical scratch was not cleared: %x", scratch)
					}
				}
			})
			assertProtocolError(t, err, "REQUEST_INVALID")
			if !observed || !bytes.Equal(input, inputBefore) {
				t.Fatalf("cleanup observed=%v caller input mutated=%v", observed, !bytes.Equal(input, inputBefore))
			}
		})
	}
}

func TestProtocolResponseValidationCanonicalityAndOwnership(t *testing.T) {
	tests := []struct {
		name     string
		response Response
		valid    bool
	}{
		{name: "ok", response: Response{RequestID: testRequestID, Outcome: OutcomeOK, RedactedResult: ResultReady}, valid: true},
		{name: "recovery", response: Response{RequestID: testRequestID, Outcome: OutcomeOK, RedactedResult: ResultRecoveryRequired}, valid: true},
		{name: "error", response: Response{RequestID: testRequestID, Outcome: OutcomeError, StableErrorCode: StableErrorCredentialLocked}, valid: true},
		{name: "uppercase secret-like unknown error", response: Response{RequestID: testRequestID, Outcome: OutcomeError, StableErrorCode: "SUPER_SECRET_PRODUCT_KEY"}},
		{name: "pending", response: Response{RequestID: testRequestID, Outcome: OutcomePending, PendingItemID: testPendingID}, valid: true},
		{name: "error text control", response: Response{RequestID: testRequestID, Outcome: OutcomeError, StableErrorCode: "BAD\nCODE"}},
		{name: "error has result", response: Response{RequestID: testRequestID, Outcome: OutcomeError, RedactedResult: ResultReady, StableErrorCode: "ERROR"}},
		{name: "error has pending", response: Response{RequestID: testRequestID, Outcome: OutcomeError, StableErrorCode: "ERROR", PendingItemID: testPendingID}},
		{name: "ok missing result", response: Response{RequestID: testRequestID, Outcome: OutcomeOK}},
		{name: "ok has error", response: Response{RequestID: testRequestID, Outcome: OutcomeOK, RedactedResult: ResultReady, StableErrorCode: "ERROR"}},
		{name: "ok has pending", response: Response{RequestID: testRequestID, Outcome: OutcomeOK, RedactedResult: ResultReady, PendingItemID: testPendingID}},
		{name: "pending missing id", response: Response{RequestID: testRequestID, Outcome: OutcomePending}},
		{name: "pending has result", response: Response{RequestID: testRequestID, Outcome: OutcomePending, RedactedResult: ResultReady, PendingItemID: testPendingID}},
		{name: "pending has error", response: Response{RequestID: testRequestID, Outcome: OutcomePending, StableErrorCode: "ERROR", PendingItemID: testPendingID}},
		{name: "unknown outcome", response: Response{RequestID: testRequestID, Outcome: 9, RedactedResult: ResultReady}},
		{name: "unknown result", response: Response{RequestID: testRequestID, Outcome: OutcomeOK, RedactedResult: 99}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := EncodeResponse(tt.response)
			if !tt.valid {
				assertProtocolError(t, err, "RESPONSE_INVALID")
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := DecodeResponse(encoded)
			if err != nil {
				t.Fatal(err)
			}
			again, err := EncodeResponse(decoded)
			if err != nil || !bytes.Equal(encoded, again) {
				t.Fatalf("noncanonical response: %x %v", again, err)
			}
			for i := range encoded {
				encoded[i] ^= 0xff
			}
			if decoded.RequestID != tt.response.RequestID || decoded.StableErrorCode != tt.response.StableErrorCode {
				t.Fatal("decoded response aliases bytes")
			}
		})
	}

	valid, _ := EncodeResponse(Response{RequestID: testRequestID, Outcome: OutcomeOK, RedactedResult: ResultReady})
	noncanonical := append(append([]byte{}, valid...), protowire.AppendTag(nil, 99, protowire.VarintType)...)
	noncanonical = protowire.AppendVarint(noncanonical, 1)
	_, err := DecodeResponse(noncanonical)
	assertProtocolError(t, err, "RESPONSE_INVALID")
	unknownError := appendStringField(nil, responseFieldRequestID, testRequestID)
	unknownError = appendVarintField(unknownError, responseFieldOutcome, uint64(OutcomeError))
	unknownError = appendStringField(unknownError, responseFieldStableErrorCode, "SUPER_SECRET_PRODUCT_KEY")
	_, err = DecodeResponse(unknownError)
	assertProtocolError(t, err, "RESPONSE_INVALID")
}

func TestProtocolErrorResponseConstructorMapsUnknownCodes(t *testing.T) {
	known := NewErrorResponse(testRequestID, StableErrorCredentialLocked)
	if known.StableErrorCode != StableErrorCredentialLocked {
		t.Fatalf("known stable error mapped to %q", known.StableErrorCode)
	}
	unknown := NewErrorResponse(testRequestID, StableErrorCode("SUPER_SECRET_PRODUCT_KEY"))
	if unknown.StableErrorCode != StableErrorHelperProtocol {
		t.Fatalf("unknown stable error = %q, want generic helper protocol code", unknown.StableErrorCode)
	}
	if _, err := EncodeResponse(unknown); err != nil {
		t.Fatalf("mapped generic response invalid: %v", err)
	}
}

func TestProtocolResponseRejectsNonCanonicalWireForms(t *testing.T) {
	canonical, err := EncodeResponse(Response{RequestID: testRequestID, Outcome: OutcomeOK, RedactedResult: ResultReady})
	if err != nil {
		t.Fatal(err)
	}
	_, _, first := protowire.ConsumeField(canonical)
	_, _, second := protowire.ConsumeField(canonical[first:])
	second += first
	duplicate := append(append([]byte{}, canonical[:first]...), canonical...)
	reordered := append(append([]byte{}, canonical[first:]...), canonical[:first]...)
	wrongWire := append([]byte{0x08, 0x01}, canonical[first:]...)
	nonMinimalOutcome := append(append([]byte{}, canonical[:first]...), 0x10, 0x81, 0x00)
	nonMinimalOutcome = append(nonMinimalOutcome, canonical[second:]...)
	nonMinimalLength := append([]byte{0x0a, 0xa4, 0x00}, canonical[2:]...)
	unknown := append(append([]byte{}, canonical...), protowire.AppendTag(nil, 99, protowire.VarintType)...)
	unknown = protowire.AppendVarint(unknown, 1)
	for name, encoded := range map[string][]byte{
		"duplicate": duplicate, "reordered": reordered, "wrong wire": wrongWire,
		"non-minimal outcome": nonMinimalOutcome, "unknown": unknown,
		"non-minimal length": nonMinimalLength,
		"trailing malformed": append(append([]byte{}, canonical...), 0x80),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := DecodeResponse(encoded)
			assertProtocolError(t, err, "RESPONSE_INVALID")
		})
	}
}

func TestProtocolSessionAllowsExactlyOneMatchingResponse(t *testing.T) {
	request := mutationRequest(OperationPrepareMutation, MutationRemoveCredential, nil)
	session := &Session{}
	if err := session.Begin(request, protocolNow); err != nil {
		t.Fatal(err)
	}
	assertProtocolError(t, session.Begin(request, protocolNow), "SESSION_BUSY")

	mismatch := Response{RequestID: testPendingID, Outcome: OutcomePending, PendingItemID: testPendingID}
	assertProtocolError(t, session.Complete(mismatch, protocolNow), "SESSION_MISMATCH")
	matching := mismatch
	matching.RequestID = request.RequestID
	if err := session.Complete(matching, protocolNow); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	assertProtocolError(t, session.Complete(matching, protocolNow), "SESSION_IDLE")

	expired := request
	expired.DeadlineMillis = protocolNow.UnixMilli()
	assertProtocolError(t, session.Begin(expired, protocolNow), "REQUEST_INVALID")
}

func TestProtocolSessionRejectsPendingOutsidePrepare(t *testing.T) {
	session := &Session{}
	if err := session.Begin(baseRequest(OperationStatus), protocolNow); err != nil {
		t.Fatal(err)
	}
	response := Response{RequestID: testRequestID, Outcome: OutcomePending, PendingItemID: testPendingID}
	assertProtocolError(t, session.Complete(response, protocolNow), "SESSION_RESPONSE_INVALID")
}

func TestProtocolSessionRecoveryResultOnlyFixtureAndReconcile(t *testing.T) {
	response := Response{RequestID: testRequestID, Outcome: OutcomeOK, RedactedResult: ResultRecoveryRequired}
	for _, operation := range []Operation{OperationFixture, OperationReconcileMutation} {
		request := baseRequest(operation)
		if operation == OperationFixture {
			request.WorkspaceID, request.OrganisationID, request.CanonicalABN, request.OpaqueScope = "", "", "", nil
			request.SimulatorCase = SimulatorAccepted
		} else {
			request.OperationID = testOperationID
			request.MutationKind = MutationRemoveCredential
		}
		session := &Session{}
		if err := session.Begin(request, protocolNow); err != nil {
			t.Fatal(err)
		}
		if err := session.Complete(response, protocolNow); err != nil {
			t.Fatalf("operation %d recovery response: %v", operation, err)
		}
	}
	session := &Session{}
	if err := session.Begin(baseRequest(OperationStatus), protocolNow); err != nil {
		t.Fatal(err)
	}
	assertProtocolError(t, session.Complete(response, protocolNow), "SESSION_RESPONSE_INVALID")
}

func TestProtocolSessionConcurrentBeginAndComplete(t *testing.T) {
	request := mutationRequest(OperationPrepareMutation, MutationRemoveCredential, nil)
	session := &Session{}
	start := make(chan struct{})
	results := make(chan error, 32)
	var workers sync.WaitGroup
	for range 32 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			results <- session.Begin(request, protocolNow)
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		} else if err.Error() != "SESSION_BUSY" {
			t.Fatalf("concurrent Begin error = %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("concurrent Begin wins = %d, want 1", wins)
	}

	response := Response{RequestID: testRequestID, Outcome: OutcomePending, PendingItemID: testPendingID}
	completeStart := make(chan struct{})
	completeResults := make(chan error, 32)
	for range 32 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-completeStart
			completeResults <- session.Complete(response, protocolNow)
		}()
	}
	close(completeStart)
	workers.Wait()
	close(completeResults)
	completions := 0
	for err := range completeResults {
		if err == nil {
			completions++
		} else if err.Error() != "SESSION_IDLE" {
			t.Fatalf("concurrent Complete error = %v", err)
		}
	}
	if completions != 1 {
		t.Fatalf("concurrent Complete wins = %d, want 1", completions)
	}
}

func TestProtocolSessionBeginCompleteRaceIsSerializable(t *testing.T) {
	for range 64 {
		first := mutationRequest(OperationPrepareMutation, MutationRemoveCredential, nil)
		second := first
		second.RequestID = "018bcfe5-6800-7000-8000-000000000006"
		session := &Session{}
		if err := session.Begin(first, protocolNow); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		completeResult := make(chan error, 1)
		beginResult := make(chan error, 1)
		go func() {
			<-start
			completeResult <- session.Complete(Response{RequestID: first.RequestID, Outcome: OutcomePending, PendingItemID: testPendingID}, protocolNow)
		}()
		go func() {
			<-start
			beginResult <- session.Begin(second, protocolNow)
		}()
		close(start)
		if err := <-completeResult; err != nil {
			t.Fatalf("matching Complete lost race: %v", err)
		}
		beginErr := <-beginResult
		if beginErr != nil && beginErr.Error() != "SESSION_BUSY" {
			t.Fatalf("racing Begin error = %v", beginErr)
		}
		if beginErr != nil {
			if err := session.Begin(second, protocolNow); err != nil {
				t.Fatalf("second Begin not admitted after completion: %v", err)
			}
		} else {
			assertProtocolError(t, session.Begin(second, protocolNow), "SESSION_BUSY")
		}
		if err := session.Complete(Response{RequestID: second.RequestID, Outcome: OutcomePending, PendingItemID: testPendingID}, protocolNow); err != nil {
			t.Fatalf("second completion: %v", err)
		}
	}
}

func TestProtocolSessionRetainsOnlyRequestIDAndOperation(t *testing.T) {
	pendingType := reflect.TypeOf(pendingRequest{})
	if pendingType.NumField() != 3 || pendingType.Field(0).Name != "requestID" || pendingType.Field(1).Name != "operation" || pendingType.Field(2).Name != "deadlineMillis" {
		t.Fatalf("pending session fields = %v; want requestID, operation, and deadline only", pendingType)
	}
	request := mutationRequest(OperationPrepareMutation, MutationImportCredential, func(r *Request) {
		r.SelectedLocalPath = "/tmp/credential.p12"
		r.Bookmark = []byte("bookmark-secret")
		r.TransientPassword = []byte("password-secret")
	})
	session := &Session{}
	if err := session.Begin(request, protocolNow); err != nil {
		t.Fatal(err)
	}
	for _, secret := range [][]byte{request.OpaqueScope, request.Bookmark, request.TransientPassword} {
		for index := range secret {
			secret[index] = 0
		}
	}
	response := Response{RequestID: testRequestID, Outcome: OutcomePending, PendingItemID: testPendingID}
	if err := session.Complete(response, protocolNow); err != nil {
		t.Fatalf("secret overwrite affected session match: %v", err)
	}
}

func TestProtocolSessionDeadlineBoundaryAndClockValidation(t *testing.T) {
	request := mutationRequest(OperationPrepareMutation, MutationRemoveCredential, nil)
	response := Response{RequestID: request.RequestID, Outcome: OutcomePending, PendingItemID: testPendingID}

	beforeDeadline := &Session{}
	if err := beforeDeadline.Begin(request, protocolNow); err != nil {
		t.Fatal(err)
	}
	if err := beforeDeadline.Complete(response, time.UnixMilli(request.DeadlineMillis-1)); err != nil {
		t.Fatalf("response immediately before deadline: %v", err)
	}

	atDeadline := &Session{}
	if err := atDeadline.Begin(request, protocolNow); err != nil {
		t.Fatal(err)
	}
	assertProtocolError(t, atDeadline.Complete(response, time.UnixMilli(request.DeadlineMillis)), "SESSION_DEADLINE_EXPIRED")
	assertProtocolError(t, atDeadline.Complete(response, time.UnixMilli(request.DeadlineMillis)), "SESSION_IDLE")

	hostileClock := &Session{}
	if err := hostileClock.Begin(request, protocolNow); err != nil {
		t.Fatal(err)
	}
	assertProtocolError(t, hostileClock.Complete(response, time.UnixMilli(request.DeadlineMillis-int64(maxDeadlineHorizon/time.Millisecond)-1)), "SESSION_CLOCK_INVALID")
	assertProtocolError(t, hostileClock.Complete(response, protocolNow), "SESSION_IDLE")
}

func TestProtocolSessionConcurrentExpiryClearsExactlyOnce(t *testing.T) {
	request := mutationRequest(OperationPrepareMutation, MutationRemoveCredential, nil)
	session := &Session{}
	if err := session.Begin(request, protocolNow); err != nil {
		t.Fatal(err)
	}
	response := Response{RequestID: request.RequestID, Outcome: OutcomePending, PendingItemID: testPendingID}
	start := make(chan struct{})
	results := make(chan error, 32)
	var workers sync.WaitGroup
	for range 32 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			results <- session.Complete(response, time.UnixMilli(request.DeadlineMillis))
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	expired := 0
	for err := range results {
		if err != nil && err.Error() == "SESSION_DEADLINE_EXPIRED" {
			expired++
		} else if err == nil || err.Error() != "SESSION_IDLE" {
			t.Fatalf("concurrent expiry error = %v", err)
		}
	}
	if expired != 1 {
		t.Fatalf("deadline transitions = %d, want 1", expired)
	}
}

func TestProtocolGoldenFixture(t *testing.T) {
	want := buildGoldenCorpus(t)
	path := goldenFixturePath(t)
	if os.Getenv("UPDATE_SBR_PROTOCOL_FIXTURE") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, want, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden fixture: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("fixture differs: got %x want %x", got, want)
	}
	records := parseGoldenCorpus(t, got)
	if len(records) != 21 {
		t.Fatalf("golden record count = %d, want 21", len(records))
	}
	for index, record := range records {
		var again []byte
		switch record.kind {
		case corpusRequestRecord:
			decoded, err := DecodeRequest(record.payload, protocolNow)
			if err != nil {
				t.Fatalf("request record %d: %v", index, err)
			}
			again, err = EncodeRequest(decoded, protocolNow)
			decoded.ClearSecrets()
			if err != nil {
				t.Fatalf("request record %d encode: %v", index, err)
			}
		case corpusResponseRecord:
			decoded, err := DecodeResponse(record.payload)
			if err != nil {
				t.Fatalf("response record %d: %v", index, err)
			}
			again, err = EncodeResponse(decoded)
			if err != nil {
				t.Fatalf("response record %d encode: %v", index, err)
			}
		default:
			t.Fatalf("record %d unknown kind %d", index, record.kind)
		}
		if !bytes.Equal(again, record.payload) {
			t.Fatalf("record %d canonical mismatch: got %x want %x", index, again, record.payload)
		}
	}
	digest := sha256.Sum256(got)
	t.Logf("fixture sha256=%s", hex.EncodeToString(digest[:]))
}

func FuzzDecodeRequest(f *testing.F) {
	seed, err := EncodeRequest(baseRequest(OperationStatus), protocolNow)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(seed)
	f.Add([]byte{0x08, 0x01})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > MaxPayloadSize {
			t.Skip()
		}
		request, err := DecodeRequest(data, protocolNow)
		if err != nil {
			assertStableFuzzError(t, err)
			return
		}
		again, err := EncodeRequest(request, protocolNow)
		request.ClearSecrets()
		if err != nil || !bytes.Equal(again, data) {
			t.Fatalf("accepted request was not canonical: %v", err)
		}
	})
}

func FuzzDecodeResponse(f *testing.F) {
	seed, err := EncodeResponse(Response{RequestID: testRequestID, Outcome: OutcomeOK, RedactedResult: ResultReady})
	if err != nil {
		f.Fatal(err)
	}
	f.Add(seed)
	f.Add([]byte{0x0a, 0x01, 'x'})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > MaxPayloadSize {
			t.Skip()
		}
		response, err := DecodeResponse(data)
		if err != nil {
			assertStableFuzzError(t, err)
			return
		}
		again, err := EncodeResponse(response)
		if err != nil || !bytes.Equal(again, data) {
			t.Fatalf("accepted response was not canonical: %v", err)
		}
	})
}

const (
	corpusRequestRecord  byte = 1
	corpusResponseRecord byte = 2
)

var corpusMagic = []byte("SBRP1")

type corpusRecord struct {
	kind    byte
	payload []byte
}

func buildGoldenCorpus(t *testing.T) []byte {
	t.Helper()
	requests := []Request{
		corpusRequest("1", OperationStatus, 0, nil),
		corpusRequest("2", OperationStatus, 0, func(r *Request) {
			r.Environment = EnvironmentEVTE
			r.EndpointProfile = []byte("signed-endpoint-profile")
		}),
		corpusRequest("3", OperationUnlock, 0, nil),
		corpusRequest("4", OperationFixture, 0, func(r *Request) {
			r.WorkspaceID, r.OrganisationID, r.CanonicalABN, r.OpaqueScope = "", "", "", nil
			r.SimulatorCase = SimulatorAccepted
		}),
		corpusRequest("5", OperationPrepareMutation, MutationImportCredential, func(r *Request) {
			r.SelectedLocalPath = "/tmp/credential-import.p12"
			r.Bookmark = []byte("bookmark")
			r.TransientPassword = []byte("password")
		}),
		corpusRequest("6", OperationPrepareMutation, MutationReplaceCredential, func(r *Request) {
			r.SelectedLocalPath = "/tmp/credential-replace.p12"
			r.TransientPassword = []byte{}
		}),
		corpusRequest("7", OperationPrepareMutation, MutationRemoveCredential, nil),
		corpusRequest("8", OperationPrepareMutation, MutationImportProductID, func(r *Request) {
			r.ProductScope = "pay.roll/v2"
			r.ServiceID = "sbr.gst-service"
			r.TransientProductID = []byte("product-secret")
		}),
		corpusRequest("9", OperationPrepareMutation, MutationRemoveProductID, func(r *Request) { r.ProductScope = "pay.roll/v2"; r.ServiceID = "sbr.gst-service" }),
		corpusRequest("a", OperationCommitMutation, MutationImportCredential, nil),
		corpusRequest("b", OperationAbortMutation, MutationReplaceCredential, nil),
		corpusRequest("c", OperationReconcileMutation, MutationRemoveProductID, func(r *Request) { r.ProductScope = "pay.roll/v2"; r.ServiceID = "sbr.gst-service" }),
	}
	responses := []Response{
		{RequestID: corpusUUID("1"), Outcome: OutcomeOK, RedactedResult: ResultReady},
		{RequestID: corpusUUID("2"), Outcome: OutcomeOK, RedactedResult: ResultCredentialLocked},
		{RequestID: corpusUUID("3"), Outcome: OutcomeOK, RedactedResult: ResultRegistrationRequired},
		{RequestID: corpusUUID("4"), Outcome: OutcomeOK, RedactedResult: ResultMutationCommitted},
		{RequestID: corpusUUID("5"), Outcome: OutcomeOK, RedactedResult: ResultMutationAborted},
		{RequestID: corpusUUID("6"), Outcome: OutcomeOK, RedactedResult: ResultRecoveryRequired},
		{RequestID: corpusUUID("7"), Outcome: OutcomeOK, RedactedResult: ResultFixtureSelected},
		NewErrorResponse(corpusUUID("8"), StableErrorCredentialLocked),
		{RequestID: corpusUUID("9"), Outcome: OutcomePending, PendingItemID: testPendingID},
	}
	records := make([]corpusRecord, 0, len(requests)+len(responses))
	for index := range requests {
		payload, err := EncodeRequest(requests[index], protocolNow)
		requests[index].ClearSecrets()
		if err != nil {
			t.Fatalf("build request corpus record %d: %v", index, err)
		}
		records = append(records, corpusRecord{kind: corpusRequestRecord, payload: payload})
	}
	for index, response := range responses {
		payload, err := EncodeResponse(response)
		if err != nil {
			t.Fatalf("build response corpus record %d: %v", index, err)
		}
		records = append(records, corpusRecord{kind: corpusResponseRecord, payload: payload})
	}
	encoded := append([]byte{}, corpusMagic...)
	for _, record := range records {
		encoded = append(encoded, record.kind)
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(record.payload)))
		encoded = append(encoded, length[:]...)
		encoded = append(encoded, record.payload...)
	}
	return encoded
}

func corpusRequest(suffix string, operation Operation, kind MutationKind, modify func(*Request)) Request {
	request := baseRequest(operation)
	request.RequestID = corpusUUID(suffix)
	if operation >= OperationPrepareMutation {
		request.OperationID = "018bcfe5-6800-7000-8000-00000000000d"
		request.MutationKind = kind
	}
	if modify != nil {
		modify(&request)
	}
	return request
}

func corpusUUID(suffix string) string { return "018bcfe5-6800-7000-8000-00000000000" + suffix }

func parseGoldenCorpus(t *testing.T, data []byte) []corpusRecord {
	t.Helper()
	if len(data) < len(corpusMagic) || !bytes.Equal(data[:len(corpusMagic)], corpusMagic) {
		t.Fatal("golden corpus magic invalid")
	}
	remaining := data[len(corpusMagic):]
	var records []corpusRecord
	for len(remaining) > 0 {
		if len(remaining) < 5 {
			t.Fatal("golden corpus truncated header")
		}
		kind := remaining[0]
		size := int(binary.BigEndian.Uint32(remaining[1:5]))
		remaining = remaining[5:]
		if size <= 0 || size > MaxPayloadSize || len(remaining) < size {
			t.Fatal("golden corpus invalid record size")
		}
		records = append(records, corpusRecord{kind: kind, payload: bytes.Clone(remaining[:size])})
		remaining = remaining[size:]
	}
	return records
}

func goldenFixturePath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller path unavailable")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "test", "fixtures", "sbr", "helper-protocol-v1.bin"))
}

func mutationRequest(operation Operation, kind MutationKind, modify func(*Request)) Request {
	r := baseRequest(operation)
	r.OperationID = testOperationID
	r.MutationKind = kind
	if kind == MutationImportProductID || kind == MutationRemoveProductID {
		r.ProductScope, r.ServiceID = "PAYROLL", "SBR_GST"
	}
	if kind == MutationImportProductID && operation == OperationPrepareMutation {
		r.TransientProductID = []byte("secret-product-id")
	}
	if modify != nil {
		modify(&r)
	}
	return r
}

func assertProtocolErrorFromEncode(t *testing.T, request Request) {
	t.Helper()
	_, err := EncodeRequest(request, protocolNow)
	assertProtocolError(t, err, "REQUEST_INVALID")
}

func withVersion(r Request, v uint32) Request            { r.ProtocolVersion = v; return r }
func withRequestID(r Request, v string) Request          { r.RequestID = v; return r }
func withOperation(r Request, v Operation) Request       { r.Operation = v; return r }
func withEnvironment(r Request, v Environment) Request   { r.Environment = v; return r }
func withDeadline(r Request, v int64) Request            { r.DeadlineMillis = v; return r }
func withWorkspace(r Request, v string) Request          { r.WorkspaceID = v; return r }
func withOrganisation(r Request, v string) Request       { r.OrganisationID = v; return r }
func withABN(r Request, v string) Request                { r.CanonicalABN = v; return r }
func withScope(r Request, v []byte) Request              { r.OpaqueScope = v; return r }
func withOperationID(r Request, v string) Request        { r.OperationID = v; return r }
func withMutationKind(r Request, v MutationKind) Request { r.MutationKind = v; return r }
func withFixtureCase(r Request, v SimulatorCase) Request { r.SimulatorCase = v; return r }
func withEndpoint(r Request, v []byte) Request           { r.EndpointProfile = v; return r }
func withPath(r Request, v string) Request               { r.SelectedLocalPath = v; return r }
func withProductID(r Request, v []byte) Request          { r.TransientProductID = v; return r }

func requestsEqual(a, b Request) bool {
	return a.ProtocolVersion == b.ProtocolVersion && a.RequestID == b.RequestID && a.Operation == b.Operation && a.DeadlineMillis == b.DeadlineMillis &&
		a.Environment == b.Environment && a.WorkspaceID == b.WorkspaceID && a.OrganisationID == b.OrganisationID && a.CanonicalABN == b.CanonicalABN &&
		bytes.Equal(a.OpaqueScope, b.OpaqueScope) && a.OperationID == b.OperationID && a.MutationKind == b.MutationKind && a.SelectedLocalPath == b.SelectedLocalPath &&
		bytes.Equal(a.Bookmark, b.Bookmark) && bytes.Equal(a.TransientPassword, b.TransientPassword) && bytes.Equal(a.TransientProductID, b.TransientProductID) &&
		a.ProductScope == b.ProductScope && a.ServiceID == b.ServiceID && bytes.Equal(a.EndpointProfile, b.EndpointProfile) && a.SimulatorCase == b.SimulatorCase
}

func cloneRequest(r Request) Request {
	r.OpaqueScope = bytes.Clone(r.OpaqueScope)
	r.Bookmark = bytes.Clone(r.Bookmark)
	r.TransientPassword = bytes.Clone(r.TransientPassword)
	r.TransientProductID = bytes.Clone(r.TransientProductID)
	r.EndpointProfile = bytes.Clone(r.EndpointProfile)
	return r
}
