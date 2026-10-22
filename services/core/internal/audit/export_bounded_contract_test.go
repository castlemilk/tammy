//go:build !tammy_sqlcipher

package audit

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestIndexStoredEvidenceArchivePreflightsAndReturnsZeroCopyStoreSlices(t *testing.T) {
	archiveBytes, err := writeDeterministicZIP(map[string][]byte{
		"events/00000000000000000001.json": []byte("first-event"),
		"manifest.json":                    []byte("manifest"),
	})
	if err != nil {
		t.Fatalf("write archive: %v", err)
	}

	indexed, err := indexStoredEvidenceArchive(archiveBytes)
	if err != nil {
		t.Fatalf("index archive: %v", err)
	}
	want := []byte("first-event")
	got := indexed["events/00000000000000000001.json"]
	if !bytes.Equal(got, want) {
		t.Fatalf("indexed event = %q, want %q", got, want)
	}

	dataOffset := storedZIPMemberDataOffset(t, archiveBytes, "events/00000000000000000001.json")
	archiveBytes[dataOffset] ^= 0xff
	if got[0] != archiveBytes[dataOffset] {
		t.Fatal("indexed Store member does not alias the original archive bytes")
	}
}

func TestIndexStoredEvidenceArchiveRejectsCRCMismatch(t *testing.T) {
	archiveBytes, err := writeDeterministicZIP(map[string][]byte{
		"manifest.json": []byte("manifest"),
	})
	if err != nil {
		t.Fatalf("write archive: %v", err)
	}
	dataOffset := storedZIPMemberDataOffset(t, archiveBytes, "manifest.json")
	archiveBytes[dataOffset] ^= 0xff

	if _, err := indexStoredEvidenceArchive(archiveBytes); !errors.Is(err, ErrEvidenceArchive) {
		t.Fatalf("index corrupted archive error = %v, want ErrEvidenceArchive", err)
	}
}

func TestIndexStoredEvidenceArchivePreflightsAggregateBeforeMemberOffsets(t *testing.T) {
	members := make(map[string][]byte, 9)
	for index := 0; index < 9; index++ {
		members[fmt.Sprintf("objects/%02d.bin", index)] = []byte{byte(index)}
	}
	archiveBytes, err := writeDeterministicZIP(members)
	if err != nil {
		t.Fatalf("write archive: %v", err)
	}
	forceZIPCentralDirectoryMemberSizes(t, archiveBytes, uint32(maxEvidenceArchiveMember))

	if _, err := indexStoredEvidenceArchive(archiveBytes); !errors.Is(err, ErrEvidenceArchive) {
		t.Fatalf("index oversized aggregate error = %v, want ErrEvidenceArchive", err)
	}
}

func TestIndexStoredEvidenceArchiveRejectsInvalidCentralDirectoryDataOffset(t *testing.T) {
	archiveBytes, err := writeDeterministicZIP(map[string][]byte{
		"manifest.json": []byte("manifest"),
	})
	if err != nil {
		t.Fatalf("write archive: %v", err)
	}
	centralOffset := storedZIPCentralDirectoryOffset(t, archiveBytes)
	if !bytes.Equal(archiveBytes[centralOffset:centralOffset+4], []byte("PK\x01\x02")) {
		t.Fatalf("central directory signature at %d is invalid", centralOffset)
	}
	binary.LittleEndian.PutUint32(archiveBytes[centralOffset+42:centralOffset+46], uint32(len(archiveBytes)+1))

	if _, err := indexStoredEvidenceArchive(archiveBytes); !errors.Is(err, ErrEvidenceArchive) {
		t.Fatalf("index invalid data offset error = %v, want ErrEvidenceArchive", err)
	}
}

