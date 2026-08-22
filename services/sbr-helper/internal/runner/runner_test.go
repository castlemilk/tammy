package runner

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/sbr-helper/internal/evte"
	"github.com/tammyapp/tammy/services/sbr-helper/internal/protocol"
	"github.com/tammyapp/tammy/services/sbr-helper/internal/simulator"
)

const runnerRequestID = "018bcfe5-6800-7000-8000-000000000001"

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

type closedSigner struct {
	calls    int
	captured [][]byte
}

func (s *closedSigner) Execute(_ context.Context, request protocol.Request) protocol.Response {
	s.calls++
	s.captured = [][]byte{request.OpaqueScope, request.Bookmark, request.TransientPassword, request.TransientProductID, request.EndpointProfile}
	return protocol.NewErrorResponse(request.RequestID, protocol.StableErrorSecureStoreUnavailable)
}

type unusedRandom struct{}

func (unusedRandom) Read([]byte) (int, error) { return 0, nil }

type runnerPolicyDialer struct {
	calls  atomic.Int32
	result error
}

func (d *runnerPolicyDialer) Dial(context.Context, string, string) error {
	d.calls.Add(1)
	return d.result
}

type cleanupCapture struct {
	requestPayload  []byte
	responsePayload []byte
}

type blockingSigner struct {
	started  chan struct{}
	release  chan struct{}
	calls    atomic.Int32
	panic    string
	captured []byte
}

func (s *blockingSigner) Execute(_ context.Context, request protocol.Request) protocol.Response {
	s.calls.Add(1)
	s.captured = request.TransientPassword
	close(s.started)
	if s.panic != "" {
		panic(s.panic)
	}
	<-s.release
	return protocol.NewErrorResponse(request.RequestID, protocol.StableErrorSecureStoreUnavailable)
}

type advancingClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *advancingClock) Now() time.Time    { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *advancingClock) Set(now time.Time) { c.mu.Lock(); c.now = now; c.mu.Unlock() }

type deadlineSigner struct{ clock *advancingClock }

func (s deadlineSigner) Execute(_ context.Context, request protocol.Request) protocol.Response {
	s.clock.Set(time.UnixMilli(request.DeadlineMillis))
	return protocol.NewErrorResponse(request.RequestID, protocol.StableErrorSecureStoreUnavailable)
}

func (c *cleanupCapture) RequestPayloadCleared(payload []byte) {
	c.requestPayload = bytes.Clone(payload)
}
func (c *cleanupCapture) ResponsePayloadCleared(payload []byte) {
	c.responsePayload = bytes.Clone(payload)
}

func TestRunnerCleanEOFAndMalformedFrameFailClosed(t *testing.T) {
	deps := testDependencies(time.Now())
	for name, input := range map[string][]byte{
		"short header": {0, 0},
		"oversized": func() []byte {
			b := make([]byte, 4)
			binary.BigEndian.PutUint32(b, protocol.MaxPayloadSize+1)
			return b
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := RunOne(context.Background(), bytes.NewReader(input), &stdout, &stderr, deps); code == 0 {
				t.Fatal("malformed frame exited zero")
			}
			if stdout.Len() != 0 || stderr.String() != "{\"code\":\"SBR_HELPER_PROTOCOL_ERROR\"}\n" {
				t.Fatalf("stdout=%x stderr=%q", stdout.Bytes(), stderr.String())
			}
		})
	}
	var stdout, stderr bytes.Buffer
	if code := RunOne(context.Background(), bytes.NewReader(nil), &stdout, &stderr, deps); code != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("clean EOF code=%d stdout=%x stderr=%q", code, stdout.Bytes(), stderr.String())
	}
}

