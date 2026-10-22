//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/authorisation"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/idempotency"
	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
	"github.com/tammyapp/tammy/services/core/internal/platform/faults"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/proto"
)

func TestAuditServiceVerifiesFullChainBeforeFilteredPagination(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	seedAuditChain(t, ctx, database, 2)
	service := newAuditServiceFixture(t, database, workspaceID, false)
	authentication := &tammyv1.AuthenticationContext{ActorUserId: "01890f60-4d6d-7c12-8f02-6c9129d5b006", SessionId: "01890f60-4d6d-7c12-8f02-6c9129d5b007"}
	verified, err := service.VerifyChain(ctx, connect.NewRequest(&tammyv1.VerifyChainRequest{Authentication: authentication, WorkspaceId: workspaceID}))
	if err != nil || verified.Msg.Integrity != tammyv1.AuditChainIntegrity_AUDIT_CHAIN_INTEGRITY_VALID || verified.Msg.VerifiedThroughSequence != 2 {
		t.Fatalf("verify response=%#v err=%v", verified, err)
	}
	first, err := service.ListAuditEvents(ctx, connect.NewRequest(&tammyv1.ListAuditEventsRequest{Authentication: authentication,
		WorkspaceId: workspaceID, Filter: &tammyv1.AuditEventFilter{EventTypes: []tammyv1.AuditEventType{tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_STATE_CHANGED}},
		Page: &tammyv1.PageRequest{PageSize: 1}}))
	if err != nil || len(first.Msg.Events) != 1 || first.Msg.Page.NextCursor == nil {
		t.Fatalf("first page=%#v err=%v", first, err)
	}
	second, err := service.ListAuditEvents(ctx, connect.NewRequest(&tammyv1.ListAuditEventsRequest{Authentication: authentication,
		WorkspaceId: workspaceID, Filter: &tammyv1.AuditEventFilter{EventTypes: []tammyv1.AuditEventType{tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_STATE_CHANGED}},
		Page: &tammyv1.PageRequest{PageSize: 1, Cursor: first.Msg.Page.NextCursor}}))
	if err != nil || len(second.Msg.Events) != 1 || second.Msg.Events[0].Sequence != 2 || second.Msg.Page.NextCursor != nil {
		t.Fatalf("second page=%#v err=%v", second, err)
	}
	if _, err := database.ExecContext(ctx, `DROP TRIGGER audit_events_v1_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE audit_events_v1 SET payload_proto=x'00' WHERE sequence=2`); err != nil {
		t.Fatal(err)
	}
	missingActor := "01890f60-4d6d-7c12-8f02-6c9129d5b099"
	if _, err := service.ListAuditEvents(ctx, connect.NewRequest(&tammyv1.ListAuditEventsRequest{Authentication: authentication,
		WorkspaceId: workspaceID, Filter: &tammyv1.AuditEventFilter{ActorUserId: &missingActor},
		Page: &tammyv1.PageRequest{PageSize: 10}})); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("tampered event outside filter error=%v, want ErrInvalidEvent", err)
	}
}

