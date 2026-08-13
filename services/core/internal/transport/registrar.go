package transport

import (
	"errors"
	"net/http"
	"reflect"
	"regexp"

	"connectrpc.com/connect"
)

const maximumRegisteredServices = 64

var (
	ErrRegistrar                   = errors.New("transport: invalid generated service registrar")
	generatedServicePrefixPattern = regexp.MustCompile(`^/tammy\.v1\.[A-Za-z][A-Za-z0-9]*Service/$`)
)

// GeneratedHandlerFactory is the exact constructor shape emitted by
// protoc-gen-connect-go. Handler options stay owned by transport and are
// supplied only after the process-local security interceptor exists.
type GeneratedHandlerFactory func(...connect.HandlerOption) (string, http.Handler)

// ServiceRegistrar produces one immutable, private HTTP routing tree.
type ServiceRegistrar interface {
	Handler(...connect.HandlerOption) (http.Handler, error)
}

// GeneratedRegistrar owns the generated handler constructors selected by the
// application composition root. The caller's slice is copied at construction.
type GeneratedRegistrar struct {
	factories []GeneratedHandlerFactory
}

// NewGeneratedRegistrar constructs a closed registration set. Services which
// are not included have no mounted prefix and therefore remain HTTP 404.
func NewGeneratedRegistrar(factories []GeneratedHandlerFactory) (*GeneratedRegistrar, error) {
	if len(factories) == 0 || len(factories) > maximumRegisteredServices {
		return nil, ErrRegistrar
	}
	owned := append([]GeneratedHandlerFactory(nil), factories...)
	for _, factory := range owned {
		if factory == nil {
			return nil, ErrRegistrar
		}
	}
	return &GeneratedRegistrar{factories: owned}, nil
}

// Handler invokes every generated constructor with the same transport-owned
// options, validates the complete route set, and publishes it only after all
// registrations pass validation.
func (registrar *GeneratedRegistrar) Handler(options ...connect.HandlerOption) (_ http.Handler, resultErr error) {
	if registrar == nil || len(registrar.factories) == 0 || len(registrar.factories) > maximumRegisteredServices {
		return nil, ErrRegistrar
	}
	for _, option := range options {
		if nilInterface(option) {
			return nil, ErrRegistrar
		}
	}
	defer func() {
		if recover() != nil {
			resultErr = ErrRegistrar
		}
	}()
	type route struct {
		path    string
		handler http.Handler
	}
	routes := make([]route, 0, len(registrar.factories))
	seen := make(map[string]struct{}, len(registrar.factories))
	for _, factory := range registrar.factories {
		if factory == nil {
			return nil, ErrRegistrar
		}
		path, handler := factory(append([]connect.HandlerOption(nil), options...)...)
		if !generatedServicePrefixPattern.MatchString(path) || nilInterface(handler) {
			return nil, ErrRegistrar
		}
		if _, duplicate := seen[path]; duplicate {
			return nil, ErrRegistrar
		}
		seen[path] = struct{}{}
		routes = append(routes, route{path: path, handler: handler})
	}
	mux := http.NewServeMux()
	for _, route := range routes {
		mux.Handle(route.path, route.handler)
	}
	return mux, nil
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