func TestRunnerFixtureCasesAndClosedCredentialOperations(t *testing.T) {
	now := time.Now().UTC()
	deps := testDependencies(now)
	for _, caseID := range []protocol.SimulatorCase{protocol.SimulatorAccepted, protocol.SimulatorNotStarted, protocol.SimulatorMaybeSent} {
		request := fixtureRequest(now, caseID)
		response, code, stderr := executeRequest(t, deps, request)
		if code != 0 || stderr != "" || response.RequestID != request.RequestID {
			t.Fatalf("case %d code=%d stderr=%q response=%#v", caseID, code, stderr, response)
		}
	}
	status := scopedRequest(now, protocol.OperationStatus, protocol.EnvironmentSimulator)
	response, code, stderr := executeRequest(t, deps, status)
	if code != 0 || stderr != "" || response.StableErrorCode != protocol.StableErrorSecureStoreUnavailable || deps.CredentialSigner.(*closedSigner).calls != 1 {
		t.Fatalf("closed status code=%d stderr=%q response=%#v calls=%d", code, stderr, response, deps.CredentialSigner.(*closedSigner).calls)
	}
	for _, captured := range deps.CredentialSigner.(*closedSigner).captured {
		for _, value := range captured {
			if value != 0 {
				t.Fatalf("credential signer retained uncleared request bytes: %x", captured)
			}
		}
	}
	evteRequest := scopedRequest(now, protocol.OperationStatus, protocol.EnvironmentEVTE)
	evteRequest.EndpointProfile = []byte("signed-profile")
	response, code, stderr = executeRequest(t, deps, evteRequest)
	if code != 0 || stderr != "" || response.StableErrorCode != protocol.StableErrorComponentUnavailable {
		t.Fatalf("EVTE code=%d stderr=%q response=%#v", code, stderr, response)
	}
}

func TestRunnerFailsClosedBeforeFixtureWhenDialerIsNotExactDenyOnly(t *testing.T) {
	now := time.Now().UTC()
	for name, result := range map[string]error{"allows": nil, "wrong error": errors.New("SBR_SIMULATOR_NETWORK_FORBIDDEN")} {
		t.Run(name, func(t *testing.T) {
			dialer := &runnerPolicyDialer{result: result}
			deps := testDependencies(now)
			deps.Dialer = dialer
			var stdout, stderr bytes.Buffer
			code := RunOne(context.Background(), bytes.NewReader(encodeFrame(t, fixtureRequest(now, protocol.SimulatorAccepted), now)), &stdout, &stderr, deps)
			if code == 0 || stdout.Len() != 0 || stderr.String() != "{\"code\":\"SBR_HELPER_UNAVAILABLE\"}\n" || dialer.calls.Load() != 1 {
				t.Fatalf("code=%d stdout=%x stderr=%q calls=%d", code, stdout.Bytes(), stderr.String(), dialer.calls.Load())
			}
		})
	}
}

func TestRunnerHelperDeathIsNonzeroAndCodeOnly(t *testing.T) {
	now := time.Now().UTC()
	request := fixtureRequest(now, protocol.SimulatorHelperDeath)
	input := encodeFrame(t, request, now)
	var stdout, stderr bytes.Buffer
	if code := RunOne(context.Background(), bytes.NewReader(input), &stdout, &stderr, testDependencies(now)); code == 0 {
		t.Fatal("helper death exited zero")
	}
	if stdout.Len() != 0 || stderr.String() != "{\"code\":\"SBR_HELPER_UNAVAILABLE\"}\n" {
		t.Fatalf("stdout=%x stderr=%q", stdout.Bytes(), stderr.String())
	}
}

func TestRunnerMalformedResponseWritesBoundedInvalidFrameAndFailsClosed(t *testing.T) {
	now := time.Now().UTC()
	request := fixtureRequest(now, protocol.SimulatorMalformedResponse)
	input := encodeFrame(t, request, now)
	var stdout, stderr bytes.Buffer
	if code := RunOne(context.Background(), bytes.NewReader(input), &stdout, &stderr, testDependencies(now)); code == 0 {
		t.Fatal("malformed simulator response exited zero")
	}
	payload, err := protocol.ReadFrame(bytes.NewReader(stdout.Bytes()))
	if err != nil {
		t.Fatalf("read intentionally malformed frame: %v", err)
	}
	if !bytes.Equal(payload, []byte{0x0a, 0x01, 'x'}) {
		t.Fatalf("malformed payload = %x", payload)
	}
	if _, err := protocol.DecodeResponse(payload); err == nil || err.Error() != "RESPONSE_INVALID" {
		t.Fatalf("DecodeResponse error = %v", err)
	}
	if stderr.String() != "{\"code\":\"SBR_HELPER_PROTOCOL_ERROR\"}\n" {
		t.Fatalf("stderr=%q", stderr.String())
	}
}

func TestRunnerUsesLocalTimeoutWithoutLeakingWork(t *testing.T) {
	now := time.Now().UTC()
	request := fixtureRequest(now, protocol.SimulatorTimeout)
	request.DeadlineMillis = now.Add(20 * time.Millisecond).UnixMilli()
	before := runtime.NumGoroutine()
	started := time.Now()
	response, code, stderr := executeRequest(t, testDependencies(now), request)
	if elapsed := time.Since(started); elapsed < 10*time.Millisecond || elapsed > time.Second {
		t.Fatalf("timeout elapsed %s", elapsed)
	}
	if code != 0 || stderr != "" || response.StableErrorCode != protocol.StableErrorDeadlineExpired {
		t.Fatalf("timeout code=%d stderr=%q response=%#v", code, stderr, response)
	}
	time.Sleep(20 * time.Millisecond)
	if after := runtime.NumGoroutine(); after > before+1 {
		t.Fatalf("goroutines before=%d after=%d", before, after)
	}
}

