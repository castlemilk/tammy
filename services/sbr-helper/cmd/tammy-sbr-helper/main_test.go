package main

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/sbr-helper/internal/protocol"
	"github.com/tammyapp/tammy/services/sbr-helper/internal/runner"
	"github.com/tammyapp/tammy/services/sbr-helper/internal/simulator"
	"github.com/tammyapp/tammy/services/sbr-helper/internal/vault"
)

const childRequestID = "018bcfe5-6800-7000-8000-000000000001"

func TestPackagedHelperComposesSimulatorVaultSignerAndUnavailableEVTE(t *testing.T) {
	dependencies := helperDependencies()
	if _, ok := dependencies.CredentialSigner.(*vault.SyntheticSigner); !ok {
		t.Fatalf("credential signer = %T", dependencies.CredentialSigner)
	}
	request := scopedRequest(time.Now().UTC(), protocol.OperationStatus)
	request.Environment = protocol.EnvironmentEVTE
	request.EndpointProfile = []byte("authenticated-but-unimplemented-evte-profile")
	response := dependencies.ComponentClient.Execute(context.Background(), request)
	if response.StableErrorCode != protocol.StableErrorComponentUnavailable {
		t.Fatalf("EVTE response = %#v", response)
	}
}

func TestHelperChildProcessFrameEOFAndRedaction(t *testing.T) {
	if !compiledHelperSupported {
		t.Skip("SBR_HELPER_UNAVAILABLE")
	}
	binary := buildHelper(t)
	now := time.Now().UTC()
	fixture := scopedRequest(now, protocol.OperationFixture)
	fixture.SimulatorCase = protocol.SimulatorAccepted
	stdout, stderr, err := runHelper(t, binary, frameRequest(t, fixture, now))
	if err != nil || len(stderr) != 0 {
		t.Fatalf("accepted fixture err=%v stderr=%q", err, stderr)
	}
	response := decodeResponseFrame(t, stdout)
	if response.RequestID != childRequestID || response.Outcome != protocol.OutcomeOK || response.RedactedResult != protocol.ResultFixtureSelected {
		t.Fatalf("response = %#v", response)
	}

}

func TestHelperChildProcessExactSimulatorCaseTable(t *testing.T) {
	if !compiledHelperSupported {
		t.Skip("SBR_HELPER_UNAVAILABLE")
	}
	binary := buildHelper(t)
	tests := []struct {
		name       string
		caseID     protocol.SimulatorCase
		result     protocol.Result
		errorCode  protocol.StableErrorCode
		exitError  bool
		lifecycle  string
		malformed  bool
		noResponse bool
	}{
		{name: "ACCEPTED", caseID: protocol.SimulatorAccepted, result: protocol.ResultFixtureSelected},
		{name: "NOT_STARTED", caseID: protocol.SimulatorNotStarted, result: protocol.ResultNotStarted},
		{name: "MAYBE_SENT", caseID: protocol.SimulatorMaybeSent, result: protocol.ResultRecoveryRequired},
		{name: "MALFORMED_RESPONSE", caseID: protocol.SimulatorMalformedResponse, exitError: true, lifecycle: "{\"code\":\"SBR_HELPER_PROTOCOL_ERROR\"}\n", malformed: true},
		{name: "HELPER_DEATH", caseID: protocol.SimulatorHelperDeath, exitError: true, lifecycle: "{\"code\":\"SBR_HELPER_UNAVAILABLE\"}\n", noResponse: true},
		{name: "TIMEOUT", caseID: protocol.SimulatorTimeout, errorCode: protocol.StableErrorDeadlineExpired},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			now := time.Now().UTC()
			deadline := now.Add(10 * time.Second)
			if test.caseID == protocol.SimulatorTimeout {
				deadline = now.Add(100 * time.Millisecond)
			}
			request := scopedRequest(now, protocol.OperationFixture)
			request.DeadlineMillis, request.SimulatorCase = deadline.UnixMilli(), test.caseID
			stdout, stderr, runErr := runHelper(t, binary, frameRequest(t, request, now))
			if (runErr != nil) != test.exitError || string(stderr) != test.lifecycle {
				t.Fatalf("run error=%v stderr=%q", runErr, stderr)
			}
			if test.noResponse {
				if len(stdout) != 0 {
					t.Fatalf("unexpected response: %x", stdout)
				}
				return
			}
			payload, err := protocol.ReadFrame(bytes.NewReader(stdout))
			if err != nil {
				t.Fatalf("read frame: %v", err)
			}
			response, decodeErr := protocol.DecodeResponse(payload)
			if test.malformed {
				if !bytes.Equal(payload, []byte{0x0a, 0x01, 'x'}) || decodeErr == nil || decodeErr.Error() != "RESPONSE_INVALID" {
					t.Fatalf("malformed payload=%x decode error=%v", payload, decodeErr)
				}
				return
			}
			if decodeErr != nil {
				t.Fatal(decodeErr)
			}
			expectedOutcome := protocol.OutcomeOK
			if test.errorCode != "" {
				expectedOutcome = protocol.OutcomeError
			}
			if response.RequestID != request.RequestID || response.Outcome != expectedOutcome || response.RedactedResult != test.result || response.StableErrorCode != test.errorCode {
				t.Fatalf("response=%#v", response)
			}
		})
	}

	now := time.Now().UTC()
	unknown := scopedRequest(now, protocol.OperationFixture)
	unknown.DeadlineMillis, unknown.SimulatorCase = now.Add(time.Second).UnixMilli(), protocol.SimulatorUnknown
	if _, err := protocol.EncodeRequest(unknown, now); err == nil || err.Error() != "REQUEST_INVALID" {
		t.Fatalf("UNKNOWN input error = %v", err)
	}
}

