// Package backup owns the authenticated encrypted workspace archive format.
package backup

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"buf.build/go/protovalidate"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
	"google.golang.org/protobuf/proto"
)

const (
	FormatV1                 = "tammy-backup-v1"
	archiveChunkSize         = 64 * 1024
	maximumArchivePlaintext  = 256 * 1024 * 1024
	maximumArchiveObjects    = 10_000
	archiveKeySize           = 32
	backupKDFSaltSize        = 16
	backupKDFMemoryKiB       = 64 * 1024
	backupKDFIterations      = 3
	backupKDFParallelism     = 1
	archiveNoncePrefixSize   = 8
	archiveHeaderSize        = 16 + 4 + 4 + 1 + backupKDFSaltSize + 12 + 48 + archiveNoncePrefixSize + 4 + 8 + 4
	archiveChunkFrameSize    = 12
	manifestSignatureSize    = ed25519.SignatureSize
	manifestLengthPrefixSize = 4
)

var (
	ErrArchiveFormat = errors.New("backup: invalid archive")
	ErrArchiveSecret = errors.New("backup: archive secret rejected")
)

var (
	archiveMagic            = [16]byte{'t', 'a', 'm', 'm', 'y', '-', 'b', 'a', 'c', 'k', 'u', 'p', '-', 'v', '1', 0}
	manifestSignatureDomain = []byte("tammy.backup.manifest.signature.v1\x00")
	archiveKeyWrapAAD       = []byte("tammy.backup.archive-key-wrap.v1\x00")
	objectProviderPattern   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
)

// Object is one immutable module-owned backup projection.
type Object struct {
	Path            string
	Provider        string
	ProviderVersion uint32
	Bytes           []byte
}

// ArchiveInput is the trusted state used to build one signed manifest.
type ArchiveInput struct {
	WorkspaceID           string
	SchemaVersion         uint64
	AppVersion            string
	AuditGeneration       uint64
	AuditSequence         uint64
	AuditHead             []byte
	AuditRoot             []byte
	SigningKeyID          string
	SigningKeyEpoch       uint64
	WorkspaceHeaderHash   []byte
	MigrationManifestHash []byte
	Objects               []Object
}

// TrustAnchor is resolved from trusted active-workspace audit lineage, never
// from public-key material embedded in the archive itself.
type TrustAnchor struct {
	WorkspaceID     string
	AuditGeneration uint64
	AuditRoot       []byte
	SigningKeyID    string
	SigningKeyEpoch uint64
	PublicKey       ed25519.PublicKey
}

// OpenedArchive contains verified metadata and owned plaintext objects.
type OpenedArchive struct {
	Manifest     *tammyv1.BackupArchiveManifest
	ManifestHash []byte
	Objects      []Object
}

// Seal serializes a deterministic protobuf manifest, signs it with the
// workspace audit key, and encrypts the payload with an independently random
// archive key wrapped by the backup passphrase.
func Seal(input ArchiveInput, passphrase []byte, signingKey ed25519.PrivateKey, random io.Reader) ([]byte, error) {
	if len(signingKey) != ed25519.PrivateKeySize {
		return nil, ErrArchiveFormat
	}
	return sealWithSigner(input, passphrase, func(message []byte) ([]byte, error) {
		return ed25519.Sign(signingKey, message), nil
	}, random)
}

