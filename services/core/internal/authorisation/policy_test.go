package authorisation

import (
	"errors"
	"slices"
	"sort"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/faults"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var publicRoles = []tammyv1.Role{
	tammyv1.Role_ROLE_WORKSPACE_ADMIN,
	tammyv1.Role_ROLE_BUSINESS_PREPARER,
	tammyv1.Role_ROLE_BUSINESS_LODGER,
	tammyv1.Role_ROLE_AUDITOR,
}

func TestPolicyRoleMatrix(t *testing.T) {
	tests := []struct {
		name    string
		role    tammyv1.Role
		action  Action
		allowed bool
	}{
		{"admin manages users", tammyv1.Role_ROLE_WORKSPACE_ADMIN, ActionManageUsers, true},
		{"preparer posts", tammyv1.Role_ROLE_BUSINESS_PREPARER, ActionPostAccounting, true},
		{"lodger cannot post", tammyv1.Role_ROLE_BUSINESS_LODGER, ActionPostAccounting, false},
		{"lodger lodges", tammyv1.Role_ROLE_BUSINESS_LODGER, ActionLodge, true},
		{"admin is not implicitly lodger", tammyv1.Role_ROLE_WORKSPACE_ADMIN, ActionLodge, false},
		{"auditor reads audit", tammyv1.Role_ROLE_AUDITOR, ActionReadAudit, true},
		{"unspecified denied", tammyv1.Role_ROLE_UNSPECIFIED, ActionReadFinancial, false},
		{"unknown action denied", tammyv1.Role_ROLE_WORKSPACE_ADMIN, Action("future_action"), false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := Authorize([]tammyv1.Role{test.role}, test.action)
			if test.allowed && err != nil {
				t.Fatal(err)
			}
			if !test.allowed && !errors.Is(err, faults.New(faults.CodePermissionDenied, nil)) {
				t.Fatalf("got %v, want permission denied", err)
			}
		})
	}
}

func TestValidateFreshFactor(t *testing.T) {
	now := time.Date(2026, 8, 4, 0, 5, 0, 0, time.UTC)
	const assertionID = "01890f3c-7b2e-7cc4-98c4-dc0c0c073982"
	existingMarker := &tammyv1.FreshFactorContext{
		AssertionId: assertionID,
		Purpose:     "change_passphrase",
		AssertedAt:  timestamppb.New(now.Add(-5*time.Minute + time.Nanosecond)),
	}
	if err := ValidateFreshFactor(existingMarker, "change_passphrase", now); err != nil {
		t.Fatalf("existing purpose: %v", err)
	}

	purposes := []string{
		"financial_close_freeze",
		"financial_close_reopen",
		"financial_close_start_correction",
		"company_tax_edit_secrets",
		"company_return_acknowledge_warning",
		"company_return_declare",
		"company_return_withdraw_declaration",
		"company_return_export",
		"company_return_prelodge",
		"company_return_lodge",
		"company_return_reconcile_unknown",
	}
	for _, purpose := range purposes {
		t.Run("accepts "+purpose, func(t *testing.T) {
			marker := &tammyv1.FreshFactorContext{
				AssertionId: assertionID,
				Purpose:     purpose,
				AssertedAt:  timestamppb.New(now.Add(-5*time.Minute + time.Nanosecond)),
			}
			if err := ValidateFreshFactor(marker, purpose, now); err != nil {
				t.Fatal(err)
			}
		})
	}

	validMarker := func() *tammyv1.FreshFactorContext {
		return &tammyv1.FreshFactorContext{
			AssertionId: assertionID,
			Purpose:     purposes[0],
			AssertedAt:  timestamppb.New(now.Add(-time.Minute)),
		}
	}
	assertAuthenticationRequired := func(t *testing.T, name string, marker *tammyv1.FreshFactorContext, purpose string) {
		t.Helper()
		if err := ValidateFreshFactor(marker, purpose, now); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
			t.Fatalf("%s returned %v", name, err)
		}
	}

	assertAuthenticationRequired(t, "missing marker", nil, purposes[0])

	marker := validMarker()
	assertAuthenticationRequired(t, "purpose mismatch", marker, purposes[1])

	marker = validMarker()
	marker.AssertedAt = timestamppb.New(now.Add(time.Nanosecond))
	assertAuthenticationRequired(t, "future assertion", marker, purposes[0])

	marker = validMarker()
	marker.AssertedAt = timestamppb.New(now.Add(-5 * time.Minute))
	assertAuthenticationRequired(t, "exactly five minutes old assertion", marker, purposes[0])

	marker = validMarker()
	marker.AssertionId = "not-a-uuidv7"
	assertAuthenticationRequired(t, "malformed assertion ID", marker, purposes[0])
}

