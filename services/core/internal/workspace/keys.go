// Package workspace owns encrypted workspace key and lifecycle controls.
package workspace

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const (
	DEKSize              = 32
	argonMemoryKiB       = 64 * 1024
	argonIterations      = 3
	argonParallelism     = 1
	argonSaltSize        = 16
	argonDigestSize      = 32
	passwordPolicyV1     = 1
	recoverySecretSize   = 32
	recoveryGroupSize    = 4
	recoveryEncodedSize  = 52
	workspaceHistorySize = 3
	userHistorySize      = 5
)

var (
	ErrPasswordPolicy = errors.New("workspace: password policy rejected secret")
	ErrPasswordReused = errors.New("workspace: password was previously used")
	ErrInvalidSecret  = errors.New("workspace: invalid secret")
	ErrKeyMaterial    = errors.New("workspace: invalid key material")
)

// PasswordVerifier is the versioned, non-secret Argon2id password verifier.
type PasswordVerifier struct {
	PolicyVersion uint16
	MemoryKiB     uint32
	Iterations    uint32
	Parallelism   uint8
	Salt          []byte
	Digest        []byte
}

func (verifier PasswordVerifier) Clone() PasswordVerifier {
	verifier.Salt = append([]byte(nil), verifier.Salt...)
	verifier.Digest = append([]byte(nil), verifier.Digest...)
	return verifier
}

func (verifier PasswordVerifier) valid() bool {
	return verifier.PolicyVersion == passwordPolicyV1 && verifier.MemoryKiB == argonMemoryKiB &&
		verifier.Iterations == argonIterations && verifier.Parallelism == argonParallelism &&
		len(verifier.Salt) == argonSaltSize && len(verifier.Digest) == argonDigestSize
}

// PasswordPolicy applies the exact v1 normalization, denylist, and Argon2id rules.
type PasswordPolicy struct {
	denied map[string]struct{}
	random io.Reader
}

func NewPasswordPolicy(entries []string, randomSource io.Reader) (*PasswordPolicy, error) {
	if randomSource == nil {
		return nil, ErrKeyMaterial
	}
	policy := &PasswordPolicy{denied: make(map[string]struct{}, len(entries)), random: randomSource}
	folder := cases.Fold()
	for _, entry := range entries {
		entry = strings.TrimSuffix(entry, "\r")
		if !utf8.ValidString(entry) {
			return nil, ErrPasswordPolicy
		}
		if entry == "" {
			continue
		}
		policy.denied[folder.String(norm.NFC.String(entry))] = struct{}{}
	}
	return policy, nil
}

// LoadPasswordDenylist verifies the pinned byte checksum and exact entry count
// before constructing the case-folded in-memory policy.
func LoadPasswordDenylist(source io.Reader, expectedCount int, expectedSHA256 string, randomSource io.Reader) (*PasswordPolicy, error) {
	if source == nil || expectedCount != 10_000 {
		return nil, ErrPasswordPolicy
	}
	expectedDigest, err := hex.DecodeString(expectedSHA256)
	if err != nil || len(expectedDigest) != sha256.Size {
		return nil, ErrPasswordPolicy
	}
	digest := sha256.New()
	scanner := bufio.NewScanner(io.TeeReader(source, digest))
	scanner.Buffer(make([]byte, 1024), 4096)
	entries := make([]string, 0, expectedCount)
	for scanner.Scan() {
		entries = append(entries, scanner.Text())
	}
	if scanner.Err() != nil || len(entries) != expectedCount || subtle.ConstantTimeCompare(digest.Sum(nil), expectedDigest) != 1 {
		return nil, ErrPasswordPolicy
	}
	return NewPasswordPolicy(entries, randomSource)
}

func (policy *PasswordPolicy) normalize(secret []byte) ([]byte, error) {
	if policy == nil || policy.random == nil || !utf8.Valid(secret) || len(secret) > 1024 {
		return nil, ErrPasswordPolicy
	}
	// Own the buffer before normalization so clearing derived material never
	// mutates a caller-owned protobuf/request buffer.
	normalized := norm.NFC.Bytes(append([]byte(nil), secret...))
	count := utf8.RuneCount(normalized)
	if count < 15 || count > 128 || len(normalized) > 1024 {
		Zero(normalized)
		return nil, ErrPasswordPolicy
	}
	for _, value := range string(normalized) {
		if !unicode.IsPrint(value) {
			Zero(normalized)
			return nil, ErrPasswordPolicy
		}
	}
	if _, denied := policy.denied[cases.Fold().String(string(normalized))]; denied {
		Zero(normalized)
		return nil, ErrPasswordPolicy
	}
	return normalized, nil
}

func (policy *PasswordPolicy) Hash(secret []byte) (PasswordVerifier, error) {
	normalized, err := policy.normalize(secret)
	if err != nil {
		return PasswordVerifier{}, err
	}
	defer Zero(normalized)
	salt := make([]byte, argonSaltSize)
	if _, err := io.ReadFull(policy.random, salt); err != nil {
		Zero(salt)
		return PasswordVerifier{}, fmt.Errorf("workspace: generate password salt: %w", err)
	}
	digest := argon2.IDKey(normalized, salt, argonIterations, argonMemoryKiB, argonParallelism, argonDigestSize)
	return PasswordVerifier{
		PolicyVersion: passwordPolicyV1,
		MemoryKiB:     argonMemoryKiB,
		Iterations:    argonIterations,
		Parallelism:   argonParallelism,
		Salt:          salt,
		Digest:        digest,
	}, nil
}

