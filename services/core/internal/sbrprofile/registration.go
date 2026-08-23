package sbrprofile

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Approval struct {
	State             string  `json:"state"`
	ExternalReference *string `json:"external_reference"`
	DecisionDate      *string `json:"decision_date"`
	ExpiresAt         *string `json:"expires_at"`
}
type OSFAssessment struct {
	Category          string  `json:"category"`
	State             string  `json:"state"`
	ExternalReference *string `json:"external_reference"`
	DecisionDate      *string `json:"decision_date"`
	RevalidationDate  *string `json:"revalidation_date"`
}
type RegisteredComponent struct {
	Name                    string `json:"name"`
	Version                 string `json:"version"`
	ComponentManifestSHA256 string `json:"component_manifest_sha256"`
	LicenceState            string `json:"licence_state"`
	Target                  string `json:"target"`
}
type RegistrationService struct {
	ServiceID        string   `json:"service_id"`
	TaxonomyVersion  string   `json:"taxonomy_version"`
	ReleaseVersion   string   `json:"release_version"`
	ArtefactSHA256s  []string `json:"artefact_sha256s"`
	EnrolmentState   string   `json:"enrolment_state"`
	ConformanceState string   `json:"conformance_state"`
}
type ProductIDScope struct {
	ProductIdentifier string `json:"product_identifier"`
	ServiceID         string `json:"service_id"`
}
type EVTEAccess struct {
	State             string  `json:"state"`
	ExternalReference *string `json:"external_reference"`
	IssuedAt          *string `json:"issued_at"`
	ExpiresAt         *string `json:"expires_at"`
}
type EndpointReference struct {
	ID                    string `json:"id"`
	Revision              int64  `json:"revision"`
	EndpointProfileSHA256 string `json:"endpoint_profile_sha256"`
	IssuedAt              string `json:"issued_at"`
	ExpiresAt             string `json:"expires_at"`
}
type Review struct {
	ReviewerIdentity string `json:"reviewer_identity"`
	ApprovedAt       string `json:"approved_at"`
	RevalidationDate string `json:"revalidation_date"`
}
type RegistrationManifest struct {
	SchemaVersion       int                   `json:"schema_version"`
	Environment         string                `json:"environment"`
	Target              string                `json:"target"`
	ProductIDScope      ProductIDScope        `json:"product_id_scope"`
	DSPRegistration     Approval              `json:"dsp_registration"`
	ProductRegistration Approval              `json:"product_registration"`
	OSFAssessment       OSFAssessment         `json:"osf_assessment"`
	Component           RegisteredComponent   `json:"component"`
	Services            []RegistrationService `json:"services"`
	EVTEAccess          EVTEAccess            `json:"evte_access"`
	EndpointProfile     EndpointReference     `json:"endpoint_profile"`
	Review              Review                `json:"review"`
}
type EndpointService struct {
	ServiceID         string `json:"service_id"`
	EndpointID        string `json:"endpoint_id"`
	EndpointURL       string `json:"endpoint_url"`
	TLSServerName     string `json:"tls_server_name"`
	CertificateSHA256 string `json:"certificate_sha256"`
}
type EndpointProfile struct {
	SchemaVersion int               `json:"schema_version"`
	Environment   string            `json:"environment"`
	ProfileID     string            `json:"profile_id"`
	Revision      int64             `json:"revision"`
	IssuedAt      string            `json:"issued_at"`
	ExpiresAt     string            `json:"expires_at"`
	Services      []EndpointService `json:"services"`
}
type ParsedRegistration struct {
	Manifest  RegistrationManifest
	Canonical []byte
	SHA256    string
}
type ParsedEndpoint struct {
	Profile   EndpointProfile
	Canonical []byte
	SHA256    string
}
type Readiness struct {
	Ready bool
	Code  string
}

func endpointProtocolBytes(endpoint ParsedEndpoint) []byte {
	return append([]byte(nil), endpoint.Canonical...)
}

var registrationKeys = []string{"schema_version", "environment", "target", "product_id_scope", "dsp_registration", "product_registration", "osf_assessment", "component", "services", "evte_access", "endpoint_profile", "review"}
var endpointKeys = []string{"schema_version", "environment", "profile_id", "revision", "issued_at", "expires_at", "services"}
var hostnameLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

