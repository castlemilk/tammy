//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package idempotency

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func newEncryptedIdempotencyDatabase(t *testing.T) *sqlcipher.Database {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "idempotency.db")
	key := bytes.Repeat([]byte{0x61}, sqlcipher.KeySize)
	if _, err := sqlcipher.MigrateWorkspace(ctx, path, key, 3); err != nil {
		t.Fatal(err)
	}
	database, err := sqlcipher.Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func observeIdempotencyDatabase(database *sqlcipher.Database) ObserveFunc {
	return func(ctx context.Context, scope Scope) (Record, error) {
		return (&Repository{executor: database}).load(ctx, scope)
	}
}

func TestElectorRejectsChangedSemanticRequestForSameScope(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedIdempotencyDatabase(t)
	now := time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)
	elector, _ := NewElector(Config{Clock: clock.Func(func() time.Time { return now }), Observe: observeIdempotencyDatabase(database)})
	scope := Scope{WorkspaceID: "01890f60-4d6d-7c12-8f02-6c9129d5b001",
		ActorUserID:  "01890f60-4d6d-7c12-8f02-6c9129d5b006",
		RPCName:      "tammy.v1.AuditService.ExportEvidence",
		OperationKey: "01890f60-4d6d-7c12-8f02-6c9129d5b004"}
	first, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	election, err := elector.Elect(ctx, first, scope, idempotencyRequest(scope.OperationKey))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := elector.Complete(ctx, first, election, idempotencyResult(), ""); err != nil {
		t.Fatal(err)
	}
	if err := first.Commit(); err != nil {
		t.Fatal(err)
	}
	changed := idempotencyRequest(scope.OperationKey)
	changed.Destination.CapabilityId = "different-approved-capability"
	second, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	defer second.Rollback()
	if _, err := elector.Elect(ctx, second, scope, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed semantic request error = %v, want ErrConflict", err)
	}
}

func idempotencyRequest(operationKey string) *tammyv1.ExportEvidenceRequest {
	return &tammyv1.ExportEvidenceRequest{
		CommandContext: &tammyv1.CommandContext{
			IdempotencyKey: operationKey,
			Authentication: &tammyv1.AuthenticationContext{
				ActorUserId: "01890f60-4d6d-7c12-8f02-6c9129d5b006",
				SessionId:   "01890f60-4d6d-7c12-8f02-6c9129d5b007",
			},
		},
		WorkspaceId: "01890f60-4d6d-7c12-8f02-6c9129d5b001",
		Filter:      &tammyv1.AuditEventFilter{},
		Destination: &tammyv1.ApprovedFileRef{CapabilityId: "approved-file-capability"},
	}
}

func idempotencyResult() *tammyv1.ExportEvidenceResponse {
	return &tammyv1.ExportEvidenceResponse{Job: &tammyv1.AuditExportJob{
		Id: "01890f60-4d6d-7c12-8f02-6c9129d5b008", Version: 1,
		OperationKey: "01890f60-4d6d-7c12-8f02-6c9129d5b004",
		State:        tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_QUEUED,
		Progress:     &tammyv1.JobProgress{},
		CreatedAt:    timestamppb.New(time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)),
	}}
}

func TestElectorFirstExecutionAndExactDeterministicReplay(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedIdempotencyDatabase(t)
	now := time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)
	elector, err := NewElector(Config{Clock: clock.Func(func() time.Time { return now }), Observe: observeIdempotencyDatabase(database)})
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{
		WorkspaceID:  "01890f60-4d6d-7c12-8f02-6c9129d5b001",
		ActorUserID:  "01890f60-4d6d-7c12-8f02-6c9129d5b006",
		RPCName:      "tammy.v1.AuditService.ExportEvidence",
		OperationKey: "01890f60-4d6d-7c12-8f02-6c9129d5b004",
	}
	transaction, err := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	election, err := elector.Elect(ctx, transaction, scope, idempotencyRequest(scope.OperationKey))
	if err != nil {
		t.Fatal(err)
	}
	if election.Decision != DecisionExecute {
		t.Fatalf("first election = %#v", election)
	}
	expected, err := proto.MarshalOptions{Deterministic: true}.Marshal(idempotencyResult())
	if err != nil {
		t.Fatal(err)
	}
	stored, err := elector.Complete(ctx, transaction, election, idempotencyResult(), "01890f60-4d6d-7c12-8f02-6c9129d5b008")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, expected) {
		t.Fatalf("stored result changed\nwant %x\n got %x", expected, stored)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	var hashVersion, requestType, retentionPolicy, outcome, actor string
	var normalized, retainedResult []byte
	if err := database.QueryRowContext(ctx, `SELECT actor_user_id, semantic_hash_version, request_type, normalized_hash,
		result_proto, retention_policy, outcome FROM command_idempotency_v1 WHERE operation_key=?`, scope.OperationKey).
		Scan(&actor, &hashVersion, &requestType, &normalized, &retainedResult, &retentionPolicy, &outcome); err != nil {
		t.Fatal(err)
	}
	if actor != scope.ActorUserID || hashVersion != "v1" || requestType != "tammy.v1.ExportEvidenceRequest" ||
		len(normalized) != 32 || !bytes.Equal(retainedResult, expected) || retentionPolicy != "WORKSPACE_LIFETIME" || outcome != "COMMITTED" {
		t.Fatalf("retained metadata actor=%q version=%q request=%q normalized=%d result=%x retention=%q outcome=%q",
			actor, hashVersion, requestType, len(normalized), retainedResult, retentionPolicy, outcome)
	}

	replayTransaction, err := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := elector.Elect(ctx, replayTransaction, scope, idempotencyRequest(scope.OperationKey))
	if err != nil {
		t.Fatal(err)
	}
	if replay.Decision != DecisionReplay || replay.ResultType != "tammy.v1.ExportEvidenceResponse" ||
		!bytes.Equal(replay.ResultProto, expected) {
		t.Fatalf("replay = %#v", replay)
	}
	if err := replayTransaction.Rollback(); err != nil {
		t.Fatal(err)
	}
}