func TestWalkExactJSONLLinesRejectsDelimiterStormBeforeVisitingAndAliasesInput(t *testing.T) {
	storm := bytes.Repeat([]byte{'\n'}, 1<<20)
	visits := 0
	err := walkExactJSONLLines(storm, 1, func(uint64, []byte) error {
		visits++
		return nil
	})
	if !errors.Is(err, ErrEvidenceArchive) {
		t.Fatalf("delimiter storm error = %v, want ErrEvidenceArchive", err)
	}
	if visits != 0 {
		t.Fatalf("delimiter storm visits = %d, want 0", visits)
	}

	encoded := []byte("first\nsecond\n")
	var lines [][]byte
	if err := walkExactJSONLLines(encoded, 2, func(_ uint64, line []byte) error {
		lines = append(lines, line)
		return nil
	}); err != nil {
		t.Fatalf("walk valid JSONL: %v", err)
	}
	if len(lines) != 2 || !bytes.Equal(lines[0], []byte("first")) || !bytes.Equal(lines[1], []byte("second")) {
		t.Fatalf("walked lines = %q, want [first second]", lines)
	}
	encoded[0] = 'F'
	encoded[len("first\n")] = 'S'
	if lines[0][0] != 'F' || lines[1][0] != 'S' {
		t.Fatal("walked lines do not alias the input member")
	}
}

func TestPreflightEvidenceObjectsRejectsLimitsPathsAndDuplicatesBeforeCopy(t *testing.T) {
	shared := make([]byte, maxEvidenceArchiveMember)
	oversizedAggregate := make([]EvidenceObject, 9)
	for index := range oversizedAggregate {
		oversizedAggregate[index] = EvidenceObject{
			Path:  fmt.Sprintf("objects/%02d.bin", index),
			Bytes: shared,
		}
	}

	tests := []struct {
		name          string
		objects       []EvidenceObject
		reservedCount int
		reservedBytes uint64
	}{
		{
			name: "unsafe path",
			objects: []EvidenceObject{
				{Path: "../secret", Bytes: []byte("secret")},
			},
		},
		{
			name: "reserved path",
			objects: []EvidenceObject{
				{Path: "manifest.json", Bytes: []byte("shadow")},
			},
		},
		{
			name: "duplicate path",
			objects: []EvidenceObject{
				{Path: "objects/value.bin", Bytes: []byte("one")},
				{Path: "objects/value.bin", Bytes: []byte("two")},
			},
		},
		{
			name: "member too large",
			objects: []EvidenceObject{
				{Path: "objects/value.bin", Bytes: make([]byte, maxEvidenceArchiveMember+1)},
			},
		},
		{
			name:          "member count including reserved members",
			objects:       []EvidenceObject{{Path: "objects/value.bin"}},
			reservedCount: maxEvidenceArchiveMembers,
		},
		{
			name:          "negative reserved member count",
			objects:       []EvidenceObject{{Path: "objects/value.bin"}},
			reservedCount: -1,
		},
		{
			name:    "aggregate size with shared backing slice",
			objects: oversizedAggregate,
		},
		{
			name:          "reserved bytes exceed archive budget",
			objects:       []EvidenceObject{{Path: "objects/value.bin"}},
			reservedBytes: maxEvidenceArchiveBytes + 1,
		},
		{
			name: "checked aggregate overflow from reserved bytes",
			objects: []EvidenceObject{
				{Path: "objects/value.bin", Bytes: []byte("value")},
			},
			reservedBytes: ^uint64(0) - 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := preflightEvidenceObjects(test.objects, test.reservedCount, test.reservedBytes); !errors.Is(err, ErrEvidenceArchive) {
				t.Fatalf("preflight error = %v, want ErrEvidenceArchive", err)
			}
		})
	}
}

func TestPreflightEvidenceObjectsAcceptsValidObjectsWithinReservedBudget(t *testing.T) {
	objects := []EvidenceObject{
		{Path: "objects/alpha.bin", Bytes: []byte("alpha")},
		{Path: "objects/beta.bin", Bytes: []byte("beta")},
	}
	if err := preflightEvidenceObjects(objects, 5, 1024); err != nil {
		t.Fatalf("preflight valid objects: %v", err)
	}
}

