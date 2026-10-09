//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"bytes"
	"context"
	"crypto/sha256"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/app"
	"github.com/tammyapp/tammy/services/core/internal/audit"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/canonical"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type ordinaryPurchaseAuditAppender struct {
	appender    *audit.Appender
	gate        *audit.WriteGate
	workspaceID string
	fingerprint []byte
	newID       func() (string, error)
	reconcile   func(context.Context, workspace.MutationExecutor) error
}

func newOrdinaryPurchaseAuditAppender(activation app.LocalWorkspaceActivation) (*ordinaryPurchaseAuditAppender, error) {
	if activation.AuditAppender == nil || activation.AuditGate == nil || len(activation.AuditSchemaFingerprint) != sha256.Size || activation.NewID == nil || !ids.IsCanonicalV7(activation.WorkspaceID) {
		return nil, ErrLedgerModule
	}
	return &ordinaryPurchaseAuditAppender{appender: activation.AuditAppender, gate: activation.AuditGate, workspaceID: activation.WorkspaceID, fingerprint: append([]byte(nil), activation.AuditSchemaFingerprint...), newID: activation.NewID, reconcile: activation.ReconcileAuditWithin}, nil
}

func (port *ordinaryPurchaseAuditAppender) RequireCoverageWithin(ctx context.Context, executor app.CommandSQLExecutor) error {
	if port == nil || executor == nil {
		return app.ErrLocalAuditCoverageUnavailable
	}
	if port.gate != nil && !port.gate.Writable() && port.reconcile != nil {
		if err := port.reconcile(ctx, executor); err != nil {
			return err
		}
	}
	return port.RequireCoverage()
}

func (port *ordinaryPurchaseAuditAppender) RequireCoverage() error {
	if port == nil || port.appender == nil || port.gate == nil || !port.gate.Writable() {
		return app.ErrLocalAuditCoverageUnavailable
	}
	return nil
}

// Append builds the ordinary event from the complete immutable accepted
// accounting result within the caller's transaction. The incoming coordinator
// envelope commits actor/key/type/result; no renderer supplies audit facts.
func (port *ordinaryPurchaseAuditAppender) Append(ctx context.Context, executor app.CommandSQLExecutor, envelope *tammyv1.AuditEvent) error {
	if err := port.RequireCoverage(); err != nil {
		return err
	}
	if executor == nil || envelope == nil || envelope.Actor == nil || envelope.IdempotencyKey == nil || envelope.Result == nil || envelope.WorkspaceId != port.workspaceID || len(envelope.Result.DeterministicSha256) != sha256.Size {
		return ErrLedgerModule
	}
	result := purchaseAuditResult(envelope.CommandType)
	if result == nil || envelope.Result.TypeName != string(result.ProtoReflect().Descriptor().FullName()) {
		return ErrLedgerModule
	}
	rows, err := executor.QueryContext(ctx, `SELECT p.organisation_id,p.accounting_version,p.financial_revision,p.result_proto,p.result_sha256,p.created_at FROM purchase_accounting_events_v1 p JOIN command_idempotency_v1 c ON c.operation_key=p.operation_key WHERE p.operation_key=? AND p.command_type=? AND p.result_sha256=? AND c.workspace_id=? AND c.actor_user_id=? AND c.fully_qualified_rpc_name=p.command_type AND c.outcome='ELECTED' AND length(p.result_proto) BETWEEN 1 AND 1048576`, *envelope.IdempotencyKey, envelope.CommandType, envelope.Result.DeterministicSha256, port.workspaceID, envelope.Actor.ActorUserId)
	if err != nil {
		return err
	}
	var organisationID, createdAt string
	var accountingVersion, financialRevision uint64
	var resultBytes, resultHash []byte
	if !rows.Next() || rows.Scan(&organisationID, &accountingVersion, &financialRevision, &resultBytes, &resultHash, &createdAt) != nil || rows.Next() || rows.Err() != nil {
		_ = rows.Close()
		return ErrLedgerModule
	}
	if err := rows.Close(); err != nil {
		return err
	}
	digest := sha256.Sum256(resultBytes)
	if !bytes.Equal(digest[:], resultHash) || !bytes.Equal(resultHash, envelope.Result.DeterministicSha256) || accountingVersion == 0 || financialRevision == 0 || !ids.IsCanonicalV7(organisationID) {
		return ErrLedgerModule
	}
	if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(resultBytes, result); err != nil {
		return ErrLedgerModule
	}
	if _, err := canonical.NormalizedJSON(result); err != nil {
		return ErrLedgerModule
	}
	canonicalBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(result)
	if err != nil || !bytes.Equal(canonicalBytes, resultBytes) {
		return ErrLedgerModule
	}
	facts, err := purchaseAuditFactsFromResult(result)
	if err != nil || facts.organisationID != organisationID || facts.accountingVersion != accountingVersion || !validPurchaseAuditRef(facts.primary) {
		return ErrLedgerModule
	}
	when, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil || when.IsZero() {
		return ErrLedgerModule
	}
	eventID, err := port.newID()
	if err != nil {
		return err
	}
	commandID := *envelope.IdempotencyKey
	event := proto.Clone(envelope).(*tammyv1.AuditEvent)
	event.Id = eventID
	event.OrganisationId = &organisationID
	event.CommandId = &commandID
	event.Type = tammyv1.AuditEventType_AUDIT_EVENT_TYPE_PURCHASE_ACCOUNTING_CHANGED
	event.OccurredAt = timestamppb.New(when.UTC())
	event.Source = facts.primary
	event.PayloadSchemaFingerprint = append([]byte(nil), port.fingerprint...)
	event.Payload = &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_PurchaseAccountingChanged{PurchaseAccountingChanged: &tammyv1.PurchaseAccountingChangedEvent{Action: facts.action, AccountingVersion: accountingVersion, FinancialRevision: financialRevision}}}
	event.AfterSemanticHash = append([]byte(nil), facts.primary.ContentHash...)
	// One complete response commitment covers every attribution/portion without
	// truncating a 200-row command into the envelope's 64-resource collection.
	event.AffectedResources = []*tammyv1.SourceRef{{Type: "purchase_accounting_event", Id: commandID, Revision: accountingVersion, ContentHash: append([]byte(nil), resultHash...)}}
	for _, source := range facts.affected {
		if !validPurchaseAuditRef(source) {
			return ErrLedgerModule
		}
		event.AffectedResources = append(event.AffectedResources, source)
	}
	_, err = port.appender.Append(ctx, executor, event, nil)
	return err
}