const maxSafeJSONInteger = int64(1<<53 - 1)

func ParseRegistrationManifest(raw []byte) (ParsedRegistration, error) {
	var manifest RegistrationManifest
	canonical, err := strictJSON(raw, maxEvidenceBytes, registrationKeys, &manifest, "REGISTRATION", func() error {
		return validateRegistration(manifest)
	})
	if err != nil {
		return ParsedRegistration{}, err
	}
	sum := sha256.Sum256(canonical)
	return ParsedRegistration{Manifest: manifest, Canonical: canonical, SHA256: hex.EncodeToString(sum[:])}, nil
}

func ParseEndpointProfile(raw []byte) (ParsedEndpoint, error) {
	var profile EndpointProfile
	canonical, err := strictJSON(raw, maxEvidenceBytes, endpointKeys, &profile, "REGISTRATION", func() error {
		return validateEndpoint(profile)
	})
	if err != nil {
		return ParsedEndpoint{}, err
	}
	sum := sha256.Sum256(canonical)
	return ParsedEndpoint{Profile: profile, Canonical: canonical, SHA256: hex.EncodeToString(sum[:])}, nil
}

func validateRegistration(m RegistrationManifest) error {
	if m.SchemaVersion != 1 {
		return invalid("REGISTRATION", "SCHEMA_VERSION")
	}
	if m.Environment != "EVTE" {
		return invalid("REGISTRATION", "ENVIRONMENT")
	}
	if m.Target != "darwin/arm64" {
		return invalid("REGISTRATION", "TARGET")
	}
	if !opaque(m.ProductIDScope.ProductIdentifier) {
		return invalid("REGISTRATION", "PRODUCT_IDENTIFIER")
	}
	if !opaque(m.ProductIDScope.ServiceID) {
		return invalid("REGISTRATION", "PRODUCT_SERVICE_ID")
	}
	if err := validateApproval(m.DSPRegistration, "DSP_REGISTRATION"); err != nil {
		return err
	}
	if err := validateApproval(m.ProductRegistration, "PRODUCT_REGISTRATION"); err != nil {
		return err
	}
	if !opaque(m.OSFAssessment.Category) {
		return invalid("REGISTRATION", "OSF_CATEGORY")
	}
	osf := m.OSFAssessment
	if !oneOf(osf.State, "NOT_STARTED", "IN_REVIEW", "APPROVED") {
		return invalid("REGISTRATION", "OSF_STATE")
	}
	if !nullableOpaque(osf.ExternalReference) {
		return invalid("REGISTRATION", "OSF_REFERENCE")
	}
	if !nullableDate(osf.DecisionDate) {
		return invalid("REGISTRATION", "OSF_DECISION_DATE")
	}
	if !nullableDate(osf.RevalidationDate) {
		return invalid("REGISTRATION", "OSF_REVALIDATION_DATE")
	}
	if !validOSFTransition(osf) {
		return invalid("REGISTRATION", "OSF_TRANSITION")
	}
	if !componentIdentifier.MatchString(m.Component.Name) {
		return invalid("REGISTRATION", "COMPONENT_NAME")
	}
	if !componentIdentifier.MatchString(m.Component.Version) {
		return invalid("REGISTRATION", "COMPONENT_VERSION")
	}
	if !hashString(m.Component.ComponentManifestSHA256) {
		return invalid("REGISTRATION", "COMPONENT_HASH")
	}
	if !oneOf(m.Component.LicenceState, "NOT_OBTAINED", "REVIEW_REQUIRED", "APPROVED") {
		return invalid("REGISTRATION", "COMPONENT_LICENCE_STATE")
	}
	if m.Component.Target != "darwin/arm64" {
		return invalid("REGISTRATION", "COMPONENT_TARGET")
	}
	if len(m.Services) < 1 || len(m.Services) > 128 {
		return invalid("REGISTRATION", "SERVICES")
	}
	previous := ""
	productServiceFound := false
	for _, service := range m.Services {
		if !opaque(service.ServiceID) {
			return invalid("REGISTRATION", "SERVICE_ID")
		}
		if !opaque(service.TaxonomyVersion) {
			return invalid("REGISTRATION", "TAXONOMY_VERSION")
		}
		if !opaque(service.ReleaseVersion) {
			return invalid("REGISTRATION", "RELEASE_VERSION")
		}
		if previous != "" && previous >= service.ServiceID {
			return invalid("REGISTRATION", "SERVICE_ORDER")
		}
		previous = service.ServiceID
		if service.ServiceID == m.ProductIDScope.ServiceID {
			productServiceFound = true
		}
		if len(service.ArtefactSHA256s) < 1 || len(service.ArtefactSHA256s) > 128 || !sortedHashes(service.ArtefactSHA256s) {
			return invalid("REGISTRATION", "ARTEFACT_HASHES")
		}
		if !oneOf(service.EnrolmentState, "NOT_STARTED", "SUBMITTED", "APPROVED") || !oneOf(service.ConformanceState, "NOT_STARTED", "RUNNING", "PASSED") {
			return invalid("REGISTRATION", "SERVICE_STATE")
		}
		if service.EnrolmentState != "APPROVED" && service.ConformanceState != "NOT_STARTED" {
			return invalid("REGISTRATION", "SERVICE_TRANSITION")
		}
	}
	if !productServiceFound {
		return invalid("REGISTRATION", "PRODUCT_SERVICE_MISMATCH")
	}
	access := m.EVTEAccess
	if !oneOf(access.State, "NOT_REQUESTED", "REQUESTED", "APPROVED") {
		return invalid("REGISTRATION", "EVTE_ACCESS_STATE")
	}
	if !nullableOpaque(access.ExternalReference) {
		return invalid("REGISTRATION", "EVTE_ACCESS_REFERENCE")
	}
	if !nullableTimestamp(access.IssuedAt) {
		return invalid("REGISTRATION", "EVTE_ACCESS_ISSUED_AT")
	}
	if !nullableTimestamp(access.ExpiresAt) {
		return invalid("REGISTRATION", "EVTE_ACCESS_EXPIRES_AT")
	}
	if !validEVTEAccessTransition(access) {
		return invalid("REGISTRATION", "EVTE_ACCESS_TRANSITION")
	}
	if access.IssuedAt != nil && access.ExpiresAt != nil {
		issued, _ := strictTimestamp(*access.IssuedAt)
		expires, _ := strictTimestamp(*access.ExpiresAt)
		if !expires.After(issued) {
			return invalid("REGISTRATION", "EVTE_ACCESS_WINDOW")
		}
	}
	ref := m.EndpointProfile
	issued, issuedOK := strictTimestamp(ref.IssuedAt)
	expires, expiresOK := strictTimestamp(ref.ExpiresAt)
	if !opaque(ref.ID) {
		return invalid("REGISTRATION", "ENDPOINT_PROFILE_ID")
	}
	if ref.Revision < 1 || ref.Revision > maxSafeJSONInteger {
		return invalid("REGISTRATION", "ENDPOINT_PROFILE_REVISION")
	}
	if !hashString(ref.EndpointProfileSHA256) {
		return invalid("REGISTRATION", "ENDPOINT_PROFILE_HASH")
	}
	if !issuedOK {
		return invalid("REGISTRATION", "ENDPOINT_PROFILE_ISSUED_AT")
	}
	if !expiresOK {
		return invalid("REGISTRATION", "ENDPOINT_PROFILE_EXPIRES_AT")
	}
	if !expires.After(issued) {
		return invalid("REGISTRATION", "ENDPOINT_PROFILE_WINDOW")
	}
	if !opaque(m.Review.ReviewerIdentity) {
		return invalid("REGISTRATION", "REVIEWER_IDENTITY")
	}
	if !strictDate(m.Review.RevalidationDate) {
		return invalid("REGISTRATION", "REVIEW_REVALIDATION_DATE")
	}
	if _, ok := strictTimestamp(m.Review.ApprovedAt); !ok {
		return invalid("REGISTRATION", "REVIEW_APPROVED_AT")
	}
	return nil
}

