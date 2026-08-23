//go:build darwin && arm64 && cgo

package sbrhelper

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/sbrprofile"
)

type launchFreshnessError struct{ err error }

var (
	errProcessExited        = errors.New("sbr helper process exited")
	errProcessOutputInvalid = errors.New("sbr helper process output invalid")
)

func (e launchFreshnessError) Error() string { return e.err.Error() }
func (e launchFreshnessError) Unwrap() error { return e.err }

func (l *Launcher) launch(ctx context.Context, profilePath string, request Request) (Response, error) {
	now := l.now()
	processContext, cancel := context.WithDeadline(ctx, time.UnixMilli(request.DeadlineMillis))
	defer cancel()
	staged, err := sbrprofile.AuthenticateAndStage(processContext, profilePath, l.locator, now)
	if err != nil {
		if processContext.Err() != nil {
			return Response{}, protocolError(string(StableErrorDeadlineExpired))
		}
		return Response{}, err
	}
	defer staged.Close()
	return l.launchStaged(processContext, staged, request)
}

func (l *Launcher) launchStaged(ctx context.Context, staged *sbrprofile.StagedResources, request Request) (Response, error) {
	now := l.now()
	processContext, cancel := context.WithDeadline(ctx, time.UnixMilli(request.DeadlineMillis))
	defer cancel()
	if err := staged.ValidateFresh(now); err != nil {
		return Response{}, err
	}
	if request.EndpointProfile != nil {
		return Response{}, protocolError("REQUEST_INVALID")
	}
	validationRequest := request
	if validationRequest.Environment == EnvironmentEVTE {
		validationRequest.EndpointProfile = []byte{1}
	}
	validationPayload, err := EncodeRequest(validationRequest, now)
	if err != nil {
		return Response{}, err
	}
	zeroBytes(validationPayload)
	if staged.Profile.Profile.Environment == "SIMULATOR" {
		if request.Environment != EnvironmentSimulator {
			return Response{}, protocolError("REQUEST_INVALID")
		}
		request.EndpointProfile = nil
	} else {
		if request.Environment != EnvironmentEVTE {
			return Response{}, protocolError("REQUEST_INVALID")
		}
		request.EndpointProfile = append([]byte(nil), staged.EndpointProfile...)
	}
	var session Session
	if err = session.Begin(request, now); err != nil {
		return Response{}, err
	}
	payload, err := EncodeRequest(request, now)
	if err != nil {
		return Response{}, err
	}
	defer zeroBytes(payload)
	var framed bytes.Buffer
	if err = WriteFrame(&framed, payload); err != nil {
		return Response{}, err
	}
	defer zeroBytes(framed.Bytes())
	sandbox, guard, err := RenderDevelopmentSandboxProfileContext(processContext, SandboxProfileInput{TrustedBase: filepath.Dir(staged.RuntimeRoot), StagedRoot: staged.RuntimeRoot, StagedExecutables: []string{staged.HelperPath}, StagedReadOnlyFiles: staged.ReadOnlyPaths})
	if err != nil {
		if processContext.Err() != nil {
			return Response{}, protocolError(string(StableErrorDeadlineExpired))
		}
		return Response{}, protocolError(string(StableErrorHelperSandboxUnavailable))
	}
	defer guard.Close()
	authority := func() error {
		if contextErr := processContext.Err(); contextErr != nil {
			return contextErr
		}
		if guardErr := guard.RevalidateContext(processContext); guardErr != nil {
			return guardErr
		}
		return staged.RevalidateContext(processContext)
	}
	if err = authority(); err != nil {
		if processContext.Err() != nil {
			return Response{}, protocolError(string(StableErrorDeadlineExpired))
		}
		return Response{}, protocolError(string(StableErrorHelperUntrusted))
	}
	if err = staged.ValidateFresh(l.now()); err != nil {
		if processContext.Err() != nil {
			return Response{}, protocolError(string(StableErrorDeadlineExpired))
		}
		return Response{}, err
	}
	if l.capture == nil || l.verify == nil {
		return Response{}, protocolError(string(StableErrorHelperUntrusted))
	}
	expectedIdentity, err := l.capture(processContext, staged)
	if err != nil {
		if processContext.Err() != nil {
			return Response{}, protocolError(string(StableErrorDeadlineExpired))
		}
		return Response{}, protocolError(string(StableErrorHelperUntrusted))
	}
	contents, err := sandbox.PrepareSpawnContext(processContext)
	if err != nil {
		if processContext.Err() != nil {
			return Response{}, protocolError(string(StableErrorDeadlineExpired))
		}
		return Response{}, protocolError(string(StableErrorHelperSandboxUnavailable))
	}
	helperFile, err := staged.OpenHelperExecutableContext(processContext)
	if err != nil {
		if processContext.Err() != nil {
			return Response{}, protocolError(string(StableErrorDeadlineExpired))
		}
		return Response{}, protocolError(string(StableErrorHelperUntrusted))
	}
	defer helperFile.Close()
	profileFile, err := staged.CreatePrivateRuntimeFileContext(processContext, "sandbox.sb", []byte(contents))
	if err != nil {
		if processContext.Err() != nil {
			return Response{}, protocolError(string(StableErrorDeadlineExpired))
		}
		return Response{}, protocolError(string(StableErrorHelperSandboxUnavailable))
	}
	defer profileFile.Close()
	framedPayload := append([]byte(nil), framed.Bytes()...)
	defer zeroBytes(framedPayload)
	// Darwin rejects execve through /dev/fd even for a retained O_EXEC descriptor.
	// Guard the fixed private pathname and retained descriptors on both sides of
	// Start, then require the live process CDHash before sending the request.
	verifyChild := func(verifyCtx context.Context, pid int, initial bool) error {
		if initial {
			return l.verify(verifyCtx, pid, staged, expectedIdentity, true)
		}
		return verifyPreStdinAuthority(
			verifyCtx,
			authority,
			func(ctx context.Context, processID int) error {
				return l.verify(ctx, processID, staged, expectedIdentity, false)
			},
			pid,
			func() error {
				if freshErr := staged.ValidateFresh(l.now()); freshErr != nil {
					return launchFreshnessError{err: freshErr}
				}
				return nil
			},
		)
	}
	output, err := l.run(processContext, "/usr/bin/sandbox-exec", []string{"-f", "/dev/fd/4", staged.HelperPath}, framedPayload, []*os.File{helperFile, profileFile}, authority, verifyChild)
	defer zeroBytes(output)
	if err != nil {
		if processContext.Err() != nil {
			return Response{}, protocolError(string(StableErrorDeadlineExpired))
		}
		var freshness launchFreshnessError
		if errors.As(err, &freshness) {
			return Response{}, freshness.err
		}
		if errors.Is(err, errProcessOutputInvalid) {
			return Response{}, errMalformedHelperResponse
		}
		if errors.Is(err, errProcessExited) && len(output) > 0 {
			if _, decodeErr := decodeAuthenticatedResponse(output, &session, l.now()); decodeErr != nil {
				return Response{}, errMalformedHelperResponse
			}
		}
		return Response{}, protocolError(string(StableErrorHelperUnavailable))
	}
	return decodeAuthenticatedResponse(output, &session, l.now())
}

