package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

func TestExternalOpeningAccumulatorBoundsChunksFanInAndReturnsEarliestDuplicateCheckpoint(t *testing.T) {
	root := t.TempDir()
	accumulator, err := newExternalOpeningAccumulator(externalOpeningAccumulatorConfig{
		Context: context.Background(), ParentDirectory: root, RecordLimit: 50_000, ChunkRecords: 37, MergeFanIn: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	rawOpening := append([]byte(nil), accumulatorOpenings(1).HiddenMetadataBlinding...)
	var duplicateHead [sha256.Size]byte
	for sequence := uint64(1); sequence <= 10_000; sequence++ {
		openings := accumulatorOpenings(sequence)
		if sequence == 7_000 {
			openings.HiddenMetadataBlinding = append([]byte(nil), accumulatorOpenings(10).HiddenMetadataBlinding...)
		}
		headBefore := sha256.Sum256([]byte{byte(sequence >> 8), byte(sequence)})
		if sequence == 7_000 {
			duplicateHead = headBefore
		}
		if err := accumulator.Add(sequence, headBefore, openings); err != nil {
			t.Fatalf("Add(%d): %v", sequence, err)
		}
	}
	inspectSpills := func() error {
		return filepath.WalkDir(accumulator.directory, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil || entry.IsDir() {
				return walkErr
			}
			info, statErr := entry.Info()
			if statErr != nil {
				return statErr
			}
			if info.Mode().Perm() != 0o600 {
				t.Fatalf("spill mode=%04o for %s", info.Mode().Perm(), path)
			}
			contents, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			if bytes.Contains(contents, rawOpening) {
				t.Fatalf("raw commitment opening leaked to %s", path)
			}
			return nil
		})
	}
	if err := inspectSpills(); err != nil {
		t.Fatal(err)
	}
	sequence, headBefore, err := accumulator.FirstDuplicate()
	if err != nil {
		t.Fatal(err)
	}
	if accumulator.maxChunkRecordsObserved == 0 || accumulator.maxChunkRecordsObserved > 37 {
		t.Fatalf("chunk records=%d, limit=37", accumulator.maxChunkRecordsObserved)
	}
	if accumulator.maxMergeFanInObserved == 0 || accumulator.maxMergeFanInObserved > 3 {
		t.Fatalf("merge fan-in=%d, limit=3", accumulator.maxMergeFanInObserved)
	}
	if err := inspectSpills(); err != nil {
		t.Fatal(err)
	}
	if sequence != 7_000 || headBefore != duplicateHead {
		t.Fatalf("duplicate=(%d,%x), want (7000,%x)", sequence, headBefore, duplicateHead)
	}
	directory := accumulator.directory
	if err := accumulator.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("spill directory remains after Close: %v", err)
	}
}

