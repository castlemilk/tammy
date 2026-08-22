package vault

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestAADGoldenUsesExactSemanticConcatenation(t *testing.T) {
	installationID := make([]byte, 16)
	for i := range installationID {
		installationID[i] = byte(i)
	}
	scopeBytes := make([]byte, 32)
	for i := range scopeBytes {
		scopeBytes[i] = byte(0x20 + i)
	}
	got, err := envelopeAADExact(installationID, hex.EncodeToString(scopeBytes), CredentialKind, "sim-v1")
	if err != nil {
		t.Fatal(err)
	}
	want := append([]byte("tammy-sbr-vault-v1"), installationID...)
	want = append(want, scopeBytes...)
	want = append(want, byte(CredentialKind))
	want = append(want, []byte("sim-v1")...)
	if !bytes.Equal(got, want) {
		t.Fatalf("AAD\n got %x\nwant %x", got, want)
	}
}

func TestClosedMutationSurfaceHasExactlyFiveKindsAndNoProductReplace(t *testing.T) {
	want := []MutationKind{ImportCredentialMutation, ReplaceCredentialMutation, RemoveCredentialMutation, ImportProductIDMutation, RemoveProductIDMutation}
	for index, kind := range want {
		if int(kind) != index+1 {
			t.Fatalf("kind %d = %d", index, kind)
		}
	}
	if MutationKind(6).valid() {
		t.Fatal("sixth mutation kind accepted")
	}
}

type mismatchComponent struct {
	unlockCalls int
	metadata    CredentialMetadata
}

