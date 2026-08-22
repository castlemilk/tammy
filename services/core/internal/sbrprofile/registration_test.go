package sbrprofile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommittedEVTEExamplesParseButCannotBecomeRunnable(t *testing.T) {
	root := repositoryRoot(t)
	registrationRaw, err := os.ReadFile(filepath.Join(root, "docs/development/sbr-registration-manifest.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	registration, err := ParseRegistrationManifest(registrationRaw)
	if err != nil {
		t.Fatal(err)
	}
	endpointRaw, err := os.ReadFile(filepath.Join(root, "docs/development/sbr-endpoint-profile.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ParseEndpointProfile(endpointRaw); err != nil {
		t.Fatal(err)
	}
	ready, err := EvaluateReadiness(registration.Manifest, testNow, "PRE_CONFORMANCE")
	if err != nil {
		t.Fatal(err)
	}
	if ready.Ready || ready.Code != "COMPONENT_LICENCE_NOT_APPROVED" {
		t.Fatalf("readiness=%+v", ready)
	}
}

func TestCrossBoundEVTEFailsClosedAtCodeOwnedUnregisteredTrustRoot(t *testing.T) {
	root := repositoryRoot(t)
	componentRaw, _ := os.ReadFile(filepath.Join(root, "docs/development/sbr-component-manifest.example.json"))
	component, err := ParseComponentManifest(componentRaw)
	if err != nil {
		t.Fatal(err)
	}
	registrationRaw, _ := os.ReadFile(filepath.Join(root, "docs/development/sbr-registration-manifest.example.json"))
	registration, err := ParseRegistrationManifest(registrationRaw)
	if err != nil {
		t.Fatal(err)
	}
	endpointRaw, _ := os.ReadFile(filepath.Join(root, "docs/development/sbr-endpoint-profile.example.json"))
	endpoint, err := ParseEndpointProfile(endpointRaw)
	if err != nil {
		t.Fatal(err)
	}
	registration.Manifest.Component.Name = component.Manifest.ComponentName
	registration.Manifest.Component.Version = component.Manifest.ComponentVersion
	registration.Manifest.Component.ComponentManifestSHA256 = component.SHA256
	registration.Manifest.EndpointProfile.EndpointProfileSHA256 = endpoint.SHA256
	profile := ParsedProfile{Profile: Profile{Environment: "EVTE", Target: "darwin/arm64", ComponentManifestSHA256: component.SHA256, RegistrationManifestSHA256: registration.SHA256, EndpointProfileSHA256: endpoint.SHA256}}
	err = AuthenticateEVTE(profile, registration, endpoint, component)
	if err == nil || err.Error() != "SBR_EVTE_TRUST_ROOT_UNREGISTERED" {
		t.Fatalf("error=%v", err)
	}
	if err = VerifyRegistrationSignature(registration, []byte("not-canonical")); err == nil || !strings.Contains(err.Error(), "SIGNATURE_ENCODING") {
		t.Fatalf("signature error=%v", err)
	}
}

func TestRegistrationRejectsEscapedDuplicateAndUnsafeEndpointURL(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "docs/development/sbr-endpoint-profile.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	duplicate := strings.Replace(string(raw), `"revision": 1`, `"revision": 1, "re\u0076ision": 1`, 1)
	if _, err = ParseEndpointProfile([]byte(duplicate)); err == nil {
		t.Fatal("accepted escaped duplicate")
	}
	for _, replacement := range []string{"http://sbr-endpoint.invalid/placeholder", "https://user@sbr-endpoint.invalid/placeholder", "https://sbr-endpoint.invalid:443/placeholder", "https://sbr-endpoint.invalid/%2e", "https://sbr-endpoint.invalid", "https://sbr-endpoint.invalid/a/../b", "https://sbr-endpoint.invalid/./b"} {
		candidate := strings.Replace(string(raw), "https://sbr-endpoint.invalid/placeholder", replacement, 1)
		if _, err = ParseEndpointProfile([]byte(candidate)); err == nil {
			t.Fatalf("accepted endpoint %q", replacement)
		}
	}
}

func TestEndpointProtocolBytesAreOwnedCanonicalBytes(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), "docs/development/sbr-endpoint-profile.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	noncanonical := append([]byte(" \n\t"), raw...)
	parsed, err := ParseEndpointProfile(noncanonical)
	if err != nil {
		t.Fatal(err)
	}
	forwarded := endpointProtocolBytes(parsed)
	if string(forwarded) != string(parsed.Canonical) || string(forwarded) == string(noncanonical) {
		t.Fatal("endpoint forwarding did not use canonical bytes")
	}
	forwarded[0] ^= 1
	if string(forwarded) == string(parsed.Canonical) {
		t.Fatal("endpoint forwarding aliased parsed canonical bytes")
	}
}

func TestPreEndpointReadinessHasStableMultiFailurePrecedence(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "docs/development/sbr-registration-manifest.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseRegistrationManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	manifest := parsed.Manifest
	manifest.Component.LicenceState = "APPROVED"
	ready, err := EvaluatePreEndpointReadiness(manifest, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if ready.Code != "DSP_REGISTRATION_NOT_APPROVED" {
		t.Fatalf("code=%s", ready.Code)
	}
	reference := "approved"
	decision := "2026-01-01"
	manifest.DSPRegistration = Approval{State: "APPROVED", ExternalReference: &reference, DecisionDate: &decision}
	ready, err = EvaluatePreEndpointReadiness(manifest, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if ready.Code != "PRODUCT_REGISTRATION_NOT_APPROVED" {
		t.Fatalf("code=%s", ready.Code)
	}
}

func TestApprovedRegistrationTransitionsRequireAllNodeFieldsWithoutReadinessPanic(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "docs/development/sbr-registration-manifest.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseRegistrationManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	manifest := parsed.Manifest
	reference := "approved"
	date := "2026-01-01"
	revalidation := "2026-12-31"
	issued := "2026-01-01T00:00:00Z"
	expires := "2026-12-31T00:00:00Z"
	manifest.OSFAssessment = OSFAssessment{Category: "BAS", State: "APPROVED", ExternalReference: &reference, DecisionDate: &date, RevalidationDate: &revalidation}
	manifest.EVTEAccess = EVTEAccess{State: "APPROVED", ExternalReference: &reference, IssuedAt: &issued, ExpiresAt: &expires}
	for _, testCase := range []struct {
		name   string
		mutate func(*RegistrationManifest)
	}{{"osf reference", func(m *RegistrationManifest) { m.OSFAssessment.ExternalReference = nil }}, {"osf decision", func(m *RegistrationManifest) { m.OSFAssessment.DecisionDate = nil }}, {"osf revalidation", func(m *RegistrationManifest) { m.OSFAssessment.RevalidationDate = nil }}, {"access reference", func(m *RegistrationManifest) { m.EVTEAccess.ExternalReference = nil }}, {"access issued", func(m *RegistrationManifest) { m.EVTEAccess.IssuedAt = nil }}, {"access expiry", func(m *RegistrationManifest) { m.EVTEAccess.ExpiresAt = nil }}} {
		t.Run(testCase.name, func(t *testing.T) {
			candidate := manifest
			testCase.mutate(&candidate)
			encoded, marshalErr := json.Marshal(candidate)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			if _, parseErr := ParseRegistrationManifest(encoded); parseErr == nil {
				t.Fatal("invalid approved transition parsed")
			}
			if _, readinessErr := EvaluateReadiness(candidate, testNow, "PRE_CONFORMANCE"); readinessErr == nil {
				t.Fatal("invalid approved transition reached readiness")
			}
			if _, readinessErr := EvaluatePreEndpointReadiness(candidate, testNow); readinessErr == nil {
				t.Fatal("invalid approved transition reached pre-endpoint readiness")
			}
		})
	}
}