func TestElectorReplaysAfterCompatibleAbsentFieldDescriptorAddition(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedIdempotencyDatabase(t)
	now := time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)
	elector, _ := NewElector(Config{Clock: clock.Func(func() time.Time { return now }), Observe: observeIdempotencyDatabase(database)})
	scope := Scope{WorkspaceID: "01890f60-4d6d-7c12-8f02-6c9129d5b001", ActorUserID: "01890f60-4d6d-7c12-8f02-6c9129d5b006",
		RPCName: "tammy.v1.AuditService.ExportEvidence", OperationKey: "01890f60-4d6d-7c12-8f02-6c9129d5b004"}
	transaction, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	election, err := elector.Elect(ctx, transaction, scope, idempotencyRequest(scope.OperationKey))
	if err != nil {
		t.Fatal(err)
	}
	expected, _ := elector.Complete(ctx, transaction, election, idempotencyResult(), "")
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	file := protodesc.ToFileDescriptorProto(tammyv1.File_tammy_v1_audit_proto)
	for _, message := range file.MessageType {
		if message.GetName() == "ExportEvidenceRequest" {
			message.Field = append(message.Field, &descriptorpb.FieldDescriptorProto{
				Name: proto.String("compatible_future_field"), Number: proto.Int32(99),
				Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
			})
		}
	}
	compatibleFile, err := protodesc.NewFile(file, protoregistry.GlobalFiles)
	if err != nil {
		t.Fatal(err)
	}
	compatible := dynamicpb.NewMessage(compatibleFile.Messages().ByName("ExportEvidenceRequest"))
	encodedRequest, _ := proto.MarshalOptions{Deterministic: true}.Marshal(idempotencyRequest(scope.OperationKey))
	if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(encodedRequest, compatible); err != nil {
		t.Fatal(err)
	}
	replayTx, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	defer replayTx.Rollback()
	replay, err := elector.Elect(ctx, replayTx, scope, compatible)
	if err != nil {
		t.Fatal(err)
	}
	if replay.Decision != DecisionReplay || !bytes.Equal(replay.ResultProto, expected) {
		t.Fatalf("compatible descriptor replay = %#v", replay)
	}
}

func TestElectorRetriesFailedCommandWithIncrementedAttempt(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedIdempotencyDatabase(t)
	now := time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)
	elector, _ := NewElector(Config{Clock: clock.Func(func() time.Time { return now }), Observe: observeIdempotencyDatabase(database)})
	scope := Scope{WorkspaceID: "01890f60-4d6d-7c12-8f02-6c9129d5b001",
		ActorUserID: "01890f60-4d6d-7c12-8f02-6c9129d5b006", RPCName: "tammy.v1.AuditService.ExportEvidence",
		OperationKey: "01890f60-4d6d-7c12-8f02-6c9129d5b004"}
	first, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	election, err := elector.Elect(ctx, first, scope, idempotencyRequest(scope.OperationKey))
	if err != nil {
		t.Fatal(err)
	}
	if err := elector.Fail(ctx, first, election, "DESTINATION_UNAVAILABLE"); err != nil {
		t.Fatal(err)
	}
	if err := first.Commit(); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	retry, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	retryElection, err := elector.Elect(ctx, retry, scope, idempotencyRequest(scope.OperationKey))
	if err != nil {
		t.Fatal(err)
	}
	if retryElection.Decision != DecisionExecute || retryElection.Attempt != 2 {
		t.Fatalf("retry election = %#v", retryElection)
	}
	if err := retry.Rollback(); err != nil {
		t.Fatal(err)
	}
}