func newReviewVault(t *testing.T, store *memoryStore, component CredentialComponent) *Vault {
	t.Helper()
	entropy := deterministicReader(61)
	v, err := newTestChannelVault(store, &entropy, component, syntheticOpener{files: map[string][]byte{"/synthetic/credential.p12": []byte("synthetic-p12"), "/synthetic/replacement.p12": []byte("replacement")}})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func reopenReviewVault(t *testing.T, store *memoryStore, component CredentialComponent) *Vault {
	t.Helper()
	entropy := deterministicReader(99)
	v, err := newTestChannelVault(store, &entropy, component, syntheticOpener{files: map[string][]byte{"/synthetic/credential.p12": []byte("synthetic-p12"), "/synthetic/replacement.p12": []byte("replacement")}})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func (c *mismatchComponent) Import([]byte, []byte) (CredentialRecord, error) {
	return CredentialRecord{Opaque: []byte("opaque"), Metadata: c.metadata}, nil
}
func (c *mismatchComponent) Unlock([]byte, []byte) ([]byte, error) {
	c.unlockCalls++
	return []byte("key"), nil
}

func TestCredentialABNMustEqualCanonicalScopeAtStageAndUnlock(t *testing.T) {
	store := newMemoryStore()
	component := &mismatchComponent{metadata: CredentialMetadata{Fingerprint: "fingerprint", CanonicalABN: "53004085616", ComponentVersion: "sim-v1"}}
	v := newReviewVault(t, store, component)
	mutation := Mutation{Kind: ImportCredentialMutation, OperationID: "018f0000-0000-7000-8000-000000000081", Scope: testScope(), SelectedPath: "/synthetic/credential.p12", Password: []byte("pw")}
	if _, err := v.StageCreate(mutation); !errors.Is(err, ErrVaultBindingMismatch) {
		t.Fatalf("stage mismatch = %v", err)
	}

	component.metadata.CanonicalABN = "11 000 000 560"
	mutation.OperationID = "018f0000-0000-7000-8000-000000000082"
	if _, err := v.StageCreate(mutation); err != nil {
		t.Fatal(err)
	}
	if err := v.Promote(mutation.OperationID); err != nil {
		t.Fatal(err)
	}
	account, _ := v.credentialAccount(testScope())
	record, err := v.readCredentialWithAAD(account, testScope())
	if err != nil {
		t.Fatal(err)
	}
	record.Metadata.CanonicalABN = "53004085616"
	plain := encodeCredentialRecord(record)
	clear(record.Opaque)
	scopeDigest := stringsTrimAccount(account, v.prefix()+"credential/")
	envelope, err := v.seal(scopeDigest, CredentialKind, record.Metadata.ComponentVersion, plain)
	clear(plain)
	if err != nil {
		t.Fatal(err)
	}
	store.items[account] = envelope
	if _, err := v.Unlock(testScope(), []byte("pw")); !errors.Is(err, ErrVaultBindingMismatch) {
		t.Fatalf("unlock mismatch = %v", err)
	}
	if component.unlockCalls != 0 {
		t.Fatal("component called for mismatched ABN")
	}
}

func TestCrossWorkspaceOrganisationAndABNAreDenied(t *testing.T) {
	store := newMemoryStore()
	v := newReviewVault(t, store, &syntheticComponent{})
	op := "018f0000-0000-7000-8000-000000000091"
	if _, err := v.StageCreate(Mutation{Kind: ImportCredentialMutation, OperationID: op, Scope: testScope(), SelectedPath: "/synthetic/credential.p12", Password: []byte("pw")}); err != nil {
		t.Fatal(err)
	}
	if err := v.Promote(op); err != nil {
		t.Fatal(err)
	}
	variants := []Scope{
		{WorkspaceID: "workspace-2", OrganisationID: "organisation-1", CanonicalABN: "11000000560"},
		{WorkspaceID: "workspace-1", OrganisationID: "organisation-2", CanonicalABN: "11000000560"},
		{WorkspaceID: "workspace-1", OrganisationID: "organisation-1", CanonicalABN: "53004085616"},
	}
	for _, scope := range variants {
		if _, err := v.ReadMetadata(scope); !errors.Is(err, ErrVaultMissing) {
			t.Fatalf("cross binding %+v = %v", scope, err)
		}
	}
	if _, err := v.ReadMetadata(Scope{WorkspaceID: "workspace-1", OrganisationID: "organisation-1", CanonicalABN: "11-000-000-560"}); err != nil {
		t.Fatalf("canonical scope denied: %v", err)
	}
}

func TestPendingReservationPreventsInterleavingAcrossVaultInstances(t *testing.T) {
	store := newMemoryStore()
	v1 := newReviewVault(t, store, &syntheticComponent{})
	v2 := reopenReviewVault(t, store, &syntheticComponent{})
	create := Mutation{Kind: ImportCredentialMutation, OperationID: "018f0000-0000-7000-8000-0000000000a1", Scope: testScope(), SelectedPath: "/synthetic/credential.p12", Password: []byte("pw")}
	if _, err := v1.StageCreate(create); err != nil {
		t.Fatal(err)
	}
	if _, err := v2.StageCreate(Mutation{Kind: ImportCredentialMutation, OperationID: "018f0000-0000-7000-8000-0000000000a2", Scope: testScope(), SelectedPath: "/synthetic/credential.p12", Password: []byte("pw")}); !errors.Is(err, ErrVaultPending) {
		t.Fatalf("concurrent create = %v", err)
	}
	if err := v1.Promote(create.OperationID); err != nil {
		t.Fatal(err)
	}
	replace := Mutation{Kind: ReplaceCredentialMutation, OperationID: "018f0000-0000-7000-8000-0000000000a3", Scope: testScope(), SelectedPath: "/synthetic/replacement.p12", Password: []byte("pw")}
	if _, err := v1.StageReplace(replace); err != nil {
		t.Fatal(err)
	}
	if err := v2.StageDelete(Mutation{Kind: RemoveCredentialMutation, OperationID: "018f0000-0000-7000-8000-0000000000a4", Scope: testScope()}); !errors.Is(err, ErrVaultPending) {
		t.Fatalf("delete interleave = %v", err)
	}
	if err := v1.Promote(replace.OperationID); err != nil {
		t.Fatal(err)
	}
}

func TestPromoteReconcilesAppliedCreateReplaceDeleteAndRejectsNewerValue(t *testing.T) {
	store := newMemoryStore()
	v := newReviewVault(t, store, &syntheticComponent{})
	create := Mutation{Kind: ImportCredentialMutation, OperationID: "018f0000-0000-7000-8000-0000000000b1", Scope: testScope(), SelectedPath: "/synthetic/credential.p12", Password: []byte("pw")}
	if _, err := v.StageCreate(create); err != nil {
		t.Fatal(err)
	}
	pending, _ := v.readPending(create.OperationID)
	if err := store.Create(pending.Account, pending.Envelope, v.policy); err != nil {
		t.Fatal(err)
	}
	clear(pending.Envelope)
	if err := v.Promote(create.OperationID); err != nil {
		t.Fatalf("create reconcile: %v", err)
	}

	replace := Mutation{Kind: ReplaceCredentialMutation, OperationID: "018f0000-0000-7000-8000-0000000000b2", Scope: testScope(), SelectedPath: "/synthetic/replacement.p12", Password: []byte("pw")}
	if _, err := v.StageReplace(replace); err != nil {
		t.Fatal(err)
	}
	pending, _ = v.readPending(replace.OperationID)
	if err := store.CompareAndReplace(pending.Account, pending.ExpectedDigest, pending.Envelope, v.policy); err != nil {
		t.Fatal(err)
	}
	clear(pending.Envelope)
	if err := v.Promote(replace.OperationID); err != nil {
		t.Fatalf("replace reconcile: %v", err)
	}

	remove := Mutation{Kind: RemoveCredentialMutation, OperationID: "018f0000-0000-7000-8000-0000000000b3", Scope: testScope()}
	if err := v.StageDelete(remove); err != nil {
		t.Fatal(err)
	}
	pending, _ = v.readPending(remove.OperationID)
	if err := store.CompareAndDelete(pending.Account, pending.ExpectedDigest); err != nil {
		t.Fatal(err)
	}
	if err := v.Promote(remove.OperationID); err != nil {
		t.Fatalf("delete reconcile: %v", err)
	}

	create.OperationID = "018f0000-0000-7000-8000-0000000000b4"
	if _, err := v.StageCreate(create); err != nil {
		t.Fatal(err)
	}
	if err := v.Promote(create.OperationID); err != nil {
		t.Fatal(err)
	}
	replace.OperationID = "018f0000-0000-7000-8000-0000000000b5"
	if _, err := v.StageReplace(replace); err != nil {
		t.Fatal(err)
	}
	pending, _ = v.readPending(replace.OperationID)
	store.mu.Lock()
	store.items[pending.Account] = []byte("authenticated-newer-generation")
	store.mu.Unlock()
	if err := v.Promote(replace.OperationID); !errors.Is(err, ErrVaultCASConflict) {
		t.Fatalf("newer replacement overwritten: %v", err)
	}
	store.mu.Lock()
	got := append([]byte(nil), store.items[pending.Account]...)
	store.mu.Unlock()
	if string(got) != "authenticated-newer-generation" {
		t.Fatal("newer value overwritten")
	}
	clear(got)
	clear(pending.Envelope)
}

func TestPromoteCASRejectsCreateReplaceDeleteInterleavingsAcrossRestart(t *testing.T) {
	cases := []struct {
		name string
		kind MutationKind
	}{{"create", ImportCredentialMutation}, {"replace", ReplaceCredentialMutation}, {"delete", RemoveCredentialMutation}}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			mutationKind := testCase.kind
			store := newMemoryStore()
			v := newReviewVault(t, store, &syntheticComponent{})
			if mutationKind != ImportCredentialMutation {
				create := Mutation{Kind: ImportCredentialMutation, OperationID: "018f0000-0000-7000-8000-0000000000c0", Scope: testScope(), SelectedPath: "/synthetic/credential.p12", Password: []byte("pw")}
				if _, err := v.StageCreate(create); err != nil {
					t.Fatal(err)
				}
				if err := v.Promote(create.OperationID); err != nil {
					t.Fatal(err)
				}
			}

			mutation := Mutation{Kind: mutationKind, OperationID: "018f0000-0000-7000-8000-0000000000c1", Scope: testScope(), SelectedPath: "/synthetic/replacement.p12", Password: []byte("pw")}
			switch mutationKind {
			case ImportCredentialMutation:
				if _, err := v.StageCreate(mutation); err != nil {
					t.Fatal(err)
				}
			case ReplaceCredentialMutation:
				if _, err := v.StageReplace(mutation); err != nil {
					t.Fatal(err)
				}
			case RemoveCredentialMutation:
				if err := v.StageDelete(mutation); err != nil {
					t.Fatal(err)
				}
			}
			pending, err := v.readPending(mutation.OperationID)
			if err != nil {
				t.Fatal(err)
			}
			newer := []byte("authenticated-newer-" + string(mutationKind))
			if mutationKind == ImportCredentialMutation {
				err = store.Create(pending.Account, newer, v.policy)
			} else {
				err = store.CompareAndReplace(pending.Account, pending.ExpectedDigest, newer, v.policy)
			}
			if err != nil {
				t.Fatal(err)
			}

			restarted := reopenReviewVault(t, store, &syntheticComponent{})
			if err := restarted.Promote(mutation.OperationID); !errors.Is(err, ErrVaultCASConflict) {
				t.Fatalf("stale %v promote = %v", mutationKind, err)
			}
			got, err := store.Read(pending.Account)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, newer) {
				t.Fatalf("stale %v overwrote newer target", mutationKind)
			}
			clear(got)
			clear(newer)
			clear(pending.Envelope)
		})
	}
}

