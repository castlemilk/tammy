package transport

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"connectrpc.com/connect"
)

func TestCapabilityInterceptorPermitsExactCapabilityOnce(t *testing.T) {
	t.Parallel()

	expected := encodedCapability(0x11, 32)
	interceptor, err := NewCapabilityInterceptor(expected)
	if err != nil {
		t.Fatalf("NewCapabilityInterceptor() error = %v", err)
	}

	var calls atomic.Int32
	wantResponse := connect.NewResponse(&struct{ Value string }{Value: "accepted"})
	handler := interceptor(connect.UnaryFunc(func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
		calls.Add(1)
		return wantResponse, nil
	}))
	request := connect.NewRequest(&struct{}{})
	request.Header().Set(CapabilityHeader, expected)

	gotResponse, gotErr := handler(context.Background(), request)
	if gotErr != nil {
		t.Fatalf("handler() error = %v", gotErr)
	}
	if gotResponse != wantResponse {
		t.Fatalf("handler() response = %p, want %p", gotResponse, wantResponse)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("next handler calls = %d, want 1", got)
	}
}

func TestCapabilityInterceptorRejectsInvalidHeadersWithoutCallingNext(t *testing.T) {
	t.Parallel()

	expected := encodedCapability(0x22, 32)
	wrongSameLength := encodedCapability(0x33, 32)
	wrongDifferentLength := encodedCapability(0x44, 31)
	malformed := strings.Repeat("A", 42) + "*"

	tests := []struct {
		name         string
		headerValues []string
		supplied     []string
	}{
		{
			name: "missing header",
		},
		{
			name:         "wrong same-length valid capability",
			headerValues: []string{wrongSameLength},
			supplied:     []string{wrongSameLength},
		},
		{
			name:         "wrong different-length value",
			headerValues: []string{wrongDifferentLength},
			supplied:     []string{wrongDifferentLength},
		},
		{
			name:         "padded Base64URL",
			headerValues: []string{expected + "="},
			supplied:     []string{expected + "="},
		},
		{
			name:         "malformed Base64URL",
			headerValues: []string{malformed},
			supplied:     []string{malformed},
		},
		{
			name:         "duplicate capability headers",
			headerValues: []string{expected, wrongSameLength},
			supplied:     []string{expected, wrongSameLength},
		},
	}

	interceptor, err := NewCapabilityInterceptor(expected)
	if err != nil {
		t.Fatalf("NewCapabilityInterceptor() error = %v", err)
	}

	var calls atomic.Int32
	handler := interceptor(connect.UnaryFunc(func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
		calls.Add(1)
		return connect.NewResponse(&struct{}{}), nil
	}))

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := connect.NewRequest(&struct{}{})
			for _, value := range tt.headerValues {
				request.Header().Add(CapabilityHeader, value)
			}

			gotResponse, gotErr := handler(context.Background(), request)
			if gotResponse != nil {
				t.Fatalf("handler() response = %v, want nil", gotResponse)
			}
			if gotCode := connect.CodeOf(gotErr); gotCode != connect.CodeUnauthenticated {
				t.Fatalf("handler() code = %v, want %v (error = %v)", gotCode, connect.CodeUnauthenticated, gotErr)
			}

			var connectErr *connect.Error
			if !errors.As(gotErr, &connectErr) {
				t.Fatalf("handler() error type = %T, want *connect.Error", gotErr)
			}
			if gotMessage := connectErr.Message(); gotMessage != "local capability rejected" {
				t.Fatalf("handler() message = %q, want %q", gotMessage, "local capability rejected")
			}

			formattedResult := fmt.Sprintf(
				"response: %v | response+: %+v | response#: %#v | error: %v | error+: %+v | error#: %#v",
				gotResponse,
				gotResponse,
				gotResponse,
				gotErr,
				gotErr,
				gotErr,
			)
			assertSecretAbsent(t, formattedResult, expected)
			for _, supplied := range tt.supplied {
				assertSecretAbsent(t, formattedResult, supplied)
			}
		})
	}

	if got := calls.Load(); got != 0 {
		t.Fatalf("next handler calls after rejected requests = %d, want 0", got)
	}
}

func TestCapabilityInterceptorPreservesNextError(t *testing.T) {
	t.Parallel()

	expected := encodedCapability(0x55, 32)
	interceptor, err := NewCapabilityInterceptor(expected)
	if err != nil {
		t.Fatalf("NewCapabilityInterceptor() error = %v", err)
	}

	wantErr := errors.New("downstream failure")
	handler := interceptor(connect.UnaryFunc(func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
		return nil, wantErr
	}))
	request := connect.NewRequest(&struct{}{})
	request.Header().Set(CapabilityHeader, expected)

	gotResponse, gotErr := handler(context.Background(), request)
	if gotResponse != nil {
		t.Fatalf("handler() response = %v, want nil", gotResponse)
	}
	if gotErr != wantErr {
		t.Fatalf("handler() error = %v, want exact error %v", gotErr, wantErr)
	}
}

func TestNewCapabilityInterceptorRejectsInvalidExpectedCapabilityWithoutLeakingIt(t *testing.T) {
	t.Parallel()

	valid := encodedCapability(0x66, 32)
	tests := []struct {
		name     string
		expected string
	}{
		{
			name: "missing",
		},
		{
			name:     "padded Base64URL",
			expected: valid + "=",
		},
		{
			name:     "malformed Base64URL",
			expected: strings.Repeat("B", 42) + "*",
		},
		{
			name:     "not 32 bytes",
			expected: encodedCapability(0x77, 31),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			interceptor, err := NewCapabilityInterceptor(tt.expected)
			if err == nil {
				t.Fatal("NewCapabilityInterceptor() error = nil, want constructor rejection")
			}
			if interceptor != nil {
				t.Fatalf("NewCapabilityInterceptor() interceptor = %v, want nil", interceptor)
			}

			formattedErr := fmt.Sprintf("error: %v | error+: %+v | error#: %#v", err, err, err)
			if tt.expected != "" {
				assertSecretAbsent(t, formattedErr, tt.expected)
			}
		})
	}
}

func encodedCapability(value byte, length int) string {
	return base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{value}, length))
}

func assertSecretAbsent(t *testing.T, output, secret string) {
	t.Helper()

	if strings.Contains(output, secret) {
		t.Fatalf("formatted public result leaked capability %q in %q", secret, output)
	}
}
