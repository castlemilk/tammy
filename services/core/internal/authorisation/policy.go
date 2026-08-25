// Package authorisation is the centralized deny-by-default role policy.
package authorisation

import (
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/faults"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

type Action string

const (
	ActionManageWorkspace             Action = "manage_workspace"
	ActionManageUsers                 Action = "manage_users"
	ActionTransferOwner               Action = "transfer_owner"
	ActionManageOrg                   Action = "manage_organisation"
	ActionReadFinancial               Action = "read_financial"
	ActionPostAccounting              Action = "post_accounting"
	ActionManageAccounts              Action = "manage_accounts"
	ActionPrepareTax                  Action = "prepare_tax"
	ActionApproveFinancialClose       Action = "approve_financial_close"
	ActionDeclareCompanyReturn        Action = "declare_company_return"
	ActionLodge                       Action = "lodge"
	ActionReadAudit                   Action = "read_audit"
	ActionExportAudit                 Action = "export_audit"
	ActionInspectSBR                  Action = "inspect_sbr"
	ActionImportSBRMachineCredential  Action = "import_sbr_machine_credential"
	ActionUnlockSBRMachineCredential  Action = "unlock_sbr_machine_credential"
	ActionReplaceSBRMachineCredential Action = "replace_sbr_machine_credential"
	ActionRemoveSBRMachineCredential  Action = "remove_sbr_machine_credential"
	ActionManageSBRProductID          Action = "manage_sbr_product_id"
	ActionUseSBRMachineCredential     Action = "use_sbr_machine_credential"
	ActionRunSBRReadinessFixture      Action = "run_sbr_readiness_fixture"
)

var permissions = map[Action]map[tammyv1.Role]struct{}{
	ActionManageWorkspace: roleSet(tammyv1.Role_ROLE_WORKSPACE_ADMIN),
	ActionManageUsers:     roleSet(tammyv1.Role_ROLE_WORKSPACE_ADMIN),
	ActionTransferOwner:   roleSet(tammyv1.Role_ROLE_WORKSPACE_ADMIN),
	ActionManageOrg:       roleSet(tammyv1.Role_ROLE_WORKSPACE_ADMIN),
	ActionReadFinancial: roleSet(
		tammyv1.Role_ROLE_WORKSPACE_ADMIN,
		tammyv1.Role_ROLE_BUSINESS_PREPARER,
		tammyv1.Role_ROLE_BUSINESS_LODGER,
		tammyv1.Role_ROLE_AUDITOR,
	),
	ActionPostAccounting:        roleSet(tammyv1.Role_ROLE_WORKSPACE_ADMIN, tammyv1.Role_ROLE_BUSINESS_PREPARER),
	ActionManageAccounts:        roleSet(tammyv1.Role_ROLE_WORKSPACE_ADMIN),
	ActionPrepareTax:            roleSet(tammyv1.Role_ROLE_WORKSPACE_ADMIN, tammyv1.Role_ROLE_BUSINESS_PREPARER),
	ActionApproveFinancialClose: roleSet(tammyv1.Role_ROLE_BUSINESS_LODGER),
	ActionDeclareCompanyReturn:  roleSet(tammyv1.Role_ROLE_BUSINESS_LODGER),
	ActionLodge:                 roleSet(tammyv1.Role_ROLE_BUSINESS_LODGER),
	ActionReadAudit:             roleSet(tammyv1.Role_ROLE_WORKSPACE_ADMIN, tammyv1.Role_ROLE_AUDITOR),
	ActionExportAudit:           roleSet(tammyv1.Role_ROLE_WORKSPACE_ADMIN, tammyv1.Role_ROLE_AUDITOR),
	ActionInspectSBR: roleSet(
		tammyv1.Role_ROLE_WORKSPACE_ADMIN,
		tammyv1.Role_ROLE_BUSINESS_LODGER,
	),
	ActionImportSBRMachineCredential:  roleSet(tammyv1.Role_ROLE_WORKSPACE_ADMIN),
	ActionUnlockSBRMachineCredential:  roleSet(tammyv1.Role_ROLE_WORKSPACE_ADMIN),
	ActionReplaceSBRMachineCredential: roleSet(tammyv1.Role_ROLE_WORKSPACE_ADMIN),
	ActionRemoveSBRMachineCredential:  roleSet(tammyv1.Role_ROLE_WORKSPACE_ADMIN),
	ActionManageSBRProductID:          roleSet(tammyv1.Role_ROLE_WORKSPACE_ADMIN),
	ActionUseSBRMachineCredential:     roleSet(tammyv1.Role_ROLE_BUSINESS_LODGER),
	ActionRunSBRReadinessFixture:      roleSet(tammyv1.Role_ROLE_WORKSPACE_ADMIN),
}

func roleSet(roles ...tammyv1.Role) map[tammyv1.Role]struct{} {
	result := make(map[tammyv1.Role]struct{}, len(roles))
	for _, role := range roles {
		result[role] = struct{}{}
	}
	return result
}

func Authorize(roles []tammyv1.Role, action Action) error {
	accepted, known := permissions[action]
	if known {
		for _, role := range roles {
			if _, ok := accepted[role]; ok {
				return nil
			}
		}
	}
	return faults.New(faults.CodePermissionDenied, nil)
}

// ValidateFreshFactor checks purpose and the strict five-minute boundary. The
// command owner must additionally atomically consume the assertion ID.
func ValidateFreshFactor(marker *tammyv1.FreshFactorContext, purpose string, now time.Time) error {
	if marker == nil || marker.Purpose != purpose || !ids.IsCanonicalV7(marker.AssertionId) ||
		marker.AssertedAt == nil || !marker.AssertedAt.IsValid() {
		return faults.New(faults.CodeAuthenticationRequired, nil)
	}
	assertedAt := marker.AssertedAt.AsTime().UTC()
	now = now.UTC()
	if assertedAt.After(now) || !assertedAt.After(now.Add(-5*time.Minute)) {
		return faults.New(faults.CodeAuthenticationRequired, nil)
	}
	return nil
}
