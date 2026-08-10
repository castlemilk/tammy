package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	tammyv1connect "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1/tammyv1connect"
	"github.com/tammyapp/tammy/services/core/internal/transport"
)

func TestProcessReadinessLifecycleAndClosedStdout(t *testing.T) {
	binary := buildCoreBinary(t)

	t.Run("serves until parent stdin closes", func(t *testing.T) {
		command := exec.Command(binary)
		stdin, err := command.StdinPipe()
		if err != nil {
			t.Fatalf("StdinPipe() error = %v", err)
		}
		stdout, err := command.StdoutPipe()
		if err != nil {
			t.Fatalf("StdoutPipe() error = %v", err)
		}
		stderr, err := command.StderrPipe()
		if err != nil {
			t.Fatalf("StderrPipe() error = %v", err)
		}
		if err := command.Start(); err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		t.Cleanup(func() {
			_ = command.Process.Kill()
			_ = command.Wait()
		})
		stderrResult := collectPipe(stderr)

		ready, err := transport.ReadReadiness(stdout)
		if err != nil {
			t.Fatalf("child emitted no valid bounded readiness record: %v", err)
		}
		assertProcessReadiness(t, ready)
		assertOfflineDiagnostics(t, ready)
		stdoutResult := collectPipe(stdout)

		if err := stdin.Close(); err != nil {
			t.Fatalf("closing child stdin: %v", err)
		}
		if err := waitForProcess(command, 3*time.Second); err != nil {
			t.Fatalf("child did not exit successfully after stdin EOF: %v", err)
		}
		remainder := receivePipeBytes(t, stdoutResult, "stdout")
		if len(remainder) != 0 {
			t.Fatal("child emitted more than one stdout readiness record")
		}
		capturedStderr := receivePipe(t, stderrResult)
		assertNoProcessSecrets(t, capturedStderr, ready.Capability)
	})

	t.Run("closed readiness pipe exits nonzero", func(t *testing.T) {
		command := exec.Command(binary)
		stdin, err := command.StdinPipe()
		if err != nil {
			t.Fatalf("StdinPipe() error = %v", err)
		}
		stdoutReader, stdoutWriter, err := os.Pipe()
		if err != nil {
			t.Fatalf("os.Pipe() error = %v", err)
		}
		if err := stdoutReader.Close(); err != nil {
			t.Fatalf("closing readiness reader: %v", err)
		}
		command.Stdout = stdoutWriter
		stderr, err := command.StderrPipe()
		if err != nil {
			t.Fatalf("StderrPipe() error = %v", err)
		}
		if err := command.Start(); err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		t.Cleanup(func() {
			_ = command.Process.Kill()
			_ = command.Wait()
		})
		_ = stdoutWriter.Close()
		stderrResult := collectPipe(stderr)

		err = waitForProcess(command, 3*time.Second)
		_ = stdin.Close()
		if err == nil {
			t.Fatal("child with closed readiness pipe exited successfully")
		}
		if command.ProcessState == nil || command.ProcessState.Success() {
			t.Fatal("child with closed readiness pipe did not report nonzero exit")
		}
		capturedStderr := receivePipe(t, stderrResult)
		if strings.Contains(capturedStderr, "PRIVATE KEY") {
			t.Fatal("child stderr contained private key material")
		}
	})
}

func TestConfiguredProcessAcceptsDevelopmentMemoryAnchorsOnlyWithAnAbsoluteDataRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "local-core")
	got, err := configuredProcess([]string{"--data-root", root})
	if err != nil || got.dataRoot != root || got.developmentMemoryAnchors {
		t.Fatalf("configuredProcess() = %#v, %v; want production local root %q", got, err, root)
	}
	got, err = configuredProcess([]string{"--data-root", root, "--development-memory-anchors"})
	if err != nil || got.dataRoot != root || !got.developmentMemoryAnchors {
		t.Fatalf("configuredProcess() = %#v, %v; want development local root %q", got, err, root)
	}
	for _, args := range [][]string{
		{"--data-root"},
		{"--data-root", "relative"},
		{"--unknown", root},
		{"--data-root", root, "extra"},
		{"--development-memory-anchors"},
		{"--development-memory-anchors", "--data-root", root},
		{"--data-root", root, "--development-memory-anchors", "extra"},
	} {
		if got, err := configuredProcess(args); err == nil || got != (processConfig{}) {
			t.Fatalf("configuredProcess(%q) = %#v, %v; want rejection", args, got, err)
		}
	}
}