func TestAuditEventCursorKeepsOriginalSnapshotAfterAppend(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	seedAuditChain(t, ctx, database, 2)
	service := newAuditServiceFixture(t, database, workspaceID, false)
	authentication := &tammyv1.AuthenticationContext{
		ActorUserId: "01890f60-4d6d-7c12-8f02-6c9129d5b006",
		SessionId:   "01890f60-4d6d-7c12-8f02-6c9129d5b007",
	}
	request := func(cursor *string) *connect.Request[tammyv1.ListAuditEventsRequest] {
		return connect.NewRequest(&tammyv1.ListAuditEventsRequest{Authentication: authentication,
			WorkspaceId: workspaceID, Filter: &tammyv1.AuditEventFilter{},
			Page: &tammyv1.PageRequest{PageSize: 1, Cursor: cursor}})
	}
	first, err := service.ListAuditEvents(ctx, request(nil))
	if err != nil || len(first.Msg.Events) != 1 || first.Msg.Events[0].Sequence != 1 || first.Msg.Page.NextCursor == nil {
		t.Fatalf("first page=%#v err=%v", first, err)
	}
	transaction, err := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	event, payload := integrationAuditEvent("01890f60-4d6d-7c12-8f02-6c9129d5b073")
	if _, err := appendStoredEventForTest(ctx, transaction, event, payload); err != nil {
		_ = transaction.Rollback()
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	second, err := service.ListAuditEvents(ctx, request(first.Msg.Page.NextCursor))
	if err != nil || len(second.Msg.Events) != 1 || second.Msg.Events[0].Sequence != 2 || second.Msg.Page.NextCursor != nil {
		t.Fatalf("stable second page=%#v err=%v", second, err)
	}
}

var errUnboundedAuditEventQuery = errors.New("unbounded audit event query")

type boundedAuditQueryTransactions struct {
	*auditServiceTransactions
	queries                [][]any
	sql                    []string
	auditQueryCount        int
	afterAuditQuery        func(int)
	beginReadWithoutCancel bool
}

func (transactions *boundedAuditQueryTransactions) Read(ctx context.Context, read func(ServiceTransaction) error) error {
	if transactions.beginReadWithoutCancel {
		transaction, err := transactions.database.BeginEncryptedTx(context.WithoutCancel(ctx), nil)
		if err != nil {
			return err
		}
		scope := &auditServiceTransaction{Executor: transaction, id: "01890f60-4d6d-7c12-8f02-6c9129d5b070"}
		if err := read(&boundedAuditQueryTransaction{ServiceTransaction: scope, owner: transactions}); err != nil {
			_ = transaction.Rollback()
			return err
		}
		return transaction.Commit()
	}
	return transactions.auditServiceTransactions.Read(ctx, func(transaction ServiceTransaction) error {
		return read(&boundedAuditQueryTransaction{ServiceTransaction: transaction, owner: transactions})
	})
}

type boundedAuditQueryTransaction struct {
	ServiceTransaction
	owner *boundedAuditQueryTransactions
}

func (transaction *boundedAuditQueryTransaction) QueryContext(
	ctx context.Context,
	query string,
	arguments ...any,
) (*sql.Rows, error) {
	transaction.owner.sql = append(transaction.owner.sql, query)
	transaction.owner.queries = append(transaction.owner.queries, append([]any(nil), arguments...))
	if strings.Contains(query, "FROM audit_events_v1") {
		transaction.owner.auditQueryCount++
		if !strings.Contains(query, "LIMIT ?") || len(arguments) == 0 {
			return nil, errUnboundedAuditEventQuery
		}
		limit, ok := arguments[len(arguments)-1].(int)
		if !ok || limit > int(StoredEventPageSizeLimit) {
			return nil, errUnboundedAuditEventQuery
		}
	}
	queryContext := ctx
	if transaction.owner.beginReadWithoutCancel {
		queryContext = context.WithoutCancel(ctx)
	}
	rows, err := transaction.ServiceTransaction.QueryContext(queryContext, query, arguments...)
	if err == nil && strings.Contains(query, "FROM audit_events_v1") && transaction.owner.afterAuditQuery != nil {
		transaction.owner.afterAuditQuery(transaction.owner.auditQueryCount)
	}
	return rows, err
}

func TestAuditServiceListUsesBoundedFullVerificationAndSparseMatchingQueries(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	seedAuditChain(t, ctx, database, 2)
	service := newAuditServiceFixture(t, database, workspaceID, false)
	base := service.transactions.(*auditServiceTransactions)
	recording := &boundedAuditQueryTransactions{auditServiceTransactions: base}
	service.transactions = recording
	response, err := service.ListAuditEvents(ctx, connect.NewRequest(&tammyv1.ListAuditEventsRequest{
		Authentication: &tammyv1.AuthenticationContext{ActorUserId: "01890f60-4d6d-7c12-8f02-6c9129d5b006",
			SessionId: "01890f60-4d6d-7c12-8f02-6c9129d5b007"},
		WorkspaceId: workspaceID, Filter: &tammyv1.AuditEventFilter{}, Page: &tammyv1.PageRequest{PageSize: 1},
	}))
	if err != nil || len(response.Msg.Events) != 1 {
		t.Fatalf("bounded list=%#v err=%v", response, err)
	}
	matchingMetadata, matchingBytes := 0, 0
	for index, query := range recording.sql {
		if !strings.Contains(query, "FROM audit_events_v1") {
			continue
		}
		arguments := recording.queries[index]
		limit, ok := arguments[len(arguments)-1].(int)
		if !ok {
			t.Fatalf("audit query LIMIT=%#v", arguments[len(arguments)-1])
		}
		if strings.Contains(query, "CASE WHEN length(event_proto) <= ?") {
			matchingMetadata++
			if limit > 201 || !strings.Contains(query, "sequence > ? AND sequence <= ?") ||
				!strings.Contains(query, "length(event_proto) <= ?") {
				t.Fatalf("unbounded matching query limit=%d: %s", limit, query)
			}
		} else if strings.Contains(query, "SELECT sequence, event_proto") {
			matchingBytes++
			if limit > 201 || !strings.Contains(query, "sequence > ? AND sequence <= ?") ||
				!strings.Contains(query, "length(event_proto) <= ?") {
				t.Fatalf("unbounded matching byte query limit=%d: %s", limit, query)
			}
		} else if limit > int(StoredEventPageSizeLimit) {
			t.Fatalf("verification query limit=%d: %s", limit, query)
		}
	}
	if matchingMetadata != 1 || matchingBytes != 1 {
		t.Fatalf("matching metadata=%d bytes=%d, SQL=%q", matchingMetadata, matchingBytes, recording.sql)
	}
}

func TestAuditServiceRejectsNoncanonicalSnapshotCursorBeforeAuditEventReads(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	seedAuditChain(t, ctx, database, 2)
	service := newAuditServiceFixture(t, database, workspaceID, false)
	base := service.transactions.(*auditServiceTransactions)
	recording := &boundedAuditQueryTransactions{auditServiceTransactions: base}
	service.transactions = recording
	filter := &tammyv1.AuditEventFilter{}
	queryHash, err := auditEventQueryHash(workspaceID, filter)
	if err != nil {
		t.Fatal(err)
	}
	token, err := service.cursors.Encode(paging.Cursor{Snapshot: "01:2:" + strings.Repeat("00", sha256.Size),
		Position: "01", QueryHash: queryHash})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ListAuditEvents(ctx, connect.NewRequest(&tammyv1.ListAuditEventsRequest{
		Authentication: &tammyv1.AuthenticationContext{ActorUserId: "actor", SessionId: "session"},
		WorkspaceId:    workspaceID, Filter: filter, Page: &tammyv1.PageRequest{PageSize: 1, Cursor: &token},
	})); !errors.Is(err, ErrAuditService) {
		t.Fatalf("noncanonical cursor error=%v, want ErrAuditService", err)
	}
	for _, query := range recording.sql {
		if strings.Contains(query, "FROM audit_events_v1") {
			t.Fatalf("noncanonical cursor reached audit event read: %s", query)
		}
	}
}

func TestAuditServiceSparseTenThousandPaginationIsStableAcrossAppend(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	matchingActor := "01890f60-4d6d-7c12-8f02-6c9129d5b006"
	seedLargeVerifiedAuditChain(t, database, 10_000, matchingActor, 997)
	service := newAuditServiceFixture(t, database, workspaceID, false)
	base := service.transactions.(*auditServiceTransactions)
	recording := &boundedAuditQueryTransactions{auditServiceTransactions: base}
	service.transactions = recording
	authentication := &tammyv1.AuthenticationContext{ActorUserId: matchingActor,
		SessionId: "01890f60-4d6d-7c12-8f02-6c9129d5b007"}
	filter := &tammyv1.AuditEventFilter{ActorUserId: &matchingActor,
		EventTypes: []tammyv1.AuditEventType{tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_STATE_CHANGED}}
	var cursor *string
	var got []uint64
	pages := 0
	for pageNumber := 0; ; pageNumber++ {
		response, err := service.ListAuditEvents(ctx, connect.NewRequest(&tammyv1.ListAuditEventsRequest{
			Authentication: authentication, WorkspaceId: workspaceID, Filter: filter,
			Page: &tammyv1.PageRequest{PageSize: 3, Cursor: cursor},
		}))
		if err != nil {
			t.Fatalf("page %d: %v", pageNumber, err)
		}
		pages++
		for _, event := range response.Msg.Events {
			got = append(got, event.Sequence)
		}
		if pageNumber == 0 {
			transaction, err := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
			if err != nil {
				t.Fatal(err)
			}
			event, payload := integrationAuditEvent("01890f60-4d6d-7c12-8f02-6c9129d5ffff")
			event.Actor.ActorUserId = matchingActor
			if _, err := appendStoredEventForTest(ctx, transaction, event, payload); err != nil {
				_ = transaction.Rollback()
				t.Fatal(err)
			}
			if err := transaction.Commit(); err != nil {
				t.Fatal(err)
			}
		}
		cursor = response.Msg.Page.NextCursor
		if cursor == nil {
			break
		}
	}
	want := make([]uint64, 0, 10)
	for sequence := uint64(997); sequence <= 10_000; sequence += 997 {
		want = append(want, sequence)
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("sparse sequences=%v, want %v", got, want)
	}
	matchingMetadata, matchingBytes := 0, 0
	for index, query := range recording.sql {
		if !strings.Contains(query, "FROM audit_events_v1") {
			continue
		}
		arguments := recording.queries[index]
		limit, ok := arguments[len(arguments)-1].(int)
		if !ok || limit <= 0 {
			t.Fatalf("audit query LIMIT=%#v: %s", arguments[len(arguments)-1], query)
		}
		switch {
		case strings.Contains(query, "CASE WHEN length(event_proto) <= ?"):
			matchingMetadata++
			if limit != 4 {
				t.Fatalf("matching metadata LIMIT=%d, want pageSize+1", limit)
			}
		case strings.Contains(query, "SELECT sequence, event_proto"):
			matchingBytes++
			if limit > 4 {
				t.Fatalf("matching byte LIMIT=%d, want <=pageSize+1", limit)
			}
		default:
			if limit > int(StoredEventPageSizeLimit) {
				t.Fatalf("verification LIMIT=%d", limit)
			}
		}
	}
	if matchingMetadata != pages || matchingBytes != pages {
		t.Fatalf("matching query pairs metadata=%d bytes=%d pages=%d", matchingMetadata, matchingBytes, pages)
	}
}

func TestAuditServiceCursorContinuationRejectsInPrefixMutation(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	seedAuditChain(t, ctx, database, 2)
	service := newAuditServiceFixture(t, database, workspaceID, false)
	authentication := &tammyv1.AuthenticationContext{ActorUserId: "01890f60-4d6d-7c12-8f02-6c9129d5b006", SessionId: "session"}
	request := func(cursor *string) *connect.Request[tammyv1.ListAuditEventsRequest] {
		return connect.NewRequest(&tammyv1.ListAuditEventsRequest{Authentication: authentication,
			WorkspaceId: workspaceID, Filter: &tammyv1.AuditEventFilter{}, Page: &tammyv1.PageRequest{PageSize: 1, Cursor: cursor}})
	}
	first, err := service.ListAuditEvents(ctx, request(nil))
	if err != nil || first.Msg.Page.NextCursor == nil {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	if _, err := database.ExecContext(ctx, `DROP TRIGGER audit_events_v1_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE audit_events_v1 SET event_proto=x'ff' WHERE sequence=2`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ListAuditEvents(ctx, request(first.Msg.Page.NextCursor)); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("mutated continuation error=%v, want ErrInvalidEvent", err)
	}
}

func TestAuditServiceVerifiesGenesisOnlyChain(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	seedAuditChain(t, ctx, database, 0)
	service := newAuditServiceFixture(t, database, workspaceID, false)
	authentication := &tammyv1.AuthenticationContext{ActorUserId: "01890f60-4d6d-7c12-8f02-6c9129d5b006", SessionId: "01890f60-4d6d-7c12-8f02-6c9129d5b007"}

	verified, err := service.VerifyChain(ctx, connect.NewRequest(&tammyv1.VerifyChainRequest{Authentication: authentication, WorkspaceId: workspaceID}))
	if err != nil || verified.Msg.Integrity != tammyv1.AuditChainIntegrity_AUDIT_CHAIN_INTEGRITY_VALID || verified.Msg.VerifiedThroughSequence != 0 {
		t.Fatalf("genesis verification=%#v err=%v", verified, err)
	}
}

func TestAuditServiceVerifyChainUsesBoundedStreamingPages(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	seedLargeVerifiedAuditChain(t, database, 300,
		"01890f60-4d6d-7c12-8f02-6c9129d5b006", 17)
	var expectedEndHead []byte
	if err := database.QueryRowContext(ctx, `SELECT event_hash FROM audit_events_v1
		WHERE workspace_id=? AND generation=1 AND sequence=270`, workspaceID).Scan(&expectedEndHead); err != nil {
		t.Fatal(err)
	}
	service := newAuditServiceFixture(t, database, workspaceID, false)
	base := service.transactions.(*auditServiceTransactions)
	recording := &boundedAuditQueryTransactions{auditServiceTransactions: base}
	service.transactions = recording
	start, end := uint64(101), uint64(270)
	response, err := service.VerifyChain(ctx, connect.NewRequest(&tammyv1.VerifyChainRequest{
		Authentication: &tammyv1.AuthenticationContext{ActorUserId: "01890f60-4d6d-7c12-8f02-6c9129d5b006",
			SessionId: "01890f60-4d6d-7c12-8f02-6c9129d5b007"},
		WorkspaceId: workspaceID, StartSequence: &start, EndSequence: &end,
	}))
	if err != nil || response.Msg.Integrity != tammyv1.AuditChainIntegrity_AUDIT_CHAIN_INTEGRITY_VALID ||
		response.Msg.VerifiedThroughSequence != end || !bytes.Equal(response.Msg.VerifiedHead, expectedEndHead) {
		t.Fatalf("bounded range verification=%#v err=%v, want end=%d head=%x", response, err, end, expectedEndHead)
	}
	auditQueries := 0
	for index, query := range recording.sql {
		if !strings.Contains(query, "FROM audit_events_v1") {
			continue
		}
		auditQueries++
		if !strings.Contains(query, "LIMIT ?") {
			t.Fatalf("whole-history audit query: %s", query)
		}
		arguments := recording.queries[index]
		limit, ok := arguments[len(arguments)-1].(int)
		if !ok || limit <= 0 || limit > int(StoredEventPageSizeLimit) {
			t.Fatalf("audit query LIMIT=%#v: %s", arguments[len(arguments)-1], query)
		}
	}
	if auditQueries < 4 {
		t.Fatalf("audit queries=%d, want metadata+bytes across at least two pages: %q", auditQueries, recording.sql)
	}
}

func TestAuditServiceRejectsUnmirroredAppender(t *testing.T) {
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	seedAuditChain(t, context.Background(), database, 0)
	configured := newAuditServiceFixture(t, database, workspaceID, false)
	service, err := NewService(ServiceConfig{Access: configured.access, Transactions: configured.transactions,
		Elector: configured.elector, Clock: configured.clock, NewID: configured.newID, Cursors: configured.cursors,
		SchemaFingerprint: configured.schemaFingerprint, Appender: &Appender{}})
	if !errors.Is(err, ErrAuditService) || service != nil {
		t.Fatalf("unmirrored service=%v err=%v, want ErrAuditService", service, err)
	}
}

func TestAuditServiceWriteGateRejectsExportBeforeElectionAndLeavesNoPartialSQL(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	seedAuditChain(t, ctx, database, 0)
	service := newAuditServiceFixture(t, database, workspaceID, false)
	service.appender.gate.set(false, false)
	newIDCalls := 0
	service.newID = func() (string, error) {
		newIDCalls++
		return "01890f60-4d6d-7c12-8f02-6c9129d5b099", nil
	}
	if _, err := service.ExportEvidence(ctx, connect.NewRequest(exportEvidenceServiceRequest(workspaceID,
		"01890f60-4d6d-7c12-8f02-6c9129d5b041"))); !errors.Is(err, ErrWriteGate) {
		t.Fatalf("locked export error=%v, want ErrWriteGate", err)
	}
	if newIDCalls != 0 {
		t.Fatalf("locked export reached work allocation %d times", newIDCalls)
	}
	assertAuditServiceCounts(t, database, 0, 0, 0)
}

func TestAuditServiceAllowsEvidenceExportInVerifiedMovedReadOnlyModeWithoutEstablishingMirror(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	seedAuditChain(t, ctx, database, 0)
	service := newAuditServiceFixture(t, database, workspaceID, false)
	service.appender.gate.set(false, true)
	response, err := service.ExportEvidence(ctx, connect.NewRequest(exportEvidenceServiceRequest(workspaceID,
		"01890f60-4d6d-7c12-8f02-6c9129d5b041")))
	if err != nil || response.Msg.Job == nil {
		t.Fatalf("moved read-only export=%#v err=%v", response, err)
	}
	store := service.appender.mirror.(*memoryMirrorStore)
	if service.appender.gate.Writable() || !service.appender.gate.EvidenceExportAllowed() || store.saves != 0 {
		t.Fatalf("moved export changed trust: writable=%v evidence=%v saves=%d", service.appender.gate.Writable(),
			service.appender.gate.EvidenceExportAllowed(), store.saves)
	}
	assertAuditServiceCounts(t, database, 1, 1, 1)
}

func TestAuditServiceExportAndCancelUseExactReplayAndOneCallerOwnedUnitOfWork(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	seedAuditChain(t, ctx, database, 0)
	service := newAuditServiceFixture(t, database, workspaceID, false)
	request := exportEvidenceServiceRequest(workspaceID, "01890f60-4d6d-7c12-8f02-6c9129d5b041")
	created, err := service.ExportEvidence(ctx, connect.NewRequest(proto.Clone(request).(*tammyv1.ExportEvidenceRequest)))
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := LoadExportJob(ctx, database, created.Msg.Job.Id)
	if err != nil || persisted.Filter == nil || !proto.Equal(persisted.Filter, request.Filter) ||
		persisted.SnapshotGeneration != 1 || len(persisted.SnapshotHead) != sha256.Size ||
		persisted.DestinationProvider != "approved_file" || persisted.EvidenceProvider != "audit_chain" || persisted.DestinationCapability != request.Destination.CapabilityId {
		t.Fatalf("persisted export input=%#v err=%v", persisted, err)
	}
	replayed, err := service.ExportEvidence(ctx, connect.NewRequest(proto.Clone(request).(*tammyv1.ExportEvidenceRequest)))
	if err != nil || !proto.Equal(created.Msg, replayed.Msg) {
		t.Fatalf("exact replay=%#v err=%v", replayed, err)
	}
	assertAuditServiceCounts(t, database, 1, 1, 1)
	var commandID, retainedOperationKey string
	if err := database.QueryRowContext(ctx, `SELECT command_id, idempotency_key FROM audit_events_v1 WHERE sequence=1`).
		Scan(&commandID, &retainedOperationKey); err != nil || commandID == retainedOperationKey || retainedOperationKey != request.CommandContext.IdempotencyKey {
		t.Fatalf("command identity command=%q operation=%q err=%v", commandID, retainedOperationKey, err)
	}
	changed := proto.Clone(request).(*tammyv1.ExportEvidenceRequest)
	changed.Destination.CapabilityId = "different-approved-capability"
	if _, err := service.ExportEvidence(ctx, connect.NewRequest(changed)); !errors.Is(err, faults.New(faults.CodeIdempotencyConflict, nil)) {
		t.Fatalf("changed export error=%v", err)
	}
	cancelKey := "01890f60-4d6d-7c12-8f02-6c9129d5b042"
	cancelRequest := &tammyv1.CancelAuditExportRequest{
		CommandContext: &tammyv1.CommandContext{IdempotencyKey: cancelKey, Authentication: request.CommandContext.Authentication},
		JobId:          created.Msg.Job.Id, ExpectedVersion: created.Msg.Job.Version,
	}
	cancelled, err := service.CancelAuditExport(ctx, connect.NewRequest(proto.Clone(cancelRequest).(*tammyv1.CancelAuditExportRequest)))
	if err != nil || cancelled.Msg.Job.State != tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_CANCELLED {
		t.Fatalf("cancel response=%#v err=%v", cancelled, err)
	}
	replayedCancellation, err := service.CancelAuditExport(ctx, connect.NewRequest(proto.Clone(cancelRequest).(*tammyv1.CancelAuditExportRequest)))
	if err != nil || !proto.Equal(cancelled.Msg, replayedCancellation.Msg) {
		t.Fatalf("exact cancellation replay=%#v err=%v", replayedCancellation, err)
	}
	assertAuditServiceCounts(t, database, 2, 2, 1)
}

func TestAuditServiceRejectsCancellationAfterCompletionWithoutCompletingCommandOrAudit(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	seedAuditChain(t, ctx, database, 0)
	service := newAuditServiceFixture(t, database, workspaceID, false)
	exportRequest := exportEvidenceServiceRequest(workspaceID, "01890f60-4d6d-7c12-8f02-6c9129d5b043")
	created, err := service.ExportEvidence(ctx, connect.NewRequest(exportRequest))
	if err != nil {
		t.Fatal(err)
	}
	completedAt := time.Date(2026, 8, 4, 8, 1, 0, 0, time.UTC)
	if _, err := database.ExecContext(ctx, `UPDATE audit_export_jobs_v1 SET state='COMPLETED', stage='COMPLETED', completed_at=?, updated_at=?, version=version+1 WHERE id=?`,
		formatTimestamp(completedAt), formatTimestamp(completedAt), created.Msg.Job.Id); err != nil {
		t.Fatal(err)
	}
	completed, err := LoadExportJob(ctx, database, created.Msg.Job.Id)
	if err != nil {
		t.Fatal(err)
	}
	response, cancelErr := service.CancelAuditExport(ctx, connect.NewRequest(&tammyv1.CancelAuditExportRequest{
		CommandContext: &tammyv1.CommandContext{IdempotencyKey: "01890f60-4d6d-7c12-8f02-6c9129d5b044", Authentication: exportRequest.CommandContext.Authentication},
		JobId:          completed.ID, ExpectedVersion: completed.Version,
	}))
	if response != nil {
		t.Fatalf("completed cancellation returned response=%#v", response)
	}
	connectErr := new(connect.Error)
	if !errors.As(cancelErr, &connectErr) || connectErr.Code() != connect.CodeFailedPrecondition || len(connectErr.Details()) != 1 {
		t.Fatalf("completed cancellation error=%#v", cancelErr)
	}
	detail, err := connectErr.Details()[0].Value()
	if err != nil {
		t.Fatal(err)
	}
	transition, ok := detail.(*tammyv1.InvalidStateTransitionErrorDetail)
	if !ok || transition.Context == nil || transition.Context.Code != "COMMIT_ALREADY_COMPLETED" ||
		transition.CurrentState != "COMPLETED" || transition.Resource != "audit_export_job" ||
		transition.Context.Retry != tammyv1.RetryClassification_RETRY_CLASSIFICATION_NEVER {
		t.Fatalf("completed cancellation detail=%#v", detail)
	}
	assertAuditServiceCounts(t, database, 1, 1, 1)
}

func TestAuditServiceReportsCommitPointBeforeStaleVersionForRacingCancellation(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	seedAuditChain(t, ctx, database, 0)
	service := newAuditServiceFixture(t, database, workspaceID, false)
	exportRequest := exportEvidenceServiceRequest(workspaceID, "01890f60-4d6d-7c12-8f02-6c9129d5b045")
	created, err := service.ExportEvidence(ctx, connect.NewRequest(exportRequest))
	if err != nil {
		t.Fatal(err)
	}
	commitElectedAt := time.Date(2026, 8, 4, 8, 2, 0, 0, time.UTC)
	if _, err := database.ExecContext(ctx, `UPDATE audit_export_jobs_v1 SET state='RUNNING', stage='DESTINATION_COMMITTING', updated_at=?, version=version+1 WHERE id=?`,
		formatTimestamp(commitElectedAt), created.Msg.Job.Id); err != nil {
		t.Fatal(err)
	}
	response, cancelErr := service.CancelAuditExport(ctx, connect.NewRequest(&tammyv1.CancelAuditExportRequest{
		CommandContext:  &tammyv1.CommandContext{IdempotencyKey: "01890f60-4d6d-7c12-8f02-6c9129d5b046", Authentication: exportRequest.CommandContext.Authentication},
		JobId:           created.Msg.Job.Id,
		ExpectedVersion: created.Msg.Job.Version,
	}))
	if response != nil {
		t.Fatalf("racing cancellation returned response=%#v", response)
	}
	connectErr := new(connect.Error)
	if !errors.As(cancelErr, &connectErr) || connectErr.Code() != connect.CodeFailedPrecondition || len(connectErr.Details()) != 1 {
		t.Fatalf("racing cancellation error=%#v", cancelErr)
	}
	detail, err := connectErr.Details()[0].Value()
	if err != nil {
		t.Fatal(err)
	}
	transition, ok := detail.(*tammyv1.InvalidStateTransitionErrorDetail)
	if !ok || transition.Context == nil || transition.Context.Code != "COMMIT_ALREADY_COMPLETED" ||
		transition.CurrentState != "DESTINATION_COMMITTING" || transition.Resource != "audit_export_job" ||
		transition.Context.Retry != tammyv1.RetryClassification_RETRY_CLASSIFICATION_NEVER {
		t.Fatalf("racing cancellation detail=%#v", detail)
	}
	assertAuditServiceCounts(t, database, 1, 1, 1)
}

func TestAuditServiceReportsReloadedCommitPointAfterCancellationCASLoses(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	seedAuditChain(t, ctx, database, 0)
	service := newAuditServiceFixture(t, database, workspaceID, false)
	exportRequest := exportEvidenceServiceRequest(workspaceID, "01890f60-4d6d-7c12-8f02-6c9129d5b047")
	created, err := service.ExportEvidence(ctx, connect.NewRequest(exportRequest))
	if err != nil {
		t.Fatal(err)
	}
	baseTransactions := service.transactions.(*auditServiceTransactions)
	service.transactions = &interleavingExportLoadTransactions{
		auditServiceTransactions: baseTransactions,
		jobID:                    created.Msg.Job.Id,
		commitElectedAt:          time.Date(2026, 8, 4, 8, 3, 0, 0, time.UTC),
	}
	response, cancelErr := service.CancelAuditExport(ctx, connect.NewRequest(&tammyv1.CancelAuditExportRequest{
		CommandContext: &tammyv1.CommandContext{IdempotencyKey: "01890f60-4d6d-7c12-8f02-6c9129d5b048", Authentication: exportRequest.CommandContext.Authentication},
		JobId:          created.Msg.Job.Id, ExpectedVersion: created.Msg.Job.Version,
	}))
	if response != nil {
		t.Fatalf("racing cancellation returned response=%#v", response)
	}
	if !errors.Is(cancelErr, ErrExportCommitAlreadyCompleted) {
		t.Fatalf("racing cancellation error=%v, want ErrExportCommitAlreadyCompleted", cancelErr)
	}
	connectErr := new(connect.Error)
	if !errors.As(cancelErr, &connectErr) || connectErr.Code() != connect.CodeFailedPrecondition || len(connectErr.Details()) != 1 {
		t.Fatalf("racing cancellation error=%#v", cancelErr)
	}
	detail, err := connectErr.Details()[0].Value()
	if err != nil {
		t.Fatal(err)
	}
	transition, ok := detail.(*tammyv1.InvalidStateTransitionErrorDetail)
	if !ok || transition.Context == nil || transition.Context.Code != "COMMIT_ALREADY_COMPLETED" ||
		transition.CurrentState != "DESTINATION_COMMITTING" || transition.Context.Retry != tammyv1.RetryClassification_RETRY_CLASSIFICATION_NEVER {
		t.Fatalf("racing cancellation detail=%#v", detail)
	}
	unchanged, err := LoadExportJob(ctx, database, created.Msg.Job.Id)
	if err != nil || unchanged.Version != created.Msg.Job.Version || unchanged.State != tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_QUEUED {
		t.Fatalf("failed cancellation persisted interleaving: job=%#v err=%v", unchanged, err)
	}
	assertAuditServiceCounts(t, database, 1, 1, 1)
}

func TestAuditServiceRollsBackJobIdempotencyAndAuditTogether(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	seedAuditChain(t, ctx, database, 0)
	service := newAuditServiceFixture(t, database, workspaceID, true)
	if _, err := service.ExportEvidence(ctx, connect.NewRequest(exportEvidenceServiceRequest(workspaceID,
		"01890f60-4d6d-7c12-8f02-6c9129d5b051"))); !errors.Is(err, errInjectedAfterMutation) {
		t.Fatalf("rollback injection error=%v", err)
	}
	assertAuditServiceCounts(t, database, 0, 0, 0)
	header, err := LoadChainHeader(ctx, database, workspaceID, 1)
	if err != nil || header.CurrentSequence != 0 || header.CurrentHead != header.GenesisHash {
		t.Fatalf("rolled-back head=%#v err=%v", header, err)
	}
}

type rejectingTransactionalAuditAccess struct{}

func (rejectingTransactionalAuditAccess) Require(ctx context.Context, transaction ServiceTransaction,
	_ *tammyv1.AuthenticationContext, _ authorisation.Action) error {
	if transaction == nil || transaction.TransactionID() == "" {
		return errors.New("authorization was not transaction scoped")
	}
	_, err := transaction.ExecContext(ctx, `INSERT INTO workspace_metadata(key, value, revision, updated_at)
		VALUES ('audit.authorization.probe', X'01', 1, '2026-08-04T00:00:00.000000000Z')`)
	if err != nil {
		return err
	}
	return errors.New("denied")
}

func TestAuditServiceAuthorizationRunsInsideAndRollsBackWithCommandUnitOfWork(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	seedAuditChain(t, ctx, database, 0)
	service := newAuditServiceFixture(t, database, workspaceID, false)
	service.access = rejectingTransactionalAuditAccess{}
	if _, err := service.ExportEvidence(ctx, connect.NewRequest(exportEvidenceServiceRequest(workspaceID,
		"01890f60-4d6d-7c12-8f02-6c9129d5b051"))); err == nil {
		t.Fatal("rejected authorization unexpectedly succeeded")
	}
	var count int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM workspace_metadata WHERE key='audit.authorization.probe'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("authorization probe survived rollback: count=%d err=%v", count, err)
	}
	assertAuditServiceCounts(t, database, 0, 0, 0)
}

func TestAuditServiceMapsIdempotencyConflictAndInFlightRetry(t *testing.T) {
	operationKey := "01890f60-4d6d-7c12-8f02-6c9129d5b051"
	if err := mapIdempotencyServiceError(idempotency.ErrConflict, operationKey); !errors.Is(err,
		faults.New(faults.CodeIdempotencyConflict, nil)) {
		t.Fatalf("conflict mapping=%v", err)
	}
	mapped := mapIdempotencyServiceError(idempotency.ErrAborted, operationKey)
	connectErr := new(connect.Error)
	if !errors.As(mapped, &connectErr) || connectErr.Code() != connect.CodeAborted || len(connectErr.Details()) != 1 {
		t.Fatalf("in-flight mapping=%#v", mapped)
	}
	detail, err := connectErr.Details()[0].Value()
	if err != nil {
		t.Fatal(err)
	}
	contextDetail, ok := detail.(*tammyv1.ErrorContext)
	if !ok || contextDetail.Code != "COMMAND_IN_FLIGHT" || contextDetail.Retry != tammyv1.RetryClassification_RETRY_CLASSIFICATION_SAFE {
		t.Fatalf("retry detail=%#v", detail)
	}
}

var errInjectedAfterMutation = errors.New("injected after mutation")

type auditServiceTransactions struct {
	database          *sqlcipher.Database
	workspaceID       string
	failAfterMutation bool
	sqlActive         *bool
}

type interleavingExportLoadTransactions struct {
	*auditServiceTransactions
	jobID           string
	commitElectedAt time.Time
}

func (transactions *interleavingExportLoadTransactions) Mutate(ctx context.Context, mutate func(ServiceTransaction) error) error {
	return transactions.auditServiceTransactions.Mutate(ctx, func(transaction ServiceTransaction) error {
		return mutate(&interleavingExportLoadTransaction{
			ServiceTransaction: transaction,
			jobID:              transactions.jobID,
			commitElectedAt:    transactions.commitElectedAt,
		})
	})
}

type interleavingExportLoadTransaction struct {
	ServiceTransaction
	jobID           string
	commitElectedAt time.Time
	exportLoads     int
}

func (transaction *interleavingExportLoadTransaction) QueryContext(
	ctx context.Context,
	query string,
	arguments ...any,
) (*sql.Rows, error) {
	if strings.HasPrefix(query, exportJobSelect) {
		transaction.exportLoads++
		if transaction.exportLoads == 2 {
			if _, err := transaction.ServiceTransaction.ExecContext(ctx, `UPDATE audit_export_jobs_v1 SET
				state='RUNNING', stage='DESTINATION_COMMITTING', updated_at=?, version=version+1 WHERE id=?`,
				formatTimestamp(transaction.commitElectedAt), transaction.jobID); err != nil {
				return nil, err
			}
		}
	}
	return transaction.ServiceTransaction.QueryContext(ctx, query, arguments...)
}

type auditServiceTransaction struct {
	Executor
	id          string
	afterCommit []func(context.Context) error
}

func (transaction *auditServiceTransaction) TransactionID() string { return transaction.id }
func (transaction *auditServiceTransaction) AfterCommit(callback func(context.Context) error) error {
	transaction.afterCommit = append(transaction.afterCommit, callback)
	return nil
}

func (transactions *auditServiceTransactions) WorkspaceID() string { return transactions.workspaceID }
func (transactions *auditServiceTransactions) Read(ctx context.Context, read func(ServiceTransaction) error) error {
	transaction, err := transactions.database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		return err
	}
	scope := &auditServiceTransaction{Executor: transaction, id: "01890f60-4d6d-7c12-8f02-6c9129d5b070"}
	if transactions.sqlActive != nil {
		*transactions.sqlActive = true
		defer func() { *transactions.sqlActive = false }()
	}
	if err := read(scope); err != nil {
		_ = transaction.Rollback()
		return err
	}
	return transaction.Commit()
}
func (transactions *auditServiceTransactions) Mutate(ctx context.Context, mutate func(ServiceTransaction) error) error {
	transaction, err := transactions.database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	scope := &auditServiceTransaction{Executor: transaction, id: "01890f60-4d6d-7c12-8f02-6c9129d5b071"}
	if transactions.sqlActive != nil {
		*transactions.sqlActive = true
		defer func() { *transactions.sqlActive = false }()
	}
	if err := mutate(scope); err != nil {
		_ = transaction.Rollback()
		return err
	}
	if transactions.failAfterMutation {
		_ = transaction.Rollback()
		return errInjectedAfterMutation
	}
	if err := transaction.Commit(); err != nil {
		return err
	}
	for _, callback := range scope.afterCommit {
		if err := callback(ctx); err != nil {
			return err
		}
	}
	return nil
}

