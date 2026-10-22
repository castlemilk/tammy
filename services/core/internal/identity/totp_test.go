package identity

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

func TestTOTPWindowAndReplay(t *testing.T) {
	secret := []byte("12345678901234567890")
	now := time.Unix(1_234_567_890, 0).UTC()
	code := TOTPCode(secret, now)
	counter, err := VerifyTOTP(secret, code, now.Add(30*time.Second), -1)
	if err != nil {
		t.Fatal(err)
	}
	if counter != now.Unix()/30 {
		t.Fatalf("counter = %d", counter)
	}
	if _, err := VerifyTOTP(secret, code, now, counter); !errors.Is(err, ErrTOTPReplay) {
		t.Fatalf("replay returned %v", err)
	}
	if _, err := VerifyTOTP(secret, "000000", now, -1); !errors.Is(err, ErrTOTPInvalid) {
		t.Fatalf("invalid code returned %v", err)
	}
}

func TestGenerateTOTPSecret(t *testing.T) {
	secret, display, err := GenerateTOTPSecret(bytes.NewReader(bytes.Repeat([]byte{0x42}, 20)))
	if err != nil {
		t.Fatal(err)
	}
	defer zero(secret)
	defer zero(display)
	if len(secret) != 20 || string(display) != "IJBEEQSCIJBEEQSCIJBEEQSCIJBEEQSC" {
		t.Fatalf("unexpected provisioning material")
	}
}