func TestRunnerDoesNotDispatchWhenParentContextAlreadyCancelled(t *testing.T) {
	now := time.Now().UTC()
	signer := &blockingSigner{started: make(chan struct{}), release: make(chan struct{})}
	deps := testDependencies(now)
	deps.CredentialSigner = signer
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stdout, stderr bytes.Buffer
	code := RunOne(ctx, bytes.NewReader(encodeFrame(t, scopedRequest(now, protocol.OperationStatus, protocol.EnvironmentSimulator), now)), &stdout, &stderr, deps)
	if code == 0 || signer.calls.Load() != 0 || stdout.Len() != 0 || stderr.String() != "{\"code\":\"SBR_HELPER_UNAVAILABLE\"}\n" {
		t.Fatalf("code=%d calls=%d stdout=%x stderr=%q", code, signer.calls.Load(), stdout.Bytes(), stderr.String())
	}
}

func TestRunnerReturnsOnCancellationWhenOperationIgnoresContext(t *testing.T) {
	now := time.Now().UTC()
	signer := &blockingSigner{started: make(chan struct{}), release: make(chan struct{})}
	deps := testDependencies(now)
	deps.CredentialSigner = signer
	ctx, cancel := context.WithCancel(context.Background())
	var stdout, stderr bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- RunOne(ctx, bytes.NewReader(encodeFrame(t, scopedRequest(now, protocol.OperationStatus, protocol.EnvironmentSimulator), now)), &stdout, &stderr, deps)
	}()
	select {
	case <-signer.started:
	case <-time.After(time.Second):
		t.Fatal("operation did not start")
	}
	cancel()
	select {
	case code := <-done:
		if code == 0 || stdout.Len() != 0 || stderr.String() != "{\"code\":\"SBR_HELPER_UNAVAILABLE\"}\n" {
			t.Fatalf("code=%d stdout=%x stderr=%q", code, stdout.Bytes(), stderr.String())
		}
	case <-time.After(time.Second):
		close(signer.release)
		t.Fatal("runner waited for context-ignoring operation")
	}
	close(signer.release)
}

func TestRunnerReturnsOnCancellationWhenComponentIgnoresContext(t *testing.T) {
	now := time.Now().UTC()
	component := &blockingSigner{started: make(chan struct{}), release: make(chan struct{})}
	deps := testDependencies(now)
	deps.ComponentClient = component
	ctx, cancel := context.WithCancel(context.Background())
	request := scopedRequest(now, protocol.OperationStatus, protocol.EnvironmentEVTE)
	request.EndpointProfile = []byte("signed-profile")
	var stdout, stderr bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- RunOne(ctx, bytes.NewReader(encodeFrame(t, request, now)), &stdout, &stderr, deps) }()
	select {
	case <-component.started:
	case <-time.After(time.Second):
		t.Fatal("component did not start")
	}
	cancel()
	select {
	case code := <-done:
		if code == 0 || stdout.Len() != 0 || stderr.String() != "{\"code\":\"SBR_HELPER_UNAVAILABLE\"}\n" {
			t.Fatalf("code=%d stdout=%x stderr=%q", code, stdout.Bytes(), stderr.String())
		}
	case <-time.After(time.Second):
		close(component.release)
		t.Fatal("runner waited for context-ignoring component")
	}
	close(component.release)
}

func TestRunnerMapsResultReturnedAtDeadlineBeforeSessionCompletion(t *testing.T) {
	now := time.Now().UTC()
	clock := &advancingClock{now: now}
	deps := testDependencies(now)
	deps.Clock = clock
	deps.CredentialSigner = deadlineSigner{clock: clock}
	response, code, stderr := executeRequest(t, deps, scopedRequest(now, protocol.OperationStatus, protocol.EnvironmentSimulator))
	if code != 0 || stderr != "" || response.StableErrorCode != protocol.StableErrorDeadlineExpired {
		t.Fatalf("code=%d stderr=%q response=%#v", code, stderr, response)
	}
}

