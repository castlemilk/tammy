package transport

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/buildinfo"
	"github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1/tammyv1connect"
	"github.com/tammyapp/tammy/services/core/internal/system"
)

type serverConfig struct {
	clock      func() time.Time
	randomness io.Reader
}

// Option configures server construction.
type Option func(*serverConfig) error

// WithClock injects the clock used for the ephemeral certificate validity
// window.
func WithClock(clock func() time.Time) Option {
	return func(config *serverConfig) error {
		if clock == nil {
			return errors.New("local API clock is nil")
		}
		config.clock = clock
		return nil
	}
}

// WithRandomSource injects the source used for all credentials, serials, and
// capability generation.
func WithRandomSource(randomness io.Reader) Option {
	return func(config *serverConfig) error {
		if randomness == nil {
			return errors.New("local API randomness source is nil")
		}
		config.randomness = randomness
		return nil
	}
}

// Server owns one ephemeral loopback TLS listener and its Connect handler.
type Server struct {
	listener    net.Listener
	tlsListener net.Listener
	httpServer  *http.Server
	ready       ReadinessRecord
	serveErrors chan error

	mu      sync.Mutex
	started bool
}

// NewServer constructs and binds an ephemeral IPv4 loopback server.
func NewServer(
	info buildinfo.Info,
	stderr io.Writer,
	options ...Option,
) (*Server, error) {
	config := serverConfig{
		clock:      time.Now,
		randomness: rand.Reader,
	}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("local API option is nil")
		}
		if err := option(&config); err != nil {
			return nil, err
		}
	}
	if stderr == nil {
		stderr = io.Discard
	}

	credentials, err := generateEphemeralCredentials(config.randomness, config.clock())
	if err != nil {
		return nil, err
	}
	interceptor, err := NewCapabilityInterceptor(credentials.capability)
	if err != nil {
		return nil, errors.New("could not configure local API authentication")
	}

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, errors.New("could not listen on local API loopback")
	}
	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok || address.Port < 1 {
		_ = listener.Close()
		return nil, errors.New("local API listener returned an invalid address")
	}

	mux := http.NewServeMux()
	path, handler := tammyv1connect.NewSystemServiceHandler(
		system.NewService(info),
		connect.WithInterceptors(interceptor),
	)
	mux.Handle(path, handler)

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{credentials.certificate},
		MinVersion:   tls.VersionTLS13,
		MaxVersion:   tls.VersionTLS13,
		NextProtos:   []string{"http/1.1"},
	}
	server := &Server{
		listener:    listener,
		tlsListener: tls.NewListener(listener, tlsConfig),
		ready: ReadinessRecord{
			Protocol:   ReadinessProtocol,
			Port:       address.Port,
			CAPEM:      credentials.caPEM,
			Capability: credentials.capability,
		},
		serveErrors: make(chan error, 1),
	}
	server.httpServer = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 2 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
		ErrorLog: log.New(
			&structuredLogWriter{destination: stderr},
			"",
			0,
		),
	}
	return server, nil
}

// Ready returns the immutable parent-child readiness record.
func (server *Server) Ready() ReadinessRecord {
	return server.ready
}

// Start begins serving Connect requests. Serve failures are reported by
// Errors.
func (server *Server) Start() error {
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.started {
		return errors.New("local API server already started")
	}
	server.started = true

	go func() {
		err := server.httpServer.Serve(server.tlsListener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			server.serveErrors <- errors.New("local API server stopped unexpectedly")
		}
		close(server.serveErrors)
	}()
	return nil
}

// Errors closes after serving ends and reports at most one unexpected serving
// failure.
func (server *Server) Errors() <-chan error {
	return server.serveErrors
}

// Shutdown gracefully stops serving within the caller's context deadline.
func (server *Server) Shutdown(ctx context.Context) error {
	server.mu.Lock()
	started := server.started
	server.mu.Unlock()
	if !started {
		if err := server.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			return errors.New("could not close local API listener")
		}
		return nil
	}
	if err := server.httpServer.Shutdown(ctx); err != nil {
		return errors.New("could not shut down local API server")
	}
	return nil
}

type structuredLogWriter struct {
	mu          sync.Mutex
	destination io.Writer
}

func (writer *structuredLogWriter) Write(message []byte) (int, error) {
	record, err := json.Marshal(map[string]string{
		"component": "local_api",
		"event":     "http_server_error",
		"level":     "error",
	})
	if err != nil {
		return 0, err
	}
	record = append(record, '\n')

	writer.mu.Lock()
	defer writer.mu.Unlock()
	_, err = writer.destination.Write(record)
	if err != nil {
		return 0, err
	}
	return len(message), nil
}
