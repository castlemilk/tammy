package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

// SQLMirrorVerifier verifies one fixed SQL read transaction from its
// authenticated header through bounded keyset pages.
type SQLMirrorVerifier struct {
	transactions ServiceTransactions
}

func NewSQLMirrorVerifier(transactions ServiceTransactions) (*SQLMirrorVerifier, error) {
	if nilInterface(transactions) || !ids.IsCanonicalV7(transactions.WorkspaceID()) {
		return nil, ErrMirrorInvalid
	}
	return &SQLMirrorVerifier{transactions: transactions}, nil
}

func (verifier *SQLMirrorVerifier) VerifyFull(ctx context.Context, workspaceID string, generation uint64) (VerifiedChain, error) {
	if verifier == nil || nilInterface(verifier.transactions) || ctx == nil || !ids.IsCanonicalV7(workspaceID) || generation == 0 ||
		workspaceID != verifier.transactions.WorkspaceID() {
		return VerifiedChain{}, ErrMirrorInvalid
	}
	if err := ctx.Err(); err != nil {
		return VerifiedChain{}, errors.Join(ErrMirrorInvalid, err)
	}

	var result VerifiedChain
	err := verifier.transactions.Read(ctx, func(executor ServiceTransaction) error {
		if nilInterface(executor) {
			return ErrMirrorInvalid
		}
		header, err := LoadChainHeader(ctx, executor, workspaceID, generation)
		if err != nil || header.WorkspaceID != workspaceID || header.Generation != generation ||
			header.CurrentSequence > ExternalOpeningRecordLimit/5 {
			return errors.Join(ErrMirrorInvalid, err)
		}
		chain, err := NewStreamingStoredChainVerifier(ctx, header)
		if err != nil {
			return errors.Join(ErrMirrorInvalid, err)
		}
		defer chain.Close()

		snapshot := storedEventSnapshotFromHeader(header)
		checkpoint := StoredEventCheckpoint{Head: header.GenesisHash}
		heads := make(map[uint64][]byte, int(header.CurrentSequence)+1)
		heads[0] = append([]byte(nil), header.GenesisHash[:]...)
		initial := initialAdministratorEvidence{}
		for checkpoint.AfterSequence < snapshot.EndSequence {
			page, pageErr := LoadStoredEventPage(ctx, executor, snapshot, checkpoint,
				StoredEventPageSizeLimit, StoredEventPageByteBudget)
			if pageErr != nil || len(page.Events) == 0 || page.Checkpoint.AfterSequence <= checkpoint.AfterSequence {
				return errors.Join(ErrMirrorInvalid, pageErr)
			}
			if acceptErr := chain.AcceptPage(page.Events); acceptErr != nil {
				return errors.Join(ErrMirrorInvalid, acceptErr)
			}
			for index := range page.Events {
				event := page.Events[index].Event
				if event == nil || len(event.EventHash) != sha256.Size {
					return ErrMirrorInvalid
				}
				heads[event.Sequence] = append([]byte(nil), event.EventHash...)
				initial.accept(event, workspaceID)
			}
			checkpoint = page.Checkpoint
			if !page.HasMore && checkpoint.AfterSequence != snapshot.EndSequence {
				return ErrMirrorInvalid
			}
		}
		verification := chain.Finish()
		if chain.TerminalError() != nil || verification == nil ||
			verification.Integrity != tammyv1.AuditChainIntegrity_AUDIT_CHAIN_INTEGRITY_VALID ||
			verification.VerifiedThroughSequence != header.CurrentSequence ||
			len(verification.VerifiedHead) != sha256.Size || !bytes.Equal(verification.VerifiedHead, header.CurrentHead[:]) {
			return ErrMirrorInvalid
		}
		result = VerifiedChain{
			Baseline: &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: generation,
				Sequence: header.CurrentSequence, Head: append([]byte(nil), header.CurrentHead[:]...)},
			Heads: heads, Valid: true, InitialAdministratorSessionComplete: initial.complete(),
		}
		return nil
	})
	if err != nil {
		return VerifiedChain{}, errors.Join(ErrMirrorInvalid, err)
	}
	return result.Clone(), nil
}

type initialAdministratorEvidence struct {
	recoverySequence uint64
	sessionSequence  uint64
	sessionID        string
	signInSequence   uint64
	workspaceState   tammyv1.WorkspaceState
	suffixValid      bool
}

