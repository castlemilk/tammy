package idempotency

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/platform/canonical"
	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
	"google.golang.org/protobuf/proto"
)

type Decision uint8

const (
	DecisionExecute Decision = iota + 1
	DecisionReplay
)

type WaitFunc func(context.Context, time.Duration) error
type ObserveFunc func(context.Context, Scope) (Record, error)

// NewObserver binds election polling to a caller-supplied fresh read owner,
// such as the SQLCipher database pool rather than the active write transaction.
func NewObserver(executor Executor) (ObserveFunc, error) {
	repository, err := NewRepository(executor)
	if err != nil {
		return nil, err
	}
	return repository.load, nil
}

type Config struct {
	Clock   clock.Clock
	Wait    WaitFunc
	Observe ObserveFunc
}

type Elector struct {
	clock   clock.Clock
	wait    WaitFunc
	observe ObserveFunc
}

type Election struct {
	Decision       Decision
	Scope          Scope
	HashVersion    string
	RequestType    string
	NormalizedHash [sha256.Size]byte
	ResultType     string
	ResultProto    []byte
	Attempt        uint32
}

func NewElector(config Config) (*Elector, error) {
	if config.Clock == nil || config.Observe == nil {
		return nil, ErrInvalidElection
	}
	if config.Wait == nil {
		config.Wait = func(ctx context.Context, duration time.Duration) error {
			timer := time.NewTimer(duration)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		}
	}
	return &Elector{clock: config.Clock, wait: config.Wait, observe: config.Observe}, nil
}

func (elector *Elector) Elect(ctx context.Context, executor Executor, scope Scope, request proto.Message) (Election, error) {
	if elector == nil || elector.clock == nil || executor == nil || !validScope(scope) || request == nil || !request.ProtoReflect().IsValid() {
		return Election{}, ErrInvalidElection
	}
	startedAt := elector.clock.Now().UTC()
	deadline := startedAt.Add(2 * time.Second)
	semantic, err := canonical.SemanticHashV1(request)
	if err != nil {
		return Election{}, err
	}
	requestType := string(request.ProtoReflect().Descriptor().FullName())
	record := Record{Scope: scope, SemanticHashVersion: semantic.Version, RequestType: requestType,
		NormalizedHash: semantic.Sum, Attempt: 1, CreatedAt: startedAt}
	repository, err := NewRepository(executor)
	if err != nil {
		return Election{}, err
	}
	inserted, err := repository.insertElection(ctx, record)
	if errors.Is(err, ErrElectionBusy) {
		return elector.awaitInFlight(ctx, repository, scope, semantic.Version, requestType, semantic.Sum, deadline)
	}
	if err != nil {
		return Election{}, err
	}
	if inserted {
		return electionFromRecord(DecisionExecute, record), nil
	}
	retained, err := repository.load(ctx, scope)
	if err != nil {
		return Election{}, err
	}
	if retained.SemanticHashVersion != semantic.Version || retained.RequestType != requestType ||
		!bytes.Equal(retained.NormalizedHash[:], semantic.Sum[:]) {
		return Election{}, ErrConflict
	}
	switch retained.Outcome {
	case "COMMITTED":
		return electionFromRecord(DecisionReplay, retained), nil
	case "ELECTED":
		return elector.awaitInFlight(ctx, repository, scope, semantic.Version, requestType, semantic.Sum, deadline)
	case "FAILED":
		retried, retryErr := repository.retryFailed(ctx, retained)
		if retryErr != nil {
			return Election{}, retryErr
		}
		return electionFromRecord(DecisionExecute, retried), nil
	default:
		return Election{}, ErrRepository
	}
}

func (elector *Elector) awaitInFlight(ctx context.Context, repository *Repository, scope Scope,
	hashVersion, requestType string, normalized [sha256.Size]byte, deadline time.Time) (Election, error) {
	for poll := 0; poll < 8 && elector.clock.Now().UTC().Before(deadline); poll++ {
		remaining := deadline.Sub(elector.clock.Now().UTC())
		delay := min(250*time.Millisecond, remaining)
		if err := elector.wait(ctx, delay); err != nil {
			return Election{}, err
		}
		var observed Record
		var loadErr error
		observed, loadErr = elector.observe(ctx, scope)
		if loadErr != nil {
			if errors.Is(loadErr, ErrRepository) {
				continue
			}
			return Election{}, loadErr
		}
		if observed.Scope != scope {
			return Election{}, ErrRepository
		}
		if observed.SemanticHashVersion != hashVersion || observed.RequestType != requestType ||
			!bytes.Equal(observed.NormalizedHash[:], normalized[:]) {
			return Election{}, ErrConflict
		}
		switch observed.Outcome {
		case "COMMITTED":
			return electionFromRecord(DecisionReplay, observed), nil
		case "FAILED":
			retried, retryErr := repository.retryFailed(ctx, observed)
			if retryErr != nil {
				return Election{}, retryErr
			}
			return electionFromRecord(DecisionExecute, retried), nil
		case "ELECTED":
		default:
			return Election{}, ErrRepository
		}
	}
	return Election{}, ErrAborted
}

// Fail records one command attempt's stable failure under the caller's transaction.
func (elector *Elector) Fail(ctx context.Context, executor Executor, election Election, failureCode string) error {
	if elector == nil || executor == nil || election.Decision != DecisionExecute || failureCode == "" {
		return ErrInvalidElection
	}
	repository, err := NewRepository(executor)
	if err != nil {
		return err
	}
	return repository.fail(ctx, Record{Scope: election.Scope, SemanticHashVersion: election.HashVersion,
		RequestType: election.RequestType, NormalizedHash: election.NormalizedHash, Attempt: election.Attempt},
		failureCode, elector.clock.Now().UTC())
}

func (elector *Elector) Complete(
	ctx context.Context,
	executor Executor,
	election Election,
	result proto.Message,
	resourceID string,
) ([]byte, error) {
	if elector == nil || executor == nil || election.Decision != DecisionExecute || result == nil || !result.ProtoReflect().IsValid() {
		return nil, ErrInvalidElection
	}
	resultProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(result)
	if err != nil {
		return nil, ErrInvalidElection
	}
	record := Record{Scope: election.Scope, SemanticHashVersion: election.HashVersion,
		RequestType: election.RequestType, NormalizedHash: election.NormalizedHash, Attempt: election.Attempt}
	repository, err := NewRepository(executor)
	if err != nil {
		return nil, err
	}
	if err := repository.complete(ctx, record, string(result.ProtoReflect().Descriptor().FullName()), resultProto,
		resourceID, elector.clock.Now().UTC()); err != nil {
		return nil, err
	}
	return append([]byte(nil), resultProto...), nil
}

func electionFromRecord(decision Decision, record Record) Election {
	return Election{Decision: decision, Scope: record.Scope, HashVersion: record.SemanticHashVersion,
		RequestType: record.RequestType, NormalizedHash: record.NormalizedHash,
		ResultType: record.ResultType, ResultProto: append([]byte(nil), record.ResultProto...), Attempt: record.Attempt}
}

func validScope(scope Scope) bool {
	return scope.WorkspaceID != "" && scope.ActorUserID != "" && scope.RPCName != "" && scope.OperationKey != ""
}
