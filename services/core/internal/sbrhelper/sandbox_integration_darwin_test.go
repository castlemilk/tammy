//go:build darwin && arm64 && cgo

package sbrhelper

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

const (
	productionProbeEnvironment = "TAMMY_CORE_SANDBOX_PROBE"
	productionProbeStarted     = "SBR_SANDBOX_PROBE_STARTED"
	productionProbeResult      = "SBR_SANDBOX_PROBE_RESULT="
	productionProbeDenied      = "DENIED"
	productionProbeAllowed     = "ALLOWED"
	productionProbeUnexpected  = "UNEXPECTED"
)

func TestProductionRenderedSandboxEndToEnd(t *testing.T) {
	if os.Getenv(productionProbeEnvironment) == "1" {
		outcome := runProductionNetworkProbe()
		_, _ = os.Stdout.WriteString(productionProbeStarted + "\n" + productionProbeResult + outcome + "\n")
		switch outcome {
		case productionProbeDenied:
			os.Exit(0)
		case productionProbeAllowed:
			os.Exit(42)
		default:
			os.Exit(43)
		}
	}
	sandboxExec, err := exec.LookPath("sandbox-exec")
	if err != nil {
		t.Skip("SBR_SANDBOX_EXEC_UNAVAILABLE")
	}
	base, root, helper, probe, component, selected := stageSandboxIntegration(t)
	profile, guard, err := RenderDevelopmentSandboxProfile(SandboxProfileInput{
		TrustedBase: base, StagedRoot: root,
		StagedExecutables: []string{helper, probe}, StagedReadOnlyFiles: []string{component}, SelectedReadFiles: []string{selected},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	contents, err := profile.PrepareSpawn()
	if err != nil {
		t.Fatal(err)
	}
	profilePath := filepath.Join(base, "sandbox-profile.sb")
	if err := os.WriteFile(profilePath, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(profilePath, 0o600); err != nil {
		t.Fatal(err)
	}

	probeStdout, probeStderr, probeErr := runSandboxedCommand(t, sandboxExec, profilePath, probe,
		[]string{"-test.run=^TestProductionRenderedSandboxEndToEnd$"}, []string{productionProbeEnvironment + "=1"}, nil)
	if !bytes.Contains(probeStdout, []byte(productionProbeStarted)) {
		if sandboxApplyUnavailable(probeStderr, probeErr) {
			t.Skip("SBR_SANDBOX_EXEC_UNAVAILABLE")
		}
		t.Fatalf("production-profile probe did not initialize: %v stdout=%q stderr=%q", probeErr, probeStdout, probeStderr)
	}
	if bytes.Contains(probeStdout, []byte(productionProbeResult+productionProbeAllowed)) {
		t.Fatalf("production profile permitted bind/listen or outbound connect: %v stdout=%q stderr=%q", probeErr, probeStdout, probeStderr)
	}
	if probeErr != nil || !bytes.Contains(probeStdout, []byte(productionProbeResult+productionProbeDenied)) {
		t.Fatalf("production profile did not prove exact network denial: %v stdout=%q stderr=%q", probeErr, probeStdout, probeStderr)
	}

	if current, err := profile.PrepareSpawn(); err != nil || current != contents {
		t.Fatalf("spawn-boundary revalidation before helper: changed=%t error=%v", current != contents, err)
	}
	now := time.Now().UTC()
	request := Request{ProtocolVersion: ProtocolVersion, RequestID: "018bcfe5-6800-7000-8000-000000000001", Operation: OperationFixture,
		DeadlineMillis: now.Add(10 * time.Second).UnixMilli(), Environment: EnvironmentSimulator, SimulatorCase: SimulatorAccepted}
	payload, err := EncodeRequest(request, now)
	if err != nil {
		t.Fatal(err)
	}
	var framed bytes.Buffer
	if err := WriteFrame(&framed, payload); err != nil {
		t.Fatal(err)
	}
	zeroBytes(payload)
	helperStdout, helperStderr, helperErr := runSandboxedCommand(t, sandboxExec, profilePath, helper, nil, nil, framed.Bytes())
	if helperErr != nil || len(helperStderr) != 0 {
		t.Fatalf("synthetic helper failed under production profile: %v stdout=%x stderr=%q", helperErr, helperStdout, helperStderr)
	}
	responsePayload, err := ReadFrame(bytes.NewReader(helperStdout))
	if err != nil {
		t.Fatal(err)
	}
	response, err := DecodeResponse(responsePayload)
	zeroBytes(responsePayload)
	if err != nil || response.Outcome != OutcomeOK || response.RedactedResult != ResultFixtureSelected {
		t.Fatalf("sandboxed synthetic response=%#v error=%v", response, err)
	}
}

func stageSandboxIntegration(t *testing.T) (string, string, string, string, string, string) {
	t.Helper()
	base, err := os.MkdirTemp(".", "tammy-sbr-base-")
	if err != nil {
		t.Fatal(err)
	}
	base, err = filepath.Abs(base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	if err := os.Chmod(base, 0o700); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "tammy-sbr-runtime-Integration1")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(root, "tammy-sbr-helper")
	command := exec.Command("go", "build", "-o", helper, "./cmd/tammy-sbr-helper")
	command.Dir = helperModuleRoot(t)
	command.Env = append(os.Environ(), "GOWORK=off", "GOCACHE=/private/tmp/tammy-go-cache", "GOTMPDIR=/private/tmp")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build staged helper: %v\n%s", err, output)
	}
	if err := os.Chmod(helper, 0o500); err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(root, "tammy-sandbox-probe")
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	copyFile(t, source, probe, 0o500)
	component := filepath.Join(root, "component.bundle")
	if err := os.WriteFile(component, []byte("synthetic component"), 0o400); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(component, 0o400); err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(base, "selected-credential.p12")
	if err := os.WriteFile(selected, []byte("synthetic selected credential"), 0o600); err != nil {
		t.Fatal(err)
	}
	return base, root, helper, probe, component, selected
}

func helperModuleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../../..", "sbr-helper"))
}

func copyFile(t *testing.T, source, destination string, mode os.FileMode) {
	t.Helper()
	input, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(destination, mode); err != nil {
		t.Fatal(err)
	}
}

func runSandboxedCommand(t *testing.T, sandboxExec, profilePath, executable string, arguments, environment []string, stdin []byte) ([]byte, []byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	commandArguments := append([]string{"-f", profilePath, executable}, arguments...)
	command := exec.CommandContext(ctx, sandboxExec, commandArguments...)
	command.Env = append(os.Environ(), environment...)
	command.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if ctx.Err() != nil {
		t.Fatal("sandboxed child exceeded bounded timeout")
	}
	return stdout.Bytes(), stderr.Bytes(), err
}

type sandboxCapability uint8

const (
	sandboxUnexpected sandboxCapability = iota
	sandboxDenied
	sandboxAllowed
)

func runProductionNetworkProbe() string {
	listen := probeLoopbackListen()
	connect := probeClosedLoopbackConnect()
	if listen == sandboxAllowed || connect == sandboxAllowed {
		return productionProbeAllowed
	}
	if listen == sandboxDenied && connect == sandboxDenied {
		return productionProbeDenied
	}
	return productionProbeUnexpected
}

func probeLoopbackListen() sandboxCapability {
	descriptor, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	if err != nil {
		return classifySandboxError(err)
	}
	defer syscall.Close(descriptor)
	if err := syscall.Bind(descriptor, &syscall.SockaddrInet4{Port: 0, Addr: [4]byte{127, 0, 0, 1}}); err != nil {
		return classifySandboxError(err)
	}
	if err := syscall.Listen(descriptor, 1); err != nil {
		return classifySandboxError(err)
	}
	return sandboxAllowed
}

func probeClosedLoopbackConnect() sandboxCapability {
	descriptor, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	if err != nil {
		return classifySandboxError(err)
	}
	defer syscall.Close(descriptor)
	err = syscall.Connect(descriptor, &syscall.SockaddrInet4{Port: 1, Addr: [4]byte{127, 0, 0, 1}})
	if permissionDenied(err) {
		return sandboxDenied
	}
	// nil, ECONNREFUSED, and any other non-policy result prove networking
	// reached the host stack.
	return sandboxAllowed
}

func classifySandboxError(err error) sandboxCapability {
	if permissionDenied(err) {
		return sandboxDenied
	}
	return sandboxUnexpected
}

func permissionDenied(err error) bool {
	return errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES)
}

func sandboxApplyUnavailable(stderr []byte, runErr error) bool {
	if runErr == nil {
		return false
	}
	lower := bytes.ToLower(stderr)
	if bytes.Contains(lower, []byte("sandbox_apply")) &&
		(bytes.Contains(lower, []byte("operation not permitted")) || bytes.Contains(lower, []byte("not supported"))) {
		return true
	}
	exitError, ok := runErr.(*exec.ExitError)
	return ok && exitError.ExitCode() == 71 && bytes.Contains(lower, []byte("sandbox-exec"))
}
