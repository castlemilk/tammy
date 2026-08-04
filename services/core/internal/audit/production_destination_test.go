package audit

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestApprovedDestinationPublishesRestrictiveFileThroughOpaqueCapability(t *testing.T) {
	base := t.TempDir()
	registry, err := NewApprovedDestinationRegistry(ApprovedDestinationConfig{
		BaseDirectory: base,
		Capacity:      2,
		NewID: func() (string, error) {
			return "01890f60-4d6d-7c12-8f02-6c9129d5b081", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })

	reference, err := registry.Approve("evidence.zip")
	if err != nil {
		t.Fatal(err)
	}
	if reference != "01890f60-4d6d-7c12-8f02-6c9129d5b081" || filepath.IsAbs(reference) || reference == "evidence.zip" {
		t.Fatalf("reference=%q, want opaque canonical UUIDv7", reference)
	}
	destination, err := registry.Resolve(reference)
	if err != nil {
		t.Fatal(err)
	}
	archive := []byte("bounded archive")
	if err := destination.AtomicCommit(context.Background(), archive); err != nil {
		t.Fatal(err)
	}
	if err := destination.AtomicCommit(context.Background(), append([]byte(nil), archive...)); err != nil {
		t.Fatalf("idempotent commit: %v", err)
	}
	if err := destination.AtomicCommit(context.Background(), []byte("different archive")); !errors.Is(err, ErrApprovedDestination) {
		t.Fatalf("mismatched replay error=%v, want ErrApprovedDestination", err)
	}
	committed, err := destination.ReadCommitted(context.Background())
	if err != nil || !bytes.Equal(committed, archive) {
		t.Fatalf("committed=%q error=%v", committed, err)
	}
	info, err := os.Lstat(filepath.Join(base, "evidence.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("destination mode=%v, want regular 0600", info.Mode())
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "evidence.zip" {
		t.Fatalf("destination entries=%v, want committed file only", entries)
	}
}

func TestApprovedDestinationRejectsUnsafeNamesAndSymlinkTarget(t *testing.T) {
	base := t.TempDir()
	registry, err := NewApprovedDestinationRegistry(ApprovedDestinationConfig{
		BaseDirectory: base, Capacity: 16,
		NewID: func() (string, error) {
			return "01890f60-4d6d-7c12-8f02-6c9129d5b082", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	for _, name := range []string{"../escape", "/absolute", "nested/file", `nested\\file`, "nul\x00file", ".", ".."} {
		if _, err := registry.Approve(name); !errors.Is(err, ErrApprovedDestination) {
			t.Errorf("Approve(%q) error=%v, want ErrApprovedDestination", name, err)
		}
	}

	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(base, "evidence.zip")); err != nil {
		t.Fatal(err)
	}
	reference, err := registry.Approve("evidence.zip")
	if err != nil {
		t.Fatal(err)
	}
	destination, err := registry.Resolve(reference)
	if err != nil {
		t.Fatal(err)
	}
	if err := destination.AtomicCommit(context.Background(), []byte("overwrite")); !errors.Is(err, ErrApprovedDestination) {
		t.Fatalf("symlink commit error=%v, want ErrApprovedDestination", err)
	}
	contents, err := os.ReadFile(outside)
	if err != nil || string(contents) != "untouched" {
		t.Fatalf("outside contents=%q error=%v", contents, err)
	}
}

func TestApprovedDestinationCleansUpEveryPreCommitFailure(t *testing.T) {
	injected := errors.New("injected filesystem failure")
	testCases := []struct {
		name  string
		hooks func(context.CancelFunc) *destinationFSHooks
	}{
		{name: "partial write", hooks: func(context.CancelFunc) *destinationFSHooks {
			return &destinationFSHooks{writeFile: func(file *os.File, value []byte) (int, error) {
				written, _ := file.Write(value[:min(2, len(value))])
				return written, injected
			}}
		}},
		{name: "cancellation during write", hooks: func(cancel context.CancelFunc) *destinationFSHooks {
			return &destinationFSHooks{writeFile: func(file *os.File, value []byte) (int, error) {
				written, err := file.Write(value[:min(2, len(value))])
				cancel()
				return written, err
			}}
		}},
		{name: "file sync", hooks: func(context.CancelFunc) *destinationFSHooks {
			return &destinationFSHooks{syncFile: func(*os.File) error { return injected }}
		}},
		{name: "directory sync after publish", hooks: func(context.CancelFunc) *destinationFSHooks {
			return &destinationFSHooks{syncDirectory: func(*os.Root) error { return injected }}
		}},
		{name: "directory sync after temp cleanup", hooks: func(context.CancelFunc) *destinationFSHooks {
			calls := 0
			return &destinationFSHooks{syncDirectory: func(root *os.Root) error {
				calls++
				if calls == 2 {
					return injected
				}
				return syncRootDirectory(root)
			}}
		}},
		{name: "publish", hooks: func(context.CancelFunc) *destinationFSHooks {
			return &destinationFSHooks{publish: func(*os.Root, string, string) error { return injected }}
		}},
	}
	for index, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			base := t.TempDir()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			registry, err := NewApprovedDestinationRegistry(ApprovedDestinationConfig{
				BaseDirectory: base, Capacity: 1,
				NewID: func() (string, error) { return "01890f60-4d6d-7c12-8f02-6c9129d5b083", nil },
				hooks: testCase.hooks(cancel),
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = registry.Close() })
			reference, err := registry.Approve("evidence.zip")
			if err != nil {
				t.Fatal(err)
			}
			destination, err := registry.Resolve(reference)
			if err != nil {
				t.Fatal(err)
			}
			err = destination.AtomicCommit(ctx, []byte("archive bytes that must never remain partial"))
			if !errors.Is(err, ErrApprovedDestination) {
				t.Fatalf("case %d error=%v, want ErrApprovedDestination", index, err)
			}
			if testCase.name == "cancellation during write" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation error=%v, want context.Canceled", err)
			}
			entries, readErr := os.ReadDir(base)
			if readErr != nil || len(entries) != 0 {
				t.Fatalf("failure left destination artifacts=%v error=%v", entries, readErr)
			}
		})
	}
}

func TestApprovedDestinationLeavesForeignTargetAndRemovesOwnedTempAtPublishBoundary(t *testing.T) {
	base := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("publish target appeared")
	registry, err := NewApprovedDestinationRegistry(ApprovedDestinationConfig{
		BaseDirectory: base, Capacity: 1,
		NewID: func() (string, error) { return "01890f60-4d6d-7c12-8f02-6c9129d5b084", nil },
		hooks: &destinationFSHooks{publish: func(root *os.Root, _, newName string) error {
			if err := os.Symlink(outside, filepath.Join(root.Name(), newName)); err != nil {
				return err
			}
			return injected
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	reference, err := registry.Approve("evidence.zip")
	if err != nil {
		t.Fatal(err)
	}
	destination, err := registry.Resolve(reference)
	if err != nil {
		t.Fatal(err)
	}
	if err := destination.AtomicCommit(context.Background(), []byte("archive")); !errors.Is(err, ErrApprovedDestination) {
		t.Fatalf("commit error=%v, want ErrApprovedDestination", err)
	}
	entries, err := os.ReadDir(base)
	if err != nil || len(entries) != 1 || entries[0].Name() != "evidence.zip" || entries[0].Type()&os.ModeSymlink == 0 {
		t.Fatalf("TOCTOU cleanup artifacts=%v error=%v, want only untouched foreign symlink", entries, err)
	}
	contents, err := os.ReadFile(outside)
	if err != nil || string(contents) != "untouched" {
		t.Fatalf("outside bytes=%q error=%v", contents, err)
	}
}

func TestApprovedDestinationLoopsAcrossShortWrites(t *testing.T) {
	base := t.TempDir()
	writes := 0
	registry, err := NewApprovedDestinationRegistry(ApprovedDestinationConfig{
		BaseDirectory: base, Capacity: 1,
		NewID: func() (string, error) { return "01890f60-4d6d-7c12-8f02-6c9129d5b085", nil },
		hooks: &destinationFSHooks{writeFile: func(file *os.File, value []byte) (int, error) {
			writes++
			return file.Write(value[:min(2, len(value))])
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	reference, err := registry.Approve("evidence.zip")
	if err != nil {
		t.Fatal(err)
	}
	destination, err := registry.Resolve(reference)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte("complete despite short writes")
	if err := destination.AtomicCommit(context.Background(), want); err != nil {
		t.Fatal(err)
	}
	got, err := destination.ReadCommitted(context.Background())
	if err != nil || !bytes.Equal(got, want) || writes <= 1 {
		t.Fatalf("committed=%q writes=%d error=%v", got, writes, err)
	}
}

func TestApprovedDestinationRejectsSymlinkBaseAndBoundsCapabilities(t *testing.T) {
	realBase := t.TempDir()
	symlinkBase := filepath.Join(t.TempDir(), "approved-link")
	if err := os.Symlink(realBase, symlinkBase); err != nil {
		t.Fatal(err)
	}
	if _, err := NewApprovedDestinationRegistry(ApprovedDestinationConfig{
		BaseDirectory: symlinkBase, Capacity: 1,
		NewID: func() (string, error) { return "01890f60-4d6d-7c12-8f02-6c9129d5b086", nil },
	}); !errors.Is(err, ErrApprovedDestination) {
		t.Fatalf("symlink base error=%v, want ErrApprovedDestination", err)
	}

	identifiers := []string{
		"01890f60-4d6d-7c12-8f02-6c9129d5b087",
		"01890f60-4d6d-7c12-8f02-6c9129d5b087",
		"01890f60-4d6d-7c12-8f02-6c9129d5b088",
	}
	calls := 0
	registry, err := NewApprovedDestinationRegistry(ApprovedDestinationConfig{
		BaseDirectory: realBase, Capacity: 2,
		NewID: func() (string, error) {
			identifier := identifiers[calls]
			calls++
			return identifier, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	if _, err := registry.Approve("first.zip"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Approve("second.zip"); !errors.Is(err, ErrApprovedDestination) {
		t.Fatalf("duplicate capability error=%v", err)
	}
	if _, err := registry.Approve("second.zip"); err != nil {
		t.Fatalf("failed duplicate consumed approval: %v", err)
	}
	if _, err := registry.Approve("first.zip"); !errors.Is(err, ErrApprovedDestination) {
		t.Fatalf("duplicate approval name error=%v", err)
	}
	if _, err := registry.Approve("third.zip"); !errors.Is(err, ErrApprovedDestination) || calls != 3 {
		t.Fatalf("capacity error=%v generator calls=%d", err, calls)
	}
}

func TestApprovedDestinationConcurrentResolveUseAndCloseLeavesNoPartialFile(t *testing.T) {
	base := t.TempDir()
	registry, err := NewApprovedDestinationRegistry(ApprovedDestinationConfig{
		BaseDirectory: base, Capacity: 1,
		NewID: func() (string, error) { return "01890f60-4d6d-7c12-8f02-6c9129d5b089", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	reference, err := registry.Approve("evidence.zip")
	if err != nil {
		t.Fatal(err)
	}
	archive := []byte("one complete concurrent archive")
	var workers sync.WaitGroup
	errorsSeen := make(chan error, 17)
	for range 16 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			destination, resolveErr := registry.Resolve(reference)
			if resolveErr == nil {
				resolveErr = destination.AtomicCommit(context.Background(), archive)
			}
			errorsSeen <- resolveErr
		}()
	}
	workers.Add(1)
	go func() {
		defer workers.Done()
		errorsSeen <- registry.Close()
	}()
	workers.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		if err != nil && !errors.Is(err, ErrApprovedDestination) {
			t.Errorf("concurrent error=%v", err)
		}
	}
	entries, err := os.ReadDir(base)
	if err != nil || len(entries) > 1 {
		t.Fatalf("concurrent artifacts=%v error=%v", entries, err)
	}
	if len(entries) == 1 {
		contents, readErr := os.ReadFile(filepath.Join(base, entries[0].Name()))
		if readErr != nil || entries[0].Name() != "evidence.zip" || !bytes.Equal(contents, archive) {
			t.Fatalf("concurrent destination=%q contents=%q error=%v", entries[0].Name(), contents, readErr)
		}
	}
}

func TestApprovedDestinationAcceptsSafeBasenameCharacters(t *testing.T) {
	registry, err := NewApprovedDestinationRegistry(ApprovedDestinationConfig{
		BaseDirectory: t.TempDir(), Capacity: 1,
		NewID: func() (string, error) { return "01890f60-4d6d-7c12-8f02-6c9129d5b090", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	if _, err := registry.Approve("tax-export-2026.zip"); err != nil {
		t.Fatalf("safe approval name rejected: %v", err)
	}
}

func TestApprovedDestinationRejectsBaseSwapBeforeRootOpen(t *testing.T) {
	parent := t.TempDir()
	base := filepath.Join(parent, "approved")
	original := filepath.Join(parent, "approved-original")
	outside := t.TempDir()
	if err := os.Mkdir(base, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := NewApprovedDestinationRegistry(ApprovedDestinationConfig{
		BaseDirectory: base, Capacity: 1,
		NewID: func() (string, error) { return "01890f60-4d6d-7c12-8f02-6c9129d5b096", nil },
		hooks: &destinationFSHooks{openRoot: func(name string) (*os.Root, error) {
			if err := os.Rename(name, original); err != nil {
				return nil, err
			}
			if err := os.Symlink(outside, name); err != nil {
				return nil, err
			}
			return os.OpenRoot(name)
		}},
	})
	if !errors.Is(err, ErrApprovedDestination) {
		t.Fatalf("base swap error=%v, want ErrApprovedDestination", err)
	}
}
