package transport

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/buildinfo"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	tammyv1connect "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1/tammyv1connect"
)

var serverTestNow = time.Date(2026, time.July, 19, 9, 30, 0, 0, time.UTC)

func TestCertificatePropertiesAndLaunchFreshness(t *testing.T) {
	t.Parallel()

	randomness := &countingReader{reader: rand.Reader}
	var stderr bytes.Buffer
	first := startTestServer(t, &stderr, randomness)
	second := startTestServer(t, &stderr, randomness)
	firstReady := first.Ready()
	secondReady := second.Ready()

	if firstReady.CAPEM == secondReady.CAPEM {
		t.Fatal("two launches returned the same CA certificate")
	}
	if firstReady.Capability == secondReady.Capability {
		t.Fatal("two launches returned the same capability")
	}
	if randomness.bytes.Load() == 0 {
		t.Fatal("server construction did not use the injected randomness source")
	}

	ca := parseCertificatePEM(t, firstReady.CAPEM)
	if !ca.IsCA || !ca.BasicConstraintsValid {
		t.Fatal("CA certificate lacks valid CA basic constraints")
	}
	if ca.KeyUsage != x509.KeyUsageCertSign {
		t.Fatalf("CA key usage = %v, want CertSign only", ca.KeyUsage)
	}
	if !ca.NotBefore.Equal(serverTestNow.Add(-time.Minute)) ||
		!ca.NotAfter.Equal(serverTestNow.Add(30*time.Minute)) {
		t.Fatal("CA validity window differs from the required ephemeral window")
	}
	if ca.SerialNumber.Sign() <= 0 || ca.SerialNumber.BitLen() != 128 {
		t.Fatal("CA serial is not a positive 128-bit value")
	}

	state := dialServerTLS(t, firstReady)
	if state.Version != tls.VersionTLS13 {
		t.Fatalf("negotiated TLS version = %#x, want TLS 1.3", state.Version)
	}
	if state.NegotiatedProtocol != "http/1.1" {
		t.Fatalf("negotiated protocol = %q, want http/1.1", state.NegotiatedProtocol)
	}
	leaf := state.PeerCertificates[0]
	if leaf.IsCA {
		t.Fatal("leaf certificate is a CA")
	}
	if len(leaf.DNSNames) != 0 ||
		len(leaf.IPAddresses) != 1 ||
		!leaf.IPAddresses[0].Equal(net.IPv4(127, 0, 0, 1)) {
		t.Fatal("leaf SANs are not limited to 127.0.0.1")
	}
	if len(leaf.ExtKeyUsage) != 1 || leaf.ExtKeyUsage[0] != x509.ExtKeyUsageServerAuth {
		t.Fatal("leaf extended key usage is not server authentication only")
	}
	if !leaf.NotBefore.Equal(serverTestNow.Add(-time.Minute)) ||
		!leaf.NotAfter.Equal(serverTestNow.Add(30*time.Minute)) {
		t.Fatal("leaf validity window differs from the required ephemeral window")
	}
	if leaf.SerialNumber.Sign() <= 0 || leaf.SerialNumber.BitLen() != 128 {
		t.Fatal("leaf serial is not a positive 128-bit value")
	}

	roots := x509.NewCertPool()
	roots.AddCert(ca)
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:       roots,
		DNSName:     "127.0.0.1",
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		CurrentTime: serverTestNow,
	}); err != nil {
		t.Fatalf("leaf.Verify() error = %v", err)
	}

	assertServerSecretAbsent(t, stderr.String(), firstReady.Capability)
	assertServerSecretAbsent(t, stderr.String(), secondReady.Capability)
	if strings.Contains(stderr.String(), "PRIVATE KEY") {
		t.Fatal("stderr contained private key material")
	}
}

