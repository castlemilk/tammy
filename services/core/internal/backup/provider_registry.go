package backup

import (
	"context"
	"errors"
	"reflect"
	"regexp"
	"sort"

	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

var ErrProviderRegistry = errors.New("backup: invalid provider registry")

var providerNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// SnapshotRequest is immutable trusted snapshot metadata. Providers receive no
// generic SQL executor and therefore cannot query another module through this boundary.
type SnapshotRequest struct {
	WorkspaceID     string
	AuditGeneration uint64
	AuditSequence   uint64
	AuditHead       []byte
}

// Projection is one provider-owned immutable snapshot.
type Projection struct {
	Objects []Object
}

// ProjectionSource supplies already-typed immutable module projections. It has
// deliberately no generic query, arbitrary provider selector, or untyped value
// surface. One instance is scoped to one registration by the composition root.
type ProjectionSource interface {
	SnapshotProjection(context.Context, SnapshotRequest) (Projection, error)
}

// ProjectionSourceRegistration binds one registration-scoped source to its
// immutable provider identity before the registry begins collection.
type ProjectionSourceRegistration struct {
	Name    string
	Version uint32
	Source  ProjectionSource
}

// Provider validates only its own copied immutable projection. It receives no
// source capability and therefore cannot request another module's data.
type Provider interface {
	ValidateSnapshot(context.Context, SnapshotRequest, Projection) error
}

type ProviderRegistration struct {
	Name     string
	Version  uint32
	Provider Provider
}

type registeredProvider struct {
	name     string
	version  uint32
	provider Provider
}

// ProviderRegistry is immutable after construction and owns canonical ordering.
type ProviderRegistry struct {
	providers []registeredProvider
	hooks     *objectCloneHooks
}

type objectCloneHooks struct {
	objectByteLength func(Object) uint64
	cloneBytes       func([]byte) []byte
}

func NewProviderRegistry(registrations []ProviderRegistration) (*ProviderRegistry, error) {
	if len(registrations) == 0 || len(registrations) > maximumArchiveObjects {
		return nil, ErrProviderRegistry
	}
	providers := make([]registeredProvider, 0, len(registrations))
	seen := make(map[string]struct{}, len(registrations))
	for _, registration := range registrations {
		if !providerNamePattern.MatchString(registration.Name) || registration.Version == 0 || nilInterface(registration.Provider) {
			return nil, ErrProviderRegistry
		}
		if _, duplicate := seen[registration.Name]; duplicate {
			return nil, ErrProviderRegistry
		}
		seen[registration.Name] = struct{}{}
		providers = append(providers, registeredProvider{name: registration.Name, version: registration.Version, provider: registration.Provider})
	}
	sort.Slice(providers, func(left, right int) bool { return providers[left].name < providers[right].name })
	return &ProviderRegistry{providers: providers}, nil
}

func (registry *ProviderRegistry) Collect(
	ctx context.Context,
	sources []ProjectionSourceRegistration,
	request SnapshotRequest,
) ([]Object, error) {
	if registry == nil || len(registry.providers) == 0 || ctx == nil ||
		len(sources) != len(registry.providers) || !ids.IsCanonicalV7(request.WorkspaceID) ||
		request.AuditGeneration == 0 && (request.AuditSequence != 0 || len(request.AuditHead) != 0) {
		return nil, ErrProviderRegistry
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrProviderRegistry, err)
	}
	sourceByName := make(map[string]ProjectionSourceRegistration, len(sources))
	for _, source := range sources {
		if !providerNamePattern.MatchString(source.Name) || source.Version == 0 || nilInterface(source.Source) {
			return nil, ErrProviderRegistry
		}
		if _, duplicate := sourceByName[source.Name]; duplicate {
			return nil, ErrProviderRegistry
		}
		sourceByName[source.Name] = source
	}
	type borrowedProjection struct {
		registration registeredProvider
		projection   Projection
	}
	borrowed := make([]borrowedProjection, 0, len(registry.providers))
	objectCount := 0
	for _, registration := range registry.providers {
		source, exists := sourceByName[registration.name]
		if !exists || source.Version != registration.version {
			return nil, ErrProviderRegistry
		}
	}
	for _, registration := range registry.providers {
		source := sourceByName[registration.name]
		sourceRequest := cloneSnapshotRequest(request)
		projection, err := source.Source.SnapshotProjection(ctx, sourceRequest)
		zero(sourceRequest.AuditHead)
		if err != nil || ctx.Err() != nil || len(projection.Objects) == 0 ||
			len(projection.Objects) > maximumArchiveObjects-objectCount {
			return nil, ErrProviderRegistry
		}
		objectCount += len(projection.Objects)
		borrowed = append(borrowed, borrowedProjection{registration: registration, projection: projection})
	}
	paths := make(map[string]struct{}, objectCount)
	var totalBytes uint64
	for _, collected := range borrowed {
		registration := collected.registration
		for _, object := range collected.projection.Objects {
			if err := ctx.Err(); err != nil {
				return nil, errors.Join(ErrProviderRegistry, err)
			}
			length := objectLengthForClone(object, registry.hooks)
			if !validateObjectMetadata(object.Path, registration.name, registration.version, length) ||
				length > uint64(maximumArchivePlaintext)-totalBytes || seenPath(paths, object.Path) {
				return nil, ErrProviderRegistry
			}
			totalBytes += length
		}
	}
	objects := make([]Object, 0, objectCount)
	succeeded := false
	defer func() {
		if !succeeded {
			zeroObjectBytes(objects)
		}
	}()
	for _, collected := range borrowed {
		registration := collected.registration
		owned := cloneProjectionWithHooks(collected.projection, registry.hooks)
		if len(owned.Objects) != len(collected.projection.Objects) {
			zeroProjection(&owned)
			return nil, ErrProviderRegistry
		}
		validationRequest := cloneSnapshotRequest(request)
		validationErr := registration.provider.ValidateSnapshot(ctx, validationRequest, owned)
		zero(validationRequest.AuditHead)
		if validationErr != nil || ctx.Err() != nil || !reflect.DeepEqual(owned, collected.projection) {
			zeroProjection(&owned)
			return nil, ErrProviderRegistry
		}
		for index := range owned.Objects {
			owned.Objects[index].Provider = registration.name
			owned.Objects[index].ProviderVersion = registration.version
		}
		objects = append(objects, owned.Objects...)
		owned.Objects = nil
	}
	sort.Slice(objects, func(left, right int) bool { return objects[left].Path < objects[right].Path })
	succeeded = true
	return objects, nil
}

