package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type countingExecutor struct {
	execCalls int
}

func (executor *countingExecutor) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	executor.execCalls++
	return nil, errors.New("unexpected SQL execution")
}

func (*countingExecutor) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, errors.New("unexpected SQL query")
}

func TestInitializeChainRejectsOutOfRangeTimestampBeforeSQL(t *testing.T) {
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	salt := bytes.Repeat([]byte{0x23}, sha256.Size)
	genesis, err := Genesis(workspaceID, salt)
	if err != nil {
		t.Fatal(err)
	}
	executor := &countingExecutor{}
	err = InitializeChain(context.Background(), executor, ChainHeader{
		WorkspaceID: workspaceID, Generation: 1, ChainSalt: salt, GenesisHash: genesis,
		CurrentHead: genesis, CreatedAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if !errors.Is(err, ErrInvalidChainInput) {
		t.Fatalf("InitializeChain error=%v, want ErrInvalidChainInput", err)
	}
	if executor.execCalls != 0 {
		t.Fatalf("InitializeChain executed %d SQL statements", executor.execCalls)
	}
}

type pageQueryExecutor struct {
	queries []string
	args    [][]any
}

type fakeStoredEventRows struct {
	remaining  int
	scan       func(...any) error
	rowsErr    error
	closeErr   error
	onExhaust  func()
	exhausted  bool
	closed     bool
	scanCalled int
}

func (rows *fakeStoredEventRows) Next() bool {
	if rows.remaining > 0 {
		rows.remaining--
		return true
	}
	if !rows.exhausted {
		rows.exhausted = true
		if rows.onExhaust != nil {
			rows.onExhaust()
		}
	}
	return false
}

func (rows *fakeStoredEventRows) Scan(destinations ...any) error {
	rows.scanCalled++
	if rows.scan != nil {
		return rows.scan(destinations...)
	}
	return nil
}

func (rows *fakeStoredEventRows) Err() error   { return rows.rowsErr }
func (rows *fakeStoredEventRows) Close() error { rows.closed = true; return rows.closeErr }

func (*pageQueryExecutor) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errors.New("unexpected SQL execution")
}

func (executor *pageQueryExecutor) QueryContext(_ context.Context, query string, arguments ...any) (*sql.Rows, error) {
	executor.queries = append(executor.queries, query)
	executor.args = append(executor.args, append([]any(nil), arguments...))
	return nil, errors.New("stop after query capture")
}

func TestLoadStoredEventPageUsesBoundedSnapshotKeysetQuery(t *testing.T) {
	executor := &pageQueryExecutor{}
	var endHead [sha256.Size]byte
	endHead[0] = 0x42
	_, err := LoadStoredEventPage(context.Background(), executor, StoredEventSnapshot{
		WorkspaceID: "01890f60-4d6d-7c12-8f02-6c9129d5b001",
		Generation:  7,
		EndSequence: 23,
		EndHead:     endHead,
	}, StoredEventCheckpoint{AfterSequence: 11, Head: sha256.Sum256([]byte("checkpoint"))}, 9, StoredEventPageByteBudget)
	if !errors.Is(err, ErrRepository) {
		t.Fatalf("LoadStoredEventPage error=%v, want ErrRepository", err)
	}
	if len(executor.queries) != 1 {
		t.Fatalf("queries=%d, want one bounded metadata query", len(executor.queries))
	}
	query := strings.Join(strings.Fields(executor.queries[0]), " ")
	for _, fragment := range []string{
		"workspace_id = ? AND generation = ?",
		"sequence > ? AND sequence <= ?",
		"length(payload_type) BETWEEN 1 AND 256",
		"length(payload_proto) <= ?",
		"length(payload_json) <= ?",
		"length(affected_resources_proto) <= ?",
		"length(canonical_event) <= ?",
		"length(event_proto) <= ?",
		"ORDER BY sequence LIMIT ?",
	} {
		if !strings.Contains(query, fragment) {
			t.Fatalf("bounded keyset query missing %q: %s", fragment, query)
		}
	}
	arguments := executor.args[0]
	if len(arguments) < 5 || arguments[0] != "01890f60-4d6d-7c12-8f02-6c9129d5b001" ||
		arguments[1] != uint64(7) || arguments[2] != uint64(11) || arguments[3] != uint64(23) ||
		arguments[len(arguments)-1] != 9 {
		t.Fatalf("bounded keyset arguments=%#v", arguments)
	}
}

func TestLoadStoredEventPageRejectsInvalidBoundsBeforeSQL(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		snapshot   StoredEventSnapshot
		checkpoint StoredEventCheckpoint
		pageSize   uint32
		byteBudget uint64
	}{
		{name: "empty workspace", snapshot: StoredEventSnapshot{Generation: 1, EndSequence: 1}, pageSize: 1, byteBudget: 1},
		{name: "zero generation", snapshot: StoredEventSnapshot{WorkspaceID: "workspace", EndSequence: 1}, pageSize: 1, byteBudget: 1},
		{name: "checkpoint beyond end", snapshot: StoredEventSnapshot{WorkspaceID: "workspace", Generation: 1, EndSequence: 1}, checkpoint: StoredEventCheckpoint{AfterSequence: 2}, pageSize: 1, byteBudget: 1},
		{name: "zero page size", snapshot: StoredEventSnapshot{WorkspaceID: "workspace", Generation: 1, EndSequence: 1}, byteBudget: 1},
		{name: "oversize page", snapshot: StoredEventSnapshot{WorkspaceID: "workspace", Generation: 1, EndSequence: 1}, pageSize: StoredEventPageSizeLimit + 1, byteBudget: 1},
		{name: "zero byte budget", snapshot: StoredEventSnapshot{WorkspaceID: "workspace", Generation: 1, EndSequence: 1}, pageSize: 1},
		{name: "oversize byte budget", snapshot: StoredEventSnapshot{WorkspaceID: "workspace", Generation: 1, EndSequence: 1}, pageSize: 1, byteBudget: StoredEventPageByteBudget + 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			executor := &pageQueryExecutor{}
			if _, err := LoadStoredEventPage(context.Background(), executor, testCase.snapshot, testCase.checkpoint,
				testCase.pageSize, testCase.byteBudget); !errors.Is(err, ErrRepository) {
				t.Fatalf("LoadStoredEventPage error=%v, want ErrRepository", err)
			}
			if len(executor.queries) != 0 {
				t.Fatalf("invalid bounds executed %d queries", len(executor.queries))
			}
		})
	}
}

