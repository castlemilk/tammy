// Package sbrprofile authenticates the complete, code-trusted SBR runtime resource set.
package sbrprofile

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	jsoncanonicalizer "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
)

const (
	MaxProfileBytes  = 64 << 10
	maxEvidenceBytes = 256 << 10
	maxJSONDepth     = 32
	// The largest legitimate registration has 128 services with 128 hashes each;
	// these caps cover that shape while bounding pre-canonicalization work.
	maxJSONTokens = 32768
	maxJSONKeys   = 4096
)

var canonicalizeJSON = jsoncanonicalizer.Transform

const simulatorPublicKeyPEM = "-----BEGIN PUBLIC KEY-----\nMCowBQYDK2VwAyEA11qYAYKxCrfVS/7TyWQHOg7hcvPapiMlrwIaaPcHURo=\n-----END PUBLIC KEY-----\n"
const unregisteredEVTEPublicKeyPEM = "-----BEGIN PUBLIC KEY-----\nMCowBQYDK2VwAyEA3YWWiH31fK93Oeb8+iuUjcrsh7+IFPz7NJsY3j2Z6og=\n-----END PUBLIC KEY-----\n"
const evteTrustRootRegistered = false

type Profile struct {
	ComponentManifestSHA256    string `json:"component_manifest_sha256"`
	EndpointProfileSHA256      string `json:"endpoint_profile_sha256"`
	Environment                string `json:"environment"`
	ExpiresAt                  string `json:"expires_at"`
	HelperSHA256               string `json:"helper_sha256"`
	IssuedAt                   string `json:"issued_at"`
	RegistrationManifestSHA256 string `json:"registration_manifest_sha256"`
	SchemaVersion              int    `json:"schema_version"`
	Target                     string `json:"target"`
}

type ParsedProfile struct {
	Profile   Profile
	Canonical []byte
	SHA256    string
}

// ResourceSet is returned by a code-owned locator. None of these paths are accepted on argv.
type ResourceSet struct {
	HelperPath                string
	ComponentManifestPath     string
	ComponentRoot             string
	RegistrationManifestPath  string
	RegistrationSignaturePath string
	EndpointProfilePath       string
	TrustedRuntimeBase        string
	ReadinessPhase            string
}

type ResourceLocator interface {
	Locate(Profile) (ResourceSet, error)
}

type directoryResourceLocator struct{ resourcesRoot, runtimeBase string }

// NewDirectoryResourceLocator creates the only filesystem mapping used by core.
// The roots are application-owned configuration, never values accepted from SBR argv.
func NewDirectoryResourceLocator(resourcesRoot, runtimeBase string) (ResourceLocator, error) {
	if !filepath.IsAbs(resourcesRoot) || filepath.Clean(resourcesRoot) != resourcesRoot || !filepath.IsAbs(runtimeBase) || filepath.Clean(runtimeBase) != runtimeBase {
		return nil, codedError("SBR_RESOURCE_LOCATOR_INVALID")
	}
	return directoryResourceLocator{resourcesRoot: resourcesRoot, runtimeBase: runtimeBase}, nil
}

func (l directoryResourceLocator) Locate(profile Profile) (ResourceSet, error) {
	environment := "simulator"
	if profile.Environment == "EVTE" {
		environment = "evte"
	} else if profile.Environment != "SIMULATOR" {
		return ResourceSet{}, codedError("SBR_RESOURCE_LOCATOR_INVALID")
	}
	root := filepath.Join(l.resourcesRoot, "sbr", environment)
	return ResourceSet{
		HelperPath:                filepath.Join(root, "tammy-sbr-helper"),
		ComponentManifestPath:     filepath.Join(root, "component", "manifest.json"),
		ComponentRoot:             filepath.Join(root, "component", "files"),
		RegistrationManifestPath:  filepath.Join(root, "registration", "manifest.json"),
		RegistrationSignaturePath: filepath.Join(root, "registration", "manifest.sig"),
		EndpointProfilePath:       filepath.Join(root, "endpoint", "profile.json"),
		TrustedRuntimeBase:        l.runtimeBase,
		ReadinessPhase:            "PRE_CONFORMANCE",
	}, nil
}

type StagedResources struct {
	Profile         ParsedProfile
	RuntimeRoot     string
	HelperPath      string
	ReadOnlyPaths   []string
	EndpointProfile []byte
	Fingerprints    map[string]string
	close           func() error
	revalidate      func() error
	revalidateCtx   func(context.Context) error
	validateFresh   func(time.Time) error
	helperFile      *os.File
	helperExecFile  *os.File
	rootFile        *os.File
	baseFile        *os.File
	helperExpected  [sha256.Size]byte
	createdFiles    []string
	createdDirs     []string
	closeMu         sync.Mutex
	closed          bool
	closeErr        error
}

