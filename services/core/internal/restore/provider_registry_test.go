package restore

import (
	"context"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/backup"
)

type validatorFunc func(context.Context, ValidationInput) error

func (function validatorFunc) Validate(ctx context.Context, input ValidationInput) error {
	return function(ctx, input)
}

func TestProviderRegistryDispatchesExactImmutableVersion(t *testing.T) {
	called := 0
	registry, err := NewProviderRegistry([]ProviderRegistration{{
		Name: "accounting", Version: 1, Validator: validatorFunc(func(_ context.Context, input ValidationInput) error {
			called++
			if len(input.Objects) != 1 || input.Objects[0].Path != "providers/accounting/ledger.pb" {
				t.Fatalf("validator objects = %#v", input.Objects)
			}
			input.Objects[0].Bytes[0] = 'X'
			return nil
		}),
	}})
	if err != nil {
		t.Fatalf("NewProviderRegistry() error = %v", err)
	}
	objects := []backup.Object{{Path: "providers/accounting/ledger.pb", Provider: "accounting", ProviderVersion: 1, Bytes: []byte("ledger")}}
	if err := registry.Validate(context.Background(), objects); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if called != 1 || string(objects[0].Bytes) != "ledger" {
		t.Fatalf("called = %d, caller bytes = %q", called, objects[0].Bytes)
	}
	objects[0].ProviderVersion = 2
	if err := registry.Validate(context.Background(), objects); err == nil {
		t.Fatal("unregistered provider version was accepted")
	}
}