func TestLoadStoredEventsRequiresExplicitBoundedRangeBeforeSQL(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		start uint64
		end   uint64
	}{
		{name: "unbounded zero range"},
		{name: "range exceeds page limit", start: 1, end: uint64(StoredEventPageSizeLimit) + 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			executor := &pageQueryExecutor{}
			if _, err := LoadStoredEvents(context.Background(), executor, "workspace", 1, testCase.start, testCase.end); !errors.Is(err, ErrRepository) {
				t.Fatalf("LoadStoredEvents error=%v, want ErrRepository", err)
			}
			if len(executor.queries) != 0 {
				t.Fatalf("invalid range executed %d SQL queries", len(executor.queries))
			}
		})
	}

	executor := &pageQueryExecutor{}
	const start, end = uint64(9), uint64(12)
	if _, err := LoadStoredEvents(context.Background(), executor, "workspace", 7, start, end); !errors.Is(err, ErrRepository) {
		t.Fatalf("captured bounded LoadStoredEvents error=%v, want ErrRepository", err)
	}
	if len(executor.queries) != 1 {
		t.Fatalf("bounded range queries=%d, want 1", len(executor.queries))
	}
	query := strings.Join(strings.Fields(executor.queries[0]), " ")
	if !strings.Contains(query, "sequence >= ? AND sequence <= ? ORDER BY sequence LIMIT ?") {
		t.Fatalf("bounded range query is missing exact range limit: %s", query)
	}
	wantCount := int(end - start + 1)
	arguments := executor.args[0]
	if len(arguments) != 5 || arguments[0] != "workspace" || arguments[1] != uint64(7) ||
		arguments[2] != start || arguments[3] != end || arguments[4] != wantCount {
		t.Fatalf("bounded range arguments=%#v, want exact count %d", arguments, wantCount)
	}
}

