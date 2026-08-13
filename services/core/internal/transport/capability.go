package transport

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"

	"connectrpc.com/connect"
)

const (
	// CapabilityHeader carries the per-process capability for local Connect calls.
	CapabilityHeader = "X-Tammy-Capability"

	capabilityLength = 32
)

var capabilityEncoding = base64.RawURLEncoding.Strict()

// NewCapabilityInterceptor authenticates unary Connect calls with a
// per-process capability.
func NewCapabilityInterceptor(expected string) (connect.UnaryInterceptorFunc, error) {
	if !validCapability(expected) {
		return nil, errors.New("invalid local capability configuration")
	}

	expectedDigest := sha256.Sum256([]byte(expected))
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			values := req.Header().Values(CapabilityHeader)
			if len(values) != 1 || !validCapability(values[0]) {
				return nil, capabilityRejectedError()
			}

			suppliedDigest := sha256.Sum256([]byte(values[0]))
			if subtle.ConstantTimeCompare(expectedDigest[:], suppliedDigest[:]) != 1 {
				return nil, capabilityRejectedError()
			}

			return next(ctx, req)
		}
	}, nil
}

func validCapability(value string) bool {
	decoded, err := capabilityEncoding.DecodeString(value)
	valid := err == nil &&
		len(decoded) == capabilityLength &&
		capabilityEncoding.EncodeToString(decoded) == value
	clear(decoded)
	return valid
}

func capabilityRejectedError() error {
	return connect.NewError(
		connect.CodeUnauthenticated,
		errors.New("local capability rejected"),
	)
}
