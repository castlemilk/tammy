//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"testing"

	"connectrpc.com/connect"

	"github.com/tammyapp/tammy/services/core/internal/audit"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/testsupport"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
	"google.golang.org/protobuf/proto"
)

func TestPurchaseCommandLegacyAndMovedAuditCoverageCannotBeEstablishedByFinancialWrite(t *testing.T) {
	for _, kind := range []string{"genuine unaudited legacy", "moved audited workspace"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			anchors := workspace.NewMemoryAnchorStore()
			mirror := newPurchaseTestAuditMirror()
			var server documentPageServer
			var request *tammyv1.PostReviewedPurchaseRequest
			if kind == "genuine unaudited legacy" {
				fixture, err := testsupport.CreateUnauditedLegacyWorkspace(ctx, root)
				if err != nil {
					t.Fatal(err)
				}
				server = newDocumentPageServerAtRoot(t, tammyv1.GstBasis_GST_BASIS_NON_CASH, root, fixture.Anchors, mirror, false, "")
				request = server.postRequest(t, server.reviewedPurchase(t))
			} else {
				first := newDocumentPageServerAtRoot(t, tammyv1.GstBasis_GST_BASIS_NON_CASH, root, anchors, newPurchaseTestAuditMirror(), true, "")
				request = first.postRequest(t, first.reviewedPurchase(t))
				first.stop()
				server = newDocumentPageServerAtRoot(t, tammyv1.GstBasis_GST_BASIS_NON_CASH, root, anchors, mirror, false, first.organisationID)
				request.CommandContext.Authentication = server.authentication
			}
			if server.activation.AuditGate.Writable() {
				t.Fatal("untrusted workspace gained purchase write admission through authentication")
			}
			read, err := server.purchases.GetPurchaseAccounting(ctx, brandingRequest(&tammyv1.GetPurchaseAccountingRequest{Authentication: server.authentication, OrganisationId: server.organisationID, DocumentId: request.DocumentId}, server.capability))
			if err != nil || read.Msg.Accounting.Posted {
				t.Fatalf("untrusted workspace retained-source read failed: %v", err)
			}
			workspaceID := server.authenticationWorkspaceID(t)
			header, headErr := audit.LoadChainHeader(ctx, server.module.db, workspaceID, 0)
			before := purchaseTLSCounts(t, server)
			_, err = server.purchases.PostReviewedPurchase(ctx, brandingRequest(request, server.capability))
			var failure *connect.Error
			if connect.CodeOf(err) != connect.CodeFailedPrecondition || !errors.As(err, &failure) || len(failure.Details()) != 1 {
				t.Fatalf("untrusted purchase write was not safely blocked: %v", err)
			}
			value, err := failure.Details()[0].Value()
			detail, ok := value.(*tammyv1.ValidationErrorDetail)
			if err != nil || !ok || detail.Context == nil || detail.Context.Code != "PURCHASE_AUDIT_COVERAGE_UNAVAILABLE" {
				t.Fatalf("untrusted purchase correction is not typed: %v %v", value, err)
			}
			assertPurchaseTLSCounts(t, before, purchaseTLSCounts(t, server))
			after, afterErr := audit.LoadChainHeader(ctx, server.module.db, workspaceID, 0)
			if !errors.Is(headErr, afterErr) || (headErr == nil && (header.CurrentSequence != after.CurrentSequence || header.CurrentHead != after.CurrentHead)) {
				t.Fatalf("blocked financial write changed ordinary audit history: %v %v", headErr, afterErr)
			}
			if _, err := mirror.Load(ctx, workspaceID); !errors.Is(err, audit.ErrMirrorMissing) {
				t.Fatalf("financial write fabricated a trusted OS baseline: %v", err)
			}
			if _, err := mirror.LoadInitialMirrorLifecycle(ctx, workspaceID); !errors.Is(err, audit.ErrMirrorMissing) {
				t.Fatalf("financial write fabricated a creation lifecycle: %v", err)
			}
		})
	}
}

// Test-only OS mirror substitute retains exact baselines across composition
// restarts without writing the developer's platform credential store.
type purchaseTestAuditMirror struct {
	mu         sync.Mutex
	baselines  map[string]*tammyv1.AuditMirrorBaseline
	lifecycles map[string]*audit.InitialMirrorLifecycle
	failNext   error
}