func validateApproval(a Approval, prefix string) error {
	if !oneOf(a.State, "NOT_STARTED", "SUBMITTED", "APPROVED") {
		return invalid("REGISTRATION", prefix+"_STATE")
	}
	if !nullableOpaque(a.ExternalReference) {
		return invalid("REGISTRATION", prefix+"_REFERENCE")
	}
	if !nullableDate(a.DecisionDate) {
		return invalid("REGISTRATION", prefix+"_DECISION_DATE")
	}
	if !nullableTimestamp(a.ExpiresAt) {
		return invalid("REGISTRATION", prefix+"_EXPIRES_AT")
	}
	if !validApprovalTransition(a) {
		return invalid("REGISTRATION", prefix+"_TRANSITION")
	}
	return nil
}

func validateEndpoint(p EndpointProfile) error {
	if p.SchemaVersion != 1 {
		return invalid("REGISTRATION", "ENDPOINT_SCHEMA_VERSION")
	}
	if p.Environment != "EVTE" {
		return invalid("REGISTRATION", "ENDPOINT_ENVIRONMENT")
	}
	issued, issuedOK := strictTimestamp(p.IssuedAt)
	expires, expiresOK := strictTimestamp(p.ExpiresAt)
	if !opaque(p.ProfileID) {
		return invalid("REGISTRATION", "ENDPOINT_PROFILE_ID")
	}
	if p.Revision < 1 || p.Revision > maxSafeJSONInteger {
		return invalid("REGISTRATION", "ENDPOINT_PROFILE_REVISION")
	}
	if !issuedOK {
		return invalid("REGISTRATION", "ENDPOINT_PROFILE_ISSUED_AT")
	}
	if !expiresOK {
		return invalid("REGISTRATION", "ENDPOINT_PROFILE_EXPIRES_AT")
	}
	if !expires.After(issued) {
		return invalid("REGISTRATION", "ENDPOINT_PROFILE_WINDOW")
	}
	if len(p.Services) < 1 || len(p.Services) > 128 {
		return invalid("REGISTRATION", "ENDPOINT_SERVICES")
	}
	previous := ""
	for _, service := range p.Services {
		if !opaque(service.ServiceID) || !opaque(service.EndpointID) || !validEndpointURL(service.EndpointURL) || !validHostname(service.TLSServerName) || !hashString(service.CertificateSHA256) {
			return invalid("REGISTRATION", "ENDPOINT_SERVICE")
		}
		if previous != "" && previous >= service.ServiceID {
			return invalid("REGISTRATION", "ENDPOINT_SERVICE_ORDER")
		}
		previous = service.ServiceID
	}
	return nil
}

