package vault

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

type ProductIDState uint8

const (
	ProductIDMissing ProductIDState = iota
	ProductIDPresent
	ProductIDInaccessible
)

type ProductScope struct{ Product, Service string }
type ProductIDMutation struct {
	OperationID string
	Scope       ProductScope
	Value       []byte
}
type ProductIDStatusResult struct {
	State       ProductIDState
	Fingerprint string
}

func (v *Vault) productAccount(scope ProductScope) (string, error) {
	if v.closed {
		return "", ErrVaultInaccessible
	}
	if !validProductField(scope.Product) || !validProductField(scope.Service) {
		return "", ErrVaultInvalidInput
	}
	mac := hmac.New(sha256.New, v.installationKey)
	_, _ = mac.Write([]byte("EVTE"))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(scope.Product))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(scope.Service))
	return v.prefix() + "product-id/" + hex.EncodeToString(mac.Sum(nil)), nil
}

func validProductField(value string) bool {
	return value != "" && len(value) <= 128 && !strings.ContainsRune(value, 0)
}

func (v *Vault) stageProductID(mutation ProductIDMutation) (ProductIDStatusResult, error) {
	defer clear(mutation.Value)
	if !validOperationID(mutation.OperationID) || len(mutation.Value) == 0 || len(mutation.Value) > maximumPassword {
		return ProductIDStatusResult{}, ErrVaultInvalidInput
	}
	account, err := v.productAccount(mutation.Scope)
	if err != nil {
		return ProductIDStatusResult{}, err
	}
	exists, err := v.exists(account)
	if err != nil {
		return ProductIDStatusResult{}, err
	}
	if exists {
		return ProductIDStatusResult{}, ErrVaultCollision
	}
	fingerprint := v.productFingerprint(mutation.Scope, mutation.Value)
	productCopy := append([]byte(nil), mutation.Value...)
	defer clear(productCopy)
	plain := encodeFields([][]byte{productCopy, []byte(fingerprint)})
	defer clear(plain)
	scopeDigest := strings.TrimPrefix(account, v.prefix()+"product-id/")
	envelope, err := v.seal(scopeDigest, ProductIDKind, "product-id-v1", plain)
	if err != nil {
		return ProductIDStatusResult{}, err
	}
	defer clear(envelope)
	if err := v.stagePending(pendingMutation{OperationID: mutation.OperationID, Action: PendingCreate, Kind: ProductIDKind, Account: account, Envelope: envelope}); err != nil {
		return ProductIDStatusResult{}, err
	}
	return ProductIDStatusResult{State: ProductIDPresent, Fingerprint: fingerprint}, nil
}

func (v *Vault) ProductIDStatus(scope ProductScope) (ProductIDStatusResult, error) {
	account, err := v.productAccount(scope)
	if err != nil {
		return ProductIDStatusResult{}, err
	}
	envelope, err := v.store.Read(account)
	if errors.Is(err, ErrVaultMissing) {
		return ProductIDStatusResult{State: ProductIDMissing}, err
	}
	if errors.Is(err, ErrVaultInaccessible) {
		return ProductIDStatusResult{State: ProductIDInaccessible}, err
	}
	if err != nil {
		return ProductIDStatusResult{}, err
	}
	defer clear(envelope)
	scopeDigest := strings.TrimPrefix(account, v.prefix()+"product-id/")
	plain, version, err := v.open(scopeDigest, ProductIDKind, envelope)
	if err != nil || version != "product-id-v1" {
		clear(plain)
		if err != nil {
			return ProductIDStatusResult{}, err
		}
		return ProductIDStatusResult{}, ErrVaultAuthentication
	}
	defer clear(plain)
	fields, err := decodeFields(plain, 2)
	if err != nil || len(fields[0]) == 0 || len(fields[0]) > maximumPassword || string(fields[1]) != v.productFingerprint(scope, fields[0]) {
		return ProductIDStatusResult{}, ErrVaultAuthentication
	}
	return ProductIDStatusResult{State: ProductIDPresent, Fingerprint: string(fields[1])}, nil
}

func (v *Vault) productFingerprint(scope ProductScope, value []byte) string {
	mac := hmac.New(sha256.New, v.installationKey)
	_, _ = mac.Write([]byte("tammy-sbr-product-id-fingerprint-v1"))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(scope.Product))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(scope.Service))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(value)
	return hex.EncodeToString(mac.Sum(nil))
}
