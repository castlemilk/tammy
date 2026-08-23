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
)

type serverConfig struct {
	clock      func() time.Time
	randomness io.Reader
}

// Option configures server construction.
type Option func(*serverConfig) error

type serverLifecycleState uint8

const (
	serverStateNew serverLifecycleState = iota
	serverStateRunning
	serverStateServeEnded
	serverStateStopping
	serverStateStopped
)

const (
	localAPIReadHeaderTimeout = 2 * time.Second
	localAPIReadTimeout       = 5 * time.Second
	// The longest explicitly bounded local operation is the 30-second SBR helper
	// request. Keep the transport write budget finite with an equal additional
	// window for response encoding and loopback TLS delivery.
	localAPIMaxRPCDuration = 30 * time.Second
	localAPIWriteTimeout   = 2 * localAPIMaxRPCDuration
	localAPIIdleTimeout    = 30 * time.Second
	localAPIMaxHeaderBytes = 16 << 10

	// Foundation RPCs are unary. The outer limit allows one maximum-sized
	// protobuf message plus the five-byte prefix used by framed Connect
	// protocols, while the Connect limit also applies after decompression.
	localAPIConnectMessageMaxBytes = 1 << 20
	localAPIConnectEnvelopeBytes   = 5
	localAPIRequestBodyMaxBytes    = localAPIConnectMessageMaxBytes +
		localAPIConnectEnvelopeBytes
)

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
	listener     net.Listener
	tlsListener  net.Listener
	httpServer   *http.Server
	ready        ReadinessRecord
	serveErrors  chan error
	serveDone    chan struct{}
	shutdownDone chan struct{}

	mu           sync.Mutex
	state        serverLifecycleState
	errorsClosed bool
	shutdownErr  error
}

// NewServer constructs and binds an ephemeral IPv4 loopback server.
func NewServer(
	registrar ServiceRegistrar,
	stderr io.Writer,
	options ...Option,
) (*Server, error) {
	if nilInterface(registrar) {
		return nil, ErrRegistrar
	}
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
	handler, err := registrar.Handler(
		connect.WithInterceptors(interceptor),
		connect.WithReadMaxBytes(localAPIConnectMessageMaxBytes),
	)
	if err != nil || nilInterface(handler) {
		return nil, errors.Join(ErrRegistrar, err)
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
		serveErrors:  make(chan error, 1),
		serveDone:    make(chan struct{}),
		shutdownDone: make(chan struct{}),
	}
	server.httpServer = &http.Server{
		Handler:           http.MaxBytesHandler(handler, localAPIRequestBodyMaxBytes),
		ReadHeaderTimeout: localAPIReadHeaderTimeout,
		ReadTimeout:       localAPIReadTimeout,
		WriteTimeout:      localAPIWriteTimeout,
		IdleTimeout:       localAPIIdleTimeout,
		MaxHeaderBytes:    localAPIMaxHeaderBytes,
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
	switch server.state {
	case serverStateNew:
		server.state = serverStateRunning
	case serverStateRunning:
		server.mu.Unlock()
		return errors.New("local API server already started")
	case serverStateServeEnded, serverStateStopping, serverStateStopped:
		server.mu.Unlock()
		return errors.New("local API server is stopped")
	default:
		server.mu.Unlock()
		return errors.New("local API server has an invalid state")
	}
	server.mu.Unlock()

	go func() {
		err := server.httpServer.Serve(server.tlsListener)
		server.mu.Lock()
		if server.state == serverStateRunning &&
			err != nil &&
			!errors.Is(err, http.ErrServerClosed) {
			server.serveErrors <- errors.New("local API server stopped unexpectedly")
		}
		if server.state == serverStateRunning {
			server.state = serverStateServeEnded
		}
		server.closeServeErrorsLocked()
		close(server.serveDone)
		server.mu.Unlock()
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
	switch server.state {
	case serverStateNew:
		server.state = serverStateStopped
		if err := server.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			server.shutdownErr = errors.New("could not close local API listener")
		}
		server.closeServeErrorsLocked()
		close(server.serveDone)
		close(server.shutdownDone)
		err := server.shutdownErr
		server.mu.Unlock()
		return err
	case serverStateRunning:
		server.state = serverStateStopping
		server.mu.Unlock()
	case serverStateServeEnded:
		server.state = serverStateStopping
		server.mu.Unlock()
	case serverStateStopping:
		done := server.shutdownDone
		server.mu.Unlock()
		select {
		case <-done:
			server.mu.Lock()
			err := server.shutdownErr
			server.mu.Unlock()
			return err
		case <-ctx.Done():
			return errors.New("could not shut down local API server")
		}
	case serverStateStopped:
		err := server.shutdownErr
		server.mu.Unlock()
		return err
	default:
		server.mu.Unlock()
		return errors.New("local API server has an invalid state")
	}

	var shutdownErr error
	if err := server.httpServer.Shutdown(ctx); err != nil {
		shutdownErr = errors.New("could not shut down local API server")
	}
	if err := server.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		shutdownErr = errors.New("could not close local API listener")
	}
	select {
	case <-server.serveDone:
	case <-ctx.Done():
		shutdownErr = errors.New("could not shut down local API server")
	}

	server.mu.Lock()
	server.state = serverStateStopped
	server.shutdownErr = shutdownErr
	close(server.shutdownDone)
	server.mu.Unlock()
	return shutdownErr
}

func (server *Server) closeServeErrorsLocked() {
	if server.errorsClosed {
		return
	}
	close(server.serveErrors)
	server.errorsClosed = true
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
	written, err := writer.destination.Write(record)
	if err != nil {
		return 0, err
	}
	if written != len(record) {
		return 0, io.ErrShortWrite
	}
	return len(message), nil
}
