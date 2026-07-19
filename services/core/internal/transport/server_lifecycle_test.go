package transport

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
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
		buildinfo.Info{Version: "lifecycle-test"},
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