func TestHelperChildProcessCleanEOFAndMalformedNonzero(t *testing.T) {
	if !compiledHelperSupported {
		t.Skip("SBR_HELPER_UNAVAILABLE")
	}
	binary := buildHelper(t)
	stdout, stderr, err := runHelper(t, binary, nil)
	if err != nil || len(stdout) != 0 || len(stderr) != 0 {
		t.Fatalf("clean EOF err=%v stdout=%x stderr=%q", err, stdout, stderr)
	}
	stdout, stderr, err = runHelper(t, binary, []byte{0, 0})
	if err == nil {
		t.Fatal("malformed frame exited zero")
	}
	if len(stdout) != 0 || string(stderr) != "{\"code\":\"SBR_HELPER_PROTOCOL_ERROR\"}\n" || len(stderr) > 64 {
		t.Fatalf("malformed stdout=%x stderr=%q", stdout, stderr)
	}
	oversized := make([]byte, 4)
	oversized[0] = 0x01
	stdout, stderr, err = runHelper(t, binary, oversized)
	if err == nil || len(stdout) != 0 || string(stderr) != "{\"code\":\"SBR_HELPER_PROTOCOL_ERROR\"}\n" {
		t.Fatalf("oversized err=%v stdout=%x stderr=%q", err, stdout, stderr)
	}
}

type channelParentMonitor struct{ exit <-chan struct{} }

func (m channelParentMonitor) Wait(ctx context.Context) error {
	select {
	case <-m.exit:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (channelParentMonitor) Close() error { return nil }

type blockedMainSigner struct {
	started chan struct{}
	release chan struct{}
}

func (s blockedMainSigner) Execute(_ context.Context, request protocol.Request) protocol.Response {
	close(s.started)
	<-s.release
	return protocol.NewErrorResponse(request.RequestID, protocol.StableErrorSecureStoreUnavailable)
}

func TestRunWithRepeatedSignalsCancelsOnceAndJoinsRunner(t *testing.T) {
	reader, writer := io.Pipe()
	t.Cleanup(func() { _ = writer.Close() })
	signals := make(chan os.Signal, 2)
	never := make(chan struct{})
	done := make(chan int, 1)
	go func() {
		done <- runWith(reader, io.Discard, io.Discard, signals, channelParentMonitor{exit: never}, mainTestDependencies(unavailableMainSigner{}))
	}()
	signals <- syscall.SIGTERM
	signals <- syscall.SIGTERM
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("signal exit = %d", code)
		}
	case <-time.After(time.Second):
		t.Fatal("signal cancellation did not join runner")
	}
}