func sealWithSigner(input ArchiveInput, passphrase []byte, sign func([]byte) ([]byte, error), random io.Reader) ([]byte, error) {
	if random == nil || sign == nil {
		return nil, ErrArchiveFormat
	}
	objects, manifestObjects, objectBytes, err := normalizeObjects(input.Objects)
	if err != nil {
		return nil, err
	}
	manifest := &tammyv1.BackupArchiveManifest{
		Format: FormatV1, WorkspaceId: input.WorkspaceID, SchemaVersion: input.SchemaVersion,
		AppVersion: input.AppVersion, AuditGeneration: input.AuditGeneration, AuditSequence: input.AuditSequence,
		AuditHead: append([]byte(nil), input.AuditHead...), AuditRoot: append([]byte(nil), input.AuditRoot...),
		SigningKeyId: input.SigningKeyID, SigningKeyEpoch: input.SigningKeyEpoch,
		WorkspaceHeaderHash:   append([]byte(nil), input.WorkspaceHeaderHash...),
		MigrationManifestHash: append([]byte(nil), input.MigrationManifestHash...), Objects: manifestObjects,
	}
	if err := validateManifest(manifest); err != nil {
		return nil, err
	}
	manifestBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(manifest)
	if err != nil || len(manifestBytes) == 0 || len(manifestBytes) > maximumArchivePlaintext {
		return nil, ErrArchiveFormat
	}
	signatureInput := make([]byte, 0, len(manifestSignatureDomain)+len(manifestBytes))
	signatureInput = append(signatureInput, manifestSignatureDomain...)
	signatureInput = append(signatureInput, manifestBytes...)
	signature, err := sign(signatureInput)
	zero(signatureInput)
	if err != nil || len(signature) != manifestSignatureSize {
		zero(signature)
		return nil, ErrArchiveFormat
	}

	payloadLength, ok := checkedPayloadLength(len(manifestBytes), objectBytes)
	if !ok {
		return nil, ErrArchiveFormat
	}
	payload := make([]byte, 0, payloadLength)
	payload = binary.BigEndian.AppendUint32(payload, uint32(len(manifestBytes)))
	payload = append(payload, manifestBytes...)
	payload = append(payload, signature...)
	for _, object := range objects {
		payload = append(payload, object.Bytes...)
	}
	if len(payload) != payloadLength {
		zero(payload)
		return nil, ErrArchiveFormat
	}
	defer zero(payload)

	archiveKey := make([]byte, archiveKeySize)
	salt := make([]byte, backupKDFSaltSize)
	wrapNonce := make([]byte, 12)
	noncePrefix := make([]byte, archiveNoncePrefixSize)
	if err := readRandom(random, archiveKey, salt, wrapNonce, noncePrefix); err != nil {
		zero(archiveKey)
		return nil, err
	}
	defer zero(archiveKey)
	kek, err := deriveBackupKEK(passphrase, salt)
	if err != nil {
		return nil, err
	}
	defer zero(kek)
	wrappedKey, err := sealGCM(kek, wrapNonce, archiveKey, archiveKeyWrapAAD)
	if err != nil || len(wrappedKey) != archiveKeySize+16 {
		return nil, ErrArchiveFormat
	}
	block, err := aes.NewCipher(archiveKey)
	if err != nil {
		return nil, ErrArchiveFormat
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrArchiveFormat
	}
	chunkCount := chunkCountFor(len(payload))
	if chunkCount == 0 {
		return nil, ErrArchiveFormat
	}
	header := encodeArchiveHeader(salt, wrapNonce, wrappedKey, noncePrefix, len(payload), chunkCount)
	archive := make([]byte, 0, len(header)+len(payload)+int(chunkCount)*(archiveChunkFrameSize+aead.Overhead()))
	archive = append(archive, header...)
	for counter := uint32(0); counter < chunkCount; counter++ {
		start := int(counter) * archiveChunkSize
		end := min(start+archiveChunkSize, len(payload))
		nonce := chunkNonce(noncePrefix, counter)
		aad := chunkAAD(header, counter, uint32(end-start))
		ciphertext := aead.Seal(nil, nonce, payload[start:end], aad)
		archive = binary.BigEndian.AppendUint32(archive, counter)
		archive = binary.BigEndian.AppendUint32(archive, uint32(end-start))
		archive = binary.BigEndian.AppendUint32(archive, uint32(len(ciphertext)))
		archive = append(archive, ciphertext...)
	}
	return archive, nil
}

