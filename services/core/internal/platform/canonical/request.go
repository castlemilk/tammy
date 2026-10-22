// Package canonical normalizes Protobuf requests and computes stable semantic hashes.
package canonical

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"sort"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const SemanticHashVersionV1 = "v1"

const semanticHashDomainV1 = "tammy.semantic-request-hash\x00"

var (
	ErrInvalidMessage   = errors.New("invalid protobuf message")
	ErrUnknownFields    = errors.New("unknown protobuf fields")
	ErrUnsupportedShape = errors.New("unsupported hash-bearing protobuf shape")
)

var fieldMaskPathPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(?:\.[a-z][a-z0-9_]*)*$`)

// SemanticHash is a versioned SHA-256 digest of normalized request semantics.
type SemanticHash struct {
	Version string
	Sum     [sha256.Size]byte
}

// Hex returns the lowercase hexadecimal digest.
func (hash SemanticHash) Hex() string {
	return hex.EncodeToString(hash.Sum[:])
}

// UnmarshalStrict decodes Protobuf JSON without discarding unknown fields.
func UnmarshalStrict(data []byte, destination proto.Message) error {
	if destination == nil || !destination.ProtoReflect().IsValid() {
		return ErrInvalidMessage
	}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(data, destination); err != nil {
		if strings.Contains(err.Error(), "unknown field") {
			return ErrUnknownFields
		}
		return ErrInvalidMessage
	}
	return nil
}

// NormalizedJSON returns RFC 8785 canonical bytes for the normalized Protobuf JSON mapping.
func NormalizedJSON(message proto.Message) ([]byte, error) {
	if message == nil || !message.ProtoReflect().IsValid() {
		return nil, ErrInvalidMessage
	}
	if err := rejectUnknownFields(message.ProtoReflect()); err != nil {
		return nil, err
	}
	cloned := proto.Clone(message)
	if err := normalizeFieldMasks(cloned.ProtoReflect()); err != nil {
		return nil, err
	}
	protobufJSON, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(cloned)
	if err != nil {
		return nil, ErrInvalidMessage
	}
	return canonicalizeJSON(protobufJSON)
}

// SemanticHashV1 hashes a request after removing only authentication and idempotency metadata.
func SemanticHashV1(message proto.Message) (SemanticHash, error) {
	if message == nil || !message.ProtoReflect().IsValid() {
		return SemanticHash{}, ErrInvalidMessage
	}
	if err := validateHashShape(message.ProtoReflect().Descriptor(), make(map[protoreflect.FullName]struct{})); err != nil {
		return SemanticHash{}, err
	}
	if err := rejectUnknownFields(message.ProtoReflect()); err != nil {
		return SemanticHash{}, err
	}
	cloned := proto.Clone(message)
	stripSemanticMetadata(cloned.ProtoReflect())
	normalized, err := NormalizedJSON(cloned)
	if err != nil {
		return SemanticHash{}, err
	}

	digest := sha256.New()
	_, _ = digest.Write([]byte(semanticHashDomainV1))
	_, _ = digest.Write([]byte(SemanticHashVersionV1))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write([]byte(message.ProtoReflect().Descriptor().FullName()))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write(normalized)
	var sum [sha256.Size]byte
	copy(sum[:], digest.Sum(nil))
	return SemanticHash{Version: SemanticHashVersionV1, Sum: sum}, nil
}

func rejectUnknownFields(message protoreflect.Message) error {
	if len(message.GetUnknown()) != 0 {
		return ErrUnknownFields
	}
	var visitErr error
	message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		if field.IsMap() {
			if field.MapValue().Kind() != protoreflect.MessageKind && field.MapValue().Kind() != protoreflect.GroupKind {
				return true
			}
			value.Map().Range(func(_ protoreflect.MapKey, mapValue protoreflect.Value) bool {
				visitErr = rejectUnknownFields(mapValue.Message())
				return visitErr == nil
			})
			return visitErr == nil
		}
		if field.Kind() != protoreflect.MessageKind && field.Kind() != protoreflect.GroupKind {
			return true
		}
		if field.IsList() {
			list := value.List()
			for index := range list.Len() {
				if visitErr = rejectUnknownFields(list.Get(index).Message()); visitErr != nil {
					return false
				}
			}
			return true
		}
		visitErr = rejectUnknownFields(value.Message())
		return visitErr == nil
	})
	return visitErr
}

func normalizeFieldMasks(message protoreflect.Message) error {
	if message.Descriptor().FullName() == "google.protobuf.FieldMask" {
		pathsField := message.Descriptor().Fields().ByName("paths")
		paths := message.Mutable(pathsField).List()
		normalized := make([]string, paths.Len())
		for index := range paths.Len() {
			normalized[index] = paths.Get(index).String()
			if !fieldMaskPathPattern.MatchString(normalized[index]) {
				return ErrInvalidMessage
			}
		}
		sort.Slice(normalized, func(left, right int) bool {
			return bytes.Compare([]byte(normalized[left]), []byte(normalized[right])) < 0
		})
		paths.Truncate(0)
		for index, path := range normalized {
			if index == 0 || path != normalized[index-1] {
				paths.Append(protoreflect.ValueOfString(path))
			}
		}
		return nil
	}

	var visitErr error
	message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		if field.IsMap() {
			if field.MapValue().Kind() != protoreflect.MessageKind && field.MapValue().Kind() != protoreflect.GroupKind {
				return true
			}
			value.Map().Range(func(_ protoreflect.MapKey, mapValue protoreflect.Value) bool {
				visitErr = normalizeFieldMasks(mapValue.Message())
				return visitErr == nil
			})
			return visitErr == nil
		}
		if field.Kind() != protoreflect.MessageKind && field.Kind() != protoreflect.GroupKind {
			return true
		}
		if field.IsList() {
			list := value.List()
			for index := range list.Len() {
				if visitErr = normalizeFieldMasks(list.Get(index).Message()); visitErr != nil {
					return false
				}
			}
			return true
		}
		visitErr = normalizeFieldMasks(value.Message())
		return visitErr == nil
	})
	return visitErr
}

func validateHashShape(
	message protoreflect.MessageDescriptor,
	visited map[protoreflect.FullName]struct{},
) error {
	if message.FullName() == "google.protobuf.Any" {
		return ErrUnsupportedShape
	}
	if _, ok := visited[message.FullName()]; ok {
		return nil
	}
	visited[message.FullName()] = struct{}{}
	fields := message.Fields()
	for index := range fields.Len() {
		field := fields.Get(index)
		if field.IsMap() || field.Kind() == protoreflect.FloatKind || field.Kind() == protoreflect.DoubleKind {
			return ErrUnsupportedShape
		}
		if field.Kind() == protoreflect.MessageKind || field.Kind() == protoreflect.GroupKind {
			if err := validateHashShape(field.Message(), visited); err != nil {
				return err
			}
		}
	}
	return nil
}

func stripSemanticMetadata(message protoreflect.Message) {
	message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		isCommandContext := message.Descriptor().FullName() == "tammy.v1.CommandContext"
		if isCommandContext && field.Name() == "idempotency_key" && field.Kind() == protoreflect.StringKind {
			message.Clear(field)
			return true
		}
		if isCommandContext && field.Name() == "authentication" &&
			field.Kind() == protoreflect.MessageKind &&
			field.Message().FullName() == "tammy.v1.AuthenticationContext" {
			message.Clear(field)
			return true
		}
		if field.IsMap() || (field.Kind() != protoreflect.MessageKind && field.Kind() != protoreflect.GroupKind) {
			return true
		}
		if field.IsList() {
			list := value.List()
			for index := range list.Len() {
				stripSemanticMetadata(list.Get(index).Message())
			}
			return true
		}
		stripSemanticMetadata(value.Message())
		return true
	})
}

func canonicalizeJSON(input []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, ErrInvalidMessage
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, ErrInvalidMessage
	}
	output, err := appendCanonicalJSON(nil, value)
	if err != nil {
		return nil, err
	}
	return output, nil
}

func appendCanonicalJSON(output []byte, value any) ([]byte, error) {
	switch typed := value.(type) {
	case nil:
		return append(output, "null"...), nil
	case bool:
		if typed {
			return append(output, "true"...), nil
		}
		return append(output, "false"...), nil
	case string:
		return appendJSONString(output, typed)
	case json.Number:
		if strings.ContainsAny(string(typed), ".eE") {
			return nil, ErrUnsupportedShape
		}
		integer := new(big.Int)
		if _, ok := integer.SetString(string(typed), 10); !ok {
			return nil, ErrInvalidMessage
		}
		return append(output, integer.String()...), nil
	case []any:
		output = append(output, '[')
		for index, item := range typed {
			if index > 0 {
				output = append(output, ',')
			}
			var err error
			output, err = appendCanonicalJSON(output, item)
			if err != nil {
				return nil, err
			}
		}
		return append(output, ']'), nil
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(left, right int) bool {
			return lessUTF16(keys[left], keys[right])
		})
		output = append(output, '{')
		for index, key := range keys {
			if index > 0 {
				output = append(output, ',')
			}
			var err error
			output, err = appendJSONString(output, key)
			if err != nil {
				return nil, err
			}
			output = append(output, ':')
			output, err = appendCanonicalJSON(output, typed[key])
			if err != nil {
				return nil, err
			}
		}
		return append(output, '}'), nil
	default:
		return nil, fmt.Errorf("%w: JSON value", ErrInvalidMessage)
	}
}

func appendJSONString(output []byte, value string) ([]byte, error) {
	if !utf8.ValidString(value) {
		return nil, ErrInvalidMessage
	}
	const hexadecimal = "0123456789abcdef"
	output = append(output, '"')
	for _, character := range value {
		switch character {
		case '"', '\\':
			output = append(output, '\\', byte(character))
		case '\b':
			output = append(output, `\b`...)
		case '\f':
			output = append(output, `\f`...)
		case '\n':
			output = append(output, `\n`...)
		case '\r':
			output = append(output, `\r`...)
		case '\t':
			output = append(output, `\t`...)
		default:
			if character < 0x20 {
				output = append(output, '\\', 'u', '0', '0', hexadecimal[character>>4], hexadecimal[character&0x0f])
			} else {
				output = utf8.AppendRune(output, character)
			}
		}
	}
	return append(output, '"'), nil
}

func lessUTF16(left, right string) bool {
	leftUnits := utf16.Encode([]rune(left))
	rightUnits := utf16.Encode([]rune(right))
	limit := min(len(leftUnits), len(rightUnits))
	for index := range limit {
		if leftUnits[index] != rightUnits[index] {
			return leftUnits[index] < rightUnits[index]
		}
	}
	return len(leftUnits) < len(rightUnits)
}