func EvaluateReadiness(m RegistrationManifest, now time.Time, phase string) (Readiness, error) {
	if err := validateRegistration(m); err != nil {
		return Readiness{}, err
	}
	if phase != "PRE_CONFORMANCE" && phase != "POST_CONFORMANCE" {
		return Readiness{}, invalid("REGISTRATION", "READINESS_PHASE")
	}
	today := now.UTC().Format("2006-01-02")
	if m.Component.LicenceState != "APPROVED" {
		return Readiness{Code: "COMPONENT_LICENCE_NOT_APPROVED"}, nil
	}
	for _, item := range []struct {
		a Approval
		p string
	}{{m.DSPRegistration, "DSP_REGISTRATION"}, {m.ProductRegistration, "PRODUCT_REGISTRATION"}} {
		if item.a.State != "APPROVED" {
			return Readiness{Code: item.p + "_NOT_APPROVED"}, nil
		}
		if item.a.ExternalReference == nil || item.a.DecisionDate == nil {
			return Readiness{}, invalid("REGISTRATION", item.p+"_TRANSITION")
		}
		if *item.a.DecisionDate > today {
			return Readiness{Code: item.p + "_DECISION_IN_FUTURE"}, nil
		}
		if item.a.ExpiresAt != nil {
			expires, _ := strictTimestamp(*item.a.ExpiresAt)
			if !expires.After(now) {
				return Readiness{Code: item.p + "_EXPIRED"}, nil
			}
		}
	}
	if m.OSFAssessment.State != "APPROVED" {
		return Readiness{Code: "OSF_ASSESSMENT_NOT_APPROVED"}, nil
	}
	if m.OSFAssessment.ExternalReference == nil || m.OSFAssessment.DecisionDate == nil || m.OSFAssessment.RevalidationDate == nil {
		return Readiness{}, invalid("REGISTRATION", "OSF_TRANSITION")
	}
	if *m.OSFAssessment.DecisionDate > today {
		return Readiness{Code: "OSF_DECISION_IN_FUTURE"}, nil
	}
	if *m.OSFAssessment.RevalidationDate < today {
		return Readiness{Code: "OSF_REVALIDATION_EXPIRED"}, nil
	}
	if m.EVTEAccess.State != "APPROVED" {
		return Readiness{Code: "EVTE_ACCESS_NOT_APPROVED"}, nil
	}
	if m.EVTEAccess.ExternalReference == nil || m.EVTEAccess.IssuedAt == nil || m.EVTEAccess.ExpiresAt == nil {
		return Readiness{}, invalid("REGISTRATION", "EVTE_ACCESS_TRANSITION")
	}
	accessStart, _ := strictTimestamp(*m.EVTEAccess.IssuedAt)
	accessEnd, _ := strictTimestamp(*m.EVTEAccess.ExpiresAt)
	if accessStart.After(now) {
		return Readiness{Code: "EVTE_ACCESS_NOT_YET_VALID"}, nil
	}
	if !accessEnd.After(now) {
		return Readiness{Code: "EVTE_ACCESS_EXPIRED"}, nil
	}
	endpointStart, _ := strictTimestamp(m.EndpointProfile.IssuedAt)
	endpointEnd, _ := strictTimestamp(m.EndpointProfile.ExpiresAt)
	if endpointStart.After(now) {
		return Readiness{Code: "ENDPOINT_PROFILE_NOT_YET_VALID"}, nil
	}
	if !endpointEnd.After(now) {
		return Readiness{Code: "ENDPOINT_PROFILE_EXPIRED"}, nil
	}
	hasApproved := false
	for _, s := range m.Services {
		if s.EnrolmentState == "APPROVED" {
			hasApproved = true
		}
	}
	if !hasApproved {
		return Readiness{Code: "SERVICE_ENROLMENT_NOT_APPROVED"}, nil
	}
	if phase == "POST_CONFORMANCE" {
		for _, s := range m.Services {
			if s.EnrolmentState != "APPROVED" {
				return Readiness{Code: "SERVICE_ENROLMENT_NOT_APPROVED"}, nil
			}
			if s.ConformanceState != "PASSED" {
				return Readiness{Code: "SERVICE_CONFORMANCE_NOT_PASSED"}, nil
			}
		}
	}
	reviewTime, _ := strictTimestamp(m.Review.ApprovedAt)
	if reviewTime.After(now) {
		return Readiness{Code: "REVIEW_NOT_YET_VALID"}, nil
	}
	if m.Review.RevalidationDate < today {
		return Readiness{Code: "REVIEW_REVALIDATION_EXPIRED"}, nil
	}
	return Readiness{Ready: true, Code: "READY_" + phase}, nil
}

