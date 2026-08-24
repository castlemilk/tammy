//go:build darwin && arm64 && cgo

package sbrhelper

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/sbrprofile"
	"golang.org/x/sys/unix"
)

type launcherLocator struct{ resources sbrprofile.ResourceSet }

func scopedLauncherFixture(request Request) Request {
	request.WorkspaceID = "018bcfe5-6800-7000-8000-000000000003"
	request.OrganisationID = "018bcfe5-6800-7000-8000-000000000004"
	request.CanonicalABN = "51824753556"
	request.OpaqueScope = bytes.Repeat([]byte{0x5a}, 32)
	request.ProfileFingerprint = bytes.Repeat([]byte{0x61}, 32)
	request.RegistrationFingerprint = bytes.Repeat([]byte{0x62}, 32)
	request.ComponentFingerprint = bytes.Repeat([]byte{0x63}, 32)
	request.ComponentVersion = "simulator-v1"
	return request
}

func (l launcherLocator) Locate(sbrprofile.Profile) (sbrprofile.ResourceSet, error) {
	return l.resources, nil
}

type delayedLauncherLocator struct {
	resources sbrprofile.ResourceSet
	delay     time.Duration
}

func installFakeCodeIdentity(launcher *Launcher) {
	identity := codeIdentity{cdHash: []byte{1}}
	launcher.capture = func(context.Context, *sbrprofile.StagedResources) (codeIdentity, error) { return identity, nil }
	launcher.verify = func(context.Context, int, *sbrprofile.StagedResources, codeIdentity, bool) error { return nil }
}

func (l delayedLauncherLocator) Locate(sbrprofile.Profile) (sbrprofile.ResourceSet, error) {
	time.Sleep(l.delay)
	return l.resources, nil
}

func TestLauncherSandboxInputPinsOnlyTheSelectedCredentialPath(t *testing.T) {
	staged := &sbrprofile.StagedResources{
		RuntimeRoot:   "/private/var/folders/runtime/tammy-sbr-runtime-0123456789abcdef01234567",
		HelperPath:    "/private/var/folders/runtime/tammy-sbr-runtime-0123456789abcdef01234567/sbr-helper",
		ReadOnlyPaths: []string{"/private/var/folders/runtime/tammy-sbr-runtime-0123456789abcdef01234567/component.bin"},
	}
	selected := "/private/tmp/synthetic-machine-credential.p12"
	input := sandboxProfileInputForLaunch(staged, Request{SelectedLocalPath: selected})
	if input.TrustedBase != filepath.Dir(staged.RuntimeRoot) || input.StagedRoot != staged.RuntimeRoot ||
		len(input.StagedExecutables) != 1 || input.StagedExecutables[0] != staged.HelperPath ||
		len(input.StagedReadOnlyFiles) != 1 || input.StagedReadOnlyFiles[0] != staged.ReadOnlyPaths[0] ||
		len(input.SelectedReadFiles) != 1 || input.SelectedReadFiles[0] != selected {
		t.Fatalf("sandbox input = %+v", input)
	}
	withoutSelection := sandboxProfileInputForLaunch(staged, Request{})
	if len(withoutSelection.SelectedReadFiles) != 0 {
		t.Fatalf("empty request selected paths = %q", withoutSelection.SelectedReadFiles)
	}
}