func TestStoredEventMetadataRowsCloseAndPropagateScanRowsContextAndCloseErrors(t *testing.T) {
	injected := errors.New("injected rows failure")
	for _, testCase := range []struct {
		name      string
		configure func(context.CancelFunc, *fakeStoredEventRows)
	}{
		{name: "scan", configure: func(_ context.CancelFunc, rows *fakeStoredEventRows) {
			rows.remaining, rows.scan = 1, func(...any) error { return injected }
		}},
		{name: "rows err", configure: func(_ context.CancelFunc, rows *fakeStoredEventRows) { rows.rowsErr = injected }},
		{name: "context after last row", configure: func(cancel context.CancelFunc, rows *fakeStoredEventRows) {
			rows.onExhaust = cancel
		}},
		{name: "close", configure: func(_ context.CancelFunc, rows *fakeStoredEventRows) { rows.closeErr = injected }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			rows := &fakeStoredEventRows{}
			testCase.configure(cancel, rows)
			_, _, err := selectStoredEventPageRows(ctx, rows, 0, 1, 1, StoredEventPageByteBudget)
			if err == nil {
				t.Fatal("row failure was accepted")
			}
			if !rows.closed {
				t.Fatal("metadata rows were not closed")
			}
		})
	}
}

func TestStoredEventMetadataRowsRejectOverproducedCountBeforeCopy(t *testing.T) {
	sequence := uint64(0)
	rows := &fakeStoredEventRows{remaining: 3, scan: func(destinations ...any) error {
		sequence++
		*destinations[0].(*uint64) = sequence
		*destinations[1].(*int64) = 1
		for index := 2; index < len(destinations); index++ {
			*destinations[index].(*int64) = 0
		}
		return nil
	}}
	if _, _, err := selectStoredEventPageRows(context.Background(), rows, 0, 3, 2,
		StoredEventPageByteBudget); !errors.Is(err, ErrRepository) || !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("overproduced full-page rows error=%v, want repository corruption", err)
	}
	if !rows.closed {
		t.Fatal("overproduced full-page rows not closed")
	}
}

func TestStoredEventDataRowsCloseOnDecodeAndRowsErr(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		rowsErr error
	}{
		{name: "decode"},
		{name: "rows err", rowsErr: errors.New("injected rows error")},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			remaining := 1
			if testCase.rowsErr != nil {
				remaining = 0
			}
			rows := &fakeStoredEventRows{remaining: remaining, rowsErr: testCase.rowsErr}
			rows.scan = func(destinations ...any) error {
				*destinations[0].(*uint64) = 1
				*destinations[1].(*string) = "x"
				*destinations[2].(*[]byte) = nil
				*destinations[3].(*[]byte) = nil
				*destinations[4].(*[]byte) = nil
				*destinations[5].(*[]byte) = nil
				*destinations[6].(*[]byte) = []byte{0xff}
				return nil
			}
			var head [sha256.Size]byte
			head[0] = 1
			_, _, err := scanStoredEventPageRows(context.Background(), rows,
				StoredEventSnapshot{WorkspaceID: "workspace", Generation: 1, EndSequence: 1, EndHead: head},
				StoredEventCheckpoint{Head: head}, []storedEventPageRow{{sequence: 1, bytes: 2}}, 2)
			if err == nil {
				t.Fatal("malformed data row was accepted")
			}
			if testCase.rowsErr != nil && !strings.Contains(err.Error(), "list event page bytes") {
				t.Fatalf("rows.Err classification=%v", err)
			}
			if !rows.closed {
				t.Fatal("data rows were not closed")
			}
		})
	}
}

