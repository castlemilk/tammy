// Package restore owns staged workspace validation and activation recovery.
package restore

import (
	"context"
	"errors"
	"reflect"
	"regexp"
	"sort"

	"github.com/tammyapp/tammy/services/core/internal/backup"
)

var ErrProviderRegistry = errors.New("restore: invalid provider registry")

var providerNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

type ValidationInput struct {
	Provider string
	Version  uint32
	Objects  []backup.Object
}

type Validator interface {
	Validate(context.Context, ValidationInput) error
}

type ProviderRegistration struct {
	Name      string
	Version   uint32
	Validator Validator
}

type providerKey struct {
	name    string
	version uint32
}

// ProviderRegistry is immutable and dispatches an archive projection only to
// the validator registered for its exact provider/version pair.
type ProviderRegistry struct {
	validators map[providerKey]Validator
}

func NewProviderRegistry(registrations []ProviderRegistration) (*ProviderRegistry, error) {
	if len(registrations) == 0 || len(registrations) > 10_000 {
		return nil, ErrProviderRegistry
	}
	validators := make(map[providerKey]Validator, len(registrations))
	for _, registration := range registrations {
		key := providerKey{name: registration.Name, version: registration.Version}
		if !providerNamePattern.MatchString(key.name) || key.version == 0 || nilInterface(registration.Validator) {
			return nil, ErrProviderRegistry
		}
		if _, duplicate := validators[key]; duplicate {
			return nil, ErrProviderRegistry
		}
		validators[key] = registration.Validator
	}
	return &ProviderRegistry{validators: validators}, nil
}

func (registry *ProviderRegistry) Validate(ctx context.Context, objects []backup.Object) error {
	if registry == nil || len(registry.validators) == 0 || ctx == nil || len(objects) == 0 || len(objects) > 10_000 {
		return ErrProviderRegistry
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrProviderRegistry, err)
	}
	groups := make(map[providerKey][]backup.Object)
	for _, object := range objects {
		key := providerKey{name: object.Provider, version: object.ProviderVersion}
		if _, registered := registry.validators[key]; !registered || object.Path == "" || len(object.Bytes) > 256*1024*1024 {
			return ErrProviderRegistry
		}
		owned := object
		owned.Bytes = append([]byte(nil), object.Bytes...)
		groups[key] = append(groups[key], owned)
	}
	keys := make([]providerKey, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool {
		if keys[left].name == keys[right].name {
			return keys[left].version < keys[right].version
		}
		return keys[left].name < keys[right].name
	})
	for _, key := range keys {
		input := ValidationInput{Provider: key.name, Version: key.version, Objects: groups[key]}
		sort.Slice(input.Objects, func(left, right int) bool { return input.Objects[left].Path < input.Objects[right].Path })
		if err := registry.validators[key].Validate(ctx, input); err != nil || ctx.Err() != nil {
			return ErrProviderRegistry
		}
	}
	return nil
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