type reservationBarrierStore struct {
	Store
	arrived chan struct{}
	release chan struct{}
	mu      sync.Mutex
	blocked bool
}

func (s *reservationBarrierStore) Create(account string, value []byte, policy AccessPolicy) error {
	err := s.Store.Create(account, value, policy)
	if err == nil && strings.Contains(account, "/reservation/") {
		s.mu.Lock()
		first := !s.blocked
		s.blocked = true
		s.mu.Unlock()
		if first {
			close(s.arrived)
			<-s.release
		}
	}
	return err
}

func TestLiveReservationCannotBeReclaimedBeforePendingWrite(t *testing.T) {
	base := newMemoryStore()
	store := &reservationBarrierStore{Store: base, arrived: make(chan struct{}), release: make(chan struct{})}
	firstEntropy := deterministicReader(41)
	first, err := newTestChannelVault(store, &firstEntropy, &syntheticComponent{}, syntheticOpener{files: map[string][]byte{"/synthetic/credential.p12": []byte("synthetic-p12")}})
	if err != nil {
		t.Fatal(err)
	}
	secondEntropy := deterministicReader(42)
	second, err := newTestChannelVault(store, &secondEntropy, &syntheticComponent{}, syntheticOpener{files: map[string][]byte{"/synthetic/credential.p12": []byte("synthetic-p12")}})
	if err != nil {
		t.Fatal(err)
	}
	firstMutation := Mutation{Kind: ImportCredentialMutation, OperationID: "018f0000-0000-7000-8000-0000000000d1", Scope: testScope(), SelectedPath: "/synthetic/credential.p12", Password: []byte("pw")}
	firstResult := make(chan error, 1)
	go func() {
		_, stageErr := first.StageCreate(firstMutation)
		firstResult <- stageErr
	}()
	<-store.arrived
	_, secondErr := second.StageCreate(Mutation{Kind: ImportCredentialMutation, OperationID: "018f0000-0000-7000-8000-0000000000d2", Scope: testScope(), SelectedPath: "/synthetic/credential.p12", Password: []byte("pw")})
	close(store.release)
	if !errors.Is(secondErr, ErrVaultPending) {
		t.Fatalf("second stage reclaimed a live reservation: %v", secondErr)
	}
	if status, err := second.PendingStatus("018f0000-0000-7000-8000-0000000000d2"); err != nil || status != PendingNone {
		t.Fatalf("losing stage retained its pending record: %v, %v", status, err)
	}
	if err := <-firstResult; err != nil {
		t.Fatalf("reservation owner lost its stage: %v", err)
	}
	if err := first.Promote(firstMutation.OperationID); err != nil {
		t.Fatalf("reservation owner could not promote: %v", err)
	}
}

