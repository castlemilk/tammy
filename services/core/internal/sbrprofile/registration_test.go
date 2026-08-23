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

func TestRegistrationParsesExactProductIDScope(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), "docs/development/sbr-registration-manifest.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseRegistrationManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Manifest.ProductIDScope.ProductIdentifier != "placeholder.product.invalid" || parsed.Manifest.ProductIDScope.ServiceID != "placeholder.service.invalid" {
		t.Fatalf("product scope=%+v", parsed.Manifest.ProductIDScope)
	}
}

func TestRegistrationRejectsInvalidProductIDScope(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), "docs/development/sbr-registration-manifest.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	withScope := string(raw)
	for _, testCase := range []struct {
		name string
		raw  string
	}{
		{name: "missing", raw: strings.Replace(withScope, `  "product_id_scope": {
    "product_identifier": "placeholder.product.invalid",
    "service_id": "placeholder.service.invalid"
  },
`, "", 1)},
		{name: "empty product", raw: strings.Replace(withScope, "placeholder.product.invalid", "", 1)},
		{name: "oversized product", raw: strings.Replace(withScope, "placeholder.product.invalid", strings.Repeat("p", 129), 1)},
		{name: "noncanonical product", raw: strings.Replace(withScope, "placeholder.product.invalid", " product", 1)},
		{name: "empty service", raw: strings.Replace(withScope, "placeholder.service.invalid", "", 1)},
		{name: "unknown service", raw: strings.Replace(withScope, `"service_id": "placeholder.service.invalid"`, `"service_id": "other.service.invalid"`, 1)},
		{name: "duplicate product field", raw: strings.Replace(withScope, `"product_identifier": "placeholder.product.invalid",`, `"product_identifier": "placeholder.product.invalid", "product_identifier": "other.product.invalid",`, 1)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, parseErr := ParseRegistrationManifest([]byte(testCase.raw)); parseErr == nil {
				t.Fatal("invalid product ID scope parsed")
			}
		})
	}
}

func TestAuthenticatedProductIDScopeCrossBindsRegistrationAndEndpointService(t *testing.T) {
	registration := ParsedRegistration{Manifest: RegistrationManifest{
		ProductIDScope: ProductIDScope{ProductIdentifier: "product.evte.invalid", ServiceID: "service.a.invalid"},
		Services:       []RegistrationService{{ServiceID: "service.a.invalid"}},
	}}
	endpoint := ParsedEndpoint{Profile: EndpointProfile{Services: []EndpointService{{ServiceID: "service.a.invalid"}}}}

	scope, err := authenticateEVTEProductIDScope(registration, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	if scope.ProductIdentifier != "product.evte.invalid" || scope.ServiceID != "service.a.invalid" {
		t.Fatalf("scope=%+v", scope)
	}

	for _, testCase := range []struct {
		name         string
		registration ParsedRegistration
		endpoint     ParsedEndpoint
	}{
		{name: "missing registration service", registration: ParsedRegistration{Manifest: RegistrationManifest{ProductIDScope: registration.Manifest.ProductIDScope}}, endpoint: endpoint},
		{name: "missing endpoint service", registration: registration, endpoint: ParsedEndpoint{Profile: EndpointProfile{Services: []EndpointService{{ServiceID: "service.b.invalid"}}}}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, scopeErr := authenticateEVTEProductIDScope(testCase.registration, testCase.endpoint); scopeErr == nil || scopeErr.Error() != "SBR_REGISTRATION_INVALID:PRODUCT_SERVICE_MISMATCH" {
				t.Fatalf("error=%v", scopeErr)
			}
		})
	}
}

func TestStagedResourcesExposeOnlyAuthenticatedProductIDScope(t *testing.T) {
	scope := AuthenticatedProductIDScope{ProductIdentifier: "product.evte.invalid", ServiceID: "service.a.invalid"}
	simulator := &StagedResources{Profile: ParsedProfile{Profile: Profile{Environment: "SIMULATOR"}}, authenticatedProductIDScope: &scope}
	if _, ok := simulator.AuthenticatedProductIDScope(); ok {
		t.Fatal("simulator exposed a Product ID scope")
	}
	evte := &StagedResources{Profile: ParsedProfile{Profile: Profile{Environment: "EVTE"}}, authenticatedProductIDScope: &scope,
		authenticatedComponentVersion: "component-v1"}
	got, ok := evte.AuthenticatedProductIDScope()
	if !ok || got != scope {
		t.Fatalf("scope=%+v ok=%v", got, ok)
	}
	got.ProductIdentifier = "mutated"
	again, ok := evte.AuthenticatedProductIDScope()
	if !ok || again != scope {
		t.Fatalf("snapshot was mutable: scope=%+v ok=%v", again, ok)
	}
	if componentVersion, ok := evte.AuthenticatedComponentVersion(); !ok || componentVersion != "component-v1" {
		t.Fatalf("authenticated component version = %q, %v", componentVersion, ok)
	}
	if _, ok := simulator.AuthenticatedComponentVersion(); ok {
		t.Fatal("simulator exposed an EVTE component version")
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
