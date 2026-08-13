//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package jobs_test

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
	"github.com/tammyapp/tammy/services/core/internal/platform/jobs"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
)

const jobID = "018f0000-0000-7000-8000-000000000050"

func TestLeaseElectionAllowsExactlyOneRunner(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspace(t)
	source := newMutableClock(time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC))
	enqueue(t, workspace, source.Now())
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	handler := jobs.HandlerFunc(func(ctx context.Context, job jobs.Job) (jobs.Outcome, error) {
		calls.Add(1)
		close(started)
		<-release
		return jobs.Outcome{Complete: true, ResultProto: progressProto("done")}, nil
	})
	first := newRunner(t, workspace, source, "worker-1", handler, func(uint32) time.Duration { return time.Second })
	second := newRunner(t, workspace, source, "worker-2", handler, func(uint32) time.Duration { return time.Second })
	firstDone := make(chan error, 1)
	go func() { _, err := first.RunNext(context.Background()); firstDone <- err }()
	<-started
	result, err := second.RunNext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Claimed {
		t.Fatalf("second runner claimed leased job: %#v", result)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("handler calls = %d", calls.Load())
	}
	assertJob(t, workspace, "COMPLETED", 1, 1, progressProto("done"))
}

func TestCheckpointResumesOnNextLeaseWithoutPolling(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspace(t)
	source := newMutableClock(time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC))
	enqueue(t, workspace, source.Now())
	handler := jobs.HandlerFunc(func(ctx context.Context, job jobs.Job) (jobs.Outcome, error) {
		if job.CheckpointSequence == 0 {
			return jobs.Outcome{CheckpointProto: progressProto("page_1"), ProgressProto: progressProto("half")}, nil
		}
		if job.CheckpointSequence != 1 || string(job.CheckpointProto) != string(progressProto("page_1")) {
			t.Fatalf("resume checkpoint = sequence:%d bytes:%q", job.CheckpointSequence, job.CheckpointProto)
		}
		return jobs.Outcome{Complete: true, ResultProto: progressProto("complete")}, nil
	})
	runner := newRunner(t, workspace, source, "worker", handler, func(uint32) time.Duration { return time.Second })
	first, err := runner.RunNext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.State != "PENDING" {
		t.Fatalf("checkpoint state = %#v", first)
	}
	second, err := runner.RunNext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.State != "COMPLETED" || second.Attempt != 2 {
		t.Fatalf("resume result = %#v", second)
	}
	assertJob(t, workspace, "COMPLETED", 2, 1, progressProto("complete"))
}

