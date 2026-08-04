package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"strings"

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
	finalSequence    uint64
	finalHasActor    bool
	finalCommandType string
}

func (evidence *initialAdministratorEvidence) accept(event *tammyv1.AuditEvent, workspaceID string) {
	if evidence == nil || event == nil {
		return
	}
	transition := event.GetPayload().GetWorkspaceStateChanged()
	if transition != nil && transition.WorkspaceId == workspaceID {
		switch transition.ReasonCode {
		case "RECOVERY_CONFIRMATION":
			if transition.FromState == tammyv1.WorkspaceState_WORKSPACE_STATE_PENDING_RECOVERY &&
				transition.ToState == tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED {
				evidence.recoverySequence = event.Sequence
			}
		case "SESSION_STARTED":
			if evidence.recoverySequence != 0 && event.Sequence > evidence.recoverySequence &&
				transition.FromState == tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED &&
				transition.ToState == tammyv1.WorkspaceState_WORKSPACE_STATE_AUTHENTICATED {
				evidence.sessionSequence = event.Sequence
			}
		}
	}
	evidence.finalSequence = event.Sequence
	evidence.finalHasActor = event.Actor != nil
	evidence.finalCommandType = event.CommandType
}

func (evidence initialAdministratorEvidence) complete() bool {
	commandType := strings.ToLower(evidence.finalCommandType)
	return evidence.sessionSequence != 0 && evidence.finalSequence > evidence.sessionSequence && evidence.finalHasActor &&
		strings.Contains(commandType, "identity") && (strings.Contains(commandType, "sign_in") || strings.Contains(commandType, "signin"))
}