func EvaluatePreEndpointReadiness(m RegistrationManifest, now time.Time) (Readiness, error) {
	if err := validateRegistration(m); err != nil {
		return Readiness{}, err
	}
	today := now.UTC().Format("2006-01-02")
	if m.Component.LicenceState != "APPROVED" {
		return Readiness{Code: "COMPONENT_LICENCE_NOT_APPROVED"}, nil
	}
	for _, item := range []struct {
		a Approval
		p string
	}{{m.DSPRegistration, "DSP_REGISTRATION"}, {m.ProductRegistration, "PRODUCT_REGISTRATION"}} {
		if item.a.State != "APPROVED" {
			return Readiness{Code: item.p + "_NOT_APPROVED"}, nil
		}
		if item.a.ExternalReference == nil || item.a.DecisionDate == nil {
			return Readiness{}, invalid("REGISTRATION", item.p+"_TRANSITION")
		}
		if *item.a.DecisionDate > today {
			return Readiness{Code: item.p + "_DECISION_IN_FUTURE"}, nil
		}
		if item.a.ExpiresAt != nil {
			expires, _ := strictTimestamp(*item.a.ExpiresAt)
			if !expires.After(now) {
				return Readiness{Code: item.p + "_EXPIRED"}, nil
			}
		}
	}
	if m.OSFAssessment.State != "APPROVED" {
		return Readiness{Code: "OSF_ASSESSMENT_NOT_APPROVED"}, nil
	}
	if m.OSFAssessment.ExternalReference == nil || m.OSFAssessment.DecisionDate == nil || m.OSFAssessment.RevalidationDate == nil {
		return Readiness{}, invalid("REGISTRATION", "OSF_TRANSITION")
	}
	if *m.OSFAssessment.DecisionDate > today {
		return Readiness{Code: "OSF_DECISION_IN_FUTURE"}, nil
	}
	if *m.OSFAssessment.RevalidationDate < today {
		return Readiness{Code: "OSF_REVALIDATION_EXPIRED"}, nil
	}
	if m.EVTEAccess.State != "APPROVED" {
		return Readiness{Code: "EVTE_ACCESS_NOT_APPROVED"}, nil
	}
	if m.EVTEAccess.ExternalReference == nil || m.EVTEAccess.IssuedAt == nil || m.EVTEAccess.ExpiresAt == nil {
		return Readiness{}, invalid("REGISTRATION", "EVTE_ACCESS_TRANSITION")
	}
	issued, _ := strictTimestamp(*m.EVTEAccess.IssuedAt)
	expires, _ := strictTimestamp(*m.EVTEAccess.ExpiresAt)
	if issued.After(now) {
		return Readiness{Code: "EVTE_ACCESS_NOT_YET_VALID"}, nil
	}
	if !expires.After(now) {
		return Readiness{Code: "EVTE_ACCESS_EXPIRED"}, nil
	}
	return Readiness{Ready: true, Code: "READY_PRE_ENDPOINT"}, nil
}

