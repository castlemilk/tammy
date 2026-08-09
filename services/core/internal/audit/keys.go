package audit

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var ErrSigningKey = errors.New("audit: signing key unavailable or invalid")

const keyEncryptionAlgorithm = "AES-256-GCM/HKDF-SHA256-v1"

const signingKeyCiphertextSize = ed25519.PrivateKeySize + 16

const (
	signingKeyRotationVersion          = "tammy.audit.signing-key-rotation.v1"
	signingKeyChainVersion             = "tammy.audit.signing-key-chain.v1"
	predecessorRotationSignatureDomain = "tammy-audit-signing-key-rotation-predecessor-v1"
	successorPossessionSignatureDomain = "tammy-audit-signing-key-rotation-successor-possession-v1"
)

type SigningKeyHeader struct {
	KeyID             string
	PublicKey         ed25519.PublicKey
	PreviousKeyID     string
	RotationSignature []byte
}

type SigningKeyRecord struct {
	KeyID               string
	WorkspaceID         string
	Generation          uint64
	Epoch               uint64
	PublicKey           ed25519.PublicKey
	EncryptedPrivateKey []byte
	Nonce               []byte
	EncryptionAlgorithm string
	SigningAlgorithm    string
	PreviousKeyID       string
	PreviousSignature   []byte
	PossessionSignature []byte
	RotationSequence    uint64
	RotationPriorHead   []byte
	CreatedAt           time.Time
	RetiredAt           *time.Time
}

// SigningLineageRootFingerprint returns the domain-separated identity of the
// authenticated root signing key. Backup and restore share this framing so an
// archive cannot assert an unrelated root or reinterpret another digest.
func SigningLineageRootFingerprint(workspaceID, keyID string, publicKey ed25519.PublicKey) ([sha256.Size]byte, error) {
	if workspaceID == "" || keyID == "" || len(publicKey) != ed25519.PublicKeySize {
		return [sha256.Size]byte{}, ErrSigningKey
	}
	digest := sha256.New()
	_, _ = digest.Write([]byte("tammy.audit.signing-lineage-root.v1\x00"))
	for _, value := range [][]byte{[]byte(workspaceID), []byte(keyID), publicKey} {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(value)))
		_, _ = digest.Write(length[:])
		_, _ = digest.Write(value)
	}
	var result [sha256.Size]byte
	copy(result[:], digest.Sum(nil))
	return result, nil
}

// SigningKeyState is the workspace-wide compare-and-swap pointer into the
// immutable signing-key lineage.
type SigningKeyState struct {
	WorkspaceID string
	RootKeyID   string
	ActiveKeyID string
	ActiveEpoch uint64
}

// SigningKeyRotationInput freezes both the audit head and active signing-key
// tuple that the caller authenticated before requesting a rotation.
type SigningKeyRotationInput struct {
	ExpectedHeader ChainHeader
	ExpectedState  SigningKeyState
	DEK            []byte
	RotatedAt      time.Time
	Random         io.Reader
	Event          *tammyv1.AuditEvent
}

// SigningKeyRotationResult contains the exact immutable lineage link and
// typed audit event staged in the caller-owned transaction.
type SigningKeyRotationResult struct {
	Retired     SigningKeyRecord
	Successor   SigningKeyRecord
	Link        *tammyv1.AuditSigningKeyRotationLink
	StoredEvent StoredEvent
}

type signingKeyRotationTransaction interface {
	Executor
	AfterCommitRegistrar
	Commit() error
	Rollback() error
}

const signingKeyRotationSavepoint = "audit_signing_key_rotation_v1"