// Open performs all cheap structural and KDF-bound checks before deriving a
// key, then authenticates every chunk and the externally anchored signature.
func Open(archive, passphrase []byte, trust TrustAnchor) (*OpenedArchive, error) {
	header, frames, err := preflightArchive(archive)
	if err != nil {
		return nil, err
	}
	if !validTrustAnchor(trust) {
		return nil, ErrArchiveFormat
	}
	kek, err := deriveBackupKEK(passphrase, header.salt)
	if err != nil {
		return nil, ErrArchiveSecret
	}
	defer zero(kek)
	archiveKey, err := openGCM(kek, header.wrapNonce, header.wrappedKey, archiveKeyWrapAAD)
	if err != nil || len(archiveKey) != archiveKeySize {
		zero(archiveKey)
		return nil, ErrArchiveSecret
	}
	defer zero(archiveKey)
	block, err := aes.NewCipher(archiveKey)
	if err != nil {
		return nil, ErrArchiveFormat
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrArchiveFormat
	}
	payload := make([]byte, 0, header.plaintextLength)
	defer zero(payload)
	for _, frame := range frames {
		nonce := chunkNonce(header.noncePrefix, frame.counter)
		aad := chunkAAD(archive[:archiveHeaderSize], frame.counter, frame.plaintextLength)
		plaintext, openErr := aead.Open(nil, nonce, frame.ciphertext, aad)
		if openErr != nil || len(plaintext) != int(frame.plaintextLength) {
			zero(plaintext)
			return nil, ErrArchiveSecret
		}
		payload = append(payload, plaintext...)
		zero(plaintext)
	}
	if uint64(len(payload)) != header.plaintextLength {
		return nil, ErrArchiveFormat
	}
	return decodePayload(payload, trust)
}

type archiveHeader struct {
	salt            []byte
	wrapNonce       []byte
	wrappedKey      []byte
	noncePrefix     []byte
	plaintextLength uint64
	chunkCount      uint32
}

type chunkFrame struct {
	counter         uint32
	plaintextLength uint32
	ciphertext      []byte
}

func preflightArchive(archive []byte) (archiveHeader, []chunkFrame, error) {
	if len(archive) < archiveHeaderSize || !bytes.Equal(archive[:16], archiveMagic[:]) {
		return archiveHeader{}, nil, ErrArchiveFormat
	}
	offset := 16
	memory := binary.BigEndian.Uint32(archive[offset:])
	offset += 4
	iterations := binary.BigEndian.Uint32(archive[offset:])
	offset += 4
	parallelism := archive[offset]
	offset++
	if memory != backupKDFMemoryKiB || iterations != backupKDFIterations || parallelism != backupKDFParallelism {
		return archiveHeader{}, nil, ErrArchiveFormat
	}
	header := archiveHeader{
		salt: append([]byte(nil), archive[offset:offset+backupKDFSaltSize]...),
	}
	offset += backupKDFSaltSize
	header.wrapNonce = append([]byte(nil), archive[offset:offset+12]...)
	offset += 12
	header.wrappedKey = append([]byte(nil), archive[offset:offset+archiveKeySize+16]...)
	offset += archiveKeySize + 16
	header.noncePrefix = append([]byte(nil), archive[offset:offset+archiveNoncePrefixSize]...)
	offset += archiveNoncePrefixSize
	chunkSize := binary.BigEndian.Uint32(archive[offset:])
	offset += 4
	header.plaintextLength = binary.BigEndian.Uint64(archive[offset:])
	offset += 8
	header.chunkCount = binary.BigEndian.Uint32(archive[offset:])
	offset += 4
	if offset != archiveHeaderSize || chunkSize != archiveChunkSize || header.plaintextLength == 0 ||
		header.plaintextLength > maximumArchivePlaintext || header.chunkCount == 0 ||
		header.chunkCount != chunkCountFor(int(header.plaintextLength)) {
		return archiveHeader{}, nil, ErrArchiveFormat
	}
	frames := make([]chunkFrame, 0, header.chunkCount)
	for counter := uint32(0); counter < header.chunkCount; counter++ {
		if len(archive)-offset < archiveChunkFrameSize {
			return archiveHeader{}, nil, ErrArchiveFormat
		}
		encodedCounter := binary.BigEndian.Uint32(archive[offset:])
		offset += 4
		plaintextLength := binary.BigEndian.Uint32(archive[offset:])
		offset += 4
		ciphertextLength := binary.BigEndian.Uint32(archive[offset:])
		offset += 4
		expectedPlaintext := uint32(archiveChunkSize)
		if counter == header.chunkCount-1 {
			expectedPlaintext = uint32(header.plaintextLength - uint64(counter)*archiveChunkSize)
		}
		if encodedCounter != counter || plaintextLength != expectedPlaintext || ciphertextLength != plaintextLength+16 ||
			ciphertextLength > archiveChunkSize+16 || uint64(ciphertextLength) > uint64(len(archive)-offset) {
			return archiveHeader{}, nil, ErrArchiveFormat
		}
		frames = append(frames, chunkFrame{counter: counter, plaintextLength: plaintextLength,
			ciphertext: archive[offset : offset+int(ciphertextLength)]})
		offset += int(ciphertextLength)
	}
	if offset != len(archive) {
		return archiveHeader{}, nil, ErrArchiveFormat
	}
	return header, frames, nil
}