type auditServiceAccess struct{}

func (auditServiceAccess) Require(_ context.Context, transaction ServiceTransaction, authentication *tammyv1.AuthenticationContext, action authorisation.Action) error {
	if transaction == nil || transaction.TransactionID() == "" || authentication == nil || authentication.ActorUserId == "" ||
		(action != authorisation.ActionReadAudit && action != authorisation.ActionExportAudit) {
		return errors.New("denied")
	}
	return nil
}

func newAuditServiceFixture(t *testing.T, database *sqlcipher.Database, workspaceID string, failAfterMutation bool) *Service {
	t.Helper()
	now := time.Date(2026, 8, 4, 8, 0, 0, 0, time.UTC)
	observer, err := idempotency.NewObserver(database)
	if err != nil {
		t.Fatal(err)
	}
	elector, err := idempotency.NewElector(idempotency.Config{Clock: clock.Func(func() time.Time { return now }), Observe: observer})
	if err != nil {
		t.Fatal(err)
	}
	cursors, err := paging.NewCodec(bytes.Repeat([]byte{0x65}, 32))
	if err != nil {
		t.Fatal(err)
	}
	identifiers := []string{
		"01890f60-4d6d-7c12-8f02-6c9129d5b061", "01890f60-4d6d-7c12-8f02-6c9129d5b062",
		"01890f60-4d6d-7c12-8f02-6c9129d5b063", "01890f60-4d6d-7c12-8f02-6c9129d5b064",
		"01890f60-4d6d-7c12-8f02-6c9129d5b065", "01890f60-4d6d-7c12-8f02-6c9129d5b066",
	}
	gate := NewWriteGate()
	gate.set(true, true)
	header, err := LoadChainHeader(context.Background(), database, workspaceID, 0)
	if err != nil {
		t.Fatal(err)
	}
	baseline := &tammyv1.AuditMirrorBaseline{WorkspaceId: workspaceID, Generation: header.Generation,
		Sequence: header.CurrentSequence, Head: append([]byte(nil), header.CurrentHead[:]...)}
	appender, err := NewMirroringAppender(&memoryMirrorStore{baseline: baseline}, gate)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(ServiceConfig{Access: auditServiceAccess{},
		Transactions: &auditServiceTransactions{database: database, workspaceID: workspaceID, failAfterMutation: failAfterMutation},
		Elector:      elector, Clock: clock.Func(func() time.Time { return now }),
		NewID:   func() (string, error) { id := identifiers[0]; identifiers = identifiers[1:]; return id, nil },
		Cursors: cursors, SchemaFingerprint: bytes.Repeat([]byte{0x44}, 32), Appender: appender})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func seedAuditChain(t *testing.T, ctx context.Context, database *sqlcipher.Database, eventCount int) {
	t.Helper()
	transaction, err := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	salt := bytes.Repeat([]byte{0x23}, 32)
	genesis, _ := Genesis("01890f60-4d6d-7c12-8f02-6c9129d5b001", salt)
	if err := InitializeChain(ctx, transaction, ChainHeader{WorkspaceID: "01890f60-4d6d-7c12-8f02-6c9129d5b001",
		Generation: 1, ChainSalt: salt, GenesisHash: genesis, CurrentHead: genesis, CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	eventIDs := []string{"01890f60-4d6d-7c12-8f02-6c9129d5b071", "01890f60-4d6d-7c12-8f02-6c9129d5b072"}
	for index := range eventCount {
		event, payload := integrationAuditEvent(eventIDs[index])
		if _, err := appendStoredEventForTest(ctx, transaction, event, payload); err != nil {
			t.Fatal(err)
		}
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
}

func seedLargeVerifiedAuditChain(
	t *testing.T,
	database *sqlcipher.Database,
	eventCount int,
	matchingActor string,
	matchingStride int,
) {
	t.Helper()
	if eventCount <= 0 || matchingStride <= 0 {
		t.Fatal("large audit fixture requires positive bounds")
	}
	ctx := context.Background()
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	salt := bytes.Repeat([]byte{0x23}, sha256.Size)
	genesis, err := Genesis(workspaceID, salt)
	if err != nil {
		t.Fatal(err)
	}
	events := make([]StoredEvent, 0, eventCount)
	previous := genesis
	for index := 1; index <= eventCount; index++ {
		stored := verifierEvent(uint64(index), previous)
		stored.Event.Id = fmt.Sprintf("01890f60-4d6d-7c12-8f02-%012x", index)
		if index%matchingStride == 0 {
			stored.Event.Actor.ActorUserId = matchingActor
		} else {
			stored.Event.Actor.ActorUserId = "01890f60-4d6d-7c12-8f02-6c9129d5b099"
		}
		stored, err = reconstructEventWithStoredOpenings(previous, stored.Event, stored.PayloadProto)
		if err != nil {
			t.Fatalf("prepare event %d: %v", index, err)
		}
		events = append(events, stored)
		copy(previous[:], stored.Event.EventHash)
	}
	transaction, err := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	if err := InitializeChain(ctx, transaction, ChainHeader{WorkspaceID: workspaceID, Generation: 1,
		ChainSalt: salt, GenesisHash: genesis, CurrentSequence: uint64(eventCount), CurrentHead: previous,
		CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)}); err != nil {
		_ = transaction.Rollback()
		t.Fatal(err)
	}
	for index := range events {
		stored := events[index]
		if _, err := transaction.ExecContext(ctx, `INSERT INTO audit_events_v1(
			workspace_id, generation, sequence, event_id, event_type, occurred_at, actor_user_id, session_id,
			command_type, affected_resources_proto, payload_type, payload_schema_fingerprint, payload_proto,
			payload_json, canonical_event, event_proto, previous_hash, event_hash
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, stored.Event.WorkspaceId,
			stored.Event.Generation, stored.Event.Sequence, stored.Event.Id, int32(stored.Event.Type),
			formatTimestamp(stored.Event.OccurredAt.AsTime()), stored.Event.Actor.ActorUserId, stored.Event.Actor.SessionId,
			stored.Event.CommandType, stored.AffectedResourcesProto, stored.PayloadType,
			stored.Event.PayloadSchemaFingerprint, stored.PayloadProto, stored.PayloadJSON, stored.CanonicalEvent,
			stored.EventProto, stored.Event.PreviousHash, stored.Event.EventHash); err != nil {
			_ = transaction.Rollback()
			t.Fatalf("insert event %d: %v", index+1, err)
		}
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
}

func exportEvidenceServiceRequest(workspaceID, operationKey string) *tammyv1.ExportEvidenceRequest {
	return &tammyv1.ExportEvidenceRequest{CommandContext: &tammyv1.CommandContext{IdempotencyKey: operationKey,
		Authentication: &tammyv1.AuthenticationContext{ActorUserId: "01890f60-4d6d-7c12-8f02-6c9129d5b006", SessionId: "01890f60-4d6d-7c12-8f02-6c9129d5b007"}},
		WorkspaceId: workspaceID, Filter: &tammyv1.AuditEventFilter{}, Destination: &tammyv1.ApprovedFileRef{CapabilityId: "approved-file-capability"}}
}

func assertAuditServiceCounts(t *testing.T, database *sqlcipher.Database, events, commands, jobs int) {
	t.Helper()
	for table, want := range map[string]int{"audit_events_v1": events, "command_idempotency_v1": commands, "audit_export_jobs_v1": jobs} {
		var got int
		if err := database.QueryRowContext(context.Background(), `SELECT count(*) FROM `+table).Scan(&got); err != nil || got != want {
			t.Fatalf("%s count=%d want=%d err=%v", table, got, want, err)
		}
	}
}