func TestCompanyEOFYAuthorizationMatrix(t *testing.T) {
	want := map[Action]map[tammyv1.Role]bool{
		ActionReadFinancial: {
			tammyv1.Role_ROLE_WORKSPACE_ADMIN:   true,
			tammyv1.Role_ROLE_BUSINESS_PREPARER: true,
			tammyv1.Role_ROLE_BUSINESS_LODGER:   true,
			tammyv1.Role_ROLE_AUDITOR:           true,
		},
		ActionPrepareTax: {
			tammyv1.Role_ROLE_WORKSPACE_ADMIN:   true,
			tammyv1.Role_ROLE_BUSINESS_PREPARER: true,
		},
		ActionApproveFinancialClose: {
			tammyv1.Role_ROLE_BUSINESS_LODGER: true,
		},
		ActionDeclareCompanyReturn: {
			tammyv1.Role_ROLE_BUSINESS_LODGER: true,
		},
		ActionLodge: {
			tammyv1.Role_ROLE_BUSINESS_LODGER: true,
		},
	}

	for action, matrix := range want {
		for _, role := range publicRoles {
			err := Authorize([]tammyv1.Role{role}, action)
			if (err == nil) != matrix[role] {
				t.Errorf("action %q role %s allowed=%v, want %v", action, role, err == nil, matrix[role])
			}
		}
	}
}

func TestCompanyEOFYRPCActionGroups(t *testing.T) {
	rpcActions := map[string]Action{
		"CreateFinancialClose":                    ActionPrepareTax,
		"GetFinancialClose":                       ActionReadFinancial,
		"ListCloseChecks":                         ActionReadFinancial,
		"ResolveCloseWarning":                     ActionPrepareTax,
		"FreezeFinancialClose":                    ActionApproveFinancialClose,
		"ReopenFinancialClose":                    ActionPrepareTax,
		"StartFinancialCloseCorrection":           ActionPrepareTax,
		"GetFinancialStatements":                  ActionReadFinancial,
		"GetCompanyTaxProfile":                    ActionReadFinancial,
		"SetCompanyTaxProfile":                    ActionPrepareTax,
		"CreateCompanyReturn":                     ActionPrepareTax,
		"GetCompanyReturn":                        ActionReadFinancial,
		"ListCompanyReturnFacts":                  ActionReadFinancial,
		"SetCompanyReturnInput":                   ActionPrepareTax,
		"UpsertTaxAdjustment":                     ActionPrepareTax,
		"RemoveTaxAdjustment":                     ActionPrepareTax,
		"UpsertTaxElection":                       ActionPrepareTax,
		"RemoveTaxElection":                       ActionPrepareTax,
		"ValidateCompanyReturn":                   ActionPrepareTax,
		"AcknowledgeReturnWarning":                ActionDeclareCompanyReturn,
		"DeclareCompanyReturn":                    ActionDeclareCompanyReturn,
		"WithdrawCompanyReturnDeclaration":        ActionDeclareCompanyReturn,
		"ExportCompanyReturnPack":                 ActionPrepareTax,
		"CreateCompanyReturnReplacement":          ActionPrepareTax,
		"CreateCompanyReturnAmendment":            ActionPrepareTax,
		"PreLodgeCompanyReturn":                   ActionLodge,
		"LodgeCompanyReturn":                      ActionLodge,
		"GetCompanyReturnSubmission":              ActionReadFinancial,
		"RefreshCompanyReturnStatus":              ActionLodge,
		"ReconcileUnknownCompanyReturnSubmission": ActionLodge,
	}
	if len(rpcActions) != 30 {
		t.Fatalf("mapped %d RPCs, want 30", len(rpcActions))
	}

	wantGroupSizes := map[Action]int{
		ActionReadFinancial:         7,
		ActionPrepareTax:            15,
		ActionApproveFinancialClose: 1,
		ActionDeclareCompanyReturn:  3,
		ActionLodge:                 4,
	}
	gotGroupSizes := make(map[Action]int, len(wantGroupSizes))
	for rpc, action := range rpcActions {
		if _, ok := wantGroupSizes[action]; !ok {
			t.Errorf("RPC %s has unexpected action %q", rpc, action)
		}
		gotGroupSizes[action]++
	}
	for action, want := range wantGroupSizes {
		if got := gotGroupSizes[action]; got != want {
			t.Errorf("action %q has %d RPCs, want %d", action, got, want)
		}
	}

	wantRPCs := companyEOFYDescriptorRPCNames(t)
	gotRPCs := make([]string, 0, len(rpcActions))
	for rpc := range rpcActions {
		gotRPCs = append(gotRPCs, rpc)
	}
	sort.Strings(gotRPCs)
	if !slices.Equal(gotRPCs, wantRPCs) {
		t.Fatalf("RPC action map = %v, descriptor RPCs = %v", gotRPCs, wantRPCs)
	}
}

