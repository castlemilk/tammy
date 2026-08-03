// Package faults defines stable typed application failures without human or secret text.
package faults

import "errors"

// Code is a stable public failure code.
type Code string

const (
	CodeAuthenticationRequired Code = "AUTHENTICATION_REQUIRED"
	CodeIdempotencyConflict    Code = "IDEMPOTENCY_CONFLICT"
	CodeInternal               Code = "INTERNAL"
	CodeNotFound               Code = "NOT_FOUND"
	CodePermissionDenied       Code = "PERMISSION_DENIED"
	CodeStaleVersion           Code = "STALE_VERSION"
	CodeValidation             Code = "VALIDATION"
)

// Fault is a typed failure with optional safe structured metadata.
type Fault struct {
	code     Code
	metadata map[string]string
}

// New creates a typed fault and defensively copies metadata.
func New(code Code, metadata map[string]string) *Fault {
	return &Fault{code: code, metadata: cloneMetadata(metadata)}
}

// Error exposes only the stable code, never metadata values.
func (fault *Fault) Error() string {
	return string(fault.code)
}

// Code returns the stable typed failure code.
func (fault *Fault) Code() Code {
	return fault.code
}

// Metadata returns a defensive copy of safe structured metadata.
func (fault *Fault) Metadata() map[string]string {
	return cloneMetadata(fault.metadata)
}

// Is matches faults by stable code.
func (fault *Fault) Is(target error) bool {
	var other *Fault
	return errors.As(target, &other) && fault.code == other.code
}

// CodeOf extracts the first typed fault code in an error chain.
func CodeOf(err error) (Code, bool) {
	var fault *Fault
	if !errors.As(err, &fault) {
		return "", false
	}
	return fault.code, true
}

func cloneMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(metadata))
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}
