//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package audit

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

type pagingQueryExecutor struct {
	Executor
	queries []string
	args    [][]any
}

func (executor *pagingQueryExecutor) QueryContext(ctx context.Context, query string, arguments ...any) (*sql.Rows, error) {
	executor.queries = append(executor.queries, query)
	executor.args = append(executor.args, append([]any(nil), arguments...))
	return executor.Executor.QueryContext(ctx, query, arguments...)
}

func TestStoredEventPageOversizedCheckBypassStopsBeforeBlobQuery(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	header := seedSyntheticPagingChain(t, database, 1)
	if _, err := database.ExecContext(ctx, `DROP TRIGGER audit_events_v1_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE audit_events_v1 SET payload_proto=zeroblob(?) WHERE sequence=1`,
		StoredEventRowByteLimit+1); err != nil {
		t.Fatal(err)
	}
	counting := &pagingQueryExecutor{Executor: database}
	_, err := LoadStoredEventPage(ctx, counting, snapshotForHeader(header),
		StoredEventCheckpoint{Head: header.GenesisHash}, 10, StoredEventPageByteBudget)
	if !errors.Is(err, ErrRepository) || !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("oversized row error=%v, want repository corruption", err)
	}
	if len(counting.queries) != 1 || strings.Contains(counting.queries[0], "SELECT sequence, payload_type, payload_proto") {
		t.Fatalf("oversized row materialized; queries=%q", counting.queries)
	}
}

func TestMatchingAuditEventPageOversizedFirstMatchStopsAfterMetadataQuery(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	header := seedSyntheticPagingChain(t, database, 1)
	if _, err := database.ExecContext(ctx, `DROP TRIGGER audit_events_v1_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE audit_events_v1 SET event_proto=zeroblob(?) WHERE sequence=1`,
		StoredEventRowByteLimit+1); err != nil {
		t.Fatal(err)
	}
	counting := &pagingQueryExecutor{Executor: database}
	_, err := LoadMatchingAuditEventPage(ctx, counting, snapshotForHeader(header), &tammyv1.AuditEventFilter{}, 0, 1)
	if !errors.Is(err, ErrRepository) || !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("oversized matching row error=%v, want repository corruption", err)
	}
	if len(counting.queries) != 1 || !strings.Contains(counting.queries[0], "CASE WHEN length(event_proto) <= ?") ||
		strings.Contains(counting.queries[0], "SELECT sequence, event_proto") {
		t.Fatalf("oversized matching row reached byte query; queries=%q", counting.queries)
	}
}

func TestStoredEventPageExcludesAppendBeyondFixedSnapshot(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	header := seedSyntheticPagingChain(t, database, 3)
	secondHead := syntheticPagingHash(2)
	snapshot := StoredEventSnapshot{WorkspaceID: header.WorkspaceID, Generation: header.Generation,
		EndSequence: 2, EndHead: secondHead}
	page, err := LoadStoredEventPage(ctx, database, snapshot, StoredEventCheckpoint{Head: header.GenesisHash},
		StoredEventPageSizeLimit, StoredEventPageByteBudget)
	if err != nil {
		t.Fatal(err)
	}
	if page.HasMore || len(page.Events) != 2 || page.Checkpoint.AfterSequence != 2 || page.Checkpoint.Head != secondHead {
		t.Fatalf("fixed snapshot page=%#v", page)
	}
}

func TestStoredEventPageRejectsInPrefixGapAndMutation(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(context.Context, *testing.T, Executor)
	}{
		{name: "gap", mutate: func(ctx context.Context, t *testing.T, executor Executor) {
			if _, err := executor.ExecContext(ctx, `DROP TRIGGER audit_events_v1_no_delete`); err != nil {
				t.Fatal(err)
			}
			if _, err := executor.ExecContext(ctx, `DELETE FROM audit_events_v1 WHERE sequence=2`); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "mutation", mutate: func(ctx context.Context, t *testing.T, executor Executor) {
			if _, err := executor.ExecContext(ctx, `DROP TRIGGER audit_events_v1_no_update`); err != nil {
				t.Fatal(err)
			}
			if _, err := executor.ExecContext(ctx, `UPDATE audit_events_v1 SET event_proto=x'ff' WHERE sequence=2`); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			database := newEncryptedAuditDatabase(t)
			header := seedSyntheticPagingChain(t, database, 3)
			testCase.mutate(ctx, t, database)
			_, err := LoadStoredEventPage(ctx, database, snapshotForHeader(header),
				StoredEventCheckpoint{Head: header.GenesisHash}, 10, StoredEventPageByteBudget)
			if !errors.Is(err, ErrRepository) || !errors.Is(err, ErrInvalidEvent) {
				t.Fatalf("corrupt prefix error=%v, want repository corruption", err)
			}
		})
	}
}

func TestLoadStoredEventsRejectsMissingOrNoncanonicalExplicitRange(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(context.Context, *testing.T, Executor)
	}{
		{name: "gap", mutate: func(ctx context.Context, t *testing.T, executor Executor) {
			if _, err := executor.ExecContext(ctx, `DROP TRIGGER audit_events_v1_no_delete`); err != nil {
				t.Fatal(err)
			}
			if _, err := executor.ExecContext(ctx, `DELETE FROM audit_events_v1 WHERE sequence=2`); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "event sequence mismatch", mutate: func(ctx context.Context, t *testing.T, executor Executor) {
			if _, err := executor.ExecContext(ctx, `DROP TRIGGER audit_events_v1_no_update`); err != nil {
				t.Fatal(err)
			}
			if _, err := executor.ExecContext(ctx, `UPDATE audit_events_v1
				SET event_proto=(SELECT event_proto FROM audit_events_v1 WHERE sequence=1)
				WHERE sequence=2`); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			database := newEncryptedAuditDatabase(t)
			header := seedSyntheticPagingChain(t, database, 3)
			testCase.mutate(ctx, t, database)
			if _, err := LoadStoredEvents(ctx, database, header.WorkspaceID, header.Generation, 1, 3); !errors.Is(err, ErrRepository) {
				t.Fatalf("corrupt explicit range error=%v, want ErrRepository", err)
			}
		})
	}
}

