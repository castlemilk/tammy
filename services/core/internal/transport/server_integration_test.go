package transport

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
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
	"github.com/tammyapp/tammy/services/core/internal/system"
	"google.golang.org/protobuf/encoding/protowire"
)

var serverTestNow = time.Date(2026, time.July, 19, 9, 30, 0, 0, time.UTC)

type recordingServiceRegistrar struct {
	options int
}

type delayedServiceRegistrar struct {
	delegate ServiceRegistrar
	delay    time.Duration
}

func (registrar delayedServiceRegistrar) Handler(options ...connect.HandlerOption) (http.Handler, error) {
	handler, err := registrar.delegate.Handler(options...)
	if err != nil {
		return nil, err
	}
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		time.Sleep(registrar.delay)
		handler.ServeHTTP(response, request)
	}), nil
}

func (registrar *recordingServiceRegistrar) Handler(options ...connect.HandlerOption) (http.Handler, error) {
	registrar.options = len(options)
	return http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}), nil
}

func TestServerReceivesRegistrarAndOwnsGeneratedHandlerSecurityOptions(t *testing.T) {
	registrar := &recordingServiceRegistrar{}
	server, err := NewServer(
		registrar,
		io.Discard,
		WithClock(func() time.Time { return serverTestNow }),
		WithRandomSource(rand.Reader),
	)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()
	if registrar.options != 2 {
		t.Fatalf("registrar handler options = %d, want interceptor and message bound", registrar.options)
	}

	var typedNil *recordingServiceRegistrar
	for _, invalid := range []ServiceRegistrar{nil, typedNil} {
		if constructed, err := NewServer(invalid, io.Discard); constructed != nil || !errors.Is(err, ErrRegistrar) {
			t.Fatalf("NewServer(%T) = %#v, %v; want ErrRegistrar", invalid, constructed, err)
		}
	}
}

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
		!ca.NotAfter.Equal(serverTestNow.AddDate(100, 0, 0)) {
		t.Fatal("CA validity window does not span the ephemeral process identity")
	}
	if ca.SerialNumber.Sign() <= 0 || ca.SerialNumber.BitLen() != 128 {
		t.Fatal("CA serial is not a positive 128-bit value")
	}

	state := dialServerTLSAt(t, firstReady, serverTestNow)
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
		!leaf.NotAfter.Equal(serverTestNow.AddDate(100, 0, 0)) {
		t.Fatal("leaf validity window does not span the ephemeral process identity")
	}
	if leaf.SerialNumber.Sign() <= 0 || leaf.SerialNumber.BitLen() != 128 {
		t.Fatal("leaf serial is not a positive 128-bit value")
	}

	roots := x509.NewCertPool()
	roots.AddCert(ca)
	for _, verificationTime := range []time.Time{
		serverTestNow,
		serverTestNow.Add(31 * time.Minute),
		serverTestNow.AddDate(50, 0, 0),
	} {
		if _, err := leaf.Verify(x509.VerifyOptions{
			Roots:       roots,
			DNSName:     "127.0.0.1",
			KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			CurrentTime: verificationTime,
		}); err != nil {
			t.Fatalf("leaf.Verify() at %v error = %v", verificationTime, err)
		}
		dialServerTLSAt(t, firstReady, verificationTime)
	}

	secondState := dialServerTLSAt(t, secondReady, serverTestNow)
	if bytes.Equal(leaf.Raw, secondState.PeerCertificates[0].Raw) {
		t.Fatal("two launches returned the same leaf certificate")
	}

	assertServerSecretAbsent(t, stderr.String(), firstReady.Capability)
	assertServerSecretAbsent(t, stderr.String(), secondReady.Capability)
	if strings.Contains(stderr.String(), "PRIVATE KEY") {
		t.Fatal("stderr contained private key material")
	}
}