func companyEOFYDescriptorRPCNames(t *testing.T) []string {
	t.Helper()
	serviceDescriptors := []string{
		"tammy.v1.FinancialCloseService",
		"tammy.v1.CompanyTaxService",
		"tammy.v1.CompanyReturnSubmissionService",
	}
	names := make([]string, 0, 30)
	for _, serviceName := range serviceDescriptors {
		descriptor, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(serviceName))
		if err != nil {
			t.Fatalf("find service %s: %v", serviceName, err)
		}
		service, ok := descriptor.(protoreflect.ServiceDescriptor)
		if !ok {
			t.Fatalf("descriptor %s is %T, want service", serviceName, descriptor)
		}
		methods := service.Methods()
		for index := 0; index < methods.Len(); index++ {
			names = append(names, string(methods.Get(index).Name()))
		}
	}
	sort.Strings(names)
	return names
}

func TestSbrAuthorizationMatrix(t *testing.T) {
	want := map[Action]map[tammyv1.Role]bool{
		ActionInspectSBR: {
			tammyv1.Role_ROLE_WORKSPACE_ADMIN: true,
			tammyv1.Role_ROLE_BUSINESS_LODGER: true,
		},
		ActionImportSBRMachineCredential:  {tammyv1.Role_ROLE_WORKSPACE_ADMIN: true},
		ActionUnlockSBRMachineCredential:  {tammyv1.Role_ROLE_WORKSPACE_ADMIN: true},
		ActionReplaceSBRMachineCredential: {tammyv1.Role_ROLE_WORKSPACE_ADMIN: true},
		ActionRemoveSBRMachineCredential:  {tammyv1.Role_ROLE_WORKSPACE_ADMIN: true},
		ActionManageSBRProductID:          {tammyv1.Role_ROLE_WORKSPACE_ADMIN: true},
		ActionUseSBRMachineCredential:     {tammyv1.Role_ROLE_BUSINESS_LODGER: true},
		ActionRunSBRReadinessFixture:      {tammyv1.Role_ROLE_WORKSPACE_ADMIN: true},
	}
	for action, matrix := range want {
		for _, role := range publicRoles {
			err := Authorize([]tammyv1.Role{role}, action)
			if (err == nil) != matrix[role] {
				t.Fatalf("action %q role %s allowed=%v, want %v", action, role, err == nil, matrix[role])
			}
		}
	}
}

func TestSbrReadinessFixtureAuthorizationIsAdministrativeNotLodgement(t *testing.T) {
	if err := Authorize([]tammyv1.Role{tammyv1.Role_ROLE_WORKSPACE_ADMIN}, ActionRunSBRReadinessFixture); err != nil {
		t.Fatalf("workspace administrator readiness fixture: %v", err)
	}
	if err := Authorize([]tammyv1.Role{tammyv1.Role_ROLE_BUSINESS_LODGER}, ActionRunSBRReadinessFixture); !errors.Is(err, faults.New(faults.CodePermissionDenied, nil)) {
		t.Fatalf("business lodger readiness fixture = %v; want permission denied", err)
	}
	if err := Authorize([]tammyv1.Role{tammyv1.Role_ROLE_WORKSPACE_ADMIN}, ActionUseSBRMachineCredential); !errors.Is(err, faults.New(faults.CodePermissionDenied, nil)) {
		t.Fatalf("workspace administrator production credential use = %v; want permission denied", err)
	}
}