func GenerateSigningKey(
	workspaceID string,
	dek []byte,
	createdAt time.Time,
	random io.Reader,
) (SigningKeyRecord, SigningKeyHeader, error) {
	if workspaceID == "" || len(dek) != 32 || !validAuditTimestamp(createdAt) || random == nil {
		return SigningKeyRecord{}, SigningKeyHeader{}, ErrSigningKey
	}
	publicKey, privateKey, err := ed25519.GenerateKey(random)
	if err != nil {
		return SigningKeyRecord{}, SigningKeyHeader{}, ErrSigningKey
	}
	defer Zero(privateKey)
	keyID, err := SigningKeyID(workspaceID, publicKey)
	if err != nil {
		return SigningKeyRecord{}, SigningKeyHeader{}, err
	}
	keyEncryptionKey, err := deriveSigningKeyEncryptionKey(workspaceID, dek)
	if err != nil {
		return SigningKeyRecord{}, SigningKeyHeader{}, err
	}
	defer Zero(keyEncryptionKey)
	block, err := aes.NewCipher(keyEncryptionKey)
	if err != nil {
		return SigningKeyRecord{}, SigningKeyHeader{}, ErrSigningKey
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return SigningKeyRecord{}, SigningKeyHeader{}, ErrSigningKey
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(random, nonce); err != nil {
		Zero(nonce)
		return SigningKeyRecord{}, SigningKeyHeader{}, ErrSigningKey
	}
	aad := signingKeyAAD(workspaceID, keyID, publicKey)
	encrypted := aead.Seal(nil, nonce, privateKey, aad)
	record := SigningKeyRecord{KeyID: keyID, WorkspaceID: workspaceID,
		Generation: 1, Epoch: 1,
		PublicKey: append(ed25519.PublicKey(nil), publicKey...), EncryptedPrivateKey: encrypted,
		Nonce: nonce, EncryptionAlgorithm: keyEncryptionAlgorithm, SigningAlgorithm: "Ed25519",
		CreatedAt: createdAt.UTC()}
	header := SigningKeyHeader{KeyID: keyID, PublicKey: append(ed25519.PublicKey(nil), publicKey...)}
	return record, header, nil
}

func createSigningKeySuccessor(current SigningKeyRecord, dek []byte, generation, priorSequence uint64,
	priorHead [sha256.Size]byte, rotatedAt time.Time, random io.Reader,
) (SigningKeyRecord, SigningKeyRecord, *tammyv1.AuditSigningKeyRotationLink, error) {
	if !validSigningKeyRecord(current) || current.RetiredAt != nil || len(dek) != 32 || generation == 0 ||
		generation < current.Generation || priorHead == ([sha256.Size]byte{}) || !validAuditTimestamp(rotatedAt) ||
		!rotatedAt.After(current.CreatedAt) || random == nil {
		return SigningKeyRecord{}, SigningKeyRecord{}, nil, ErrSigningKey
	}
	next, _, err := GenerateSigningKey(current.WorkspaceID, dek, rotatedAt, random)
	if err != nil {
		return SigningKeyRecord{}, SigningKeyRecord{}, nil, err
	}
	successorPrivate, err := DecryptSigningKey(next, dek)
	if err != nil {
		return SigningKeyRecord{}, SigningKeyRecord{}, nil, err
	}
	defer Zero(successorPrivate)
	next.Generation = generation
	next.Epoch = current.Epoch + 1
	next.PreviousKeyID = current.KeyID
	next.RotationSequence = priorSequence
	next.RotationPriorHead = append([]byte(nil), priorHead[:]...)
	link := &tammyv1.AuditSigningKeyRotationLink{
		Version: signingKeyRotationVersion, WorkspaceId: current.WorkspaceID, Generation: generation,
		PriorSequence: priorSequence, PriorHead: append([]byte(nil), priorHead[:]...), SuccessorEpoch: next.Epoch,
		PredecessorKeyId: current.KeyID, PredecessorPublicKey: append([]byte(nil), current.PublicKey...),
		SuccessorKeyId: next.KeyID, SuccessorPublicKey: append([]byte(nil), next.PublicKey...),
		RotatedAt: timestamppb.New(rotatedAt.UTC()),
	}
	predecessorDigest, err := signingKeyLinkDigest(predecessorRotationSignatureDomain, link)
	if err != nil {
		return SigningKeyRecord{}, SigningKeyRecord{}, nil, err
	}
	predecessorPrivate, err := DecryptSigningKey(current, dek)
	if err != nil {
		return SigningKeyRecord{}, SigningKeyRecord{}, nil, err
	}
	link.PredecessorSignature = ed25519.Sign(predecessorPrivate, predecessorDigest[:])
	Zero(predecessorPrivate)
	successorDigest, err := signingKeyLinkDigest(successorPossessionSignatureDomain, link)
	if err != nil {
		return SigningKeyRecord{}, SigningKeyRecord{}, nil, err
	}
	link.SuccessorPossessionSignature = ed25519.Sign(successorPrivate, successorDigest[:])
	next.PreviousSignature = append([]byte(nil), link.PredecessorSignature...)
	next.PossessionSignature = append([]byte(nil), link.SuccessorPossessionSignature...)
	retired := cloneSigningKeyRecord(current)
	retirement := rotatedAt.UTC()
	retired.RetiredAt = &retirement
	if !validSigningKeyRecord(retired) || !validSigningKeyRecord(next) || !verifySigningKeyRotationLink(link) {
		return SigningKeyRecord{}, SigningKeyRecord{}, nil, ErrSigningKey
	}
	return retired, next, proto.Clone(link).(*tammyv1.AuditSigningKeyRotationLink), nil
}

func signingKeyLinkDigest(domain string, link *tammyv1.AuditSigningKeyRotationLink) ([sha256.Size]byte, error) {
	if domain == "" || link == nil {
		return [sha256.Size]byte{}, ErrSigningKey
	}
	unsigned := proto.Clone(link).(*tammyv1.AuditSigningKeyRotationLink)
	unsigned.PredecessorSignature = nil
	unsigned.SuccessorPossessionSignature = nil
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(unsigned)
	if err != nil {
		return [sha256.Size]byte{}, ErrSigningKey
	}
	digest := sha256.New()
	_, _ = digest.Write([]byte(domain))
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(encoded)))
	_, _ = digest.Write(length[:])
	_, _ = digest.Write(encoded)
	var result [sha256.Size]byte
	copy(result[:], digest.Sum(nil))
	return result, nil
}