func authenticatePreEndpointBindings(profile ParsedProfile, registration ParsedRegistration, component ParsedComponent) error {
	if profile.Profile.Environment != "EVTE" || profile.Profile.RegistrationManifestSHA256 != registration.SHA256 {
		return invalid("REGISTRATION", "REGISTRATION_HASH_MISMATCH")
	}
	if profile.Profile.ComponentManifestSHA256 != component.SHA256 || registration.Manifest.Component.ComponentManifestSHA256 != component.SHA256 {
		return invalid("REGISTRATION", "COMPONENT_HASH_MISMATCH")
	}
	if registration.Manifest.Component.Name != component.Manifest.ComponentName || registration.Manifest.Component.Version != component.Manifest.ComponentVersion || registration.Manifest.Component.Target != component.Manifest.Target {
		return invalid("REGISTRATION", "COMPONENT_METADATA_MISMATCH")
	}
	return nil
}

func AuthenticateEVTE(profile ParsedProfile, registration ParsedRegistration, endpoint ParsedEndpoint, component ParsedComponent) error {
	if profile.Profile.Environment != "EVTE" || profile.Profile.RegistrationManifestSHA256 != registration.SHA256 || profile.Profile.ComponentManifestSHA256 != component.SHA256 || profile.Profile.EndpointProfileSHA256 != endpoint.SHA256 {
		return invalid("REGISTRATION", "CROSS_HASH_MISMATCH")
	}
	if registration.Manifest.Component.Name != component.Manifest.ComponentName || registration.Manifest.Component.Version != component.Manifest.ComponentVersion || registration.Manifest.Component.Target != component.Manifest.Target || registration.Manifest.Component.ComponentManifestSHA256 != component.SHA256 {
		return invalid("REGISTRATION", "COMPONENT_METADATA_MISMATCH")
	}
	if registration.Manifest.EndpointProfile.ID != endpoint.Profile.ProfileID || registration.Manifest.EndpointProfile.Revision != endpoint.Profile.Revision || registration.Manifest.EndpointProfile.IssuedAt != endpoint.Profile.IssuedAt || registration.Manifest.EndpointProfile.ExpiresAt != endpoint.Profile.ExpiresAt || registration.Manifest.EndpointProfile.EndpointProfileSHA256 != endpoint.SHA256 {
		return invalid("REGISTRATION", "ENDPOINT_METADATA_MISMATCH")
	}
	if _, err := authenticateEVTEProductIDScope(registration, endpoint); err != nil {
		return err
	}
	if len(registration.Manifest.Services) != len(endpoint.Profile.Services) {
		return invalid("REGISTRATION", "SERVICE_SET_MISMATCH")
	}
	for i := range registration.Manifest.Services {
		if registration.Manifest.Services[i].ServiceID != endpoint.Profile.Services[i].ServiceID {
			return invalid("REGISTRATION", "SERVICE_SET_MISMATCH")
		}
	}
	if !evteTrustRootRegistered {
		return codedError("SBR_EVTE_TRUST_ROOT_UNREGISTERED")
	}
	return nil
}

