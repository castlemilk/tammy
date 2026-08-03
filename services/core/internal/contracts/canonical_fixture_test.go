package contracts_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

type canonicalFixtureDocument struct {
	SchemaVersion     int                           `json:"schemaVersion"`
	MessageType       string                        `json:"messageType"`
	Cases             []canonicalFixtureCase        `json:"cases"`
	UnknownFieldCases []canonicalUnknownFixtureCase `json:"unknownFieldCases"`
}

type canonicalFixtureCase struct {
	Name           string          `json:"name"`
	Input          json.RawMessage `json:"input"`
	ExpectedJSON   json.RawMessage `json:"expectedNormalizedJson"`
}

type canonicalUnknownFixtureCase struct {
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type transitionFixtureDocument struct {
	SchemaVersion int                     `json:"schemaVersion"`
	Transitions   []transitionFixtureEdge `json:"transitions"`
}

type transitionFixtureEdge struct {
	Enum       string `json:"enum"`
	Transition string `json:"transition"`
}

func loadCanonicalFixture(t *testing.T) canonicalFixtureDocument {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve canonical fixture test path")
	}
	fixturePath := filepath.Join(filepath.Dir(sourceFile), "../../../../test/fixtures/proto/canonical-requests.json")
	source, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read canonical fixture: %v", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(source))
	decoder.DisallowUnknownFields()
	var fixture canonicalFixtureDocument
	if err := decoder.Decode(&fixture); err != nil {
		t.Fatalf("decode canonical fixture: %v", err)
	}
	if fixture.SchemaVersion != 1 || fixture.MessageType != "tammy.v1.CanonicalRequest" {
		t.Fatalf("unexpected canonical fixture header: version=%d type=%q", fixture.SchemaVersion, fixture.MessageType)
	}
	return fixture
}

func canonicalCase(t *testing.T, name string) canonicalFixtureCase {
	t.Helper()
	for _, fixtureCase := range loadCanonicalFixture(t).Cases {
		if fixtureCase.Name == name {
			return fixtureCase
		}
	}
	t.Fatalf("canonical fixture case %q not found", name)
	return canonicalFixtureCase{}
}

func canonicalUnknownCase(t *testing.T, name string) canonicalUnknownFixtureCase {
	t.Helper()
	for _, fixtureCase := range loadCanonicalFixture(t).UnknownFieldCases {
		if fixtureCase.Name == name {
			return fixtureCase
		}
	}
	t.Fatalf("canonical unknown-field fixture case %q not found", name)
	return canonicalUnknownFixtureCase{}
}

func normalizeCanonicalRequest(t *testing.T, input json.RawMessage) []byte {
	t.Helper()
	request := &tammyv1.CanonicalRequest{}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(input, request); err != nil {
		t.Fatalf("unmarshal canonical request: %v", err)
	}
	if request.UpdateMask != nil {
		paths := append([]string(nil), request.UpdateMask.Paths...)
		sort.Strings(paths)
		request.UpdateMask.Paths = paths[:0]
		for _, path := range paths {
			if len(request.UpdateMask.Paths) == 0 || request.UpdateMask.Paths[len(request.UpdateMask.Paths)-1] != path {
				request.UpdateMask.Paths = append(request.UpdateMask.Paths, path)
			}
		}
	}
	normalized, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(request)
	if err != nil {
		t.Fatalf("marshal normalized canonical request: %v", err)
	}
	return normalized
}

func assertJSONEqual(t *testing.T, want, got []byte) {
	t.Helper()
	var wantValue any
	if err := json.Unmarshal(want, &wantValue); err != nil {
		t.Fatalf("decode expected JSON: %v", err)
	}
	var gotValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("decode actual JSON: %v", err)
	}
	wantJSON, _ := json.Marshal(wantValue)
	gotJSON, _ := json.Marshal(gotValue)
	if !bytes.Equal(wantJSON, gotJSON) {
		t.Fatalf("normalized JSON mismatch\nwant: %s\n got: %s", wantJSON, gotJSON)
	}
}