func verifySigningKeyRotationLink(link *tammyv1.AuditSigningKeyRotationLink) bool {
	if link == nil || link.Version != signingKeyRotationVersion || link.WorkspaceId == "" || link.Generation == 0 ||
		len(link.PriorHead) != sha256.Size || bytes.Equal(link.PriorHead, make([]byte, sha256.Size)) ||
		link.SuccessorEpoch < 2 || link.PredecessorKeyId == "" ||
		len(link.PredecessorPublicKey) != ed25519.PublicKeySize || link.SuccessorKeyId == "" ||
		len(link.SuccessorPublicKey) != ed25519.PublicKeySize || link.RotatedAt == nil || !link.RotatedAt.IsValid() ||
		len(link.PredecessorSignature) != ed25519.SignatureSize ||
		len(link.SuccessorPossessionSignature) != ed25519.SignatureSize {
		return false
	}
	predecessorID, predecessorIDErr := SigningKeyID(link.WorkspaceId, link.PredecessorPublicKey)
	successorID, successorIDErr := SigningKeyID(link.WorkspaceId, link.SuccessorPublicKey)
	predecessorDigest, predecessorErr := signingKeyLinkDigest(predecessorRotationSignatureDomain, link)
	successorDigest, successorErr := signingKeyLinkDigest(successorPossessionSignatureDomain, link)
	return predecessorIDErr == nil && successorIDErr == nil && predecessorErr == nil && successorErr == nil &&
		predecessorID == link.PredecessorKeyId && successorID == link.SuccessorKeyId &&
		ed25519.Verify(link.PredecessorPublicKey, predecessorDigest[:], link.PredecessorSignature) &&
		ed25519.Verify(link.SuccessorPublicKey, successorDigest[:], link.SuccessorPossessionSignature)
}

func signedSigningKeyRotationLinkDigest(link *tammyv1.AuditSigningKeyRotationLink) ([sha256.Size]byte, error) {
	if !verifySigningKeyRotationLink(link) {
		return [sha256.Size]byte{}, ErrSigningKey
	}
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(link)
	if err != nil {
		return [sha256.Size]byte{}, ErrSigningKey
	}
	return sha256.Sum256(encoded), nil
}

