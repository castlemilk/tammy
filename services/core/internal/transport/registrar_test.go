package transport

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
)

type nilHTTPHandler struct{}

func (*nilHTTPHandler) ServeHTTP(http.ResponseWriter, *http.Request) {}

func TestGeneratedRegistrarIsImmutableAndLeavesUnknownRoutesUnavailable(t *testing.T) {
	factories := []GeneratedHandlerFactory{
		func(options ...connect.HandlerOption) (string, http.Handler) {
			if len(options) != 2 {
				t.Fatalf("handler options = %d, want 2", len(options))
			}
			return "/tammy.v1.ReadyService/", http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(http.StatusNoContent)
			})
		},
	}
	registrar, err := NewGeneratedRegistrar(factories)
	if err != nil {
		t.Fatalf("NewGeneratedRegistrar() error = %v", err)
	}
	factories[0] = nil

	handler, err := registrar.Handler(connect.WithReadMaxBytes(1024), connect.WithCompressMinBytes(512))
	if err != nil {
		t.Fatalf("Handler() error = %v", err)
	}
	for _, test := range []struct {
		path string
		want int
	}{
		{path: "/tammy.v1.ReadyService/Ping", want: http.StatusNoContent},
		{path: "/tammy.v1.FutureService/Get", want: http.StatusNotFound},
		{path: "/undeclared", want: http.StatusNotFound},
	} {
		request := httptest.NewRequest(http.MethodPost, test.path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.want {
			t.Fatalf("path %q status = %d, want %d", test.path, response.Code, test.want)
		}
	}
}

func TestGeneratedRegistrarFailsClosedBeforePublishingInvalidRoutes(t *testing.T) {
	valid := func(...connect.HandlerOption) (string, http.Handler) {
		return "/tammy.v1.ValidService/", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	}
	var typedNil *nilHTTPHandler
	tests := []struct {
		name      string
		factories []GeneratedHandlerFactory
	}{
		{name: "empty"},
		{name: "nil factory", factories: []GeneratedHandlerFactory{nil}},
		{name: "empty path", factories: []GeneratedHandlerFactory{func(...connect.HandlerOption) (string, http.Handler) {
			return "", http.NotFoundHandler()
		}}},
		{name: "non service prefix", factories: []GeneratedHandlerFactory{func(...connect.HandlerOption) (string, http.Handler) {
			return "/tammy.v1.InvalidService", http.NotFoundHandler()
		}}},
		{name: "typed nil handler", factories: []GeneratedHandlerFactory{func(...connect.HandlerOption) (string, http.Handler) {
			return "/tammy.v1.InvalidService/", typedNil
		}}},
		{name: "duplicate", factories: []GeneratedHandlerFactory{valid, valid}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registrar, err := NewGeneratedRegistrar(test.factories)
			if err == nil {
				if _, err = registrar.Handler(); err == nil {
					t.Fatal("invalid registrar returned a handler")
				}
			}
			if !errors.Is(err, ErrRegistrar) {
				t.Fatalf("error = %v, want ErrRegistrar", err)
			}
		})
	}
}
