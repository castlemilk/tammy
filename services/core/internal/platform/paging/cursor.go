// Package paging encodes deterministic signed opaque pagination cursors.
package paging

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"unicode/utf8"
)

var (
	ErrInvalidCursor = errors.New("invalid cursor")
	ErrInvalidKey    = errors.New("invalid cursor signing key")
)

const (
	cursorVersion       = byte(1)
	maximumComponentLen = 512
	maximumTokenLen     = 2048
	minimumKeyLen       = 32
	signatureLen        = sha256.Size
)

// Cursor binds a stable snapshot and position to one normalized query hash.
type Cursor struct {
	Snapshot  string
	Position  string
	QueryHash [sha256.Size]byte
}

// Codec signs and verifies deterministic opaque cursor tokens.
type Codec struct {
	key []byte
}

// NewCodec creates a cursor codec with a caller-owned signing key.
func NewCodec(key []byte) (*Codec, error) {
	if len(key) < minimumKeyLen {
		return nil, ErrInvalidKey
	}
	return &Codec{key: append([]byte(nil), key...)}, nil
}

// Encode returns canonical unpadded Base64URL over a versioned payload and HMAC.
func (codec *Codec) Encode(cursor Cursor) (string, error) {
	if !validCursor(cursor) {
		return "", ErrInvalidCursor
	}
	payload := make([]byte, 0, 1+2+len(cursor.Snapshot)+2+len(cursor.Position)+sha256.Size)
	payload = append(payload, cursorVersion)
	payload = appendUint16(payload, uint16(len(cursor.Snapshot)))
	payload = append(payload, cursor.Snapshot...)
	payload = appendUint16(payload, uint16(len(cursor.Position)))
	payload = append(payload, cursor.Position...)
	payload = append(payload, cursor.QueryHash[:]...)
	signature := sign(codec.key, payload)
	raw := append(payload, signature...)
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// Decode verifies a cursor before returning its bound snapshot, position, and query hash.
func (codec *Codec) Decode(token string) (Cursor, error) {
	if token == "" || len(token) > maximumTokenLen {
		return Cursor{}, ErrInvalidCursor
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || base64.RawURLEncoding.EncodeToString(raw) != token || len(raw) <= signatureLen {
		return Cursor{}, ErrInvalidCursor
	}
	payload := raw[:len(raw)-signatureLen]
	signature := raw[len(raw)-signatureLen:]
	if !hmac.Equal(signature, sign(codec.key, payload)) {
		return Cursor{}, ErrInvalidCursor
	}
	cursor, ok := decodePayload(payload)
	if !ok || !validCursor(cursor) {
		return Cursor{}, ErrInvalidCursor
	}
	return cursor, nil
}

func decodePayload(payload []byte) (Cursor, bool) {
	if len(payload) < 1+2+2+sha256.Size || payload[0] != cursorVersion {
		return Cursor{}, false
	}
	offset := 1
	snapshot, next, ok := readString(payload, offset)
	if !ok {
		return Cursor{}, false
	}
	position, next, ok := readString(payload, next)
	if !ok || len(payload)-next != sha256.Size {
		return Cursor{}, false
	}
	var queryHash [sha256.Size]byte
	copy(queryHash[:], payload[next:])
	return Cursor{Snapshot: snapshot, Position: position, QueryHash: queryHash}, true
}

func readString(payload []byte, offset int) (string, int, bool) {
	if len(payload)-offset < 2 {
		return "", 0, false
	}
	length := int(binary.BigEndian.Uint16(payload[offset : offset+2]))
	offset += 2
	if length > maximumComponentLen || len(payload)-offset < length {
		return "", 0, false
	}
	return string(payload[offset : offset+length]), offset + length, true
}

func validCursor(cursor Cursor) bool {
	if cursor.Snapshot == "" || cursor.Position == "" ||
		len(cursor.Snapshot) > maximumComponentLen || len(cursor.Position) > maximumComponentLen ||
		!utf8.ValidString(cursor.Snapshot) || !utf8.ValidString(cursor.Position) {
		return false
	}
	return cursor.QueryHash != [sha256.Size]byte{}
}

func appendUint16(destination []byte, value uint16) []byte {
	return append(destination, byte(value>>8), byte(value))
}

func sign(key, payload []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}
