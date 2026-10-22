package app

import (
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/authorisation"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestBASApprovalRegistryUsesClosedOperationAndEventMap(t *testing.T) {
	static := []struct {
		operation OrdinaryOperation
		request   string
		result    string
		action    authorisation.Action
	}{
		{OrdinaryOperationFreezeBasForManualHandoff, "tammy.v1.FreezeBasForManualHandoffRequest", "tammy.v1.FreezeBasForManualHandoffResponse", authorisation.ActionApproveBASFreeze},
		{OrdinaryOperationCreateBasReplacement, "tammy.v1.CreateBasReplacementRequest", "tammy.v1.CreateBasReplacementResponse", authorisation.ActionApproveBASUnusedReplacement},
		{OrdinaryOperationResolveExternalBasDispute, "tammy.v1.ResolveExternalBasDisputeRequest", "tammy.v1.ResolveExternalBasDisputeResponse", authorisation.ActionApproveBASDisputeResolution},
		{OrdinaryOperationReplaceRejectedBasAttempt, "tammy.v1.ReplaceRejectedBasAttemptRequest", "tammy.v1.ReplaceRejectedBasAttemptResponse", authorisation.ActionApproveBASRejectedReplacement},
	}
	for _, test := range static {
		t.Run(string(test.operation), func(t *testing.T) {
			definition, ok := ordinaryOperationDefinitionFor(test.operation)
			if !ok || definition.requestType != test.request || definition.resultType != test.result || definition.action != test.action || definition.actionResolver != nil {
				t.Fatalf("ordinaryOperationDefinitionFor(%q) = %#v, %v", test.operation, definition, ok)
			}
		})
	}

	for _, test := range []struct {
		kind   tammyv1.BasExternalEventKind
		action authorisation.Action
	}{
		{tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT, authorisation.ActionApproveBASExternalConfirmation},
		{tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE, authorisation.ActionApproveBASExternalConfirmation},
		{tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING, authorisation.ActionPrepareTax},
		{tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_UNKNOWN, authorisation.ActionPrepareTax},
		{tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION, authorisation.ActionPrepareTax},
		{tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REVIEWER_HANDOFF, authorisation.ActionPrepareTax},
		{tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PAYMENT, authorisation.ActionPrepareTax},
		{tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REFUND, authorisation.ActionPrepareTax},
	} {
		request := &tammyv1.RecordExternalBasObservationRequest{
			Observation: &tammyv1.BasExternalObservationInput{EventKind: test.kind},
		}
		action, ok := ordinaryOperationActionFor(OrdinaryOperationRecordExternalBasObservation, request)
		if !ok || action != test.action {
			t.Fatalf("event kind %s action = %q, %v; want %q", test.kind, action, ok, test.action)
		}
	}
	for _, test := range []struct {
		name    string
		request any
	}{
		{"missing observation", &tammyv1.RecordExternalBasObservationRequest{}},
		{"unspecified event", &tammyv1.RecordExternalBasObservationRequest{Observation: &tammyv1.BasExternalObservationInput{}}},
		{"future event", &tammyv1.RecordExternalBasObservationRequest{Observation: &tammyv1.BasExternalObservationInput{EventKind: tammyv1.BasExternalEventKind(99)}}},
		{"wrong request type", &tammyv1.FreezeBasForManualHandoffRequest{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			message, ok := test.request.(interface{ ProtoReflect() protoreflect.Message })
			if !ok {
				t.Fatal("test request is not a protobuf message")
			}
			if action, known := ordinaryOperationActionFor(OrdinaryOperationRecordExternalBasObservation, message); known || action != "" {
				t.Fatalf("unknown event action = %q, %v", action, known)
			}
		})
	}
	if _, known := ordinaryOperationDefinitionFor(OrdinaryOperation("tammy.v1.BasPreparationService.UnregisteredApproval")); known {
		t.Fatal("unregistered operation was accepted")
	}
}
