package main

import (
	"context"
	"crypto/rand"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/tammyapp/tammy/services/sbr-helper/internal/evte"
	"github.com/tammyapp/tammy/services/sbr-helper/internal/protocol"
	"github.com/tammyapp/tammy/services/sbr-helper/internal/runner"
	"github.com/tammyapp/tammy/services/sbr-helper/internal/simulator"
)

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

type unavailableCredentialSigner struct{}

func (unavailableCredentialSigner) Execute(ctx context.Context, request protocol.Request) protocol.Response {
	if ctx.Err() != nil {
		return protocol.NewErrorResponse(request.RequestID, protocol.StableErrorHelperUnavailable)
	}
	return protocol.NewErrorResponse(request.RequestID, protocol.StableErrorSecureStoreUnavailable)
}

func main() {
	os.Exit(run())
}

func run() (exitCode int) {
	exitCode = 1
	defer func() {
		if recover() != nil {
			_, _ = io.WriteString(os.Stderr, "{\"code\":\"SBR_HELPER_UNAVAILABLE\"}\n")
			exitCode = 1
		}
	}()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	monitor, err := newParentLifetimeMonitor()
	if err != nil {
		_, _ = io.WriteString(os.Stderr, "{\"code\":\"SBR_HELPER_UNAVAILABLE\"}\n")
		return 1
	}
	defer monitor.Close()
	return runWith(os.Stdin, os.Stdout, os.Stderr, signals, monitor, runner.Dependencies{
		Clock:            systemClock{},
		RandomSource:     rand.Reader,
		Dialer:           simulator.DenyDialer{},
		CredentialSigner: unavailableCredentialSigner{},
		ComponentClient:  evte.Adapter{},
	})
}

type parentLifetimeMonitor interface {
	Wait(context.Context) error
	Close() error
}

func runWith(stdin io.ReadCloser, stdout, stderr io.Writer, signals <-chan os.Signal, monitor parentLifetimeMonitor, deps runner.Dependencies) int {
	ctx, cancel := context.WithCancel(context.Background())
	var cancelOnce sync.Once
	cancelAndClose := func() { cancelOnce.Do(func() { cancel(); _ = stdin.Close() }) }
	defer cancelAndClose()
	result := make(chan int, 1)
	go func() {
		result <- runner.RunOne(ctx, stdin, stdout, stderr, deps)
	}()
	parentExit := make(chan bool, 1)
	go func() {
		failed := false
		defer func() {
			if recover() != nil {
				failed = true
			}
			parentExit <- failed
		}()
		failed = monitor.Wait(ctx) != nil && ctx.Err() == nil
	}()
	select {
	case code := <-result:
		cancel()
		_ = monitor.Close()
		<-parentExit
		return code
	case <-signals:
		cancelAndClose()
		code := <-result
		_ = monitor.Close()
		<-parentExit
		return code
	case monitorFailed := <-parentExit:
		cancelAndClose()
		code := <-result
		if monitorFailed {
			if code == 0 {
				_, _ = io.WriteString(stderr, "{\"code\":\"SBR_HELPER_UNAVAILABLE\"}\n")
			}
			return 1
		}
		return code
	}
}
