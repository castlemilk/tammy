package authorisation

import (
	"errors"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/faults"
	"google.golang.org/protobuf/types/known/timestamppb"
)

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
	marker := &tammyv1.FreshFactorContext{
		AssertionId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073982",
		Purpose:     "change_passphrase",
		AssertedAt:  timestamppb.New(now.Add(-5*time.Minute + time.Nanosecond)),
	}
	if err := ValidateFreshFactor(marker, "change_passphrase", now); err != nil {
		t.Fatal(err)
	}
	if err := ValidateFreshFactor(nil, "change_passphrase", now); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("missing marker returned %v", err)
	}
	if err := ValidateFreshFactor(marker, "ownership_transfer", now); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("wrong-purpose marker returned %v", err)
	}
	marker.AssertedAt = timestamppb.New(now.Add(-5 * time.Minute))
	if err := ValidateFreshFactor(marker, "change_passphrase", now); !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
		t.Fatalf("stale marker returned %v", err)
	}
}

func TestSbrAuthorizationMatrix(t *testing.T) {
	roles := []tammyv1.Role{
		tammyv1.Role_ROLE_WORKSPACE_ADMIN,
		tammyv1.Role_ROLE_BUSINESS_PREPARER,
		tammyv1.Role_ROLE_BUSINESS_LODGER,
		tammyv1.Role_ROLE_AUDITOR,
	}
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
	}
	for action, matrix := range want {
		for _, role := range roles {
			err := Authorize([]tammyv1.Role{role}, action)
			if (err == nil) != matrix[role] {
				t.Fatalf("action %q role %s allowed=%v, want %v", action, role, err == nil, matrix[role])
			}
		}
	}
}
