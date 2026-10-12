//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"context"
	"errors"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
)

func TestPurchaseCoverageRejectsCanonicalAuditPayloadForDifferentPurchase(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	posted := postReportingPurchase(t, f, "AUDIT-PAYLOAD-INTEGRITY", purchaseReportingDate(2026, 9, 10), 10000, 1000)
	if posted == nil || posted.Posting == nil || posted.Posting.Id == "" {
		t.Fatalf("repository post did not create a purchase posting: %+v", posted)
	}
	operationKey := f.request.CommandContext.IdempotencyKey
	if got := postScalar(t, tx, `SELECT count(*) FROM purchase_accounting_events_v1 WHERE operation_key=?`, operationKey); got != 1 {
		t.Fatalf("repository post event count=%d, want 1", got)
	}
	if got := postScalar(t, tx, `SELECT count(*) FROM purchase_command_audit_v1 WHERE operation_key=?`, operationKey); got != 0 {
		t.Fatalf("repository-only post unexpectedly wrote an audit row: count=%d", got)
	}

	// Keep every audit identity field and the linked event result hash valid,
	// while storing a canonical request for a different document under this
	// operation key. The row-level schema/trigger permits this payload; capture
	// must reject the contradicted source lineage rather than accept opaque bytes.
	wrongPayload := proto.Clone(f.request).(*tammyv1.PostReviewedPurchaseRequest)
	wrongPayload.DocumentId = "018f0000-0000-7000-8000-000000009999"
	encodedPayload, err := proto.MarshalOptions{Deterministic: true}.Marshal(wrongPayload)
	if err != nil {
		t.Fatal(err)
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO purchase_command_audit_v1(
			operation_key,organisation_id,workspace_id,actor_user_id,session_id,command_type,result_sha256,payload_proto,occurred_at
		)
		SELECT e.operation_key,e.organisation_id,c.workspace_id,c.actor_user_id,?,e.command_type,e.result_sha256,?,e.created_at
		FROM purchase_accounting_events_v1 e
		JOIN command_idempotency_v1 c ON c.operation_key=e.operation_key
		WHERE e.operation_key=?`,
		"coverage-integrity-session", encodedPayload, operationKey)
	if err != nil {
		t.Fatalf("insert validly linked audit metadata with a different canonical purchase payload: %v", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		t.Fatalf("inserted audit rows=%d, want 1 (err=%v)", affected, err)
	}

	capture, err := f.repo(tx).CaptureCoverageForBAS(ctx, postingOrg)
	if capture != nil || !errors.Is(err, purchases.ErrRepository) {
		t.Fatalf("capture accepted or partially returned mismatched audit payload: capture=%+v err=%v", capture, err)
	}
}
