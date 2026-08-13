package identity

import (
	"crypto/hmac"
	"crypto/sha1" // RFC 6238's interoperable default algorithm.
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"
)

var (
	ErrTOTPInvalid = errors.New("identity: invalid TOTP challenge")
	ErrTOTPReplay  = errors.New("identity: TOTP counter replay")
)

func GenerateTOTPSecret(randomSource io.Reader) ([]byte, []byte, error) {
	if randomSource == nil {
		return nil, nil, ErrTOTPInvalid
	}
	secret := make([]byte, 20)
	if _, err := io.ReadFull(randomSource, secret); err != nil {
		zero(secret)
		return nil, nil, fmt.Errorf("identity: generate TOTP secret: %w", err)
	}
	display := []byte(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret))
	return secret, display, nil
}

func TOTPCode(secret []byte, instant time.Time) string {
	return totpAtCounter(secret, instant.UTC().Unix()/30)
}

// VerifyTOTP accepts RFC 6238's current counter and one adjacent step. Every
// accepted counter must be strictly newer than the retained counter.
func VerifyTOTP(secret []byte, code string, instant time.Time, lastCounter int64) (int64, error) {
	if len(secret) < 16 || len(code) != 6 {
		return 0, ErrTOTPInvalid
	}
	for _, value := range code {
		if value < '0' || value > '9' {
			return 0, ErrTOTPInvalid
		}
	}
	current := instant.UTC().Unix() / 30
	matchedCounter := int64(-1)
	for _, counter := range []int64{current, current - 1, current + 1} {
		expected := totpAtCounter(secret, counter)
		if subtle.ConstantTimeCompare([]byte(expected), []byte(code)) == 1 {
			matchedCounter = counter
		}
	}
	if matchedCounter < 0 {
		return 0, ErrTOTPInvalid
	}
	if matchedCounter <= lastCounter {
		return 0, ErrTOTPReplay
	}
	return matchedCounter, nil
}

func totpAtCounter(secret []byte, counter int64) string {
	var message [8]byte
	binary.BigEndian.PutUint64(message[:], uint64(counter))
	digest := hmac.New(sha1.New, secret)
	_, _ = digest.Write(message[:])
	sum := digest.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", value%1_000_000)
}

func zero(secret []byte) {
	for index := range secret {
		secret[index] = 0
	}
}