func TestEvidenceMemberRegistryZIPMatchesDeterministicWriter(t *testing.T) {
	original, _ := buildEvidenceArchiveFixtureWithKey(t)
	members, err := indexStoredEvidenceArchive(original)
	if err != nil {
		t.Fatal(err)
	}
	want, err := writeDeterministicZIP(members)
	if err != nil {
		t.Fatal(err)
	}

	for _, fileBacked := range []bool{false, true} {
		name := "byte sources"
		if fileBacked {
			name = "one file source"
		}
		t.Run(name, func(t *testing.T) {
			registry := newEvidenceMemberRegistry()
			fileMember := "public-key.ed25519"
			for path, content := range members {
				if fileBacked && path == fileMember {
					filePath := filepath.Join(t.TempDir(), "member.bin")
					if err := os.WriteFile(filePath, content, 0o600); err != nil {
						t.Fatal(err)
					}
					if err := registry.addFile(path, filePath, uint64(len(content)), sha256.Sum256(content)); err != nil {
						t.Fatal(err)
					}
					continue
				}
				if err := registry.addBytes(path, content); err != nil {
					t.Fatal(err)
				}
			}
			got, err := writeDeterministicZIPFromRegistry(registry)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatal("member-source ZIP differs from writeDeterministicZIP")
			}
			if _, err := indexStoredEvidenceArchive(got); err != nil {
				t.Fatalf("index rebuilt archive: %v", err)
			}
			if _, err := VerifyEvidenceArchive(got); err != nil {
				t.Fatalf("verify rebuilt archive: %v", err)
			}
		})
	}
}

func TestEvidenceMemberRegistryRejectsMutatedByteSourceBeforeZIPWrite(t *testing.T) {
	content := []byte("immutable member")
	registry := newEvidenceMemberRegistry()
	if err := registry.addBytes("objects/member.bin", content); err != nil {
		t.Fatal(err)
	}
	content[0] ^= 0xff
	if _, err := writeDeterministicZIPFromRegistry(registry); !errors.Is(err, ErrEvidenceArchive) {
		t.Fatalf("mutated byte source error=%v, want ErrEvidenceArchive", err)
	}
}

func storedZIPMemberDataOffset(t *testing.T, archiveBytes []byte, path string) int {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(archiveBytes), int64(len(archiveBytes)))
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	for _, file := range reader.File {
		if file.Name != path {
			continue
		}
		offset, err := file.DataOffset()
		if err != nil {
			t.Fatalf("data offset for %q: %v", path, err)
		}
		return int(offset)
	}
	t.Fatalf("archive member %q not found", path)
	return 0
}

func storedZIPCentralDirectoryOffset(t *testing.T, archiveBytes []byte) int {
	t.Helper()
	eocd := bytes.LastIndex(archiveBytes, []byte("PK\x05\x06"))
	if eocd < 0 || eocd+20 > len(archiveBytes) {
		t.Fatal("end of central directory not found")
	}
	return int(binary.LittleEndian.Uint32(archiveBytes[eocd+16 : eocd+20]))
}

func forceZIPCentralDirectoryMemberSizes(t *testing.T, archiveBytes []byte, size uint32) {
	t.Helper()
	offset := storedZIPCentralDirectoryOffset(t, archiveBytes)
	for offset+46 <= len(archiveBytes) && bytes.Equal(archiveBytes[offset:offset+4], []byte("PK\x01\x02")) {
		binary.LittleEndian.PutUint32(archiveBytes[offset+20:offset+24], size)
		binary.LittleEndian.PutUint32(archiveBytes[offset+24:offset+28], size)
		nameBytes := int(binary.LittleEndian.Uint16(archiveBytes[offset+28 : offset+30]))
		extraBytes := int(binary.LittleEndian.Uint16(archiveBytes[offset+30 : offset+32]))
		commentBytes := int(binary.LittleEndian.Uint16(archiveBytes[offset+32 : offset+34]))
		offset += 46 + nameBytes + extraBytes + commentBytes
	}
}