func TestRunnerRecoversOperationPanicWithoutLeakingPanicOrSecret(t *testing.T) {
	now := time.Now().UTC()
	secret := "panic-secret-must-not-escape"
	signer := &blockingSigner{started: make(chan struct{}), release: make(chan struct{}), panic: secret}
	deps := testDependencies(now)
	deps.CredentialSigner = signer
	request := scopedRequest(now, protocol.OperationPrepareMutation, protocol.EnvironmentSimulator)
	request.OperationID = "018bcfe5-6800-7000-8000-000000000004"
	request.MutationKind = protocol.MutationImportCredential
	request.SelectedLocalPath = "/tmp/secret.p12"
	request.Bookmark = []byte("bookmark")
	request.TransientPassword = []byte(secret)
	var stdout, stderr bytes.Buffer
	code := RunOne(context.Background(), bytes.NewReader(encodeFrame(t, request, now)), &stdout, &stderr, deps)
	if code == 0 || stdout.Len() != 0 || stderr.String() != "{\"code\":\"SBR_HELPER_UNAVAILABLE\"}\n" {
		t.Fatalf("code=%d stdout=%x stderr=%q", code, stdout.Bytes(), stderr.String())
	}
	if strings.Contains(stderr.String(), secret) || strings.Contains(stderr.String(), "panic") || strings.Contains(stderr.String(), "goroutine") {
		t.Fatalf("panic details escaped: %q", stderr.String())
	}
	for _, value := range signer.captured {
		if value != 0 {
			t.Fatalf("panic path retained secret bytes: %x", signer.captured)
		}
	}
}

func TestRunnerClearsOwnedRequestAndResponsePayloads(t *testing.T) {
	now := time.Now().UTC()
	request := fixtureRequest(now, protocol.SimulatorAccepted)
	capture := &cleanupCapture{}
	deps := testDependencies(now)
	deps.cleanupObserver = capture
	_, code, stderr := executeRequest(t, deps, request)
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	for name, payload := range map[string][]byte{"request": capture.requestPayload, "response": capture.responsePayload} {
		if len(payload) == 0 {
			t.Fatalf("%s cleanup was not observed", name)
		}
		for _, value := range payload {
			if value != 0 {
				t.Fatalf("%s payload was not cleared: %x", name, payload)
			}
		}
	}
}

func testDependencies(now time.Time) Dependencies {
	return Dependencies{
		Clock:            fixedClock{now: now},
		RandomSource:     unusedRandom{},
		Dialer:           simulator.DenyDialer{},
		CredentialSigner: &closedSigner{},
		ComponentClient:  evte.Adapter{},
	}
}

func executeRequest(t *testing.T, deps Dependencies, request protocol.Request) (protocol.Response, int, string) {
	t.Helper()
	input := encodeFrame(t, request, deps.Clock.Now())
	var stdout, stderr bytes.Buffer
	code := RunOne(context.Background(), bytes.NewReader(input), &stdout, &stderr, deps)
	if stdout.Len() == 0 {
		return protocol.Response{}, code, stderr.String()
	}
	payload, err := protocol.ReadFrame(bytes.NewReader(stdout.Bytes()))
	if err != nil {
		t.Fatalf("read response frame: %v", err)
	}
	response, err := protocol.DecodeResponse(payload)
	for index := range payload {
		payload[index] = 0
	}
	if err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return response, code, stderr.String()
}

func encodeFrame(t *testing.T, request protocol.Request, now time.Time) []byte {
	t.Helper()
	payload, err := protocol.EncodeRequest(request, now)
	if err != nil {
		t.Fatal(err)
	}
	var framed bytes.Buffer
	if err := protocol.WriteFrame(&framed, payload); err != nil {
		t.Fatal(err)
	}
	return framed.Bytes()
}

func fixtureRequest(now time.Time, caseID protocol.SimulatorCase) protocol.Request {
	return protocol.Request{ProtocolVersion: protocol.ProtocolVersion, RequestID: runnerRequestID, Operation: protocol.OperationFixture,
		DeadlineMillis: now.Add(time.Minute).UnixMilli(), Environment: protocol.EnvironmentSimulator, SimulatorCase: caseID}
}

func scopedRequest(now time.Time, operation protocol.Operation, environment protocol.Environment) protocol.Request {
	return protocol.Request{ProtocolVersion: protocol.ProtocolVersion, RequestID: runnerRequestID, Operation: operation,
		DeadlineMillis: now.Add(time.Minute).UnixMilli(), Environment: environment,
		WorkspaceID: "018bcfe5-6800-7000-8000-000000000002", OrganisationID: "018bcfe5-6800-7000-8000-000000000003",
		CanonicalABN: "51824753556", OpaqueScope: bytes.Repeat([]byte{0x55}, 32)}
}