func TestExternalOpeningAccumulatorCancellationAndFailuresCleanEveryTemporary(t *testing.T) {
	injected := errors.New("injected accumulator operation failure")
	tests := []struct {
		name   string
		hooks  externalOpeningAccumulatorHooks
		cancel bool
		stage  string
	}{
		{name: "create", stage: "create", hooks: externalOpeningAccumulatorHooks{
			CreateTemp: func(string, string) (*os.File, error) { return nil, injected },
		}},
		{name: "write", stage: "add", hooks: externalOpeningAccumulatorHooks{
			WriteAll: func(io.Writer, []byte) error { return injected },
		}},
		{name: "read", stage: "finish", hooks: externalOpeningAccumulatorHooks{
			ReadFull: func(io.Reader, []byte) (int, error) { return 0, injected },
		}},
		{name: "close", stage: "finish", hooks: externalOpeningAccumulatorHooks{
			Close: func(file *os.File) error { _ = file.Close(); return injected },
		}},
		{name: "cancel", stage: "finish", cancel: true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			accumulator, err := newExternalOpeningAccumulator(externalOpeningAccumulatorConfig{
				Context: ctx, ParentDirectory: root, RecordLimit: 5, ChunkRecords: 2, MergeFanIn: 2, Hooks: testCase.hooks,
			})
			if testCase.stage == "create" {
				if !errors.Is(err, injected) {
					t.Fatalf("create error=%v, want injected", err)
				}
				assertDirectoryEmpty(t, root)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			addErr := accumulator.Add(1, sha256.Sum256([]byte("head")), accumulatorOpenings(1))
			if testCase.stage == "add" {
				if !errors.Is(addErr, injected) {
					t.Fatalf("Add error=%v, want injected", addErr)
				}
				_ = accumulator.Close()
				assertDirectoryEmpty(t, root)
				return
			}
			if addErr != nil {
				t.Fatal(addErr)
			}
			if testCase.cancel {
				cancel()
			}
			_, _, finishErr := accumulator.FirstDuplicate()
			if testCase.cancel {
				if !errors.Is(finishErr, context.Canceled) {
					t.Fatalf("finish error=%v, want context.Canceled", finishErr)
				}
			} else if !errors.Is(finishErr, injected) {
				t.Fatalf("finish error=%v, want injected", finishErr)
			}
			assertDirectoryEmpty(t, root)
		})
	}
}

func TestExternalOpeningAccumulatorRejectsLimitBeforeWriteAndFailureIsSticky(t *testing.T) {
	root := t.TempDir()
	writes := 0
	accumulator, err := newExternalOpeningAccumulator(externalOpeningAccumulatorConfig{
		Context: context.Background(), ParentDirectory: root, RecordLimit: 5, ChunkRecords: 2, MergeFanIn: 2,
		Hooks: externalOpeningAccumulatorHooks{WriteAll: func(writer io.Writer, value []byte) error {
			writes++
			return writeAll(writer, value)
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := accumulator.Add(1, sha256.Sum256([]byte("head-1")), accumulatorOpenings(1)); err != nil {
		t.Fatal(err)
	}
	if writes != 5 {
		t.Fatalf("writes=%d, want 5", writes)
	}
	limitErr := accumulator.Add(2, sha256.Sum256([]byte("head-2")), accumulatorOpenings(2))
	if !errors.Is(limitErr, ErrRepository) {
		t.Fatalf("limit error=%v, want ErrRepository", limitErr)
	}
	if writes != 5 {
		t.Fatalf("limit performed a partial write; writes=%d", writes)
	}
	if err := accumulator.Add(3, sha256.Sum256([]byte("head-3")), accumulatorOpenings(3)); !errors.Is(err, limitErr) {
		t.Fatalf("repeated Add error=%v, want sticky %v", err, limitErr)
	}
	if _, _, err := accumulator.FirstDuplicate(); !errors.Is(err, limitErr) {
		t.Fatalf("FirstDuplicate error=%v, want sticky %v", err, limitErr)
	}
	if err := accumulator.Close(); err != nil {
		t.Fatal(err)
	}
	if err := accumulator.Close(); err != nil {
		t.Fatalf("second Close error=%v", err)
	}
	assertDirectoryEmpty(t, root)
}

func accumulatorOpenings(sequence uint64) *tammyv1.AuditCommitmentOpenings {
	values := make([][]byte, 5)
	for index := range values {
		digest := sha256.Sum256([]byte{byte(index + 1), byte(sequence >> 24), byte(sequence >> 16), byte(sequence >> 8), byte(sequence)})
		values[index] = append([]byte(nil), digest[:]...)
	}
	return &tammyv1.AuditCommitmentOpenings{
		HiddenMetadataBlinding: values[0], PayloadIdentityBlinding: values[1], EventTypeBlinding: values[2],
		OccurredAtBlinding: values[3], ActorUserIdBlinding: values[4],
	}
}

func assertDirectoryEmpty(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary residue=%v", entries)
	}
}