func newPurchaseTestAuditMirror() *purchaseTestAuditMirror {
	return &purchaseTestAuditMirror{baselines: map[string]*tammyv1.AuditMirrorBaseline{}, lifecycles: map[string]*audit.InitialMirrorLifecycle{}}
}

func (store *purchaseTestAuditMirror) Load(_ context.Context, workspaceID string) (*tammyv1.AuditMirrorBaseline, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	value := store.baselines[workspaceID]
	if value == nil {
		return nil, audit.ErrMirrorMissing
	}
	return proto.Clone(value).(*tammyv1.AuditMirrorBaseline), nil
}
func (store *purchaseTestAuditMirror) CompareAndSwap(_ context.Context, expected, target *tammyv1.AuditMirrorBaseline) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.failNext != nil {
		err := store.failNext
		store.failNext = nil
		return err
	}
	current := store.baselines[target.WorkspaceId]
	if !proto.Equal(current, expected) {
		return audit.ErrMirrorConflict
	}
	store.baselines[target.WorkspaceId] = proto.Clone(target).(*tammyv1.AuditMirrorBaseline)
	return nil
}

func (store *purchaseTestAuditMirror) failNextPublication() {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.failNext = errors.New("injected platform mirror publication failure")
}

func assertPurchaseCommandOrdinaryAudit(t *testing.T, server documentPageServer, operationKey string, result proto.Message, action tammyv1.PurchaseAuditAction) {
	t.Helper()
	ctx := context.Background()
	rows, err := server.module.db.QueryContext(ctx, `SELECT workspace_id,generation,sequence FROM audit_events_v1 WHERE idempotency_key=? AND event_type=?`, operationKey, tammyv1.AuditEventType_AUDIT_EVENT_TYPE_PURCHASE_ACCOUNTING_CHANGED)
	if err != nil {
		t.Fatal(err)
	}
	var workspaceID string
	var generation, sequence uint64
	if !rows.Next() || rows.Scan(&workspaceID, &generation, &sequence) != nil || rows.Next() || rows.Err() != nil {
		_ = rows.Close()
		t.Fatal("accepted/replayed purchase has other than one specific ordinary event")
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	stored, err := audit.LoadStoredEvents(ctx, server.module.db, workspaceID, generation, sequence, sequence)
	if err != nil || len(stored) != 1 {
		t.Fatalf("retained ordinary event: %v", err)
	}
	event := stored[0].Event
	body := event.GetPayload().GetPurchaseAccountingChanged()
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	if body == nil || body.Action != action || body.AccountingVersion == 0 || body.FinancialRevision == 0 || event.Actor == nil || !proto.Equal(event.Actor, server.authentication) || event.GetOrganisationId() != server.organisationID || event.Result == nil || event.Result.TypeName != string(result.ProtoReflect().Descriptor().FullName()) || !bytes.Equal(event.Result.DeterministicSha256, digest[:]) || len(stored[0].PayloadProto) > 64 || len(event.AffectedResources) == 0 || len(event.AffectedResources) > 64 {
		t.Fatalf("specific bounded ordinary action/result/source provenance missing: %v", event)
	}
	found := false
	for _, resource := range event.AffectedResources {
		if resource.Type == "purchase_accounting_event" && resource.Id == operationKey && bytes.Equal(resource.ContentHash, digest[:]) {
			found = true
		}
	}
	if !found {
		t.Fatal("ordinary audit omits complete immutable accounting-result reference")
	}
	var accountingVersion, financialRevision uint64
	if err := server.module.db.QueryRowContext(ctx, `SELECT accounting_version,financial_revision FROM purchase_accounting_events_v1 WHERE operation_key=?`, operationKey).Scan(&accountingVersion, &financialRevision); err != nil {
		t.Fatal(err)
	}
	if body.AccountingVersion != accountingVersion || body.FinancialRevision != financialRevision {
		t.Fatal("ordinary audit did not retain actual committed revisions")
	}
}

func TestPurchaseCommandOrdinaryAuditLateFailureRollsBackChainAndMirror(t *testing.T) {
	for _, trigger := range []string{
		`CREATE TRIGGER reject_purchase_ordinary BEFORE INSERT ON audit_events_v1 WHEN NEW.event_type=18 BEGIN SELECT RAISE(ABORT,'private ordinary audit rejection'); END`,
		`CREATE TRIGGER reject_purchase_result BEFORE UPDATE ON command_idempotency_v1 WHEN NEW.outcome='COMMITTED' BEGIN SELECT RAISE(ABORT,'private result rejection'); END`,
	} {
		t.Run(trigger[:35], func(t *testing.T) {
			server := newDocumentPageServer(t)
			ctx := context.Background()
			document := server.reviewedPurchase(t)
			request := server.postRequest(t, document)
			before := purchaseTLSCounts(t, server)
			header, err := audit.LoadChainHeader(ctx, server.module.db, server.authenticationWorkspaceID(t), 0)
			if err != nil {
				t.Fatal(err)
			}
			mirror, err := server.auditMirror.Load(ctx, header.WorkspaceID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := server.module.db.ExecContext(ctx, trigger); err != nil {
				t.Fatal(err)
			}
			if _, err := server.purchases.PostReviewedPurchase(ctx, brandingRequest(request, server.capability)); connect.CodeOf(err) != connect.CodeInternal {
				t.Fatalf("late audit/result error: %v", err)
			}
			assertPurchaseTLSCounts(t, before, purchaseTLSCounts(t, server))
			after, err := audit.LoadChainHeader(ctx, server.module.db, header.WorkspaceID, 0)
			if err != nil || after.CurrentSequence != header.CurrentSequence || after.CurrentHead != header.CurrentHead {
				t.Fatalf("late failure retained ordinary head: %v", err)
			}
			mirrored, err := server.auditMirror.Load(ctx, header.WorkspaceID)
			if err != nil || !proto.Equal(mirror, mirrored) {
				t.Fatalf("late failure published external mirror: %v", err)
			}
		})
	}
}

func TestPurchaseCommandMirrorFailureRecoversExactCommittedKey(t *testing.T) {
	server := newDocumentPageServer(t)
	ctx := context.Background()
	document := server.reviewedPurchase(t)
	request := server.postRequest(t, document)
	header, err := audit.LoadChainHeader(ctx, server.module.db, server.authenticationWorkspaceID(t), 0)
	if err != nil {
		t.Fatal(err)
	}
	mirror, err := server.auditMirror.Load(ctx, header.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	server.auditMirror.failNextPublication()
	if response, err := server.purchases.PostReviewedPurchase(ctx, brandingRequest(request, server.capability)); err == nil || response != nil {
		t.Fatalf("post-commit publication failure must leave result uncertain: %v %v", response, err)
	}
	committed, err := audit.LoadChainHeader(ctx, server.module.db, header.WorkspaceID, 0)
	if err != nil || committed.CurrentSequence != header.CurrentSequence+1 || server.activation.AuditGate.Writable() {
		t.Fatalf("failed publication must keep SQL result and disable writes: %v", err)
	}
	mirrored, err := server.auditMirror.Load(ctx, header.WorkspaceID)
	if err != nil || !proto.Equal(mirror, mirrored) {
		t.Fatalf("failed publication changed OS mirror: %v", err)
	}
	beforeRetry := purchaseTLSCounts(t, server)
	var resultBytes []byte
	if err := server.module.db.QueryRowContext(ctx, `SELECT result_proto FROM command_idempotency_v1 WHERE operation_key=? AND outcome='COMMITTED'`, request.CommandContext.IdempotencyKey).Scan(&resultBytes); err != nil {
		t.Fatal(err)
	}
	retained := &tammyv1.PostReviewedPurchaseResponse{}
	if err := proto.Unmarshal(resultBytes, retained); err != nil {
		t.Fatal(err)
	}
	replayed, err := server.purchases.PostReviewedPurchase(ctx, brandingRequest(request, server.capability))
	if err != nil || !proto.Equal(retained, replayed.Msg) {
		t.Fatalf("exact committed key could not reconcile and replay: %v %v", replayed, err)
	}
	assertPurchaseTLSCounts(t, beforeRetry, purchaseTLSCounts(t, server))
	after, err := audit.LoadChainHeader(ctx, server.module.db, header.WorkspaceID, 0)
	if err != nil || after.CurrentSequence != committed.CurrentSequence || after.CurrentHead != committed.CurrentHead || !server.activation.AuditGate.Writable() {
		t.Fatalf("replay changed SQL audit head or did not recover existing baseline: %v", err)
	}
	mirrored, err = server.auditMirror.Load(ctx, header.WorkspaceID)
	if err != nil || mirrored.Sequence != after.CurrentSequence || !bytes.Equal(mirrored.Head, after.CurrentHead[:]) {
		t.Fatalf("verified existing baseline was not repaired: %v", err)
	}
	assertPurchaseCommandOrdinaryAudit(t, server, request.CommandContext.IdempotencyKey, replayed.Msg, tammyv1.PurchaseAuditAction_PURCHASE_AUDIT_ACTION_POSTED)
}
func (store *purchaseTestAuditMirror) LoadInitialMirrorLifecycle(_ context.Context, workspaceID string) (*audit.InitialMirrorLifecycle, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	value := store.lifecycles[workspaceID]
	if value == nil {
		return nil, audit.ErrMirrorMissing
	}
	copy := *value
	return &copy, nil
}
func (store *purchaseTestAuditMirror) SaveInitialMirrorLifecycle(_ context.Context, value *audit.InitialMirrorLifecycle) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	copy := *value
	store.lifecycles[value.WorkspaceID] = &copy
	return nil
}

func TestPurchaseCommandOrdinaryAuditChainCommitsOnce(t *testing.T) {
	server := newDocumentPageServer(t)
	ctx := context.Background()
	initial, err := audit.LoadChainHeader(ctx, server.module.db, server.authenticationWorkspaceID(t), 0)
	if err != nil {
		t.Fatalf("new local workspace has no ordinary audit chain: %v", err)
	}
	document := server.reviewedPurchase(t)
	request := server.postRequest(t, document)
	posted, err := server.purchases.PostReviewedPurchase(ctx, brandingRequest(request, server.capability))
	if err != nil {
		t.Fatal(err)
	}
	after, err := audit.LoadChainHeader(ctx, server.module.db, initial.WorkspaceID, 0)
	if err != nil || after.CurrentSequence != initial.CurrentSequence+1 {
		t.Fatalf("purchase must append one ordinary audit event: before=%d after=%d error=%v", initial.CurrentSequence, after.CurrentSequence, err)
	}
	stored, err := audit.LoadStoredEvents(ctx, server.module.db, initial.WorkspaceID, after.Generation, after.CurrentSequence, after.CurrentSequence)
	if err != nil || len(stored) == 0 {
		t.Fatalf("load retained chain: %v", err)
	}
	event := stored[len(stored)-1].Event
	resultBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(posted.Msg)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(resultBytes)
	if event == nil || event.CommandType != "tammy.v1.PurchaseService.PostReviewedPurchase" || event.GetIdempotencyKey() != request.CommandContext.IdempotencyKey || event.Actor == nil || !proto.Equal(event.Actor, server.authentication) || event.GetOrganisationId() != server.organisationID || event.Result == nil || !bytes.Equal(event.Result.DeterministicSha256, digest[:]) {
		t.Fatalf("ordinary audit omitted accepted command/result provenance: %v", event)
	}
	if event.Type == tammyv1.AuditEventType_AUDIT_EVENT_TYPE_JOURNAL_POSTED || event.Payload == nil || event.Source == nil || event.Source.Id != posted.Msg.Posting.Id || len(event.AffectedResources) == 0 {
		t.Fatalf("purchase requires a bounded specific ordinary payload and affected sources: %v", event)
	}
	if _, err := server.purchases.PostReviewedPurchase(ctx, brandingRequest(request, server.capability)); err != nil {
		t.Fatal(err)
	}
	replayed, err := audit.LoadChainHeader(ctx, server.module.db, initial.WorkspaceID, 0)
	if err != nil || replayed.CurrentSequence != after.CurrentSequence || replayed.CurrentHead != after.CurrentHead {
		t.Fatalf("exact replay changed audit chain/head: %v", err)
	}
}

func (server documentPageServer) authenticationWorkspaceID(t *testing.T) string {
	t.Helper()
	var workspaceID string
	if err := server.module.db.QueryRowContext(context.Background(), `SELECT workspace_id FROM command_idempotency_v1 LIMIT 1`).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	return workspaceID
}
