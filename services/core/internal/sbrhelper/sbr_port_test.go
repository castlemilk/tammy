package sbrhelper

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/sbr"
	"github.com/tammyapp/tammy/services/core/internal/sbrprofile"
	"google.golang.org/protobuf/encoding/protowire"
)

type fakePortLauncher struct {
	request  Request
	response Response
	err      error
}

type encodingPortLauncher struct {
	now       time.Time
	request   Request
	encoded   []byte
	encodeErr error
}

type portRequestCase struct {
	name    string
	request sbr.HelperRequest
}

var errEncodingPortStopped = errors.New("encoding port stopped")

func (launcher *encodingPortLauncher) LaunchStaged(_ context.Context, _ *sbrprofile.StagedResources, request Request) (Response, error) {
	launcher.request = request
	launcher.encoded, launcher.encodeErr = EncodeRequest(request, launcher.now)
	return Response{}, errEncodingPortStopped
}

type countingProfileLocator struct {
	resources sbrprofile.ResourceSet
	calls     int
}

func (locator *countingProfileLocator) Locate(sbrprofile.Profile) (sbrprofile.ResourceSet, error) {
	locator.calls++
	return locator.resources, nil
}

type snapshotLauncher struct {
	stagedRoot  string
	helperBytes []byte
}

func (launcher *snapshotLauncher) LaunchStaged(_ context.Context, staged *sbrprofile.StagedResources, request Request) (Response, error) {
	launcher.stagedRoot = staged.RuntimeRoot
	value, err := os.ReadFile(staged.HelperPath)
	if err != nil {
		return Response{}, err
	}
	launcher.helperBytes = value
	return Response{RequestID: request.RequestID, Outcome: OutcomeOK, RedactedResult: ResultFixtureSelected,
		ProfileFingerprint: bytes.Clone(request.ProfileFingerprint), RegistrationFingerprint: bytes.Clone(request.RegistrationFingerprint),
		ComponentFingerprint: bytes.Clone(request.ComponentFingerprint), ComponentVersion: request.ComponentVersion,
		SimulatorCase: SimulatorAccepted, SimulatorState: SimulatorStateAccepted}, nil
}

func TestAuthenticatedProfilePortLoadsCommittedSimulatorResources(t *testing.T) {
	root := launcherRepositoryRoot(t)
	resources := filepath.Join(root, "apps", "desktop", "resources")
	profilePath := filepath.Join(resources, "sbr", "simulator", "sbr-profile-v1.json")
	temporary, err := os.MkdirTemp(root, ".sbr-profile-port-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(temporary) })
	runtimeBase := filepath.Join(temporary, "runtime")
	if err := os.Mkdir(runtimeBase, 0o700); err != nil {
		t.Fatal(err)
	}
	locator, err := sbrprofile.NewDirectoryResourceLocator(resources, runtimeBase)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := NewAuthenticatedProfilePort(profilePath, locator)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := provider.Current(context.Background(), time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC))
	if err != nil || profile.Environment != tammyv1.SbrEnvironment_SBR_ENVIRONMENT_SIMULATOR ||
		profile.ComponentVersion != "tammy-sbr-simulator-v1" || profile.Conformance != sbr.ConformanceSimulator ||
		profile.ProfileFingerprint == [sha256.Size]byte{} ||
		profile.RegistrationFingerprint == [sha256.Size]byte{} || profile.ComponentFingerprint == [sha256.Size]byte{} ||
		!profile.AuthenticatedUntil.After(time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("Current() = %+v, %v", profile, err)
	}
}