func TestStoredEventPageRejectsWrongSnapshotTerminalAsCorruption(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	header := seedSyntheticPagingChain(t, database, 1)
	snapshot := snapshotForHeader(header)
	snapshot.EndHead[0] ^= 0xff
	_, err := LoadStoredEventPage(ctx, database, snapshot, StoredEventCheckpoint{Head: header.GenesisHash},
		1, StoredEventPageByteBudget)
	if !errors.Is(err, ErrRepository) || !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("terminal mismatch error=%v, want repository corruption", err)
	}
}

func TestStoredEventPageWalksTenThousandRowsWithinCountAndByteCeilings(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	header := seedSyntheticPagingChain(t, database, 10_000)
	counting := &pagingQueryExecutor{Executor: database}
	snapshot := snapshotForHeader(header)
	checkpoint := StoredEventCheckpoint{Head: header.GenesisHash}
	expected := uint64(1)
	for {
		page, err := LoadStoredEventPage(ctx, counting, snapshot, checkpoint, 127, StoredEventPageByteBudget)
		if err != nil {
			t.Fatalf("page after %d: %v", checkpoint.AfterSequence, err)
		}
		if len(page.Events) == 0 || len(page.Events) > 127 {
			t.Fatalf("page count=%d", len(page.Events))
		}
		var pageBytes uint64
		for _, stored := range page.Events {
			if stored.Event.Sequence != expected {
				t.Fatalf("sequence=%d, want %d", stored.Event.Sequence, expected)
			}
			expected++
			pageBytes += uint64(len(stored.PayloadType) + len(stored.PayloadProto) + len(stored.PayloadJSON) +
				len(stored.AffectedResourcesProto) + len(stored.CanonicalEvent) + len(stored.EventProto))
		}
		if pageBytes > StoredEventPageByteBudget {
			t.Fatalf("page bytes=%d", pageBytes)
		}
		checkpoint = page.Checkpoint
		if !page.HasMore {
			break
		}
	}
	if expected != 10_001 || checkpoint.AfterSequence != 10_000 || checkpoint.Head != header.CurrentHead {
		t.Fatalf("terminal expected=%d checkpoint=%#v", expected, checkpoint)
	}
	for index, arguments := range counting.args {
		limit, ok := arguments[len(arguments)-1].(int)
		if !ok || limit > int(StoredEventPageSizeLimit) {
			t.Fatalf("query %d LIMIT=%#v", index, arguments[len(arguments)-1])
		}
	}
}

func seedSyntheticPagingChain(t *testing.T, executor Executor, count uint64) ChainHeader {
	t.Helper()
	ctx := context.Background()
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	salt := make([]byte, sha256.Size)
	for index := range salt {
		salt[index] = 0x23
	}
	genesis, err := Genesis(workspaceID, salt)
	if err != nil {
		t.Fatal(err)
	}
	head := genesis
	if count != 0 {
		head = syntheticPagingHash(count)
	}
	if err := InitializeChain(ctx, executor, ChainHeader{WorkspaceID: workspaceID, Generation: 1,
		ChainSalt: salt, GenesisHash: genesis, CurrentSequence: count, CurrentHead: head,
		CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	previous := genesis
	for sequence := uint64(1); sequence <= count; sequence++ {
		eventHash := syntheticPagingHash(sequence)
		event := &tammyv1.AuditEvent{WorkspaceId: workspaceID, Generation: 1, Sequence: sequence,
			PreviousHash: append([]byte(nil), previous[:]...), EventHash: append([]byte(nil), eventHash[:]...)}
		eventProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := executor.ExecContext(ctx, `INSERT INTO audit_events_v1(
			workspace_id, generation, sequence, event_id, event_type, occurred_at, command_type,
			payload_type, payload_schema_fingerprint, payload_proto, payload_json, canonical_event,
			event_proto, previous_hash, event_hash
		) VALUES (?, 1, ?, ?, 1, '2026-08-05T01:00:00.000000000Z', 'synthetic.command',
			'synthetic.payload', zeroblob(32), x'01', x'01', x'01', ?, ?, ?)`, workspaceID, sequence,
			fmt.Sprintf("01890f60-4d6d-7c12-8f02-%012x", sequence), eventProto, previous[:], eventHash[:]); err != nil {
			t.Fatalf("insert synthetic event %d: %v", sequence, err)
		}
		previous = eventHash
	}
	return ChainHeader{WorkspaceID: workspaceID, Generation: 1, ChainSalt: salt, GenesisHash: genesis,
		CurrentSequence: count, CurrentHead: head, CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)}
}

func syntheticPagingHash(sequence uint64) [sha256.Size]byte {
	return sha256.Sum256([]byte(fmt.Sprintf("tammy-synthetic-paging/%d", sequence)))
}

func snapshotForHeader(header ChainHeader) StoredEventSnapshot {
	return StoredEventSnapshot{WorkspaceID: header.WorkspaceID, Generation: header.Generation,
		EndSequence: header.CurrentSequence, EndHead: header.CurrentHead}
}