type missingAsConflictStore struct{ Store }

func (s missingAsConflictStore) CompareAndDelete(account, expectedDigest string) error {
	err := s.Store.CompareAndDelete(account, expectedDigest)
	if errors.Is(err, ErrVaultMissing) {
		return ErrVaultCASConflict
	}
	return err
}

func TestDeletePromoteReconcilesStoreMissingReportedAsCASConflict(t *testing.T) {
	base := newMemoryStore()
	store := missingAsConflictStore{Store: base}
	entropy := deterministicReader(51)
	v, err := newTestChannelVault(store, &entropy, &syntheticComponent{}, syntheticOpener{files: map[string][]byte{"/synthetic/credential.p12": []byte("synthetic-p12")}})
	if err != nil {
		t.Fatal(err)
	}
	create := Mutation{Kind: ImportCredentialMutation, OperationID: "018f0000-0000-7000-8000-0000000000e1", Scope: testScope(), SelectedPath: "/synthetic/credential.p12", Password: []byte("pw")}
	if _, err := v.StageCreate(create); err != nil {
		t.Fatal(err)
	}
	if err := v.Promote(create.OperationID); err != nil {
		t.Fatal(err)
	}
	remove := Mutation{Kind: RemoveCredentialMutation, OperationID: "018f0000-0000-7000-8000-0000000000e2", Scope: testScope()}
	if err := v.StageDelete(remove); err != nil {
		t.Fatal(err)
	}
	pending, err := v.readPending(remove.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if err := base.CompareAndDelete(pending.Account, pending.ExpectedDigest); err != nil {
		t.Fatal(err)
	}
	clear(pending.Envelope)
	if err := v.Promote(remove.OperationID); err != nil {
		t.Fatalf("delete crash reconciliation = %v", err)
	}
}

func TestConcurrentInitialisationConvergesOnOneGeneration(t *testing.T) {
	store := newMemoryStore()
	var wait sync.WaitGroup
	wait.Add(2)
	results := make(chan *Vault, 2)
	failures := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func(seed byte) {
			defer wait.Done()
			entropy := deterministicReader(seed)
			v, err := newTestChannelVault(store, &entropy, &syntheticComponent{}, syntheticOpener{files: map[string][]byte{}})
			if err != nil {
				failures <- err
			} else {
				results <- v
			}
		}(byte(i + 1))
	}
	wait.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	var first *Vault
	for v := range results {
		if first == nil {
			first = v
		} else if !bytes.Equal(first.installationKey, v.installationKey) || !bytes.Equal(first.wrappingSecret, v.wrappingSecret) || !bytes.Equal(first.installationID, v.installationID) {
			t.Fatal("constructors accepted mixed installation generations")
		}
	}
}

