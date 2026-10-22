//go:build !darwin && !windows

package workspace

func newPlatformSecretStore() (SecretStore, error) {
	return nil, ErrRememberedKeyUnavailable
}