func TestServerLoopbackTLSConnectAuthenticationAndShutdown(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	server := startTestServer(t, &stderr, &countingReader{reader: rand.Reader})
	ready := server.Ready()

	if ready.Protocol != ReadinessProtocol {
		t.Fatalf("readiness protocol = %q, want %q", ready.Protocol, ReadinessProtocol)
	}
	if ready.Port < 1 || ready.Port > 65535 {
		t.Fatal("server did not receive a non-zero ephemeral port")
	}
	addr, ok := server.listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address type = %T, want *net.TCPAddr", server.listener.Addr())
	}
	if addr.IP.String() != "127.0.0.1" || addr.IP.To4() == nil {
		t.Fatalf("listener address = %v, want IPv4 127.0.0.1", addr.IP)
	}
	if addr.Port != ready.Port {
		t.Fatal("listener and readiness ports differ")
	}

	httpClient := authenticatedHTTPClient(t, ready)
	client := tammyv1connect.NewSystemServiceClient(
		httpClient,
		serverURL(ready),
	)
	request := connect.NewRequest(&tammyv1.GetDiagnosticsRequest{})
	request.Header().Set(CapabilityHeader, ready.Capability)
	response, err := client.GetDiagnostics(context.Background(), request)
	if err != nil {
		t.Fatalf("GetDiagnostics() error = %v", redactCapability(err, ready.Capability))
	}
	if response.Msg.GetApiVersion() != "tammy.v1" ||
		response.Msg.GetCoreVersion() != "test-core-version" ||
		response.Msg.GetRuntimeMode() != tammyv1.RuntimeMode_RUNTIME_MODE_OFFLINE ||
		response.Msg.GetNetworkRequired() {
		t.Fatalf("GetDiagnostics() returned unexpected offline diagnostics: %+v", response.Msg)
	}

	wrongCapability := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0xa5}, 32))
	tests := []struct {
		name    string
		headers []string
	}{
		{name: "missing"},
		{name: "wrong", headers: []string{wrongCapability}},
		{name: "duplicate", headers: []string{ready.Capability, ready.Capability}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := connect.NewRequest(&tammyv1.GetDiagnosticsRequest{})
			for _, value := range tt.headers {
				request.Header().Add(CapabilityHeader, value)
			}
			response, err := client.GetDiagnostics(context.Background(), request)
			if response != nil {
				t.Fatal("unauthenticated request returned a response")
			}
			if got := connect.CodeOf(err); got != connect.CodeUnauthenticated {
				t.Fatalf("unauthenticated request code = %v, want %v", got, connect.CodeUnauthenticated)
			}
			assertServerSecretAbsent(t, redactCapability(err, ready.Capability), ready.Capability)
		})
	}

	plainClient := &http.Client{Timeout: 2 * time.Second}
	if response, err := plainClient.Get("http://127.0.0.1:" + portString(ready.Port)); err == nil {
		response.Body.Close()
		if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
			t.Fatal("plain HTTP unexpectedly succeeded")
		}
	}

	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if elapsed := time.Since(started); elapsed >= 3*time.Second {
		t.Fatalf("Shutdown() took %v, want under 3s", elapsed)
	}
	select {
	case err, ok := <-server.Errors():
		if ok {
			t.Fatalf("Errors() returned unexpected serve error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Errors() did not close after Shutdown")
	}

	assertServerSecretAbsent(t, stderr.String(), ready.Capability)
	if strings.Contains(stderr.String(), "PRIVATE KEY") {
		t.Fatal("stderr contained private key material")
	}
}

func startTestServer(t *testing.T, stderr io.Writer, randomness io.Reader) *Server {
	t.Helper()

	server, err := NewServer(
		buildinfo.Info{Version: "test-core-version"},
		stderr,
		WithClock(func() time.Time { return serverTestNow }),
		WithRandomSource(randomness),
	)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	})
	return server
}

func authenticatedHTTPClient(t *testing.T, ready ReadinessRecord) *http.Client {
	t.Helper()

	roots := x509.NewCertPool()
	if ok := roots.AppendCertsFromPEM([]byte(ready.CAPEM)); !ok {
		t.Fatal("readiness CA PEM could not be added to roots")
	}
	return &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS13,
				MaxVersion: tls.VersionTLS13,
				RootCAs:    roots,
				ServerName: "127.0.0.1",
				NextProtos: []string{"http/1.1"},
				Time:       func() time.Time { return serverTestNow },
			},
		},
	}
}

func dialServerTLS(t *testing.T, ready ReadinessRecord) tls.ConnectionState {
	t.Helper()

	client := authenticatedHTTPClient(t, ready)
	transport := client.Transport.(*http.Transport)
	connection, err := tls.Dial("tcp4", "127.0.0.1:"+portString(ready.Port), transport.TLSClientConfig)
	if err != nil {
		t.Fatalf("tls.Dial() error = %v", err)
	}
	defer connection.Close()
	return connection.ConnectionState()
}

func parseCertificatePEM(t *testing.T, value string) *x509.Certificate {
	t.Helper()

	block, rest := pem.Decode([]byte(value))
	if block == nil || block.Type != "CERTIFICATE" || len(bytes.TrimSpace(rest)) != 0 {
		t.Fatal("CA PEM is not exactly one certificate")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("x509.ParseCertificate() error = %v", err)
	}
	return certificate
}

func serverURL(ready ReadinessRecord) string {
	return "https://127.0.0.1:" + portString(ready.Port)
}

func portString(port int) string {
	return strconv.Itoa(port)
}

func redactCapability(err error, capability string) string {
	if err == nil {
		return ""
	}
	return strings.ReplaceAll(err.Error(), capability, "[redacted]")
}

func assertServerSecretAbsent(t *testing.T, output, capability string) {
	t.Helper()
	if capability != "" && strings.Contains(output, capability) {
		t.Fatal("public output contained capability material")
	}
}

type countingReader struct {
	reader io.Reader
	bytes  atomic.Int64
}

func (reader *countingReader) Read(buffer []byte) (int, error) {
	read, err := reader.reader.Read(buffer)
	reader.bytes.Add(int64(read))
	return read, err
}
