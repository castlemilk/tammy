//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package app

import (
	"context"
	"sync"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/audit"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

type localTestAuditMirror struct {
	mu         sync.Mutex
	baselines  map[string]*tammyv1.AuditMirrorBaseline
	lifecycles map[string]*audit.InitialMirrorLifecycle
}

func newLocalTestAuditMirror() *localTestAuditMirror {
	return &localTestAuditMirror{baselines: map[string]*tammyv1.AuditMirrorBaseline{}, lifecycles: map[string]*audit.InitialMirrorLifecycle{}}
}
func (store *localTestAuditMirror) Load(_ context.Context, workspaceID string) (*tammyv1.AuditMirrorBaseline, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	value := store.baselines[workspaceID]
	if value == nil {
		return nil, audit.ErrMirrorMissing
	}
	return proto.Clone(value).(*tammyv1.AuditMirrorBaseline), nil
}
func (store *localTestAuditMirror) CompareAndSwap(_ context.Context, expected, target *tammyv1.AuditMirrorBaseline) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if !proto.Equal(store.baselines[target.WorkspaceId], expected) {
		return audit.ErrMirrorConflict
	}
	store.baselines[target.WorkspaceId] = proto.Clone(target).(*tammyv1.AuditMirrorBaseline)
	return nil
}
func (store *localTestAuditMirror) LoadInitialMirrorLifecycle(_ context.Context, workspaceID string) (*audit.InitialMirrorLifecycle, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	value := store.lifecycles[workspaceID]
	if value == nil {
		return nil, audit.ErrMirrorMissing
	}
	copy := *value
	return &copy, nil
}
func (store *localTestAuditMirror) SaveInitialMirrorLifecycle(_ context.Context, value *audit.InitialMirrorLifecycle) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	copy := *value
	store.lifecycles[value.WorkspaceID] = &copy
	return nil
}

func TestLocalAuditRuntimeWorkspaceClassificationIsRaceSafe(t *testing.T) {
	runtime := &localAuditRuntime{}
	states := []localAuditWorkspaceState{
		{},
		{workspaceID: "legacy", legacy: true},
		{workspaceID: "moved", movedReadOnly: true},
		{workspaceID: "trusted"},
	}
	var workers sync.WaitGroup
	var invalidMu sync.Mutex
	var invalid localAuditWorkspaceState
	hasInvalid := false
	for worker := 0; worker < 32; worker++ {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			for iteration := 0; iteration < 500; iteration++ {
				if worker%4 == 0 {
					runtime.setWorkspaceState(states[(worker+iteration)%len(states)])
					continue
				}
				state := runtime.workspaceState()
				if state.legacy && state.movedReadOnly || (state.workspaceID == "legacy") != state.legacy || (state.workspaceID == "moved") != state.movedReadOnly {
					invalidMu.Lock()
					if !hasInvalid {
						invalid, hasInvalid = state, true
					}
					invalidMu.Unlock()
				}
			}
		}(worker)
	}
	workers.Wait()
	if hasInvalid {
		t.Fatalf("inconsistent audit workspace classification: %+v", invalid)
	}
}
