package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

var (
	ErrChainNotFound  = errors.New("audit: chain not found")
	ErrConcurrentHead = errors.New("audit: concurrent head changed")
	ErrRepository     = errors.New("audit: repository failure")
)

const timestampLayout = "2006-01-02T15:04:05.000000000Z"

const (
	// StoredEventPageSizeLimit bounds both SQL result cardinality and retained
	// event objects held by a paging caller.
	StoredEventPageSizeLimit uint32 = 256
	// StoredEventPageByteBudget is the largest retained-byte aggregate a page
	// may materialize. A caller may request a smaller budget.
	StoredEventPageByteBudget uint64 = 8 << 20
	// StoredEventRowByteLimit rejects a corrupt row before any retained BLOB is
	// selected from SQLite.
	StoredEventRowByteLimit uint64 = 8 << 20
)

// StoredEventSnapshot fixes a chain prefix independently of later appends.
type StoredEventSnapshot struct {
	WorkspaceID string
	Generation  uint64
	EndSequence uint64
	EndHead     [sha256.Size]byte
}

// StoredEventCheckpoint authenticates the next keyset page boundary.
type StoredEventCheckpoint struct {
	AfterSequence uint64
	Head          [sha256.Size]byte
}

// StoredEventPage contains at most the caller's count and byte bounds.
type StoredEventPage struct {
	Events     []StoredEvent
	Checkpoint StoredEventCheckpoint
	HasMore    bool
}

const storedEventLengthPredicates = `
		AND length(payload_type) BETWEEN 1 AND 256
		AND length(payload_proto) <= ?
		AND length(payload_json) <= ?
		AND (affected_resources_proto IS NULL OR length(affected_resources_proto) <= ?)
		AND length(canonical_event) <= ?
		AND length(event_proto) <= ?
		AND length(payload_type) + length(payload_proto) + length(payload_json)
			+ coalesce(length(affected_resources_proto), 0) + length(canonical_event) + length(event_proto) <= ?`

// Executor is the caller-owned mutation/query capability required by Audit.
// Deliberately omitting Commit and Rollback prevents this package from owning
// the transaction boundary.
type Executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// ChainHeader is the authenticated start and current position of one generation.
type ChainHeader struct {
	WorkspaceID     string
	Generation      uint64
	ChainSalt       []byte
	GenesisHash     [sha256.Size]byte
	CurrentSequence uint64
	CurrentHead     [sha256.Size]byte
	CreatedAt       time.Time
}

// Repository performs only SQL through a caller-owned capability.
type Repository struct {
	executor Executor
}

func NewRepository(executor Executor) (*Repository, error) {
	if executor == nil {
		return nil, ErrRepository
	}
	return &Repository{executor: executor}, nil
}

// InitializeChain persists one generation's exact salt and genesis under the caller's transaction.
func InitializeChain(ctx context.Context, executor Executor, header ChainHeader) error {
	repository, err := NewRepository(executor)
	if err != nil {
		return err
	}
	return repository.InitializeChain(ctx, header)
}

func (repository *Repository) InitializeChain(ctx context.Context, header ChainHeader) error {
	if repository == nil || repository.executor == nil || header.WorkspaceID == "" || header.Generation == 0 ||
		len(header.ChainSalt) != sha256.Size || !validAuditTimestamp(header.CreatedAt) {
		return ErrInvalidChainInput
	}
	wantGenesis, err := Genesis(header.WorkspaceID, header.ChainSalt)
	if err != nil || wantGenesis != header.GenesisHash {
		return ErrInvalidChainInput
	}
	if header.CurrentSequence == 0 {
		if header.CurrentHead == ([sha256.Size]byte{}) {
			header.CurrentHead = header.GenesisHash
		}
		if header.CurrentHead != header.GenesisHash {
			return ErrInvalidChainInput
		}
	}
	_, err = repository.executor.ExecContext(ctx, `INSERT INTO audit_chain_headers_v1(
		workspace_id, generation, chain_salt, genesis_hash, current_sequence, current_head, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?)`, header.WorkspaceID, header.Generation, header.ChainSalt,
		header.GenesisHash[:], header.CurrentSequence, header.CurrentHead[:], formatTimestamp(header.CreatedAt))
	if err != nil {
		return fmt.Errorf("%w: initialize chain", ErrRepository)
	}
	return nil
}

