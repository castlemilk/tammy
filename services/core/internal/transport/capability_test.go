package transport

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"connectrpc.com/connect"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1/tammyv1connect"
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
	nonCanonical := nonCanonicalCapability(t, expected)
	withLineBreak := expected[:10] + "\r\n" + expected[10:]
	assertPermissiveDecodeMatches(t, nonCanonical, expected)
	assertPermissiveDecodeMatches(t, withLineBreak, expected)

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
		{
			name:         "duplicate identical valid capability headers",
			headerValues: []string{expected, expected},
			supplied:     []string{expected},
		},
		{
			name:         "non-canonical trailing bits",
			headerValues: []string{nonCanonical},
			supplied:     []string{nonCanonical},
		},
		{
			name:         "embedded CRLF",
			headerValues: []string{withLineBreak},
			supplied:     []string{withLineBreak},
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

func TestCapabilityInterceptorRejectsRepeatedHeadersOverConnectHTTP(t *testing.T) {
	t.Parallel()

	expected := encodedCapability(0x45, 32)
	interceptor, err := NewCapabilityInterceptor(expected)
	if err != nil {
		t.Fatalf("NewCapabilityInterceptor() error = %v", err)
	}

	observedHeaders := make(chan []string, 1)
	observeHeaders := connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, request connect.AnyRequest) (connect.AnyResponse, error) {
			observedHeaders <- append([]string(nil), request.Header().Values(CapabilityHeader)...)
			return next(ctx, request)
		}
	})

	var applicationCalls atomic.Int32
	handler := connect.NewUnaryHandler(
		tammyv1connect.SystemServiceGetDiagnosticsProcedure,
		func(context.Context, *connect.Request[tammyv1.GetDiagnosticsRequest]) (*connect.Response[tammyv1.GetDiagnosticsResponse], error) {
			applicationCalls.Add(1)
			return connect.NewResponse(&tammyv1.GetDiagnosticsResponse{}), nil
		},
		connect.WithInterceptors(observeHeaders, interceptor),
	)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := connect.NewClient[tammyv1.GetDiagnosticsRequest, tammyv1.GetDiagnosticsResponse](
		server.Client(),
		server.URL+tammyv1connect.SystemServiceGetDiagnosticsProcedure,
	)
	request := connect.NewRequest(&tammyv1.GetDiagnosticsRequest{})
	request.Header().Add(CapabilityHeader, expected)
	request.Header().Add(CapabilityHeader, expected)

	gotResponse, gotErr := client.CallUnary(context.Background(), request)
	if gotResponse != nil {
		t.Fatalf("CallUnary() response = %v, want nil", gotResponse)
	}
	if gotCode := connect.CodeOf(gotErr); gotCode != connect.CodeUnauthenticated {
		t.Fatalf("CallUnary() code = %v, want %v (error = %v)", gotCode, connect.CodeUnauthenticated, gotErr)
	}
	var connectErr *connect.Error
	if !errors.As(gotErr, &connectErr) {
		t.Fatalf("CallUnary() error type = %T, want *connect.Error", gotErr)
	}
	if gotMessage := connectErr.Message(); gotMessage != "local capability rejected" {
		t.Fatalf("CallUnary() message = %q, want %q", gotMessage, "local capability rejected")
	}

	var gotHeaders []string
	select {
	case gotHeaders = <-observedHeaders:
	default:
		t.Fatal("probe interceptor did not observe the Connect request")
	}
	if len(gotHeaders) != 2 || gotHeaders[0] != expected || gotHeaders[1] != expected {
		t.Fatalf("server-side capability headers = %q, want two identical values", gotHeaders)
	}
	if got := applicationCalls.Load(); got != 0 {
		t.Fatalf("application handler calls = %d, want 0", got)
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
	nonCanonical := nonCanonicalCapability(t, valid)
	withLineBreak := valid[:10] + "\r\n" + valid[10:]
	assertPermissiveDecodeMatches(t, nonCanonical, valid)
	assertPermissiveDecodeMatches(t, withLineBreak, valid)
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
		{
			name:     "non-canonical trailing bits",
			expected: nonCanonical,
		},
		{
			name:     "embedded CRLF",
			expected: withLineBreak,
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

func nonCanonicalCapability(t *testing.T, canonical string) string {
	t.Helper()

	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	lastIndex := strings.IndexByte(alphabet, canonical[len(canonical)-1])
	if lastIndex < 0 || lastIndex&0x03 != 0 {
		t.Fatalf("fixture %q does not end with canonical two-byte Base64URL trailing bits", canonical)
	}
	return canonical[:len(canonical)-1] + string(alphabet[lastIndex|0x01])
}

func assertPermissiveDecodeMatches(t *testing.T, encoded, canonical string) {
	t.Helper()

	got, gotErr := base64.RawURLEncoding.DecodeString(encoded)
	want, wantErr := base64.RawURLEncoding.DecodeString(canonical)
	if gotErr != nil || wantErr != nil {
		t.Fatalf("permissive fixture decoding failed: encoded error = %v, canonical error = %v", gotErr, wantErr)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("permissive fixture decoded bytes differ from canonical capability")
	}
}

func assertSecretAbsent(t *testing.T, output, secret string) {
	t.Helper()

	if strings.Contains(output, secret) {
		t.Fatalf("formatted public result leaked capability %q in %q", secret, output)
	}
}
