package sbrhelper

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protowire"
)

type coreShortWriter struct {
	buf bytes.Buffer
	max int
}

func (w *coreShortWriter) Write(p []byte) (int, error) {
	if len(p) > w.max {
		p = p[:w.max]
	}
	return w.buf.Write(p)
}

func TestProtocolCoreFrameBoundsAndShortWrites(t *testing.T) {
	w := &coreShortWriter{max: 2}
	if err := WriteFrame(w, []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFrame(bytes.NewReader(w.buf.Bytes()))
	if err != nil || !bytes.Equal(got, []byte{1, 2, 3}) {
		t.Fatalf("ReadFrame = %x, %v", got, err)
	}
	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, MaxPayloadSize+1)
	_, err = ReadFrame(io.MultiReader(bytes.NewReader(header), corePanicReader{}))
	assertCoreProtocolError(t, err, "FRAME_SIZE_INVALID", 0)
	_, err = ReadFrame(bytes.NewReader([]byte{0, 0, 0, 1, 1, 2}))
	assertCoreProtocolError(t, err, "FRAME_TRAILING", 0)
}

func TestProtocolCoreFrameClearsAllocatedPayloadOnError(t *testing.T) {
	observed := false
	_, err := readFrame(bytes.NewReader([]byte{0, 0, 0, 2, 0xaa, 0xbb, 0xcc}), func(payload []byte) {
		observed = true
		for _, value := range payload {
			if value != 0 {
				t.Fatalf("error payload was not cleared: %x", payload)
			}
		}
	})
	if err == nil || !observed {
		t.Fatalf("readFrame error=%v observed=%v", err, observed)
	}
}

func FuzzReadFrame(f *testing.F) {
	f.Add([]byte{0, 0, 0, 1, 1})
	f.Add([]byte{0, 0, 0, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > MaxPayloadSize+8 {
			t.Skip()
		}
		payload, err := ReadFrame(bytes.NewReader(data))
		if err != nil {
			assertCoreStableFuzzError(t, err)
			return
		}
		zeroBytes(payload)
	})
}

type corePanicReader struct{}

func (corePanicReader) Read([]byte) (int, error) { panic("oversize body was read") }

var protocolTestNow = time.UnixMilli(1_700_000_000_000)

const (
	protocolTestRequestID   = "018bcfe5-6800-7000-8000-000000000001"
	protocolTestOperationID = "018bcfe5-6800-7000-8000-000000000002"
	protocolTestWorkspaceID = "018bcfe5-6800-7000-8000-000000000003"
	protocolTestOrgID       = "018bcfe5-6800-7000-8000-000000000004"
	protocolTestPendingID   = "018bcfe5-6800-7000-8000-000000000005"
)

func TestProtocolCoreGoldenFixtureParity(t *testing.T) {
	path := coreGoldenFixturePath(t)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden fixture: %v", err)
	}
	records := parseCoreGoldenCorpus(t, got)
	if len(records) != 22 {
		t.Fatalf("golden record count = %d, want 22", len(records))
	}
	for index, record := range records {
		var again []byte
		switch record.kind {
		case 1:
			decoded, err := DecodeRequest(record.payload, protocolTestNow)
			if err != nil {
				t.Fatalf("request record %d: %v", index, err)
			}
			again, err = EncodeRequest(decoded, protocolTestNow)
			decoded.ClearSecrets()
			if err != nil {
				t.Fatalf("request record %d encode: %v", index, err)
			}
		case 2:
			decoded, err := DecodeResponse(record.payload)
			if err != nil {
				t.Fatalf("response record %d: %v", index, err)
			}
			again, err = EncodeResponse(decoded)
			if err != nil {
				t.Fatalf("response record %d encode: %v", index, err)
			}
		default:
			t.Fatalf("record %d kind = %d", index, record.kind)
		}
		if !bytes.Equal(again, record.payload) {
			t.Fatalf("record %d canonical mismatch", index)
		}
	}
	digest := sha256.Sum256(got)
	t.Logf("fixture sha256=%s", hex.EncodeToString(digest[:]))
}

