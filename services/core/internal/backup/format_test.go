package backup

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

func TestTammyBackupV1RoundTrip(t *testing.T) {
	seed := sha256.Sum256([]byte("tammy backup signing key test seed"))
	privateKey := ed25519.NewKeyFromSeed(seed[:])
	input := ArchiveInput{
		WorkspaceID:           "018f0000-0000-7000-8000-000000000001",
		SchemaVersion:         3,
		AppVersion:            "0.1.0",
		AuditGeneration:       1,
		AuditSequence:         2,
		AuditHead:             bytes.Repeat([]byte{0x42}, sha256.Size),
		AuditRoot:             bytes.Repeat([]byte{0x43}, sha256.Size),
		SigningKeyID:          "018f0000-0000-7000-8000-000000000002",
		SigningKeyEpoch:       1,
		WorkspaceHeaderHash:   bytes.Repeat([]byte{0x44}, sha256.Size),
		MigrationManifestHash: bytes.Repeat([]byte{0x45}, sha256.Size),
		Objects: []Object{
			{Path: "database/workspace.db", Provider: "workspace", ProviderVersion: 1, Bytes: []byte("consistent online SQLCipher snapshot")},
			{Path: "workspace/header.pb", Provider: "workspace", ProviderVersion: 1, Bytes: []byte("header")},
		},
	}

	archive, err := Seal(input, []byte("correct horse battery staple"), privateKey, bytes.NewReader(bytes.Repeat([]byte{0x7a}, 128)))
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	opened, err := Open(archive, []byte("correct horse battery staple"), TrustAnchor{
		WorkspaceID: input.WorkspaceID, AuditGeneration: input.AuditGeneration, AuditRoot: input.AuditRoot,
		SigningKeyID: input.SigningKeyID, SigningKeyEpoch: input.SigningKeyEpoch,
		PublicKey: privateKey.Public().(ed25519.PublicKey),
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if opened.Manifest.Format != FormatV1 {
		t.Fatalf("format = %q, want %q", opened.Manifest.Format, FormatV1)
	}
	if opened.Manifest.WorkspaceId != input.WorkspaceID || opened.Manifest.SchemaVersion != input.SchemaVersion ||
		opened.Manifest.AppVersion != input.AppVersion || opened.Manifest.AuditGeneration != input.AuditGeneration ||
		opened.Manifest.AuditSequence != input.AuditSequence || !bytes.Equal(opened.Manifest.AuditHead, input.AuditHead) ||
		!bytes.Equal(opened.Manifest.AuditRoot, input.AuditRoot) || opened.Manifest.SigningKeyId != input.SigningKeyID {
		t.Fatalf("manifest metadata = %#v, want input metadata", opened.Manifest)
	}
	if len(opened.Objects) != len(input.Objects) {
		t.Fatalf("objects = %d, want %d", len(opened.Objects), len(input.Objects))
	}
	for index := range input.Objects {
		if opened.Objects[index].Path != input.Objects[index].Path || !bytes.Equal(opened.Objects[index].Bytes, input.Objects[index].Bytes) {
			t.Fatalf("object %d = %#v, want %#v", index, opened.Objects[index], input.Objects[index])
		}
	}
}

func TestArchiveManifestMetadataPreflightDoesNotAllocateFromClaimedLength(t *testing.T) {
	allocations := testing.AllocsPerRun(10, func() {
		for index := 0; index < maximumArchiveObjects; index++ {
			if validateObjectMetadata("providers/accounting/ledger.pb", "accounting_core", 1, maximumArchivePlaintext+1) {
				t.Fatal("oversized claimed object length was accepted")
			}
		}
	})
	if allocations != 0 {
		t.Fatalf("metadata preflight allocations = %v, want 0", allocations)
	}
}

func TestNormalizeObjectsRejectsAggregateBeforeAnyByteCopy(t *testing.T) {
	for _, test := range []struct {
		name    string
		objects []Object
		length  func(Object) uint64
	}{
		{name: "aggregate", objects: []Object{
			{Path: "rules/one.pb", Provider: "rules", ProviderVersion: 1, Bytes: []byte("one")},
			{Path: "rules/two.pb", Provider: "rules", ProviderVersion: 1, Bytes: []byte("two")},
		}, length: func(Object) uint64 { return uint64(maximumArchivePlaintext/2 + 1) }},
		{name: "duplicate", objects: []Object{
			{Path: "rules/same.pb", Provider: "rules", ProviderVersion: 1, Bytes: []byte("one")},
			{Path: "rules/same.pb", Provider: "rules", ProviderVersion: 1, Bytes: []byte("two")},
		}},
		{name: "invalid_metadata", objects: []Object{
			{Path: "../escape.pb", Provider: "rules", ProviderVersion: 1, Bytes: []byte("one")},
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			clones := 0
			_, _, _, err := normalizeObjectsWithHooks(test.objects, &objectCloneHooks{
				objectByteLength: test.length,
				cloneBytes: func(value []byte) []byte {
					clones++
					return append([]byte(nil), value...)
				},
			})
			if !errors.Is(err, ErrArchiveFormat) || clones != 0 {
				t.Fatalf("normalize error=%v clones=%d", err, clones)
			}
		})
	}
}

func TestArchiveRejectsWrongSigningLineageEpoch(t *testing.T) {
	seed := sha256.Sum256([]byte("tammy backup epoch signing key"))
	privateKey := ed25519.NewKeyFromSeed(seed[:])
	input := ArchiveInput{
		WorkspaceID:           "018f0000-0000-7000-8000-000000000001",
		SchemaVersion:         3,
		AppVersion:            "0.1.0",
		AuditGeneration:       2,
		AuditSequence:         7,
		AuditHead:             bytes.Repeat([]byte{0x42}, sha256.Size),
		AuditRoot:             bytes.Repeat([]byte{0x43}, sha256.Size),
		SigningKeyID:          "018f0000-0000-7000-8000-000000000002",
		SigningKeyEpoch:       4,
		WorkspaceHeaderHash:   bytes.Repeat([]byte{0x44}, sha256.Size),
		MigrationManifestHash: bytes.Repeat([]byte{0x45}, sha256.Size),
		Objects:               []Object{{Path: "database/workspace.db", Provider: "workspace", ProviderVersion: 1, Bytes: []byte("snapshot")}},
	}
	archive, err := Seal(input, []byte("correct horse battery staple"), privateKey, bytes.NewReader(bytes.Repeat([]byte{0x6b}, 128)))
	if err != nil {
		t.Fatal(err)
	}
	_, err = Open(archive, []byte("correct horse battery staple"), TrustAnchor{
		WorkspaceID: input.WorkspaceID, AuditGeneration: input.AuditGeneration, AuditRoot: input.AuditRoot,
		SigningKeyID: input.SigningKeyID, SigningKeyEpoch: input.SigningKeyEpoch + 1,
		PublicKey: privateKey.Public().(ed25519.PublicKey),
	})
	if !errors.Is(err, ErrArchiveFormat) {
		t.Fatalf("wrong signing lineage epoch error = %v, want ErrArchiveFormat", err)
	}
}

func TestArchiveHostileFrameAndTrustMatrix(t *testing.T) {
	input, archive, privateKey, passphrase := hostileArchiveFixture(t)
	trust := TrustAnchor{
		WorkspaceID: input.WorkspaceID, AuditGeneration: input.AuditGeneration, AuditRoot: append([]byte(nil), input.AuditRoot...),
		SigningKeyID: input.SigningKeyID, SigningKeyEpoch: input.SigningKeyEpoch,
		PublicKey: append(ed25519.PublicKey(nil), privateKey.Public().(ed25519.PublicKey)...),
	}

	t.Run("truncated", func(t *testing.T) {
		assertFormatPreflightFailure(t, archive[:len(archive)-1], trust)
	})
	t.Run("trailing_bytes", func(t *testing.T) {
		assertFormatPreflightFailure(t, append(append([]byte(nil), archive...), 0), trust)
	})
	t.Run("reordered_chunks", func(t *testing.T) {
		mutated := append([]byte(nil), archive...)
		firstEnd := archiveHeaderSize + archiveChunkFrameSize + int(binary.BigEndian.Uint32(mutated[archiveHeaderSize+8:]))
		first := append([]byte(nil), mutated[archiveHeaderSize:firstEnd]...)
		second := append([]byte(nil), mutated[firstEnd:]...)
		mutated = append(mutated[:archiveHeaderSize], second...)
		mutated = append(mutated, first...)
		assertFormatPreflightFailure(t, mutated, trust)
	})
	t.Run("duplicate_counter", func(t *testing.T) {
		mutated := append([]byte(nil), archive...)
		firstEnd := archiveHeaderSize + archiveChunkFrameSize + int(binary.BigEndian.Uint32(mutated[archiveHeaderSize+8:]))
		binary.BigEndian.PutUint32(mutated[firstEnd:firstEnd+4], 0)
		assertFormatPreflightFailure(t, mutated, trust)
	})
	t.Run("incorrect_counter", func(t *testing.T) {
		mutated := append([]byte(nil), archive...)
		binary.BigEndian.PutUint32(mutated[archiveHeaderSize:archiveHeaderSize+4], ^uint32(0))
		assertFormatPreflightFailure(t, mutated, trust)
	})
	t.Run("kdf_memory_parameter_bomb", func(t *testing.T) {
		mutated := append([]byte(nil), archive...)
		binary.BigEndian.PutUint32(mutated[16:20], ^uint32(0))
		assertFormatPreflightFailure(t, mutated, trust)
	})
	t.Run("kdf_iteration_parameter_bomb", func(t *testing.T) {
		mutated := append([]byte(nil), archive...)
		binary.BigEndian.PutUint32(mutated[20:24], ^uint32(0))
		assertFormatPreflightFailure(t, mutated, trust)
	})
	t.Run("chunk_size_overflow", func(t *testing.T) {
		mutated := append([]byte(nil), archive...)
		binary.BigEndian.PutUint32(mutated[archiveHeaderSize-16:archiveHeaderSize-12], ^uint32(0))
		assertFormatPreflightFailure(t, mutated, trust)
	})
	t.Run("plaintext_length_overflow", func(t *testing.T) {
		mutated := append([]byte(nil), archive...)
		binary.BigEndian.PutUint64(mutated[archiveHeaderSize-12:archiveHeaderSize-4], ^uint64(0))
		assertFormatPreflightFailure(t, mutated, trust)
	})
	t.Run("chunk_count_and_nonce_counter_overflow", func(t *testing.T) {
		mutated := append([]byte(nil), archive...)
		binary.BigEndian.PutUint32(mutated[archiveHeaderSize-4:archiveHeaderSize], ^uint32(0))
		assertFormatPreflightFailure(t, mutated, trust)
	})
	t.Run("frame_ciphertext_length_overflow", func(t *testing.T) {
		mutated := append([]byte(nil), archive...)
		binary.BigEndian.PutUint32(mutated[archiveHeaderSize+8:archiveHeaderSize+12], ^uint32(0))
		assertFormatPreflightFailure(t, mutated, trust)
	})

	for name, mutate := range map[string]func(*TrustAnchor){
		"wrong_workspace":  func(anchor *TrustAnchor) { anchor.WorkspaceID = "018f0000-0000-7000-8000-000000000099" },
		"wrong_generation": func(anchor *TrustAnchor) { anchor.AuditGeneration++ },
		"wrong_root":       func(anchor *TrustAnchor) { anchor.AuditRoot = bytes.Repeat([]byte{0x99}, sha256.Size) },
		"wrong_key_id":     func(anchor *TrustAnchor) { anchor.SigningKeyID = "018f0000-0000-7000-8000-000000000099" },
		"wrong_epoch":      func(anchor *TrustAnchor) { anchor.SigningKeyEpoch++ },
		"wrong_public_key": func(anchor *TrustAnchor) {
			seed := sha256.Sum256([]byte("different signing key"))
			anchor.PublicKey = ed25519.NewKeyFromSeed(seed[:]).Public().(ed25519.PublicKey)
		},
	} {
		t.Run(name, func(t *testing.T) {
			wrong := trust
			wrong.AuditRoot = append([]byte(nil), trust.AuditRoot...)
			wrong.PublicKey = append(ed25519.PublicKey(nil), trust.PublicKey...)
			mutate(&wrong)
			if _, err := Open(archive, passphrase, wrong); !errors.Is(err, ErrArchiveFormat) {
				t.Fatalf("Open() error = %v, want ErrArchiveFormat", err)
			}
		})
	}
}

func TestArchiveRejectsTamperedCanonicalPayloadAndPaths(t *testing.T) {
	input, archive, privateKey, passphrase := hostileArchiveFixture(t)
	trust := TrustAnchor{
		WorkspaceID: input.WorkspaceID, AuditGeneration: input.AuditGeneration, AuditRoot: input.AuditRoot,
		SigningKeyID: input.SigningKeyID, SigningKeyEpoch: input.SigningKeyEpoch,
		PublicKey: privateKey.Public().(ed25519.PublicKey),
	}

	for name, mutate := range map[string]func([]byte) []byte{
		"manifest": func(payload []byte) []byte {
			payload[manifestLengthPrefixSize+1] ^= 0x01
			return payload
		},
		"signature": func(payload []byte) []byte {
			manifestLength := int(binary.BigEndian.Uint32(payload[:4]))
			payload[4+manifestLength] ^= 0x01
			return payload
		},
		"object": func(payload []byte) []byte {
			manifestLength := int(binary.BigEndian.Uint32(payload[:4]))
			payload[4+manifestLength+manifestSignatureSize] ^= 0x01
			return payload
		},
		"unknown_manifest_field": func(payload []byte) []byte {
			return appendManifestEncoding(payload, []byte{0xf8, 0x07, 0x01})
		},
		"noncanonical_duplicate_format_field": func(payload []byte) []byte {
			duplicate := append([]byte{0x0a, byte(len(FormatV1))}, []byte(FormatV1)...)
			return appendManifestEncoding(payload, duplicate)
		},
	} {
		t.Run(name, func(t *testing.T) {
			mutated := rewriteArchivePayload(t, archive, passphrase, mutate)
			if _, err := Open(mutated, passphrase, trust); !errors.Is(err, ErrArchiveFormat) {
				t.Fatalf("Open() error = %v, want ErrArchiveFormat", err)
			}
		})
	}

	t.Run("duplicate_paths", func(t *testing.T) {
		duplicate := input
		duplicate.Objects = append(append([]Object(nil), input.Objects...), input.Objects[0])
		if _, err := Seal(duplicate, passphrase, privateKey, bytes.NewReader(bytes.Repeat([]byte{0x72}, 128))); !errors.Is(err, ErrArchiveFormat) {
			t.Fatalf("Seal() error = %v, want ErrArchiveFormat", err)
		}
	})
	for _, unsafePath := range []string{"/absolute.db", "database/../vault", "workspace/session.pb", "evidence/rpc-material.pb", "logs/core.log", "database\\workspace.db"} {
		t.Run("unsafe_path_"+unsafePath, func(t *testing.T) {
			unsafe := input
			unsafe.Objects = append([]Object(nil), input.Objects...)
			unsafe.Objects[0].Path = unsafePath
			if _, err := Seal(unsafe, passphrase, privateKey, bytes.NewReader(bytes.Repeat([]byte{0x73}, 128))); !errors.Is(err, ErrArchiveFormat) {
				t.Fatalf("Seal(%q) error = %v, want ErrArchiveFormat", unsafePath, err)
			}
		})
	}
}

func TestArchiveDeterministicObjectOrderingAndRandomFailure(t *testing.T) {
	input, _, privateKey, passphrase := hostileArchiveFixture(t)
	input.Objects[0], input.Objects[1] = input.Objects[1], input.Objects[0]
	archive, err := Seal(input, passphrase, privateKey, bytes.NewReader(bytes.Repeat([]byte{0x74}, 128)))
	if err != nil {
		t.Fatal(err)
	}
	opened, err := Open(archive, passphrase, TrustAnchor{
		WorkspaceID: input.WorkspaceID, AuditGeneration: input.AuditGeneration, AuditRoot: input.AuditRoot,
		SigningKeyID: input.SigningKeyID, SigningKeyEpoch: input.SigningKeyEpoch,
		PublicKey: privateKey.Public().(ed25519.PublicKey),
	})
	if err != nil {
		t.Fatal(err)
	}
	if opened.Objects[0].Path >= opened.Objects[1].Path {
		t.Fatalf("objects not sorted: %q, %q", opened.Objects[0].Path, opened.Objects[1].Path)
	}
	secret := []byte("correct horse battery staple")
	if archive, err := Seal(input, secret, privateKey, io.LimitReader(bytes.NewReader(bytes.Repeat([]byte{0x75}, 8)), 8)); err == nil || archive != nil || bytes.Contains([]byte(err.Error()), secret) {
		t.Fatalf("short random source = (%d bytes, %v)", len(archive), err)
	}
}

func hostileArchiveFixture(t *testing.T) (ArchiveInput, []byte, ed25519.PrivateKey, []byte) {
	t.Helper()
	seed := sha256.Sum256([]byte("hostile archive fixture signing key"))
	privateKey := ed25519.NewKeyFromSeed(seed[:])
	input := ArchiveInput{
		WorkspaceID:           "018f0000-0000-7000-8000-000000000001",
		SchemaVersion:         3,
		AppVersion:            "0.1.0",
		AuditGeneration:       2,
		AuditSequence:         7,
		AuditHead:             bytes.Repeat([]byte{0x42}, sha256.Size),
		AuditRoot:             bytes.Repeat([]byte{0x43}, sha256.Size),
		SigningKeyID:          "018f0000-0000-7000-8000-000000000002",
		SigningKeyEpoch:       4,
		WorkspaceHeaderHash:   bytes.Repeat([]byte{0x44}, sha256.Size),
		MigrationManifestHash: bytes.Repeat([]byte{0x45}, sha256.Size),
		Objects: []Object{
			{Path: "database/workspace.db", Provider: "workspace", ProviderVersion: 1, Bytes: bytes.Repeat([]byte{0x61}, archiveChunkSize+128)},
			{Path: "rules/current.pb", Provider: "rules", ProviderVersion: 2, Bytes: []byte("current rules")},
		},
	}
	passphrase := []byte("correct horse battery staple")
	archive, err := Seal(input, passphrase, privateKey, bytes.NewReader(bytes.Repeat([]byte{0x71}, 128)))
	if err != nil {
		t.Fatal(err)
	}
	return input, archive, privateKey, passphrase
}

func assertFormatPreflightFailure(t *testing.T, archive []byte, trust TrustAnchor) {
	t.Helper()
	if _, err := Open(archive, []byte("short"), trust); !errors.Is(err, ErrArchiveFormat) {
		t.Fatalf("preflight Open() error = %v, want ErrArchiveFormat before password KDF", err)
	}
}

func rewriteArchivePayload(t *testing.T, archive, passphrase []byte, mutate func([]byte) []byte) []byte {
	t.Helper()
	header, frames, err := preflightArchive(archive)
	if err != nil {
		t.Fatal(err)
	}
	kek, err := deriveBackupKEK(passphrase, header.salt)
	if err != nil {
		t.Fatal(err)
	}
	defer zero(kek)
	archiveKey, err := openGCM(kek, header.wrapNonce, header.wrappedKey, archiveKeyWrapAAD)
	if err != nil {
		t.Fatal(err)
	}
	defer zero(archiveKey)
	block, err := aes.NewCipher(archiveKey)
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 0, header.plaintextLength)
	for _, frame := range frames {
		plain, err := aead.Open(nil, chunkNonce(header.noncePrefix, frame.counter), frame.ciphertext,
			chunkAAD(archive[:archiveHeaderSize], frame.counter, frame.plaintextLength))
		if err != nil {
			t.Fatal(err)
		}
		payload = append(payload, plain...)
	}
	payload = mutate(payload)
	chunkCount := chunkCountFor(len(payload))
	encodedHeader := encodeArchiveHeader(header.salt, header.wrapNonce, header.wrappedKey, header.noncePrefix, len(payload), chunkCount)
	result := append([]byte(nil), encodedHeader...)
	for counter := uint32(0); counter < chunkCount; counter++ {
		start := int(counter) * archiveChunkSize
		end := min(start+archiveChunkSize, len(payload))
		ciphertext := aead.Seal(nil, chunkNonce(header.noncePrefix, counter), payload[start:end],
			chunkAAD(encodedHeader, counter, uint32(end-start)))
		result = binary.BigEndian.AppendUint32(result, counter)
		result = binary.BigEndian.AppendUint32(result, uint32(end-start))
		result = binary.BigEndian.AppendUint32(result, uint32(len(ciphertext)))
		result = append(result, ciphertext...)
	}
	zero(payload)
	return result
}

func appendManifestEncoding(payload, suffix []byte) []byte {
	manifestLength := int(binary.BigEndian.Uint32(payload[:4]))
	result := make([]byte, 4, len(payload)+len(suffix))
	binary.BigEndian.PutUint32(result, uint32(manifestLength+len(suffix)))
	result = append(result, payload[4:4+manifestLength]...)
	result = append(result, suffix...)
	result = append(result, payload[4+manifestLength:]...)
	return result
}
