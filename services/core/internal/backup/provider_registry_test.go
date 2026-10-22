package backup

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"
)

// SnapshotReader is deliberately an empty test compatibility value. Production
// Provider has no reader parameter or query method.
type SnapshotReader struct{}

type providerFunc func(context.Context, SnapshotReader, SnapshotRequest) (Projection, error)

func (function providerFunc) ValidateSnapshot(ctx context.Context, request SnapshotRequest, projection Projection) error {
	expected, err := function(ctx, SnapshotReader{}, request)
	if err != nil || !reflect.DeepEqual(expected, projection) {
		return ErrProviderRegistry
	}
	return nil
}

type projectionSourceFunc func(context.Context, SnapshotRequest) (Projection, error)

func (function projectionSourceFunc) SnapshotProjection(
	ctx context.Context,
	request SnapshotRequest,
) (Projection, error) {
	return function(ctx, request)
}

type providerValidatorFunc func(context.Context, SnapshotRequest, Projection) error

func (function providerValidatorFunc) ValidateSnapshot(ctx context.Context, request SnapshotRequest, projection Projection) error {
	return function(ctx, request, projection)
}

func TestProviderSurfaceCannotIssueRawQueries(t *testing.T) {
	provider := reflect.TypeOf((*Provider)(nil)).Elem()
	method, ok := provider.MethodByName("ValidateSnapshot")
	if !ok {
		t.Fatal("Provider has no validation method")
	}
	for index := 1; index < method.Type.NumIn(); index++ {
		parameter := method.Type.In(index)
		if _, exposed := parameter.MethodByName("QueryContext"); exposed {
			t.Fatalf("Provider.ValidateSnapshot parameter %d exposes raw QueryContext: %v", index, parameter)
		}
	}
}

func TestProjectionSourceIsRegistrationScoped(t *testing.T) {
	sourceType := reflect.TypeOf((*ProjectionSource)(nil)).Elem()
	method, ok := sourceType.MethodByName("SnapshotProjection")
	if !ok || method.Type.NumIn() != 2 || method.Type.In(1) != reflect.TypeOf(SnapshotRequest{}) {
		t.Fatalf("ProjectionSource can select arbitrary module data: %#v", method)
	}
	registry, err := NewProviderRegistry([]ProviderRegistration{{Name: "rules", Version: 2,
		Provider: providerFunc(func(_ context.Context, _ SnapshotReader, _ SnapshotRequest) (Projection, error) {
			return Projection{Objects: []Object{{Path: "rules/current.pb", Bytes: []byte("rules")}}}, nil
		})}})
	if err != nil {
		t.Fatal(err)
	}
	called := false
	_, err = registry.Collect(context.Background(), []ProjectionSourceRegistration{{Name: "rules", Version: 3,
		Source: projectionSourceFunc(func(context.Context, SnapshotRequest) (Projection, error) {
			called = true
			return Projection{}, nil
		})}}, SnapshotRequest{WorkspaceID: "018f0000-0000-7000-8000-000000000001"})
	if err == nil || called {
		t.Fatalf("version-mismatched source used: called=%t error=%v", called, err)
	}
}

func TestProviderRegistryOwnsDeterministicOrderingAndCopies(t *testing.T) {
	registrations := []ProviderRegistration{
		{Name: "rules", Version: 2, Provider: providerFunc(func(_ context.Context, _ SnapshotReader, _ SnapshotRequest) (Projection, error) {
			return Projection{Objects: []Object{{Path: "rules/current.pb", Bytes: []byte("rules")}}}, nil
		})},
		{Name: "accounting", Version: 1, Provider: providerFunc(func(_ context.Context, _ SnapshotReader, _ SnapshotRequest) (Projection, error) {
			return Projection{Objects: []Object{{Path: "providers/accounting/ledger.pb", Bytes: []byte("ledger")}}}, nil
		})},
	}
	registry, err := NewProviderRegistry(registrations)
	if err != nil {
		t.Fatalf("NewProviderRegistry() error = %v", err)
	}
	registrations[0].Name = "mutated"

	sources := []ProjectionSourceRegistration{
		{Name: "accounting", Version: 1, Source: projectionSourceFunc(func(_ context.Context, _ SnapshotRequest) (Projection, error) {
			return Projection{Objects: []Object{{Path: "providers/accounting/ledger.pb", Bytes: []byte("ledger")}}}, nil
		})},
		{Name: "rules", Version: 2, Source: projectionSourceFunc(func(_ context.Context, _ SnapshotRequest) (Projection, error) {
			return Projection{Objects: []Object{{Path: "rules/current.pb", Bytes: []byte("rules")}}}, nil
		})},
	}
	objects, err := registry.Collect(context.Background(), sources, SnapshotRequest{WorkspaceID: "018f0000-0000-7000-8000-000000000001"})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	got := []string{objects[0].Provider, objects[0].Path, objects[1].Provider, objects[1].Path}
	want := []string{"accounting", "providers/accounting/ledger.pb", "rules", "rules/current.pb"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ordered objects = %#v, want %#v", got, want)
	}
	objects[0].Bytes[0] = 'X'
	again, err := registry.Collect(context.Background(), sources, SnapshotRequest{WorkspaceID: "018f0000-0000-7000-8000-000000000001"})
	if err != nil {
		t.Fatal(err)
	}
	if string(again[0].Bytes) != "ledger" {
		t.Fatalf("provider bytes alias caller output: %q", again[0].Bytes)
	}
}

