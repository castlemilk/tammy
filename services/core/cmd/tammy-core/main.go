package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/buildinfo"
	"github.com/tammyapp/tammy/services/core/internal/transport"
)

const shutdownTimeout = 3 * time.Second

var errProcessConfig = errors.New("tammy-core: invalid process configuration")

func run(stdin *os.File, stdout *os.File, stderr *os.File) int {
	return runWithArgs(stdin, stdout, stderr, nil)
}

func runWithArgs(stdin *os.File, stdout *os.File, stderr *os.File, args []string) int {
	config, err := configuredProcess(args)
	if err != nil {
		logLifecycleError(stderr, "configuration_failed")
		return 1
	}
	composition, err := newConfiguredComposition(buildinfo.Current(), config)
	if err != nil {
		if config.developmentMemoryAnchors {
			logDevelopmentCompositionError(stderr, err)
		} else {
			logLifecycleError(stderr, "composition_failed")
		}
		return 1
	}
	defer func() { _ = composition.Close() }()
	server, err := transport.NewServer(
		composition.Registrar(),
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

func main() {
	if handled, exitCode := reportSQLCipher(os.Args[1:], os.Stdout, os.Stderr); handled {
		os.Exit(exitCode)
	}
	os.Exit(runWithArgs(os.Stdin, os.Stdout, os.Stderr, os.Args[1:]))
}

type processConfig struct {
	dataRoot                 string
	developmentMemoryAnchors bool
}

func configuredProcess(args []string) (processConfig, error) {
	if len(args) == 0 {
		return processConfig{}, nil
	}
	if (len(args) != 2 && len(args) != 3) || args[0] != "--data-root" ||
		!filepath.IsAbs(args[1]) || filepath.Clean(args[1]) != args[1] {
		return processConfig{}, errProcessConfig
	}
	config := processConfig{dataRoot: args[1]}
	if len(args) == 3 {
		if args[2] != "--development-memory-anchors" {
			return processConfig{}, errProcessConfig
		}
		config.developmentMemoryAnchors = true
	}
	return config, nil
}

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

func logDevelopmentCompositionError(stderr io.Writer, err error) {
	if stderr == nil || err == nil {
		return
	}
	_ = json.NewEncoder(stderr).Encode(map[string]string{
		"component": "tammy_core",
		"detail":    err.Error(),
		"event":     "development_composition_failed",
		"level":     "error",
	})
}