func TestServerHTTPTimeoutConfiguration(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	server := newTestServer(t, &stderr, &countingReader{reader: rand.Reader})

	if got, want := server.httpServer.ReadHeaderTimeout, 2*time.Second; got != want {
		t.Fatalf("ReadHeaderTimeout = %s, want %s", got, want)
	}
	if got, want := server.httpServer.ReadTimeout, 5*time.Second; got != want {
		t.Fatalf("ReadTimeout = %s, want %s", got, want)
	}
	if got, want := server.httpServer.WriteTimeout, 60*time.Second; got != want {
		t.Fatalf("WriteTimeout = %s, want %s", got, want)
	}
	if got, want := server.httpServer.IdleTimeout, 30*time.Second; got != want {
		t.Fatalf("IdleTimeout = %s, want %s", got, want)
	}
	if got, want := server.httpServer.MaxHeaderBytes, 16<<10; got != want {
		t.Fatalf("MaxHeaderBytes = %d, want %d", got, want)
	}
}

func TestServerDoesNotCorruptTLSResponseForLongRunningLocalRPC(t *testing.T) {
	registrar := delayedServiceRegistrar{
		delegate: testSystemRegistrar(t, buildinfo.Info{Version: "slow-test-core"}),
		delay:    250 * time.Millisecond,
	}
	server, err := NewServer(
		registrar,
		io.Discard,
		WithClock(func() time.Time { return serverTestNow }),
		WithRandomSource(rand.Reader),
	)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	})
	if err := server.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ready := server.Ready()
	httpClient := authenticatedHTTPClient(t, ready)
	httpClient.Timeout = 3 * time.Second
	client := tammyv1connect.NewSystemServiceClient(httpClient, serverURL(ready))
	request := connect.NewRequest(&tammyv1.GetDiagnosticsRequest{})
	request.Header().Set(CapabilityHeader, ready.Capability)
	response, err := client.GetDiagnostics(context.Background(), request)
	if err != nil {
		t.Fatalf("GetDiagnostics() error = %v", redactCapability(err, ready.Capability))
	}
	if got, want := response.Msg.GetCoreVersion(), "slow-test-core"; got != want {
		t.Fatalf("core version = %q, want %q", got, want)
	}
}

func TestServerFiniteWriteTimeoutTerminatesResponseBeyondInjectedBound(t *testing.T) {
	registrar := delayedServiceRegistrar{delegate: testSystemRegistrar(t, buildinfo.Info{Version: "bounded-test-core"}), delay: 150 * time.Millisecond}
	server, err := NewServer(registrar, io.Discard, WithClock(func() time.Time { return serverTestNow }), WithRandomSource(rand.Reader))
	if err != nil {
		t.Fatal(err)
	}
	server.httpServer.WriteTimeout = 50 * time.Millisecond
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	})
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	ready := server.Ready()
	httpClient := authenticatedHTTPClient(t, ready)
	httpClient.Timeout = time.Second
	client := tammyv1connect.NewSystemServiceClient(httpClient, serverURL(ready))
	request := connect.NewRequest(&tammyv1.GetDiagnosticsRequest{})
	request.Header().Set(CapabilityHeader, ready.Capability)
	if _, err := client.GetDiagnostics(context.Background(), request); err == nil {
		t.Fatal("response beyond injected finite write timeout unexpectedly succeeded")
	}
}