func TestLauncherExecutesOnlyStagedHelperWithSandboxProfileAndFramedStdio(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	root, err := os.MkdirTemp(launcherRepositoryRoot(t), ".sbrhelper-launcher-test-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	helperPath := filepath.Join(root, "source-helper")
	helper := []byte("fake helper")
	writeLauncherFile(t, helperPath, helper, 0o500)
	runtimeBase := filepath.Join(root, "runtime")
	if err := os.Mkdir(runtimeBase, 0o700); err != nil {
		t.Fatal(err)
	}
	profilePath := writeLauncherProfile(t, root, helper, now)
	launcher := NewLauncher(launcherLocator{sbrprofile.ResourceSet{HelperPath: helperPath, TrustedRuntimeBase: runtimeBase}})
	launcher.now = func() time.Time { return now }
	identity := codeIdentity{cdHash: []byte{1}}
	launcher.capture = func(context.Context, *sbrprofile.StagedResources) (codeIdentity, error) { return identity, nil }
	request := scopedLauncherFixture(Request{ProtocolVersion: 1, RequestID: "018bcfe5-6800-7000-8000-000000000001", Operation: OperationStatus, DeadlineMillis: now.Add(time.Minute).UnixMilli(), Environment: EnvironmentSimulator})
	var capturedInput []byte
	verified := 0
	launcher.verify = func(_ context.Context, _ int, _ *sbrprofile.StagedResources, got codeIdentity, _ bool) error {
		if !sameCodeIdentity(identity, got) {
			t.Fatal("captured identity changed")
		}
		verified++
		return nil
	}
	launcher.run = func(runCtx context.Context, path string, args []string, input []byte, extraFiles []*os.File, validate func() error, verify childVerifier) ([]byte, error) {
		capturedInput = input
		if path != "/usr/bin/sandbox-exec" || len(args) != 3 || args[0] != "-f" || args[1] != "/dev/fd/4" || args[2] == helperPath || !strings.Contains(args[2], "tammy-sbr-runtime-") || len(extraFiles) != 2 {
			t.Fatalf("unsafe argv: %q %q", path, args)
		}
		if strings.Contains(strings.Join(args, " "), "51824753556") {
			t.Fatal("scope leaked to argv")
		}
		if err := validate(); err != nil {
			t.Fatalf("pre-start authority: %v", err)
		}
		if err := verify(runCtx, 123, true); err != nil {
			t.Fatalf("process verifier: %v", err)
		}
		if err := verify(runCtx, 123, false); err != nil {
			t.Fatalf("pre-stdin process verifier: %v", err)
		}
		helperInfo, err := extraFiles[0].Stat()
		if err != nil || !helperInfo.Mode().IsRegular() || helperInfo.Mode().Perm() != 0o500 {
			t.Fatalf("retained executable descriptor: %v %v", helperInfo, err)
		}
		helperBytes := make([]byte, len(helper))
		if _, err = extraFiles[0].ReadAt(helperBytes, 0); err != nil || !bytes.Equal(helperBytes, helper) {
			t.Fatalf("retained helper bytes=%q error=%v", helperBytes, err)
		}
		profileBytes, err := io.ReadAll(extraFiles[1])
		if err != nil || !bytes.Contains(profileBytes, []byte(`(allow process-exec (literal "`+args[2]+`"))`)) {
			t.Fatalf("profile descriptor unavailable: %v %q", err, profileBytes)
		}
		if err := validate(); err != nil {
			t.Fatalf("post-start authority: %v", err)
		}
		payload, err := ReadFrame(bytes.NewReader(input))
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := DecodeRequest(payload, now)
		if err != nil || decoded.EndpointProfile != nil {
			t.Fatalf("request=%+v err=%v", decoded, err)
		}
		response, err := EncodeResponse(Response{RequestID: decoded.RequestID, Outcome: OutcomeOK, RedactedResult: ResultReady})
		if err != nil {
			t.Fatal(err)
		}
		var framed bytes.Buffer
		if err = WriteFrame(&framed, response); err != nil {
			t.Fatal(err)
		}
		return framed.Bytes(), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	response, err := launcher.Launch(ctx, profilePath, request)
	if err != nil {
		t.Fatal(err)
	}
	if response.RedactedResult != ResultReady || verified != 2 {
		t.Fatalf("response=%+v", response)
	}
	if !allZero(capturedInput) || !allZero(request.OpaqueScope) {
		t.Fatal("launcher retained request secrets or framed payload")
	}
}

func TestLauncherClassifiesMalformedFrameFromExitedAuthenticatedHelper(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	root, err := os.MkdirTemp(launcherRepositoryRoot(t), ".sbrhelper-malformed-test-")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	helper := []byte("authenticated helper")
	helperPath := filepath.Join(root, "helper")
	writeLauncherFile(t, helperPath, helper, 0o500)
	runtimeBase := filepath.Join(root, "runtime")
	if err = os.Mkdir(runtimeBase, 0o700); err != nil {
		t.Fatal(err)
	}
	profilePath := writeLauncherProfile(t, root, helper, now)
	launcher := NewLauncher(launcherLocator{sbrprofile.ResourceSet{HelperPath: helperPath, TrustedRuntimeBase: runtimeBase}})
	launcher.now = func() time.Time { return now }
	installFakeCodeIdentity(launcher)
	launcher.run = func(context.Context, string, []string, []byte, []*os.File, func() error, childVerifier) ([]byte, error) {
		var framed bytes.Buffer
		if err := WriteFrame(&framed, []byte{0x0a, 0x01, 'x'}); err != nil {
			t.Fatal(err)
		}
		return framed.Bytes(), errProcessExited
	}
	request := scopedLauncherFixture(Request{ProtocolVersion: ProtocolVersion,
		RequestID: "018bcfe5-6800-7000-8000-000000000001", Operation: OperationFixture,
		DeadlineMillis: now.Add(time.Minute).UnixMilli(), Environment: EnvironmentSimulator,
		SimulatorCase: SimulatorMalformedResponse})
	if _, err = launcher.Launch(context.Background(), profilePath, request); !errors.Is(err, errMalformedHelperResponse) ||
		err.Error() != "sbr helper malformed response" {
		t.Fatalf("malformed launcher error = %v", err)
	}
}

func TestLauncherRejectsHelperSwapAcrossSpawnBoundary(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	root, err := os.MkdirTemp(launcherRepositoryRoot(t), ".sbrhelper-swap-test-")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	helper := []byte("trusted helper")
	helperPath := filepath.Join(root, "source-helper")
	writeLauncherFile(t, helperPath, helper, 0o500)
	runtimeBase := filepath.Join(root, "runtime")
	if err = os.Mkdir(runtimeBase, 0o700); err != nil {
		t.Fatal(err)
	}
	profilePath := writeLauncherProfile(t, root, helper, now)
	launcher := NewLauncher(launcherLocator{sbrprofile.ResourceSet{HelperPath: helperPath, TrustedRuntimeBase: runtimeBase}})
	launcher.now = func() time.Time { return now }
	installFakeCodeIdentity(launcher)
	launcher.run = func(_ context.Context, _ string, args []string, _ []byte, _ []*os.File, validate func() error, _ childVerifier) ([]byte, error) {
		if err := validate(); err != nil {
			t.Fatalf("pre-start authority: %v", err)
		}
		renamed := args[2] + ".trusted"
		if err := os.Rename(args[2], renamed); err != nil {
			t.Fatal(err)
		}
		writeLauncherFile(t, args[2], []byte("malicious helper"), 0o500)
		if err := validate(); err == nil {
			t.Fatal("post-start helper swap retained authority")
		}
		return nil, errors.New("authority changed")
	}
	request := scopedLauncherFixture(Request{ProtocolVersion: 1, RequestID: "018bcfe5-6800-7000-8000-000000000001", Operation: OperationFixture, DeadlineMillis: now.Add(time.Minute).UnixMilli(), Environment: EnvironmentSimulator, SimulatorCase: SimulatorAccepted})
	if _, err = launcher.Launch(context.Background(), profilePath, request); err == nil || err.Error() != string(StableErrorHelperUnavailable) {
		t.Fatalf("error=%v", err)
	}
}

func TestLauncherRejectsRuntimeBaseAncestorSwapAcrossSpawnBoundary(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	root, err := os.MkdirTemp(launcherRepositoryRoot(t), ".sbrhelper-base-swap-test-")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	helper := []byte("trusted helper")
	helperPath := filepath.Join(root, "source-helper")
	writeLauncherFile(t, helperPath, helper, 0o500)
	runtimeBase := filepath.Join(root, "runtime")
	if err = os.Mkdir(runtimeBase, 0o700); err != nil {
		t.Fatal(err)
	}
	profilePath := writeLauncherProfile(t, root, helper, now)
	launcher := NewLauncher(launcherLocator{sbrprofile.ResourceSet{HelperPath: helperPath, TrustedRuntimeBase: runtimeBase}})
	launcher.now = func() time.Time { return now }
	installFakeCodeIdentity(launcher)
	launcher.run = func(_ context.Context, _ string, _ []string, _ []byte, _ []*os.File, validate func() error, _ childVerifier) ([]byte, error) {
		if err := validate(); err != nil {
			t.Fatalf("pre-start authority: %v", err)
		}
		if err := os.Rename(runtimeBase, runtimeBase+".trusted"); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(runtimeBase, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := validate(); err == nil {
			t.Fatal("post-start runtime-base swap retained authority")
		}
		return nil, errors.New("authority changed")
	}
	request := scopedLauncherFixture(Request{ProtocolVersion: 1, RequestID: "018bcfe5-6800-7000-8000-000000000001", Operation: OperationFixture, DeadlineMillis: now.Add(time.Minute).UnixMilli(), Environment: EnvironmentSimulator, SimulatorCase: SimulatorAccepted})
	if _, err = launcher.Launch(context.Background(), profilePath, request); err == nil || err.Error() != string(StableErrorHelperUnavailable) {
		t.Fatalf("error=%v", err)
	}
}

func TestLauncherClearsCallerSecretBackingsBeforeInvalidRequestReturns(t *testing.T) {
	scope := bytes.Repeat([]byte{0x5a}, 32)
	password := []byte("secret")
	product := []byte("product")
	launcher := NewLauncher(launcherLocator{})
	request := Request{ProtocolVersion: 99, OpaqueScope: scope, TransientPassword: password, TransientProductID: product}
	if _, err := launcher.Launch(context.Background(), "invalid", request); err == nil {
		t.Fatal("invalid request accepted")
	}
	if !allZero(scope) || !allZero(password) || !allZero(product) {
		t.Fatal("invalid request retained secrets")
	}
}

func TestLauncherClearsCallerSecretBackingsBeforeNilContextValidation(t *testing.T) {
	scope := bytes.Repeat([]byte{0x5a}, 32)
	launcher := NewLauncher(launcherLocator{})
	if _, err := launcher.Launch(nil, "invalid", Request{OpaqueScope: scope}); err == nil {
		t.Fatal("nil context accepted")
	}
	if !allZero(scope) {
		t.Fatal("entry validation retained secrets")
	}
}

func TestLauncherDeadlineCoversAuthenticationDelay(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	root, err := os.MkdirTemp(launcherRepositoryRoot(t), ".sbrhelper-deadline-test-")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	helper := []byte("helper")
	helperPath := filepath.Join(root, "helper")
	writeLauncherFile(t, helperPath, helper, 0o500)
	base := filepath.Join(root, "runtime")
	if err = os.Mkdir(base, 0o700); err != nil {
		t.Fatal(err)
	}
	profilePath := writeLauncherProfile(t, root, helper, now)
	launcher := NewLauncher(delayedLauncherLocator{resources: sbrprofile.ResourceSet{HelperPath: helperPath, TrustedRuntimeBase: base}, delay: 50 * time.Millisecond})
	launcher.now = func() time.Time { return now }
	request := scopedLauncherFixture(Request{ProtocolVersion: 1, RequestID: "018bcfe5-6800-7000-8000-000000000001", Operation: OperationFixture, DeadlineMillis: now.Add(10 * time.Millisecond).UnixMilli(), Environment: EnvironmentSimulator, SimulatorCase: SimulatorAccepted})
	_, err = launcher.Launch(context.Background(), profilePath, request)
	if err == nil || err.Error() != string(StableErrorDeadlineExpired) {
		t.Fatalf("error=%v", err)
	}
	entries, readErr := os.ReadDir(base)
	if readErr != nil || len(entries) != 0 {
		t.Fatalf("deadline staged resources: %v %v", entries, readErr)
	}
}

func TestLauncherMapsChildDeadlineToStableDeadlineError(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	root, err := os.MkdirTemp(launcherRepositoryRoot(t), ".sbrhelper-child-deadline-test-")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	helper := []byte("helper")
	helperPath := filepath.Join(root, "helper")
	writeLauncherFile(t, helperPath, helper, 0o500)
	base := filepath.Join(root, "runtime")
	if err = os.Mkdir(base, 0o700); err != nil {
		t.Fatal(err)
	}
	profilePath := writeLauncherProfile(t, root, helper, now)
	launcher := NewLauncher(launcherLocator{sbrprofile.ResourceSet{HelperPath: helperPath, TrustedRuntimeBase: base}})
	launcher.now = func() time.Time { return now }
	installFakeCodeIdentity(launcher)
	launcher.run = func(ctx context.Context, _ string, _ []string, _ []byte, _ []*os.File, _ func() error, _ childVerifier) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	request := scopedLauncherFixture(Request{ProtocolVersion: 1, RequestID: "018bcfe5-6800-7000-8000-000000000001", Operation: OperationFixture, DeadlineMillis: now.Add(20 * time.Millisecond).UnixMilli(), Environment: EnvironmentSimulator, SimulatorCase: SimulatorAccepted})
	_, err = launcher.Launch(context.Background(), profilePath, request)
	if err == nil || err.Error() != string(StableErrorDeadlineExpired) {
		t.Fatalf("error=%v", err)
	}
}

func TestLauncherMapsParentCancellationToStableDeadlineWithoutStaging(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	root, err := os.MkdirTemp(launcherRepositoryRoot(t), ".sbrhelper-cancel-test-")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	helper := []byte("helper")
	helperPath := filepath.Join(root, "helper")
	writeLauncherFile(t, helperPath, helper, 0o500)
	base := filepath.Join(root, "runtime")
	if err = os.Mkdir(base, 0o700); err != nil {
		t.Fatal(err)
	}
	profilePath := writeLauncherProfile(t, root, helper, now)
	launcher := NewLauncher(launcherLocator{sbrprofile.ResourceSet{HelperPath: helperPath, TrustedRuntimeBase: base}})
	launcher.now = func() time.Time { return now }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := scopedLauncherFixture(Request{ProtocolVersion: 1, RequestID: "018bcfe5-6800-7000-8000-000000000001", Operation: OperationFixture, DeadlineMillis: now.Add(time.Minute).UnixMilli(), Environment: EnvironmentSimulator, SimulatorCase: SimulatorAccepted})
	_, err = launcher.Launch(ctx, profilePath, request)
	if err == nil || err.Error() != string(StableErrorDeadlineExpired) {
		t.Fatalf("error=%v", err)
	}
	entries, readErr := os.ReadDir(base)
	if readErr != nil || len(entries) != 0 {
		t.Fatalf("canceled launch leaked runtime root: %v %v", entries, readErr)
	}
}

func TestRunSandboxedProcessRetainsProfileForDelayedChildRead(t *testing.T) {
	const childRun = "-test.run=^TestRunSandboxedProcessRetainsProfileForDelayedChildRead/child$"
	for _, argument := range os.Args {
		if argument == childRun {
			time.Sleep(25 * time.Millisecond)
			profile := os.NewFile(4, "profile-pipe")
			if profile == nil {
				os.Exit(91)
			}
			payload, err := io.ReadAll(profile)
			if err != nil {
				os.Exit(92)
			}
			_, _ = os.Stdout.Write(payload)
			return
		}
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	profilePath := filepath.Join(root, "profile.sb")
	writeLauncherFile(t, profilePath, []byte("delayed-profile-consumed"), 0o600)
	profileFile, err := os.Open(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	defer profileFile.Close()
	helperFile, err := os.Open("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	defer helperFile.Close()
	validations := 0
	validate := func() error {
		validations++
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	output, err := runSandboxedProcess(ctx, executable, []string{childRun}, nil, []*os.File{helperFile, profileFile}, validate, func(context.Context, int, bool) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if validations != 2 || !bytes.Contains(output, []byte("delayed-profile-consumed")) {
		t.Fatalf("validations=%d output=%q", validations, output)
	}
}

func TestCopyRetainedProfileStopsAtContextCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.sb")
	writeLauncherFile(t, path, bytes.Repeat([]byte("p"), 128<<10), 0o600)
	profile, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer profile.Close()
	ctx, cancel := context.WithCancel(context.Background())
	writes := 0
	err = copyRetainedProfileContext(ctx, profile, writerFunc(func(value []byte) (int, error) {
		writes++
		cancel()
		return len(value), nil
	}))
	if !errors.Is(err, context.Canceled) || writes != 1 {
		t.Fatalf("writes=%d error=%v", writes, err)
	}
}

type writerFunc func([]byte) (int, error)

func (w writerFunc) Write(value []byte) (int, error) { return w(value) }

func TestRunSandboxedProcessKillsChildOnPostStartHelperSwap(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	root, err := os.MkdirTemp(launcherRepositoryRoot(t), ".sbrhelper-post-start-swap-test-")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	helper := []byte("trusted helper")
	sourceHelper := filepath.Join(root, "source-helper")
	writeLauncherFile(t, sourceHelper, helper, 0o500)
	runtimeBase := filepath.Join(root, "runtime")
	if err = os.Mkdir(runtimeBase, 0o700); err != nil {
		t.Fatal(err)
	}
	profilePath := writeLauncherProfile(t, root, helper, now)
	staged, err := sbrprofile.AuthenticateAndStage(context.Background(), profilePath, launcherLocator{sbrprofile.ResourceSet{HelperPath: sourceHelper, TrustedRuntimeBase: runtimeBase}}, now)
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Close()
	helperFile, err := staged.OpenHelperExecutable()
	if err != nil {
		t.Fatal(err)
	}
	defer helperFile.Close()
	profileFile, err := staged.CreatePrivateRuntimeFile("sandbox.sb", []byte("profile"))
	if err != nil {
		t.Fatal(err)
	}
	defer profileFile.Close()
	validations := 0
	validate := func() error {
		validations++
		if validations == 2 {
			if err := os.Rename(staged.HelperPath, staged.HelperPath+".trusted"); err != nil {
				return err
			}
			if err := os.WriteFile(staged.HelperPath, []byte("malicious"), 0o500); err != nil {
				return err
			}
			if err := os.Chmod(staged.HelperPath, 0o500); err != nil {
				return err
			}
		}
		return staged.Revalidate()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := time.Now()
	if _, err = runSandboxedProcess(ctx, "/bin/sleep", []string{"10"}, nil, []*os.File{helperFile, profileFile}, validate, func(context.Context, int, bool) error { return nil }); err == nil {
		t.Fatal("post-start swap retained child")
	}
	if validations != 2 || time.Since(started) > 2*time.Second {
		t.Fatalf("child was not killed promptly: validations=%d elapsed=%s", validations, time.Since(started))
	}
}

func TestRunSandboxedProcessWithholdsRequestUntilProcessIdentityVerified(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "stdin-received")
	profilePath := filepath.Join(root, "profile.sb")
	writeLauncherFile(t, profilePath, []byte("profile"), 0o600)
	profileFile, err := os.Open(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	defer profileFile.Close()
	helperFile, err := os.Open("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	defer helperFile.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	command := "IFS= read -r line && /usr/bin/touch " + marker
	verificationCalls := 0
	_, err = runSandboxedProcess(ctx, "/bin/sh", []string{"-c", command}, []byte("secret-frame\n"), []*os.File{helperFile, profileFile}, func() error { return nil }, func(context.Context, int, bool) error {
		verificationCalls++
		if verificationCalls == 1 {
			return nil
		}
		return errors.New("wrong executable")
	})
	if err == nil {
		t.Fatal("wrong executable accepted")
	}
	if verificationCalls != 2 {
		t.Fatalf("verification calls=%d, want initial and pre-stdin checks", verificationCalls)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("wrong executable received stdin: %v", statErr)
	}
}

func TestRunSandboxedProcessChecksFreshnessAfterFinalAuthorityAndIdentity(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "stdin-received")
	profilePath := filepath.Join(root, "profile.sb")
	writeLauncherFile(t, profilePath, []byte("profile"), 0o600)
	profileFile, err := os.Open(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	defer profileFile.Close()
	helperFile, err := os.Open("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	defer helperFile.Close()
	now := time.Now().UTC()
	clock := now
	events := []string{}
	var childPID int
	verificationCalls := 0
	verify := func(ctx context.Context, pid int, initial bool) error {
		verificationCalls++
		childPID = pid
		if initial {
			return nil
		}
		return verifyPreStdinAuthority(
			ctx,
			func() error {
				events = append(events, "authority")
				return nil
			},
			func(context.Context, int) error {
				events = append(events, "identity")
				clock = now.Add(2 * time.Hour)
				return nil
			},
			pid,
			func() error {
				events = append(events, "freshness")
				if clock.After(now.Add(time.Hour)) {
					return launchFreshnessError{err: protocolError(string(StableErrorProfileExpired))}
				}
				return nil
			},
		)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	command := "IFS= read -r line && /usr/bin/touch " + marker
	_, err = runSandboxedProcess(ctx, "/bin/sh", []string{"-c", command}, []byte("secret-frame\n"), []*os.File{helperFile, profileFile}, func() error { return nil }, verify)
	if err == nil {
		t.Fatal("expiry during final identity verification accepted")
	}
	if verificationCalls != 2 || strings.Join(events, ",") != "authority,identity,freshness" {
		t.Fatalf("verification calls=%d order=%q", verificationCalls, events)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("expired child received stdin: %v", statErr)
	}
	if childPID <= 0 || !errors.Is(unix.Kill(childPID, 0), unix.ESRCH) {
		t.Fatalf("expired child was not killed and waited: pid=%d", childPID)
	}
}

func TestLauncherRechecksProfileExpiryImmediatelyBeforeSpawn(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	root, err := os.MkdirTemp(launcherRepositoryRoot(t), ".sbrhelper-expiry-test-")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	helper := []byte("helper")
	helperPath := filepath.Join(root, "helper")
	writeLauncherFile(t, helperPath, helper, 0o500)
	base := filepath.Join(root, "runtime")
	if err = os.Mkdir(base, 0o700); err != nil {
		t.Fatal(err)
	}
	profilePath := writeLauncherProfile(t, root, helper, now)
	launcher := NewLauncher(launcherLocator{sbrprofile.ResourceSet{HelperPath: helperPath, TrustedRuntimeBase: base}})
	calls := 0
	launcher.now = func() time.Time {
		calls++
		if calls == 1 {
			return now
		}
		return now.Add(2 * time.Hour)
	}
	launched := false
	launcher.run = func(context.Context, string, []string, []byte, []*os.File, func() error, childVerifier) ([]byte, error) {
		launched = true
		return nil, nil
	}
	request := scopedLauncherFixture(Request{ProtocolVersion: 1, RequestID: "018bcfe5-6800-7000-8000-000000000001", Operation: OperationFixture, DeadlineMillis: now.Add(time.Minute).UnixMilli(), Environment: EnvironmentSimulator, SimulatorCase: SimulatorAccepted})
	_, err = launcher.Launch(context.Background(), profilePath, request)
	if err == nil || err.Error() != string(StableErrorProfileExpired) || launched {
		t.Fatalf("error=%v launched=%t", err, launched)
	}
	entries, readErr := os.ReadDir(base)
	if readErr != nil || len(entries) != 0 {
		t.Fatalf("expired launch leaked runtime root: %v %v", entries, readErr)
	}
}

func TestLauncherRechecksProfileExpiryAfterProcessIdentityBeforeStdin(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	root, err := os.MkdirTemp(launcherRepositoryRoot(t), ".sbrhelper-prestdin-expiry-test-")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	helper := []byte("helper")
	helperPath := filepath.Join(root, "helper")
	writeLauncherFile(t, helperPath, helper, 0o500)
	base := filepath.Join(root, "runtime")
	if err = os.Mkdir(base, 0o700); err != nil {
		t.Fatal(err)
	}
	profilePath := writeLauncherProfile(t, root, helper, now)
	launcher := NewLauncher(launcherLocator{sbrprofile.ResourceSet{HelperPath: helperPath, TrustedRuntimeBase: base}})
	advanced := false
	launcher.now = func() time.Time {
		if advanced {
			return now.Add(2 * time.Hour)
		}
		return now
	}
	installFakeCodeIdentity(launcher)
	launcher.run = func(runCtx context.Context, _ string, _ []string, _ []byte, _ []*os.File, _ func() error, verify childVerifier) ([]byte, error) {
		if err := verify(runCtx, 123, true); err != nil {
			return nil, err
		}
		advanced = true
		return nil, verify(runCtx, 123, false)
	}
	request := scopedLauncherFixture(Request{ProtocolVersion: 1, RequestID: "018bcfe5-6800-7000-8000-000000000001", Operation: OperationFixture, DeadlineMillis: now.Add(time.Minute).UnixMilli(), Environment: EnvironmentSimulator, SimulatorCase: SimulatorAccepted})
	_, err = launcher.Launch(context.Background(), profilePath, request)
	if err == nil || err.Error() != string(StableErrorProfileExpired) {
		t.Fatalf("error=%v", err)
	}
	entries, readErr := os.ReadDir(base)
	if readErr != nil || len(entries) != 0 {
		t.Fatalf("expired pre-stdin launch leaked runtime root: %v %v", entries, readErr)
	}
}

func allZero(value []byte) bool {
	for _, b := range value {
		if b != 0 {
			return false
		}
	}
	return true
}

func writeLauncherProfile(t *testing.T, root string, helper []byte, now time.Time) string {
	t.Helper()
	sum := sha256.Sum256(helper)
	raw := []byte(`{"component_manifest_sha256":"NONE","endpoint_profile_sha256":"NONE","environment":"SIMULATOR","expires_at":"` + now.Add(time.Hour).Format("2006-01-02T15:04:05Z") + `","helper_sha256":"` + hex.EncodeToString(sum[:]) + `","issued_at":"` + now.Add(-time.Hour).Format("2006-01-02T15:04:05Z") + `","registration_manifest_sha256":"NONE","schema_version":1,"target":"darwin/arm64"}`)
	parsed, err := sbrprofile.ParseProfile(raw, now)
	if err != nil {
		t.Fatal(err)
	}
	seed, _ := hex.DecodeString("9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60")
	signature := append([]byte(base64.StdEncoding.EncodeToString(ed25519.Sign(ed25519.NewKeyFromSeed(seed), parsed.Canonical))), '\n')
	path := filepath.Join(root, "profile.json")
	writeLauncherFile(t, path, raw, 0o600)
	writeLauncherFile(t, strings.TrimSuffix(path, filepath.Ext(path))+".sig", signature, 0o600)
	return path
}
func writeLauncherFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func launcherRepositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../../../.."))
}
