package authorisation

import (
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestBASApprovalActionsAreExclusiveToBusinessLodger(t *testing.T) {
	actions := []Action{
		ActionCorrectBASExternalObservation,
		ActionApproveBASFreeze,
		ActionApproveBASExternalConfirmation,
		ActionApproveBASUnusedReplacement,
		ActionApproveBASDisputeResolution,
		ActionApproveBASRejectedReplacement,
	}
	for _, action := range actions {
		for _, role := range []tammyv1.Role{
			tammyv1.Role_ROLE_WORKSPACE_ADMIN,
			tammyv1.Role_ROLE_BUSINESS_PREPARER,
			tammyv1.Role_ROLE_BUSINESS_LODGER,
			tammyv1.Role_ROLE_AUDITOR,
		} {
			gotAllowed := Authorize([]tammyv1.Role{role}, action) == nil
			wantAllowed := role == tammyv1.Role_ROLE_BUSINESS_LODGER
			if gotAllowed != wantAllowed {
				t.Fatalf("action %q role %s allowed=%v, want %v", action, role, gotAllowed, wantAllowed)
			}
		}
	}
	for _, purpose := range []string{
		PurposeBASFreezeManualHandoff,
		PurposeBASRecordExternalConfirmation,
		PurposeBASReplaceUnusedAttempt,
		PurposeBASResolveExternalDispute,
		PurposeBASReplaceRejectedAttempt,
	} {
		if purpose == "" {
			t.Fatal("BAS approval purpose is empty")
		}
	}
}

func TestBASApprovalPurposesUseStrictFiveMinuteFreshness(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	purposes := []string{
		PurposeBASFreezeManualHandoff,
		PurposeBASRecordExternalConfirmation,
		PurposeBASReplaceUnusedAttempt,
		PurposeBASResolveExternalDispute,
		PurposeBASReplaceRejectedAttempt,
	}
	for _, purpose := range purposes {
		t.Run(purpose, func(t *testing.T) {
			marker := &tammyv1.FreshFactorContext{AssertionId: "01890f3c-7b2e-7cc4-98c4-dc0c0c073982",
				Purpose: purpose, AssertedAt: timestamppb.New(now.Add(-5*time.Minute + time.Nanosecond))}
			if err := ValidateFreshFactor(marker, purpose, now); err != nil {
				t.Fatalf("valid assertion rejected: %v", err)
			}
			if err := ValidateFreshFactor(marker, "different_purpose", now); err == nil {
				t.Fatal("assertion was accepted for a different purpose")
			}
			marker.AssertedAt = timestamppb.New(now.Add(-5 * time.Minute))
			if err := ValidateFreshFactor(marker, purpose, now); err == nil {
				t.Fatal("assertion at the five-minute boundary was accepted")
			}
		})
	}
}
