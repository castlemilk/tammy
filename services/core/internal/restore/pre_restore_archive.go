package restore

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	"buf.build/go/protovalidate"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	PreRestoreFormatV1                = "tammy-pre-restore-v1"
	preRestoreArchiveKeySize          = 32
	preRestoreNoncePrefixSize         = 8
	preRestoreChunkSize               = 64 * 1024
	maximumPreRestoreArchiveBytes     = 256 * 1024 * 1024
	maximumPreRestoreArchiveFileBytes = maximumPreRestoreArchiveBytes + 1024*1024
	preRestoreHeaderSize              = 24 + 12 + 48 + preRestoreNoncePrefixSize + 4 + 8 + 4 + 4
)

var (
	ErrPreRestoreArchive       = errors.New("restore: invalid pre-restore archive")
	ErrPreRestoreArchiveSecret = errors.New("restore: pre-restore archive authentication failed")
	preRestoreMagic            = [24]byte{'t', 'a', 'm', 'm', 'y', '-', 'p', 'r', 'e', '-', 'r', 'e', 's', 't', 'o', 'r', 'e', '-', 'v', '1'}
)

type PreRestoreArchiveFormatInput struct {
	ArchiveID        string
	WorkspaceID      string
	SourceGeneration uint64
	CreatedAt        time.Time
	DeleteEligibleAt time.Time
	Predecessor      []byte
}

type OpenedPreRestoreArchive struct {
	Manifest    *tammyv1.PreRestoreArchiveManifest
	Predecessor []byte
}

func SealPreRestoreArchive(input PreRestoreArchiveFormatInput, restoredDEK []byte, random io.Reader) ([]byte, error) {
	if !validPreRestoreFormatInput(input) || len(restoredDEK) != preRestoreArchiveKeySize || random == nil {
		return nil, ErrPreRestoreArchive
	}
	predecessorHash := sha256.Sum256(input.Predecessor)
	manifest := &tammyv1.PreRestoreArchiveManifest{Format: PreRestoreFormatV1, ArchiveId: input.ArchiveID,
		SourceGeneration: input.SourceGeneration, CreatedAt: timestamppb.New(input.CreatedAt.UTC()),
		DeletionEligibleAt: timestamppb.New(input.DeleteEligibleAt.UTC()), PredecessorSha256: predecessorHash[:]}
	if err := validatePreRestoreManifest(manifest, input.ArchiveID); err != nil {
		return nil, err
	}
	manifestBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(manifest)
	if err != nil || len(manifestBytes) == 0 || len(manifestBytes) > 4096 ||
		len(input.Predecessor) > maximumPreRestoreArchiveBytes-4-len(manifestBytes) {
		return nil, ErrPreRestoreArchive
	}
	plaintextLength := 4 + len(manifestBytes) + len(input.Predecessor)
	chunkCount := preRestoreChunkCount(plaintextLength)
	if chunkCount == 0 {
		return nil, ErrPreRestoreArchive
	}
	archiveKey := make([]byte, preRestoreArchiveKeySize)
	wrapNonce := make([]byte, 12)
	noncePrefix := make([]byte, preRestoreNoncePrefixSize)
	if err := readPreRestoreRandom(random, archiveKey, wrapNonce, noncePrefix); err != nil {
		return nil, err
	}
	defer zeroBytes(archiveKey)
	kek, wrapAAD, err := preRestoreKEK(restoredDEK, input.WorkspaceID, input.ArchiveID)
	if err != nil {
		return nil, err
	}
	defer zeroBytes(kek)
	defer zeroBytes(wrapAAD)
	wrappedKey, err := preRestoreSealGCM(kek, wrapNonce, archiveKey, wrapAAD)
	if err != nil || len(wrappedKey) != preRestoreArchiveKeySize+16 {
		return nil, ErrPreRestoreArchive
	}
	header := encodePreRestoreHeader(wrapNonce, wrappedKey, noncePrefix, len(manifestBytes), plaintextLength, chunkCount)
	block, err := aes.NewCipher(archiveKey)
	if err != nil {
		return nil, ErrPreRestoreArchive
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrPreRestoreArchive
	}
	archive := make([]byte, 0, preRestoreHeaderSize+plaintextLength+int(chunkCount)*24)
	archive = append(archive, header...)
	for counter, offset := uint32(0), 0; counter < chunkCount; counter++ {
		end := min(offset+preRestoreChunkSize, plaintextLength)
		plaintext := preRestorePlaintextRange(manifestBytes, input.Predecessor, offset, end)
		nonce := preRestoreNonce(noncePrefix, counter)
		aad := preRestoreChunkAAD(header, counter)
		ciphertext := aead.Seal(nil, nonce, plaintext, aad)
		zeroBytes(plaintext)
		zeroBytes(aad)
		archive = binary.BigEndian.AppendUint32(archive, counter)
		archive = binary.BigEndian.AppendUint32(archive, uint32(len(ciphertext)))
		archive = append(archive, ciphertext...)
		offset = end
	}
	return archive, nil
}