func rotationEventMatchesLink(link *tammyv1.AuditSigningKeyRotationLink, eventProto, payloadProto []byte) (*tammyv1.AuditEvent, bool) {
	if !verifySigningKeyRotationLink(link) || len(eventProto) == 0 || len(payloadProto) == 0 {
		return nil, false
	}
	event := new(tammyv1.AuditEvent)
	if (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(eventProto, event) != nil ||
		messageHasUnknown(event.ProtoReflect()) || event.WorkspaceId != link.WorkspaceId || event.Generation != link.Generation ||
		event.Sequence == 0 || event.Sequence-1 != link.PriorSequence ||
		event.Type != tammyv1.AuditEventType_AUDIT_EVENT_TYPE_SIGNING_KEY_ROTATED || event.OccurredAt == nil ||
		!event.OccurredAt.AsTime().Equal(link.RotatedAt.AsTime()) ||
		!bytes.Equal(event.PreviousHash, link.PriorHead) {
		return nil, false
	}
	canonicalEventProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(event)
	if err != nil || !bytes.Equal(canonicalEventProto, eventProto) {
		return nil, false
	}
	payload := event.GetPayload().GetSigningKeyRotated()
	if payload == nil || messageHasUnknown(payload.ProtoReflect()) {
		return nil, false
	}
	canonicalPayloadProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(payload)
	linkDigest, digestErr := signedSigningKeyRotationLinkDigest(link)
	if err != nil || digestErr != nil || !bytes.Equal(canonicalPayloadProto, payloadProto) ||
		payload.WorkspaceId != link.WorkspaceId || payload.Generation != link.Generation ||
		payload.SuccessorEpoch != link.SuccessorEpoch || payload.PredecessorKeyId != link.PredecessorKeyId ||
		payload.SuccessorKeyId != link.SuccessorKeyId || !bytes.Equal(payload.RotationLinkSha256, linkDigest[:]) {
		return nil, false
	}
	var previous [sha256.Size]byte
	copy(previous[:], link.PriorHead)
	prepared, err := reconstructEventWithStoredOpenings(previous, event, payloadProto)
	if err != nil || !bytes.Equal(prepared.EventProto, eventProto) {
		return nil, false
	}
	return event, true
}

func signingKeyRotationLink(previous, successor SigningKeyRecord) *tammyv1.AuditSigningKeyRotationLink {
	return &tammyv1.AuditSigningKeyRotationLink{
		Version: signingKeyRotationVersion, WorkspaceId: successor.WorkspaceID, Generation: successor.Generation,
		PriorSequence: successor.RotationSequence, PriorHead: append([]byte(nil), successor.RotationPriorHead...),
		SuccessorEpoch: successor.Epoch, PredecessorKeyId: previous.KeyID,
		PredecessorPublicKey: append([]byte(nil), previous.PublicKey...), SuccessorKeyId: successor.KeyID,
		SuccessorPublicKey: append([]byte(nil), successor.PublicKey...), RotatedAt: timestamppb.New(successor.CreatedAt.UTC()),
		PredecessorSignature:         append([]byte(nil), successor.PreviousSignature...),
		SuccessorPossessionSignature: append([]byte(nil), successor.PossessionSignature...),
	}
}

func cloneSigningKeyRecord(record SigningKeyRecord) SigningKeyRecord {
	cloned := record
	cloned.PublicKey = append(ed25519.PublicKey(nil), record.PublicKey...)
	cloned.EncryptedPrivateKey = append([]byte(nil), record.EncryptedPrivateKey...)
	cloned.Nonce = append([]byte(nil), record.Nonce...)
	cloned.PreviousSignature = append([]byte(nil), record.PreviousSignature...)
	cloned.PossessionSignature = append([]byte(nil), record.PossessionSignature...)
	cloned.RotationPriorHead = append([]byte(nil), record.RotationPriorHead...)
	if record.RetiredAt != nil {
		retired := record.RetiredAt.UTC()
		cloned.RetiredAt = &retired
	}
	return cloned
}

func signingKeyChainFromRecords(records []SigningKeyRecord, active SigningKeyRecord) (*tammyv1.AuditSigningKeyChain, error) {
	if len(records) == 0 || len(records) > 1024 || !validSigningKeyRecord(active) || active.RetiredAt != nil {
		return nil, ErrSigningKey
	}
	chain := &tammyv1.AuditSigningKeyChain{Version: signingKeyChainVersion,
		Keys:  make([]*tammyv1.AuditSigningPublicKey, 0, len(records)),
		Links: make([]*tammyv1.AuditSigningKeyRotationLink, 0, len(records)-1)}
	for index := range records {
		record := records[index]
		if !validSigningKeyRecord(record) {
			return nil, ErrSigningKey
		}
		public := &tammyv1.AuditSigningPublicKey{WorkspaceId: record.WorkspaceID, Generation: record.Generation,
			Epoch: record.Epoch, KeyId: record.KeyID, PublicKey: append([]byte(nil), record.PublicKey...),
			CreatedAt: timestamppb.New(record.CreatedAt.UTC())}
		if record.RetiredAt != nil {
			public.RetiredAt = timestamppb.New(record.RetiredAt.UTC())
		}
		chain.Keys = append(chain.Keys, public)
		if index != 0 {
			chain.Links = append(chain.Links, signingKeyRotationLink(records[index-1], record))
		}
	}
	if !verifyPublicSigningKeyChain(chain) {
		return nil, ErrSigningKey
	}
	terminal := records[len(records)-1]
	if terminal.KeyID != active.KeyID || terminal.WorkspaceID != active.WorkspaceID || terminal.Generation != active.Generation ||
		terminal.Epoch != active.Epoch || !bytes.Equal(terminal.PublicKey, active.PublicKey) || terminal.RetiredAt != nil {
		return nil, ErrSigningKey
	}
	return chain, nil
}

func verifyPublicSigningKeyChain(chain *tammyv1.AuditSigningKeyChain) bool {
	if chain == nil || chain.Version != signingKeyChainVersion || len(chain.Keys) == 0 || len(chain.Keys) > 1024 ||
		len(chain.Links) != len(chain.Keys)-1 {
		return false
	}
	seenIDs := make(map[string]struct{}, len(chain.Keys))
	seenPublicKeys := make(map[[ed25519.PublicKeySize]byte]struct{}, len(chain.Keys))
	workspaceID := ""
	for index, key := range chain.Keys {
		if key == nil || key.WorkspaceId == "" || key.Generation == 0 || key.Epoch != uint64(index+1) || key.KeyId == "" ||
			len(key.PublicKey) != ed25519.PublicKeySize || key.CreatedAt == nil || !key.CreatedAt.IsValid() ||
			key.RetiredAt != nil && !key.RetiredAt.IsValid() {
			return false
		}
		if index == 0 {
			workspaceID = key.WorkspaceId
		} else if key.WorkspaceId != workspaceID || key.Generation < chain.Keys[index-1].Generation ||
			!key.CreatedAt.AsTime().After(chain.Keys[index-1].CreatedAt.AsTime()) {
			return false
		}
		derivedID, err := SigningKeyID(key.WorkspaceId, key.PublicKey)
		if err != nil || derivedID != key.KeyId {
			return false
		}
		var publicKey [ed25519.PublicKeySize]byte
		copy(publicKey[:], key.PublicKey)
		if _, duplicate := seenIDs[key.KeyId]; duplicate {
			return false
		}
		if _, duplicate := seenPublicKeys[publicKey]; duplicate {
			return false
		}
		seenIDs[key.KeyId] = struct{}{}
		seenPublicKeys[publicKey] = struct{}{}
		if index == len(chain.Keys)-1 {
			if key.RetiredAt != nil {
				return false
			}
			continue
		}
		next := chain.Keys[index+1]
		if key.RetiredAt == nil || next == nil || !key.RetiredAt.AsTime().Equal(next.CreatedAt.AsTime()) {
			return false
		}
	}
	for index, link := range chain.Links {
		previous, next := chain.Keys[index], chain.Keys[index+1]
		if !verifySigningKeyRotationLink(link) || link.WorkspaceId != workspaceID || link.Generation != next.Generation ||
			link.SuccessorEpoch != next.Epoch || link.PredecessorKeyId != previous.KeyId ||
			!bytes.Equal(link.PredecessorPublicKey, previous.PublicKey) || link.SuccessorKeyId != next.KeyId ||
			!bytes.Equal(link.SuccessorPublicKey, next.PublicKey) || !link.RotatedAt.AsTime().Equal(next.CreatedAt.AsTime()) ||
			link.Generation < previous.Generation {
			return false
		}
		if index != 0 {
			prior := chain.Links[index-1]
			if link.Generation < prior.Generation || link.Generation == prior.Generation && link.PriorSequence <= prior.PriorSequence {
				return false
			}
		}
	}
	return true
}

// SigningKeyID deterministically binds the public verification key to one workspace.
func SigningKeyID(workspaceID string, publicKey ed25519.PublicKey) (string, error) {
	if workspaceID == "" || len(publicKey) != ed25519.PublicKeySize {
		return "", ErrSigningKey
	}
	identifier := sha256.New()
	_, _ = identifier.Write([]byte("tammy-audit-signing-key-v1"))
	_, _ = identifier.Write([]byte(workspaceID))
	_, _ = identifier.Write(publicKey)
	digest := identifier.Sum(nil)
	identifierBytes := append([]byte(nil), digest[:16]...)
	identifierBytes[6] = (identifierBytes[6] & 0x0f) | 0x70
	identifierBytes[8] = (identifierBytes[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", identifierBytes[0:4], identifierBytes[4:6], identifierBytes[6:8],
		identifierBytes[8:10], identifierBytes[10:16]), nil
}

func DecryptSigningKey(record SigningKeyRecord, dek []byte) (ed25519.PrivateKey, error) {
	if !validSigningKeyRecord(record) || len(dek) != 32 {
		return nil, ErrSigningKey
	}
	keyEncryptionKey, err := deriveSigningKeyEncryptionKey(record.WorkspaceID, dek)
	if err != nil {
		return nil, ErrSigningKey
	}
	defer Zero(keyEncryptionKey)
	block, err := aes.NewCipher(keyEncryptionKey)
	if err != nil {
		return nil, ErrSigningKey
	}
	aead, err := cipher.NewGCM(block)
	if err != nil || len(record.Nonce) != aead.NonceSize() {
		return nil, ErrSigningKey
	}
	plaintext, err := aead.Open(nil, record.Nonce, record.EncryptedPrivateKey,
		signingKeyAAD(record.WorkspaceID, record.KeyID, record.PublicKey))
	if err != nil || len(plaintext) != ed25519.PrivateKeySize {
		Zero(plaintext)
		return nil, ErrSigningKey
	}
	privateKey := ed25519.PrivateKey(plaintext)
	if !bytes.Equal(privateKey.Public().(ed25519.PublicKey), record.PublicKey) {
		Zero(privateKey)
		return nil, ErrSigningKey
	}
	return privateKey, nil
}

func SignManifestHash(record SigningKeyRecord, dek []byte, manifestHash [sha256.Size]byte) ([]byte, error) {
	privateKey, err := DecryptSigningKey(record, dek)
	if err != nil {
		return nil, err
	}
	defer Zero(privateKey)
	return ed25519.Sign(privateKey, manifestHash[:]), nil
}

func PersistSigningKey(ctx context.Context, executor Executor, record SigningKeyRecord) error {
	if executor == nil || !validSigningKeyRecord(record) {
		return ErrSigningKey
	}
	if _, err := executor.ExecContext(ctx, `INSERT INTO audit_signing_keys_v1(
		key_id, workspace_id, generation, epoch, public_key, encrypted_private_key, nonce,
		encryption_algorithm, signing_algorithm, predecessor_key_id, predecessor_signature,
		successor_possession_signature, rotation_prior_sequence, rotation_prior_head, created_at, retired_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, record.KeyID, record.WorkspaceID,
		record.Generation, record.Epoch, record.PublicKey,
		record.EncryptedPrivateKey, record.Nonce, record.EncryptionAlgorithm, record.SigningAlgorithm,
		nullString(record.PreviousKeyID), nullBytes(record.PreviousSignature), nullBytes(record.PossessionSignature),
		nullRotationSequence(record), nullBytes(record.RotationPriorHead), formatTimestamp(record.CreatedAt),
		nullTimestamp(record.RetiredAt)); err != nil {
		return ErrSigningKey
	}
	return nil
}

func LoadSigningKey(ctx context.Context, executor Executor, workspaceID, keyID string) (SigningKeyRecord, error) {
	if executor == nil || workspaceID == "" || keyID == "" {
		return SigningKeyRecord{}, ErrSigningKey
	}
	rows, err := executor.QueryContext(ctx, `SELECT key_id, workspace_id, generation, epoch, public_key, encrypted_private_key,
		nonce, encryption_algorithm, signing_algorithm, COALESCE(predecessor_key_id, ''), predecessor_signature,
		successor_possession_signature, COALESCE(rotation_prior_sequence, 0), rotation_prior_head, created_at, retired_at
		FROM audit_signing_keys_v1
		WHERE workspace_id = ? AND key_id = ? AND length(encrypted_private_key) = ?`,
		workspaceID, keyID, signingKeyCiphertextSize)
	if err != nil {
		return SigningKeyRecord{}, ErrSigningKey
	}
	defer rows.Close()
	if !rows.Next() {
		return SigningKeyRecord{}, ErrSigningKey
	}
	var record SigningKeyRecord
	var created string
	var retired sql.NullString
	if err := rows.Scan(&record.KeyID, &record.WorkspaceID, &record.Generation, &record.Epoch,
		&record.PublicKey, &record.EncryptedPrivateKey,
		&record.Nonce, &record.EncryptionAlgorithm, &record.SigningAlgorithm, &record.PreviousKeyID,
		&record.PreviousSignature, &record.PossessionSignature, &record.RotationSequence,
		&record.RotationPriorHead, &created, &retired); err != nil || rows.Next() || rows.Err() != nil {
		return SigningKeyRecord{}, ErrSigningKey
	}
	record.CreatedAt, err = time.Parse(timestampLayout, created)
	if err == nil && retired.Valid {
		instant, parseErr := time.Parse(timestampLayout, retired.String)
		if parseErr != nil {
			err = parseErr
		} else {
			record.RetiredAt = &instant
		}
	}
	if err != nil || !validSigningKeyRecord(record) {
		return SigningKeyRecord{}, ErrSigningKey
	}
	record.PublicKey = append(ed25519.PublicKey(nil), record.PublicKey...)
	record.EncryptedPrivateKey = append([]byte(nil), record.EncryptedPrivateKey...)
	record.Nonce = append([]byte(nil), record.Nonce...)
	record.PreviousSignature = append([]byte(nil), record.PreviousSignature...)
	record.PossessionSignature = append([]byte(nil), record.PossessionSignature...)
	record.RotationPriorHead = append([]byte(nil), record.RotationPriorHead...)
	return record, nil
}

// InitializeSigningKeyState installs the immutable workspace root and active
// pointer. It must run in the same caller-owned transaction as root persistence.
func InitializeSigningKeyState(ctx context.Context, executor Executor, root SigningKeyRecord) error {
	if executor == nil || !validSigningKeyRecord(root) || root.Epoch != 1 || root.RetiredAt != nil {
		return ErrSigningKey
	}
	if _, err := executor.ExecContext(ctx, `INSERT INTO audit_signing_key_state_v1(
		workspace_id, root_key_id, active_key_id, active_epoch
	) VALUES (?, ?, ?, ?)`, root.WorkspaceID, root.KeyID, root.KeyID, root.Epoch); err != nil {
		return ErrSigningKey
	}
	return nil
}

// LoadSigningKeyState reads the exact workspace-wide active-key CAS tuple.
func LoadSigningKeyState(ctx context.Context, executor Executor, workspaceID string) (SigningKeyState, error) {
	if executor == nil || workspaceID == "" {
		return SigningKeyState{}, ErrSigningKey
	}
	rows, err := executor.QueryContext(ctx, `SELECT workspace_id, root_key_id, active_key_id, active_epoch
		FROM audit_signing_key_state_v1 WHERE workspace_id=?`, workspaceID)
	if err != nil {
		return SigningKeyState{}, ErrSigningKey
	}
	defer rows.Close()
	var state SigningKeyState
	if !rows.Next() || rows.Scan(&state.WorkspaceID, &state.RootKeyID, &state.ActiveKeyID, &state.ActiveEpoch) != nil ||
		rows.Next() || rows.Err() != nil || !validSigningKeyState(state) || state.WorkspaceID != workspaceID {
		return SigningKeyState{}, ErrSigningKey
	}
	return state, nil
}

// LoadSigningKeyHistory authenticates the complete persisted public lineage
// and requires its terminal key to match the workspace state pointer.
func LoadSigningKeyHistory(ctx context.Context, executor Executor, workspaceID string) ([]SigningKeyRecord, error) {
	if executor == nil || workspaceID == "" {
		return nil, ErrSigningKey
	}
	rows, err := executor.QueryContext(ctx, `SELECT key_id FROM audit_signing_keys_v1
		WHERE workspace_id=? ORDER BY epoch`, workspaceID)
	if err != nil {
		return nil, ErrSigningKey
	}
	var keyIDs []string
	for rows.Next() {
		var keyID string
		if rows.Scan(&keyID) != nil || keyID == "" || len(keyIDs) >= 1024 {
			_ = rows.Close()
			return nil, ErrSigningKey
		}
		keyIDs = append(keyIDs, keyID)
	}
	rowsErr := rows.Err()
	_ = rows.Close()
	if rowsErr != nil || len(keyIDs) == 0 {
		return nil, ErrSigningKey
	}
	history := make([]SigningKeyRecord, 0, len(keyIDs))
	for _, keyID := range keyIDs {
		record, loadErr := LoadSigningKey(ctx, executor, workspaceID, keyID)
		if loadErr != nil {
			return nil, ErrSigningKey
		}
		history = append(history, record)
	}
	state, err := LoadSigningKeyState(ctx, executor, workspaceID)
	if err != nil || state.ActiveEpoch != uint64(len(history)) || state.RootKeyID != history[0].KeyID ||
		state.ActiveKeyID != history[len(history)-1].KeyID {
		return nil, ErrSigningKey
	}
	if _, err := signingKeyChainFromRecords(history, history[len(history)-1]); err != nil {
		return nil, ErrSigningKey
	}
	for index := 1; index < len(history); index++ {
		link := signingKeyRotationLink(history[index-1], history[index])
		sequence := history[index].RotationSequence + 1
		events, loadErr := LoadStoredEvents(ctx, executor, workspaceID, history[index].Generation, sequence, sequence)
		if loadErr != nil || len(events) != 1 {
			return nil, ErrSigningKey
		}
		if _, matches := rotationEventMatchesLink(link, events[0].EventProto, events[0].PayloadProto); !matches {
			return nil, ErrSigningKey
		}
	}
	return history, nil
}

// RotateSigningKey authenticates the complete persisted lineage, stages an
// immutable successor and its typed rotation event, advances the workspace
// active-key tuple, and reserves mirror publication under one savepoint. The
// caller retains ownership of the outer transaction.
func (appender *Appender) RotateSigningKey(
	ctx context.Context,
	executor Executor,
	input SigningKeyRotationInput,
) (SigningKeyRotationResult, error) {
	transaction, ok := executor.(signingKeyRotationTransaction)
	if !ok || appender == nil || ctx == nil || ctx.Err() != nil || !validSigningKeyState(input.ExpectedState) ||
		!validSigningKeyRotationHeader(input.ExpectedHeader) ||
		input.ExpectedState.WorkspaceID != input.ExpectedHeader.WorkspaceID || len(input.DEK) != 32 ||
		!validAuditTimestamp(input.RotatedAt) || input.Random == nil || !validSigningKeyRotationEventTemplate(input.Event) ||
		input.Event.WorkspaceId != input.ExpectedState.WorkspaceID {
		return SigningKeyRotationResult{}, ErrSigningKey
	}
	state, err := LoadSigningKeyState(ctx, executor, input.ExpectedState.WorkspaceID)
	if err != nil || state != input.ExpectedState {
		return SigningKeyRotationResult{}, ErrSigningKey
	}
	header, err := LoadChainHeader(ctx, executor, input.ExpectedHeader.WorkspaceID, 0)
	if err != nil || !sameSigningKeyRotationHeader(header, input.ExpectedHeader) {
		return SigningKeyRotationResult{}, ErrSigningKey
	}
	history, err := LoadSigningKeyHistory(ctx, executor, input.ExpectedState.WorkspaceID)
	if err != nil || len(history) == 0 {
		return SigningKeyRotationResult{}, ErrSigningKey
	}
	current := history[len(history)-1]
	if current.KeyID != state.ActiveKeyID || current.Epoch != state.ActiveEpoch || current.RetiredAt != nil {
		return SigningKeyRotationResult{}, ErrSigningKey
	}
	retired, successor, link, err := createSigningKeySuccessor(current, input.DEK, header.Generation,
		header.CurrentSequence, header.CurrentHead, input.RotatedAt, input.Random)
	if err != nil {
		return SigningKeyRotationResult{}, err
	}
	linkDigest, err := signedSigningKeyRotationLinkDigest(link)
	if err != nil {
		return SigningKeyRotationResult{}, err
	}
	payload := &tammyv1.SigningKeyRotatedEvent{
		WorkspaceId: input.ExpectedState.WorkspaceID, Generation: header.Generation,
		SuccessorEpoch: successor.Epoch, PredecessorKeyId: current.KeyID, SuccessorKeyId: successor.KeyID,
		RotationLinkSha256: append([]byte(nil), linkDigest[:]...),
	}
	payloadProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(payload)
	if err != nil {
		return SigningKeyRotationResult{}, ErrSigningKey
	}
	event := proto.Clone(input.Event).(*tammyv1.AuditEvent)
	event.Generation = header.Generation
	event.Type = tammyv1.AuditEventType_AUDIT_EVENT_TYPE_SIGNING_KEY_ROTATED
	event.OccurredAt = timestamppb.New(input.RotatedAt.UTC())
	event.Payload = &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_SigningKeyRotated{
		SigningKeyRotated: payload,
	}}

	if _, err := transaction.ExecContext(ctx, `SAVEPOINT `+signingKeyRotationSavepoint); err != nil {
		return SigningKeyRotationResult{}, ErrSigningKey
	}
	var publication *guardedMirrorPublication
	abort := func(cause error) (SigningKeyRotationResult, error) {
		if publication != nil {
			publication.cancel()
		}
		_, rollbackErr := transaction.ExecContext(context.WithoutCancel(ctx),
			`ROLLBACK TO SAVEPOINT `+signingKeyRotationSavepoint)
		_, releaseErr := transaction.ExecContext(context.WithoutCancel(ctx),
			`RELEASE SAVEPOINT `+signingKeyRotationSavepoint)
		if rollbackErr != nil || releaseErr != nil {
			return SigningKeyRotationResult{}, ErrSigningKey
		}
		if cause == nil {
			cause = ErrSigningKey
		}
		return SigningKeyRotationResult{}, cause
	}
	result, err := transaction.ExecContext(ctx, `UPDATE audit_signing_keys_v1 SET retired_at=?
		WHERE workspace_id=? AND key_id=? AND generation=? AND epoch=? AND retired_at IS NULL`,
		formatTimestamp(input.RotatedAt), current.WorkspaceID, current.KeyID, current.Generation, current.Epoch)
	if err != nil {
		return abort(ErrSigningKey)
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return abort(ErrSigningKey)
	}
	if err := PersistSigningKey(ctx, transaction, successor); err != nil {
		return abort(err)
	}
	stored, expectedMirror, targetMirror, mirrorEpoch, err := appender.appendSigningKeyRotationEvent(
		ctx, transaction, event, payloadProto)
	if err != nil {
		return abort(err)
	}
	if _, matches := rotationEventMatchesLink(link, stored.EventProto, stored.PayloadProto); !matches {
		return abort(ErrSigningKey)
	}
	stateResult, err := transaction.ExecContext(ctx, `UPDATE audit_signing_key_state_v1
		SET active_key_id=?, active_epoch=?
		WHERE workspace_id=? AND root_key_id=? AND active_key_id=? AND active_epoch=?`,
		successor.KeyID, successor.Epoch, state.WorkspaceID, state.RootKeyID, state.ActiveKeyID, state.ActiveEpoch)
	if err != nil {
		return abort(ErrSigningKey)
	}
	stateCount, err := stateResult.RowsAffected()
	if err != nil || stateCount != 1 {
		return abort(ErrSigningKey)
	}
	publication, err = appender.publisher.registerGuardedAfterCommitAtEpoch(
		transaction, expectedMirror, targetMirror, mirrorEpoch)
	if err != nil {
		return abort(err)
	}
	if _, err := transaction.ExecContext(ctx, `RELEASE SAVEPOINT `+signingKeyRotationSavepoint); err != nil {
		return abort(ErrSigningKey)
	}
	publication.arm()
	return SigningKeyRotationResult{Retired: retired, Successor: successor,
		Link: proto.Clone(link).(*tammyv1.AuditSigningKeyRotationLink), StoredEvent: stored}, nil
}

func validSigningKeyRotationHeader(header ChainHeader) bool {
	if header.WorkspaceID == "" || header.Generation == 0 || header.Generation > math.MaxInt64 ||
		header.CurrentSequence > math.MaxInt64 || len(header.ChainSalt) != sha256.Size ||
		header.GenesisHash == ([sha256.Size]byte{}) || header.CurrentHead == ([sha256.Size]byte{}) ||
		!validAuditTimestamp(header.CreatedAt) {
		return false
	}
	wantGenesis, err := Genesis(header.WorkspaceID, header.ChainSalt)
	return err == nil && wantGenesis == header.GenesisHash &&
		(header.CurrentSequence != 0 || header.CurrentHead == header.GenesisHash)
}

func sameSigningKeyRotationHeader(left, right ChainHeader) bool {
	return left.WorkspaceID == right.WorkspaceID && left.Generation == right.Generation &&
		bytes.Equal(left.ChainSalt, right.ChainSalt) && left.GenesisHash == right.GenesisHash &&
		left.CurrentSequence == right.CurrentSequence && left.CurrentHead == right.CurrentHead &&
		left.CreatedAt.Equal(right.CreatedAt)
}

func validSigningKeyRotationEventTemplate(event *tammyv1.AuditEvent) bool {
	return event != nil && event.WorkspaceId != "" && event.Generation == 0 && event.Sequence == 0 &&
		event.Type == tammyv1.AuditEventType_AUDIT_EVENT_TYPE_UNSPECIFIED && event.OccurredAt == nil &&
		event.Payload == nil && len(event.PreviousHash) == 0 && len(event.EventHash) == 0
}

func LoadActiveSigningKey(ctx context.Context, executor Executor, workspaceID string) (SigningKeyRecord, error) {
	if executor == nil || workspaceID == "" {
		return SigningKeyRecord{}, ErrSigningKey
	}
	history, err := LoadSigningKeyHistory(ctx, executor, workspaceID)
	if err != nil {
		return SigningKeyRecord{}, ErrSigningKey
	}
	return cloneSigningKeyRecord(history[len(history)-1]), nil
}

func validSigningKeyState(state SigningKeyState) bool {
	return state.WorkspaceID != "" && state.RootKeyID != "" && state.ActiveKeyID != "" &&
		state.ActiveEpoch > 0 && state.ActiveEpoch <= math.MaxInt64
}

func nullRotationSequence(record SigningKeyRecord) any {
	if record.Epoch == 1 {
		return nil
	}
	return int64(record.RotationSequence)
}

func nullTimestamp(instant *time.Time) any {
	if instant == nil {
		return nil
	}
	return formatTimestamp(*instant)
}

func deriveSigningKeyEncryptionKey(workspaceID string, dek []byte) ([]byte, error) {
	key, err := hkdf.Key(sha256.New, dek, []byte(workspaceID), "tammy.audit.export-signing-key.v1", 32)
	if err != nil {
		return nil, fmt.Errorf("%w: derive encryption key", ErrSigningKey)
	}
	return key, nil
}

func signingKeyAAD(workspaceID, keyID string, publicKey []byte) []byte {
	aad := make([]byte, 0, len(workspaceID)+len(keyID)+len(publicKey)+48)
	aad = append(aad, "tammy.audit.signing-key.v1\x00"...)
	aad = append(aad, workspaceID...)
	aad = append(aad, 0)
	aad = append(aad, keyID...)
	aad = append(aad, 0)
	aad = append(aad, publicKey...)
	return aad
}

func validSigningKeyRecord(record SigningKeyRecord) bool {
	if record.KeyID == "" || record.WorkspaceID == "" || record.Generation == 0 || record.Epoch == 0 ||
		record.Generation > math.MaxInt64 || record.Epoch > math.MaxInt64 || record.RotationSequence > math.MaxInt64 ||
		len(record.PublicKey) != ed25519.PublicKeySize || len(record.EncryptedPrivateKey) != signingKeyCiphertextSize ||
		len(record.Nonce) != 12 || record.EncryptionAlgorithm != keyEncryptionAlgorithm || record.SigningAlgorithm != "Ed25519" ||
		!validAuditTimestamp(record.CreatedAt) || record.RetiredAt != nil &&
		(!validAuditTimestamp(*record.RetiredAt) || !record.RetiredAt.After(record.CreatedAt)) {
		return false
	}
	if record.Epoch == 1 {
		return record.PreviousKeyID == "" && len(record.PreviousSignature) == 0 && len(record.PossessionSignature) == 0 &&
			record.RotationSequence == 0 && len(record.RotationPriorHead) == 0
	}
	return record.PreviousKeyID != "" && len(record.PreviousSignature) == ed25519.SignatureSize &&
		len(record.PossessionSignature) == ed25519.SignatureSize && len(record.RotationPriorHead) == sha256.Size
}

// Zero clears mutable secret buffers where the Go runtime permits.
func Zero(secret []byte) {
	for index := range secret {
		secret[index] = 0
	}
}
