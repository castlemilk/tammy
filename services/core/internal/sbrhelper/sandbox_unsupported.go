//go:build !darwin || !arm64 || !cgo

package sbrhelper

import (
	"context"
	"errors"
	"io/fs"
)

var (
	ErrSandboxProfileInvalid   = errors.New("SBR_SANDBOX_PROFILE_INVALID")
	ErrSandboxAuthorityChanged = errors.New("SBR_SANDBOX_AUTHORITY_CHANGED")
	ErrSandboxAuthorityClosed  = errors.New("SBR_SANDBOX_AUTHORITY_CLOSED")
)

type SandboxProfileInput struct {
	TrustedBase         string
	StagedRoot          string
	StagedExecutables   []string
	StagedReadOnlyFiles []string
	SelectedReadFiles   []string
}
type SandboxProfile struct{ guard *SandboxProfileGuard }
type SandboxProfileGuard struct{}

func RenderDevelopmentSandboxProfile(SandboxProfileInput) (SandboxProfile, *SandboxProfileGuard, error) {
	return SandboxProfile{}, nil, ErrSandboxProfileInvalid
}
func RenderDevelopmentSandboxProfileContext(context.Context, SandboxProfileInput) (SandboxProfile, *SandboxProfileGuard, error) {
	return SandboxProfile{}, nil, ErrSandboxProfileInvalid
}
func (SandboxProfile) PrepareSpawn() (string, error) { return "", ErrSandboxProfileInvalid }
func (SandboxProfile) PrepareSpawnContext(context.Context) (string, error) {
	return "", ErrSandboxProfileInvalid
}
func (SandboxProfile) FileMode() fs.FileMode   { return 0 }
func (SandboxProfile) OwnerUID() int           { return -1 }
func (*SandboxProfileGuard) Revalidate() error { return ErrSandboxProfileInvalid }
func (*SandboxProfileGuard) RevalidateContext(context.Context) error {
	return ErrSandboxProfileInvalid
}
func (*SandboxProfileGuard) Close() error { return nil }
