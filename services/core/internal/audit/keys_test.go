package audit

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

type signingKeyQueryCapture struct {
	query     string
	arguments []any
}

func (*signingKeyQueryCapture) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errors.New("unexpected exec")
}

func (capture *signingKeyQueryCapture) QueryContext(_ context.Context, query string, arguments ...any) (*sql.Rows, error) {
	capture.query = query
	capture.arguments = append([]any(nil), arguments...)
	return nil, errors.New("captured query")
}

func TestSigningKeyLifecycleEncryptsPrivateKeyUnderWorkspaceDEK(t *testing.T) {
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	dek := bytes.Repeat([]byte{0x41}, 32)
	random := bytes.NewReader(bytes.Repeat([]byte{0x52}, 128))
	record, header, err := GenerateSigningKey(workspaceID, dek,
		time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC), random)
	if err != nil {
		t.Fatal(err)
	}
	if record.KeyID == "" || record.KeyID != header.KeyID || len(header.PublicKey) != ed25519.PublicKeySize ||
		!bytes.Equal(record.PublicKey, header.PublicKey) || len(record.Nonce) != 12 || len(record.EncryptedPrivateKey) <= ed25519.PrivateKeySize {
		t.Fatalf("key record/header = %#v / %#v", record, header)
	}
	privateKey, err := DecryptSigningKey(record, dek)
	if err != nil {
		t.Fatal(err)
	}
	defer Zero(privateKey)
	if !bytes.Equal(privateKey.Public().(ed25519.PublicKey), header.PublicKey) ||
		bytes.Contains(record.EncryptedPrivateKey, privateKey) {
		t.Fatal("encrypted record exposes or does not match private key")
	}
	manifestHash := sha256.Sum256([]byte("canonical manifest"))
	signature, err := SignManifestHash(record, dek, manifestHash)
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(header.PublicKey, manifestHash[:], signature) {
		t.Fatal("signature does not verify with header public key")
	}
	if _, err := DecryptSigningKey(record, bytes.Repeat([]byte{0x42}, 32)); !errors.Is(err, ErrSigningKey) {
		t.Fatalf("wrong DEK error = %v, want ErrSigningKey", err)
	}
}

func TestGenerateSigningKeyRejectsOutOfRangeTimestamp(t *testing.T) {
	if _, _, err := GenerateSigningKey(
		"01890f60-4d6d-7c12-8f02-6c9129d5b001",
		bytes.Repeat([]byte{0x41}, 32),
		time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC),
		bytes.NewReader(bytes.Repeat([]byte{0x52}, 128)),
	); !errors.Is(err, ErrSigningKey) {
		t.Fatalf("GenerateSigningKey error=%v, want ErrSigningKey", err)
	}
}

func TestSigningKeyRecordRejectsOverlongEncryptedPrivateKey(t *testing.T) {
	record, _, err := GenerateSigningKey(
		"01890f60-4d6d-7c12-8f02-6c9129d5b001",
		bytes.Repeat([]byte{0x41}, 32),
		time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC),
		bytes.NewReader(bytes.Repeat([]byte{0x52}, 128)),
	)
	if err != nil {
		t.Fatal(err)
	}
	record.EncryptedPrivateKey = append(record.EncryptedPrivateKey, 0xff)
	if validSigningKeyRecord(record) {
		t.Fatalf("accepted %d-byte encrypted Ed25519 private key", len(record.EncryptedPrivateKey))
	}
}

func TestLoadSigningKeyFiltersNonCanonicalCiphertextBeforeReadingBlob(t *testing.T) {
	capture := new(signingKeyQueryCapture)
	_, _ = LoadSigningKey(context.Background(), capture,
		"01890f60-4d6d-7c12-8f02-6c9129d5b001", "01890f60-4d6d-7c12-8f02-6c9129d5b002")
	if !strings.Contains(capture.query, "length(encrypted_private_key) = ?") {
		t.Fatalf("LoadSigningKey query lacks canonical ciphertext predicate: %q", capture.query)
	}
	if len(capture.arguments) != 3 || capture.arguments[2] != ed25519.PrivateKeySize+16 {
		t.Fatalf("LoadSigningKey query arguments=%#v, want exact 80-byte boundary", capture.arguments)
	}
}