type purchaseAuditFacts struct {
	organisationID    string
	accountingVersion uint64
	action            tammyv1.PurchaseAuditAction
	primary           *tammyv1.SourceRef
	affected          []*tammyv1.SourceRef
}

func purchaseAuditResult(operation string) proto.Message {
	switch app.OrdinaryOperation(operation) {
	case app.OrdinaryOperationPostReviewedPurchase:
		return &tammyv1.PostReviewedPurchaseResponse{}
	case app.OrdinaryOperationRecordPurchasePayment:
		return &tammyv1.RecordPurchasePaymentResponse{}
	case app.OrdinaryOperationRecordPurchaseTaxEvidence:
		return &tammyv1.RecordPurchaseTaxEvidenceResponse{}
	case app.OrdinaryOperationAttributePurchaseGst:
		return &tammyv1.AttributePurchaseGstResponse{}
	case app.OrdinaryOperationDeferPurchaseGstClaim:
		return &tammyv1.DeferPurchaseGstClaimResponse{}
	case app.OrdinaryOperationResolveLegacyPurchaseReview:
		return &tammyv1.ResolveLegacyPurchaseReviewResponse{}
	default:
		return nil
	}
}

func purchaseAuditSource(kind, id string, version uint64, hash []byte) *tammyv1.SourceRef {
	return &tammyv1.SourceRef{Type: kind, Id: id, Revision: version, ContentHash: append([]byte(nil), hash...)}
}
func validPurchaseAuditRef(source *tammyv1.SourceRef) bool {
	return source != nil && ids.IsCanonicalV7(source.Id) && source.Revision > 0 && len(source.ContentHash) == sha256.Size
}