func (s *StagedResources) Revalidate() error {
	if s == nil || s.revalidate == nil {
		return codedError("SBR_HELPER_UNAVAILABLE")
	}
	return s.revalidate()
}

func (s *StagedResources) RevalidateContext(ctx context.Context) error {
	if s == nil || s.revalidateCtx == nil || ctx == nil {
		return codedError("SBR_HELPER_UNAVAILABLE")
	}
	return s.revalidateCtx(ctx)
}

func (s *StagedResources) ValidateFresh(now time.Time) error {
	if s == nil || s.validateFresh == nil {
		return codedError("SBR_HELPER_UNAVAILABLE")
	}
	return s.validateFresh(now)
}

func (s *StagedResources) Close() error {
	if s == nil {
		return nil
	}
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	if s.closed {
		return s.closeErr
	}
	s.closed = true
	if s.close != nil {
		s.closeErr = s.close()
	}
	return s.closeErr
}

type codedError string

func (e codedError) Error() string      { return string(e) }
func invalid(kind, reason string) error { return codedError("SBR_" + kind + "_INVALID:" + reason) }

var profileKeys = []string{"component_manifest_sha256", "endpoint_profile_sha256", "environment", "expires_at", "helper_sha256", "issued_at", "registration_manifest_sha256", "schema_version", "target"}

func ParseProfile(raw []byte, now time.Time) (ParsedProfile, error) {
	var profile Profile
	canonical, err := strictJSON(raw, MaxProfileBytes, profileKeys, &profile, "PROFILE", func() error {
		return validateProfile(profile, now)
	})
	if err != nil {
		return ParsedProfile{}, err
	}
	sum := sha256.Sum256(canonical)
	return ParsedProfile{Profile: profile, Canonical: canonical, SHA256: hex.EncodeToString(sum[:])}, nil
}

func validateProfile(profile Profile, now time.Time) error {
	if profile.SchemaVersion != 1 {
		return invalid("PROFILE", "SCHEMA_VERSION")
	}
	if profile.Environment != "SIMULATOR" && profile.Environment != "EVTE" {
		return invalid("PROFILE", "ENVIRONMENT")
	}
	if profile.Target != "darwin/arm64" {
		return invalid("PROFILE", "TARGET")
	}
	if !hashString(profile.HelperSHA256) {
		return invalid("PROFILE", "HELPER_HASH")
	}
	for _, value := range []string{profile.ComponentManifestSHA256, profile.RegistrationManifestSHA256, profile.EndpointProfileSHA256} {
		if profile.Environment == "SIMULATOR" {
			if value != "NONE" {
				return invalid("PROFILE", "CROSS_HASH")
			}
		} else if !hashString(value) {
			return invalid("PROFILE", "CROSS_HASH")
		}
	}
	issued, ok := strictTimestamp(profile.IssuedAt)
	if !ok {
		return invalid("PROFILE", "ISSUED_AT")
	}
	expires, ok := strictTimestamp(profile.ExpiresAt)
	if !ok {
		return invalid("PROFILE", "EXPIRES_AT")
	}
	if issued.After(now) {
		return invalid("PROFILE", "NOT_YET_VALID")
	}
	if !expires.After(now) {
		return invalid("PROFILE", "EXPIRED")
	}
	if !expires.After(issued) {
		return invalid("PROFILE", "VALIDITY_WINDOW")
	}
	return nil
}

func AuthenticateProfile(raw, signature []byte, now time.Time) (ParsedProfile, error) {
	parsed, err := ParseProfile(raw, now)
	if err != nil {
		return ParsedProfile{}, err
	}
	keyPEM := simulatorPublicKeyPEM
	if parsed.Profile.Environment == "EVTE" {
		keyPEM = unregisteredEVTEPublicKeyPEM
	}
	key, err := parsePublicKey(keyPEM)
	if err != nil {
		return ParsedProfile{}, invalid("PROFILE", "PUBLIC_KEY_FORMAT")
	}
	sig, err := parseSignature(signature, "PROFILE")
	if err != nil {
		return ParsedProfile{}, err
	}
	if !ed25519.Verify(key, parsed.Canonical, sig) {
		return ParsedProfile{}, invalid("PROFILE", "SIGNATURE_MISMATCH")
	}
	return parsed, nil
}