func TestRunWithParentDeathCancelsBlockedOperationAndJoinsRunner(t *testing.T) {
	now := time.Now().UTC()
	signer := blockedMainSigner{started: make(chan struct{}), release: make(chan struct{})}
	exit := make(chan struct{})
	request := scopedRequest(now, protocol.OperationStatus)
	framed := frameRequest(t, request, now)
	var stdout, stderr bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- runWith(io.NopCloser(bytes.NewReader(framed)), &stdout, &stderr, make(chan os.Signal), channelParentMonitor{exit: exit}, mainTestDependencies(signer))
	}()
	select {
	case <-signer.started:
	case <-time.After(time.Second):
		t.Fatal("blocked operation did not start")
	}
	close(exit)
	select {
	case code := <-done:
		if code == 0 || stdout.Len() != 0 || stderr.String() != "{\"code\":\"SBR_HELPER_UNAVAILABLE\"}\n" {
			t.Fatalf("code=%d stdout=%x stderr=%q", code, stdout.Bytes(), stderr.String())
		}
	case <-time.After(time.Second):
		close(signer.release)
		t.Fatal("parent death did not join runner")
	}
	close(signer.release)
}

func TestParentDeathChildIntegration(t *testing.T) {
	if os.Getenv("TAMMY_PARENT_DEATH_HARNESS") == "1" {
		helper := os.Getenv("TAMMY_PARENT_DEATH_HELPER")
		now := time.Now().UTC()
		request := scopedRequest(now, protocol.OperationFixture)
		request.DeadlineMillis, request.SimulatorCase = now.Add(30*time.Second).UnixMilli(), protocol.SimulatorTimeout
		command := exec.Command(helper)
		command.Stdin = bytes.NewReader(frameRequest(t, request, now))
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		if err := command.Start(); err != nil {
			os.Exit(90)
		}
		os.Exit(0)
	}
	if runtime.GOOS != "darwin" || !compiledHelperSupported {
		t.Skip("SBR_PARENT_MONITOR_UNAVAILABLE: requires Darwin arm64 cgo")
	}
	helper := buildHelper(t)
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, testBinary, "-test.run=TestParentDeathChildIntegration")
	command.Env = append(os.Environ(), "TAMMY_PARENT_DEATH_HARNESS=1", "TAMMY_PARENT_DEATH_HELPER="+helper)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("parent harness: %v stdout=%x stderr=%q", err, stdout.Bytes(), stderr.String())
	}
	if ctx.Err() != nil {
		t.Fatalf("parent-death helper was orphaned: %v", ctx.Err())
	}
	if stdout.Len() != 0 || stderr.String() != "{\"code\":\"SBR_HELPER_UNAVAILABLE\"}\n" {
		t.Fatalf("parent-death stdout=%x stderr=%q", stdout.Bytes(), stderr.String())
	}
}

type unusedMainRandom struct{}

func (unusedMainRandom) Read([]byte) (int, error) { return 0, nil }

type unavailableMainComponent struct{}

func (unavailableMainComponent) Execute(_ context.Context, request protocol.Request) protocol.Response {
	return protocol.NewErrorResponse(request.RequestID, protocol.StableErrorComponentUnavailable)
}

type unavailableMainSigner struct{}

func (unavailableMainSigner) Execute(_ context.Context, request protocol.Request) protocol.Response {
	return protocol.NewErrorResponse(request.RequestID, protocol.StableErrorSecureStoreUnavailable)
}

func mainTestDependencies(signer runner.CredentialSigner) runner.Dependencies {
	return runner.Dependencies{Clock: systemClock{}, RandomSource: unusedMainRandom{}, Dialer: simulator.DenyDialer{}, CredentialSigner: signer, ComponentClient: unavailableMainComponent{}}
}

func TestFinalHelperAndSimulatorDependencyClosureForbidsNetworkClients(t *testing.T) {
	root := helperModuleRoot(t)
	command := exec.Command("go", "list", "-deps", "./internal/simulator", "./cmd/tammy-sbr-helper")
	command.Dir = root
	command.Env = append(os.Environ(), "GOWORK=off", "GOCACHE=/private/tmp/tammy-go-cache", "GOTMPDIR=/private/tmp")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, output)
	}
	packages := make(map[string]bool)
	for _, line := range strings.Split(string(output), "\n") {
		packages[line] = true
	}
	for _, forbidden := range []string{"net", "net/http", "net/url", "crypto/tls", "os/exec", "plugin"} {
		if packages[forbidden] {
			t.Fatalf("forbidden dependency %q in helper composition", forbidden)
		}
	}
}