func (policy *PasswordPolicy) Verify(secret []byte, verifier PasswordVerifier) bool {
	if !verifier.valid() {
		return false
	}
	normalized, err := policy.normalize(secret)
	if err != nil {
		return false
	}
	defer Zero(normalized)
	digest := argon2.IDKey(normalized, verifier.Salt, verifier.Iterations, verifier.MemoryKiB, verifier.Parallelism, uint32(len(verifier.Digest)))
	defer Zero(digest)
	return subtle.ConstantTimeCompare(digest, verifier.Digest) == 1
}

func (policy *PasswordPolicy) Reused(secret []byte, history []PasswordVerifier) bool {
	// Deliberately evaluate every retained verifier to avoid leaking its position.
	match := 0
	for _, verifier := range history {
		if policy.Verify(secret, verifier) {
			match |= 1
		}
	}
	return match == 1
}

func RetainPasswordHistory(current PasswordVerifier, prior []PasswordVerifier, limit int) []PasswordVerifier {
	if limit <= 0 {
		return nil
	}
	history := make([]PasswordVerifier, 0, limit)
	if current.valid() {
		history = append(history, current.Clone())
	}
	for _, verifier := range prior {
		if len(history) == limit {
			break
		}
		history = append(history, verifier.Clone())
	}
	return history
}

// WrappedKey is one authenticated AES-256-GCM DEK wrap.
type WrappedKey struct {
	Salt       []byte
	Nonce      []byte
	Ciphertext []byte
	Verifier   PasswordVerifier
}

func (wrapped WrappedKey) Clone() WrappedKey {
	wrapped.Salt = append([]byte(nil), wrapped.Salt...)
	wrapped.Nonce = append([]byte(nil), wrapped.Nonce...)
	wrapped.Ciphertext = append([]byte(nil), wrapped.Ciphertext...)
	wrapped.Verifier = wrapped.Verifier.Clone()
	return wrapped
}

type KeyMaterial struct {
	DEK            []byte
	PassphraseWrap WrappedKey
	RecoveryWrap   WrappedKey
}

func (material *KeyMaterial) Destroy() {
	if material == nil {
		return
	}
	Zero(material.DEK)
	material.DEK = nil
}

func GenerateKeyMaterial(policy *PasswordPolicy, passphrase []byte, workspaceID string, version uint64) (*KeyMaterial, []byte, error) {
	if policy == nil || workspaceID == "" || version == 0 {
		return nil, nil, ErrKeyMaterial
	}
	dek := make([]byte, DEKSize)
	recovery := make([]byte, recoverySecretSize)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return nil, nil, fmt.Errorf("workspace: generate DEK: %w", err)
	}
	if _, err := io.ReadFull(rand.Reader, recovery); err != nil {
		Zero(dek)
		return nil, nil, fmt.Errorf("workspace: generate recovery secret: %w", err)
	}
	defer Zero(recovery)
	passphraseWrap, err := WrapWithPassphrase(policy, passphrase, dek, workspaceID, version)
	if err != nil {
		Zero(dek)
		return nil, nil, err
	}
	recoveryWrap, err := wrapWithRecoveryBytes(recovery, dek, workspaceID, version)
	if err != nil {
		Zero(dek)
		return nil, nil, err
	}
	display := formatRecovery(recovery)
	return &KeyMaterial{DEK: dek, PassphraseWrap: passphraseWrap, RecoveryWrap: recoveryWrap}, display, nil
}

func WrapWithPassphrase(policy *PasswordPolicy, passphrase, dek []byte, workspaceID string, version uint64) (WrappedKey, error) {
	if len(dek) != DEKSize {
		return WrappedKey{}, ErrKeyMaterial
	}
	verifier, err := policy.Hash(passphrase)
	if err != nil {
		return WrappedKey{}, err
	}
	salt := make([]byte, argonSaltSize)
	if _, err := io.ReadFull(policy.random, salt); err != nil {
		return WrappedKey{}, fmt.Errorf("workspace: generate passphrase wrapping salt: %w", err)
	}
	kek, err := derivePassphraseKEK(policy, passphrase, salt)
	if err != nil {
		Zero(salt)
		return WrappedKey{}, err
	}
	defer Zero(kek)
	nonce, ciphertext, err := seal(kek, dek, wrapAAD("passphrase", workspaceID, version))
	if err != nil {
		Zero(salt)
		return WrappedKey{}, err
	}
	return WrappedKey{Salt: salt, Nonce: nonce, Ciphertext: ciphertext, Verifier: verifier}, nil
}

