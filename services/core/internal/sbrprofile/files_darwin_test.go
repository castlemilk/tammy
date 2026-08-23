//go:build darwin && arm64 && cgo

package sbrprofile

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type testLocator struct{ resources ResourceSet }

func (l testLocator) Locate(Profile) (ResourceSet, error) { return l.resources, nil }

type countingLocator struct{ calls int }

func (l *countingLocator) Locate(Profile) (ResourceSet, error) {
	l.calls++
	return ResourceSet{}, errors.New("must not locate")
}

func TestAuthenticateAndStageSimulatorNeverExecutesSourceAndOwnsCleanup(t *testing.T) {
	root := trustedTestRoot(t)
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	helperPath := filepath.Join(root, "source-helper")
	helper := []byte("authenticated helper bytes")
	writePrivate(t, helperPath, helper, 0o500)
	runtimeBase := filepath.Join(root, "runtime")
	if err := os.Mkdir(runtimeBase, 0o700); err != nil {
		t.Fatal(err)
	}
	profilePath := filepath.Join(root, "profile.json")
	writeSignedSimulatorProfile(t, profilePath, helper)
	if _, err := openSecure(profilePath, MaxProfileBytes); err != nil {
		t.Fatalf("profile secure open: %v", err)
	}
	if _, err := openSecure(profileSignaturePath(profilePath), 128); err != nil {
		t.Fatalf("signature secure open: %v", err)
	}
	staged, err := AuthenticateAndStage(context.Background(), profilePath, testLocator{ResourceSet{HelperPath: helperPath, TrustedRuntimeBase: runtimeBase}}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if staged.HelperPath == helperPath || !strings.HasPrefix(filepath.Base(staged.RuntimeRoot), "tammy-sbr-runtime-") {
		t.Fatalf("invalid staged result: %+v", staged)
	}
	info, err := os.Stat(staged.HelperPath)
	if err != nil || info.Mode().Perm() != 0o500 {
		t.Fatalf("helper mode=%v err=%v", info.Mode(), err)
	}
	if err := staged.Revalidate(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(staged.HelperPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged.HelperPath, []byte("tampered helper bytes"), 0o500); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(staged.HelperPath, 0o500); err != nil {
		t.Fatal(err)
	}
	if err := staged.Revalidate(); err == nil {
		t.Fatal("staged tamper not detected")
	}
	runtimeRoot := staged.RuntimeRoot
	if err = staged.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Lstat(runtimeRoot); !os.IsNotExist(err) {
		t.Fatal("runtime root retained")
	}
}

func TestSecureOpenRejectsSymlinkHardlinkAndWritableSource(t *testing.T) {
	root := trustedTestRoot(t)
	source := filepath.Join(root, "source")
	writePrivate(t, source, []byte("x"), 0o600)
	symlink := filepath.Join(root, "symlink")
	if err := os.Symlink(source, symlink); err != nil {
		t.Fatal(err)
	}
	if _, err := openSecure(symlink, 10); err == nil {
		t.Fatal("accepted symlink")
	}
	hardlink := filepath.Join(root, "hardlink")
	if err := os.Link(source, hardlink); err != nil {
		t.Fatal(err)
	}
	if _, err := openSecure(source, 10); err == nil {
		t.Fatal("accepted multiply linked file")
	}
	if err := os.Remove(hardlink); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(source, 0o622); err != nil {
		t.Fatal(err)
	}
	if _, err := openSecure(source, 10); err == nil {
		t.Fatal("accepted group/world writable file")
	}
}

func TestSecureDescriptorTraversalDoesNotLeakDeepParentsOnSuccessOrError(t *testing.T) {
	root := trustedTestRoot(t)
	deep := filepath.Join(root, "one", "two", "three", "four")
	if err := os.MkdirAll(deep, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(deep, "value")
	writePrivate(t, path, []byte("value"), 0o600)
	before := openDescriptorCount(t)
	for attempt := 0; attempt < 128; attempt++ {
		if _, err := openSecureContext(context.Background(), path, 16); err != nil {
			t.Fatal(err)
		}
		if _, err := openSecureContext(context.Background(), filepath.Join(deep, "missing"), 16); err == nil {
			t.Fatal("missing leaf opened")
		}
	}
	after := openDescriptorCount(t)
	if after > before+2 {
		t.Fatalf("descriptor count grew from %d to %d", before, after)
	}
}

func openDescriptorCount(t *testing.T) int {
	t.Helper()
	count := 0
	for fd := 0; fd < unix.Getdtablesize(); fd++ {
		if _, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); err == nil {
			count++
		}
	}
	return count
}

func TestSecureTraversalNeverClosesAConcurrentSentinelDescriptor(t *testing.T) {
	root := trustedTestRoot(t)
	deep := filepath.Join(root, "one", "two", "three")
	if err := os.MkdirAll(deep, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(deep, "value")
	writePrivate(t, path, []byte("value"), 0o600)
	stop := make(chan struct{})
	failure := make(chan error, 1)
	var workers sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				fd, err := unix.Open("/dev/null", unix.O_RDONLY|unix.O_CLOEXEC, 0)
				if err != nil {
					select {
					case failure <- err:
					default:
					}
					return
				}
				runtime.Gosched()
				_, sentinelErr := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
				_ = unix.Close(fd)
				if sentinelErr != nil {
					select {
					case failure <- sentinelErr:
					default:
					}
					return
				}
			}
		}()
	}
	for attempt := 0; attempt < 512; attempt++ {
		if _, err := openSecureContext(context.Background(), path, 16); err != nil {
			close(stop)
			workers.Wait()
			t.Fatal(err)
		}
	}
	close(stop)
	workers.Wait()
	select {
	case err := <-failure:
		t.Fatalf("concurrent sentinel descriptor was closed: %v", err)
	default:
	}
}

