package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/buildinfo"
	"github.com/tammyapp/tammy/services/core/internal/transport"
)

const shutdownTimeout = 3 * time.Second

func run(stdin *os.File, stdout *os.File, stderr *os.File) int {
	server, err := transport.NewServer(
		buildinfo.Current(),
		stderr,
		transport.WithRandomSource(rand.Reader),
	)
	if err != nil {
		logLifecycleError(stderr, "server_construction_failed")
		return 1
	}
	if err := server.Start(); err != nil {
		logLifecycleError(stderr, "server_start_failed")
		shutdown(server)
		return 1
	}
	if err := transport.WriteReadiness(stdout, server.Ready()); err != nil {
		logLifecycleError(stderr, "readiness_write_failed")
		shutdown(server)
		return 1
	}

	parentEOF := make(chan struct{}, 1)
	go func() {
		_, _ = io.Copy(io.Discard, stdin)
		parentEOF <- struct{}{}
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)

	exitCode := 0
	serveEnded := false
	select {
	case <-parentEOF:
	case <-signals:
		_ = stdin.Close()
	case serveErr, ok := <-server.Errors():
		serveEnded = true
		_ = stdin.Close()
		if ok && serveErr != nil {
			logLifecycleError(stderr, "server_serve_failed")
		}
		exitCode = 1
	}

	if err := shutdown(server); err != nil {
		logLifecycleError(stderr, "server_shutdown_failed")
		exitCode = 1
	}
	if !serveEnded {
		select {
		case serveErr, ok := <-server.Errors():
			if ok && serveErr != nil {
				logLifecycleError(stderr, "server_serve_failed")
				exitCode = 1
			}
		case <-time.After(shutdownTimeout):
			logLifecycleError(stderr, "server_stop_timeout")
			exitCode = 1
		}
	}
	return exitCode
}

func main() { os.Exit(run(os.Stdin, os.Stdout, os.Stderr)) }

func shutdown(server *transport.Server) error {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return server.Shutdown(ctx)
}

func logLifecycleError(stderr io.Writer, event string) {
	if stderr == nil {
		return
	}
	_ = json.NewEncoder(stderr).Encode(map[string]string{
		"component": "tammy_core",
		"event":     event,
		"level":     "error",
	})
}