func TestAllProductionHelperSourcesForbidNetworkAndDynamicExecutionAPIs(t *testing.T) {
	root := helperModuleRoot(t)
	forbidden := []string{`"net"`, `"net/http"`, `"net/url"`, `"crypto/tls"`, `"plugin"`, `"os/exec"`, "syscall.Socket(", "unix.Socket(", "C.socket(", "C.connect(", "getaddrinfo(", "dlopen("}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		extension := filepath.Ext(path)
		if extension != ".go" && extension != ".c" && extension != ".m" && extension != ".mm" && extension != ".h" {
			return nil
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, token := range forbidden {
			if bytes.Contains(contents, []byte(token)) {
				t.Errorf("forbidden production token %q in %s", token, filepath.Base(path))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLinkedHelperHasNoNetworkClientSymbolsOrFrameworks(t *testing.T) {
	binary := buildHelper(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "tool", "nm", binary)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go tool nm: %v\n%s", err, output)
	}
	for _, symbol := range []string{" net/http.", " crypto/tls.", " syscall.Socket", " unix.Socket"} {
		if bytes.Contains(output, []byte(symbol)) {
			t.Fatalf("forbidden linked symbol %q", symbol)
		}
	}
	if runtime.GOOS == "darwin" {
		command = exec.CommandContext(ctx, "otool", "-L", binary)
		output, err = command.CombinedOutput()
		if err != nil {
			t.Fatalf("otool: %v\n%s", err, output)
		}
		for _, framework := range []string{"CFNetwork.framework", "Network.framework", "SecurityFoundation.framework"} {
			if bytes.Contains(output, []byte(framework)) {
				t.Fatalf("forbidden linked network framework %q", framework)
			}
		}
	}
}

func buildHelper(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tammy-sbr-helper")
	command := exec.Command("go", "build", "-o", path, "./cmd/tammy-sbr-helper")
	command.Dir = helperModuleRoot(t)
	command.Env = append(os.Environ(), "GOWORK=off", "GOCACHE=/private/tmp/tammy-go-cache", "GOTMPDIR=/private/tmp")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build helper: %v\n%s", err, output)
	}
	return path
}

func runHelper(t *testing.T, binary string, stdin []byte) ([]byte, []byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary)
	command.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if ctx.Err() != nil {
		t.Fatalf("helper child timeout: %v", ctx.Err())
	}
	return stdout.Bytes(), stderr.Bytes(), err
}

func frameRequest(t *testing.T, request protocol.Request, now time.Time) []byte {
	t.Helper()
	payload, err := protocol.EncodeRequest(request, now)
	if err != nil {
		t.Fatal(err)
	}
	var framed bytes.Buffer
	if err := protocol.WriteFrame(&framed, payload); err != nil {
		t.Fatal(err)
	}
	for index := range payload {
		payload[index] = 0
	}
	return framed.Bytes()
}

func decodeResponseFrame(t *testing.T, framed []byte) protocol.Response {
	t.Helper()
	payload, err := protocol.ReadFrame(bytes.NewReader(framed))
	if err != nil {
		t.Fatal(err)
	}
	response, err := protocol.DecodeResponse(payload)
	for index := range payload {
		payload[index] = 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func scopedRequest(now time.Time, operation protocol.Operation) protocol.Request {
	return protocol.Request{ProtocolVersion: protocol.ProtocolVersion, RequestID: childRequestID, Operation: operation,
		DeadlineMillis: now.Add(time.Minute).UnixMilli(), Environment: protocol.EnvironmentSimulator,
		WorkspaceID: "018bcfe5-6800-7000-8000-000000000002", OrganisationID: "018bcfe5-6800-7000-8000-000000000003",
		CanonicalABN: "51824753556", OpaqueScope: bytes.Repeat([]byte{0x55}, 32),
		ProfileFingerprint: bytes.Repeat([]byte{0x61}, 32), RegistrationFingerprint: bytes.Repeat([]byte{0x62}, 32),
		ComponentFingerprint: bytes.Repeat([]byte{0x63}, 32), ComponentVersion: "simulator-v1"}
}

func helperModuleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
}