func TestComponentBundleRejectsUndeclaredEmptyDirectory(t *testing.T) {
	root := trustedTestRoot(t)
	componentRoot := filepath.Join(root, "component")
	if err := os.Mkdir(componentRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(componentRoot, "extra"), 0o700); err != nil {
		t.Fatal(err)
	}
	declared := []byte("x")
	writePrivate(t, filepath.Join(componentRoot, "declared.bin"), declared, 0o400)
	manifest := ComponentManifest{Files: []ComponentFile{{Path: "declared.bin", ByteLength: 1, SHA256: hashBytes(declared)}}}
	if _, err := loadComponentBundle(componentRoot, manifest); err == nil {
		t.Fatal("accepted undeclared empty directory")
	}
}

func TestComponentFilesAlwaysStageReadOnly(t *testing.T) {
	root := trustedTestRoot(t)
	base := filepath.Join(root, "runtime")
	if err := os.Mkdir(base, 0o700); err != nil {
		t.Fatal(err)
	}
	profile := ParsedProfile{Profile: Profile{HelperSHA256: hashBytes([]byte("helper"))}}
	staged, err := stageAuthenticated(base, profile, []byte("helper"), nil, map[string]componentResource{"tool.bin": {bytes: []byte("component")}})
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Close()
	info, err := os.Stat(filepath.Join(staged.RuntimeRoot, "component", "tool.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o400 {
		t.Fatalf("component mode=%#o, want 0400", info.Mode().Perm())
	}
}

func TestCurrentEVTETrustRootFailsBeforeResourceLocationOrStaging(t *testing.T) {
	root := trustedTestRoot(t)
	profilePath := filepath.Join(root, "evte.json")
	raw := []byte(`{"component_manifest_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","endpoint_profile_sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","environment":"EVTE","expires_at":"2026-08-22T00:00:00Z","helper_sha256":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","issued_at":"2026-08-21T00:00:00Z","registration_manifest_sha256":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd","schema_version":1,"target":"darwin/arm64"}`)
	writePrivate(t, profilePath, raw, 0o600)
	writePrivate(t, profileSignaturePath(profilePath), []byte("not-used-currently"), 0o600)
	locator := &countingLocator{}
	_, err := AuthenticateAndStage(context.Background(), profilePath, locator, testNow)
	if err == nil || err.Error() != "SBR_EVTE_TRUST_ROOT_UNREGISTERED" {
		t.Fatalf("error=%v", err)
	}
	if locator.calls != 0 {
		t.Fatalf("locator calls=%d", locator.calls)
	}
}

func TestComponentProfileHashMismatchReturnsBeforeBundleRootOpen(t *testing.T) {
	component := ParsedComponent{Manifest: ComponentManifest{Files: []ComponentFile{{Path: "file", ByteLength: 1, SHA256: hashBytes([]byte("x"))}}}, SHA256: strings.Repeat("a", 64)}
	profile := ParsedProfile{Profile: Profile{ComponentManifestSHA256: strings.Repeat("b", 64)}}
	_, err := loadProfileBoundComponent(context.Background(), filepath.Join(trustedTestRoot(t), "must-not-open"), profile, component)
	if err == nil || !strings.Contains(err.Error(), "COMPONENT_HASH_MISMATCH") {
		t.Fatalf("error=%v", err)
	}
}

func TestComponentBundleRetainedTraversalRejectsHostileTrees(t *testing.T) {
	t.Run("symlink root", func(t *testing.T) {
		root := trustedTestRoot(t)
		bundle := filepath.Join(root, "bundle")
		actual := filepath.Join(root, "actual")
		if err := os.Mkdir(actual, 0o700); err != nil {
			t.Fatal(err)
		}
		data := []byte("ok")
		writePrivate(t, filepath.Join(actual, "file.bin"), data, 0o400)
		if err := os.Symlink(actual, bundle); err != nil {
			t.Fatal(err)
		}
		if _, err := loadComponentBundle(bundle, ComponentManifest{Files: []ComponentFile{{Path: "file.bin", ByteLength: 2, SHA256: hashBytes(data)}}}); err == nil {
			t.Fatal("accepted symlink component root")
		}
	})
	t.Run("valid nested", func(t *testing.T) {
		root := trustedTestRoot(t)
		bundle := filepath.Join(root, "bundle")
		if err := os.MkdirAll(filepath.Join(bundle, "nested"), 0o700); err != nil {
			t.Fatal(err)
		}
		data := []byte("ok")
		writePrivate(t, filepath.Join(bundle, "nested", "file.bin"), data, 0o400)
		loaded, err := loadComponentBundle(bundle, ComponentManifest{Files: []ComponentFile{{Path: "nested/file.bin", ByteLength: int64(len(data)), SHA256: hashBytes(data)}}})
		if err != nil || !bytes.Equal(loaded["nested/file.bin"].bytes, data) {
			t.Fatalf("loaded=%v error=%v", loaded, err)
		}
	})
	t.Run("valid dot-prefixed names", func(t *testing.T) {
		root := trustedTestRoot(t)
		bundle := filepath.Join(root, "bundle")
		if err := os.MkdirAll(filepath.Join(bundle, ".well-known"), 0o700); err != nil {
			t.Fatal(err)
		}
		files := map[string][]byte{".hidden": []byte("hidden"), ".well-known/config": []byte("config")}
		manifest := ComponentManifest{}
		for _, relative := range []string{".hidden", ".well-known/config"} {
			writePrivate(t, filepath.Join(bundle, relative), files[relative], 0o400)
			manifest.Files = append(manifest.Files, ComponentFile{Path: relative, ByteLength: int64(len(files[relative])), SHA256: hashBytes(files[relative])})
		}
		loaded, err := loadComponentBundle(bundle, manifest)
		if err != nil || len(loaded) != len(files) {
			t.Fatalf("loaded=%v error=%v", loaded, err)
		}
	})
	t.Run("writable nested directory", func(t *testing.T) {
		root := trustedTestRoot(t)
		bundle := filepath.Join(root, "bundle")
		nested := filepath.Join(bundle, "nested")
		if err := os.MkdirAll(nested, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(nested, 0o720); err != nil {
			t.Fatal(err)
		}
		data := []byte("ok")
		writePrivate(t, filepath.Join(nested, "file.bin"), data, 0o400)
		if _, err := loadComponentBundle(bundle, ComponentManifest{Files: []ComponentFile{{Path: "nested/file.bin", ByteLength: 2, SHA256: hashBytes(data)}}}); err == nil {
			t.Fatal("accepted writable component directory")
		}
	})
	t.Run("hardlink alias", func(t *testing.T) {
		root := trustedTestRoot(t)
		bundle := filepath.Join(root, "bundle")
		if err := os.Mkdir(bundle, 0o700); err != nil {
			t.Fatal(err)
		}
		data := []byte("same")
		first := filepath.Join(bundle, "a")
		writePrivate(t, first, data, 0o400)
		if err := os.Link(first, filepath.Join(bundle, "b")); err != nil {
			t.Fatal(err)
		}
		hash := hashBytes(data)
		_, err := loadComponentBundle(bundle, ComponentManifest{Files: []ComponentFile{{Path: "a", ByteLength: 4, SHA256: hash}, {Path: "b", ByteLength: 4, SHA256: hash}}})
		if err == nil {
			t.Fatal("accepted hardlinks")
		}
	})
	for name, setup := range map[string]func(*testing.T, string) ComponentManifest{
		"extra file": func(t *testing.T, b string) ComponentManifest {
			data := []byte("ok")
			writePrivate(t, filepath.Join(b, "declared"), data, 0o400)
			writePrivate(t, filepath.Join(b, "extra"), data, 0o400)
			return ComponentManifest{Files: []ComponentFile{{Path: "declared", ByteLength: 2, SHA256: hashBytes(data)}}}
		},
		"hidden": func(t *testing.T, b string) ComponentManifest {
			data := []byte("ok")
			writePrivate(t, filepath.Join(b, "declared"), data, 0o400)
			writePrivate(t, filepath.Join(b, ".hidden"), data, 0o400)
			return ComponentManifest{Files: []ComponentFile{{Path: "declared", ByteLength: 2, SHA256: hashBytes(data)}}}
		},
		"symlink": func(t *testing.T, b string) ComponentManifest {
			data := []byte("ok")
			outside := filepath.Join(filepath.Dir(b), "outside")
			writePrivate(t, outside, data, 0o400)
			if err := os.Symlink(outside, filepath.Join(b, "declared")); err != nil {
				t.Fatal(err)
			}
			return ComponentManifest{Files: []ComponentFile{{Path: "declared", ByteLength: 2, SHA256: hashBytes(data)}}}
		},
		"writable": func(t *testing.T, b string) ComponentManifest {
			data := []byte("ok")
			writePrivate(t, filepath.Join(b, "declared"), data, 0o620)
			return ComponentManifest{Files: []ComponentFile{{Path: "declared", ByteLength: 2, SHA256: hashBytes(data)}}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := trustedTestRoot(t)
			bundle := filepath.Join(root, "bundle")
			if err := os.Mkdir(bundle, 0o700); err != nil {
				t.Fatal(err)
			}
			manifest := setup(t, bundle)
			if _, err := loadComponentBundle(bundle, manifest); err == nil {
				t.Fatal("accepted hostile tree")
			}
		})
	}
}

func TestComponentBundleEnforcesEntryAndDepthBounds(t *testing.T) {
	t.Run("depth", func(t *testing.T) {
		for _, testCase := range []struct {
			name        string
			directories int
			wantError   bool
		}{{name: "fifteen directories", directories: 15}, {name: "sixteen directories", directories: 16, wantError: true}} {
			t.Run(testCase.name, func(t *testing.T) {
				root := trustedTestRoot(t)
				bundle := filepath.Join(root, "bundle")
				deep := strings.Repeat("d/", testCase.directories) + "file"
				if err := os.MkdirAll(filepath.Dir(filepath.Join(bundle, deep)), 0o700); err != nil {
					t.Fatal(err)
				}
				data := []byte("x")
				writePrivate(t, filepath.Join(bundle, deep), data, 0o400)
				_, err := loadComponentBundle(bundle, ComponentManifest{Files: []ComponentFile{{Path: deep, ByteLength: 1, SHA256: hashBytes(data)}}})
				if (err != nil) != testCase.wantError {
					t.Fatalf("directories=%d error=%v", testCase.directories, err)
				}
			})
		}
	})
	t.Run("entries", func(t *testing.T) {
		root := trustedTestRoot(t)
		bundle := filepath.Join(root, "bundle")
		data := []byte("x")
		files := make([]ComponentFile, 0, MaxComponentFiles)
		for index := 0; index < MaxComponentFiles; index++ {
			relative := fmt.Sprintf("common/d%03d/file", index)
			if err := os.MkdirAll(filepath.Dir(filepath.Join(bundle, relative)), 0o700); err != nil {
				t.Fatal(err)
			}
			writePrivate(t, filepath.Join(bundle, relative), data, 0o400)
			files = append(files, ComponentFile{Path: relative, ByteLength: 1, SHA256: hashBytes(data)})
		}
		if _, err := loadComponentBundle(bundle, ComponentManifest{Files: files}); err == nil {
			t.Fatal("accepted excessive entries")
		}
	})
}

func TestStagedCleanupIsDescriptorRelativeScopedAndIdempotent(t *testing.T) {
	root := trustedTestRoot(t)
	base := filepath.Join(root, "runtime")
	if err := os.Mkdir(base, 0o700); err != nil {
		t.Fatal(err)
	}
	profile := ParsedProfile{Profile: Profile{HelperSHA256: hashBytes([]byte("helper"))}}
	staged, err := stageAuthenticated(base, profile, []byte("helper"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	unexpected := filepath.Join(staged.RuntimeRoot, "external-entry")
	writePrivate(t, unexpected, []byte("external"), 0o400)
	first := staged.Close()
	second := staged.Close()
	if first == nil || second == nil || first.Error() != second.Error() {
		t.Fatalf("close errors=%v %v", first, second)
	}
	if _, err := os.Stat(unexpected); err != nil {
		t.Fatalf("unexpected entry deleted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(staged.RuntimeRoot, "sbr-helper")); !os.IsNotExist(err) {
		t.Fatal("owned helper retained")
	}
}

func TestStagingFailureRemovesOnlyItsRuntimeRoot(t *testing.T) {
	root := trustedTestRoot(t)
	base := filepath.Join(root, "runtime")
	if err := os.Mkdir(base, 0o700); err != nil {
		t.Fatal(err)
	}
	profile := ParsedProfile{Profile: Profile{HelperSHA256: hashBytes([]byte("helper"))}}
	_, err := stageAuthenticated(base, profile, []byte("helper"), nil, map[string]componentResource{
		"collision":      {bytes: []byte("file")},
		"collision/file": {bytes: []byte("nested")},
	})
	if err == nil {
		t.Fatal("conflicting component tree staged")
	}
	entries, readErr := os.ReadDir(base)
	if readErr != nil || len(entries) != 0 {
		t.Fatalf("failed stage retained runtime entries: %v %v", entries, readErr)
	}
}

func TestFinalStagingValidationUsesCallerContextAndCleansUpOnCancellation(t *testing.T) {
	root := trustedTestRoot(t)
	base := filepath.Join(root, "runtime")
	if err := os.Mkdir(base, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	profile := ParsedProfile{Profile: Profile{HelperSHA256: hashBytes([]byte("helper"))}}
	started := time.Now()
	_, err := stageAuthenticatedContextWithFinalValidation(ctx, base, profile, []byte("helper"), nil, nil, cancel)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("final validation error=%v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("final validation cancellation was not prompt: %s", elapsed)
	}
	entries, readErr := os.ReadDir(base)
	if readErr != nil || len(entries) != 0 {
		t.Fatalf("canceled final validation leaked runtime root: %v %v", entries, readErr)
	}
}

func TestRuntimeProfileExclusiveDescriptorRejectsHelperPathSwaps(t *testing.T) {
	root := trustedTestRoot(t)
	base := filepath.Join(root, "runtime")
	if err := os.Mkdir(base, 0o700); err != nil {
		t.Fatal(err)
	}
	helper := []byte("trusted-helper")
	profile := ParsedProfile{Profile: Profile{HelperSHA256: hashBytes(helper)}}
	staged, err := stageAuthenticated(base, profile, helper, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(root, "external")
	writePrivate(t, external, []byte("external"), 0o600)
	if err = os.Symlink(external, filepath.Join(staged.RuntimeRoot, "sandbox.sb")); err != nil {
		t.Fatal(err)
	}
	if _, err = staged.CreatePrivateRuntimeFile("sandbox.sb", []byte("profile")); err == nil {
		t.Fatal("accepted precreated profile symlink")
	}
	payload, err := os.ReadFile(external)
	if err != nil || string(payload) != "external" {
		t.Fatal("profile precreation changed external target")
	}
	if err = os.Remove(filepath.Join(staged.RuntimeRoot, "sandbox.sb")); err != nil {
		t.Fatal(err)
	}
	originalPath := staged.HelperPath
	renamed := filepath.Join(staged.RuntimeRoot, "renamed-helper")
	if err = os.Rename(originalPath, renamed); err != nil {
		t.Fatal(err)
	}
	writePrivate(t, originalPath, []byte("malicious"), 0o500)
	if err = staged.Revalidate(); err == nil {
		t.Fatal("helper pathname swap retained launch authority")
	}
	if _, err = staged.OpenHelperExecutable(); err == nil {
		t.Fatal("helper pathname swap produced executable descriptor")
	}
	if err = staged.Close(); err == nil {
		t.Fatal("cleanup ignored unexpected renamed inode")
	}
	if _, err = os.Stat(renamed); err != nil {
		t.Fatal("unexpected renamed inode deleted")
	}
}

func TestWriteFullyAndSyncHandlesPartialWritesAndFsyncFailure(t *testing.T) {
	t.Run("partial writes", func(t *testing.T) {
		var written []byte
		synced := false
		err := writeFullyAndSync([]byte("abcdef"), func(value []byte) (int, error) {
			limit := 2
			if len(value) < limit {
				limit = len(value)
			}
			written = append(written, value[:limit]...)
			return limit, nil
		}, func() error {
			synced = true
			return nil
		})
		if err != nil || string(written) != "abcdef" || !synced {
			t.Fatalf("written=%q synced=%t error=%v", written, synced, err)
		}
	})
	t.Run("zero progress", func(t *testing.T) {
		synced := false
		err := writeFullyAndSync([]byte("x"), func([]byte) (int, error) { return 0, nil }, func() error {
			synced = true
			return nil
		})
		if err == nil || synced {
			t.Fatalf("synced=%t error=%v", synced, err)
		}
	})
	t.Run("fsync failure", func(t *testing.T) {
		expected := errors.New("fsync failure")
		err := writeFullyAndSync([]byte("x"), func(value []byte) (int, error) { return len(value), nil }, func() error { return expected })
		if !errors.Is(err, expected) {
			t.Fatalf("error=%v", err)
		}
	})
}

func TestWriteFullyAndSyncStopsAtContextDeadlineBeforeFsync(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	writes := 0
	synced := false
	err := writeFullyAndSyncContext(ctx, bytes.Repeat([]byte("x"), 128<<10), func(value []byte) (int, error) {
		writes++
		cancel()
		return len(value), nil
	}, func() error {
		synced = true
		return nil
	})
	if !errors.Is(err, context.Canceled) || writes != 1 || synced {
		t.Fatalf("writes=%d synced=%t error=%v", writes, synced, err)
	}
}

func hashBytes(value []byte) string { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }

func writeSignedSimulatorProfile(t *testing.T, path string, helper []byte) {
	t.Helper()
	sum := sha256.Sum256(helper)
	raw := []byte(`{"component_manifest_sha256":"NONE","endpoint_profile_sha256":"NONE","environment":"SIMULATOR","expires_at":"2026-08-22T00:00:00Z","helper_sha256":"` + hex.EncodeToString(sum[:]) + `","issued_at":"2026-08-21T00:00:00Z","registration_manifest_sha256":"NONE","schema_version":1,"target":"darwin/arm64"}`)
	parsed, err := ParseProfile(raw, testNow)
	if err != nil {
		t.Fatal(err)
	}
	signature := append([]byte(base64.StdEncoding.EncodeToString(ed25519.Sign(ed25519.NewKeyFromSeed(testSeed), parsed.Canonical))), '\n')
	writePrivate(t, path, raw, 0o600)
	writePrivate(t, profileSignaturePath(path), signature, 0o600)
}
func writePrivate(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func trustedTestRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp(repositoryRoot(t), ".sbrprofile-test-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	return root
}