func (repository *Repository) latestHeader(ctx context.Context, workspaceID string, generation uint64) (ChainHeader, error) {
	if repository == nil || repository.executor == nil || workspaceID == "" {
		return ChainHeader{}, ErrRepository
	}
	query := `SELECT workspace_id, generation, chain_salt, genesis_hash, current_sequence, current_head, created_at
		FROM audit_chain_headers_v1 WHERE workspace_id = ?`
	arguments := []any{workspaceID}
	if generation != 0 {
		query += ` AND generation = ?`
		arguments = append(arguments, generation)
	}
	query += ` ORDER BY generation DESC LIMIT 1`
	rows, err := repository.executor.QueryContext(ctx, query, arguments...)
	if err != nil {
		return ChainHeader{}, fmt.Errorf("%w: read chain", ErrRepository)
	}
	defer rows.Close()
	if !rows.Next() {
		if rows.Err() != nil {
			return ChainHeader{}, fmt.Errorf("%w: read chain", ErrRepository)
		}
		return ChainHeader{}, ErrChainNotFound
	}
	var header ChainHeader
	var genesis, current []byte
	var created string
	if err := rows.Scan(&header.WorkspaceID, &header.Generation, &header.ChainSalt, &genesis,
		&header.CurrentSequence, &current, &created); err != nil || rows.Next() || rows.Err() != nil ||
		len(header.ChainSalt) != sha256.Size || len(genesis) != sha256.Size || len(current) != sha256.Size {
		return ChainHeader{}, fmt.Errorf("%w: malformed chain", ErrRepository)
	}
	copy(header.GenesisHash[:], genesis)
	copy(header.CurrentHead[:], current)
	header.CreatedAt, err = time.Parse(timestampLayout, created)
	if err != nil {
		return ChainHeader{}, fmt.Errorf("%w: malformed chain time", ErrRepository)
	}
	return header, nil
}

// LoadChainHeader reads one exact generation (or latest when generation is zero).
func LoadChainHeader(ctx context.Context, executor Executor, workspaceID string, generation uint64) (ChainHeader, error) {
	repository, err := NewRepository(executor)
	if err != nil {
		return ChainHeader{}, err
	}
	return repository.latestHeader(ctx, workspaceID, generation)
}

