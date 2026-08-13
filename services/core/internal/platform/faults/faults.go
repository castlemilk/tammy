// Package faults defines stable typed application failures without human or secret text.
package faults

import "errors"

// Code is a closed stable public failure vocabulary.
type Code uint8

const (
	CodeInternal Code = iota
	CodeAuthenticationRequired
	CodeIdempotencyConflict
	CodeNotFound
	CodePermissionDenied
	CodeStaleVersion
	CodeValidation
)

// String returns the stable public spelling, normalizing unknown values to INTERNAL.
func (code Code) String() string {
	switch code {
	case CodeAuthenticationRequired:
		return "AUTHENTICATION_REQUIRED"
	case CodeIdempotencyConflict:
		return "IDEMPOTENCY_CONFLICT"
	case CodeNotFound:
		return "NOT_FOUND"
	case CodePermissionDenied:
		return "PERMISSION_DENIED"
	case CodeStaleVersion:
		return "STALE_VERSION"
	case CodeValidation:
		return "VALIDATION"
	case CodeInternal:
		return "INTERNAL"
	default:
		return "INTERNAL"
	}
}

// Fault is a typed failure with optional safe structured metadata.
type Fault struct {
	code     Code
	metadata map[string]string
}

// New creates a typed fault and defensively copies metadata.
func New(code Code, metadata map[string]string) *Fault {
	return &Fault{code: normalizeCode(code), metadata: cloneMetadata(metadata)}
}

// Error exposes only the stable code, never metadata values.
func (fault *Fault) Error() string {
	if fault == nil {
		return CodeInternal.String()
	}
	return fault.code.String()
}

// Code returns the stable typed failure code.
func (fault *Fault) Code() Code {
	if fault == nil {
		return CodeInternal
	}
	return fault.code
}

// Metadata returns a defensive copy of safe structured metadata.
func (fault *Fault) Metadata() map[string]string {
	if fault == nil {
		return nil
	}
	return cloneMetadata(fault.metadata)
}

// Is matches faults by stable code.
func (fault *Fault) Is(target error) bool {
	if fault == nil {
		return false
	}
	var other *Fault
	return errors.As(target, &other) && other != nil && fault.code == other.code
}

// CodeOf extracts the first typed fault code in an error chain.
func CodeOf(err error) (Code, bool) {
	var fault *Fault
	if !errors.As(err, &fault) || fault == nil {
		return CodeInternal, false
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

func normalizeCode(code Code) Code {
	switch code {
	case CodeInternal,
		CodeAuthenticationRequired,
		CodeIdempotencyConflict,
		CodeNotFound,
		CodePermissionDenied,
		CodeStaleVersion,
		CodeValidation:
		return code
	default:
		return CodeInternal
	}
}
