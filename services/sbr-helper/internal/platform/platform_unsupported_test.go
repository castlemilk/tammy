//go:build !darwin || !arm64 || !cgo

package platform

import (
	"errors"
	"testing"
)

func TestUnsupportedPlatformAuthorityFailsClosed(t *testing.T) {
	if _, err := NewBookmarkResolver().Resolve([]byte("bookmark"), "/tmp/credential.p12"); !errors.Is(err, ErrBookmarkInvalid) {
		t.Fatalf("bookmark error = %v", err)
	}
}