func authenticateEVTEProductIDScope(registration ParsedRegistration, endpoint ParsedEndpoint) (AuthenticatedProductIDScope, error) {
	selected := registration.Manifest.ProductIDScope
	if !opaque(selected.ProductIdentifier) || !opaque(selected.ServiceID) {
		return AuthenticatedProductIDScope{}, invalid("REGISTRATION", "PRODUCT_SERVICE_MISMATCH")
	}
	registrationMatches := 0
	for _, service := range registration.Manifest.Services {
		if service.ServiceID == selected.ServiceID {
			registrationMatches++
		}
	}
	endpointMatches := 0
	for _, service := range endpoint.Profile.Services {
		if service.ServiceID == selected.ServiceID {
			endpointMatches++
		}
	}
	if registrationMatches != 1 || endpointMatches != 1 {
		return AuthenticatedProductIDScope{}, invalid("REGISTRATION", "PRODUCT_SERVICE_MISMATCH")
	}
	return AuthenticatedProductIDScope{ProductIdentifier: selected.ProductIdentifier, ServiceID: selected.ServiceID}, nil
}

// VerifyRegistrationSignature is the bounded detached-signature primitive. It
// does not imply EVTE readiness; the code-owned trust-root registration flag is
// checked independently by AuthenticateEVTE.
func VerifyRegistrationSignature(registration ParsedRegistration, signature []byte) error {
	key, err := parsePublicKey(unregisteredEVTEPublicKeyPEM)
	if err != nil {
		return invalid("REGISTRATION", "PUBLIC_KEY_FORMAT")
	}
	decoded, err := parseSignature(signature, "REGISTRATION")
	if err != nil {
		return err
	}
	if !ed25519.Verify(key, registration.Canonical, decoded) {
		return invalid("REGISTRATION", "SIGNATURE_MISMATCH")
	}
	return nil
}

