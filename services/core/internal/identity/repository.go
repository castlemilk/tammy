package identity

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
)

var (
	ErrRepositoryConflict  = errors.New("identity: repository stale write conflict")
	ErrRepositoryIntegrity = errors.New("identity: repository integrity failure")
)

type userRoleRowKey struct {
	userID   string
	roleCode string
}

type passwordHistoryRowKey struct {
	userID  string
	ordinal int
}

type repositoryPersistence struct {
	users              map[string]uint64
	userRoles          map[userRoleRowKey]uint64
	passwordHistory    map[passwordHistoryRowKey]uint64
	sessions           map[string]uint64
	factors            map[string]uint64
	assertions         map[string]uint64
	idempotency        map[string]uint64
	idempotencyDigests map[string][32]byte
}

func newRepositoryPersistence() repositoryPersistence {
	return repositoryPersistence{
		users: make(map[string]uint64), userRoles: make(map[userRoleRowKey]uint64),
		passwordHistory: make(map[passwordHistoryRowKey]uint64), sessions: make(map[string]uint64),
		factors: make(map[string]uint64), assertions: make(map[string]uint64),
		idempotency: make(map[string]uint64), idempotencyDigests: make(map[string][32]byte),
	}
}

func (persistence repositoryPersistence) clone() repositoryPersistence {
	cloned := newRepositoryPersistence()
	for key, value := range persistence.users {
		cloned.users[key] = value
	}
	for key, value := range persistence.userRoles {
		cloned.userRoles[key] = value
	}
	for key, value := range persistence.passwordHistory {
		cloned.passwordHistory[key] = value
	}
	for key, value := range persistence.sessions {
		cloned.sessions[key] = value
	}
	for key, value := range persistence.factors {
		cloned.factors[key] = value
	}
	for key, value := range persistence.assertions {
		cloned.assertions[key] = value
	}
	for key, value := range persistence.idempotency {
		cloned.idempotency[key] = value
	}
	for key, value := range persistence.idempotencyDigests {
		cloned.idempotencyDigests[key] = value
	}
	return cloned
}

type userRecord struct {
	ID                     string
	Version                uint64
	Username               string
	DisplayName            string
	State                  tammyv1.UserState
	Roles                  []tammyv1.Role
	Password               workspace.PasswordVerifier
	PasswordHistory        []workspace.PasswordVerifier
	ActivationHash         []byte
	ActivationConsumedHash []byte
	ActivationEncrypted    []byte
	ActivationExpires      time.Time
	ActivationFails        int
	ActivationSessionID    string
	SignInFailures         []time.Time
	LockedUntil            time.Time
}

type sessionRecord struct {
	ID         string
	UserID     string
	State      tammyv1.SessionState
	CreatedAt  time.Time
	LastActive time.Time
	ExpiresAt  time.Time
	EndedAt    time.Time
}

type factorRecord struct {
	ID              string
	UserID          string
	Version         uint64
	State           tammyv1.FactorState
	CreatedAt       time.Time
	EncryptedSecret []byte
	LastCounter     int64
}

type assertionRecord struct {
	ID        string
	UserID    string
	SessionID string
	Purpose   string
	Asserted  time.Time
	Consumed  bool
}

type idempotencyRecord struct {
	Command           string
	SemanticHash      string
	ActorUserID       string
	UserID            string
	FactorID          string
	SessionID         string
	ResponseEncrypted []byte
}

type repositoryState struct {
	Users       map[string]*userRecord
	Usernames   map[string]string
	Sessions    map[string]*sessionRecord
	Factors     map[string]*factorRecord
	Assertions  map[string]*assertionRecord
	Idempotency map[string]idempotencyRecord
	persistence repositoryPersistence
}

func newRepositoryState() repositoryState {
	return repositoryState{
		Users: make(map[string]*userRecord), Usernames: make(map[string]string),
		Sessions: make(map[string]*sessionRecord), Factors: make(map[string]*factorRecord),
		Assertions: make(map[string]*assertionRecord), Idempotency: make(map[string]idempotencyRecord),
		persistence: newRepositoryPersistence(),
	}
}

func normalizeRepositoryState(state *repositoryState) {
	if state.Users == nil {
		state.Users = make(map[string]*userRecord)
	}
	if state.Usernames == nil {
		state.Usernames = make(map[string]string)
	}
	if state.Sessions == nil {
		state.Sessions = make(map[string]*sessionRecord)
	}
	if state.Factors == nil {
		state.Factors = make(map[string]*factorRecord)
	}
	if state.Assertions == nil {
		state.Assertions = make(map[string]*assertionRecord)
	}
	if state.Idempotency == nil {
		state.Idempotency = make(map[string]idempotencyRecord)
	}
	if state.persistence.users == nil {
		state.persistence = newRepositoryPersistence()
	}
}

type Repository interface {
	Load(context.Context) (repositoryState, error)
	Save(context.Context, repositoryState) error
	Mutate(context.Context, func(context.Context, workspace.MutationExecutor, *repositoryState) error) error
}

type executorRepository interface {
	LoadFrom(context.Context, workspace.MutationExecutor) (repositoryState, error)
	SaveTo(context.Context, workspace.MutationExecutor, repositoryState) error
}

type MemoryRepository struct {
	mu    sync.Mutex
	state repositoryState
}

func NewMemoryRepository() *MemoryRepository { return &MemoryRepository{state: newRepositoryState()} }

func (repository *MemoryRepository) Load(_ context.Context) (repositoryState, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return cloneState(repository.state)
}

func (repository *MemoryRepository) Save(_ context.Context, state repositoryState) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	cloned, err := cloneState(state)
	if err != nil {
		return err
	}
	repository.state = cloned
	return nil
}

func (repository *MemoryRepository) Mutate(ctx context.Context, work func(context.Context, workspace.MutationExecutor, *repositoryState) error) error {
	if work == nil {
		return ErrRepositoryIntegrity
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	state, err := cloneState(repository.state)
	if err != nil {
		return err
	}
	if err := work(ctx, nil, &state); err != nil {
		return err
	}
	committed, err := cloneState(state)
	if err != nil {
		return err
	}
	repository.state = committed
	return nil
}

func cloneState(state repositoryState) (repositoryState, error) {
	payload, err := json.Marshal(state)
	if err != nil {
		return repositoryState{}, ErrRepositoryIntegrity
	}
	var cloned repositoryState
	if err := json.Unmarshal(payload, &cloned); err != nil {
		return repositoryState{}, ErrRepositoryIntegrity
	}
	normalizeRepositoryState(&cloned)
	cloned.persistence = state.persistence.clone()
	return cloned, nil
}