func TestCancellationObservedBeforeCommitPoint(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspace(t)
	source := newMutableClock(time.Date(2026, 8, 4, 2, 0, 0, 0, time.UTC))
	enqueue(t, workspace, source.Now())
	if _, err := workspace.Database.Exec(`CREATE TABLE job_domain_effects(value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	var runner *jobs.Runner
	handler := jobs.HandlerFunc(func(ctx context.Context, job jobs.Job) (jobs.Outcome, error) {
		if err := runner.RequestCancel(ctx, job.ID); err != nil {
			t.Fatal(err)
		}
		return jobs.Outcome{
			Complete: true, ResultProto: progressProto("must_not_commit"),
			Commit: func(ctx context.Context, transaction *jobs.DomainTransaction) error {
				return transaction.InsertRow(ctx, "job_domain_effects", []string{"value"}, "committed")
			},
		}, nil
	})
	runner = newRunner(t, workspace, source, "worker", handler, func(uint32) time.Duration { return time.Second })
	result, err := runner.RunNext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "CANCELLED" {
		t.Fatalf("cancel result = %#v", result)
	}
	assertJob(t, workspace, "CANCELLED", 1, 0, nil)
	var domainEffects int
	if err := workspace.Database.QueryRow(`SELECT count(*) FROM job_domain_effects`).Scan(&domainEffects); err != nil {
		t.Fatal(err)
	}
	if domainEffects != 0 {
		t.Fatalf("cancelled job committed %d domain effects", domainEffects)
	}
}

func TestRetriesUseInjectedClockAndBackoffWithoutSleeping(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspace(t)
	initial := time.Date(2026, 8, 4, 3, 0, 0, 0, time.UTC)
	source := newMutableClock(initial)
	enqueue(t, workspace, source.Now())
	transient := errors.New("transient")
	var calls atomic.Int32
	handler := jobs.HandlerFunc(func(ctx context.Context, job jobs.Job) (jobs.Outcome, error) {
		if calls.Add(1) == 1 {
			return jobs.Outcome{}, transient
		}
		return jobs.Outcome{Complete: true, ResultProto: progressProto("retried")}, nil
	})
	runner := newRunner(t, workspace, source, "worker", handler, func(attempt uint32) time.Duration {
		if attempt != 1 {
			t.Fatalf("backoff attempt = %d", attempt)
		}
		return 7 * time.Second
	})
	first, err := runner.RunNext(context.Background())
	if !errors.Is(err, transient) || first.State != "RETRY_WAIT" {
		t.Fatalf("first retry = %#v, %v", first, err)
	}
	result, err := runner.RunNext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Claimed {
		t.Fatalf("retry elected before deterministic deadline: %#v", result)
	}
	source.Set(initial.Add(7 * time.Second))
	result, err = runner.RunNext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "COMPLETED" || result.Attempt != 2 {
		t.Fatalf("retry completion = %#v", result)
	}
	assertJob(t, workspace, "COMPLETED", 2, 1, progressProto("retried"))
}

func TestExpiredCancellationLeaseFinalizesWithoutRunningHandler(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspace(t)
	initial := time.Date(2026, 8, 4, 4, 0, 0, 0, time.UTC)
	source := newMutableClock(initial)
	enqueue(t, workspace, source.Now())
	if _, err := workspace.Database.Exec(`UPDATE jobs SET state='CANCEL_REQUESTED', lease_owner='dead-worker', lease_expires_at=? WHERE id=?`, initial.Add(-time.Second).Format("2006-01-02T15:04:05.000000000Z"), jobID); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	runner := newRunner(t, workspace, source, "recovery-worker", jobs.HandlerFunc(func(context.Context, jobs.Job) (jobs.Outcome, error) {
		calls.Add(1)
		return jobs.Outcome{Complete: true}, nil
	}), func(uint32) time.Duration { return time.Second })
	result, err := runner.RunNext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "CANCELLED" || calls.Load() != 0 {
		t.Fatalf("recovered cancellation = %#v, calls=%d", result, calls.Load())
	}
	assertJob(t, workspace, "CANCELLED", 0, 0, nil)
}

func TestInvalidHandlerOutcomeIsPersistedAsDeterministicRetry(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspace(t)
	source := newMutableClock(time.Date(2026, 8, 4, 5, 0, 0, 0, time.UTC))
	enqueue(t, workspace, source.Now())
	runner := newRunner(t, workspace, source, "worker", jobs.HandlerFunc(func(context.Context, jobs.Job) (jobs.Outcome, error) {
		return jobs.Outcome{}, nil
	}), func(uint32) time.Duration { return time.Second })
	result, err := runner.RunNext(context.Background())
	if !errors.Is(err, jobs.ErrInvalidOutcome) || result.State != "RETRY_WAIT" {
		t.Fatalf("invalid outcome = %#v, %v", result, err)
	}
	assertJob(t, workspace, "RETRY_WAIT", 1, 0, nil)
}

func TestPayloadHashIsVerifiedBeforeHandlerExecution(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspace(t)
	source := newMutableClock(time.Date(2026, 8, 4, 6, 0, 0, 0, time.UTC))
	enqueue(t, workspace, source.Now())
	if _, err := workspace.Database.Exec(`DROP TRIGGER jobs_immutable_input_guard`); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Database.Exec(`UPDATE jobs SET payload_proto=? WHERE id=?`, progressProto("tampered"), jobID); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	runner := newRunner(t, workspace, source, "worker", jobs.HandlerFunc(func(context.Context, jobs.Job) (jobs.Outcome, error) {
		calls.Add(1)
		return jobs.Outcome{Complete: true}, nil
	}), func(uint32) time.Duration { return time.Second })
	result, err := runner.RunNext(context.Background())
	if !errors.Is(err, jobs.ErrPayloadTampered) || result.Claimed || calls.Load() != 0 {
		t.Fatalf("tampered payload = %#v, %v, calls=%d", result, err, calls.Load())
	}
	assertJob(t, workspace, "PENDING", 0, 0, nil)
}

func TestSameWorkerLeaseABARejectsStaleResultAndCommit(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspace(t)
	initial := time.Date(2026, 8, 4, 7, 0, 0, 0, time.UTC)
	source := newMutableClock(initial)
	enqueue(t, workspace, source.Now())
	if _, err := workspace.Database.Exec(`CREATE TABLE job_domain_effects(value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	releaseSecond := make(chan struct{})
	handler := jobs.HandlerFunc(func(ctx context.Context, job jobs.Job) (jobs.Outcome, error) {
		if job.Attempt == 1 {
			close(firstStarted)
			<-releaseFirst
			return jobs.Outcome{
				Complete: true, ResultProto: progressProto("stale"),
				Commit: func(ctx context.Context, transaction *jobs.DomainTransaction) error {
					return transaction.InsertRow(ctx, "job_domain_effects", []string{"value"}, "stale")
				},
			}, nil
		}
		close(secondStarted)
		<-releaseSecond
		return jobs.Outcome{
			Complete: true, ResultProto: progressProto("winner"),
			Commit: func(ctx context.Context, transaction *jobs.DomainTransaction) error {
				return transaction.InsertRow(ctx, "job_domain_effects", []string{"value"}, "winner")
			},
		}, nil
	})
	first := newRunner(t, workspace, source, "same-worker", handler, func(uint32) time.Duration { return time.Second })
	second := newRunner(t, workspace, source, "same-worker", handler, func(uint32) time.Duration { return time.Second })
	firstDone := make(chan error, 1)
	go func() { _, err := first.RunNext(context.Background()); firstDone <- err }()
	<-firstStarted
	source.Set(initial.Add(2 * time.Minute))
	secondDone := make(chan error, 1)
	go func() { _, err := second.RunNext(context.Background()); secondDone <- err }()
	<-secondStarted
	close(releaseFirst)
	if err := <-firstDone; !errors.Is(err, jobs.ErrLeaseLost) {
		t.Fatalf("stale claimant error = %v, want ErrLeaseLost", err)
	}
	close(releaseSecond)
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
	assertJob(t, workspace, "COMPLETED", 2, 1, progressProto("winner"))
	rows, err := workspace.Database.Query(`SELECT value FROM job_domain_effects`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var effects []string
	for rows.Next() {
		var effect string
		if err := rows.Scan(&effect); err != nil {
			t.Fatal(err)
		}
		effects = append(effects, effect)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(effects) != 1 || effects[0] != "winner" {
		t.Fatalf("domain effects = %v", effects)
	}
	var checkpoints int
	if err := workspace.Database.QueryRow(`SELECT count(*) FROM job_checkpoints WHERE job_id=?`, jobID).Scan(&checkpoints); err != nil {
		t.Fatal(err)
	}
	if checkpoints != 0 {
		t.Fatalf("stale claimant persisted %d checkpoints", checkpoints)
	}
}

func TestDomainCommitCapabilityCannotCommitRollbackOrWriteRunnerTables(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspace(t)
	source := newMutableClock(time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC))
	enqueue(t, workspace, source.Now())
	if _, err := workspace.Database.Exec(`CREATE TABLE job_domain_effects(value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	handler := jobs.HandlerFunc(func(ctx context.Context, job jobs.Job) (jobs.Outcome, error) {
		return jobs.Outcome{
			Complete: true, ResultProto: progressProto("blocked"),
			Commit: func(ctx context.Context, transaction *jobs.DomainTransaction) error {
				type transactionController interface {
					Commit() error
					Rollback() error
				}
				if _, exposed := any(transaction).(transactionController); exposed {
					t.Fatal("domain commit capability exposes transaction control")
				}
				if _, exposed := reflect.TypeOf(transaction).MethodByName("ExecContext"); exposed {
					t.Fatal("domain commit capability exposes arbitrary SQL")
				}
				return transaction.InsertRow(ctx, "jobs", []string{"state"}, "COMPLETED")
			},
		}, nil
	})
	runner := newRunner(t, workspace, source, "worker", handler, func(uint32) time.Duration { return time.Second })
	result, err := runner.RunNext(context.Background())
	if !errors.Is(err, jobs.ErrDomainWriteDenied) || result.State != "RETRY_WAIT" {
		t.Fatalf("restricted domain commit = %#v, %v", result, err)
	}
	assertJob(t, workspace, "RETRY_WAIT", 1, 0, nil)
	var effects int
	if err := workspace.Database.QueryRow(`SELECT count(*) FROM job_domain_effects`).Scan(&effects); err != nil {
		t.Fatal(err)
	}
	if effects != 0 {
		t.Fatalf("restricted callback committed %d domain effects", effects)
	}
}

func TestRunnerRejectsMixedCaseProtectedDomainTables(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspace(t)
	for _, table := range []string{"JOBS", "Jobs", "JOB_CHECKPOINTS", "Job_Checkpoints"} {
		_, err := jobs.NewRunner(jobs.Config{
			Database: workspace.Database.DB,
			Clock:    clock.NewFixed(time.Date(2026, 8, 4, 9, 30, 0, 0, time.UTC)),
			Handler: jobs.HandlerFunc(func(context.Context, jobs.Job) (jobs.Outcome, error) {
				return jobs.Outcome{Complete: true, ResultProto: progressProto("unused")}, nil
			}),
			LeaseDuration: time.Minute,
			MaxAttempts:   1,
			RetryBackoff:  func(uint32) time.Duration { return time.Second },
			WorkerID:      "worker",
			DomainTables:  []string{table},
		})
		if !errors.Is(err, jobs.ErrInvalidConfig) {
			t.Errorf("protected table %q error = %v", table, err)
		}
	}
}

func TestExpiredLeaseRejectsStaleCheckpoint(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspace(t)
	initial := time.Date(2026, 8, 4, 8, 0, 0, 0, time.UTC)
	source := newMutableClock(initial)
	enqueue(t, workspace, source.Now())
	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	releaseSecond := make(chan struct{})
	handler := jobs.HandlerFunc(func(ctx context.Context, job jobs.Job) (jobs.Outcome, error) {
		if job.Attempt == 1 {
			close(firstStarted)
			<-releaseFirst
			return jobs.Outcome{CheckpointProto: progressProto("stale_checkpoint")}, nil
		}
		close(secondStarted)
		<-releaseSecond
		return jobs.Outcome{Complete: true, ResultProto: progressProto("winner")}, nil
	})
	first := newRunner(t, workspace, source, "same-worker", handler, func(uint32) time.Duration { return time.Second })
	second := newRunner(t, workspace, source, "same-worker", handler, func(uint32) time.Duration { return time.Second })
	firstDone := make(chan error, 1)
	go func() { _, err := first.RunNext(context.Background()); firstDone <- err }()
	<-firstStarted
	source.Set(initial.Add(2 * time.Minute))
	secondDone := make(chan error, 1)
	go func() { _, err := second.RunNext(context.Background()); secondDone <- err }()
	<-secondStarted
	close(releaseFirst)
	if err := <-firstDone; !errors.Is(err, jobs.ErrLeaseLost) {
		t.Fatalf("stale checkpoint claimant error = %v", err)
	}
	close(releaseSecond)
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
	var checkpoints int
	if err := workspace.Database.QueryRow(`SELECT count(*) FROM job_checkpoints WHERE job_id=?`, jobID).Scan(&checkpoints); err != nil {
		t.Fatal(err)
	}
	if checkpoints != 0 {
		t.Fatalf("stale claimant persisted %d checkpoints", checkpoints)
	}
	assertJob(t, workspace, "COMPLETED", 2, 1, progressProto("winner"))
}

type mutableClock struct{ value atomic.Value }

func newMutableClock(initial time.Time) *mutableClock {
	clock := &mutableClock{}
	clock.value.Store(initial.UTC())
	return clock
}
func (clock *mutableClock) Now() time.Time      { return clock.value.Load().(time.Time) }
func (clock *mutableClock) Set(value time.Time) { clock.value.Store(value.UTC()) }

func newRunner(t *testing.T, workspace *testkit.EncryptedWorkspace, source clock.Clock, worker string, handler jobs.Handler, backoff jobs.Backoff) *jobs.Runner {
	t.Helper()
	runner, err := jobs.NewRunner(jobs.Config{
		Database: workspace.Database.DB, Clock: source, Handler: handler,
		LeaseDuration: time.Minute, MaxAttempts: 3, RetryBackoff: backoff, WorkerID: worker,
		DomainTables: []string{"job_domain_effects"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return runner
}

func enqueue(t *testing.T, workspace *testkit.EncryptedWorkspace, now time.Time) {
	t.Helper()
	err := jobs.Enqueue(context.Background(), workspace.Database.DB, now, jobs.JobSpec{
		ID: jobID, Kind: "test", OperationKey: "018f0000-0000-7000-8000-000000000060", PayloadProto: progressProto("payload"),
	})
	if err != nil {
		t.Fatal(err)
	}
}

func progressProto(stage string) []byte {
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(&tammyv1.JobProgress{Stage: stage, CompletedUnits: 1, TotalUnits: 1})
	if err != nil {
		panic(err)
	}
	return encoded
}

func assertJob(t *testing.T, workspace *testkit.EncryptedWorkspace, state string, attempts, commitPoint int, result []byte) {
	t.Helper()
	var gotState string
	var gotAttempts, gotCommitPoint int
	var gotResult []byte
	if err := workspace.Database.QueryRow(`SELECT state, attempt_count, commit_point_reached, result_proto FROM jobs WHERE id=?`, jobID).Scan(&gotState, &gotAttempts, &gotCommitPoint, &gotResult); err != nil {
		t.Fatal(err)
	}
	if gotState != state || gotAttempts != attempts || gotCommitPoint != commitPoint || string(gotResult) != string(result) {
		t.Fatalf("job = state:%s attempts:%d commit:%d result:%q", gotState, gotAttempts, gotCommitPoint, gotResult)
	}
}
