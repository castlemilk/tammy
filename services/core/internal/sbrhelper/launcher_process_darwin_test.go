//go:build darwin && arm64 && cgo

package sbrhelper

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestFinishSandboxedProcessOutputZerosCapturedBytesOnPostReadFailure(t *testing.T) {
	for _, test := range []struct {
		name       string
		writeErr   error
		profileErr error
		readErr    error
	}{
		{name: "stdin write", writeErr: errors.New("stdin closed")},
		{name: "profile copy", profileErr: errors.New("profile copy")},
		{name: "stdout read", readErr: errors.New("stdout read")},
	} {
		t.Run(test.name, func(t *testing.T) {
			captured := []byte("sensitive malformed helper stdout")
			retained := captured
			output, err := finishSandboxedProcessOutput(captured, test.writeErr, test.profileErr, test.readErr, nil)
			if err == nil || output != nil {
				t.Fatalf("finish output = %x, %v", output, err)
			}
			if !bytes.Equal(retained, make([]byte, len(retained))) {
				t.Fatalf("captured stdout backing retained bytes: %x", retained)
			}
		})
	}
}

func TestFinishSandboxedProcessOutputTransfersNonzeroExitOwnership(t *testing.T) {
	captured := []byte{0, 0, 0, 3, 0x0a, 0x01, 'x'}
	output, err := finishSandboxedProcessOutput(captured, nil, nil, nil, errors.New("exit status 31"))
	if !errors.Is(err, errProcessExited) || !bytes.Equal(output, captured) || len(output) == 0 || &output[0] != &captured[0] {
		t.Fatalf("nonzero exit output = %x, %v", output, err)
	}
}

func TestSandboxedProcessRetainsBoundedStdoutOnNonzeroExit(t *testing.T) {
	root := t.TempDir()
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
	want := []byte{0, 0, 0, 3, 0x0a, 0x01, 'x'}
	output, err := runSandboxedProcess(ctx, "/bin/sh", []string{"-c", "cat >/dev/null; printf '\\000\\000\\000\\003\\012\\001x'; exit 31"}, nil,
		[]*os.File{helperFile, profileFile}, func() error { return nil }, func(context.Context, int, bool) error { return nil })
	if !errors.Is(err, errProcessExited) {
		t.Fatalf("process exit error = %v", err)
	}
	if !bytes.Equal(output, want) {
		t.Fatalf("bounded stdout = %x, want %x", output, want)
	}
}