func decodePayload(payload []byte, trust TrustAnchor) (*OpenedArchive, error) {
	if len(payload) < manifestLengthPrefixSize+manifestSignatureSize {
		return nil, ErrArchiveFormat
	}
	manifestLength := uint64(binary.BigEndian.Uint32(payload[:manifestLengthPrefixSize]))
	if manifestLength == 0 || manifestLength > uint64(len(payload)-manifestLengthPrefixSize-manifestSignatureSize) {
		return nil, ErrArchiveFormat
	}
	manifestEnd := manifestLengthPrefixSize + int(manifestLength)
	manifestBytes := payload[manifestLengthPrefixSize:manifestEnd]
	signature := payload[manifestEnd : manifestEnd+manifestSignatureSize]
	manifest := new(tammyv1.BackupArchiveManifest)
	if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(manifestBytes, manifest); err != nil ||
		len(manifest.ProtoReflect().GetUnknown()) != 0 || validateManifest(manifest) != nil {
		return nil, ErrArchiveFormat
	}
	canonical, err := proto.MarshalOptions{Deterministic: true}.Marshal(manifest)
	if err != nil || !bytes.Equal(canonical, manifestBytes) {
		return nil, ErrArchiveFormat
	}
	if manifest.WorkspaceId != trust.WorkspaceID || manifest.AuditGeneration != trust.AuditGeneration ||
		manifest.SigningKeyId != trust.SigningKeyID || manifest.SigningKeyEpoch != trust.SigningKeyEpoch ||
		subtle.ConstantTimeCompare(manifest.AuditRoot, trust.AuditRoot) != 1 {
		return nil, ErrArchiveFormat
	}
	signed := make([]byte, 0, len(manifestSignatureDomain)+len(manifestBytes))
	signed = append(signed, manifestSignatureDomain...)
	signed = append(signed, manifestBytes...)
	verified := ed25519.Verify(trust.PublicKey, signed, signature)
	zero(signed)
	if !verified {
		return nil, ErrArchiveFormat
	}
	offset := manifestEnd + manifestSignatureSize
	objects := make([]Object, 0, len(manifest.Objects))
	for _, objectManifest := range manifest.Objects {
		if objectManifest.ByteLength > uint64(len(payload)-offset) {
			return nil, ErrArchiveFormat
		}
		end := offset + int(objectManifest.ByteLength)
		objectBytes := append([]byte(nil), payload[offset:end]...)
		digest := sha256.Sum256(objectBytes)
		if subtle.ConstantTimeCompare(digest[:], objectManifest.Sha256) != 1 {
			zero(objectBytes)
			for index := range objects {
				zero(objects[index].Bytes)
			}
			return nil, ErrArchiveFormat
		}
		objects = append(objects, Object{Path: objectManifest.Path, Provider: objectManifest.Provider,
			ProviderVersion: objectManifest.ProviderVersion, Bytes: objectBytes})
		offset = end
	}
	if offset != len(payload) {
		for index := range objects {
			zero(objects[index].Bytes)
		}
		return nil, ErrArchiveFormat
	}
	manifestHash := sha256.Sum256(manifestBytes)
	return &OpenedArchive{Manifest: manifest, ManifestHash: append([]byte(nil), manifestHash[:]...), Objects: objects}, nil
}