func decodeAuthenticatedResponse(output []byte, session *Session, now time.Time) (Response, error) {
	responsePayload, err := ReadFrame(bytes.NewReader(output))
	if err != nil {
		return Response{}, errMalformedHelperResponse
	}
	defer zeroBytes(responsePayload)
	response, err := DecodeResponse(responsePayload)
	if err != nil || session == nil || session.Complete(response, now) != nil {
		return Response{}, errMalformedHelperResponse
	}
	return response, nil
}

func verifyPreStdinAuthority(ctx context.Context, authority func() error, verify func(context.Context, int) error, pid int, validateFresh func() error) error {
	if ctx == nil || authority == nil || verify == nil || validateFresh == nil {
		return errors.New("pre-stdin authority")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := authority(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := verify(ctx, pid); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateFresh(); err != nil {
		return err
	}
	return ctx.Err()
}

func runSandboxedProcess(ctx context.Context, path string, args []string, input []byte, extraFiles []*os.File, validate func() error, verify childVerifier) ([]byte, error) {
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > 5*time.Minute {
		return nil, errors.New("deadline")
	}
	if len(extraFiles) != 2 {
		return nil, errors.New("extra files")
	}
	profileReader, profileWriter, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer profileReader.Close()
	defer profileWriter.Close()
	command := exec.CommandContext(ctx, path, args...)
	command.Env = []string{"PATH=/usr/bin:/bin"}
	command.ExtraFiles = []*os.File{extraFiles[0], profileReader}
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	command.Stderr = io.Discard
	if validate == nil || verify == nil || validate() != nil {
		return nil, errors.New("authority")
	}
	if err = command.Start(); err != nil {
		return nil, err
	}
	if validate() != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, errors.New("authority changed")
	}
	_ = profileReader.Close()
	profileErr := make(chan error, 1)
	go func() {
		copyErr := copyRetainedProfileContext(ctx, extraFiles[1], profileWriter)
		closeErr := profileWriter.Close()
		if copyErr != nil {
			profileErr <- copyErr
		} else {
			profileErr <- closeErr
		}
	}()
	if verifyErr := verify(ctx, command.Process.Pid, true); verifyErr != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		_ = profileWriter.Close()
		<-profileErr
		return nil, verifyErr
	}
	if verifyErr := verify(ctx, command.Process.Pid, false); verifyErr != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		_ = profileWriter.Close()
		<-profileErr
		return nil, verifyErr
	}
	if err := ctx.Err(); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		_ = profileWriter.Close()
		<-profileErr
		return nil, err
	}
	writeErr := make(chan error, 1)
	go func() { defer stdin.Close(); writeErr <- writeAll(stdin, input) }()
	limited := io.LimitReader(stdout, MaxPayloadSize+5)
	output, readErr := io.ReadAll(limited)
	waitErr := command.Wait()
	profileWriteErr := <-profileErr
	if err := <-writeErr; err != nil {
		return nil, err
	}
	if profileWriteErr != nil || readErr != nil {
		return nil, errors.New("process")
	}
	if len(output) > MaxPayloadSize+4 {
		return output, errProcessOutputInvalid
	}
	if waitErr != nil {
		return output, errProcessExited
	}
	return output, nil
}

func copyRetainedProfileContext(ctx context.Context, source *os.File, destination io.Writer) error {
	if ctx == nil || source == nil || destination == nil {
		return errors.New("profile copy")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := source.Seek(0, 0); err != nil {
		return err
	}
	buffer := make([]byte, 32<<10)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		read, readErr := source.Read(buffer)
		for offset := 0; offset < read; {
			if err := ctx.Err(); err != nil {
				return err
			}
			written, writeErr := destination.Write(buffer[offset:read])
			if writeErr != nil || written <= 0 || written > read-offset {
				if writeErr != nil {
					return writeErr
				}
				return errors.New("profile write")
			}
			offset += written
		}
		if errors.Is(readErr, io.EOF) {
			return ctx.Err()
		}
		if readErr != nil {
			return readErr
		}
	}
}

func ValidateProfilePath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return protocolError("SBR_PROFILE_INVALID")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return protocolError("SBR_PROFILE_INVALID")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != path {
		return protocolError("SBR_PROFILE_INVALID")
	}
	return nil
}
