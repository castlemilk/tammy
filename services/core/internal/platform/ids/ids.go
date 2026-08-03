// Package ids creates and validates canonical lowercase UUIDv7 identifiers.
package ids

import (
	"encoding/hex"
	"errors"
	"io"
	"reflect"
	"regexp"
	"sync"

	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
)

var (
	ErrEntropy       = errors.New("identifier entropy unavailable")
	ErrInvalidSource = errors.New("invalid identifier source")
	ErrInvalidTime   = errors.New("identifier time outside UUIDv7 range")
)

var canonicalV7Pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

const maximumUUIDv7Milliseconds = int64(1<<48 - 1)

// Generator builds UUIDv7 values from injected time and entropy sources.
type Generator struct {
	mu      sync.Mutex
	clock   clock.Clock
	entropy io.Reader
}

// NewGenerator creates a UUIDv7 generator without ambient time or randomness.
func NewGenerator(source clock.Clock, entropy io.Reader) (*Generator, error) {
	if isNilSource(source) || isNilSource(entropy) {
		return nil, ErrInvalidSource
	}
	return &Generator{clock: source, entropy: entropy}, nil
}

func isNilSource(source any) bool {
	if source == nil {
		return true
	}
	reflected := reflect.ValueOf(source)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

// New returns one canonical lowercase UUIDv7 string.
func (generator *Generator) New() (string, error) {
	generator.mu.Lock()
	defer generator.mu.Unlock()

	milliseconds := generator.clock.Now().UnixMilli()
	if milliseconds < 0 || milliseconds > maximumUUIDv7Milliseconds {
		return "", ErrInvalidTime
	}
	var random [10]byte
	if _, err := io.ReadFull(generator.entropy, random[:]); err != nil {
		return "", ErrEntropy
	}
	var identifier [16]byte
	identifier[0] = byte(milliseconds >> 40)
	identifier[1] = byte(milliseconds >> 32)
	identifier[2] = byte(milliseconds >> 24)
	identifier[3] = byte(milliseconds >> 16)
	identifier[4] = byte(milliseconds >> 8)
	identifier[5] = byte(milliseconds)
	copy(identifier[6:], random[:])
	identifier[6] = identifier[6]&0x0f | 0x70
	identifier[8] = identifier[8]&0x3f | 0x80

	var compact [32]byte
	hex.Encode(compact[:], identifier[:])
	formatted := make([]byte, 0, 36)
	formatted = append(formatted, compact[0:8]...)
	formatted = append(formatted, '-')
	formatted = append(formatted, compact[8:12]...)
	formatted = append(formatted, '-')
	formatted = append(formatted, compact[12:16]...)
	formatted = append(formatted, '-')
	formatted = append(formatted, compact[16:20]...)
	formatted = append(formatted, '-')
	formatted = append(formatted, compact[20:32]...)
	return string(formatted), nil
}

// IsCanonicalV7 reports whether value is the exact lowercase UUIDv7 representation.
func IsCanonicalV7(value string) bool {
	return canonicalV7Pattern.MatchString(value)
}