func TestAuthenticatedProfileLeasePinsSnapshotAcrossSourceRotationAndCleansUp(t *testing.T) {
	now := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	root, err := os.MkdirTemp(launcherRepositoryRoot(t), ".sbr-profile-rotation-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	helperPath := filepath.Join(root, "helper")
	runtimeBase := filepath.Join(root, "runtime")
	if err := os.Mkdir(runtimeBase, 0o700); err != nil {
		t.Fatal(err)
	}
	helperA, helperB := []byte("authenticated helper A"), []byte("rotated helper B")
	writeLauncherFile(t, helperPath, helperA, 0o500)
	profilePath := writeLauncherProfile(t, root, helperA, now)
	locator := &countingProfileLocator{resources: sbrprofile.ResourceSet{HelperPath: helperPath, TrustedRuntimeBase: runtimeBase}}
	launcher := &snapshotLauncher{}
	provider := &AuthenticatedProfilePort{profilePath: profilePath, locator: locator, launcher: launcher, now: func() time.Time { return now }}
	profile, err := provider.Current(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(helperPath, 0o700); err != nil {
		t.Fatal(err)
	}
	writeLauncherFile(t, helperPath, helperB, 0o500)
	_ = writeLauncherProfile(t, root, helperB, now)
	result, err := profile.Execute(context.Background(), sbr.HelperRequest{Operation: sbr.HelperOperationFixture,
		RequestID: "018f0000-0000-7000-8000-000000000712", Environment: profile.Environment,
		WorkspaceID: "018f0000-0000-7000-8000-000000000701", OrganisationID: "018f0000-0000-7000-8000-000000000702",
		CanonicalABN: "11000000560", OpaqueScope: bytes.Repeat([]byte{0x52}, sha256.Size),
		ProfileFingerprint: profile.ProfileFingerprint, RegistrationFingerprint: profile.RegistrationFingerprint,
		ComponentFingerprint: profile.ComponentFingerprint, ComponentVersion: profile.ComponentVersion})
	if err != nil || result.RequestID == "" || locator.calls != 1 || !bytes.Equal(launcher.helperBytes, helperA) {
		t.Fatalf("pinned execution result=%+v error=%v locate calls=%d helper=%q", result, err, locator.calls, launcher.helperBytes)
	}
	rootPath := launcher.stagedRoot
	if err := profile.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(rootPath); !os.IsNotExist(err) {
		t.Fatalf("closed lease retained staged root %q: %v", rootPath, err)
	}
}

func TestAuthenticatedSimulatorPortLaunchesCurrentHelperAndClassifiesFailures(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("authenticated simulator helper is a macOS arm64 resource")
	}
	probe := exec.Command("/usr/bin/sandbox-exec", "-p", "(version 1) (allow default)", "/usr/bin/true")
	if output, probeErr := probe.CombinedOutput(); probeErr != nil {
		if sandboxApplyUnavailable(output, probeErr) {
			t.Skip("SBR_SANDBOX_EXEC_UNAVAILABLE")
		}
		t.Fatalf("sandbox availability probe: %v\n%s", probeErr, output)
	}
	root := launcherRepositoryRoot(t)
	temporary, err := os.MkdirTemp(root, ".sbr-actual-port-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(temporary) })
	runtimeBase := filepath.Join(temporary, "runtime")
	if err := os.Mkdir(runtimeBase, 0o700); err != nil {
		t.Fatal(err)
	}
	helperPath := filepath.Join(temporary, "tammy-sbr-helper")
	command := exec.Command("go", "build", "-o", helperPath, "./cmd/tammy-sbr-helper")
	command.Dir = helperModuleRoot(t)
	command.Env = append(os.Environ(), "GOWORK=off", "GOCACHE=/private/tmp/tammy-go-cache", "GOTMPDIR=/private/tmp")
	if output, buildErr := command.CombinedOutput(); buildErr != nil {
		t.Fatalf("build current helper: %v\n%s", buildErr, output)
	}
	if err := os.Chmod(helperPath, 0o500); err != nil {
		t.Fatal(err)
	}
	profilePath := writeLauncherProfile(t, temporary, mustReadFile(t, helperPath), time.Now().UTC())
	locator := launcherLocator{sbrprofile.ResourceSet{HelperPath: helperPath, TrustedRuntimeBase: runtimeBase}}
	now := time.Now
	profiles, err := NewAuthenticatedProfilePort(profilePath, locator)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := profiles.Current(context.Background(), now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	defer profile.Close()
	request := sbr.HelperRequest{Operation: sbr.HelperOperationFixture,
		RequestID: "018f0000-0000-7000-8000-000000000712", Environment: profile.Environment,
		WorkspaceID: "018f0000-0000-7000-8000-000000000701", OrganisationID: "018f0000-0000-7000-8000-000000000702",
		CanonicalABN: "11000000560", OpaqueScope: bytes.Repeat([]byte{0x52}, sha256.Size),
		ProfileFingerprint: profile.ProfileFingerprint, RegistrationFingerprint: profile.RegistrationFingerprint,
		ComponentFingerprint: profile.ComponentFingerprint, ComponentVersion: profile.ComponentVersion}
	result, err := profile.Execute(context.Background(), request)
	if err != nil || result.Outcome != sbr.HelperOutcomeOK || result.ResultCode != sbr.HelperResultFixtureSelected || result.FixtureState != sbr.TransportAccepted {
		t.Fatalf("actual helper Execute() = %+v, %v", result, err)
	}

	malformed := request
	malformed.RequestID = "018f0000-0000-7000-8000-000000000713"
	malformed.FixtureFailureCase = tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_MALFORMED_RESPONSE
	if _, err = profile.Execute(context.Background(), malformed); !errors.Is(err, sbr.ErrHelperMalformedResponse) || err.Error() != "SBR_HELPER_MALFORMED_RESPONSE" {
		t.Fatalf("malformed helper response error = %v", err)
	}

	death := request
	death.RequestID = "018f0000-0000-7000-8000-000000000714"
	death.FixtureFailureCase = tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_HELPER_DEATH
	if _, err = profile.Execute(context.Background(), death); err == nil || err.Error() != string(StableErrorHelperUnavailable) ||
		errors.Is(err, sbr.ErrHelperMalformedResponse) || errors.Is(err, sbr.ErrHelperDeadlineExpired) {
		t.Fatalf("helper death error = %v", err)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	value, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func (launcher *fakePortLauncher) LaunchStaged(_ context.Context, _ *sbrprofile.StagedResources, request Request) (Response, error) {
	launcher.request = request
	launcher.request.OpaqueScope = bytes.Clone(request.OpaqueScope)
	return launcher.response, launcher.err
}

func TestSBRPortForwardsServerDerivedScopeAndMapsAuthenticatedSimulatorFixture(t *testing.T) {
	profile, registration, component := sha256.Sum256([]byte("profile")), sha256.Sum256([]byte("registration")), sha256.Sum256([]byte("component"))
	launcher := &fakePortLauncher{response: Response{RequestID: "018f0000-0000-7000-8000-000000000711",
		Outcome: OutcomeOK, RedactedResult: ResultFixtureSelected, SimulatorCase: SimulatorAccepted, SimulatorState: SimulatorStateAccepted,
		ProfileFingerprint: profile[:], RegistrationFingerprint: registration[:], ComponentFingerprint: component[:], ComponentVersion: "simulator-v1"}}
	port, err := NewSBRPort(launcher, "/Applications/Tammy.app/Contents/Resources/sbr/simulator/sbr-profile-v1.json",
		func() time.Time { return time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	scope := bytes.Repeat([]byte{0x51}, sha256.Size)
	result, err := port.executeStaged(context.Background(), nil, sbr.HelperRequest{
		Operation: sbr.HelperOperationFixture, RequestID: "018f0000-0000-7000-8000-000000000711",
		Environment: tammyv1.SbrEnvironment_SBR_ENVIRONMENT_SIMULATOR,
		WorkspaceID: "018f0000-0000-7000-8000-000000000701", OrganisationID: "018f0000-0000-7000-8000-000000000702",
		CanonicalABN: "11000000560", OpaqueScope: scope, ProfileFingerprint: profile,
		RegistrationFingerprint: registration, ComponentFingerprint: component, ComponentVersion: "simulator-v1",
	})
	if err != nil || result.Outcome != sbr.HelperOutcomeOK || result.ResultCode != sbr.HelperResultFixtureSelected ||
		result.FixtureState != sbr.TransportAccepted || result.ProfileFingerprint != profile ||
		result.RegistrationFingerprint != registration || result.ComponentFingerprint != component {
		t.Fatalf("Execute() = %+v, %v", result, err)
	}
	if launcher.request.WorkspaceID != "018f0000-0000-7000-8000-000000000701" ||
		launcher.request.OrganisationID != "018f0000-0000-7000-8000-000000000702" ||
		launcher.request.CanonicalABN != "11000000560" || !bytes.Equal(launcher.request.OpaqueScope, scope) {
		t.Fatalf("launcher request lost server-derived scope: %+v", launcher.request)
	}
}

func TestSBRPortRejectsResponseWithoutAuthenticatedProfileEnvelope(t *testing.T) {
	requestID := "018f0000-0000-7000-8000-000000000711"
	port, err := NewSBRPort(&fakePortLauncher{response: Response{RequestID: requestID, Outcome: OutcomeOK,
		RedactedResult: ResultFixtureSelected, SimulatorCase: SimulatorAccepted, SimulatorState: SimulatorStateAccepted}},
		"/Applications/Tammy.app/Contents/Resources/sbr/simulator/sbr-profile-v1.json",
		func() time.Time { return time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	profile, registration, component := sha256.Sum256([]byte("profile")), sha256.Sum256([]byte("registration")), sha256.Sum256([]byte("component"))
	_, err = port.executeStaged(context.Background(), nil, sbr.HelperRequest{Operation: sbr.HelperOperationFixture, RequestID: requestID,
		Environment: tammyv1.SbrEnvironment_SBR_ENVIRONMENT_SIMULATOR,
		WorkspaceID: "018f0000-0000-7000-8000-000000000701", OrganisationID: "018f0000-0000-7000-8000-000000000702",
		CanonicalABN: "11000000560", OpaqueScope: bytes.Repeat([]byte{0x51}, sha256.Size),
		ProfileFingerprint: profile, RegistrationFingerprint: registration, ComponentFingerprint: component, ComponentVersion: "simulator-v1"})
	if err == nil {
		t.Fatal("response without authenticated profile envelope was accepted")
	}
}

func TestSBRPortMapsRunnerAndLauncherDeadlineCodesToCoreDeadline(t *testing.T) {
	requestID := "018f0000-0000-7000-8000-000000000711"
	for _, launcher := range []*fakePortLauncher{
		{response: Response{RequestID: requestID, Outcome: OutcomeError, StableErrorCode: StableErrorDeadlineExpired}},
		{err: protocolError(string(StableErrorDeadlineExpired))},
	} {
		port, err := NewSBRPort(launcher, "/Applications/Tammy.app/Contents/Resources/sbr/simulator/sbr-profile-v1.json",
			func() time.Time { return time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC) })
		if err != nil {
			t.Fatal(err)
		}
		_, err = port.executeStaged(context.Background(), nil, sbr.HelperRequest{Operation: sbr.HelperOperationFixture, RequestID: requestID,
			Environment: tammyv1.SbrEnvironment_SBR_ENVIRONMENT_SIMULATOR, WorkspaceID: "018f0000-0000-7000-8000-000000000701",
			OrganisationID: "018f0000-0000-7000-8000-000000000702", CanonicalABN: "11000000560",
			OpaqueScope: bytes.Repeat([]byte{0x51}, sha256.Size)})
		if !errors.Is(err, sbr.ErrHelperDeadlineExpired) {
			t.Fatalf("deadline mapping error = %v", err)
		}
	}
}

func TestSBRPortMapsOnlyClosedLauncherMalformedResponseToCore(t *testing.T) {
	requestID := "018f0000-0000-7000-8000-000000000715"
	port, err := NewSBRPort(&fakePortLauncher{err: errMalformedHelperResponse},
		"/Applications/Tammy.app/Contents/Resources/sbr/simulator/sbr-profile-v1.json",
		func() time.Time { return time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	_, err = port.executeStaged(context.Background(), nil, sbr.HelperRequest{Operation: sbr.HelperOperationFixture, RequestID: requestID,
		Environment: tammyv1.SbrEnvironment_SBR_ENVIRONMENT_SIMULATOR, WorkspaceID: "018f0000-0000-7000-8000-000000000701",
		OrganisationID: "018f0000-0000-7000-8000-000000000702", CanonicalABN: "11000000560",
		OpaqueScope: bytes.Repeat([]byte{0x51}, sha256.Size)})
	if !errors.Is(err, sbr.ErrHelperMalformedResponse) || err.Error() != "SBR_HELPER_MALFORMED_RESPONSE" {
		t.Fatalf("malformed mapping error = %v", err)
	}
}

func TestSBRPortRejectsUnknownFixtureCaseInsteadOfDefaultingToAccepted(t *testing.T) {
	launcher := &fakePortLauncher{}
	port, err := NewSBRPort(launcher, "/Applications/Tammy.app/Contents/Resources/sbr/simulator/sbr-profile-v1.json",
		func() time.Time { return time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	_, err = port.executeStaged(context.Background(), nil, sbr.HelperRequest{Operation: sbr.HelperOperationFixture,
		RequestID: "018f0000-0000-7000-8000-000000000716", FixtureFailureCase: 256})
	if err == nil || launcher.request.RequestID != "" {
		t.Fatalf("unknown fixture case error=%v launcher request=%+v", err, launcher.request)
	}
}

func TestSBRPortKeepsSimulatorCaseAbsentForEveryNonFixtureOperation(t *testing.T) {
	now := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	base := sbr.HelperRequest{RequestID: "018f0000-0000-7000-8000-000000000716",
		Environment: tammyv1.SbrEnvironment_SBR_ENVIRONMENT_SIMULATOR,
		WorkspaceID: "018f0000-0000-7000-8000-000000000701", OrganisationID: "018f0000-0000-7000-8000-000000000702",
		CanonicalABN: "11000000560", OpaqueScope: bytes.Repeat([]byte{0x51}, sha256.Size),
		ProfileFingerprint: sha256.Sum256([]byte("profile")), RegistrationFingerprint: sha256.Sum256([]byte("registration")),
		ComponentFingerprint: sha256.Sum256([]byte("component")), ComponentVersion: "simulator-v1"}
	for _, test := range nonFixturePortRequests(base) {
		t.Run(test.name, func(t *testing.T) {
			launcher := &encodingPortLauncher{now: now}
			port, err := NewSBRPort(launcher, "/Applications/Tammy.app/Contents/Resources/sbr/simulator/sbr-profile-v1.json",
				func() time.Time { return now })
			if err != nil {
				t.Fatal(err)
			}
			if _, err = port.executeStaged(context.Background(), nil, test.request); !errors.Is(err, errEncodingPortStopped) {
				t.Fatalf("execute error=%v", err)
			}
			if launcher.encodeErr != nil || launcher.request.SimulatorCase != 0 || requestWireHasField(launcher.encoded, requestFieldSimulatorCase) {
				t.Fatalf("launcher request simulator case=%d encoded field=%t encode error=%v",
					launcher.request.SimulatorCase, requestWireHasField(launcher.encoded, requestFieldSimulatorCase), launcher.encodeErr)
			}
			decoded, err := DecodeRequest(launcher.encoded, now)
			if err != nil || decoded.SimulatorCase != 0 {
				t.Fatalf("decoded launcher request=%+v error=%v", decoded, err)
			}
		})
	}
}

func TestSBRPortRejectsFixtureCaseOnEveryNonFixtureOperationBeforeLauncher(t *testing.T) {
	base := sbr.HelperRequest{}
	for _, test := range nonFixturePortRequests(base) {
		t.Run(test.name, func(t *testing.T) {
			launcher := &fakePortLauncher{}
			port, err := NewSBRPort(launcher, "/Applications/Tammy.app/Contents/Resources/sbr/simulator/sbr-profile-v1.json", time.Now)
			if err != nil {
				t.Fatal(err)
			}
			test.request.FixtureFailureCase = tammyv1.SbrReadinessFixtureFailure_SBR_READINESS_FIXTURE_FAILURE_NOT_STARTED
			_, err = port.executeStaged(context.Background(), nil, test.request)
			if err == nil || launcher.request.RequestID != "" {
				t.Fatalf("error=%v launcher request=%+v", err, launcher.request)
			}
		})
	}
}

func nonFixturePortRequests(base sbr.HelperRequest) []portRequestCase {
	return []portRequestCase{
		{name: "status", request: withPortOperation(base, sbr.HelperOperationStatus)},
		{name: "unlock", request: func() sbr.HelperRequest {
			request := withPortOperation(base, sbr.HelperOperationUnlock)
			request.Password = []byte("123456")
			return request
		}()},
		{name: "credential mutation", request: func() sbr.HelperRequest {
			request := withPortOperation(base, sbr.HelperOperationPrepareMutation)
			request.OperationID = "018f0000-0000-7000-8000-000000000717"
			request.MutationKind = sbr.MutationImportCredential
			request.SelectedLocalPath = "/tmp/credential.p12"
			request.Bookmark = []byte("bookmark")
			request.Password = []byte("secret")
			return request
		}()},
		{name: "Product mutation", request: func() sbr.HelperRequest {
			request := withPortOperation(base, sbr.HelperOperationPrepareMutation)
			request.OperationID = "018f0000-0000-7000-8000-000000000718"
			request.MutationKind = sbr.MutationImportProductID
			request.ProductID = []byte("product-secret")
			request.ProductIdentifier = "PAYROLL"
			request.ServiceIdentifier = "SBR_GST"
			return request
		}()},
	}
}

func withPortOperation(request sbr.HelperRequest, operation sbr.HelperOperation) sbr.HelperRequest {
	request.Operation = operation
	return request
}

func requestWireHasField(encoded []byte, target protowire.Number) bool {
	for len(encoded) > 0 {
		number, wireType, tagLength := protowire.ConsumeTag(encoded)
		if tagLength < 0 {
			return false
		}
		encoded = encoded[tagLength:]
		valueLength := protowire.ConsumeFieldValue(number, wireType, encoded)
		if valueLength < 0 {
			return false
		}
		if number == target {
			return true
		}
		encoded = encoded[valueLength:]
	}
	return false
}
