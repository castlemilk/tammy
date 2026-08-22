package sbrprofile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	MaxComponentManifestBytes = 256 << 10
	MaxComponentFileBytes     = 64 << 20
	MaxComponentBundleBytes   = 256 << 20
	MaxComponentFiles         = 256
	MaxComponentEntries       = 512
	MaxComponentDepth         = 16
)

type ComponentFile struct {
	ByteLength int64  `json:"byte_length"`
	Path       string `json:"path"`
	SHA256     string `json:"sha256"`
}
type ComponentManifest struct {
	ComponentName    string          `json:"component_name"`
	ComponentVersion string          `json:"component_version"`
	Files            []ComponentFile `json:"files"`
	SchemaVersion    int             `json:"schema_version"`
	Target           string          `json:"target"`
}
type ParsedComponent struct {
	Manifest  ComponentManifest
	Canonical []byte
	SHA256    string
}

var componentKeys = []string{"component_name", "component_version", "files", "schema_version", "target"}
var componentIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+\-]{0,63}$`)

func ParseComponentManifest(raw []byte) (ParsedComponent, error) {
	var manifest ComponentManifest
	canonical, err := strictJSON(raw, MaxComponentManifestBytes, componentKeys, &manifest, "COMPONENT", func() error {
		return validateComponentManifest(manifest)
	})
	if err != nil {
		return ParsedComponent{}, err
	}
	sum := sha256.Sum256(canonical)
	return ParsedComponent{Manifest: manifest, Canonical: canonical, SHA256: hex.EncodeToString(sum[:])}, nil
}

func validateComponentManifest(manifest ComponentManifest) error {
	if manifest.SchemaVersion != 1 {
		return invalid("COMPONENT", "SCHEMA_VERSION")
	}
	if !componentIdentifier.MatchString(manifest.ComponentName) {
		return invalid("COMPONENT", "COMPONENT_NAME")
	}
	if !componentIdentifier.MatchString(manifest.ComponentVersion) {
		return invalid("COMPONENT", "COMPONENT_VERSION")
	}
	if manifest.Target != "darwin/arm64" {
		return invalid("COMPONENT", "TARGET")
	}
	if len(manifest.Files) < 1 || len(manifest.Files) > MaxComponentFiles {
		return invalid("COMPONENT", "FILE_COUNT")
	}
	var total int64
	previous := ""
	seen := make(map[string]struct{}, len(manifest.Files))
	for _, file := range manifest.Files {
		if !validComponentPath(file.Path) {
			return invalid("COMPONENT", "FILE_PATH")
		}
		if _, ok := seen[file.Path]; ok {
			return invalid("COMPONENT", "FILE_DUPLICATE")
		}
		if previous != "" && strings.Compare(previous, file.Path) >= 0 {
			return invalid("COMPONENT", "FILE_ORDER")
		}
		if file.ByteLength < 0 || file.ByteLength > MaxComponentFileBytes {
			return invalid("COMPONENT", "FILE_LENGTH")
		}
		if !hashString(file.SHA256) {
			return invalid("COMPONENT", "FILE_HASH")
		}
		total += file.ByteLength
		if total > MaxComponentBundleBytes {
			return invalid("COMPONENT", "BUNDLE_SIZE")
		}
		seen[file.Path] = struct{}{}
		previous = file.Path
	}
	return nil
}

func componentShape(raw []byte) bool {
	var root map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil {
		return false
	}
	return exactArrayRaw(root, "files", []string{"byte_length", "path", "sha256"})
}

func validComponentPath(value string) bool {
	if value == "" || len(value) > 1024 || !utf8.ValidString(value) || !norm.NFC.IsNormalString(value) || strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") || strings.Contains(value, "\\") || path.Clean(value) != value {
		return false
	}
	for _, r := range value {
		if r <= 0x1f || r >= 0x7f && r <= 0x9f {
			return false
		}
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func componentPaths(manifest ComponentManifest) []string {
	paths := make([]string, len(manifest.Files))
	for i, file := range manifest.Files {
		paths[i] = file.Path
	}
	sort.Strings(paths)
	return paths
}