func cloneSnapshotRequest(request SnapshotRequest) SnapshotRequest {
	request.AuditHead = append([]byte(nil), request.AuditHead...)
	return request
}

func cloneProjection(projection Projection) Projection {
	return cloneProjectionWithHooks(projection, nil)
}

func cloneProjectionWithHooks(projection Projection, hooks *objectCloneHooks) Projection {
	cloned := Projection{Objects: make([]Object, len(projection.Objects))}
	copy(cloned.Objects, projection.Objects)
	for index := range cloned.Objects {
		cloned.Objects[index].Bytes = cloneObjectBytes(projection.Objects[index].Bytes, hooks)
	}
	return cloned
}

func objectLengthForClone(object Object, hooks *objectCloneHooks) uint64 {
	if hooks != nil && hooks.objectByteLength != nil {
		return hooks.objectByteLength(object)
	}
	return uint64(len(object.Bytes))
}

func cloneObjectBytes(value []byte, hooks *objectCloneHooks) []byte {
	if hooks != nil && hooks.cloneBytes != nil {
		return hooks.cloneBytes(value)
	}
	return append([]byte(nil), value...)
}

func zeroProjection(projection *Projection) {
	if projection == nil {
		return
	}
	zeroObjectBytes(projection.Objects)
	projection.Objects = nil
}

func zeroObjectBytes(objects []Object) {
	for index := range objects {
		zero(objects[index].Bytes)
		objects[index].Bytes = nil
	}
}

// ValidateCollected authenticates the provider/version ownership of objects
// captured while the source-owned fixed read boundary was still open. It does
// not perform any new live provider reads.
func (registry *ProviderRegistry) ValidateCollected(objects []Object) error {
	if registry == nil || len(registry.providers) == 0 || len(objects) == 0 || len(objects) > maximumArchiveObjects {
		return ErrProviderRegistry
	}
	registered := make(map[string]uint32, len(registry.providers))
	for _, provider := range registry.providers {
		registered[provider.name] = provider.version
	}
	seen := make(map[string]bool, len(registry.providers))
	paths := make(map[string]struct{}, len(objects))
	for _, object := range objects {
		if registered[object.Provider] != object.ProviderVersion || !safeObject(object) || seenPath(paths, object.Path) {
			return ErrProviderRegistry
		}
		seen[object.Provider] = true
	}
	for name := range registered {
		if !seen[name] {
			return ErrProviderRegistry
		}
	}
	return nil
}

func seenPath(paths map[string]struct{}, objectPath string) bool {
	if _, duplicate := paths[objectPath]; duplicate {
		return true
	}
	paths[objectPath] = struct{}{}
	return false
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
