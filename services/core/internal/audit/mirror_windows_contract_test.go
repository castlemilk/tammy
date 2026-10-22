package audit

import (
	"strings"
	"testing"
)

func TestWindowsMirrorMutexContractIsGlobalUserRestrictedAndOpaque(t *testing.T) {
	userSID := "S-1-5-21-111111111-222222222-333333333-1001"
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	contract, err := newWindowsMirrorMutexContract(userSID, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	wantSDDL := "O:" + userSID + "D:P(A;;0x00100001;;;SY)(A;;0x00100001;;;" + userSID + ")"
	if !strings.HasPrefix(contract.name, `Global\TammyAuditMirror-`) ||
		strings.Contains(contract.name, userSID) || strings.Contains(contract.name, workspaceID) ||
		contract.access != 0x00100001 || contract.sddl != wantSDDL {
		t.Fatalf("Windows mutex contract=%#v want global opaque name, access 0x00100001, and protected user/SYSTEM DACL %q",
			contract, wantSDDL)
	}

	other, err := newWindowsMirrorMutexContract("S-1-5-21-111111111-222222222-333333333-1002", workspaceID)
	if err != nil || other.name == contract.name {
		t.Fatalf("cross-user mutex other=%#v err=%v", other, err)
	}
	otherWorkspace, err := newWindowsMirrorMutexContract(userSID, "01890f60-4d6d-7c12-8f02-6c9129d5b002")
	if err != nil || otherWorkspace.name == contract.name {
		t.Fatalf("cross-workspace mutex other=%#v err=%v", otherWorkspace, err)
	}
	if _, err := newWindowsMirrorMutexContract(userSID, ""); err == nil {
		t.Fatal("empty workspace label unexpectedly produced a mutex contract")
	}
	if _, err := newWindowsMirrorMutexContract(userSID+";D:(A;;GA;;;WD)", workspaceID); err == nil {
		t.Fatal("injectable SID text unexpectedly produced a mutex contract")
	}
}