func TestServerBoundsSlowTLSHeadersAndBodies(t *testing.T) {
	t.Parallel()

	const (
		testReadHeaderTimeout = 150 * time.Millisecond
		testReadTimeout       = 750 * time.Millisecond
		testWriteTimeout      = 2 * time.Second
		outerDeadline         = 3 * time.Second
	)

	var stderr bytes.Buffer
	server := newTestServer(t, &stderr, &countingReader{reader: rand.Reader})
	server.httpServer.ReadHeaderTimeout = testReadHeaderTimeout
	server.httpServer.ReadTimeout = testReadTimeout
	server.httpServer.WriteTimeout = testWriteTimeout
	if err := server.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	ready := server.Ready()

	t.Run("partial TLS handshake", func(t *testing.T) {
		connection, err := net.DialTimeout(
			"tcp4",
			"127.0.0.1:"+portString(ready.Port),
			time.Second,
		)
		if err != nil {
			t.Fatalf("dial server: %v", err)
		}
		defer connection.Close()

		if _, err := connection.Write([]byte{0x16}); err != nil {
			t.Fatalf("write partial TLS record: %v", err)
		}
		assertConnectionTerminatesBefore(t, connection, outerDeadline)
	})

	t.Run("incomplete HTTP headers", func(t *testing.T) {
		connection := dialServerTLSConnection(t, ready)
		defer connection.Close()

		if _, err := io.WriteString(
			connection,
			"POST "+tammyv1connect.SystemServiceGetDiagnosticsProcedure+" HTTP/1.1\r\n"+
				"Host: 127.0.0.1:"+portString(ready.Port)+"\r\n"+
				"Content-Type: application/proto\r\n",
		); err != nil {
			t.Fatalf("write incomplete HTTP headers: %v", err)
		}
		assertConnectionTerminatesBefore(t, connection, outerDeadline)
	})

	for _, test := range []struct {
		name       string
		capability string
	}{
		{name: "missing capability"},
		{name: "valid capability", capability: ready.Capability},
	} {
		t.Run("incomplete body with "+test.name, func(t *testing.T) {
			connection := dialServerTLSConnection(t, ready)
			defer connection.Close()

			request := "POST " + tammyv1connect.SystemServiceGetDiagnosticsProcedure + " HTTP/1.1\r\n" +
				"Host: 127.0.0.1:" + portString(ready.Port) + "\r\n" +
				"Content-Type: application/proto\r\n" +
				"Connect-Protocol-Version: 1\r\n" +
				"Content-Length: 1024\r\n" +
				"Connection: close\r\n"
			if test.capability != "" {
				request += CapabilityHeader + ": " + test.capability + "\r\n"
			}
			request += "\r\n"

			if _, err := io.WriteString(connection, request); err != nil {
				t.Fatalf("write HTTP request headers: %v", err)
			}
			if _, err := connection.Write([]byte{0x0a}); err != nil {
				t.Fatalf("write partial Connect request body: %v", err)
			}
			assertConnectionTerminatesBefore(t, connection, outerDeadline)
		})
	}

	t.Run("legitimate Connect request still succeeds", func(t *testing.T) {
		client := authenticatedHTTPClient(t, ready)
		client.Timeout = outerDeadline
		connectClient := tammyv1connect.NewSystemServiceClient(client, serverURL(ready))
		request := connect.NewRequest(&tammyv1.GetDiagnosticsRequest{})
		request.Header().Set(CapabilityHeader, ready.Capability)

		response, err := connectClient.GetDiagnostics(context.Background(), request)
		if err != nil {
			t.Fatalf("GetDiagnostics() error = %v", redactCapability(err, ready.Capability))
		}
		if response.Msg.GetApiVersion() != "tammy.v1" {
			t.Fatalf("GetDiagnostics() API version = %q, want tammy.v1", response.Msg.GetApiVersion())
		}
	})

	shutdownContext, cancel := context.WithTimeout(context.Background(), outerDeadline)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	select {
	case err, ok := <-server.Errors():
		if ok && err != nil {
			t.Fatalf("Errors() = %v after shutdown", err)
		}
	case <-time.After(outerDeadline):
		t.Fatal("Errors() did not close after graceful shutdown")
	}
}