func TestSliceOneDescriptorsExposeSecureLedgerContract(t *testing.T) {
	wantMessages := []protoreflect.FullName{
		"tammy.v1.Money",
		"tammy.v1.Decimal",
		"tammy.v1.CivilDate",
		"tammy.v1.SourceRef",
		"tammy.v1.CommandContext",
		"tammy.v1.PageRequest",
		"tammy.v1.TotpCodeInput",
		"tammy.v1.ValidationErrorDetail",
		"tammy.v1.AuthenticationErrorDetail",
		"tammy.v1.PermissionDeniedErrorDetail",
		"tammy.v1.FreshFactorRequiredErrorDetail",
		"tammy.v1.StaleVersionErrorDetail",
		"tammy.v1.InvalidStateTransitionErrorDetail",
		"tammy.v1.IdempotencyConflictErrorDetail",
		"tammy.v1.DuplicateSourceErrorDetail",
		"tammy.v1.ClosedPeriodErrorDetail",
		"tammy.v1.ImbalanceErrorDetail",
		"tammy.v1.EvidenceCorruptionErrorDetail",
	}
	wantEnums := []protoreflect.FullName{
		"tammy.v1.WorkspaceState",
		"tammy.v1.WorkspaceTrustState",
		"tammy.v1.UserState",
		"tammy.v1.SessionState",
		"tammy.v1.FactorState",
		"tammy.v1.OrganisationVerificationState",
		"tammy.v1.AccountStatus",
		"tammy.v1.OpeningConversionState",
		"tammy.v1.JournalState",
		"tammy.v1.PeriodState",
		"tammy.v1.BackupJobState",
		"tammy.v1.RestoreState",
		"tammy.v1.PreRestoreArchiveState",
		"tammy.v1.PreRestoreArchiveExportJobState",
		"tammy.v1.AuditExportJobState",
	}
	wantRPCs := map[protoreflect.FullName][]protoreflect.Name{
		"tammy.v1.WorkspaceService": {
			"CreateWorkspace", "ConfirmRecovery", "UnlockWorkspace", "LockWorkspace",
			"ForgetRememberedWorkspace", "GetWorkspaceState", "ChangePassphrase",
			"RecoverWorkspace", "EstablishMovedWorkspaceTrust", "BackupWorkspace",
			"CancelBackup", "GetBackupJob", "ListBackupJobs", "RestoreWorkspace",
			"GetRestoreStatus", "ExportPreRestoreArchive", "CancelPreRestoreArchiveExport",
			"GetPreRestoreArchiveExportJob", "ListPreRestoreArchiveExportJobs",
			"DeletePreRestoreArchive", "GetPreRestoreArchive", "ListPreRestoreArchives",
			"TransferOwnership",
		},
		"tammy.v1.IdentityService": {
			"SignIn", "SignOut", "GetSession", "GetCurrentUser", "CreateUser",
			"AssignRoles", "ResetUserAuthentication", "GetUser", "ListUsers",
			"ActivateUser", "ChangePassword", "EnrolTOTP", "ConfirmTOTP",
			"AssertTOTP", "DisableTOTP", "RecoverAdministrator",
		},
		"tammy.v1.OrganisationService": {
			"CreateOrganisation", "UpdateOrganisation", "RecordEntityVerification",
			"GetOrganisation",
		},
		"tammy.v1.AccountingService": {
			"CreateAccount", "UpdateAccount", "SetAccountStatus", "ListTaxCodes",
			"PostOpeningConversion", "ReplaceOpeningConversion", "PostManualJournal",
			"ReverseJournal", "ClosePeriod", "ReopenPeriod", "GetAccount",
			"ListAccounts", "GetJournal", "ListJournals", "GetGeneralLedger",
			"GetTrialBalance",
		},
		"tammy.v1.AuditService": {
			"VerifyChain", "ListAuditEvents", "ExportEvidence", "CancelAuditExport",
			"GetAuditExportJob", "ListAuditExportJobs",
		},
	}

	var missing []string
	for _, fullName := range wantMessages {
		descriptor, err := protoregistry.GlobalFiles.FindDescriptorByName(fullName)
		if err != nil {
			missing = append(missing, string(fullName))
			continue
		}
		if _, ok := descriptor.(protoreflect.MessageDescriptor); !ok {
			missing = append(missing, fmt.Sprintf("%s (not a message)", fullName))
		}
	}
	for _, fullName := range wantEnums {
		descriptor, err := protoregistry.GlobalFiles.FindDescriptorByName(fullName)
		if err != nil {
			missing = append(missing, string(fullName))
			continue
		}
		if _, ok := descriptor.(protoreflect.EnumDescriptor); !ok {
			missing = append(missing, fmt.Sprintf("%s (not an enum)", fullName))
		}
	}
	for serviceName, methodNames := range wantRPCs {
		descriptor, err := protoregistry.GlobalFiles.FindDescriptorByName(serviceName)
		if err != nil {
			missing = append(missing, string(serviceName))
			continue
		}
		service, ok := descriptor.(protoreflect.ServiceDescriptor)
		if !ok {
			missing = append(missing, fmt.Sprintf("%s (not a service)", serviceName))
			continue
		}
		for _, methodName := range methodNames {
			if service.Methods().ByName(methodName) == nil {
				missing = append(missing, fmt.Sprintf("%s.%s", serviceName, methodName))
			}
		}
	}
	if len(missing) > 0 {
		t.Fatalf("Slice 1 descriptors are missing: %v", missing)
	}
}