func TestProviderRegistryPreflightsBeforeCopyAndZerosOwnedErrors(t *testing.T) {
	newRegistry := func(provider Provider) *ProviderRegistry {
		registry, err := NewProviderRegistry([]ProviderRegistration{{Name: "rules", Version: 1, Provider: provider}})
		if err != nil {
			t.Fatal(err)
		}
		return registry
	}
	t.Run("success_clones_once_and_does_not_alias_source", func(t *testing.T) {
		borrowed := []byte("stable source")
		registry := newRegistry(providerFunc(func(context.Context, SnapshotReader, SnapshotRequest) (Projection, error) {
			return Projection{Objects: []Object{{Path: "rules/current.pb", Bytes: []byte("stable source")}}}, nil
		}))
		clones := 0
		registry.hooks = &objectCloneHooks{cloneBytes: func(value []byte) []byte {
			clones++
			return append([]byte(nil), value...)
		}}
		objects, err := registry.Collect(context.Background(), []ProjectionSourceRegistration{{Name: "rules", Version: 1,
			Source: projectionSourceFunc(func(context.Context, SnapshotRequest) (Projection, error) {
				return Projection{Objects: []Object{{Path: "rules/current.pb", Bytes: borrowed}}}, nil
			})}}, SnapshotRequest{WorkspaceID: "018f0000-0000-7000-8000-000000000001"})
		if err != nil || clones != 1 || len(objects) != 1 {
			t.Fatalf("objects=%#v error=%v clones=%d", objects, err, clones)
		}
		objects[0].Bytes[0] = 'X'
		if string(borrowed) != "stable source" {
			t.Fatalf("returned ownership aliases source: %q", borrowed)
		}
	})
	t.Run("source_error_never_clones_or_zeroes_borrowed_bytes", func(t *testing.T) {
		validatorCalled := false
		registry := newRegistry(providerFunc(func(context.Context, SnapshotReader, SnapshotRequest) (Projection, error) {
			validatorCalled = true
			return Projection{}, nil
		}))
		clones := 0
		registry.hooks = &objectCloneHooks{cloneBytes: func(value []byte) []byte {
			clones++
			return append([]byte(nil), value...)
		}}
		borrowed := []byte("source retains this")
		_, err := registry.Collect(context.Background(), []ProjectionSourceRegistration{{Name: "rules", Version: 1,
			Source: projectionSourceFunc(func(context.Context, SnapshotRequest) (Projection, error) {
				return Projection{Objects: []Object{{Path: "rules/current.pb", Bytes: borrowed}}}, errors.New("source failed")
			})}}, SnapshotRequest{WorkspaceID: "018f0000-0000-7000-8000-000000000001"})
		if !errors.Is(err, ErrProviderRegistry) || clones != 0 || validatorCalled || string(borrowed) != "source retains this" {
			t.Fatalf("error=%v clones=%d validator=%t borrowed=%q", err, clones, validatorCalled, borrowed)
		}
	})

	t.Run("aggregate_rejected_before_clone", func(t *testing.T) {
		validatorCalled := false
		registry := newRegistry(providerFunc(func(context.Context, SnapshotReader, SnapshotRequest) (Projection, error) {
			validatorCalled = true
			return Projection{}, nil
		}))
		clones := 0
		registry.hooks = &objectCloneHooks{
			objectByteLength: func(Object) uint64 { return uint64(maximumArchivePlaintext/2 + 1) },
			cloneBytes: func(value []byte) []byte {
				clones++
				return append([]byte(nil), value...)
			},
		}
		_, err := registry.Collect(context.Background(), []ProjectionSourceRegistration{{Name: "rules", Version: 1,
			Source: projectionSourceFunc(func(context.Context, SnapshotRequest) (Projection, error) {
				return Projection{Objects: []Object{
					{Path: "rules/one.pb", Bytes: []byte("one")},
					{Path: "rules/two.pb", Bytes: []byte("two")},
				}}, nil
			})}}, SnapshotRequest{WorkspaceID: "018f0000-0000-7000-8000-000000000001"})
		if !errors.Is(err, ErrProviderRegistry) || clones != 0 || validatorCalled {
			t.Fatalf("error=%v clones=%d validator=%t", err, clones, validatorCalled)
		}
	})

	for _, test := range []struct {
		name       string
		accounting Object
		rules      Object
		hooks      *objectCloneHooks
	}{
		{name: "later_provider_cross_duplicate", accounting: Object{Path: "rules/shared.pb", Bytes: []byte("one")},
			rules: Object{Path: "rules/shared.pb", Bytes: []byte("two")}},
		{name: "later_provider_aggregate_overflow", accounting: Object{Path: "providers/accounting/one.pb", Bytes: []byte("one")},
			rules: Object{Path: "rules/two.pb", Bytes: []byte("two")}, hooks: &objectCloneHooks{
				objectByteLength: func(Object) uint64 { return uint64(maximumArchivePlaintext/2 + 1) }}},
	} {
		t.Run(test.name, func(t *testing.T) {
			validators := 0
			registry, err := NewProviderRegistry([]ProviderRegistration{
				{Name: "accounting", Version: 1, Provider: providerValidatorFunc(func(context.Context, SnapshotRequest, Projection) error {
					validators++
					return nil
				})},
				{Name: "rules", Version: 1, Provider: providerValidatorFunc(func(context.Context, SnapshotRequest, Projection) error {
					validators++
					return nil
				})},
			})
			if err != nil {
				t.Fatal(err)
			}
			clones := 0
			registry.hooks = test.hooks
			if registry.hooks == nil {
				registry.hooks = &objectCloneHooks{}
			}
			registry.hooks.cloneBytes = func(value []byte) []byte {
				clones++
				return append([]byte(nil), value...)
			}
			_, err = registry.Collect(context.Background(), []ProjectionSourceRegistration{
				{Name: "accounting", Version: 1, Source: projectionSourceFunc(func(context.Context, SnapshotRequest) (Projection, error) {
					return Projection{Objects: []Object{test.accounting}}, nil
				})},
				{Name: "rules", Version: 1, Source: projectionSourceFunc(func(context.Context, SnapshotRequest) (Projection, error) {
					return Projection{Objects: []Object{test.rules}}, nil
				})},
			}, SnapshotRequest{WorkspaceID: "018f0000-0000-7000-8000-000000000001"})
			if !errors.Is(err, ErrProviderRegistry) || clones != 0 || validators != 0 {
				t.Fatalf("error=%v clones=%d validators=%d", err, clones, validators)
			}
		})
	}

	t.Run("later_source_error_prevents_all_clones_and_validation", func(t *testing.T) {
		validators := 0
		registry, err := NewProviderRegistry([]ProviderRegistration{
			{Name: "accounting", Version: 1, Provider: providerValidatorFunc(func(context.Context, SnapshotRequest, Projection) error {
				validators++
				return nil
			})},
			{Name: "rules", Version: 1, Provider: providerValidatorFunc(func(context.Context, SnapshotRequest, Projection) error {
				validators++
				return nil
			})},
		})
		if err != nil {
			t.Fatal(err)
		}
		var ownedCopies [][]byte
		registry.hooks = &objectCloneHooks{cloneBytes: func(value []byte) []byte {
			copy := append([]byte(nil), value...)
			ownedCopies = append(ownedCopies, copy)
			return copy
		}}
		borrowed := []byte("borrowed ledger")
		laterSourceCalled := false
		_, err = registry.Collect(context.Background(), []ProjectionSourceRegistration{
			{Name: "accounting", Version: 1, Source: projectionSourceFunc(func(context.Context, SnapshotRequest) (Projection, error) {
				return Projection{Objects: []Object{{Path: "providers/accounting/ledger.pb", Bytes: borrowed}}}, nil
			})},
			{Name: "rules", Version: 1, Source: projectionSourceFunc(func(context.Context, SnapshotRequest) (Projection, error) {
				laterSourceCalled = true
				return Projection{}, errors.New("later source failed")
			})},
		}, SnapshotRequest{WorkspaceID: "018f0000-0000-7000-8000-000000000001"})
		if !errors.Is(err, ErrProviderRegistry) || len(ownedCopies) != 0 || validators != 0 || !laterSourceCalled ||
			string(borrowed) != "borrowed ledger" {
			t.Fatalf("error=%v copies=%d validators=%d later=%t borrowed=%q",
				err, len(ownedCopies), validators, laterSourceCalled, borrowed)
		}
	})

	t.Run("validator_mutation_is_rejected_and_zeroed", func(t *testing.T) {
		registry := newRegistry(providerValidatorFunc(func(_ context.Context, _ SnapshotRequest, projection Projection) error {
			projection.Objects[0].Bytes[0] = 'X'
			return nil
		}))
		borrowed := []byte("immutable source")
		var owned []byte
		registry.hooks = &objectCloneHooks{cloneBytes: func(value []byte) []byte {
			owned = append([]byte(nil), value...)
			return owned
		}}
		objects, err := registry.Collect(context.Background(), []ProjectionSourceRegistration{{Name: "rules", Version: 1,
			Source: projectionSourceFunc(func(context.Context, SnapshotRequest) (Projection, error) {
				return Projection{Objects: []Object{{Path: "rules/current.pb", Bytes: borrowed}}}, nil
			})}}, SnapshotRequest{WorkspaceID: "018f0000-0000-7000-8000-000000000001"})
		if !errors.Is(err, ErrProviderRegistry) || objects != nil || string(borrowed) != "immutable source" ||
			!bytes.Equal(owned, make([]byte, len(owned))) {
			t.Fatalf("objects=%#v error=%v borrowed=%q owned=%x", objects, err, borrowed, owned)
		}
	})
}
