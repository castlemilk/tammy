//go:build !darwin || !arm64 || !cgo

package sbrhelper

import (
	"errors"
	"testing"
)

func TestSandboxAuthorityUnsupportedPlatformFailsClosed(t *testing.T) {
	profile, guard, err := RenderDevelopmentSandboxProfile(SandboxProfileInput{})
	if !errors.Is(err, ErrSandboxProfileInvalid) || guard != nil {
		t.Fatalf("profile=%#v guard=%#v error=%v", profile, guard, err)
	}
	if _, err := profile.PrepareSpawn(); !errors.Is(err, ErrSandboxProfileInvalid) {
		t.Fatalf("prepare error=%v", err)
	}
}