func strictJSON(raw []byte, maximum int, keys []string, destination any, kind string, validators ...func() error) ([]byte, error) {
	if len(raw) > maximum {
		return nil, invalid(kind, "INPUT_TOO_LARGE")
	}
	if !utf8.Valid(raw) {
		return nil, invalid(kind, "UTF8")
	}
	if !validJSONUnicode(raw) {
		return nil, invalid(kind, "UNICODE")
	}
	if !boundedJSONDepth(raw, maxJSONDepth) {
		return nil, invalid(kind, "JSON_DEPTH")
	}
	if err := preflightJSON(raw); err != nil {
		return nil, invalid(kind, "JSON")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || !exactKeys(object, keys) {
		return nil, invalid(kind, "FIELDS")
	}
	shapeOK := true
	switch destination.(type) {
	case *ComponentManifest:
		shapeOK = componentShape(raw)
	case *RegistrationManifest:
		shapeOK = registrationShape(raw)
	case *EndpointProfile:
		shapeOK = endpointShape(raw)
	}
	if !shapeOK {
		return nil, invalid(kind, "FIELDS")
	}
	if err := decodeTypedPrecanonical(raw, destination); err != nil {
		return nil, invalid(kind, "JSON")
	}
	for _, validate := range validators {
		if err := validate(); err != nil {
			return nil, err
		}
	}
	canonical, err := canonicalizeJSON(raw)
	if err != nil {
		return nil, invalid(kind, "JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(canonical))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return nil, invalid(kind, "JSON")
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, invalid(kind, "TRAILING_DATA")
	}
	return canonical, nil
}

// decodeTypedPrecanonical enforces the concrete schema before the potentially
// CPU-expensive JCS transform. encoding/json cannot decode the valid JSON number
// 1e0 into an int, so this narrow decoder accepts only finite, safe integral
// numbers and populates the same typed destination used by the final strict decode.
func decodeTypedPrecanonical(raw []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return err
	}
	target := reflect.ValueOf(destination)
	if target.Kind() != reflect.Pointer || target.IsNil() {
		return errors.New("typed destination")
	}
	return assignJSONValue(target.Elem(), value)
}

func assignJSONValue(target reflect.Value, value any) error {
	if target.Kind() == reflect.Pointer {
		if value == nil {
			target.SetZero()
			return nil
		}
		target.Set(reflect.New(target.Type().Elem()))
		return assignJSONValue(target.Elem(), value)
	}
	switch target.Kind() {
	case reflect.Struct:
		object, ok := value.(map[string]any)
		if !ok || len(object) != target.NumField() {
			return errors.New("object shape")
		}
		for index := 0; index < target.NumField(); index++ {
			field := target.Type().Field(index)
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			child, found := object[name]
			if !found || !target.Field(index).CanSet() {
				return errors.New("object field")
			}
			if err := assignJSONValue(target.Field(index), child); err != nil {
				return err
			}
		}
		return nil
	case reflect.Slice:
		array, ok := value.([]any)
		if !ok {
			return errors.New("array type")
		}
		target.Set(reflect.MakeSlice(target.Type(), len(array), len(array)))
		for index := range array {
			if err := assignJSONValue(target.Index(index), array[index]); err != nil {
				return err
			}
		}
		return nil
	case reflect.String:
		text, ok := value.(string)
		if !ok {
			return errors.New("string type")
		}
		target.SetString(text)
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		number, ok := value.(json.Number)
		if !ok {
			return errors.New("integer type")
		}
		parsed, err := strconv.ParseFloat(number.String(), 64)
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || math.Trunc(parsed) != parsed || math.Abs(parsed) > float64(maxSafeJSONInteger) {
			return errors.New("unsafe integer")
		}
		integer := int64(parsed)
		if integer < -maxSafeJSONInteger || integer > maxSafeJSONInteger {
			return errors.New("unsafe integer")
		}
		if target.OverflowInt(integer) {
			return errors.New("integer overflow")
		}
		target.SetInt(integer)
		return nil
	default:
		return errors.New("unsupported JSON type")
	}
}

type jsonPreflightFrame struct {
	object    bool
	expectKey bool
	keys      map[string]struct{}
}

func preflightJSON(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	frames := make([]jsonPreflightFrame, 0, maxJSONDepth)
	tokens, keys, roots := 0, 0, 0
	completeValue := func() {
		if len(frames) == 0 {
			roots++
			return
		}
		parent := &frames[len(frames)-1]
		if parent.object {
			parent.expectKey = true
		}
	}
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		tokens++
		if tokens > maxJSONTokens {
			return errors.New("token limit")
		}
		if delimiter, ok := token.(json.Delim); ok {
			switch delimiter {
			case '{':
				frames = append(frames, jsonPreflightFrame{object: true, expectKey: true, keys: make(map[string]struct{})})
			case '[':
				frames = append(frames, jsonPreflightFrame{})
			case '}', ']':
				if len(frames) == 0 || (delimiter == '}' && !frames[len(frames)-1].object) || (delimiter == ']' && frames[len(frames)-1].object) {
					return errors.New("delimiter")
				}
				frames = frames[:len(frames)-1]
				completeValue()
			}
			continue
		}
		if len(frames) > 0 && frames[len(frames)-1].object && frames[len(frames)-1].expectKey {
			key, ok := token.(string)
			if !ok {
				return errors.New("object key")
			}
			frame := &frames[len(frames)-1]
			if _, duplicate := frame.keys[key]; duplicate {
				return errors.New("duplicate key")
			}
			frame.keys[key] = struct{}{}
			frame.expectKey = false
			keys++
			if keys > maxJSONKeys {
				return errors.New("key limit")
			}
			continue
		}
		completeValue()
	}
	if len(frames) != 0 || roots != 1 {
		return errors.New("root")
	}
	return nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("trailing")
	}
	return nil
}

