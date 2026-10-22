//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

func TestBASPreparationAuditActionsMatchRegisteredOperations(t *testing.T) {
	cases := []struct {
		operation app.OrdinaryOperation
		action    tammyv1.BasPreparationAuditAction
	}{
		{app.OrdinaryOperationSetBasObligationsProfile,
			tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_PROFILE_SET},
		{app.OrdinaryOperationConfirmBasPeriod,
			tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_PERIOD_CONFIRMED},
		{app.OrdinaryOperationSaveBasPaygInputs,
			tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_PAYG_INPUTS_SAVED},
		{app.OrdinaryOperationRegenerateBasPreparation,
			tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_PREPARATION_REGENERATED},
		{app.OrdinaryOperationValidateBasPreparation, tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_VALIDATION_RECORDED},
		{app.OrdinaryOperationAcknowledgeBasWarnings, tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_WARNINGS_ACKNOWLEDGED},
		{app.OrdinaryOperationFreezeBasForManualHandoff, tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_FROZEN_FOR_MANUAL_HANDOFF},
		{app.OrdinaryOperationRecordExternalBasObservation, tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_EXTERNAL_OBSERVATION_RECORDED},
		{app.OrdinaryOperationCorrectExternalBasObservation, tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_EXTERNAL_OBSERVATION_CORRECTED},
		{app.OrdinaryOperationResolveExternalBasDispute, tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_EXTERNAL_DISPUTE_RESOLVED},
		{app.OrdinaryOperationRecordBasExportEvent, tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_EXPORT_EVENT_RECORDED},
		{app.OrdinaryOperationCreateBasReplacement, tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_EXTERNAL_REPLACEMENT_CREATED},
		{app.OrdinaryOperationCreateBasRevision, tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_REVISION_CREATED},
		{app.OrdinaryOperationReplaceRejectedBasAttempt, tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_REJECTED_ATTEMPT_REPLACED},
	}
	if len(tammyv1.BasPreparationAuditAction_name) != len(cases)+1 {
		t.Fatalf("defined BAS audit actions = %d, registered operation mappings = %d",
			len(tammyv1.BasPreparationAuditAction_name)-1, len(cases))
	}
	for _, test := range cases {
		t.Run(string(test.operation), func(t *testing.T) {
			action, ok := basPreparationActionForOperation(test.operation)
			if !ok || action != test.action || action == tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_UNSPECIFIED {
				t.Fatalf("operation %q action = %v, %t; want %v", test.operation, action, ok, test.action)
			}
		})
	}
	if action, ok := basPreparationActionForOperation(app.OrdinaryOperation("tammy.v1.BasPreparationService.UnknownOperation")); ok || action != tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_UNSPECIFIED {
		t.Fatalf("unregistered operation mapped to %v, %t", action, ok)
	}
}