func TestServerBoundsCompletedOversizedRequests(t *testing.T) {
	t.Parallel()

	const (
		connectMessageMaxBytes  = 1 << 20
		connectEnvelopePrefix   = 5
		httpRequestBodyMaxBytes = connectMessageMaxBytes + connectEnvelopePrefix
		maxErrorResponseBytes   = 4 << 10
		requestDeadline         = 4 * time.Second
	)

	var stderr bytes.Buffer
	server := startTestServer(t, &stderr, &countingReader{reader: rand.Reader})
	ready := server.Ready()
	client := authenticatedHTTPClient(t, ready)
	client.Timeout = requestDeadline
	transport := client.Transport.(*http.Transport)
	t.Cleanup(transport.CloseIdleConnections)

	outerOversizedBody := unknownProtobufMessageOfSize(
		t,
		httpRequestBodyMaxBytes+1,
	)
	for _, test := range []struct {
		name       string
		capability string
	}{
		{name: "missing capability"},
		{name: "valid capability", capability: ready.Capability},
	} {
		t.Run("outer body limit with "+test.name, func(t *testing.T) {
			response := postProtobufRequest(
				t,
				client,
				ready,
				outerOversizedBody,
				test.capability,
			)
			assertConnectResourceExhaustedResponse(
				t,
				response,
				maxErrorResponseBytes,
				fmt.Sprintf("configured max %d", connectMessageMaxBytes),
				"request body too large",
			)
		})
	}

	t.Run("decoded Connect message limit", func(t *testing.T) {
		response := postProtobufRequest(
			t,
			client,
			ready,
			unknownProtobufMessageOfSize(t, connectMessageMaxBytes+1),
			ready.Capability,
		)
		assertConnectResourceExhaustedResponse(
			t,
			response,
			maxErrorResponseBytes,
			fmt.Sprintf(
				"message size %d is larger than configured max %d",
				connectMessageMaxBytes+1,
				connectMessageMaxBytes,
			),
		)
	})

	t.Run("just under decoded message limit succeeds", func(t *testing.T) {
		connectClient := tammyv1connect.NewSystemServiceClient(client, serverURL(ready))
		request := connect.NewRequest(&tammyv1.GetDiagnosticsRequest{})
		request.Msg.ProtoReflect().SetUnknown(
			unknownProtobufMessageOfSize(t, connectMessageMaxBytes-1),
		)
		request.Header().Set(CapabilityHeader, ready.Capability)

		response, err := connectClient.GetDiagnostics(context.Background(), request)
		if err != nil {
			t.Fatalf("GetDiagnostics() error = %v", redactCapability(err, ready.Capability))
		}
		if response.Msg.GetApiVersion() != "tammy.v1" {
			t.Fatalf("GetDiagnostics() API version = %q, want tammy.v1", response.Msg.GetApiVersion())
		}
	})

	transport.CloseIdleConnections()
	shutdownContext, cancel := context.WithTimeout(context.Background(), requestDeadline)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	select {
	case err, ok := <-server.Errors():
		if ok && err != nil {
			t.Fatalf("Errors() = %v after shutdown", err)
		}
	case <-time.After(requestDeadline):
		t.Fatal("Errors() did not close after oversized requests and graceful shutdown")
	}
	assertServerSecretAbsent(t, stderr.String(), ready.Capability)
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

	t.Run("plaintext Connect is rejected", func(t *testing.T) {
		plainClient := &http.Client{Timeout: 2 * time.Second}
		if response, err := plainClient.Get("http://127.0.0.1:" + portString(ready.Port)); err == nil {
			response.Body.Close()
			if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
				t.Fatal("plain HTTP unexpectedly succeeded")
			}
		}
		plaintextConnect := tammyv1connect.NewSystemServiceClient(
			plainClient,
			"http://127.0.0.1:"+portString(ready.Port),
		)
		plaintextRequest := connect.NewRequest(&tammyv1.GetDiagnosticsRequest{})
		plaintextRequest.Header().Set(CapabilityHeader, ready.Capability)
		plaintextResponse, plaintextErr := plaintextConnect.GetDiagnostics(
			context.Background(),
			plaintextRequest,
		)
		if plaintextErr == nil || plaintextResponse != nil {
			t.Fatal("plaintext Connect diagnostics unexpectedly succeeded")
		}
		assertServerSecretAbsent(
			t,
			plaintextErr.Error(),
			ready.Capability,
		)
	})

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

	server := newTestServer(t, stderr, randomness)
	if err := server.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	return server
}

func newTestServer(t *testing.T, stderr io.Writer, randomness io.Reader) *Server {
	t.Helper()

	server, err := NewServer(
		testSystemRegistrar(t, buildinfo.Info{Version: "test-core-version"}),
		stderr,
		WithClock(func() time.Time { return serverTestNow }),
		WithRandomSource(randomness),
	)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	})
	return server
}

func testSystemRegistrar(t *testing.T, info buildinfo.Info) ServiceRegistrar {
	t.Helper()
	registrar, err := NewGeneratedRegistrar([]GeneratedHandlerFactory{
		func(options ...connect.HandlerOption) (string, http.Handler) {
			return tammyv1connect.NewSystemServiceHandler(system.NewService(info), options...)
		},
	})
	if err != nil {
		t.Fatalf("NewGeneratedRegistrar() error = %v", err)
	}
	return registrar
}

