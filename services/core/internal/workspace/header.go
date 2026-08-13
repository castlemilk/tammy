package workspace

import (
	"bytes"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

var (
	ErrHeaderAuthentication = errors.New("workspace: header authentication failed")
	ErrHeaderVersion        = errors.New("workspace: invalid header version")
	ErrHeaderOperation      = errors.New("workspace: no committed header operation")
)

type HeaderSlot struct {
	Version           uint64
	OperationID       string
	WorkspaceID       string
	PassphraseWrap    WrappedKey
	RecoveryWrap      WrappedKey
	RecoveryVersion   uint64
	PassphraseHistory []PasswordVerifier
	Audit             *AuditHeaderMetadata
	MAC               []byte
}

// AuditHeaderMetadata contains only public/non-secret audit bootstrap state.
// The encrypted private signing key remains inside SQLCipher.
type AuditHeaderMetadata struct {
	ChainSalt            []byte
	GenesisHash          []byte
	SigningPublicKey     []byte
	SigningKeyID         string
	PreviousSigningKeyID string
	RotationSignature    []byte
}

func (metadata *AuditHeaderMetadata) Clone() *AuditHeaderMetadata {
	if metadata == nil {
		return nil
	}
	return &AuditHeaderMetadata{ChainSalt: append([]byte(nil), metadata.ChainSalt...),
		GenesisHash:      append([]byte(nil), metadata.GenesisHash...),
		SigningPublicKey: append([]byte(nil), metadata.SigningPublicKey...), SigningKeyID: metadata.SigningKeyID,
		PreviousSigningKeyID: metadata.PreviousSigningKeyID,
		RotationSignature:    append([]byte(nil), metadata.RotationSignature...)}
}

// Slots returns authenticated slots with the active slot first. Callers use
// them only to acquire a candidate DEK before database-backed election.
func (store *HeaderStore) Slots() ([2]HeaderSlot, error) {
	content, err := store.load()
	if err != nil {
		return [2]HeaderSlot{}, err
	}
	return [2]HeaderSlot{content.Slots[content.Active].Clone(), content.Slots[1-content.Active].Clone()}, nil
}

func (slot HeaderSlot) Clone() HeaderSlot {
	slot.PassphraseWrap = slot.PassphraseWrap.Clone()
	slot.RecoveryWrap = slot.RecoveryWrap.Clone()
	slot.MAC = append([]byte(nil), slot.MAC...)
	slot.PassphraseHistory = make([]PasswordVerifier, len(slot.PassphraseHistory))
	for index := range slot.PassphraseHistory {
		slot.PassphraseHistory[index] = slot.PassphraseHistory[index].Clone()
	}
	slot.Audit = slot.Audit.Clone()
	return slot
}

type headerFile struct {
	Format    string        `json:"format"`
	Active    int           `json:"active"`
	Slots     [2]HeaderSlot `json:"slots"`
	ActiveMAC []byte        `json:"active_mac"`
}

type HeaderStore struct {
	path    string
	authKey []byte
}

func NewHeaderStore(path string, authenticationKey []byte) (*HeaderStore, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || len(authenticationKey) != 32 {
		return nil, ErrHeaderAuthentication
	}
	return &HeaderStore{path: path, authKey: append([]byte(nil), authenticationKey...)}, nil
}

func (store *HeaderStore) Close() {
	if store != nil {
		Zero(store.authKey)
		store.authKey = nil
	}
}

func (store *HeaderStore) Initialize(initial HeaderSlot) error {
	if store == nil || initial.Version != 1 || initial.OperationID == "" || initial.WorkspaceID == "" || !validAuditMetadata(initial) {
		return ErrHeaderVersion
	}
	if _, err := os.Lstat(store.path); err == nil || !os.IsNotExist(err) {
		return ErrHeaderVersion
	}
	content := headerFile{Format: "tammy-workspace-header-v1", Active: 0}
	content.Slots[0] = store.authenticateSlot(initial)
	content.Slots[1] = store.authenticateSlot(HeaderSlot{})
	content.ActiveMAC = store.activeMAC(content)
	return writeHeaderFile(store.path, content)
}

func (store *HeaderStore) Prepare(next HeaderSlot) error {
	content, err := store.load()
	if err != nil {
		return err
	}
	current := content.Slots[content.Active]
	if next.Version != current.Version+1 || next.OperationID == "" || next.WorkspaceID != current.WorkspaceID {
		return ErrHeaderVersion
	}
	if !validAuditMetadata(next) || !validAuditTransition(next.WorkspaceID, current.Audit, next.Audit) {
		return ErrHeaderVersion
	}
	inactive := 1 - content.Active
	content.Slots[inactive] = store.authenticateSlot(next)
	content.ActiveMAC = store.activeMAC(content)
	return writeHeaderFile(store.path, content)
}

func (store *HeaderStore) Activate(operationID string) error {
	content, err := store.load()
	if err != nil {
		return err
	}
	inactive := 1 - content.Active
	if content.Slots[inactive].OperationID != operationID || content.Slots[inactive].Version <= content.Slots[content.Active].Version {
		return ErrHeaderOperation
	}
	content.Active = inactive
	content.ActiveMAC = store.activeMAC(content)
	return writeHeaderFile(store.path, content)
}

// Elect reconciles authenticated slots with durable database operation IDs.
// The highest committed version wins, and an interrupted activation is completed.
func (store *HeaderStore) Elect(committed func(operationID string, version uint64) bool) (HeaderSlot, error) {
	if committed == nil {
		return HeaderSlot{}, ErrHeaderOperation
	}
	content, err := store.load()
	if err != nil {
		return HeaderSlot{}, err
	}
	winner := -1
	for index := range content.Slots {
		slot := content.Slots[index]
		if slot.Version == 0 || !committed(slot.OperationID, slot.Version) {
			continue
		}
		if winner == -1 || slot.Version > content.Slots[winner].Version {
			winner = index
		} else if slot.Version == content.Slots[winner].Version {
			return HeaderSlot{}, ErrHeaderOperation
		}
	}
	if winner == -1 {
		return HeaderSlot{}, ErrHeaderOperation
	}
	if content.Active != winner {
		content.Active = winner
		content.ActiveMAC = store.activeMAC(content)
		if err := writeHeaderFile(store.path, content); err != nil {
			return HeaderSlot{}, err
		}
	}
	return content.Slots[winner].Clone(), nil
}

func (store *HeaderStore) load() (headerFile, error) {
	if store == nil || len(store.authKey) != 32 {
		return headerFile{}, ErrHeaderAuthentication
	}
	content, err := readHeaderFile(store.path)
	if err != nil {
		return headerFile{}, err
	}
	if content.Format != "tammy-workspace-header-v1" || content.Active < 0 || content.Active > 1 {
		return headerFile{}, ErrHeaderAuthentication
	}
	for _, slot := range content.Slots {
		if !hmac.Equal(slot.MAC, store.authenticateSlot(slot).MAC) {
			return headerFile{}, ErrHeaderAuthentication
		}
		if slot.Version != 0 && !validAuditMetadata(slot) {
			return headerFile{}, ErrHeaderAuthentication
		}
	}
	if !hmac.Equal(content.ActiveMAC, store.activeMAC(content)) {
		return headerFile{}, ErrHeaderAuthentication
	}
	return content, nil
}

func validAuditMetadata(slot HeaderSlot) bool {
	if slot.Audit == nil {
		return true
	}
	metadata := slot.Audit
	if len(metadata.ChainSalt) != sha256.Size || len(metadata.GenesisHash) != sha256.Size ||
		len(metadata.SigningPublicKey) != ed25519.PublicKeySize || metadata.SigningKeyID == "" || len(metadata.SigningKeyID) > 128 ||
		!((metadata.PreviousSigningKeyID == "" && len(metadata.RotationSignature) == 0) ||
			(metadata.PreviousSigningKeyID != "" && len(metadata.PreviousSigningKeyID) <= 128 && len(metadata.RotationSignature) == ed25519.SignatureSize)) {
		return false
	}
	digest := sha256.New()
	_, _ = digest.Write([]byte("tammy-audit-v1"))
	_, _ = digest.Write([]byte(slot.WorkspaceID))
	_, _ = digest.Write(metadata.ChainSalt)
	return hmac.Equal(digest.Sum(nil), metadata.GenesisHash)
}

func validAuditTransition(workspaceID string, current, next *AuditHeaderMetadata) bool {
	if current == nil {
		return next == nil || next.PreviousSigningKeyID == "" && len(next.RotationSignature) == 0
	}
	if next == nil || !bytes.Equal(current.ChainSalt, next.ChainSalt) || !bytes.Equal(current.GenesisHash, next.GenesisHash) {
		return false
	}
	if bytes.Equal(current.SigningPublicKey, next.SigningPublicKey) && current.SigningKeyID == next.SigningKeyID {
		return current.PreviousSigningKeyID == next.PreviousSigningKeyID && bytes.Equal(current.RotationSignature, next.RotationSignature)
	}
	if next.PreviousSigningKeyID != current.SigningKeyID || len(next.RotationSignature) != ed25519.SignatureSize {
		return false
	}
	digest := auditSigningKeyRotationDigest(workspaceID, current.SigningKeyID, next.SigningKeyID, next.SigningPublicKey)
	return ed25519.Verify(ed25519.PublicKey(current.SigningPublicKey), digest[:], next.RotationSignature)
}

func auditSigningKeyRotationDigest(workspaceID, previousKeyID, newKeyID string, newPublicKey []byte) [sha256.Size]byte {
	digest := sha256.New()
	_, _ = digest.Write([]byte("tammy-audit-signing-key-rotation-v1\x00"))
	_, _ = digest.Write([]byte(workspaceID))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write([]byte(previousKeyID))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write([]byte(newKeyID))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write(newPublicKey)
	var result [sha256.Size]byte
	copy(result[:], digest.Sum(nil))
	return result
}

func (store *HeaderStore) authenticateSlot(slot HeaderSlot) HeaderSlot {
	slot = slot.Clone()
	slot.MAC = nil
	payload, _ := json.Marshal(slot)
	digest := hmac.New(sha256.New, store.authKey)
	_, _ = digest.Write([]byte("tammy.workspace.header-slot.v1\x00"))
	_, _ = digest.Write(payload)
	slot.MAC = digest.Sum(nil)
	return slot
}

func (store *HeaderStore) activeMAC(content headerFile) []byte {
	digest := hmac.New(sha256.New, store.authKey)
	_, _ = digest.Write([]byte("tammy.workspace.header-active.v1\x00"))
	_, _ = digest.Write([]byte{byte(content.Active)})
	for _, slot := range content.Slots {
		_, _ = digest.Write(slot.MAC)
	}
	return digest.Sum(nil)
}

func readHeaderFile(path string) (headerFile, error) {
	payload, _, err := readSecureRegularFile(path, maxHeaderFileSize)
	if err != nil {
		return headerFile{}, ErrHeaderAuthentication
	}
	var content headerFile
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&content); err != nil {
		return headerFile{}, ErrHeaderAuthentication
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return headerFile{}, ErrHeaderAuthentication
	}
	return content, nil
}

func writeHeaderFile(path string, content headerFile) error {
	parent := filepath.Dir(path)
	temporary, err := os.CreateTemp(parent, ".tammy-header-*")
	if err != nil {
		return fmt.Errorf("workspace: create header: %w", err)
	}
	temporaryPath := temporary.Name()
	cleanup := func() { _ = os.Remove(temporaryPath) }
	defer cleanup()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	encoder := json.NewEncoder(temporary)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	directory, err := os.Open(parent)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