func normalizeObjects(input []Object) ([]Object, []*tammyv1.BackupArchiveObject, int, error) {
	return normalizeObjectsWithHooks(input, nil)
}

func normalizeObjectsWithHooks(input []Object, hooks *objectCloneHooks) ([]Object, []*tammyv1.BackupArchiveObject, int, error) {
	if len(input) == 0 || len(input) > maximumArchiveObjects {
		return nil, nil, 0, ErrArchiveFormat
	}
	paths := make(map[string]struct{}, len(input))
	var total uint64
	for _, object := range input {
		length := objectLengthForClone(object, hooks)
		if !validateObjectMetadata(object.Path, object.Provider, object.ProviderVersion, length) ||
			length > uint64(maximumArchivePlaintext)-total || seenPath(paths, object.Path) {
			return nil, nil, 0, ErrArchiveFormat
		}
		total += length
	}
	objects := make([]Object, len(input))
	for index, object := range input {
		objects[index] = Object{Path: object.Path, Provider: object.Provider, ProviderVersion: object.ProviderVersion,
			Bytes: cloneObjectBytes(object.Bytes, hooks)}
		if len(objects[index].Bytes) != len(object.Bytes) {
			zeroObjectBytes(objects)
			return nil, nil, 0, ErrArchiveFormat
		}
	}
	sort.Slice(objects, func(left, right int) bool { return objects[left].Path < objects[right].Path })
	manifests := make([]*tammyv1.BackupArchiveObject, len(objects))
	for index, object := range objects {
		digest := sha256.Sum256(object.Bytes)
		manifests[index] = &tammyv1.BackupArchiveObject{Path: object.Path, Sha256: append([]byte(nil), digest[:]...),
			ByteLength: uint64(len(object.Bytes)), Provider: object.Provider, ProviderVersion: object.ProviderVersion}
	}
	return objects, manifests, int(total), nil
}

func safeObject(object Object) bool {
	return validateObjectMetadata(object.Path, object.Provider, object.ProviderVersion, uint64(len(object.Bytes)))
}

func validateObjectMetadata(objectPath, provider string, providerVersion uint32, byteLength uint64) bool {
	// Reject attacker-controlled sizes before any path normalization or other
	// work which might allocate in proportion to manifest input.
	if byteLength == 0 || byteLength > maximumArchivePlaintext || providerVersion == 0 ||
		!objectProviderPattern.MatchString(provider) || objectPath == "" || len(objectPath) > 512 ||
		!utf8.ValidString(objectPath) || path.Clean(objectPath) != objectPath || strings.HasPrefix(objectPath, "/") ||
		strings.Contains(objectPath, "\\") {
		return false
	}
	root, _, _ := strings.Cut(objectPath, "/")
	switch root {
	case "database", "workspace", "evidence", "rules", "artefacts", "providers":
	default:
		return false
	}
	lower := strings.ToLower(objectPath)
	for _, excluded := range []string{"vault", "password", "remembered", "session", "rpc", "log"} {
		if strings.Contains(lower, excluded) {
			return false
		}
	}
	return true
}

func validateManifest(manifest *tammyv1.BackupArchiveManifest) error {
	if manifest == nil || len(manifest.ProtoReflect().GetUnknown()) != 0 || len(manifest.Objects) == 0 ||
		len(manifest.Objects) > maximumArchiveObjects || protovalidate.Validate(manifest) != nil {
		return ErrArchiveFormat
	}
	totalLength := uint64(0)
	for index, object := range manifest.Objects {
		if object == nil || !validateObjectMetadata(object.Path, object.Provider, object.ProviderVersion, object.ByteLength) ||
			index > 0 && manifest.Objects[index-1].Path >= object.Path {
			return ErrArchiveFormat
		}
		if object.ByteLength > maximumArchivePlaintext-totalLength {
			return ErrArchiveFormat
		}
		totalLength += object.ByteLength
	}
	return nil
}