func TestDarwinCodeIdentityBindsStaticHelperToLivePID(t *testing.T) {
	if os.Getenv("TAMMY_CODE_IDENTITY_CHILD") == "1" {
		time.Sleep(10 * time.Second)
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	expected, err := captureStaticCodeIdentity(executable)
	if err != nil {
		t.Fatalf("test helper has no usable static code identity: %v", err)
	}
	command := exec.Command(executable, "-test.run=^TestDarwinCodeIdentityBindsStaticHelperToLivePID$")
	command.Env = append(os.Environ(), "TAMMY_CODE_IDENTITY_CHILD=1")
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	live, err := waitLiveCodeIdentity(ctx, command.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if !sameCodeIdentity(expected, live) {
		t.Fatalf("static/live identity mismatch: expected=%+v live=%+v", expected, live)
	}
	other, err := captureStaticCodeIdentity("/bin/sleep")
	if err != nil {
		t.Fatal(err)
	}
	if sameCodeIdentity(other, live) {
		t.Fatal("different executable shared live code identity")
	}
}

func TestDarwinCodeIdentityRejectsWrongCDHash(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	identity, err := captureStaticCodeIdentity(executable)
	if err != nil {
		t.Fatal(err)
	}
	wrong := identity
	wrong.cdHash = append([]byte(nil), identity.cdHash...)
	wrong.cdHash[0] ^= 0xff
	if sameCodeIdentity(identity, wrong) {
		t.Fatal("wrong CDHash accepted")
	}
}

func TestDarwinStaticCodeIdentityCanBeCapturedFromRetainedDescriptor(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	expected, err := captureStaticCodeIdentity(executable)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := os.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	defer retained.Close()
	fromDescriptor, err := captureStaticCodeIdentity("/dev/fd/" + strconv.Itoa(int(retained.Fd())))
	if err != nil {
		t.Fatalf("retained descriptor identity unavailable: %v", err)
	}
	if !sameCodeIdentity(expected, fromDescriptor) {
		t.Fatalf("descriptor identity mismatch: expected=%+v got=%+v", expected, fromDescriptor)
	}
}

func TestWrongLiveCDHashWithholdsStdinEvenWhenPathMatches(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	expected, err := captureStaticCodeIdentity(executable)
	if err != nil {
		t.Fatal(err)
	}
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
	_, err = runSandboxedProcess(ctx, "/bin/sh", []string{"-c", command}, []byte("secret-frame\n"), []*os.File{helperFile, profileFile}, func() error { return nil }, func(verifyCtx context.Context, pid int, _ bool) error {
		return waitForExpectedLiveCodeIdentity(verifyCtx, pid, expected, "/bin/sh", false)
	})
	if err == nil {
		t.Fatal("wrong live CDHash accepted")
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("wrong-CDHash process received stdin: %v", statErr)
	}
}

func TestSamePathSwapRestoreCannotChangeLiveCodeIdentity(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	expected, err := captureStaticCodeIdentity(executable)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	trustedPath := filepath.Join(root, "trusted-helper")
	copyExecutableForIdentityTest(t, "/bin/sleep", trustedPath)
	command := exec.Command(trustedPath, "10")
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	})
	runningPath := trustedPath + ".running"
	if err = os.Rename(trustedPath, runningPath); err != nil {
		t.Fatal(err)
	}
	copyExecutableForIdentityTest(t, executable, trustedPath)
	restored, err := captureStaticCodeIdentity(trustedPath)
	if err != nil || !sameCodeIdentity(expected, restored) {
		t.Fatalf("trusted path was not restored to expected identity: %+v %v", restored, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err = waitForExpectedLiveCodeIdentity(ctx, command.Process.Pid, expected, trustedPath, false); err == nil {
		t.Fatal("same-path swap/restore changed live process identity")
	}
}

func TestLiveIdentityPollingStopsAtDeadline(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	expected, err := captureStaticCodeIdentity(executable)
	if err != nil {
		t.Fatal(err)
	}
	sandboxPath := filepath.Join(t.TempDir(), "sandbox-exec")
	copyExecutableForIdentityTest(t, "/bin/sleep", sandboxPath)
	command := exec.Command(sandboxPath, "10")
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	err = waitForExpectedLiveCodeIdentity(ctx, command.Process.Pid, expected, filepath.Join(t.TempDir(), "helper"), true)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%v", err)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("identity polling ignored deadline for %s", elapsed)
	}
}

func TestLiveIdentityPollingStopsWhenChildIsZombie(t *testing.T) {
	command := exec.Command("/usr/bin/true")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Wait() }()
	deadline := time.Now().Add(time.Second)
	for !processExited(command.Process.Pid) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !processExited(command.Process.Pid) {
		t.Fatal("child did not enter an exited state")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	started := time.Now()
	err := waitForExpectedLiveCodeIdentityWithSamplers(ctx, command.Process.Pid, codeIdentity{cdHash: []byte{1}}, "/helper", true,
		func(int) (string, error) { return "", errors.New("unavailable") },
		func(context.Context, int) (codeIdentity, error) { return codeIdentity{}, errors.New("unavailable") })
	if err == nil || time.Since(started) > 100*time.Millisecond {
		t.Fatalf("zombie child was not rejected promptly: elapsed=%s error=%v", time.Since(started), err)
	}
}

func TestLiveIdentityResamplesAcrossSandboxExecTransition(t *testing.T) {
	expected := codeIdentity{cdHash: []byte{1, 2, 3}, identifier: "helper"}
	helperPath := "/private/runtime/sbr-helper"
	paths := []string{"/usr/bin/sandbox-exec", helperPath, helperPath, helperPath}
	pathCalls := 0
	identityCalls := 0
	err := waitForExpectedLiveCodeIdentityWithSamplers(
		context.Background(),
		123,
		expected,
		helperPath,
		true,
		func(int) (string, error) {
			path := paths[pathCalls]
			pathCalls++
			return path, nil
		},
		func(context.Context, int) (codeIdentity, error) {
			identityCalls++
			return expected, nil
		},
	)
	if err != nil {
		t.Fatalf("valid sandbox-exec transition rejected: %v", err)
	}
	if pathCalls != 4 || identityCalls != 4 {
		t.Fatalf("path calls=%d identity calls=%d", pathCalls, identityCalls)
	}
}

func TestLiveIdentityStableWrongExecutableStillFails(t *testing.T) {
	expected := codeIdentity{cdHash: []byte{1, 2, 3}}
	wrong := codeIdentity{cdHash: []byte{9, 9, 9}}
	err := waitForExpectedLiveCodeIdentityWithSamplers(
		context.Background(),
		123,
		expected,
		"/private/runtime/sbr-helper",
		true,
		func(int) (string, error) { return "/private/runtime/sbr-helper", nil },
		func(context.Context, int) (codeIdentity, error) { return wrong, nil },
	)
	if err == nil {
		t.Fatal("stable wrong executable identity accepted")
	}
}

func copyExecutableForIdentityTest(t *testing.T, source, target string) {
	t.Helper()
	contents, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(target, contents, 0o500); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(target, 0o500); err != nil {
		t.Fatal(err)
	}
}