func dialServerTLSConnection(t *testing.T, ready ReadinessRecord) *tls.Conn {
	t.Helper()

	client := authenticatedHTTPClient(t, ready)
	transport := client.Transport.(*http.Transport)
	connection, err := tls.Dial(
		"tcp4",
		"127.0.0.1:"+portString(ready.Port),
		transport.TLSClientConfig.Clone(),
	)
	if err != nil {
		t.Fatalf("tls.Dial() error = %v", err)
	}
	return connection
}

func assertConnectionTerminatesBefore(t *testing.T, connection net.Conn, deadline time.Duration) {
	t.Helper()

	if err := connection.SetReadDeadline(time.Now().Add(deadline)); err != nil {
		t.Fatalf("set client read deadline: %v", err)
	}
	_, err := io.ReadAll(connection)
	if networkError, ok := err.(net.Error); ok && networkError.Timeout() {
		t.Fatalf("server did not terminate the incomplete request within %s", deadline)
	}
}

func unknownProtobufMessageOfSize(t *testing.T, targetSize int) []byte {
	t.Helper()

	tagSize := protowire.SizeTag(1)
	for payloadSize := targetSize - tagSize; payloadSize >= 0; payloadSize-- {
		if tagSize+protowire.SizeBytes(payloadSize) != targetSize {
			continue
		}
		message := make([]byte, 0, targetSize)
		message = protowire.AppendTag(message, 1, protowire.BytesType)
		message = protowire.AppendVarint(message, uint64(payloadSize))
		message = append(message, make([]byte, payloadSize)...)
		return message
	}
	t.Fatalf("cannot construct a valid unknown protobuf field of size %d", targetSize)
	return nil
}

func postProtobufRequest(
	t *testing.T,
	client *http.Client,
	ready ReadinessRecord,
	body []byte,
	capability string,
) *http.Response {
	t.Helper()

	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		serverURL(ready)+tammyv1connect.SystemServiceGetDiagnosticsProcedure,
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatalf("http.NewRequestWithContext() error = %v", err)
	}
	request.Header.Set("Content-Type", "application/proto")
	request.Header.Set("Connect-Protocol-Version", "1")
	if capability != "" {
		request.Header.Set(CapabilityHeader, capability)
	}

	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("completed protobuf request error = %v", redactCapability(err, ready.Capability))
	}
	return response
}

func assertConnectResourceExhaustedResponse(
	t *testing.T,
	response *http.Response,
	maxResponseBytes int64,
	messageFragments ...string,
) {
	t.Helper()
	defer response.Body.Close()

	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf(
			"oversized request HTTP status = %d, want %d",
			response.StatusCode,
			http.StatusTooManyRequests,
		)
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("oversized request content type = %q, want application/json", contentType)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		t.Fatalf("read oversized request response: %v", err)
	}
	if int64(len(body)) > maxResponseBytes {
		t.Fatalf(
			"oversized request response length = %d, want at most %d",
			len(body),
			maxResponseBytes,
		)
	}
	var connectError struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &connectError); err != nil {
		t.Fatalf("decode oversized request response: %v", err)
	}
	if connectError.Code != connect.CodeResourceExhausted.String() {
		t.Fatalf(
			"oversized request Connect code = %q, want %q",
			connectError.Code,
			connect.CodeResourceExhausted,
		)
	}
	for _, fragment := range messageFragments {
		if !strings.Contains(connectError.Message, fragment) {
			t.Fatalf(
				"oversized request message = %q, want fragment %q",
				connectError.Message,
				fragment,
			)
		}
	}
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

func dialServerTLSAt(
	t *testing.T,
	ready ReadinessRecord,
	verificationTime time.Time,
) tls.ConnectionState {
	t.Helper()

	client := authenticatedHTTPClient(t, ready)
	transport := client.Transport.(*http.Transport)
	tlsConfig := transport.TLSClientConfig.Clone()
	tlsConfig.Time = func() time.Time { return verificationTime }
	connection, err := tls.Dial("tcp4", "127.0.0.1:"+portString(ready.Port), tlsConfig)
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