func FuzzDecodeRequest(f *testing.F) {
	seed, err := EncodeRequest(coreBaseRequest(OperationStatus), protocolTestNow)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(seed)
	f.Add([]byte{0x08, 0x01})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > MaxPayloadSize {
			t.Skip()
		}
		request, err := DecodeRequest(data, protocolTestNow)
		if err != nil {
			assertCoreStableFuzzError(t, err)
			return
		}
		again, err := EncodeRequest(request, protocolTestNow)
		request.ClearSecrets()
		if err != nil || !bytes.Equal(again, data) {
			t.Fatalf("accepted request was not canonical: %v", err)
		}
	})
}

func FuzzDecodeResponse(f *testing.F) {
	seed, err := EncodeResponse(Response{RequestID: protocolTestRequestID, Outcome: OutcomeOK, RedactedResult: ResultReady})
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
			assertCoreStableFuzzError(t, err)
			return
		}
		again, err := EncodeResponse(response)
		if err != nil || !bytes.Equal(again, data) {
			t.Fatalf("accepted response was not canonical: %v", err)
		}
	})
}

type coreCorpusRecord struct {
	kind    byte
	payload []byte
}

func parseCoreGoldenCorpus(t *testing.T, data []byte) []coreCorpusRecord {
	t.Helper()
	magic := []byte("SBRP1")
	if len(data) < len(magic) || !bytes.Equal(data[:len(magic)], magic) {
		t.Fatal("golden corpus magic invalid")
	}
	remaining := data[len(magic):]
	var records []coreCorpusRecord
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
		records = append(records, coreCorpusRecord{kind: kind, payload: bytes.Clone(remaining[:size])})
		remaining = remaining[size:]
	}
	return records
}

func TestProtocolCoreNumericConstantsAreLocked(t *testing.T) {
	requestFields := []protowire.Number{
		requestFieldVersion, requestFieldRequestID, requestFieldOperation, requestFieldDeadline,
		requestFieldEnvironment, requestFieldWorkspaceID, requestFieldOrganisationID, requestFieldABN,
		requestFieldOpaqueScope, requestFieldOperationID, requestFieldMutationKind, requestFieldSelectedPath,
		requestFieldBookmark, requestFieldPassword, requestFieldProductID, requestFieldProductScope,
		requestFieldServiceID, requestFieldEndpointProfile, requestFieldSimulatorCase,
	}
	for index, field := range requestFields {
		if field != protowire.Number(index+1) {
			t.Fatalf("request field %d = %d", index, field)
		}
	}
	if OperationStatus != 1 || OperationUnlock != 2 || OperationFixture != 3 || OperationPrepareMutation != 4 || OperationCommitMutation != 5 || OperationAbortMutation != 6 || OperationReconcileMutation != 7 {
		t.Fatal("operation values changed")
	}
	if MutationImportCredential != 1 || MutationReplaceCredential != 2 || MutationRemoveCredential != 3 || MutationImportProductID != 4 || MutationRemoveProductID != 5 {
		t.Fatal("mutation values changed")
	}
	if EnvironmentSimulator != 1 || EnvironmentEVTE != 2 {
		t.Fatal("environment values changed")
	}
	if SimulatorAccepted != 1 || SimulatorNotStarted != 2 || SimulatorMaybeSent != 3 || SimulatorMalformedResponse != 4 || SimulatorHelperDeath != 5 || SimulatorTimeout != 6 || SimulatorUnknown != 7 {
		t.Fatal("simulator case values changed")
	}
	if OutcomeOK != 1 || OutcomeError != 2 || OutcomePending != 3 {
		t.Fatal("outcome values changed")
	}
	if ResultReady != 1 || ResultCredentialLocked != 2 || ResultRegistrationRequired != 3 || ResultMutationCommitted != 4 || ResultMutationAborted != 5 || ResultRecoveryRequired != 6 || ResultFixtureSelected != 7 || ResultNotStarted != 8 {
		t.Fatal("redacted result values changed")
	}
}