func TestSliceOneContractFilesUseProto3Syntax(t *testing.T) {
	for _, path := range []string{
		"tammy/v1/common.proto",
		"tammy/v1/workspace.proto",
		"tammy/v1/identity.proto",
		"tammy/v1/organisation.proto",
		"tammy/v1/accounting.proto",
		"tammy/v1/audit.proto",
		"tammy/v1/events.proto",
		"tammy/v1/fixtures.proto",
	} {
		file, err := protoregistry.GlobalFiles.FindFileByPath(path)
		if err != nil {
			t.Fatalf("find %s: %v", path, err)
		}
		if got := file.Syntax(); got != protoreflect.Proto3 {
			t.Errorf("%s syntax = %s, want proto3", path, got)
		}
	}
}

func TestCanonicalFixtureOmitsAbsentPresenceAwareFields(t *testing.T) {
	fixtureCase := canonicalCase(t, "absent-presence")
	assertJSONEqual(t, fixtureCase.ExpectedJSON, normalizeCanonicalRequest(t, fixtureCase.Input))
}

func TestCanonicalFixtureRetainsExplicitDefaultPresence(t *testing.T) {
	fixtureCase := canonicalCase(t, "explicit-default-presence")
	assertJSONEqual(t, fixtureCase.ExpectedJSON, normalizeCanonicalRequest(t, fixtureCase.Input))
}

func TestCanonicalFixtureSortsAndDeduplicatesFieldMask(t *testing.T) {
	fixtureCase := canonicalCase(t, "field-mask-normalization")
	assertJSONEqual(t, fixtureCase.ExpectedJSON, normalizeCanonicalRequest(t, fixtureCase.Input))
}

func TestCanonicalFixturePreservesRepeatedFieldOrder(t *testing.T) {
	fixtureCase := canonicalCase(t, "repeated-order")
	assertJSONEqual(t, fixtureCase.ExpectedJSON, normalizeCanonicalRequest(t, fixtureCase.Input))
}

func TestCanonicalFixtureUsesDecimalStringsForInt64(t *testing.T) {
	fixtureCase := canonicalCase(t, "int64-decimal-string")
	assertJSONEqual(t, fixtureCase.ExpectedJSON, normalizeCanonicalRequest(t, fixtureCase.Input))
}

func TestCanonicalFixtureNormalizesTimestampToUTC(t *testing.T) {
	fixtureCase := canonicalCase(t, "timestamp-normalization")
	assertJSONEqual(t, fixtureCase.ExpectedJSON, normalizeCanonicalRequest(t, fixtureCase.Input))
}

func TestCanonicalFixtureUsesSymbolicEnumNames(t *testing.T) {
	fixtureCase := canonicalCase(t, "symbolic-enum-name")
	assertJSONEqual(t, fixtureCase.ExpectedJSON, normalizeCanonicalRequest(t, fixtureCase.Input))
}

func TestCanonicalFixtureRejectsUnknownFields(t *testing.T) {
	fixtureCase := canonicalUnknownCase(t, "reject-unknown-field")
	request := &tammyv1.CanonicalRequest{}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(fixtureCase.Input, request); err == nil {
		t.Fatal("strict Protobuf JSON decoder accepted an unknown field")
	}
}