func (repository *Repository) insertEvent(ctx context.Context, stored StoredEvent, expected ChainHeader) error {
	if stored.Event == nil || stored.Event.WorkspaceId != expected.WorkspaceID || stored.Event.Generation != expected.Generation ||
		stored.Event.Sequence != expected.CurrentSequence+1 || len(stored.Event.EventHash) != sha256.Size {
		return ErrInvalidEvent
	}
	sourceProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(stored.Event.Source)
	if err != nil {
		return ErrInvalidEvent
	}
	result := stored.Event.Result
	var actorUserID, sessionID any
	if stored.Event.Actor != nil {
		actorUserID, sessionID = nullString(stored.Event.Actor.ActorUserId), nullString(stored.Event.Actor.SessionId)
	}
	if _, err := repository.executor.ExecContext(ctx, `INSERT INTO audit_events_v1(
		workspace_id, generation, sequence, event_id, event_type, occurred_at, actor_user_id, session_id,
		organisation_id, command_id, command_type, idempotency_key, source_proto, affected_resources_proto,
		before_semantic_hash, after_semantic_hash, result_type, result_sha256, outcome_code,
		payload_type, payload_schema_fingerprint, payload_proto, payload_json, canonical_event, event_proto,
		previous_hash, event_hash
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		stored.Event.WorkspaceId, stored.Event.Generation, stored.Event.Sequence, stored.Event.Id, int32(stored.Event.Type),
		formatTimestamp(stored.Event.OccurredAt.AsTime()), actorUserID, sessionID,
		nullPointerString(stored.Event.OrganisationId), nullPointerString(stored.Event.CommandId), stored.Event.CommandType,
		nullPointerString(stored.Event.IdempotencyKey), sourceProto, stored.AffectedResourcesProto,
		nullBytes(stored.Event.BeforeSemanticHash), nullBytes(stored.Event.AfterSemanticHash), result.TypeName,
		result.DeterministicSha256, result.OutcomeCode, stored.PayloadType, stored.Event.PayloadSchemaFingerprint,
		stored.PayloadProto, stored.PayloadJSON, stored.CanonicalEvent, stored.EventProto,
		stored.Event.PreviousHash, stored.Event.EventHash); err != nil {
		return fmt.Errorf("%w: insert event", ErrRepository)
	}
	updated, err := repository.executor.ExecContext(ctx, `UPDATE audit_chain_headers_v1
		SET current_sequence = ?, current_head = ?
		WHERE workspace_id = ? AND generation = ? AND current_sequence = ? AND current_head = ?`,
		stored.Event.Sequence, stored.Event.EventHash, expected.WorkspaceID, expected.Generation,
		expected.CurrentSequence, expected.CurrentHead[:])
	if err != nil {
		return fmt.Errorf("%w: update head", ErrRepository)
	}
	count, err := updated.RowsAffected()
	if err != nil || count != 1 {
		return ErrConcurrentHead
	}
	return nil
}

// LoadStoredEventPage loads one authenticated keyset page from an immutable
// snapshot. It first selects only lengths, so a CHECK-bypassed oversized BLOB
// is rejected without being materialized by database/sql.
func LoadStoredEventPage(
	ctx context.Context,
	executor Executor,
	snapshot StoredEventSnapshot,
	checkpoint StoredEventCheckpoint,
	pageSize uint32,
	byteBudget uint64,
) (StoredEventPage, error) {
	repository, err := NewRepository(executor)
	if err != nil {
		return StoredEventPage{}, err
	}
	return repository.LoadStoredEventPage(ctx, snapshot, checkpoint, pageSize, byteBudget)
}

type storedEventPageRow struct {
	sequence uint64
	bytes    uint64
}

type storedEventRows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close() error
}

func (repository *Repository) LoadStoredEventPage(
	ctx context.Context,
	snapshot StoredEventSnapshot,
	checkpoint StoredEventCheckpoint,
	pageSize uint32,
	byteBudget uint64,
) (StoredEventPage, error) {
	page := StoredEventPage{Checkpoint: checkpoint}
	if repository == nil || repository.executor == nil || ctx == nil || snapshot.WorkspaceID == "" ||
		snapshot.Generation == 0 || snapshot.EndHead == ([sha256.Size]byte{}) ||
		checkpoint.AfterSequence > snapshot.EndSequence || pageSize == 0 || pageSize > StoredEventPageSizeLimit ||
		byteBudget == 0 || byteBudget > StoredEventPageByteBudget ||
		checkpoint.AfterSequence < snapshot.EndSequence && checkpoint.Head == ([sha256.Size]byte{}) {
		return StoredEventPage{}, ErrRepository
	}
	if err := ctx.Err(); err != nil {
		return StoredEventPage{}, fmt.Errorf("%w: page context: %v", ErrRepository, err)
	}
	if checkpoint.AfterSequence == snapshot.EndSequence {
		if checkpoint.Head != snapshot.EndHead {
			return StoredEventPage{}, storedEventCorruption("snapshot terminal mismatch")
		}
		return page, nil
	}

	rows, err := repository.executor.QueryContext(ctx, `SELECT sequence,
		length(payload_type), length(payload_proto), length(payload_json),
		coalesce(length(affected_resources_proto), 0), length(canonical_event), length(event_proto)
		FROM audit_events_v1
		WHERE workspace_id = ? AND generation = ?
		AND sequence > ? AND sequence <= ?`+storedEventLengthPredicates+`
		ORDER BY sequence LIMIT ?`, snapshot.WorkspaceID, snapshot.Generation,
		checkpoint.AfterSequence, snapshot.EndSequence,
		StoredEventRowByteLimit, StoredEventRowByteLimit, StoredEventRowByteLimit,
		StoredEventRowByteLimit, StoredEventRowByteLimit, StoredEventRowByteLimit,
		int(pageSize))
	if err != nil {
		return StoredEventPage{}, fmt.Errorf("%w: list event page lengths", ErrRepository)
	}
	selected, hasMore, err := selectStoredEventPageRows(ctx, rows, checkpoint.AfterSequence, snapshot.EndSequence,
		pageSize, byteBudget)
	if err != nil {
		return StoredEventPage{}, err
	}
	if len(selected) == 0 {
		return StoredEventPage{}, storedEventCorruption(fmt.Sprintf("missing event at sequence %d", checkpoint.AfterSequence+1))
	}
	selectedEnd := selected[len(selected)-1].sequence

	dataRows, err := repository.executor.QueryContext(ctx, `SELECT sequence, payload_type, payload_proto, payload_json,
		affected_resources_proto, canonical_event, event_proto
		FROM audit_events_v1
		WHERE workspace_id = ? AND generation = ?
		AND sequence > ? AND sequence <= ?`+storedEventLengthPredicates+`
		ORDER BY sequence LIMIT ?`, snapshot.WorkspaceID, snapshot.Generation,
		checkpoint.AfterSequence, selectedEnd,
		StoredEventRowByteLimit, StoredEventRowByteLimit, StoredEventRowByteLimit,
		StoredEventRowByteLimit, StoredEventRowByteLimit, StoredEventRowByteLimit,
		len(selected))
	if err != nil {
		return StoredEventPage{}, fmt.Errorf("%w: list event page bytes", ErrRepository)
	}
	events, next, err := scanStoredEventPageRows(ctx, dataRows, snapshot, checkpoint, selected, byteBudget)
	if err != nil {
		return StoredEventPage{}, err
	}
	page.Events = events
	page.Checkpoint = next
	page.HasMore = hasMore || selectedEnd < snapshot.EndSequence && len(selected) == int(pageSize)
	if selectedEnd == snapshot.EndSequence {
		if page.HasMore || next.Head != snapshot.EndHead {
			return StoredEventPage{}, storedEventCorruption("snapshot terminal mismatch")
		}
	} else if !page.HasMore {
		return StoredEventPage{}, storedEventCorruption(fmt.Sprintf("missing event at sequence %d", selectedEnd+1))
	}
	return page, nil
}

func selectStoredEventPageRows(
	ctx context.Context,
	rows storedEventRows,
	afterSequence, endSequence uint64,
	pageSize uint32,
	byteBudget uint64,
) (selected []storedEventPageRow, hasMore bool, resultErr error) {
	if rows == nil {
		return nil, false, fmt.Errorf("%w: nil event page rows", ErrRepository)
	}
	defer func() {
		if err := rows.Close(); err != nil && resultErr == nil {
			resultErr = fmt.Errorf("%w: close event page lengths", ErrRepository)
		}
	}()
	selected = make([]storedEventPageRow, 0, pageSize)
	expected := afterSequence + 1
	var used uint64
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, false, fmt.Errorf("%w: page context: %v", ErrRepository, err)
		}
		if len(selected) >= int(pageSize) {
			return nil, false, storedEventCorruption("excess event page length row")
		}
		var sequence uint64
		var payloadType, payloadProto, payloadJSON, affected, canonical, eventProto int64
		if err := rows.Scan(&sequence, &payloadType, &payloadProto, &payloadJSON, &affected, &canonical, &eventProto); err != nil {
			return nil, false, fmt.Errorf("%w: scan event page lengths", ErrRepository)
		}
		if sequence != expected || sequence > endSequence {
			return nil, false, storedEventCorruption("event page sequence")
		}
		lengths := [...]int64{payloadType, payloadProto, payloadJSON, affected, canonical, eventProto}
		var rowBytes uint64
		for index, length := range lengths {
			if length < 0 || index == 0 && (length == 0 || length > 256) || index != 0 && uint64(length) > StoredEventRowByteLimit ||
				rowBytes > StoredEventRowByteLimit-uint64(length) {
				return nil, false, storedEventCorruption("invalid event page length")
			}
			rowBytes += uint64(length)
		}
		if rowBytes > byteBudget-used {
			if len(selected) == 0 {
				return nil, false, storedEventCorruption("event exceeds page byte budget")
			}
			hasMore = true
			break
		}
		selected = append(selected, storedEventPageRow{sequence: sequence, bytes: rowBytes})
		used += rowBytes
		expected++
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("%w: list event page lengths", ErrRepository)
	}
	if err := ctx.Err(); err != nil {
		return nil, false, fmt.Errorf("%w: page context: %v", ErrRepository, err)
	}
	return selected, hasMore, nil
}

func scanStoredEventPageRows(
	ctx context.Context,
	rows storedEventRows,
	snapshot StoredEventSnapshot,
	checkpoint StoredEventCheckpoint,
	selected []storedEventPageRow,
	byteBudget uint64,
) (events []StoredEvent, next StoredEventCheckpoint, resultErr error) {
	if rows == nil {
		return nil, StoredEventCheckpoint{}, fmt.Errorf("%w: nil event page rows", ErrRepository)
	}
	defer func() {
		if err := rows.Close(); err != nil && resultErr == nil {
			resultErr = fmt.Errorf("%w: close event page bytes", ErrRepository)
		}
	}()
	events = make([]StoredEvent, 0, len(selected))
	next = checkpoint
	var used uint64
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, StoredEventCheckpoint{}, fmt.Errorf("%w: page context: %v", ErrRepository, err)
		}
		index := len(events)
		if index >= len(selected) {
			return nil, StoredEventCheckpoint{}, storedEventCorruption("excess event page row")
		}
		var sequence uint64
		var stored StoredEvent
		if err := rows.Scan(&sequence, &stored.PayloadType, &stored.PayloadProto, &stored.PayloadJSON,
			&stored.AffectedResourcesProto, &stored.CanonicalEvent, &stored.EventProto); err != nil {
			return nil, StoredEventCheckpoint{}, fmt.Errorf("%w: scan event page bytes", ErrRepository)
		}
		if sequence != selected[index].sequence || selected[index].bytes > byteBudget-used {
			return nil, StoredEventCheckpoint{}, storedEventCorruption("event page changed")
		}
		actualBytes := uint64(len(stored.PayloadType) + len(stored.PayloadProto) + len(stored.PayloadJSON) +
			len(stored.AffectedResourcesProto) + len(stored.CanonicalEvent) + len(stored.EventProto))
		if actualBytes != selected[index].bytes || actualBytes > StoredEventRowByteLimit {
			return nil, StoredEventCheckpoint{}, storedEventCorruption("event page length changed")
		}
		stored.Event = &tammyv1.AuditEvent{}
		if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(stored.EventProto, stored.Event); err != nil ||
			stored.Event.WorkspaceId != snapshot.WorkspaceID || stored.Event.Generation != snapshot.Generation ||
			stored.Event.Sequence != sequence || !bytes.Equal(stored.Event.PreviousHash, next.Head[:]) ||
			len(stored.Event.EventHash) != sha256.Size {
			return nil, StoredEventCheckpoint{}, storedEventCorruption("decode event page")
		}
		if !storedAffectedResourcesMatch(stored) {
			return nil, StoredEventCheckpoint{}, storedEventCorruption("decode affected resources")
		}
		copy(next.Head[:], stored.Event.EventHash)
		next.AfterSequence = sequence
		used += actualBytes
		events = append(events, stored)
	}
	if err := rows.Err(); err != nil {
		return nil, StoredEventCheckpoint{}, fmt.Errorf("%w: list event page bytes", ErrRepository)
	}
	if err := ctx.Err(); err != nil {
		return nil, StoredEventCheckpoint{}, fmt.Errorf("%w: page context: %v", ErrRepository, err)
	}
	if len(events) != len(selected) {
		return nil, StoredEventCheckpoint{}, storedEventCorruption("missing event page row")
	}
	return events, next, nil
}

func storedAffectedResourcesMatch(stored StoredEvent) bool {
	affected := &tammyv1.AuditEvent{}
	return !(len(stored.AffectedResourcesProto) == 0 && len(stored.Event.AffectedResources) != 0 ||
		len(stored.AffectedResourcesProto) != 0 && ((proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(
			stored.AffectedResourcesProto, affected) != nil || len(affected.ProtoReflect().GetUnknown()) != 0 || !proto.Equal(
			&tammyv1.AuditEvent{AffectedResources: affected.AffectedResources},
			&tammyv1.AuditEvent{AffectedResources: stored.Event.AffectedResources})))
}

func storedEventCorruption(label string) error {
	return fmt.Errorf("%w: %w: %s", ErrRepository, ErrInvalidEvent, label)
}

// MatchingAuditEventPage is a bounded sparse projection from one fixed chain
// snapshot. Events are the exact decoded retained event_proto rows.
type MatchingAuditEventPage struct {
	Events  []*tammyv1.AuditEvent
	HasMore bool
}

type matchingAuditEventRow struct {
	sequence uint64
	bytes    uint64
}

// LoadMatchingAuditEventPage pushes a canonical audit filter into SQL. It
// first reads only lengths, then materializes at most pageSize admitted event
// protos within StoredEventPageByteBudget.
func LoadMatchingAuditEventPage(
	ctx context.Context,
	executor Executor,
	snapshot StoredEventSnapshot,
	filter *tammyv1.AuditEventFilter,
	after uint64,
	pageSize uint32,
) (MatchingAuditEventPage, error) {
	repository, err := NewRepository(executor)
	if err != nil {
		return MatchingAuditEventPage{}, err
	}
	return repository.LoadMatchingAuditEventPage(ctx, snapshot, filter, after, pageSize)
}

func (repository *Repository) LoadMatchingAuditEventPage(
	ctx context.Context,
	snapshot StoredEventSnapshot,
	filter *tammyv1.AuditEventFilter,
	after uint64,
	pageSize uint32,
) (MatchingAuditEventPage, error) {
	if repository == nil || repository.executor == nil || ctx == nil || snapshot.WorkspaceID == "" ||
		snapshot.Generation == 0 || snapshot.EndHead == ([sha256.Size]byte{}) || after > snapshot.EndSequence ||
		pageSize == 0 || pageSize > 200 || !canonicalAuditEventFilter(filter) {
		return MatchingAuditEventPage{}, ErrRepository
	}
	if err := ctx.Err(); err != nil {
		return MatchingAuditEventPage{}, fmt.Errorf("%w: matching page context: %v", ErrRepository, err)
	}
	if after == snapshot.EndSequence {
		return MatchingAuditEventPage{}, nil
	}
	metadataQuery, metadataArguments := matchingAuditEventSQL(snapshot, filter, after,
		`SELECT sequence, CASE WHEN length(event_proto) <= ? THEN length(event_proto) ELSE -1 END`,
		[]any{StoredEventRowByteLimit})
	metadataQuery += ` ORDER BY sequence LIMIT ?`
	metadataArguments = append(metadataArguments, int(pageSize)+1)
	rows, err := repository.executor.QueryContext(ctx, metadataQuery, metadataArguments...)
	if err != nil {
		return MatchingAuditEventPage{}, fmt.Errorf("%w: list matching event lengths", ErrRepository)
	}
	selected, hasMore, err := scanMatchingAuditEventLengths(ctx, rows, pageSize)
	if err != nil {
		return MatchingAuditEventPage{}, err
	}
	if len(selected) == 0 {
		return MatchingAuditEventPage{}, nil
	}
	returnCount := len(selected)
	if returnCount > int(pageSize) {
		returnCount = int(pageSize)
		hasMore = true
	}
	selected = selected[:returnCount]
	selectedEnd := selected[len(selected)-1].sequence
	bodyQuery, bodyArguments := matchingAuditEventSQL(snapshot, filter, after,
		`SELECT sequence, event_proto`, nil)
	bodyQuery += ` AND sequence <= ? AND length(event_proto) <= ? ORDER BY sequence LIMIT ?`
	bodyArguments = append(bodyArguments, selectedEnd, StoredEventRowByteLimit, returnCount)
	bodyRows, err := repository.executor.QueryContext(ctx, bodyQuery, bodyArguments...)
	if err != nil {
		return MatchingAuditEventPage{}, fmt.Errorf("%w: list matching event bytes", ErrRepository)
	}
	events, err := scanMatchingAuditEventBytes(ctx, bodyRows, snapshot, filter, after, selected)
	if err != nil {
		return MatchingAuditEventPage{}, err
	}
	return MatchingAuditEventPage{Events: events, HasMore: hasMore}, nil
}

func matchingAuditEventSQL(
	snapshot StoredEventSnapshot,
	filter *tammyv1.AuditEventFilter,
	after uint64,
	selection string,
	arguments []any,
) (string, []any) {
	query := selection + ` FROM audit_events_v1 WHERE workspace_id = ? AND generation = ?
		AND sequence > ? AND sequence <= ?`
	arguments = append(arguments, snapshot.WorkspaceID, snapshot.Generation, after, snapshot.EndSequence)
	if filter.StartSequence != nil {
		query += ` AND sequence >= ?`
		arguments = append(arguments, *filter.StartSequence)
	}
	if filter.EndSequence != nil {
		query += ` AND sequence <= ?`
		arguments = append(arguments, *filter.EndSequence)
	}
	if filter.ActorUserId != nil {
		query += ` AND actor_user_id = ?`
		arguments = append(arguments, *filter.ActorUserId)
	}
	if filter.FromTime != nil {
		query += ` AND occurred_at >= ?`
		arguments = append(arguments, formatTimestamp(filter.FromTime.AsTime()))
	}
	if filter.ToTime != nil {
		query += ` AND occurred_at <= ?`
		arguments = append(arguments, formatTimestamp(filter.ToTime.AsTime()))
	}
	if len(filter.EventTypes) != 0 {
		query += ` AND event_type IN (`
		for index, eventType := range filter.EventTypes {
			if index != 0 {
				query += `, `
			}
			query += `?`
			arguments = append(arguments, int32(eventType))
		}
		query += `)`
	}
	return query, arguments
}

func canonicalAuditEventFilter(filter *tammyv1.AuditEventFilter) bool {
	if filter == nil || len(filter.ProtoReflect().GetUnknown()) != 0 ||
		len(filter.EventTypes) > len(tammyv1.AuditEventType_name)-1 ||
		filter.ActorUserId != nil && !exportReferencePattern.MatchString(*filter.ActorUserId) ||
		filter.StartSequence != nil && *filter.StartSequence == 0 ||
		filter.EndSequence != nil && *filter.EndSequence == 0 ||
		filter.StartSequence != nil && filter.EndSequence != nil && *filter.StartSequence > *filter.EndSequence ||
		filter.FromTime != nil && (!filter.FromTime.IsValid() || !validAuditTimestamp(filter.FromTime.AsTime())) ||
		filter.ToTime != nil && (!filter.ToTime.IsValid() || !validAuditTimestamp(filter.ToTime.AsTime())) ||
		filter.FromTime != nil && filter.ToTime != nil && filter.FromTime.AsTime().After(filter.ToTime.AsTime()) {
		return false
	}
	previous := tammyv1.AuditEventType_AUDIT_EVENT_TYPE_UNSPECIFIED
	for _, eventType := range filter.EventTypes {
		if eventType <= previous {
			return false
		}
		if _, defined := tammyv1.AuditEventType_name[int32(eventType)]; !defined ||
			eventType == tammyv1.AuditEventType_AUDIT_EVENT_TYPE_UNSPECIFIED {
			return false
		}
		previous = eventType
	}
	return true
}

func scanMatchingAuditEventLengths(
	ctx context.Context,
	rows storedEventRows,
	pageSize uint32,
) (selected []matchingAuditEventRow, hasMore bool, resultErr error) {
	if rows == nil {
		return nil, false, fmt.Errorf("%w: nil matching length rows", ErrRepository)
	}
	defer func() {
		if err := rows.Close(); err != nil && resultErr == nil {
			resultErr = fmt.Errorf("%w: close matching length rows", ErrRepository)
		}
	}()
	selected = make([]matchingAuditEventRow, 0, int(pageSize)+1)
	var used uint64
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, false, fmt.Errorf("%w: matching page context: %v", ErrRepository, err)
		}
		if len(selected) >= int(pageSize)+1 {
			return nil, false, storedEventCorruption("excess matching length row")
		}
		var sequence uint64
		var length int64
		if err := rows.Scan(&sequence, &length); err != nil {
			return nil, false, fmt.Errorf("%w: scan matching event length", ErrRepository)
		}
		if sequence == 0 || length <= 0 || uint64(length) > StoredEventRowByteLimit {
			return nil, false, storedEventCorruption("invalid matching event length")
		}
		if uint64(length) > StoredEventPageByteBudget-used {
			if len(selected) == 0 {
				return nil, false, storedEventCorruption("matching event exceeds page budget")
			}
			hasMore = true
			break
		}
		if len(selected) != 0 && sequence <= selected[len(selected)-1].sequence {
			return nil, false, storedEventCorruption("matching event order")
		}
		selected = append(selected, matchingAuditEventRow{sequence: sequence, bytes: uint64(length)})
		used += uint64(length)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("%w: list matching event lengths", ErrRepository)
	}
	if err := ctx.Err(); err != nil {
		return nil, false, fmt.Errorf("%w: matching page context: %v", ErrRepository, err)
	}
	return selected, hasMore, nil
}

func scanMatchingAuditEventBytes(
	ctx context.Context,
	rows storedEventRows,
	snapshot StoredEventSnapshot,
	filter *tammyv1.AuditEventFilter,
	after uint64,
	selected []matchingAuditEventRow,
) (events []*tammyv1.AuditEvent, resultErr error) {
	if rows == nil {
		return nil, fmt.Errorf("%w: nil matching byte rows", ErrRepository)
	}
	defer func() {
		if err := rows.Close(); err != nil && resultErr == nil {
			resultErr = fmt.Errorf("%w: close matching byte rows", ErrRepository)
		}
	}()
	events = make([]*tammyv1.AuditEvent, 0, len(selected))
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("%w: matching page context: %v", ErrRepository, err)
		}
		index := len(events)
		if index >= len(selected) {
			return nil, storedEventCorruption("excess matching event row")
		}
		var sequence uint64
		var eventProto []byte
		if err := rows.Scan(&sequence, &eventProto); err != nil {
			return nil, fmt.Errorf("%w: scan matching event bytes", ErrRepository)
		}
		if sequence != selected[index].sequence || uint64(len(eventProto)) != selected[index].bytes {
			return nil, storedEventCorruption("matching event page changed")
		}
		event := &tammyv1.AuditEvent{}
		if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(eventProto, event); err != nil ||
			len(event.ProtoReflect().GetUnknown()) != 0 || event.WorkspaceId != snapshot.WorkspaceID ||
			event.Generation != snapshot.Generation || event.Sequence != sequence || event.Sequence <= after ||
			event.Sequence > snapshot.EndSequence || !auditEventMatchesFilter(event, filter) {
			return nil, storedEventCorruption("decode matching event")
		}
		canonical, err := proto.MarshalOptions{Deterministic: true}.Marshal(event)
		if err != nil || !bytes.Equal(canonical, eventProto) {
			return nil, storedEventCorruption("noncanonical matching event")
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: list matching event bytes", ErrRepository)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%w: matching page context: %v", ErrRepository, err)
	}
	if len(events) != len(selected) {
		return nil, storedEventCorruption("missing matching event row")
	}
	return events, nil
}

func auditEventMatchesFilter(event *tammyv1.AuditEvent, filter *tammyv1.AuditEventFilter) bool {
	if event == nil || event.OccurredAt == nil || !event.OccurredAt.IsValid() {
		return false
	}
	if filter.StartSequence != nil && event.Sequence < *filter.StartSequence ||
		filter.EndSequence != nil && event.Sequence > *filter.EndSequence ||
		filter.ActorUserId != nil && (event.Actor == nil || event.Actor.ActorUserId != *filter.ActorUserId) ||
		filter.FromTime != nil && event.OccurredAt.AsTime().Before(filter.FromTime.AsTime()) ||
		filter.ToTime != nil && event.OccurredAt.AsTime().After(filter.ToTime.AsTime()) {
		return false
	}
	if len(filter.EventTypes) == 0 {
		return true
	}
	for _, eventType := range filter.EventTypes {
		if event.Type == eventType {
			return true
		}
	}
	return false
}

// LoadStoredEvents returns exact retained bytes in sequence order without
// reserializing any payload or event.
func LoadStoredEvents(
	ctx context.Context,
	executor Executor,
	workspaceID string,
	generation, startSequence, endSequence uint64,
) ([]StoredEvent, error) {
	repository, err := NewRepository(executor)
	if err != nil {
		return nil, err
	}
	return repository.LoadStoredEvents(ctx, workspaceID, generation, startSequence, endSequence)
}

func (repository *Repository) LoadStoredEvents(
	ctx context.Context,
	workspaceID string,
	generation, startSequence, endSequence uint64,
) ([]StoredEvent, error) {
	if repository == nil || repository.executor == nil || workspaceID == "" || generation == 0 ||
		startSequence == 0 || endSequence == 0 || endSequence < startSequence {
		return nil, ErrRepository
	}
	count := endSequence - startSequence + 1
	if count > uint64(StoredEventPageSizeLimit) {
		return nil, ErrRepository
	}
	query := `SELECT payload_type, payload_proto, payload_json, affected_resources_proto, canonical_event, event_proto
		FROM audit_events_v1 WHERE workspace_id = ? AND generation = ?
		AND sequence >= ? AND sequence <= ? ORDER BY sequence LIMIT ?`
	arguments := []any{workspaceID, generation, startSequence, endSequence, int(count)}
	rows, err := repository.executor.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("%w: list events", ErrRepository)
	}
	defer rows.Close()
	events := make([]StoredEvent, 0)
	for rows.Next() {
		if len(events) >= int(count) {
			return nil, fmt.Errorf("%w: excess events", ErrRepository)
		}
		var stored StoredEvent
		if err := rows.Scan(&stored.PayloadType, &stored.PayloadProto, &stored.PayloadJSON, &stored.AffectedResourcesProto,
			&stored.CanonicalEvent, &stored.EventProto); err != nil {
			return nil, fmt.Errorf("%w: scan event", ErrRepository)
		}
		stored.Event = &tammyv1.AuditEvent{}
		expectedSequence := startSequence + uint64(len(events))
		if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(stored.EventProto, stored.Event); err != nil ||
			stored.Event.WorkspaceId != workspaceID || stored.Event.Generation != generation ||
			stored.Event.Sequence != expectedSequence {
			return nil, fmt.Errorf("%w: decode event", ErrRepository)
		}
		affected := &tammyv1.AuditEvent{}
		if len(stored.AffectedResourcesProto) == 0 && len(stored.Event.AffectedResources) != 0 ||
			len(stored.AffectedResourcesProto) != 0 && ((proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(stored.AffectedResourcesProto, affected) != nil ||
				len(affected.ProtoReflect().GetUnknown()) != 0 || !proto.Equal(
				&tammyv1.AuditEvent{AffectedResources: affected.AffectedResources},
				&tammyv1.AuditEvent{AffectedResources: stored.Event.AffectedResources})) {
			return nil, fmt.Errorf("%w: decode affected resources", ErrRepository)
		}
		stored.PayloadProto = append([]byte(nil), stored.PayloadProto...)
		stored.PayloadJSON = append([]byte(nil), stored.PayloadJSON...)
		stored.AffectedResourcesProto = append([]byte(nil), stored.AffectedResourcesProto...)
		stored.CanonicalEvent = append([]byte(nil), stored.CanonicalEvent...)
		stored.EventProto = append([]byte(nil), stored.EventProto...)
		events = append(events, stored)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: list events", ErrRepository)
	}
	if len(events) != int(count) {
		return nil, fmt.Errorf("%w: incomplete event range", ErrRepository)
	}
	return events, nil
}

func formatTimestamp(value time.Time) string { return value.UTC().Format(timestampLayout) }

func validAuditTimestamp(value time.Time) bool {
	if value.IsZero() {
		return false
	}
	formatted := formatTimestamp(value)
	if len(formatted) != len("2006-01-02T15:04:05.000000000Z") {
		return false
	}
	parsed, err := time.Parse(timestampLayout, formatted)
	return err == nil && parsed.Equal(value)
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullPointerString(value *string) any {
	if value == nil || *value == "" {
		return nil
	}
	return *value
}

func nullBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}
