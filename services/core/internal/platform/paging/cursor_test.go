package paging_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
)

func TestSignedCursorIsDeterministicOpaqueAndRoundTrips(t *testing.T) {
	codec, err := paging.NewCodec(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	cursor := paging.Cursor{
		Snapshot:  "financial-revision:42",
		Position:  "account:01890f3c-7b2e-7cc4-98c4-dc0c0c07398f",
		QueryHash: sha256.Sum256([]byte("organisation=stable&status=active")),
	}
	first, err := codec.Encode(cursor)
	if err != nil {
		t.Fatal(err)
	}
	second, err := codec.Encode(cursor)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || strings.Contains(first, "=") || strings.Contains(first, cursor.Snapshot) {
		t.Fatalf("cursor is not deterministic opaque unpadded Base64URL: %q / %q", first, second)
	}
	decoded, err := codec.Decode(first, cursor.QueryHash)
	if err != nil {
		t.Fatal(err)
	}
	if decoded != cursor {
		t.Fatalf("decoded = %#v, want %#v", decoded, cursor)
	}
}

func TestSignedCursorRejectsTamperingWrongKeyAndNonCanonicalEncoding(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 32)
	codec, err := paging.NewCodec(key)
	if err != nil {
		t.Fatal(err)
	}
	token, err := codec.Encode(paging.Cursor{
		Snapshot:  "revision:1",
		Position:  "line:a",
		QueryHash: sha256.Sum256([]byte("query")),
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 1
	tampered := base64.RawURLEncoding.EncodeToString(raw)
	wrongCodec, err := paging.NewCodec(bytes.Repeat([]byte{0x24}, 32))
	if err != nil {
		t.Fatal(err)
	}

	for name, candidate := range map[string]string{
		"tampered":       tampered,
		"padded":         token + "=",
		"truncated":      token[:len(token)-1],
		"oversized":      strings.Repeat("a", 2049),
		"invalid_base64": "not+a+cursor",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := codec.Decode(candidate, sha256.Sum256([]byte("query"))); !errors.Is(err, paging.ErrInvalidCursor) || err.Error() != paging.ErrInvalidCursor.Error() {
				t.Fatalf("error = %v, want stable %v", err, paging.ErrInvalidCursor)
			}
		})
	}
	if _, err := wrongCodec.Decode(token, sha256.Sum256([]byte("query"))); !errors.Is(err, paging.ErrInvalidCursor) {
		t.Fatalf("wrong-key error = %v, want %v", err, paging.ErrInvalidCursor)
	}
}

func TestSignedCursorRejectsDifferentOrMissingExpectedQueryHash(t *testing.T) {
	codec, err := paging.NewCodec(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	queryA := sha256.Sum256([]byte("organisation=a"))
	queryB := sha256.Sum256([]byte("organisation=b"))
	token, err := codec.Encode(paging.Cursor{
		Snapshot:  "revision:1",
		Position:  "line:a",
		QueryHash: queryA,
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, expected := range map[string][sha256.Size]byte{
		"different_query": queryB,
		"missing_query":   {},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := codec.Decode(token, expected); !errors.Is(err, paging.ErrInvalidCursor) || err.Error() != paging.ErrInvalidCursor.Error() {
				t.Fatalf("error = %v, want stable %v", err, paging.ErrInvalidCursor)
			}
		})
	}
}

func TestSignedCursorRejectsWeakKeysAndInvalidFields(t *testing.T) {
	if _, err := paging.NewCodec([]byte("secret")); !errors.Is(err, paging.ErrInvalidKey) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("weak key error = %v", err)
	}
	codec, err := paging.NewCodec(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	for _, cursor := range []paging.Cursor{
		{Position: "line:a", QueryHash: sha256.Sum256([]byte("query"))},
		{Snapshot: "revision:1", Position: "line:a"},
		{Snapshot: "revision:1", Position: strings.Repeat("x", 1025), QueryHash: sha256.Sum256([]byte("query"))},
		{Snapshot: "revision:1", Position: string([]byte{0xff}), QueryHash: sha256.Sum256([]byte("query"))},
	} {
		if _, err := codec.Encode(cursor); !errors.Is(err, paging.ErrInvalidCursor) {
			t.Fatalf("invalid cursor error = %v, want %v", err, paging.ErrInvalidCursor)
		}
	}
}

func TestCodecDefensivelyCopiesSigningKey(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 32)
	codec, err := paging.NewCodec(key)
	if err != nil {
		t.Fatal(err)
	}
	key[0] ^= 0xff
	reference, err := paging.NewCodec(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	cursor := paging.Cursor{
		Snapshot:  "revision:1",
		Position:  "line:a",
		QueryHash: sha256.Sum256([]byte("query")),
	}
	got, err := codec.Encode(cursor)
	if err != nil {
		t.Fatal(err)
	}
	want, err := reference.Encode(cursor)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatal("codec retained caller-owned key storage")
	}
}