func TestLoadMatchingAuditEventPagePushesCanonicalSparseFilterAndBoundedEventProtoOnly(t *testing.T) {
	executor := &pageQueryExecutor{}
	actor := "01890f60-4d6d-7c12-8f02-6c9129d5b006"
	start, end := uint64(10), uint64(190)
	from := time.Date(2026, 8, 4, 1, 2, 3, 0, time.UTC)
	to := from.Add(2 * time.Hour)
	var endHead [sha256.Size]byte
	endHead[0] = 0x42
	_, err := LoadMatchingAuditEventPage(context.Background(), executor, StoredEventSnapshot{
		WorkspaceID: "01890f60-4d6d-7c12-8f02-6c9129d5b001", Generation: 7, EndSequence: 200, EndHead: endHead,
	}, &tammyv1.AuditEventFilter{
		EventTypes: []tammyv1.AuditEventType{
			tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_STATE_CHANGED,
			tammyv1.AuditEventType_AUDIT_EVENT_TYPE_USER_STATE_CHANGED,
		},
		ActorUserId: &actor, FromTime: timestamppb.New(from), ToTime: timestamppb.New(to),
		StartSequence: &start, EndSequence: &end,
	}, 30, 3)
	if !errors.Is(err, ErrRepository) {
		t.Fatalf("LoadMatchingAuditEventPage error=%v, want ErrRepository", err)
	}
	if len(executor.queries) != 1 {
		t.Fatalf("queries=%d, want one matching query", len(executor.queries))
	}
	query := strings.Join(strings.Fields(executor.queries[0]), " ")
	for _, fragment := range []string{
		"SELECT sequence, CASE WHEN length(event_proto) <= ? THEN length(event_proto) ELSE -1 END",
		"workspace_id = ? AND generation = ?",
		"sequence > ? AND sequence <= ?",
		"sequence >= ? AND sequence <= ?",
		"actor_user_id = ?",
		"occurred_at >= ? AND occurred_at <= ?",
		"event_type IN (?, ?)",
		"length(event_proto) <= ?",
		"ORDER BY sequence LIMIT ?",
	} {
		if !strings.Contains(query, fragment) {
			t.Fatalf("matching query missing %q: %s", fragment, query)
		}
	}
	for _, forbidden := range []string{"payload_proto", "payload_json", "canonical_event", "affected_resources_proto", "SELECT sequence, event_proto"} {
		if strings.Contains(query, forbidden) {
			t.Fatalf("matching query selected retained payload column %q: %s", forbidden, query)
		}
	}
	arguments := executor.args[0]
	if arguments[0] != uint64(StoredEventRowByteLimit) || arguments[1] != "01890f60-4d6d-7c12-8f02-6c9129d5b001" ||
		arguments[2] != uint64(7) || arguments[3] != uint64(30) || arguments[4] != uint64(200) ||
		arguments[len(arguments)-1] != 4 {
		t.Fatalf("matching query arguments=%#v", arguments)
	}
}

func TestLoadMatchingAuditEventPageRejectsNoncanonicalOrUnboundedInputBeforeSQL(t *testing.T) {
	var endHead [sha256.Size]byte
	endHead[0] = 1
	snapshot := StoredEventSnapshot{WorkspaceID: "workspace", Generation: 1, EndSequence: 20, EndHead: endHead}
	for _, testCase := range []struct {
		name     string
		filter   *tammyv1.AuditEventFilter
		after    uint64
		pageSize uint32
	}{
		{name: "nil filter", pageSize: 1},
		{name: "after snapshot", filter: &tammyv1.AuditEventFilter{}, after: 21, pageSize: 1},
		{name: "zero page", filter: &tammyv1.AuditEventFilter{}},
		{name: "oversize page", filter: &tammyv1.AuditEventFilter{}, pageSize: 201},
		{name: "noncanonical actor", filter: &tammyv1.AuditEventFilter{ActorUserId: proto.String("actor")}, pageSize: 1},
		{name: "unsorted event types", filter: &tammyv1.AuditEventFilter{EventTypes: []tammyv1.AuditEventType{
			tammyv1.AuditEventType_AUDIT_EVENT_TYPE_USER_STATE_CHANGED,
			tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_STATE_CHANGED,
		}}, pageSize: 1},
		{name: "duplicate event types", filter: &tammyv1.AuditEventFilter{EventTypes: []tammyv1.AuditEventType{
			tammyv1.AuditEventType_AUDIT_EVENT_TYPE_USER_STATE_CHANGED,
			tammyv1.AuditEventType_AUDIT_EVENT_TYPE_USER_STATE_CHANGED,
		}}, pageSize: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			executor := &pageQueryExecutor{}
			if _, err := LoadMatchingAuditEventPage(context.Background(), executor, snapshot, testCase.filter,
				testCase.after, testCase.pageSize); !errors.Is(err, ErrRepository) {
				t.Fatalf("error=%v, want ErrRepository", err)
			}
			if len(executor.queries) != 0 {
				t.Fatalf("invalid matching input executed %d queries", len(executor.queries))
			}
		})
	}
}