func TestSliceOneTransitionFixtureMatchesDocumentedLifecycleEdges(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve transition fixture test path")
	}
	fixturePath := filepath.Join(filepath.Dir(sourceFile), "../../../../test/fixtures/proto/transitions.pb.json")
	source, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read transition fixture: %v", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(source))
	decoder.DisallowUnknownFields()
	var fixture transitionFixtureDocument
	if err := decoder.Decode(&fixture); err != nil {
		t.Fatalf("decode transition fixture: %v", err)
	}
	if fixture.SchemaVersion != 1 {
		t.Fatalf("transition schemaVersion = %d, want 1", fixture.SchemaVersion)
	}

	want := map[string]struct{}{}
	add := func(enumName string, edges ...string) {
		for _, edge := range edges {
			want[enumName+"."+edge] = struct{}{}
		}
	}
	add("tammy.v1.WorkspaceState",
		"WORKSPACE_STATE_PENDING_RECOVERY->WORKSPACE_STATE_UNAUTHENTICATED",
		"WORKSPACE_STATE_LOCKED->WORKSPACE_STATE_UNAUTHENTICATED",
		"WORKSPACE_STATE_UNAUTHENTICATED->WORKSPACE_STATE_AUTHENTICATED",
		"WORKSPACE_STATE_UNAUTHENTICATED->WORKSPACE_STATE_LOCKED",
		"WORKSPACE_STATE_AUTHENTICATED->WORKSPACE_STATE_LOCKED",
	)
	add("tammy.v1.WorkspaceTrustState",
		"WORKSPACE_TRUST_STATE_TRUSTED->WORKSPACE_TRUST_STATE_MOVED_READ_ONLY",
		"WORKSPACE_TRUST_STATE_MOVED_READ_ONLY->WORKSPACE_TRUST_STATE_TRUSTED",
	)
	add("tammy.v1.UserState",
		"USER_STATE_PENDING_ACTIVATION->USER_STATE_ACTIVE",
		"USER_STATE_ACTIVE->USER_STATE_AUTHENTICATION_LOCKED",
		"USER_STATE_ACTIVE->USER_STATE_PENDING_ACTIVATION",
		"USER_STATE_AUTHENTICATION_LOCKED->USER_STATE_ACTIVE",
		"USER_STATE_AUTHENTICATION_LOCKED->USER_STATE_PENDING_ACTIVATION",
	)
	add("tammy.v1.SessionState",
		"SESSION_STATE_ACTIVE->SESSION_STATE_SIGNED_OUT",
		"SESSION_STATE_ACTIVE->SESSION_STATE_EXPIRED",
		"SESSION_STATE_ACTIVE->SESSION_STATE_INVALIDATED",
	)
	add("tammy.v1.FactorState",
		"FACTOR_STATE_PENDING_CONFIRMATION->FACTOR_STATE_ENABLED",
		"FACTOR_STATE_ENABLED->FACTOR_STATE_DISABLED",
	)
	add("tammy.v1.OrganisationVerificationState",
		"ORGANISATION_VERIFICATION_STATE_UNVERIFIED->ORGANISATION_VERIFICATION_STATE_VERIFIED",
		"ORGANISATION_VERIFICATION_STATE_UNVERIFIED->ORGANISATION_VERIFICATION_STATE_FAILED",
		"ORGANISATION_VERIFICATION_STATE_FAILED->ORGANISATION_VERIFICATION_STATE_VERIFIED",
		"ORGANISATION_VERIFICATION_STATE_FAILED->ORGANISATION_VERIFICATION_STATE_EXPIRED",
		"ORGANISATION_VERIFICATION_STATE_VERIFIED->ORGANISATION_VERIFICATION_STATE_EXPIRED",
		"ORGANISATION_VERIFICATION_STATE_VERIFIED->ORGANISATION_VERIFICATION_STATE_SUPERSEDED",
	)
	add("tammy.v1.AccountStatus",
		"ACCOUNT_STATUS_ACTIVE->ACCOUNT_STATUS_ARCHIVED",
		"ACCOUNT_STATUS_ARCHIVED->ACCOUNT_STATUS_ACTIVE",
	)
	add("tammy.v1.OpeningConversionState",
		"OPENING_CONVERSION_STATE_POSTED->OPENING_CONVERSION_STATE_REPLACED",
	)
	add("tammy.v1.JournalState",
		"JOURNAL_STATE_POSTED->JOURNAL_STATE_REVERSED",
	)
	add("tammy.v1.PeriodState",
		"PERIOD_STATE_OPEN->PERIOD_STATE_CLOSED",
		"PERIOD_STATE_CLOSED->PERIOD_STATE_OPEN",
	)
	add("tammy.v1.BackupJobState",
		"BACKUP_JOB_STATE_QUEUED->BACKUP_JOB_STATE_RUNNING",
		"BACKUP_JOB_STATE_QUEUED->BACKUP_JOB_STATE_CANCELLED",
		"BACKUP_JOB_STATE_RUNNING->BACKUP_JOB_STATE_QUEUED",
		"BACKUP_JOB_STATE_RUNNING->BACKUP_JOB_STATE_COMPLETED",
		"BACKUP_JOB_STATE_RUNNING->BACKUP_JOB_STATE_FAILED_RETRYABLE",
		"BACKUP_JOB_STATE_RUNNING->BACKUP_JOB_STATE_FAILED_TERMINAL",
		"BACKUP_JOB_STATE_RUNNING->BACKUP_JOB_STATE_CANCELLED",
		"BACKUP_JOB_STATE_FAILED_RETRYABLE->BACKUP_JOB_STATE_QUEUED",
		"BACKUP_JOB_STATE_FAILED_RETRYABLE->BACKUP_JOB_STATE_CANCELLED",
	)
	add("tammy.v1.RestoreState",
		"RESTORE_STATE_PREPARED->RESTORE_STATE_STAGED",
		"RESTORE_STATE_STAGED->RESTORE_STATE_SWAPPED",
		"RESTORE_STATE_SWAPPED->RESTORE_STATE_COMPLETE",
	)
	add("tammy.v1.PreRestoreArchiveState",
		"PRE_RESTORE_ARCHIVE_STATE_AVAILABLE->PRE_RESTORE_ARCHIVE_STATE_DELETED",
	)
	add("tammy.v1.PreRestoreArchiveExportJobState",
		"PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_QUEUED->PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_WRITING",
		"PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_QUEUED->PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_CANCELLED",
		"PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_QUEUED->PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_FAILED_RETRYABLE",
		"PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_WRITING->PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_VERIFIED",
		"PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_WRITING->PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_CANCELLED",
		"PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_WRITING->PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_FAILED_RETRYABLE",
		"PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_VERIFIED->PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_COMPLETED",
		"PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_FAILED_RETRYABLE->PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_QUEUED",
		"PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_FAILED_RETRYABLE->PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_CANCELLED",
	)
	add("tammy.v1.AuditExportJobState",
		"AUDIT_EXPORT_JOB_STATE_QUEUED->AUDIT_EXPORT_JOB_STATE_RUNNING",
		"AUDIT_EXPORT_JOB_STATE_QUEUED->AUDIT_EXPORT_JOB_STATE_CANCELLED",
		"AUDIT_EXPORT_JOB_STATE_RUNNING->AUDIT_EXPORT_JOB_STATE_QUEUED",
		"AUDIT_EXPORT_JOB_STATE_RUNNING->AUDIT_EXPORT_JOB_STATE_WAITING_FOR_INPUT",
		"AUDIT_EXPORT_JOB_STATE_RUNNING->AUDIT_EXPORT_JOB_STATE_COMPLETED",
		"AUDIT_EXPORT_JOB_STATE_RUNNING->AUDIT_EXPORT_JOB_STATE_FAILED_RETRYABLE",
		"AUDIT_EXPORT_JOB_STATE_RUNNING->AUDIT_EXPORT_JOB_STATE_FAILED_TERMINAL",
		"AUDIT_EXPORT_JOB_STATE_RUNNING->AUDIT_EXPORT_JOB_STATE_CANCELLED",
		"AUDIT_EXPORT_JOB_STATE_WAITING_FOR_INPUT->AUDIT_EXPORT_JOB_STATE_QUEUED",
		"AUDIT_EXPORT_JOB_STATE_WAITING_FOR_INPUT->AUDIT_EXPORT_JOB_STATE_CANCELLED",
		"AUDIT_EXPORT_JOB_STATE_FAILED_RETRYABLE->AUDIT_EXPORT_JOB_STATE_QUEUED",
		"AUDIT_EXPORT_JOB_STATE_FAILED_RETRYABLE->AUDIT_EXPORT_JOB_STATE_CANCELLED",
	)

	got := make(map[string]struct{}, len(fixture.Transitions))
	for _, transition := range fixture.Transitions {
		id := transition.Enum + "." + transition.Transition
		if _, duplicate := got[id]; duplicate {
			t.Fatalf("duplicate transition %q", id)
		}
		got[id] = struct{}{}
		descriptor, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(transition.Enum))
		if err != nil {
			t.Fatalf("find transition enum %q: %v", transition.Enum, err)
		}
		enumDescriptor, ok := descriptor.(protoreflect.EnumDescriptor)
		if !ok {
			t.Fatalf("transition descriptor %q is not an enum", transition.Enum)
		}
		values := strings.Split(transition.Transition, "->")
		if len(values) != 2 {
			t.Fatalf("transition %q must contain exactly one ->", id)
		}
		for _, value := range values {
			if enumDescriptor.Values().ByName(protoreflect.Name(value)) == nil {
				t.Fatalf("transition %q references unknown enum value %q", id, value)
			}
			if strings.HasSuffix(value, "_UNSPECIFIED") {
				t.Fatalf("transition %q references an unspecified sentinel", id)
			}
		}
	}
	for id := range want {
		if _, ok := got[id]; !ok {
			t.Errorf("missing documented transition %q", id)
		}
	}
	for id := range got {
		if _, ok := want[id]; !ok {
			t.Errorf("undocumented transition %q", id)
		}
	}
}
