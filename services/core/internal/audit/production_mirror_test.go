package audit

import (
	"bytes"
	"crypto/sha256"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestInitialAdministratorEvidenceRequiresCorrelatedSessionAndSecuritySuffix(t *testing.T) {
	const workspaceID = "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	const userID = "01890f60-4d6d-7c12-8f02-6c9129d5b006"
	const sessionID = "01890f60-4d6d-7c12-8f02-6c9129d5b007"
	newEvidence := func() initialAdministratorEvidence {
		evidence := initialAdministratorEvidence{}
		evidence.accept(testWorkspaceEvidenceEvent(1, workspaceID, "RECOVERY_CONFIRMATION",
			tammyv1.WorkspaceState_WORKSPACE_STATE_PENDING_RECOVERY,
			tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED,
			"01890f60-4d6d-7c12-8f02-6c9129d5b010"), workspaceID)
		evidence.accept(testWorkspaceEvidenceEvent(2, workspaceID, "SESSION_STARTED",
			tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED,
			tammyv1.WorkspaceState_WORKSPACE_STATE_AUTHENTICATED, sessionID), workspaceID)
		return evidence
	}

	t.Run("actual session linked sign-in survives only LOCK and UNLOCK", func(t *testing.T) {
		evidence := newEvidence()
		evidence.accept(testAdministratorSignInEvent(3, workspaceID, userID, sessionID), workspaceID)
		evidence.accept(testWorkspaceEvidenceEvent(4, workspaceID, "LOCK",
			tammyv1.WorkspaceState_WORKSPACE_STATE_AUTHENTICATED,
			tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED,
			"01890f60-4d6d-7c12-8f02-6c9129d5b011"), workspaceID)
		evidence.accept(testWorkspaceEvidenceEvent(5, workspaceID, "UNLOCK",
			tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED,
			tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED,
			"01890f60-4d6d-7c12-8f02-6c9129d5b012"), workspaceID)
		if !evidence.complete() {
			t.Fatal("verified initial session evidence was lost after a valid security-state suffix")
		}
	})

	t.Run("unrelated terminal actor event cannot establish the first session", func(t *testing.T) {
		evidence := newEvidence()
		unrelated := testAdministratorSignInEvent(3, workspaceID, userID, "01890f60-4d6d-7c12-8f02-6c9129d5b099")
		evidence.accept(unrelated, workspaceID)
		if evidence.complete() {
			t.Fatal("unrelated actor event was accepted without matching the actual workspace session")
		}
	})

	t.Run("identity events after the initial sign-in are not an allowed suffix", func(t *testing.T) {
		evidence := newEvidence()
		evidence.accept(testAdministratorSignInEvent(3, workspaceID, userID, sessionID), workspaceID)
		evidence.accept(testAdministratorSignInEvent(4, workspaceID, userID, sessionID), workspaceID)
		if evidence.complete() {
			t.Fatal("arbitrary trailing identity event was accepted as a security-state suffix")
		}
	})
}

func testWorkspaceEvidenceEvent(sequence uint64, workspaceID, reason string, from, to tammyv1.WorkspaceState,
	commandID string) *tammyv1.AuditEvent {
	digest := sha256.Sum256([]byte(commandID))
	return &tammyv1.AuditEvent{
		Id: "01890f60-4d6d-7c12-8f02-6c9129d5b020", WorkspaceId: workspaceID, Sequence: sequence,
		Type:       tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_STATE_CHANGED,
		OccurredAt: timestamppb.New(time.Date(2026, 8, 4, 7, 30, 0, 0, time.UTC)),
		Source:     &tammyv1.SourceRef{Type: "workspace", Id: workspaceID, Revision: 1, ContentHash: digest[:]},
		Payload: &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_WorkspaceStateChanged{WorkspaceStateChanged: &tammyv1.WorkspaceStateChangedEvent{
			WorkspaceId: workspaceID, FromState: from, ToState: to, ReasonCode: reason,
		}}},
		PayloadSchemaFingerprint: bytes.Repeat([]byte{0x31}, sha256.Size), CommandId: &commandID,
		CommandType: "tammy.workspace." + reason,
		Result:      &tammyv1.AuditResultMetadata{TypeName: "tammy.v1.Workspace", DeterministicSha256: bytes.Repeat([]byte{0x32}, sha256.Size), OutcomeCode: "OK"},
	}
}

func testAdministratorSignInEvent(sequence uint64, workspaceID, userID, actorSessionID string) *tammyv1.AuditEvent {
	commandID := actorSessionID
	digest := sha256.Sum256([]byte(actorSessionID))
	payload := &tammyv1.UserStateChangedEvent{UserId: userID,
		FromState: tammyv1.UserState_USER_STATE_ACTIVE, ToState: tammyv1.UserState_USER_STATE_ACTIVE, ReasonCode: "SIGN_IN"}
	return &tammyv1.AuditEvent{
		Id: "01890f60-4d6d-7c12-8f02-6c9129d5b021", WorkspaceId: workspaceID, Sequence: sequence,
		Type:                     tammyv1.AuditEventType_AUDIT_EVENT_TYPE_USER_STATE_CHANGED,
		OccurredAt:               timestamppb.New(time.Date(2026, 8, 4, 7, 30, 0, 0, time.UTC)),
		Actor:                    &tammyv1.AuthenticationContext{ActorUserId: userID, SessionId: actorSessionID},
		Source:                   &tammyv1.SourceRef{Type: "identity_session", Id: actorSessionID, Revision: 1, ContentHash: digest[:]},
		AffectedResources:        []*tammyv1.SourceRef{{Type: "identity_user", Id: userID, Revision: 1, ContentHash: digest[:]}},
		Payload:                  &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_UserStateChanged{UserStateChanged: payload}},
		PayloadSchemaFingerprint: bytes.Repeat([]byte{0x31}, sha256.Size), CommandId: &commandID,
		CommandType: "tammy.v1.IdentityService.SignIn",
		Result:      &tammyv1.AuditResultMetadata{TypeName: "tammy.v1.SignInResponse", DeterministicSha256: bytes.Repeat([]byte{0x32}, sha256.Size), OutcomeCode: "SUCCESS"},
	}
}
