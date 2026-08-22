package sbrprofile

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var testNow = time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
var testSeed, _ = hex.DecodeString("9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60")

func TestAuthenticateSimulatorProfileUsesEmbeddedFixtureTrustRootAndRFC8785Bytes(t *testing.T) {
	helper := []byte("deterministic-helper")
	hash := sha256.Sum256(helper)
	raw := []byte(`{"target":"darwin/arm64","schema_version":1e0,"registration_manifest_sha256":"NONE","issued_at":"2026-08-21T00:00:00Z","helper_sha256":"` + hex.EncodeToString(hash[:]) + `","expires_at":"2026-08-22T00:00:00Z","environment":"SIMULATOR","endpoint_profile_sha256":"NONE","component_manifest_sha256":"NONE"}`)
	parsed, err := ParseProfile(raw, testNow)
	if err != nil {
		t.Fatal(err)
	}
	private := ed25519.NewKeyFromSeed(testSeed)
	signature := append([]byte(base64.StdEncoding.EncodeToString(ed25519.Sign(private, parsed.Canonical))), '\n')
	got, err := AuthenticateProfile(raw, signature, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if got.Profile.Environment != "SIMULATOR" || string(got.Canonical) == string(raw) {
		t.Fatalf("unexpected parse: %+v %s", got.Profile, got.Canonical)
	}
}

func TestProfileRejectsAmbiguousAndNonRunnableInputs(t *testing.T) {
	valid := `{"component_manifest_sha256":"NONE","endpoint_profile_sha256":"NONE","environment":"SIMULATOR","expires_at":"2026-08-22T00:00:00Z","helper_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","issued_at":"2026-08-21T00:00:00Z","registration_manifest_sha256":"NONE","schema_version":1,"target":"darwin/arm64"}`
	cases := map[string]string{
		"escaped duplicate":    strings.Replace(valid, `"target":"darwin/arm64"`, `"target":"darwin/arm64","tar\u0067et":"darwin/arm64"`, 1),
		"expired":              strings.Replace(valid, "2026-08-22T00:00:00Z", "2026-08-21T12:00:00Z", 1),
		"wrong target":         strings.Replace(valid, "darwin/arm64", "windows/amd64", 1),
		"simulator cross hash": strings.Replace(valid, `"component_manifest_sha256":"NONE"`, `"component_manifest_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`, 1),
		"extra":                strings.Replace(valid, "{", `{"extra":true,`, 1),
		"lone surrogate":       strings.Replace(valid, "SIMULATOR", `SIMULATOR\uD800`, 1),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseProfile([]byte(raw), testNow); err == nil {
				t.Fatal("accepted invalid profile")
			}
		})
	}
	deep := strings.Repeat("[", 33) + "0" + strings.Repeat("]", 33)
	if _, err := ParseProfile([]byte(deep), testNow); err == nil || !strings.Contains(err.Error(), "JSON_DEPTH") {
		t.Fatalf("depth error=%v", err)
	}
}

func TestInvalidLargeSchemaShapesNeverReachCanonicalizer(t *testing.T) {
	original := canonicalizeJSON
	calls := 0
	canonicalizeJSON = func(value []byte) ([]byte, error) {
		calls++
		return original(value)
	}
	t.Cleanup(func() { canonicalizeJSON = original })
	var builder strings.Builder
	builder.WriteByte('{')
	for index := 0; index < 3000; index++ {
		if index > 0 {
			builder.WriteByte(',')
		}
		fmt.Fprintf(&builder, `"unknown_%04d":"%s"`, index, strings.Repeat("x", 48))
	}
	builder.WriteByte('}')
	raw := []byte(builder.String())
	for name, parse := range map[string]func([]byte) error{
		"profile":      func(value []byte) error { _, err := ParseProfile(value, testNow); return err },
		"component":    func(value []byte) error { _, err := ParseComponentManifest(value); return err },
		"registration": func(value []byte) error { _, err := ParseRegistrationManifest(value); return err },
		"endpoint":     func(value []byte) error { _, err := ParseEndpointProfile(value); return err },
	} {
		t.Run(name, func(t *testing.T) {
			if err := parse(raw); err == nil {
				t.Fatal("ascending unknown-key object parsed")
			}
		})
	}
	if calls != 0 {
		t.Fatalf("canonicalizer invoked %d times for invalid shapes", calls)
	}
}

func TestSemanticallyInvalidSchemasNeverReachCanonicalizer(t *testing.T) {
	original := canonicalizeJSON
	calls := 0
	canonicalizeJSON = func(value []byte) ([]byte, error) {
		calls++
		return original(value)
	}
	t.Cleanup(func() { canonicalizeJSON = original })
	root := repositoryRoot(t)
	component, err := os.ReadFile(filepath.Join(root, "docs/development/sbr-component-manifest.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	registration, err := os.ReadFile(filepath.Join(root, "docs/development/sbr-registration-manifest.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := os.ReadFile(filepath.Join(root, "docs/development/sbr-endpoint-profile.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	profile := []byte(`{"component_manifest_sha256":"NONE","endpoint_profile_sha256":"NONE","environment":"BOGUS","expires_at":"2026-08-22T00:00:00Z","helper_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","issued_at":"2026-08-21T00:00:00Z","registration_manifest_sha256":"NONE","schema_version":1,"target":"darwin/arm64"}`)
	cases := map[string]func() error{
		"profile": func() error { _, parseErr := ParseProfile(profile, testNow); return parseErr },
		"component": func() error {
			_, parseErr := ParseComponentManifest([]byte(strings.Replace(string(component), `"target": "darwin/arm64"`, `"target": "windows/amd64"`, 1)))
			return parseErr
		},
		"registration": func() error {
			_, parseErr := ParseRegistrationManifest([]byte(strings.Replace(string(registration), `"state": "NOT_STARTED"`, `"state": "APPROVED"`, 1)))
			return parseErr
		},
		"endpoint": func() error {
			_, parseErr := ParseEndpointProfile([]byte(strings.Replace(string(endpoint), "https://", "http://", 1)))
			return parseErr
		},
	}
	for name, parse := range cases {
		t.Run(name, func(t *testing.T) {
			if err := parse(); err == nil {
				t.Fatal("semantically invalid schema parsed")
			}
		})
	}
	if calls != 0 {
		t.Fatalf("canonicalizer invoked %d times for semantically invalid schemas", calls)
	}
}

func TestDirectoryResourceLocatorUsesFixedCodeOwnedConvention(t *testing.T) {
	locator, err := NewDirectoryResourceLocator("/Applications/Tammy.app/Contents/Resources", "/Users/example/Library/Caches/Tammy/SBR")
	if err != nil {
		t.Fatal(err)
	}
	resources, err := locator.Locate(Profile{Environment: "EVTE"})
	if err != nil {
		t.Fatal(err)
	}
	if resources.HelperPath != "/Applications/Tammy.app/Contents/Resources/sbr/evte/tammy-sbr-helper" || resources.ComponentRoot != "/Applications/Tammy.app/Contents/Resources/sbr/evte/component/files" {
		t.Fatalf("resources=%+v", resources)
	}
}

func TestEmbeddedSimulatorTrustRootMatchesCommittedPublicFixture(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join(repositoryRoot(t), "config/sbr/simulator/profile-public-key.pem"))
	if err != nil {
		t.Fatal(err)
	}
	if string(fixture) != simulatorPublicKeyPEM {
		t.Fatal("embedded simulator trust root drifted from committed fixture")
	}
}