func purchaseAuditFactsFromResult(result proto.Message) (purchaseAuditFacts, error) {
	var facts purchaseAuditFacts
	var accounting *tammyv1.PurchaseAccounting
	var journal *tammyv1.Journal
	switch value := result.(type) {
	case *tammyv1.PostReviewedPurchaseResponse:
		if value.Posting == nil {
			return facts, ErrLedgerModule
		}
		facts.primary = purchaseAuditSource("purchase_posting", value.Posting.Id, 1, value.Posting.SourceHash)
		facts.action = tammyv1.PurchaseAuditAction_PURCHASE_AUDIT_ACTION_POSTED
		accounting, journal = value.Accounting, value.Journal
	case *tammyv1.RecordPurchasePaymentResponse:
		if value.Payment == nil {
			return facts, ErrLedgerModule
		}
		facts.primary = purchaseAuditSource("purchase_payment", value.Payment.Id, value.Payment.AccountingVersion, value.Payment.SourceHash)
		facts.action = tammyv1.PurchaseAuditAction_PURCHASE_AUDIT_ACTION_PAYMENT_RECORDED
		accounting, journal = value.Accounting, value.Journal
	case *tammyv1.RecordPurchaseTaxEvidenceResponse:
		if value.Evidence == nil {
			return facts, ErrLedgerModule
		}
		facts.primary = purchaseAuditSource("purchase_tax_evidence", value.Evidence.Id, value.Evidence.AccountingVersion, value.Evidence.SourceHash)
		facts.action = tammyv1.PurchaseAuditAction_PURCHASE_AUDIT_ACTION_TAX_EVIDENCE_RECORDED
		accounting = value.Accounting
	case *tammyv1.AttributePurchaseGstResponse:
		if len(value.Attributions) == 0 || value.Attributions[0] == nil {
			return facts, ErrLedgerModule
		}
		first := value.Attributions[0]
		facts.primary = purchaseAuditSource("purchase_gst_attribution", first.Id, first.AccountingVersion, first.SourceHash)
		facts.action = tammyv1.PurchaseAuditAction_PURCHASE_AUDIT_ACTION_GST_ATTRIBUTED
		accounting, journal = value.Accounting, value.Journal
	case *tammyv1.DeferPurchaseGstClaimResponse:
		if value.Deferral == nil {
			return facts, ErrLedgerModule
		}
		facts.primary = purchaseAuditSource("purchase_gst_deferral", value.Deferral.Id, value.Deferral.AccountingVersion, value.Deferral.SourceHash)
		facts.action = tammyv1.PurchaseAuditAction_PURCHASE_AUDIT_ACTION_GST_DEFERRED
		accounting = value.Accounting
	case *tammyv1.ResolveLegacyPurchaseReviewResponse:
		if value.Review == nil || value.Review.Review == nil || value.Review.Review.Source == nil {
			return facts, ErrLedgerModule
		}
		facts.primary = purchaseAuditSource("legacy_purchase_review", value.Review.Id, value.Review.SourceRevision, value.Review.SourceHash)
		facts.action = tammyv1.PurchaseAuditAction_PURCHASE_AUDIT_ACTION_LEGACY_REVIEW_RESOLVED
		facts.organisationID, facts.accountingVersion = value.Review.OrganisationId, value.Review.SourceRevision
		if document := value.Review.Review.Source.GetDocument(); document != nil {
			facts.affected = append(facts.affected, purchaseAuditSource("document", document.DocumentId, document.ReviewedVersion, document.ReviewedSnapshotHash))
		} else if opening := value.Review.Review.Source.GetOpeningPayable(); opening != nil {
			facts.affected = append(facts.affected, purchaseAuditSource("purchase_opening_payable", opening.OpeningPayableId, opening.SourceVersion, opening.SourceHash))
		} else {
			return facts, ErrLedgerModule
		}
	default:
		return facts, ErrLedgerModule
	}
	if accounting != nil {
		if !accounting.Posted || accounting.Posting == nil {
			return facts, ErrLedgerModule
		}
		posting := accounting.Posting
		facts.organisationID, facts.accountingVersion = accounting.OrganisationId, accounting.AccountingVersion
		facts.affected = append(facts.affected, purchaseAuditSource("purchase_posting", posting.Id, 1, posting.SourceHash), purchaseAuditSource("document", posting.DocumentId, posting.ReviewedVersion, posting.ReviewedSnapshotHash))
	}
	if journal != nil {
		encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(journal)
		if err != nil || journal.OrganisationId != facts.organisationID {
			return facts, ErrLedgerModule
		}
		digest := sha256.Sum256(encoded)
		facts.affected = append(facts.affected, purchaseAuditSource("journal", journal.Id, journal.Version, digest[:]))
	}
	return facts, nil
}