func TestElectorBoundsInFlightWaitAtTwoSecondsThenAborts(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedIdempotencyDatabase(t)
	now := time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)
	scope := Scope{WorkspaceID: "01890f60-4d6d-7c12-8f02-6c9129d5b001",
		ActorUserID: "01890f60-4d6d-7c12-8f02-6c9129d5b006", RPCName: "tammy.v1.AuditService.ExportEvidence",
		OperationKey: "01890f60-4d6d-7c12-8f02-6c9129d5b004"}
	firstElector, _ := NewElector(Config{Clock: clock.Func(func() time.Time { return now }), Observe: observeIdempotencyDatabase(database)})
	first, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if _, err := firstElector.Elect(ctx, first, scope, idempotencyRequest(scope.OperationKey)); err != nil {
		t.Fatal(err)
	}
	if err := first.Commit(); err != nil {
		t.Fatal(err)
	}
	waits := 0
	elector, _ := NewElector(Config{
		Clock:   clock.Func(func() time.Time { return now }),
		Observe: observeIdempotencyDatabase(database),
		Wait: func(_ context.Context, duration time.Duration) error {
			waits++
			now = now.Add(duration)
			return nil
		},
	})
	second, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	defer second.Rollback()
	started := now
	if _, err := elector.Elect(ctx, second, scope, idempotencyRequest(scope.OperationKey)); !errors.Is(err, ErrAborted) {
		t.Fatalf("in-flight error = %v, want ErrAborted", err)
	}
	if elapsed := now.Sub(started); elapsed != 2*time.Second || waits != 8 {
		t.Fatalf("wait = %s across %d polls, want 2s across 8", elapsed, waits)
	}
}

func TestElectorInFlightObservationReturnsCommittedReplayWithoutSecondExecution(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedIdempotencyDatabase(t)
	now := time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)
	scope := Scope{WorkspaceID: "01890f60-4d6d-7c12-8f02-6c9129d5b001", ActorUserID: "01890f60-4d6d-7c12-8f02-6c9129d5b006",
		RPCName: "tammy.v1.AuditService.ExportEvidence", OperationKey: "01890f60-4d6d-7c12-8f02-6c9129d5b004"}
	seed, _ := NewElector(Config{Clock: clock.Func(func() time.Time { return now }), Observe: observeIdempotencyDatabase(database)})
	first, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if _, err := seed.Elect(ctx, first, scope, idempotencyRequest(scope.OperationKey)); err != nil {
		t.Fatal(err)
	}
	if err := first.Commit(); err != nil {
		t.Fatal(err)
	}
	retained, err := (&Repository{executor: database}).load(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	retained.Outcome = "COMMITTED"
	retained.ResultType = "tammy.v1.ExportEvidenceResponse"
	retained.ResultProto, _ = proto.MarshalOptions{Deterministic: true}.Marshal(idempotencyResult())
	waits := 0
	elector, err := NewElector(Config{Clock: clock.Func(func() time.Time { return now }),
		Wait:    func(context.Context, time.Duration) error { waits++; now = now.Add(250 * time.Millisecond); return nil },
		Observe: func(context.Context, Scope) (Record, error) { return retained, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	second, _ := database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	defer second.Rollback()
	replay, err := elector.Elect(ctx, second, scope, idempotencyRequest(scope.OperationKey))
	if err != nil {
		t.Fatal(err)
	}
	if replay.Decision != DecisionReplay || waits != 1 || !bytes.Equal(replay.ResultProto, retained.ResultProto) {
		t.Fatalf("observed replay = %#v waits=%d", replay, waits)
	}
}

func TestElectorRealConcurrentSQLCipherWriterAbortsWithinTotalTwoSecondBudget(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedIdempotencyDatabase(t)
	scope := Scope{WorkspaceID: "01890f60-4d6d-7c12-8f02-6c9129d5b001",
		ActorUserID: "01890f60-4d6d-7c12-8f02-6c9129d5b006", RPCName: "tammy.v1.AuditService.ExportEvidence",
		OperationKey: "01890f60-4d6d-7c12-8f02-6c9129d5b004"}
	firstElector, _ := NewElector(Config{Clock: clock.Func(time.Now), Observe: observeIdempotencyDatabase(database)})
	first, _ := database.BeginEncryptedTx(ctx, nil)
	if _, err := firstElector.Elect(ctx, first, scope, idempotencyRequest(scope.OperationKey)); err != nil {
		t.Fatal(err)
	}
	defer first.Rollback()

	var observations atomic.Int32
	secondElector, _ := NewElector(Config{Clock: clock.Func(time.Now), Observe: func(observeContext context.Context, observed Scope) (Record, error) {
		observations.Add(1)
		return (&Repository{executor: database}).load(observeContext, observed)
	}})
	second, _ := database.BeginEncryptedTx(ctx, nil)
	defer second.Rollback()
	started := time.Now()
	_, err := secondElector.Elect(ctx, second, scope, idempotencyRequest(scope.OperationKey))
	elapsed := time.Since(started)
	if !errors.Is(err, ErrAborted) {
		t.Fatalf("concurrent election error=%v, want ErrAborted", err)
	}
	if elapsed < 1900*time.Millisecond || elapsed > 2400*time.Millisecond || observations.Load() != 8 {
		t.Fatalf("concurrent election elapsed=%s observations=%d, want total 2s/8", elapsed, observations.Load())
	}
}