func (evidence *initialAdministratorEvidence) accept(event *tammyv1.AuditEvent, workspaceID string) {
	if evidence == nil || event == nil {
		return
	}
	if evidence.signInSequence != 0 {
		if !evidence.acceptSecuritySuffix(event, workspaceID) {
			evidence.suffixValid = false
		}
		return
	}
	transition := event.GetPayload().GetWorkspaceStateChanged()
	if transition != nil && transition.WorkspaceId == workspaceID && event.Type == tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_STATE_CHANGED {
		if transition.ReasonCode == "RECOVERY_CONFIRMATION" &&
			validWorkspaceTransition(event, workspaceID, "RECOVERY_CONFIRMATION") &&
			transition.FromState == tammyv1.WorkspaceState_WORKSPACE_STATE_PENDING_RECOVERY &&
			transition.ToState == tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED {
			evidence.recoverySequence = event.Sequence
			return
		}
		if transition.ReasonCode == "SESSION_STARTED" && evidence.recoverySequence != 0 &&
			event.Sequence > evidence.recoverySequence &&
			validWorkspaceTransition(event, workspaceID, "SESSION_STARTED") &&
			transition.FromState == tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED &&
			transition.ToState == tammyv1.WorkspaceState_WORKSPACE_STATE_AUTHENTICATED && event.CommandId != nil && *event.CommandId != "" {
			evidence.sessionSequence = event.Sequence
			evidence.sessionID = *event.CommandId
			return
		}
	}
	if evidence.sessionSequence != 0 && event.Sequence == evidence.sessionSequence+1 &&
		validInitialAdministratorSignIn(event, workspaceID, evidence.sessionID) {
		evidence.signInSequence = event.Sequence
		evidence.workspaceState = tammyv1.WorkspaceState_WORKSPACE_STATE_AUTHENTICATED
		evidence.suffixValid = true
	}
}

func (evidence initialAdministratorEvidence) complete() bool {
	return evidence.recoverySequence != 0 && evidence.sessionSequence > evidence.recoverySequence &&
		evidence.signInSequence == evidence.sessionSequence+1 && evidence.sessionID != "" && evidence.suffixValid
}

func (evidence *initialAdministratorEvidence) acceptSecuritySuffix(event *tammyv1.AuditEvent, workspaceID string) bool {
	if event == nil || event.Type != tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_STATE_CHANGED || event.Actor != nil {
		return false
	}
	transition := event.GetPayload().GetWorkspaceStateChanged()
	if transition == nil || transition.WorkspaceId != workspaceID || event.CommandId == nil || *event.CommandId == "" {
		return false
	}
	switch transition.ReasonCode {
	case "LOCK":
		if transition.ToState != tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED ||
			(evidence.workspaceState != tammyv1.WorkspaceState_WORKSPACE_STATE_AUTHENTICATED &&
				evidence.workspaceState != tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED) ||
			transition.FromState != evidence.workspaceState ||
			!validWorkspaceTransition(event, workspaceID, "LOCK") {
			return false
		}
		evidence.workspaceState = tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED
		return true
	case "UNLOCK":
		if evidence.workspaceState != tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED ||
			transition.FromState != tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED ||
			transition.ToState != tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED ||
			!validWorkspaceTransition(event, workspaceID, "UNLOCK") {
			return false
		}
		evidence.workspaceState = tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED
		return true
	default:
		return false
	}
}

func validWorkspaceTransition(event *tammyv1.AuditEvent, workspaceID, kind string) bool {
	if event == nil {
		return false
	}
	transition := event.GetPayload().GetWorkspaceStateChanged()
	return transition != nil && transition.WorkspaceId == workspaceID && transition.ReasonCode == kind &&
		event.WorkspaceId == workspaceID && event.Type == tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_STATE_CHANGED &&
		event.Actor == nil && event.CommandId != nil && *event.CommandId != "" &&
		event.CommandType == "tammy.workspace."+kind && event.Source != nil && event.Source.Type == "workspace" &&
		event.Source.Id == workspaceID && event.Source.Revision > 0 && len(event.Source.ContentHash) == sha256.Size &&
		event.Result != nil && event.Result.TypeName == "tammy.v1.Workspace" && event.Result.OutcomeCode == "OK" &&
		len(event.Result.DeterministicSha256) == sha256.Size && len(event.PayloadSchemaFingerprint) == sha256.Size
}

func validInitialAdministratorSignIn(event *tammyv1.AuditEvent, workspaceID, sessionID string) bool {
	if event == nil || event.WorkspaceId != workspaceID || event.Type != tammyv1.AuditEventType_AUDIT_EVENT_TYPE_USER_STATE_CHANGED ||
		event.Actor == nil || event.Actor.ActorUserId == "" || event.Actor.SessionId != sessionID ||
		event.CommandId == nil || *event.CommandId != sessionID || event.CommandType != "tammy.v1.IdentityService.SignIn" ||
		event.Source == nil || event.Source.Type != "identity_session" || event.Source.Id != sessionID || event.Source.Revision == 0 ||
		len(event.Source.ContentHash) != sha256.Size || event.Result == nil || event.Result.TypeName != "tammy.v1.SignInResponse" ||
		event.Result.OutcomeCode != "SUCCESS" || len(event.Result.DeterministicSha256) != sha256.Size ||
		len(event.PayloadSchemaFingerprint) != sha256.Size {
		return false
	}
	payload := event.GetPayload().GetUserStateChanged()
	if payload == nil || payload.UserId == "" || payload.UserId != event.Actor.ActorUserId ||
		payload.FromState != tammyv1.UserState_USER_STATE_ACTIVE || payload.ToState != tammyv1.UserState_USER_STATE_ACTIVE || payload.ReasonCode != "SIGN_IN" {
		return false
	}
	for _, resource := range event.AffectedResources {
		if resource != nil && resource.Type == "identity_user" && resource.Id == payload.UserId &&
			resource.Revision > 0 && len(resource.ContentHash) == sha256.Size {
			return true
		}
	}
	return false
}
