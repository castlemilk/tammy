//go:build darwin && arm64 && cgo

package sbrhelper

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestSandboxGuardContextStopsRevalidationBeforePathWork(t *testing.T) {
	base, root, executable, _, _ := secureSandboxTree(t)
	_, guard, err := RenderDevelopmentSandboxProfile(SandboxProfileInput{TrustedBase: base, StagedRoot: root, StagedExecutables: []string{executable}})
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := guard.RevalidateContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}

func TestCoreConstructsExactSandboxProfileAndRevalidatesAtSpawnBoundary(t *testing.T) {
	base, root, executable, readable, selected := secureSandboxTree(t)
	profile, guard, err := RenderDevelopmentSandboxProfile(SandboxProfileInput{
		TrustedBase: base, StagedRoot: root, StagedExecutables: []string{executable}, StagedReadOnlyFiles: []string{readable}, SelectedReadFiles: []string{selected},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	contents, err := profile.PrepareSpawn()
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(contents, "\n")
	if len(lines) < 4 || lines[0] != "(version 1)" || lines[1] != `(import "system.sb")` || lines[2] != "(deny default)" {
		t.Fatalf("profile preamble/order invalid:\n%s", contents)
	}
	for _, literal := range []string{executable, readable, selected} {
		if !strings.Contains(contents, `(literal "`+literal+`")`) {
			t.Fatalf("missing exact literal %q:\n%s", literal, contents)
		}
	}
	var processRule string
	for _, line := range lines {
		if strings.HasPrefix(line, "(allow process-exec") {
			processRule = line
		}
	}
	if processRule == "" || !strings.Contains(processRule, `(literal "`+executable+`")`) || strings.Contains(processRule, readable) || strings.Contains(processRule, selected) ||
		strings.Contains(contents, `(allow process-exec (subpath`) || strings.Contains(contents, `(allow process-exec)`) || strings.Contains(contents, "tammy-keychain-service") {
		t.Fatalf("profile has broad execution or inert Keychain claim:\n%s", contents)
	}
	if !strings.Contains(contents, `(allow mach-lookup (global-name "com.apple.securityd"))`) || !strings.Contains(contents, "(deny network*)") || strings.Contains(contents, "(allow network") {
		t.Fatalf("profile must expose only the documented securityd platform lookup and explicitly deny network:\n%s", contents)
	}
	if profile.FileMode() != 0o600 || profile.OwnerUID() != os.Geteuid() {
		t.Fatalf("profile ownership contract mode=%o uid=%d", profile.FileMode(), profile.OwnerUID())
	}
}

func TestSandboxRejectsUntrustedBaseAndInvalidStagedRoot(t *testing.T) {
	base, root, executable, readable, selected := secureSandboxTree(t)
	valid := sandboxInput(base, root, executable, readable, selected)
	for name, mutate := range map[string]func(*SandboxProfileInput){
		"relative base":    func(v *SandboxProfileInput) { v.TrustedBase = "relative" },
		"root base":        func(v *SandboxProfileInput) { v.TrustedBase = "/" },
		"temp base":        func(v *SandboxProfileInput) { v.TrustedBase = "/private/tmp" },
		"home base":        func(v *SandboxProfileInput) { home, _ := os.UserHomeDir(); v.TrustedBase = home },
		"system base":      func(v *SandboxProfileInput) { v.TrustedBase = "/Applications" },
		"wrong prefix":     func(v *SandboxProfileInput) { v.StagedRoot = filepath.Join(base, "runtime-bad") },
		"not direct child": func(v *SandboxProfileInput) { v.StagedRoot = filepath.Join(base, "nested", filepath.Base(root)) },
	} {
		t.Run(name, func(t *testing.T) {
			input := valid
			mutate(&input)
			if profile, guard, err := RenderDevelopmentSandboxProfile(input); !errors.Is(err, ErrSandboxProfileInvalid) || guard != nil || profile.guard != nil {
				t.Fatalf("profile=%#v guard=%#v error=%v", profile, guard, err)
			}
		})
	}

	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, guard, err := RenderDevelopmentSandboxProfile(valid); !errors.Is(err, ErrSandboxProfileInvalid) || guard != nil {
		t.Fatalf("wrong root mode guard=%#v error=%v", guard, err)
	}
}

func TestSandboxRequiresExactStagedModesButDoesNotChmodSelectedFile(t *testing.T) {
	for name, mutate := range map[string]func(string, string, string){
		"executable 0700": func(executable, _, _ string) { _ = os.Chmod(executable, 0o700) },
		"executable 0400": func(executable, _, _ string) { _ = os.Chmod(executable, 0o400) },
		"readonly 0600":   func(_, readonly, _ string) { _ = os.Chmod(readonly, 0o600) },
		"readonly 0500":   func(_, readonly, _ string) { _ = os.Chmod(readonly, 0o500) },
	} {
		t.Run(name, func(t *testing.T) {
			base, root, executable, readonly, selected := secureSandboxTree(t)
			mutate(executable, readonly, selected)
			if _, guard, err := RenderDevelopmentSandboxProfile(sandboxInput(base, root, executable, readonly, selected)); !errors.Is(err, ErrSandboxProfileInvalid) || guard != nil {
				t.Fatalf("guard=%#v error=%v", guard, err)
			}
		})
	}
	base, root, executable, readonly, selected := secureSandboxTree(t)
	if err := os.Chmod(selected, 0o644); err != nil {
		t.Fatal(err)
	}
	profile, guard, err := RenderDevelopmentSandboxProfile(sandboxInput(base, root, executable, readonly, selected))
	if err != nil {
		t.Fatalf("selected user file mode must not be constrained: %v", err)
	}
	defer guard.Close()
	if _, err := profile.PrepareSpawn(); err != nil {
		t.Fatal(err)
	}
	external, err := os.CreateTemp("/private/tmp", "tammy-selected-credential-")
	if err != nil {
		t.Fatal(err)
	}
	externalPath := external.Name()
	if err := external.Close(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(externalPath) })
	if err := os.Chmod(externalPath, 0o644); err != nil {
		t.Fatal(err)
	}
	base, root, executable, readonly, _ = secureSandboxTree(t)
	profile, guard, err = RenderDevelopmentSandboxProfile(sandboxInput(base, root, executable, readonly, externalPath))
	if err != nil {
		t.Fatalf("separately guarded selected path under user-writable ancestor was rejected: %v", err)
	}
	defer guard.Close()
	if _, err := profile.PrepareSpawn(); err != nil {
		t.Fatal(err)
	}
}

func TestSandboxRejectsStagedSpecialFilesSymlinksAndSelectedStagingAliases(t *testing.T) {
	t.Run("executable symlink", func(t *testing.T) {
		base, root, executable, readonly, selected := secureSandboxTree(t)
		if err := os.Remove(executable); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(selected, executable); err != nil {
			t.Fatal(err)
		}
		if _, guard, err := RenderDevelopmentSandboxProfile(sandboxInput(base, root, executable, readonly, selected)); !errors.Is(err, ErrSandboxProfileInvalid) || guard != nil {
			t.Fatalf("guard=%#v error=%v", guard, err)
		}
	})
	t.Run("readonly fifo", func(t *testing.T) {
		base, root, executable, readonly, selected := secureSandboxTree(t)
		if err := os.Remove(readonly); err != nil {
			t.Fatal(err)
		}
		if err := unix.Mkfifo(readonly, 0o400); err != nil {
			t.Fatal(err)
		}
		if _, guard, err := RenderDevelopmentSandboxProfile(sandboxInput(base, root, executable, readonly, selected)); !errors.Is(err, ErrSandboxProfileInvalid) || guard != nil {
			t.Fatalf("guard=%#v error=%v", guard, err)
		}
	})
	t.Run("selected path aliases staging", func(t *testing.T) {
		base, root, executable, readonly, _ := secureSandboxTree(t)
		input := sandboxInput(base, root, executable, readonly, readonly)
		if _, guard, err := RenderDevelopmentSandboxProfile(input); !errors.Is(err, ErrSandboxProfileInvalid) || guard != nil {
			t.Fatalf("guard=%#v error=%v", guard, err)
		}
	})
}

func TestTrustedBaseLexicallyRejectsBroadAuthorities(t *testing.T) {
	home, _ := os.UserHomeDir()
	for _, path := range []string{"/", "/tmp", "/private", "/private/tmp", "/Applications", "/Library", "/System", "/Users", "/Volumes", home, "relative"} {
		if validTrustedBasePath(path) {
			t.Fatalf("broad base accepted: %q", path)
		}
	}
}

func TestSandboxIdentityRejectsWrongOwnerAndEscapesSchemeLiterals(t *testing.T) {
	wrongOwner := uint64(os.Geteuid() + 1)
	path := &retainedPath{components: []retainedComponent{{directory: true, identity: pathIdentity{mode: uint64(0o700) | uint64(unix.S_IFDIR), uid: wrongOwner}}}}
	if path.leafDirectory(uint64(os.Geteuid()), 0o700) {
		t.Fatal("wrong-owner staging directory accepted")
	}
	regular := &retainedPath{components: []retainedComponent{{identity: pathIdentity{mode: uint64(0o500) | uint64(unix.S_IFREG), uid: wrongOwner}}}}
	if regular.leafRegularExact(uint64(os.Geteuid()), 0o500) {
		t.Fatal("wrong-owner staged executable accepted")
	}
	if got, want := schemeString(`a\b"c`), `a\\b\"c`; got != want {
		t.Fatalf("scheme escape=%q want=%q", got, want)
	}
	if validAbsolute("/safe/path\nmalicious") {
		t.Fatal("control-bearing path accepted")
	}
}

func TestSandboxGuardRejectsSwapsAndBecomesUnusableAfterClose(t *testing.T) {
	base, root, executable, readable, selected := secureSandboxTree(t)
	profile, guard, err := RenderDevelopmentSandboxProfile(sandboxInput(base, root, executable, readable, selected))
	if err != nil {
		t.Fatal(err)
	}
	original := executable + ".original"
	if err := os.Rename(executable, original); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("replacement"), 0o500); err != nil {
		t.Fatal(err)
	}
	if _, err := profile.PrepareSpawn(); !errors.Is(err, ErrSandboxAuthorityChanged) {
		t.Fatalf("leaf swap error=%v", err)
	}
	descriptor := guard.paths[0].components[len(guard.paths[0].components)-1].fd
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(descriptor, &stat); !errors.Is(err, unix.EBADF) {
		t.Fatalf("retained descriptor remained open: %v", err)
	}
	if _, err := profile.PrepareSpawn(); !errors.Is(err, ErrSandboxAuthorityClosed) {
		t.Fatalf("closed profile error=%v", err)
	}
}

