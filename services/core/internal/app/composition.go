package app

import (
	"errors"
	"net/http"
	"strings"
	"sync"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/buildinfo"
	tammyv1connect "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1/tammyv1connect"
	"github.com/tammyapp/tammy/services/core/internal/system"
	"github.com/tammyapp/tammy/services/core/internal/transport"
)

var ErrComposition = errors.New("app: invalid production composition")

const maximumCoreVersionLength = 128

type ResourceCloser interface {
	Close() error
}

// WorkspaceCompositionConfig contains complete generated handlers already
// bound to one application-owned encrypted workspace. The Workspace service
// remains absent until Task 12 supplies its concrete complete aggregate;
// accepting the generated interface here would also accept its unimplemented
// embed. Identity and Audit are complete current handlers and are required
// together. Resources transfers their lifecycle to Composition.
type WorkspaceCompositionConfig struct {
	Info      buildinfo.Info
	Identity  tammyv1connect.IdentityServiceHandler
	Audit     tammyv1connect.AuditServiceHandler
	Resources []ResourceCloser
}

// Composition owns an immutable generated-service registrar and the runtime
// resources behind it.
type Composition struct {
	registrar transport.ServiceRegistrar
	resources []ResourceCloser

	closeOnce sync.Once
	closeErr  error
}

// NewBootComposition constructs the pre-workspace process surface. It exposes
// diagnostics only; workspace-scoped and future service prefixes remain
// absent rather than using permissive placeholders.
func NewBootComposition(info buildinfo.Info) (*Composition, error) {
	if !validBuildInfo(info) {
		return nil, ErrComposition
	}
	return newComposition([]transport.GeneratedHandlerFactory{systemHandlerFactory(info)}, nil)
}

// NewWorkspaceComposition assembles the complete handlers supplied by the
// secure workspace activation boundary. Workspace remains deliberately
// unregistered until its concrete aggregate factory exists.
func NewWorkspaceComposition(config WorkspaceCompositionConfig) (*Composition, error) {
	if !validBuildInfo(config.Info) || nilInterface(config.Identity) || nilInterface(config.Audit) ||
		len(config.Resources) == 0 {
		return nil, ErrComposition
	}
	resources := append([]ResourceCloser(nil), config.Resources...)
	for _, resource := range resources {
		if nilInterface(resource) {
			return nil, ErrComposition
		}
	}
	factories := []transport.GeneratedHandlerFactory{
		systemHandlerFactory(config.Info),
		func(options ...connect.HandlerOption) (string, http.Handler) {
			return tammyv1connect.NewIdentityServiceHandler(config.Identity, options...)
		},
		func(options ...connect.HandlerOption) (string, http.Handler) {
			return tammyv1connect.NewAuditServiceHandler(config.Audit, options...)
		},
	}
	return newComposition(factories, resources)
}

func newComposition(factories []transport.GeneratedHandlerFactory, resources []ResourceCloser) (*Composition, error) {
	registrar, err := transport.NewGeneratedRegistrar(factories)
	if err != nil {
		return nil, errors.Join(ErrComposition, err)
	}
	return &Composition{registrar: registrar, resources: append([]ResourceCloser(nil), resources...)}, nil
}

func systemHandlerFactory(info buildinfo.Info) transport.GeneratedHandlerFactory {
	service := system.NewService(info)
	return func(options ...connect.HandlerOption) (string, http.Handler) {
		return tammyv1connect.NewSystemServiceHandler(service, options...)
	}
}

func validBuildInfo(info buildinfo.Info) bool {
	return info.Version != "" && strings.TrimSpace(info.Version) == info.Version && len(info.Version) <= maximumCoreVersionLength
}

func (composition *Composition) Registrar() transport.ServiceRegistrar {
	if composition == nil {
		return nil
	}
	return composition.registrar
}

// Close releases workspace resources once, in reverse construction order.
func (composition *Composition) Close() error {
	if composition == nil {
		return nil
	}
	composition.closeOnce.Do(func() {
		var failures []error
		for index := len(composition.resources) - 1; index >= 0; index-- {
			if err := composition.resources[index].Close(); err != nil {
				failures = append(failures, err)
			}
		}
		if len(failures) != 0 {
			composition.closeErr = errors.Join(append([]error{ErrComposition}, failures...)...)
		}
	})
	return composition.closeErr
}
