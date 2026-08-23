// Package runner executes exactly one bounded helper request per process.
package runner

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"time"

	"github.com/tammyapp/tammy/services/sbr-helper/internal/protocol"
	"github.com/tammyapp/tammy/services/sbr-helper/internal/simulator"
)

type Clock interface {
	Now() time.Time
}

type RandomSource interface {
	Read([]byte) (int, error)
}

type Dialer interface {
	Dial(context.Context, string, string) error
}

type CredentialSigner interface {
	Execute(context.Context, protocol.Request) protocol.Response
}

type ComponentClient interface {
	Execute(context.Context, protocol.Request) protocol.Response
}

type cleanupObserver interface {
	RequestPayloadCleared([]byte)
	ResponsePayloadCleared([]byte)
}

type Dependencies struct {
	Clock            Clock
	RandomSource     RandomSource
	Dialer           Dialer
	CredentialSigner CredentialSigner
	ComponentClient  ComponentClient
	cleanupObserver  cleanupObserver
}

const (
	exitOK    = 0
	exitError = 1
)

func RunOne(ctx context.Context, input io.Reader, output, lifecycle io.Writer, deps Dependencies) (exitCode int) {
	exitCode = exitError
	defer func() {
		if recover() != nil {
			writeLifecycle(lifecycle, protocol.StableErrorHelperUnavailable)
			exitCode = exitError
		}
	}()
	if deps.Clock == nil || deps.RandomSource == nil || deps.Dialer == nil || deps.CredentialSigner == nil || deps.ComponentClient == nil {
		writeLifecycle(lifecycle, protocol.StableErrorHelperProtocol)
		return exitError
	}

	buffered := bufio.NewReader(input)
	if _, err := buffered.ReadByte(); err != nil {
		if ctx.Err() != nil {
			return exitOK
		}
		if err == io.EOF {
			return exitOK
		}
		writeLifecycle(lifecycle, protocol.StableErrorHelperProtocol)
		return exitError
	}
	if err := buffered.UnreadByte(); err != nil {
		writeLifecycle(lifecycle, protocol.StableErrorHelperProtocol)
		return exitError
	}
	payload, err := protocol.ReadFrame(buffered)
	if err != nil {
		if ctx.Err() != nil {
			return exitOK
		}
		writeLifecycle(lifecycle, protocol.StableErrorHelperProtocol)
		return exitError
	}
	now := deps.Clock.Now()
	request, err := protocol.DecodeRequest(payload, now)
	clear(payload)
	if deps.cleanupObserver != nil {
		deps.cleanupObserver.RequestPayloadCleared(payload)
	}
	if err != nil {
		writeLifecycle(lifecycle, protocol.StableErrorHelperProtocol)
		return exitError
	}
	defer request.ClearSecrets()

	session := &protocol.Session{}
	if err := session.Begin(request, now); err != nil {
		writeLifecycle(lifecycle, protocol.StableErrorHelperProtocol)
		return exitError
	}
	timeRemaining := time.Duration(request.DeadlineMillis-now.UnixMilli()) * time.Millisecond
	operationContext, cancel := context.WithTimeout(ctx, timeRemaining)
	defer cancel()
	if operationContext.Err() != nil {
		writeLifecycle(lifecycle, protocol.StableErrorHelperUnavailable)
		return exitError
	}
	result := make(chan executionResult, 1)
	operationRequest := cloneRequest(request)
	go func() {
		var execution executionResult
		func() {
			defer func() {
				if recover() != nil {
					execution = executionResult{fatal: true}
				}
			}()
			execution = execute(operationContext, operationRequest, deps)
		}()
		operationRequest.ClearSecrets()
		result <- execution
	}()
	var execution executionResult
	select {
	case <-operationContext.Done():
		if ctx.Err() != nil {
			writeLifecycle(lifecycle, protocol.StableErrorHelperUnavailable)
			return exitError
		}
		execution = executionResult{response: protocol.NewErrorResponse(request.RequestID, protocol.StableErrorDeadlineExpired), timedOut: true}
	case execution = <-result:
		if ctx.Err() != nil {
			writeLifecycle(lifecycle, protocol.StableErrorHelperUnavailable)
			return exitError
		}
		if !deps.Clock.Now().Before(time.UnixMilli(request.DeadlineMillis)) {
			execution = executionResult{response: protocol.NewErrorResponse(request.RequestID, protocol.StableErrorDeadlineExpired), timedOut: true}
		}
	}
	if execution.fatal {
		writeLifecycle(lifecycle, protocol.StableErrorHelperUnavailable)
		return exitError
	}
	if execution.malformedPayload != nil {
		err = protocol.WriteFrame(output, execution.malformedPayload)
		clear(execution.malformedPayload)
		if deps.cleanupObserver != nil {
			deps.cleanupObserver.ResponsePayloadCleared(execution.malformedPayload)
		}
		if err != nil {
			writeLifecycle(lifecycle, protocol.StableErrorHelperUnavailable)
			return exitError
		}
		writeLifecycle(lifecycle, protocol.StableErrorHelperProtocol)
		return exitError
	}
	response := execution.response
	if execution.timedOut {
		response = protocol.NewErrorResponse(response.RequestID, protocol.StableErrorDeadlineExpired)
	}
	bindAuthenticatedEnvelope(&response, request)
	if !execution.timedOut {
		if err := session.Complete(response, deps.Clock.Now()); err != nil {
			writeLifecycle(lifecycle, protocol.StableErrorHelperProtocol)
			return exitError
		}
	}
	encoded, err := protocol.EncodeResponse(response)
	if err != nil {
		writeLifecycle(lifecycle, protocol.StableErrorHelperProtocol)
		return exitError
	}
	err = protocol.WriteFrame(output, encoded)
	clear(encoded)
	if deps.cleanupObserver != nil {
		deps.cleanupObserver.ResponsePayloadCleared(encoded)
	}
	if err != nil {
		writeLifecycle(lifecycle, protocol.StableErrorHelperUnavailable)
		return exitError
	}
	return exitOK
}