func TestSandboxGuardRejectsAncestorSwapAndInitialSymlink(t *testing.T) {
	base, root, executable, readable, selected := secureSandboxTree(t)
	profile, guard, err := RenderDevelopmentSandboxProfile(sandboxInput(base, root, executable, readable, selected))
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	original := root + ".original"
	if err := os.Rename(root, original); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(original, root); err != nil {
		t.Fatal(err)
	}
	if _, err := profile.PrepareSpawn(); !errors.Is(err, ErrSandboxAuthorityChanged) {
		t.Fatalf("ancestor swap error=%v", err)
	}

	linkedBase := filepath.Join(filepath.Dir(base), "tammy-sbr-linked-base")
	if err := os.Symlink(base, linkedBase); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(linkedBase) })
	linkedRoot := filepath.Join(linkedBase, filepath.Base(root))
	if _, linkedGuard, err := RenderDevelopmentSandboxProfile(SandboxProfileInput{TrustedBase: linkedBase, StagedRoot: linkedRoot, StagedExecutables: []string{filepath.Join(linkedRoot, filepath.Base(executable))}}); !errors.Is(err, ErrSandboxProfileInvalid) || linkedGuard != nil {
		t.Fatalf("initial symlink guard=%#v error=%v", linkedGuard, err)
	}
}

