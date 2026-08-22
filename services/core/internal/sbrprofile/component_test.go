package sbrprofile

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestComponentExampleIsStructuralButExplicitlyNotRunnable(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "docs/development/sbr-component-manifest.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseComponentManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(parsed.Manifest.ComponentName, "NOT-RUNNABLE") {
		t.Fatal("documentation example lost non-runnable marker")
	}
}

func TestComponentManifestRejectsUnsafePathsOrderAndNumbers(t *testing.T) {
	base := `{"component_name":"helper","component_version":"1.0","files":[{"byte_length":1e0,"path":"a.bin","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}],"schema_version":1,"target":"darwin/arm64"}`
	if _, err := ParseComponentManifest([]byte(base)); err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string]string{"traversal": strings.Replace(base, "a.bin", "../a.bin", 1), "uppercase hash": strings.Replace(base, strings.Repeat("a", 64), strings.Repeat("A", 64), 1), "unknown": strings.Replace(base, `"target"`, `"unknown":0,"target"`, 1), "missing length": strings.Replace(base, `"byte_length":1e0,`, "", 1), "fractional length": strings.Replace(base, "1e0", "1.5", 1)} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseComponentManifest([]byte(raw)); err == nil {
				t.Fatal("accepted invalid component")
			}
		})
	}
}

func TestComponentManifestDotSegmentParity(t *testing.T) {
	hash := strings.Repeat("a", 64)
	valid := `{"component_name":"helper","component_version":"1.0","files":[{"byte_length":0,"path":".hidden","sha256":"` + hash + `"},{"byte_length":0,"path":".well-known/config","sha256":"` + hash + `"}],"schema_version":1,"target":"darwin/arm64"}`
	if _, err := ParseComponentManifest([]byte(valid)); err != nil {
		t.Fatalf("dot-prefixed path rejected: %v", err)
	}
	for _, path := range []string{".", "..", "a/./b", "a/../b"} {
		raw := `{"component_name":"helper","component_version":"1.0","files":[{"byte_length":0,"path":"` + path + `","sha256":"` + hash + `"}],"schema_version":1,"target":"darwin/arm64"}`
		if _, err := ParseComponentManifest([]byte(raw)); err == nil {
			t.Fatalf("exact dot segment accepted: %q", path)
		}
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../../../.."))
}