func UnwrapWithPassphrase(policy *PasswordPolicy, passphrase []byte, wrapped WrappedKey, workspaceID string, version uint64) ([]byte, error) {
	if !policy.Verify(passphrase, wrapped.Verifier) {
		return nil, ErrInvalidSecret
	}
	kek, err := derivePassphraseKEK(policy, passphrase, wrapped.Salt)
	if err != nil {
		return nil, ErrInvalidSecret
	}
	defer Zero(kek)
	plaintext, err := open(kek, wrapped.Nonce, wrapped.Ciphertext, wrapAAD("passphrase", workspaceID, version))
	if err != nil || len(plaintext) != DEKSize {
		Zero(plaintext)
		return nil, ErrInvalidSecret
	}
	return plaintext, nil
}

func derivePassphraseKEK(policy *PasswordPolicy, passphrase, salt []byte) ([]byte, error) {
	if policy == nil || len(salt) != argonSaltSize {
		return nil, ErrInvalidSecret
	}
	normalized, err := policy.normalize(passphrase)
	if err != nil {
		return nil, err
	}
	defer Zero(normalized)
	return argon2.IDKey(normalized, salt, argonIterations, argonMemoryKiB, argonParallelism, argonDigestSize), nil
}

func wrapWithRecoveryBytes(recovery, dek []byte, workspaceID string, version uint64) (WrappedKey, error) {
	salt := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return WrappedKey{}, fmt.Errorf("workspace: generate recovery salt: %w", err)
	}
	kek, err := hkdf.Key(sha256.New, recovery, salt, "tammy.workspace.recovery-kek.v1", 32)
	if err != nil {
		return WrappedKey{}, err
	}
	defer Zero(kek)
	nonce, ciphertext, err := seal(kek, dek, wrapAAD("recovery", workspaceID, version))
	if err != nil {
		return WrappedKey{}, err
	}
	return WrappedKey{Salt: salt, Nonce: nonce, Ciphertext: ciphertext}, nil
}

func WrapWithRecovery(display, dek []byte, workspaceID string, version uint64) (WrappedKey, error) {
	recovery, err := decodeRecovery(display)
	if err != nil {
		return WrappedKey{}, ErrInvalidSecret
	}
	defer Zero(recovery)
	return wrapWithRecoveryBytes(recovery, dek, workspaceID, version)
}

func UnwrapWithRecovery(display []byte, wrapped WrappedKey, workspaceID string, version uint64) ([]byte, error) {
	recovery, err := decodeRecovery(display)
	if err != nil {
		return nil, ErrInvalidSecret
	}
	defer Zero(recovery)
	kek, err := hkdf.Key(sha256.New, recovery, wrapped.Salt, "tammy.workspace.recovery-kek.v1", 32)
	if err != nil {
		return nil, ErrInvalidSecret
	}
	defer Zero(kek)
	plaintext, err := open(kek, wrapped.Nonce, wrapped.Ciphertext, wrapAAD("recovery", workspaceID, version))
	if err != nil || len(plaintext) != DEKSize {
		Zero(plaintext)
		return nil, ErrInvalidSecret
	}
	return plaintext, nil
}

func seal(key, plaintext, aad []byte) ([]byte, []byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	return nonce, aead.Seal(nil, nonce, plaintext, aad), nil
}

func open(key, nonce, ciphertext, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil || len(nonce) != aead.NonceSize() {
		return nil, ErrInvalidSecret
	}
	return aead.Open(nil, nonce, ciphertext, aad)
}

func wrapAAD(purpose, workspaceID string, version uint64) []byte {
	return []byte(fmt.Sprintf("tammy.workspace.%s-wrap.v1\x00%s\x00%d", purpose, workspaceID, version))
}

func formatRecovery(recovery []byte) []byte {
	raw := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(recovery)
	grouped := make([]byte, 0, len(raw)+len(raw)/recoveryGroupSize)
	for index := 0; index < len(raw); index += recoveryGroupSize {
		if index != 0 {
			grouped = append(grouped, '-')
		}
		end := index + recoveryGroupSize
		if end > len(raw) {
			end = len(raw)
		}
		grouped = append(grouped, raw[index:end]...)
	}
	return grouped
}

func decodeRecovery(display []byte) ([]byte, error) {
	canonical := strings.ToUpper(strings.ReplaceAll(string(display), "-", ""))
	if len(canonical) != recoveryEncodedSize {
		return nil, ErrInvalidSecret
	}
	decoded, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(canonical)
	if err != nil || len(decoded) != recoverySecretSize {
		Zero(decoded)
		return nil, ErrInvalidSecret
	}
	return decoded, nil
}

func ParseRecoveryGroups(display []byte) ([][]byte, error) {
	recovery, err := decodeRecovery(display)
	if err != nil {
		return nil, err
	}
	Zero(recovery)
	canonical := strings.ToUpper(strings.ReplaceAll(string(display), "-", ""))
	groups := make([][]byte, 0, 13)
	for index := 0; index < len(canonical); index += recoveryGroupSize {
		end := index + recoveryGroupSize
		if end > len(canonical) {
			end = len(canonical)
		}
		groups = append(groups, []byte(canonical[index:end]))
	}
	return groups, nil
}

func Zero(secret []byte) {
	for index := range secret {
		secret[index] = 0
	}
}