func OpenPreRestoreArchive(archive, restoredDEK []byte, workspaceID, archiveID string) (*OpenedPreRestoreArchive, error) {
	if len(restoredDEK) != preRestoreArchiveKeySize || !ids.IsCanonicalV7(workspaceID) || !ids.IsCanonicalV7(archiveID) {
		return nil, ErrPreRestoreArchive
	}
	header, err := preflightPreRestoreArchive(archive)
	if err != nil {
		return nil, err
	}
	kek, wrapAAD, err := preRestoreKEK(restoredDEK, workspaceID, archiveID)
	if err != nil {
		return nil, err
	}
	defer zeroBytes(kek)
	defer zeroBytes(wrapAAD)
	archiveKey, err := preRestoreOpenGCM(kek, header.wrapNonce, header.wrappedKey, wrapAAD)
	if err != nil || len(archiveKey) != preRestoreArchiveKeySize {
		zeroBytes(archiveKey)
		return nil, ErrPreRestoreArchiveSecret
	}
	defer zeroBytes(archiveKey)
	block, err := aes.NewCipher(archiveKey)
	if err != nil {
		return nil, ErrPreRestoreArchiveSecret
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrPreRestoreArchiveSecret
	}
	plaintext := make([]byte, 0, header.plaintextLength)
	offset := preRestoreHeaderSize
	for counter := uint32(0); counter < header.chunkCount; counter++ {
		if offset > len(archive)-8 || binary.BigEndian.Uint32(archive[offset:offset+4]) != counter {
			zeroBytes(plaintext)
			return nil, ErrPreRestoreArchive
		}
		ciphertextLength := int(binary.BigEndian.Uint32(archive[offset+4 : offset+8]))
		offset += 8
		if ciphertextLength < aead.Overhead()+1 || ciphertextLength > preRestoreChunkSize+aead.Overhead() ||
			offset > len(archive)-ciphertextLength {
			zeroBytes(plaintext)
			return nil, ErrPreRestoreArchive
		}
		nonce := preRestoreNonce(header.noncePrefix, counter)
		aad := preRestoreChunkAAD(archive[:preRestoreHeaderSize], counter)
		chunk, openErr := aead.Open(nil, nonce, archive[offset:offset+ciphertextLength], aad)
		zeroBytes(aad)
		if openErr != nil {
			zeroBytes(plaintext)
			return nil, ErrPreRestoreArchiveSecret
		}
		if len(plaintext) > header.plaintextLength-len(chunk) {
			zeroBytes(chunk)
			zeroBytes(plaintext)
			return nil, ErrPreRestoreArchive
		}
		plaintext = append(plaintext, chunk...)
		zeroBytes(chunk)
		offset += ciphertextLength
	}
	if offset != len(archive) || len(plaintext) != header.plaintextLength || len(plaintext) < 4 ||
		int(binary.BigEndian.Uint32(plaintext[:4])) != header.manifestLength || header.manifestLength > len(plaintext)-4 {
		zeroBytes(plaintext)
		return nil, ErrPreRestoreArchive
	}
	manifestBytes := plaintext[4 : 4+header.manifestLength]
	manifest := &tammyv1.PreRestoreArchiveManifest{}
	if (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(manifestBytes, manifest) != nil ||
		validatePreRestoreManifest(manifest, archiveID) != nil {
		zeroBytes(plaintext)
		return nil, ErrPreRestoreArchive
	}
	canonical, _ := proto.MarshalOptions{Deterministic: true}.Marshal(manifest)
	if !bytes.Equal(canonical, manifestBytes) {
		zeroBytes(plaintext)
		return nil, ErrPreRestoreArchive
	}
	predecessor := append([]byte(nil), plaintext[4+header.manifestLength:]...)
	zeroBytes(plaintext)
	digest := sha256.Sum256(predecessor)
	if !bytes.Equal(digest[:], manifest.PredecessorSha256) {
		zeroBytes(predecessor)
		return nil, ErrPreRestoreArchiveSecret
	}
	return &OpenedPreRestoreArchive{Manifest: manifest, Predecessor: predecessor}, nil
}

type preRestoreHeader struct {
	wrapNonce       []byte
	wrappedKey      []byte
	noncePrefix     []byte
	manifestLength  int
	plaintextLength int
	chunkCount      uint32
}

func preflightPreRestoreArchive(archive []byte) (preRestoreHeader, error) {
	if len(archive) < preRestoreHeaderSize || !bytes.Equal(archive[:24], preRestoreMagic[:]) {
		return preRestoreHeader{}, ErrPreRestoreArchive
	}
	offset := 24
	header := preRestoreHeader{wrapNonce: archive[offset : offset+12]}
	offset += 12
	header.wrappedKey = archive[offset : offset+48]
	offset += 48
	header.noncePrefix = archive[offset : offset+preRestoreNoncePrefixSize]
	offset += preRestoreNoncePrefixSize
	header.manifestLength = int(binary.BigEndian.Uint32(archive[offset : offset+4]))
	offset += 4
	plaintextLength64 := binary.BigEndian.Uint64(archive[offset : offset+8])
	offset += 8
	chunkSize := binary.BigEndian.Uint32(archive[offset : offset+4])
	offset += 4
	header.chunkCount = binary.BigEndian.Uint32(archive[offset : offset+4])
	if plaintextLength64 == 0 || plaintextLength64 > maximumPreRestoreArchiveBytes ||
		plaintextLength64 > uint64(math.MaxInt) || header.manifestLength <= 0 || header.manifestLength > 4096 ||
		uint64(header.manifestLength)+4 >= plaintextLength64 || chunkSize != preRestoreChunkSize ||
		header.chunkCount == 0 || header.chunkCount != preRestoreChunkCount(int(plaintextLength64)) {
		return preRestoreHeader{}, ErrPreRestoreArchive
	}
	header.plaintextLength = int(plaintextLength64)
	minimumFrameBytes := uint64(header.chunkCount) * 8
	if uint64(len(archive)-preRestoreHeaderSize) < minimumFrameBytes+plaintextLength64+uint64(header.chunkCount)*16 {
		return preRestoreHeader{}, ErrPreRestoreArchive
	}
	return header, nil
}

func validPreRestoreFormatInput(input PreRestoreArchiveFormatInput) bool {
	return ids.IsCanonicalV7(input.ArchiveID) && ids.IsCanonicalV7(input.WorkspaceID) && input.SourceGeneration > 0 &&
		!input.CreatedAt.IsZero() && input.CreatedAt.Equal(input.CreatedAt.UTC()) &&
		!input.DeleteEligibleAt.Before(input.CreatedAt.AddDate(1, 0, 0)) && len(input.Predecessor) > 0 &&
		len(input.Predecessor) <= maximumPreRestoreArchiveBytes
}

func validatePreRestoreManifest(manifest *tammyv1.PreRestoreArchiveManifest, archiveID string) error {
	if manifest == nil || len(manifest.ProtoReflect().GetUnknown()) != 0 || manifest.Format != PreRestoreFormatV1 ||
		manifest.ArchiveId != archiveID || protovalidate.Validate(manifest) != nil || manifest.CreatedAt == nil ||
		manifest.DeletionEligibleAt == nil || !manifest.CreatedAt.IsValid() || !manifest.DeletionEligibleAt.IsValid() ||
		manifest.DeletionEligibleAt.AsTime().Before(manifest.CreatedAt.AsTime().AddDate(1, 0, 0)) {
		return ErrPreRestoreArchive
	}
	return nil
}

func preRestoreKEK(restoredDEK []byte, workspaceID, archiveID string) ([]byte, []byte, error) {
	if len(restoredDEK) != preRestoreArchiveKeySize || !ids.IsCanonicalV7(workspaceID) || !ids.IsCanonicalV7(archiveID) {
		return nil, nil, ErrPreRestoreArchive
	}
	aad := []byte("tammy.pre-restore.archive-key-wrap.v1\x00" + workspaceID + "\x00" + archiveID)
	kek, err := hkdf.Key(sha256.New, restoredDEK, nil, string(aad), preRestoreArchiveKeySize)
	if err != nil {
		zeroBytes(aad)
		return nil, nil, ErrPreRestoreArchive
	}
	return kek, aad, nil
}

func preRestoreSealGCM(key, nonce, plaintext, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil || len(nonce) != aead.NonceSize() {
		return nil, ErrPreRestoreArchive
	}
	return aead.Seal(nil, nonce, plaintext, aad), nil
}

func preRestoreOpenGCM(key, nonce, ciphertext, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil || len(nonce) != aead.NonceSize() {
		return nil, ErrPreRestoreArchive
	}
	return aead.Open(nil, nonce, ciphertext, aad)
}

func encodePreRestoreHeader(wrapNonce, wrappedKey, noncePrefix []byte, manifestLength, plaintextLength int, chunkCount uint32) []byte {
	header := make([]byte, 0, preRestoreHeaderSize)
	header = append(header, preRestoreMagic[:]...)
	header = append(header, wrapNonce...)
	header = append(header, wrappedKey...)
	header = append(header, noncePrefix...)
	header = binary.BigEndian.AppendUint32(header, uint32(manifestLength))
	header = binary.BigEndian.AppendUint64(header, uint64(plaintextLength))
	header = binary.BigEndian.AppendUint32(header, preRestoreChunkSize)
	header = binary.BigEndian.AppendUint32(header, chunkCount)
	return header
}

func preRestoreChunkCount(length int) uint32 {
	if length <= 0 || length > maximumPreRestoreArchiveBytes {
		return 0
	}
	return uint32((uint64(length) + preRestoreChunkSize - 1) / preRestoreChunkSize)
}

func preRestoreNonce(prefix []byte, counter uint32) []byte {
	nonce := make([]byte, 12)
	copy(nonce, prefix)
	binary.BigEndian.PutUint32(nonce[8:], counter)
	return nonce
}

func preRestoreChunkAAD(header []byte, counter uint32) []byte {
	aad := make([]byte, 0, len(header)+4)
	aad = append(aad, header...)
	aad = binary.BigEndian.AppendUint32(aad, counter)
	return aad
}

func preRestorePlaintextRange(manifest, predecessor []byte, start, end int) []byte {
	result := make([]byte, end-start)
	for index := range result {
		position := start + index
		switch {
		case position < 4:
			var length [4]byte
			binary.BigEndian.PutUint32(length[:], uint32(len(manifest)))
			result[index] = length[position]
		case position < 4+len(manifest):
			result[index] = manifest[position-4]
		default:
			result[index] = predecessor[position-4-len(manifest)]
		}
	}
	return result
}

func readPreRestoreRandom(random io.Reader, values ...[]byte) error {
	for _, value := range values {
		if _, err := io.ReadFull(random, value); err != nil {
			for _, owned := range values {
				zeroBytes(owned)
			}
			return fmt.Errorf("%w: random source", ErrPreRestoreArchive)
		}
	}
	return nil
}