func TestInitialisationRecoversAfterEveryInstallationItemCrashPoint(t *testing.T) {
	for completedCreates := 1; completedCreates <= 3; completedCreates++ {
		t.Run(fmt.Sprintf("after-%d", completedCreates), func(t *testing.T) {
			store := newMemoryStore()
			store.failCreateAfter = completedCreates
			entropy := deterministicReader(11)
			if _, err := newTestChannelVault(store, &entropy, &syntheticComponent{}, syntheticOpener{files: map[string][]byte{}}); !errors.Is(err, ErrVaultInaccessible) {
				t.Fatalf("interrupted constructor = %v", err)
			}
			store.failCreateAfter = 0
			store.createCount = 0
			retryEntropy := deterministicReader(91)
			v, err := newTestChannelVault(store, &retryEntropy, &syntheticComponent{}, syntheticOpener{files: map[string][]byte{}})
			if err != nil {
				t.Fatal(err)
			}
			reopened := reopenReviewVault(t, store, &syntheticComponent{})
			if !bytes.Equal(v.installationID, reopened.installationID) || !bytes.Equal(v.installationKey, reopened.installationKey) || !bytes.Equal(v.wrappingSecret, reopened.wrappingSecret) {
				t.Fatal("retry did not converge on marker-selected generation")
			}
		})
	}
}

func stringsTrimAccount(value, prefix string) string {
	if len(value) < len(prefix) {
		return value
	}
	return value[len(prefix):]
}