func TestSandboxDirectoryGuardAllowsSiblingChurnButRejectsModeChange(t *testing.T) {
	base, root, executable, readable, selected := secureSandboxTree(t)
	profile, guard, err := RenderDevelopmentSandboxProfile(sandboxInput(base, root, executable, readable, selected))
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	sibling := filepath.Join(root, "unrelated")
	if err := os.WriteFile(sibling, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := profile.PrepareSpawn(); err != nil {
		t.Fatalf("sibling churn invalidated authority: %v", err)
	}
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := profile.PrepareSpawn(); !errors.Is(err, ErrSandboxAuthorityChanged) {
		t.Fatalf("mode change error=%v", err)
	}
}

func secureSandboxTree(t *testing.T) (string, string, string, string, string) {
	t.Helper()
	base, err := os.MkdirTemp(".", "tammy-sbr-base-")
	if err != nil {
		t.Fatal(err)
	}
	base, err = filepath.Abs(base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	if err := os.Chmod(base, 0o700); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "tammy-sbr-runtime-Abc123")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(root, "tammy-sbr-helper")
	readable := filepath.Join(root, "component.bundle")
	selected := filepath.Join(base, "selected-credential.p12")
	if err := os.WriteFile(executable, []byte("synthetic executable"), 0o500); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(readable, []byte("synthetic component"), 0o400); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(selected, []byte("synthetic selected credential"), 0o600); err != nil {
		t.Fatal(err)
	}
	return base, root, executable, readable, selected
}

func sandboxInput(base, root, executable, readonly, selected string) SandboxProfileInput {
	return SandboxProfileInput{TrustedBase: base, StagedRoot: root, StagedExecutables: []string{executable}, StagedReadOnlyFiles: []string{readonly}, SelectedReadFiles: []string{selected}}
}