func TestProtocolCoreResponseFieldNumbersAreLocked(t *testing.T) {
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

func TestProtocolCoreResponseRejectsCanonicalWireMutations(t *testing.T) {
	canonical, err := EncodeResponse(Response{RequestID: protocolTestRequestID, Outcome: OutcomeOK, RedactedResult: ResultReady})
	if err != nil {
		t.Fatal(err)
	}
	_, _, first := protowire.ConsumeField(canonical)
	duplicate := append(append([]byte{}, canonical[:first]...), canonical...)
	reordered := append(append([]byte{}, canonical[first:]...), canonical[:first]...)
	nonMinimalLength := append([]byte{0x0a, 0xa4, 0x00}, canonical[2:]...)
	unknown := append(append([]byte{}, canonical...), protowire.AppendTag(nil, 99, protowire.VarintType)...)
	unknown = protowire.AppendVarint(unknown, 1)
	for index, encoded := range [][]byte{duplicate, reordered, nonMinimalLength, unknown, append(append([]byte{}, canonical...), 0x80)} {
		_, err := DecodeResponse(encoded)
		assertCoreProtocolError(t, err, "RESPONSE_INVALID", index)
	}
}

func TestProtocolCoreRejectsCanonicalWireMutations(t *testing.T) {
	request := coreBaseRequest(OperationStatus)
	canonical, err := EncodeRequest(request, protocolTestNow)
	if err != nil {
		t.Fatal(err)
	}
	_, _, first := protowire.ConsumeField(canonical)
	mutations := [][]byte{
		append(append([]byte{}, canonical[:first]...), canonical...),
		append(append([]byte{}, canonical...), 0x80),
		append([]byte{0x08, 0x81, 0x00}, canonical[first:]...),
		append(append([]byte{}, canonical[first:]...), canonical[:first]...),
	}
	unknown := append(append([]byte{}, canonical...), protowire.AppendTag(nil, 99, protowire.VarintType)...)
	unknown = protowire.AppendVarint(unknown, 1)
	mutations = append(mutations, unknown)
	for i, encoded := range mutations {
		_, err := DecodeRequest(encoded, protocolTestNow)
		assertCoreProtocolError(t, err, "REQUEST_INVALID", i)
	}
}

func TestProtocolCoreCombinationAndOwnership(t *testing.T) {
	request := coreBaseRequest(OperationPrepareMutation)
	request.OperationID = protocolTestOperationID
	request.MutationKind = MutationImportCredential
	request.SelectedLocalPath = "/tmp/credential.p12"
	request.Bookmark = []byte("bookmark")
	request.TransientPassword = []byte("password")
	encoded, err := EncodeRequest(request, protocolTestNow)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeRequest(encoded, protocolTestNow)
	if err != nil {
		t.Fatal(err)
	}
	before := cloneRequest(decoded)
	for i := range encoded {
		encoded[i] ^= 0xff
	}
	request.Bookmark[0] ^= 0xff
	request.TransientPassword[0] ^= 0xff
	request.OpaqueScope[0] ^= 0xff
	if !requestsEqual(decoded, before) {
		t.Fatal("core decoded request aliases bytes")
	}

	invalid := request
	invalid.SelectedLocalPath = "relative"
	_, err = EncodeRequest(invalid, protocolTestNow)
	assertCoreProtocolError(t, err, "REQUEST_INVALID", 0)
	for index, path := range []string{"/tmp/a\x00b", "/tmp/a\u0085b"} {
		invalid = request
		invalid.SelectedLocalPath = path
		_, err = EncodeRequest(invalid, protocolTestNow)
		assertCoreProtocolError(t, err, "REQUEST_INVALID", index)
	}
	validUnicode := request
	validUnicode.SelectedLocalPath = "/tmp/café/証明.p12"
	if _, err = EncodeRequest(validUnicode, protocolTestNow); err != nil {
		t.Fatalf("valid Unicode path rejected: %v", err)
	}
	product := coreBaseRequest(OperationPrepareMutation)
	product.OperationID = protocolTestOperationID
	product.MutationKind = MutationImportProductID
	product.TransientProductID = []byte("secret")
	product.ProductScope, product.ServiceID = "pay.roll/v2:demo-product", "sbr.gst-service-v1"
	if _, err = EncodeRequest(product, protocolTestNow); err != nil {
		t.Fatalf("legitimate public identifiers rejected: %v", err)
	}
	product.ProductScope = "bad\u0085scope"
	if _, err = EncodeRequest(product, protocolTestNow); err == nil {
		t.Fatal("controlled public identifier accepted")
	}
}

func TestProtocolCoreOwnsProductAndEndpointBytes(t *testing.T) {
	product := coreBaseRequest(OperationPrepareMutation)
	product.OperationID = protocolTestOperationID
	product.MutationKind = MutationImportProductID
	product.ProductScope, product.ServiceID = "PAYROLL", "SBR_GST"
	product.TransientProductID = []byte("product-secret")
	evte := coreBaseRequest(OperationStatus)
	evte.Environment = EnvironmentEVTE
	evte.EndpointProfile = []byte("signed-profile")

	for index, request := range []Request{product, evte} {
		encoded, err := EncodeRequest(request, protocolTestNow)
		if err != nil {
			t.Fatalf("case %d EncodeRequest: %v", index, err)
		}
		encodedBefore := bytes.Clone(encoded)
		decoded, err := DecodeRequest(encoded, protocolTestNow)
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

func TestProtocolCoreClearsRequestSecretsAndDecodeScratch(t *testing.T) {
	request := coreBaseRequest(OperationPrepareMutation)
	request.OperationID = protocolTestOperationID
	request.MutationKind = MutationImportCredential
	request.SelectedLocalPath = "/tmp/credential.p12"
	request.Bookmark = []byte("bookmark-secret")
	request.TransientPassword = []byte("password-secret")
	backings := [][]byte{request.OpaqueScope, request.Bookmark, request.TransientPassword}
	encoded, err := EncodeRequest(request, protocolTestNow)
	if err != nil {
		t.Fatal(err)
	}
	lateUnknown := append(append([]byte{}, encoded...), protowire.AppendTag(nil, 30, protowire.VarintType)...)
	lateUnknown = protowire.AppendVarint(lateUnknown, 1)
	inputBefore := bytes.Clone(lateUnknown)
	observed := false
	_, err = decodeRequest(lateUnknown, protocolTestNow, func(buffers [][]byte, scratch []byte) {
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
	assertCoreProtocolError(t, err, "REQUEST_INVALID", 0)
	if !observed || !bytes.Equal(lateUnknown, inputBefore) {
		t.Fatal("decode cleanup was not observed or caller input mutated")
	}
	request.ClearSecrets()
	for _, backing := range backings {
		for _, value := range backing {
			if value != 0 {
				t.Fatalf("request secret backing was not cleared: %x", backing)
			}
		}
	}
}

func TestProtocolCoreResponseAndSession(t *testing.T) {
	request := coreBaseRequest(OperationPrepareMutation)
	request.OperationID = protocolTestOperationID
	request.MutationKind = MutationRemoveCredential
	session := &Session{}
	if err := session.Begin(request, protocolTestNow); err != nil {
		t.Fatal(err)
	}
	assertCoreProtocolError(t, session.Begin(request, protocolTestNow), "SESSION_BUSY", 0)
	response := Response{RequestID: request.RequestID, Outcome: OutcomePending, PendingItemID: protocolTestPendingID}
	mismatch := response
	mismatch.RequestID = protocolTestPendingID
	assertCoreProtocolError(t, session.Complete(mismatch, protocolTestNow), "SESSION_MISMATCH", 0)
	assertCoreProtocolError(t, session.Begin(request, protocolTestNow), "SESSION_BUSY", 0)
	encoded, err := EncodeResponse(response)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeResponse(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Complete(decoded, protocolTestNow); err != nil {
		t.Fatal(err)
	}
	assertCoreProtocolError(t, session.Complete(decoded, protocolTestNow), "SESSION_IDLE", 0)

	bad := Response{RequestID: request.RequestID, Outcome: OutcomeError, StableErrorCode: "secret/value"}
	_, err = EncodeResponse(bad)
	assertCoreProtocolError(t, err, "RESPONSE_INVALID", 0)
}

func TestProtocolCoreErrorCodesAreClosedAndMapped(t *testing.T) {
	bad := Response{RequestID: protocolTestRequestID, Outcome: OutcomeError, StableErrorCode: StableErrorCode("SUPER_SECRET_PRODUCT_KEY")}
	if _, err := EncodeResponse(bad); err == nil {
		t.Fatal("unknown uppercase error code accepted")
	}
	mapped := NewErrorResponse(protocolTestRequestID, bad.StableErrorCode)
	if mapped.StableErrorCode != StableErrorHelperProtocol {
		t.Fatalf("mapped error = %q", mapped.StableErrorCode)
	}
	unknownWire := appendStringField(nil, responseFieldRequestID, protocolTestRequestID)
	unknownWire = appendVarintField(unknownWire, responseFieldOutcome, uint64(OutcomeError))
	unknownWire = appendStringField(unknownWire, responseFieldStableErrorCode, "SUPER_SECRET_PRODUCT_KEY")
	if _, err := DecodeResponse(unknownWire); err == nil {
		t.Fatal("unknown wire error code accepted")
	}
}

func TestProtocolCoreResponseRepresentsFixtureRecovery(t *testing.T) {
	request := coreBaseRequest(OperationFixture)
	request.WorkspaceID, request.OrganisationID, request.CanonicalABN, request.OpaqueScope = "", "", "", nil
	request.SimulatorCase = SimulatorAccepted
	session := &Session{}
	if err := session.Begin(request, protocolTestNow); err != nil {
		t.Fatal(err)
	}
	response := Response{RequestID: protocolTestRequestID, Outcome: OutcomeOK, RedactedResult: ResultRecoveryRequired}
	if err := session.Complete(response, protocolTestNow); err != nil {
		t.Fatalf("fixture recovery response: %v", err)
	}
}

func TestProtocolCoreResponseRepresentsFixtureNotStarted(t *testing.T) {
	request := coreBaseRequest(OperationFixture)
	request.WorkspaceID, request.OrganisationID, request.CanonicalABN, request.OpaqueScope = "", "", "", nil
	request.SimulatorCase = SimulatorNotStarted
	session := &Session{}
	if err := session.Begin(request, protocolTestNow); err != nil {
		t.Fatal(err)
	}
	response := Response{RequestID: protocolTestRequestID, Outcome: OutcomeOK, RedactedResult: ResultNotStarted}
	if err := session.Complete(response, protocolTestNow); err != nil {
		t.Fatalf("fixture NOT_STARTED response: %v", err)
	}
}

func TestProtocolCoreSessionConcurrentAndSecretFree(t *testing.T) {
	pendingType := reflect.TypeOf(pendingRequest{})
	if pendingType.NumField() != 3 || pendingType.Field(0).Name != "requestID" || pendingType.Field(1).Name != "operation" || pendingType.Field(2).Name != "deadlineMillis" {
		t.Fatalf("pending session fields = %v; want requestID, operation, and deadline only", pendingType)
	}
	request := coreBaseRequest(OperationPrepareMutation)
	request.OperationID = protocolTestOperationID
	request.MutationKind = MutationImportCredential
	request.SelectedLocalPath = "/tmp/credential.p12"
	request.Bookmark = []byte("bookmark-secret")
	request.TransientPassword = []byte("password-secret")
	session := &Session{}
	start := make(chan struct{})
	results := make(chan error, 16)
	var workers sync.WaitGroup
	for range 16 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			results <- session.Begin(request, protocolTestNow)
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
	for _, secret := range [][]byte{request.OpaqueScope, request.Bookmark, request.TransientPassword} {
		for index := range secret {
			secret[index] = 0
		}
	}
	response := Response{RequestID: protocolTestRequestID, Outcome: OutcomePending, PendingItemID: protocolTestPendingID}
	completeStart := make(chan struct{})
	completeResults := make(chan error, 16)
	for range 16 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-completeStart
			completeResults <- session.Complete(response, protocolTestNow)
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

func TestProtocolCoreSessionDeadlineAndConcurrentExpiry(t *testing.T) {
	request := coreBaseRequest(OperationPrepareMutation)
	request.OperationID = protocolTestOperationID
	request.MutationKind = MutationRemoveCredential
	response := Response{RequestID: request.RequestID, Outcome: OutcomePending, PendingItemID: protocolTestPendingID}
	session := &Session{}
	if err := session.Begin(request, protocolTestNow); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 16)
	var workers sync.WaitGroup
	for range 16 {
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

func TestProtocolCoreSessionBeginCompleteRaceIsSerializable(t *testing.T) {
	for range 32 {
		first := coreBaseRequest(OperationPrepareMutation)
		first.OperationID = protocolTestOperationID
		first.MutationKind = MutationRemoveCredential
		second := first
		second.RequestID = "018bcfe5-6800-7000-8000-000000000006"
		session := &Session{}
		if err := session.Begin(first, protocolTestNow); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		completeResult := make(chan error, 1)
		beginResult := make(chan error, 1)
		go func() {
			<-start
			completeResult <- session.Complete(Response{RequestID: first.RequestID, Outcome: OutcomePending, PendingItemID: protocolTestPendingID}, protocolTestNow)
		}()
		go func() {
			<-start
			beginResult <- session.Begin(second, protocolTestNow)
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
			if err := session.Begin(second, protocolTestNow); err != nil {
				t.Fatalf("second Begin not admitted after completion: %v", err)
			}
		} else {
			assertCoreProtocolError(t, session.Begin(second, protocolTestNow), "SESSION_BUSY", 0)
		}
		if err := session.Complete(Response{RequestID: second.RequestID, Outcome: OutcomePending, PendingItemID: protocolTestPendingID}, protocolTestNow); err != nil {
			t.Fatalf("second completion: %v", err)
		}
	}
}

func coreBaseRequest(operation Operation) Request {
	return Request{
		ProtocolVersion: 1, RequestID: protocolTestRequestID, Operation: operation,
		DeadlineMillis: protocolTestNow.Add(time.Minute).UnixMilli(), Environment: EnvironmentSimulator,
		WorkspaceID: protocolTestWorkspaceID, OrganisationID: protocolTestOrgID,
		CanonicalABN: "51824753556", OpaqueScope: bytes.Repeat([]byte{0x5a}, 32),
	}
}

func coreGoldenFixturePath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller path unavailable")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "test", "fixtures", "sbr", "helper-protocol-v1.bin"))
}

func assertCoreProtocolError(t *testing.T, err error, code string, index int) {
	t.Helper()
	var protocolErr *Error
	if err == nil || !errors.As(err, &protocolErr) || protocolErr.Code() != code || err.Error() != code {
		t.Fatalf("case %d error = %#v, want %s", index, err, code)
	}
}

func assertCoreStableFuzzError(t *testing.T, err error) {
	t.Helper()
	var protocolErr *Error
	if !errors.As(err, &protocolErr) || protocolErr.Code() == "" || err.Error() != protocolErr.Code() || len(err.Error()) > 64 {
		t.Fatalf("unstable protocol error: %#v", err)
	}
}

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
