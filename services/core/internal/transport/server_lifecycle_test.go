package transport

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/buildinfo"
)

func TestServerLifecycleShutdownBeforeStartClosesErrors(t *testing.T) {
	server := newUnstartedTestServer(t)

	shutdownTestServer(t, server)
	assertServeErrorsClosed(t, server.Errors())
}

func TestServerLifecycleRepeatedShutdownIsIdempotent(t *testing.T) {
	server := newUnstartedTestServer(t)
	if err := server.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	shutdownTestServer(t, server)
	shutdownTestServer(t, server)
	assertServeErrorsClosed(t, server.Errors())
}

func TestServerLifecycleRejectsStartAfterShutdown(t *testing.T) {
	server := newUnstartedTestServer(t)

	shutdownTestServer(t, server)
	if err := server.Start(); err == nil {
		t.Fatal("Start() after Shutdown() error = nil, want rejection")
	}
	assertServeErrorsClosed(t, server.Errors())
}

func TestServerLifecycleRejectsRepeatedStart(t *testing.T) {
	server := newUnstartedTestServer(t)
	if err := server.Start(); err != nil {
		t.Fatalf("first Start() error = %v", err)
	}
	if err := server.Start(); err == nil {
		t.Fatal("second Start() error = nil, want rejection")
	}

	shutdownTestServer(t, server)
	assertServeErrorsClosed(t, server.Errors())
}

func TestServerLifecycleConcurrentStartShutdownSerializes(t *testing.T) {
	const iterations = 32

	for iteration := 0; iteration < iterations; iteration++ {
		server := newUnstartedTestServer(t)
		release := make(chan struct{})
		startResult := make(chan error, 1)
		shutdownResult := make(chan error, 1)

		go func() {
			<-release
			startResult <- server.Start()
		}()
		go func() {
			<-release
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			shutdownResult <- server.Shutdown(ctx)
		}()
		close(release)

		startErr := receiveLifecycleResult(t, startResult, "Start")
		if shutdownErr := receiveLifecycleResult(t, shutdownResult, "Shutdown"); shutdownErr != nil {
			t.Fatalf("concurrent Shutdown() error = %v", shutdownErr)
		}
		if startErr != nil && startErr.Error() != "local API server is stopped" {
			t.Fatalf("concurrent Start() error = %q, want nil or stopped rejection", startErr)
		}

		shutdownTestServer(t, server)
		assertServeErrorsClosed(t, server.Errors())
	}
}

func TestServerLifecycleShutdownAfterServeFailureDrainsActiveHandler(t *testing.T) {
	server := newUnstartedTestServer(t)
	server.tlsListener = &failAfterOneAcceptListener{
		Listener: server.tlsListener,
		err:      errors.New("forced accept failure"),
	}

	handlerEntered := make(chan struct{})
	handlerExited := make(chan struct{})
	releaseHandler := make(chan struct{})
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			close(releaseHandler)
		})
	}
	t.Cleanup(release)
	server.httpServer.Handler = http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		close(handlerEntered)
		<-releaseHandler
		response.WriteHeader(http.StatusNoContent)
		close(handlerExited)
	})

	if err := server.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	request, err := http.NewRequest(http.MethodGet, serverURL(server.Ready()), nil)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	request.Header.Set(CapabilityHeader, server.Ready().Capability)
	client := authenticatedHTTPClient(t, server.Ready())
	clientResult := make(chan httpClientResult, 1)
	go func() {
		response, err := client.Do(request)
		clientResult <- httpClientResult{response: response, err: err}
	}()

	assertLifecycleSignal(t, handlerEntered, "handler did not start")
	select {
	case serveErr, ok := <-server.Errors():
		if !ok {
			t.Fatal("Errors() closed without the expected serve failure")
		}
		if serveErr == nil || serveErr.Error() != "local API server stopped unexpectedly" {
			t.Fatalf("Errors() value = %v, want generic serve failure", serveErr)
		}
	case <-time.After(time.Second):
		t.Fatal("Errors() did not report the serve failure")
	}
	assertServeErrorsClosed(t, server.Errors())

	shutdownResult := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		shutdownResult <- server.Shutdown(ctx)
	}()
	select {
	case shutdownErr := <-shutdownResult:
		t.Fatalf("Shutdown() completed before active handler release: %v", shutdownErr)
	case <-time.After(100 * time.Millisecond):
	}

	release()
	assertLifecycleSignal(t, handlerExited, "handler did not exit")
	if shutdownErr := receiveLifecycleResult(t, shutdownResult, "Shutdown"); shutdownErr != nil {
		t.Fatalf("Shutdown() error = %v", shutdownErr)
	}
	select {
	case result := <-clientResult:
		if result.err != nil {
			t.Fatalf("client request error = %v", result.err)
		}
		defer result.response.Body.Close()
		if result.response.StatusCode != http.StatusNoContent {
			t.Fatalf("client status = %d, want %d", result.response.StatusCode, http.StatusNoContent)
		}
	case <-time.After(time.Second):
		t.Fatal("client request did not finish")
	}
	assertLifecycleSignal(t, server.serveDone, "serveDone did not close")
	assertLifecycleSignal(t, server.shutdownDone, "shutdownDone did not close")

	shutdownTestServer(t, server)
	assertServeErrorsClosed(t, server.Errors())
}

