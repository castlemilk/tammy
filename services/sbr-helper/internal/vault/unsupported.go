//go:build !darwin || !cgo

package vault

type KeychainStore struct{}

func newProductionKeychainStore(string) (*KeychainStore, error) {
	return nil, ErrVaultUnsupported
}
func newDevelopmentKeychainStore(string) (*KeychainStore, error) {
	return nil, ErrVaultUnsupported
}
func (*KeychainStore) Read(string) ([]byte, error)                { return nil, ErrVaultUnsupported }
func (*KeychainStore) Create(string, []byte, AccessPolicy) error  { return ErrVaultUnsupported }
func (*KeychainStore) Replace(string, []byte, AccessPolicy) error { return ErrVaultUnsupported }
func (*KeychainStore) Delete(string) error                        { return ErrVaultUnsupported }
func (*KeychainStore) CompareAndReplace(string, string, []byte, AccessPolicy) error {
	return ErrVaultUnsupported
}
func (*KeychainStore) CompareAndDelete(string, string) error { return ErrVaultUnsupported }

var _ Store = (*KeychainStore)(nil)