func validJSONUnicode(raw []byte) bool {
	for index := 0; index < len(raw); index++ {
		if raw[index] != '"' {
			continue
		}
		for index++; index < len(raw); index++ {
			if raw[index] == '"' {
				break
			}
			if raw[index] != '\\' {
				continue
			}
			index++
			if index >= len(raw) {
				return false
			}
			if raw[index] != 'u' {
				continue
			}
			first, ok := hex4(raw, index+1)
			if !ok {
				return false
			}
			index += 4
			if first >= 0xd800 && first <= 0xdbff {
				if index+6 >= len(raw) || raw[index+1] != '\\' || raw[index+2] != 'u' {
					return false
				}
				second, ok := hex4(raw, index+3)
				if !ok || second < 0xdc00 || second > 0xdfff {
					return false
				}
				index += 6
			} else if first >= 0xdc00 && first <= 0xdfff {
				return false
			}
		}
	}
	return true
}

func hex4(raw []byte, start int) (uint16, bool) {
	if start+4 > len(raw) {
		return 0, false
	}
	var value uint16
	for _, b := range raw[start : start+4] {
		value <<= 4
		switch {
		case b >= '0' && b <= '9':
			value += uint16(b - '0')
		case b >= 'a' && b <= 'f':
			value += uint16(b - 'a' + 10)
		case b >= 'A' && b <= 'F':
			value += uint16(b - 'A' + 10)
		default:
			return 0, false
		}
	}
	return value, true
}

func exactKeys(object map[string]json.RawMessage, keys []string) bool {
	if object == nil || len(object) != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, ok := object[key]; !ok {
			return false
		}
	}
	return true
}

func boundedJSONDepth(raw []byte, maximum int) bool {
	depth, inString, escaped := 0, false, false
	for _, b := range raw {
		if inString {
			if escaped {
				escaped = false
			} else if b == '\\' {
				escaped = true
			} else if b == '"' {
				inString = false
			}
			continue
		}
		if b == '"' {
			inString = true
			continue
		}
		if b == '{' || b == '[' {
			depth++
			if depth > maximum {
				return false
			}
		}
		if b == '}' || b == ']' {
			depth--
		}
	}
	return depth == 0 && !inString
}

func strictTimestamp(value string) (time.Time, bool) {
	if len(value) != len("2006-01-02T15:04:05Z") || !strings.HasSuffix(value, "Z") {
		return time.Time{}, false
	}
	parsed, err := time.Parse("2006-01-02T15:04:05Z", value)
	return parsed, err == nil && parsed.UTC().Format("2006-01-02T15:04:05Z") == value
}

func strictDate(value string) bool {
	if len(value) != 10 {
		return false
	}
	parsed, err := time.Parse("2006-01-02", value)
	return err == nil && parsed.Format("2006-01-02") == value
}

func hashString(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
}

func parsePublicKey(value string) (ed25519.PublicKey, error) {
	block, rest := pem.Decode([]byte(value))
	if block == nil || block.Type != "PUBLIC KEY" || len(rest) != 0 {
		return nil, errors.New("key")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("key type")
	}
	return key, nil
}

func parseSignature(value []byte, kind string) ([]byte, error) {
	if len(value) != 89 || value[88] != '\n' {
		return nil, invalid(kind, "SIGNATURE_ENCODING")
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(string(value[:88]))
	if err != nil || len(decoded) != ed25519.SignatureSize || base64.StdEncoding.EncodeToString(decoded) != string(value[:88]) {
		return nil, invalid(kind, "SIGNATURE_ENCODING")
	}
	return decoded, nil
}

func UnsupportedTargetError() error {
	return codedError(fmt.Sprintf("UNSUPPORTED_SBR_TARGET:%s/%s", runtime.GOOS, runtime.GOARCH))
}