func TestStructuredLogWriterFullWriteConsumesInput(t *testing.T) {
	t.Parallel()

	var destination bytes.Buffer
	writer := &structuredLogWriter{destination: &destination}
	message := []byte("sensitive transport detail")

	written, err := writer.Write(message)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if written != len(message) {
		t.Fatalf("Write() bytes = %d, want %d", written, len(message))
	}
	if bytes.Contains(destination.Bytes(), message) {
		t.Fatal("structured log included the raw input message")
	}
	if bytes.Contains(destination.Bytes(), []byte("PRIVATE KEY")) {
		t.Fatal("structured log included private key material")
	}
}

func TestStructuredLogWriterShortWriteReturnsError(t *testing.T) {
	t.Parallel()

	writer := &structuredLogWriter{destination: shortWriteDestination{}}
	written, err := writer.Write([]byte("input"))
	if written != 0 {
		t.Fatalf("Write() bytes = %d, want 0 after failed structured emission", written)
	}
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("Write() error = %v, want %v", err, io.ErrShortWrite)
	}
}

func TestStructuredLogWriterPartialWriteWithErrorReturnsError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("destination failed")
	writer := &structuredLogWriter{
		destination: partialErrorDestination{err: wantErr},
	}
	written, err := writer.Write([]byte("input"))
	if written != 0 {
		t.Fatalf("Write() bytes = %d, want 0 after failed structured emission", written)
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("Write() error = %v, want destination error", err)
	}
}

func newUnstartedTestServer(t *testing.T) *Server {
	t.Helper()

	server, err := NewServer(
		testSystemRegistrar(t, buildinfo.Info{Version: "lifecycle-test"}),
		io.Discard,
		WithClock(func() time.Time { return serverTestNow }),
		WithRandomSource(rand.Reader),
	)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	})
	return server
}

func shutdownTestServer(t *testing.T, server *Server) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func assertServeErrorsClosed(t *testing.T, serveErrors <-chan error) {
	t.Helper()

	select {
	case err, ok := <-serveErrors:
		if ok {
			t.Fatalf("Errors() returned spurious error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Errors() did not close")
	}
}

func receiveLifecycleResult(t *testing.T, result <-chan error, operation string) error {
	t.Helper()

	select {
	case err := <-result:
		return err
	case <-time.After(time.Second):
		t.Fatalf("%s did not finish", operation)
		return nil
	}
}

func assertLifecycleSignal(t *testing.T, signal <-chan struct{}, failure string) {
	t.Helper()

	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal(failure)
	}
}

type failAfterOneAcceptListener struct {
	net.Listener

	mu       sync.Mutex
	accepted bool
	err      error
}

func (listener *failAfterOneAcceptListener) Accept() (net.Conn, error) {
	listener.mu.Lock()
	if listener.accepted {
		listener.mu.Unlock()
		return nil, listener.err
	}
	listener.accepted = true
	listener.mu.Unlock()
	return listener.Listener.Accept()
}

type httpClientResult struct {
	response *http.Response
	err      error
}

type shortWriteDestination struct{}

func (shortWriteDestination) Write(buffer []byte) (int, error) {
	return len(buffer) - 1, nil
}

type partialErrorDestination struct {
	err error
}

func (destination partialErrorDestination) Write(buffer []byte) (int, error) {
	return len(buffer) / 2, destination.err
}