func cloneRequest(request protocol.Request) protocol.Request {
	request.OpaqueScope = bytes.Clone(request.OpaqueScope)
	request.Bookmark = bytes.Clone(request.Bookmark)
	request.TransientPassword = bytes.Clone(request.TransientPassword)
	request.TransientProductID = bytes.Clone(request.TransientProductID)
	request.EndpointProfile = bytes.Clone(request.EndpointProfile)
	request.ProfileFingerprint = bytes.Clone(request.ProfileFingerprint)
	request.RegistrationFingerprint = bytes.Clone(request.RegistrationFingerprint)
	request.ComponentFingerprint = bytes.Clone(request.ComponentFingerprint)
	return request
}

func bindAuthenticatedEnvelope(response *protocol.Response, request protocol.Request) {
	if response == nil {
		return
	}
	response.ProfileFingerprint = bytes.Clone(request.ProfileFingerprint)
	response.RegistrationFingerprint = bytes.Clone(request.RegistrationFingerprint)
	response.ComponentFingerprint = bytes.Clone(request.ComponentFingerprint)
	response.ComponentVersion = request.ComponentVersion
}

type executionResult struct {
	response         protocol.Response
	malformedPayload []byte
	fatal            bool
	timedOut         bool
}

func execute(ctx context.Context, request protocol.Request, deps Dependencies) executionResult {
	if request.Operation == protocol.OperationFixture {
		adapter, err := simulator.NewAdapter(deps.Dialer)
		if err != nil {
			return executionResult{fatal: true}
		}
		selection, err := adapter.Select(ctx, request.RequestID, request.SimulatorCase)
		if err != nil {
			return executionResult{response: protocol.NewErrorResponse(request.RequestID, protocol.StableErrorHelperProtocol)}
		}
		if selection.Fatal {
			return executionResult{response: protocol.Response{RequestID: request.RequestID}, fatal: true}
		}
		if selection.SemanticOutcome == simulator.SemanticTimeout {
			<-ctx.Done()
			return executionResult{response: protocol.NewErrorResponse(request.RequestID, protocol.StableErrorDeadlineExpired), timedOut: true}
		}
		return executionResult{response: selection.Response, malformedPayload: selection.MalformedPayload}
	}
	if request.Environment == protocol.EnvironmentEVTE {
		return executionResult{response: deps.ComponentClient.Execute(ctx, request)}
	}
	return executionResult{response: deps.CredentialSigner.Execute(ctx, request)}
}

func writeLifecycle(writer io.Writer, code protocol.StableErrorCode) {
	if code != protocol.StableErrorHelperProtocol && code != protocol.StableErrorHelperUnavailable {
		code = protocol.StableErrorHelperProtocol
	}
	_, _ = io.WriteString(writer, "{\"code\":\""+string(code)+"\"}\n")
}

func clear(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