func opaque(v string) bool {
	if len(v) < 1 || len(v) > 128 || strings.TrimSpace(v) != v {
		return false
	}
	for _, r := range v {
		if r < 0x20 || r > 0x7e {
			return false
		}
	}
	return true
}
func oneOf(v string, values ...string) bool {
	for _, candidate := range values {
		if v == candidate {
			return true
		}
	}
	return false
}
func nullableOpaque(value *string) bool { return value == nil || opaque(*value) }
func validApprovalTransition(value Approval) bool {
	switch value.State {
	case "NOT_STARTED":
		return value.ExternalReference == nil && value.DecisionDate == nil && value.ExpiresAt == nil
	case "SUBMITTED":
		return value.ExternalReference != nil && value.DecisionDate == nil && value.ExpiresAt == nil
	case "APPROVED":
		return value.ExternalReference != nil && value.DecisionDate != nil
	default:
		return false
	}
}
func validOSFTransition(value OSFAssessment) bool {
	switch value.State {
	case "NOT_STARTED":
		return value.ExternalReference == nil && value.DecisionDate == nil && value.RevalidationDate == nil
	case "IN_REVIEW":
		return value.ExternalReference != nil && value.DecisionDate == nil && value.RevalidationDate == nil
	case "APPROVED":
		return value.ExternalReference != nil && value.DecisionDate != nil && value.RevalidationDate != nil
	default:
		return false
	}
}
func validEVTEAccessTransition(value EVTEAccess) bool {
	switch value.State {
	case "NOT_REQUESTED":
		return value.ExternalReference == nil && value.IssuedAt == nil && value.ExpiresAt == nil
	case "REQUESTED":
		return value.ExternalReference != nil && value.IssuedAt == nil && value.ExpiresAt == nil
	case "APPROVED":
		return value.ExternalReference != nil && value.IssuedAt != nil && value.ExpiresAt != nil
	default:
		return false
	}
}
func nullableDate(v *string) bool { return v == nil || strictDate(*v) }
func nullableTimestamp(v *string) bool {
	if v == nil {
		return true
	}
	_, ok := strictTimestamp(*v)
	return ok
}
func sortedHashes(values []string) bool {
	if !sort.StringsAreSorted(values) {
		return false
	}
	for i, v := range values {
		if !hashString(v) || (i > 0 && values[i-1] == v) {
			return false
		}
	}
	return true
}
func validHostname(v string) bool {
	if len(v) > 253 || v != strings.ToLower(v) || !strings.Contains(v, ".") || net.ParseIP(v) != nil {
		return false
	}
	for _, label := range strings.Split(v, ".") {
		if !hostnameLabel.MatchString(label) {
			return false
		}
	}
	return true
}
func validEndpointURL(v string) bool {
	if len(v) > 2048 || strings.ContainsAny(v, "\\%") {
		return false
	}
	u, err := url.Parse(v)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" || !validHostname(u.Hostname()) {
		return false
	}
	if u.Path == "" || !strings.HasPrefix(u.Path, "/") {
		return false
	}
	for _, segment := range strings.Split(u.Path, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	authority := strings.TrimPrefix(v, "https://")
	if slash := strings.IndexByte(authority, '/'); slash >= 0 {
		authority = authority[:slash]
	}
	if strings.Contains(authority, ":") {
		return false
	}
	return u.String() == v
}

func registrationShape(raw []byte) bool {
	var root map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil {
		return false
	}
	return exactRaw(root, "product_id_scope", []string{"product_identifier", "service_id"}) && exactRaw(root, "dsp_registration", []string{"state", "external_reference", "decision_date", "expires_at"}) && exactRaw(root, "product_registration", []string{"state", "external_reference", "decision_date", "expires_at"}) && exactRaw(root, "osf_assessment", []string{"category", "state", "external_reference", "decision_date", "revalidation_date"}) && exactRaw(root, "component", []string{"name", "version", "component_manifest_sha256", "licence_state", "target"}) && exactArrayRaw(root, "services", []string{"service_id", "taxonomy_version", "release_version", "artefact_sha256s", "enrolment_state", "conformance_state"}) && exactRaw(root, "evte_access", []string{"state", "external_reference", "issued_at", "expires_at"}) && exactRaw(root, "endpoint_profile", []string{"id", "revision", "endpoint_profile_sha256", "issued_at", "expires_at"}) && exactRaw(root, "review", []string{"reviewer_identity", "approved_at", "revalidation_date"})
}
func endpointShape(raw []byte) bool {
	var root map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil {
		return false
	}
	return exactArrayRaw(root, "services", []string{"service_id", "endpoint_id", "endpoint_url", "tls_server_name", "certificate_sha256"})
}
func exactRaw(root map[string]json.RawMessage, key string, keys []string) bool {
	var child map[string]json.RawMessage
	if json.Unmarshal(root[key], &child) != nil {
		return false
	}
	return exactKeys(child, keys)
}
func exactArrayRaw(root map[string]json.RawMessage, key string, keys []string) bool {
	var array []map[string]json.RawMessage
	if json.Unmarshal(root[key], &array) != nil {
		return false
	}
	for _, child := range array {
		if !exactKeys(child, keys) {
			return false
		}
	}
	return true
}