func TestMatchingAuditEventLengthRowsRejectZeroAndOverproducedRowsBeforeCopy(t *testing.T) {
	t.Run("zero length", func(t *testing.T) {
		rows := &fakeStoredEventRows{remaining: 1, scan: func(destinations ...any) error {
			*destinations[0].(*uint64) = 1
			*destinations[1].(*int64) = 0
			return nil
		}}
		if _, _, err := scanMatchingAuditEventLengths(context.Background(), rows, 2); !errors.Is(err, ErrRepository) ||
			!errors.Is(err, ErrInvalidEvent) {
			t.Fatalf("zero length error=%v, want repository corruption", err)
		}
		if !rows.closed {
			t.Fatal("zero-length rows not closed")
		}
	})
	t.Run("overproduced", func(t *testing.T) {
		sequence := uint64(0)
		rows := &fakeStoredEventRows{remaining: 4, scan: func(destinations ...any) error {
			sequence++
			*destinations[0].(*uint64) = sequence
			*destinations[1].(*int64) = 1
			return nil
		}}
		if _, _, err := scanMatchingAuditEventLengths(context.Background(), rows, 2); !errors.Is(err, ErrRepository) ||
			!errors.Is(err, ErrInvalidEvent) {
			t.Fatalf("overproduced rows error=%v, want repository corruption", err)
		}
		if !rows.closed {
			t.Fatal("overproduced rows not closed")
		}
	})
}

func TestMatchingAuditEventBytesRejectNoncanonicalRetainedProto(t *testing.T) {
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	event := &tammyv1.AuditEvent{WorkspaceId: workspaceID, Generation: 1, Sequence: 1,
		Type:       tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_STATE_CHANGED,
		OccurredAt: timestamppb.New(time.Date(2026, 8, 4, 1, 2, 3, 0, time.UTC))}
	canonical, err := proto.MarshalOptions{Deterministic: true}.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	noncanonical := protowire.AppendTag(nil, 4, protowire.VarintType)
	noncanonical = protowire.AppendVarint(noncanonical, 1)
	noncanonical = append(noncanonical, canonical...)
	rows := &fakeStoredEventRows{remaining: 1, scan: func(destinations ...any) error {
		*destinations[0].(*uint64) = 1
		*destinations[1].(*[]byte) = noncanonical
		return nil
	}}
	var endHead [sha256.Size]byte
	endHead[0] = 1
	_, err = scanMatchingAuditEventBytes(context.Background(), rows,
		StoredEventSnapshot{WorkspaceID: workspaceID, Generation: 1, EndSequence: 1, EndHead: endHead},
		&tammyv1.AuditEventFilter{}, 0, []matchingAuditEventRow{{sequence: 1, bytes: uint64(len(noncanonical))}})
	if !errors.Is(err, ErrRepository) {
		t.Fatalf("noncanonical event proto error=%v, want ErrRepository", err)
	}
	if !rows.closed {
		t.Fatal("noncanonical event rows not closed")
	}
}