func buildCoreBinary(t *testing.T) string {
	t.Helper()

	name := "tammy-core"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(t.TempDir(), name)
	command := exec.Command("go", "build", "-o", binary, ".")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("building tammy-core: %v\n%s", err, output)
	}
	return binary
}

func assertProcessReadiness(t *testing.T, ready transport.ReadinessRecord) {
	t.Helper()

	if ready.Protocol != transport.ReadinessProtocol {
		t.Fatalf("readiness protocol = %q, want %q", ready.Protocol, transport.ReadinessProtocol)
	}
	if ready.Port < 1 || ready.Port > 65535 {
		t.Fatal("readiness port is not a valid ephemeral port")
	}
	if ok := x509.NewCertPool().AppendCertsFromPEM([]byte(ready.CAPEM)); !ok {
		t.Fatal("readiness CA is not a certificate")
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(ready.Capability)
	if err != nil || len(decoded) != 32 {
		t.Fatal("readiness capability is not canonical unpadded Base64URL for 32 bytes")
	}
	clear(decoded)
}

func assertOfflineDiagnostics(t *testing.T, ready transport.ReadinessRecord) {
	t.Helper()

	roots := x509.NewCertPool()
	if ok := roots.AppendCertsFromPEM([]byte(ready.CAPEM)); !ok {
		t.Fatal("could not configure readiness CA")
	}
	httpClient := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS13,
				MaxVersion: tls.VersionTLS13,
				RootCAs:    roots,
				ServerName: "127.0.0.1",
				NextProtos: []string{"http/1.1"},
			},
		},
	}
	client := tammyv1connect.NewSystemServiceClient(
		httpClient,
		"https://127.0.0.1:"+decimalPort(ready.Port),
	)
	request := connect.NewRequest(&tammyv1.GetDiagnosticsRequest{})
	request.Header().Set(transport.CapabilityHeader, ready.Capability)
	response, err := client.GetDiagnostics(context.Background(), request)
	if err != nil {
		t.Fatalf("GetDiagnostics() failed: %s", redactProcessCapability(err.Error(), ready.Capability))
	}
	if response.Msg.GetApiVersion() != "tammy.v1" ||
		response.Msg.GetCoreVersion() != "dev" ||
		response.Msg.GetRuntimeMode() != tammyv1.RuntimeMode_RUNTIME_MODE_OFFLINE ||
		response.Msg.GetNetworkRequired() {
		t.Fatalf("GetDiagnostics() returned unexpected offline diagnostics: %+v", response.Msg)
	}
}

func waitForProcess(command *exec.Cmd, timeout time.Duration) error {
	result := make(chan error, 1)
	go func() {
		result <- command.Wait()
	}()
	select {
	case err := <-result:
		return err
	case <-time.After(timeout):
		_ = command.Process.Kill()
		<-result
		return context.DeadlineExceeded
	}
}

func collectPipe(reader io.Reader) <-chan []byte {
	result := make(chan []byte, 1)
	go func() {
		data, _ := io.ReadAll(reader)
		result <- data
	}()
	return result
}

func receivePipe(t *testing.T, result <-chan []byte) string {
	t.Helper()

	return string(receivePipeBytes(t, result, "stderr"))
}

func receivePipeBytes(t *testing.T, result <-chan []byte, name string) []byte {
	t.Helper()

	select {
	case data := <-result:
		return data
	case <-time.After(3 * time.Second):
		t.Fatalf("%s pipe did not close", name)
		return nil
	}
}

func assertNoProcessSecrets(t *testing.T, stderr, capability string) {
	t.Helper()

	if strings.Contains(stderr, capability) {
		t.Fatal("child stderr contained capability material")
	}
	if strings.Contains(stderr, "PRIVATE KEY") {
		t.Fatal("child stderr contained private key material")
	}
}

func redactProcessCapability(value, capability string) string {
	return strings.ReplaceAll(value, capability, "[redacted]")
}

func decimalPort(port int) string {
	var buffer bytes.Buffer
	const digits = "0123456789"
	var reversed [5]byte
	index := len(reversed)
	for port > 0 {
		index--
		reversed[index] = digits[port%10]
		port /= 10
	}
	buffer.Write(reversed[index:])
	return buffer.String()
}