func deriveBackupKEK(passphrase, salt []byte) ([]byte, error) {
	if len(salt) != backupKDFSaltSize {
		return nil, ErrArchiveFormat
	}
	policy, err := workspace.NewPasswordPolicy(nil, bytes.NewReader(salt))
	if err != nil {
		return nil, ErrArchiveSecret
	}
	verifier, err := policy.Hash(passphrase)
	if err != nil || verifier.MemoryKiB != backupKDFMemoryKiB || verifier.Iterations != backupKDFIterations ||
		verifier.Parallelism != backupKDFParallelism || !bytes.Equal(verifier.Salt, salt) || len(verifier.Digest) != archiveKeySize {
		zero(verifier.Digest)
		return nil, ErrArchiveSecret
	}
	return verifier.Digest, nil
}

func encodeArchiveHeader(salt, wrapNonce, wrappedKey, noncePrefix []byte, plaintextLength int, chunkCount uint32) []byte {
	header := make([]byte, 0, archiveHeaderSize)
	header = append(header, archiveMagic[:]...)
	header = binary.BigEndian.AppendUint32(header, backupKDFMemoryKiB)
	header = binary.BigEndian.AppendUint32(header, backupKDFIterations)
	header = append(header, backupKDFParallelism)
	header = append(header, salt...)
	header = append(header, wrapNonce...)
	header = append(header, wrappedKey...)
	header = append(header, noncePrefix...)
	header = binary.BigEndian.AppendUint32(header, archiveChunkSize)
	header = binary.BigEndian.AppendUint64(header, uint64(plaintextLength))
	header = binary.BigEndian.AppendUint32(header, chunkCount)
	return header
}

func checkedPayloadLength(manifestLength, objectLength int) (int, bool) {
	if manifestLength <= 0 || objectLength < 0 || manifestLength > maximumArchivePlaintext {
		return 0, false
	}
	fixed := manifestLengthPrefixSize + manifestSignatureSize
	if manifestLength > maximumArchivePlaintext-fixed || objectLength > maximumArchivePlaintext-fixed-manifestLength {
		return 0, false
	}
	return fixed + manifestLength + objectLength, true
}

func chunkCountFor(length int) uint32 {
	if length <= 0 || length > maximumArchivePlaintext {
		return 0
	}
	count := (uint64(length) + archiveChunkSize - 1) / archiveChunkSize
	if count == 0 || count > math.MaxUint32 {
		return 0
	}
	return uint32(count)
}

func chunkNonce(prefix []byte, counter uint32) []byte {
	nonce := make([]byte, 12)
	copy(nonce, prefix)
	binary.BigEndian.PutUint32(nonce[archiveNoncePrefixSize:], counter)
	return nonce
}

func chunkAAD(header []byte, counter, plaintextLength uint32) []byte {
	aad := make([]byte, 0, len(header)+8)
	aad = append(aad, header...)
	aad = binary.BigEndian.AppendUint32(aad, counter)
	aad = binary.BigEndian.AppendUint32(aad, plaintextLength)
	return aad
}

func sealGCM(key, nonce, plaintext, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil || len(nonce) != aead.NonceSize() {
		return nil, ErrArchiveFormat
	}
	return aead.Seal(nil, nonce, plaintext, aad), nil
}

func openGCM(key, nonce, ciphertext, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil || len(nonce) != aead.NonceSize() {
		return nil, ErrArchiveFormat
	}
	return aead.Open(nil, nonce, ciphertext, aad)
}

func readRandom(reader io.Reader, values ...[]byte) error {
	for _, value := range values {
		if _, err := io.ReadFull(reader, value); err != nil {
			for _, owned := range values {
				zero(owned)
			}
			return fmt.Errorf("backup: random source: %w", err)
		}
	}
	return nil
}

func validTrustAnchor(trust TrustAnchor) bool {
	return trust.WorkspaceID != "" && trust.AuditGeneration > 0 && len(trust.AuditRoot) == sha256.Size &&
		trust.SigningKeyID != "" && trust.SigningKeyEpoch > 0 && len(trust.PublicKey) == ed25519.PublicKeySize
}

func zero(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
