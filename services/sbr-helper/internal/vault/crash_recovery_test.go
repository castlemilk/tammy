package vault

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

type transactionCrashPoint uint8

const (
	crashBeforeReservationCreate transactionCrashPoint = iota + 1
	crashAfterReservationCreate
	crashBeforePendingApplied
	crashBeforeReservationRelease
	crashBeforePendingDelete
)

type transactionCrashStore struct {
	Store
	mu    sync.Mutex
	point transactionCrashPoint
	fired bool
}

func (s *transactionCrashStore) arm(point transactionCrashPoint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.point = point
	s.fired = false
}

func (s *transactionCrashStore) shouldFail(point transactionCrashPoint) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fired || s.point != point {
		return false
	}
	s.fired = true
	return true
}

func (s *transactionCrashStore) didFire() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fired
}

func (s *transactionCrashStore) Create(account string, value []byte, policy AccessPolicy) error {
	reservation := strings.Contains(account, "/reservation/")
	if reservation && s.shouldFail(crashBeforeReservationCreate) {
		return ErrVaultInaccessible
	}
	err := s.Store.Create(account, value, policy)
	if err == nil && reservation && s.shouldFail(crashAfterReservationCreate) {
		return ErrVaultInaccessible
	}
	return err
}

func (s *transactionCrashStore) CompareAndDelete(account, expectedDigest string) error {
	if strings.Contains(account, "/reservation/") && s.shouldFail(crashBeforeReservationRelease) {
		return ErrVaultInaccessible
	}
	if strings.Contains(account, "/pending/") && s.shouldFail(crashBeforePendingDelete) {
		return ErrVaultInaccessible
	}
	return s.Store.CompareAndDelete(account, expectedDigest)
}

func (s *transactionCrashStore) CompareAndReplace(account, expectedDigest string, value []byte, policy AccessPolicy) error {
	if strings.Contains(account, "/pending/") && s.shouldFail(crashBeforePendingApplied) {
		return ErrVaultInaccessible
	}
	return s.Store.CompareAndReplace(account, expectedDigest, value, policy)
}

type crashMutationCase struct {
	name   string
	action PendingState
	make   func() Mutation
	setup  func(*testing.T, *Vault)
	check  func(*testing.T, *Vault)
}

func crashMutationCases() []crashMutationCase {
	credentialScope := testScope()
	productScope := ProductScope{Product: "SBR", Service: "EVTE.BAS"}
	seedCredential := func(t *testing.T, v *Vault) {
		t.Helper()
		mutation := Mutation{Kind: ImportCredentialMutation, OperationID: "018f0000-0000-7000-8000-0000000000f0", Scope: credentialScope, SelectedPath: "/synthetic/credential.p12", Password: []byte("pw")}
		if _, err := v.StageCreate(mutation); err != nil {
			t.Fatal(err)
		}
		if err := v.Promote(mutation.OperationID); err != nil {
			t.Fatal(err)
		}
	}
	seedProduct := func(t *testing.T, v *Vault) {
		t.Helper()
		mutation := Mutation{Kind: ImportProductIDMutation, OperationID: "018f0000-0000-7000-8000-0000000000f1", ProductScope: productScope, ProductID: []byte("seed-product")}
		if _, err := v.StageCreate(mutation); err != nil {
			t.Fatal(err)
		}
		if err := v.Promote(mutation.OperationID); err != nil {
			t.Fatal(err)
		}
	}
	credentialPresent := func(t *testing.T, v *Vault) {
		t.Helper()
		if _, err := v.ReadMetadata(credentialScope); err != nil {
			t.Fatalf("credential not present: %v", err)
		}
	}
	credentialMissing := func(t *testing.T, v *Vault) {
		t.Helper()
		if _, err := v.ReadMetadata(credentialScope); !errors.Is(err, ErrVaultMissing) {
			t.Fatalf("credential remains: %v", err)
		}
	}
	productPresent := func(t *testing.T, v *Vault) {
		t.Helper()
		if status, err := v.ProductIDStatus(productScope); err != nil || status.State != ProductIDPresent {
			t.Fatalf("Product ID not present: %+v, %v", status, err)
		}
	}
	productMissing := func(t *testing.T, v *Vault) {
		t.Helper()
		if status, err := v.ProductIDStatus(productScope); !errors.Is(err, ErrVaultMissing) || status.State != ProductIDMissing {
			t.Fatalf("Product ID remains: %+v, %v", status, err)
		}
	}
	return []crashMutationCase{
		{name: "credential-create", action: PendingCreate, make: func() Mutation {
			return Mutation{Kind: ImportCredentialMutation, OperationID: "018f0000-0000-7000-8000-000000000101", Scope: credentialScope, SelectedPath: "/synthetic/credential.p12", Password: []byte("pw")}
		}, check: credentialPresent},
		{name: "credential-replace", action: PendingReplace, setup: seedCredential, make: func() Mutation {
			return Mutation{Kind: ReplaceCredentialMutation, OperationID: "018f0000-0000-7000-8000-000000000102", Scope: credentialScope, SelectedPath: "/synthetic/replacement.p12", Password: []byte("pw")}
		}, check: credentialPresent},
		{name: "credential-delete", action: PendingDelete, setup: seedCredential, make: func() Mutation {
			return Mutation{Kind: RemoveCredentialMutation, OperationID: "018f0000-0000-7000-8000-000000000103", Scope: credentialScope}
		}, check: credentialMissing},
		{name: "product-create", action: PendingCreate, make: func() Mutation {
			return Mutation{Kind: ImportProductIDMutation, OperationID: "018f0000-0000-7000-8000-000000000104", ProductScope: productScope, ProductID: []byte("new-product")}
		}, check: productPresent},
		{name: "product-delete", action: PendingDelete, setup: seedProduct, make: func() Mutation {
			return Mutation{Kind: RemoveProductIDMutation, OperationID: "018f0000-0000-7000-8000-000000000105", ProductScope: productScope}
		}, check: productMissing},
	}
}

func openTransactionCrashVault(t *testing.T, store Store, seed byte) *Vault {
	t.Helper()
	entropy := deterministicReader(seed)
	v, err := newTestChannelVault(store, &entropy, &syntheticComponent{}, syntheticOpener{files: map[string][]byte{
		"/synthetic/credential.p12":  []byte("synthetic-p12"),
		"/synthetic/replacement.p12": []byte("replacement-p12"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func stageCrashMutation(v *Vault, mutation Mutation) error {
	switch mutation.Kind {
	case ImportCredentialMutation, ImportProductIDMutation:
		_, err := v.StageCreate(mutation)
		return err
	case ReplaceCredentialMutation:
		_, err := v.StageReplace(mutation)
		return err
	case RemoveCredentialMutation, RemoveProductIDMutation:
		return v.StageDelete(mutation)
	default:
		return ErrVaultInvalidInput
	}
}

func TestStageCrashWindowsKeepAuthenticatedPendingRecoveryHandle(t *testing.T) {
	points := []struct {
		name  string
		point transactionCrashPoint
	}{{"after-pending-before-reservation", crashBeforeReservationCreate}, {"after-reservation-before-return", crashAfterReservationCreate}}
	for _, mutationCase := range crashMutationCases() {
		for _, point := range points {
			t.Run(mutationCase.name+"/"+point.name, func(t *testing.T) {
				base := newMemoryStore()
				setupVault := openTransactionCrashVault(t, base, 61)
				if mutationCase.setup != nil {
					mutationCase.setup(t, setupVault)
				}
				store := &transactionCrashStore{Store: base}
				v := openTransactionCrashVault(t, store, 62)
				mutation := mutationCase.make()
				store.arm(point.point)
				if err := stageCrashMutation(v, mutation); !errors.Is(err, ErrVaultInaccessible) {
					t.Fatalf("crash stage = %v", err)
				}
				if !store.didFire() {
					t.Fatal("crash point did not fire")
				}
				if status, err := v.PendingStatus(mutation.OperationID); err != nil || status != mutationCase.action {
					t.Fatalf("pending recovery handle = %v, %v", status, err)
				}
				reopened := openTransactionCrashVault(t, store, 63)
				if err := reopened.Promote(mutation.OperationID); err != nil {
					t.Fatalf("resume = %v", err)
				}
				if status, err := reopened.PendingStatus(mutation.OperationID); err != nil || status != PendingNone {
					t.Fatalf("pending after resume = %v, %v", status, err)
				}
				mutationCase.check(t, reopened)
			})
		}
	}
}

func TestPromoteCrashWindowsRetainPendingUntilReservationReleased(t *testing.T) {
	points := []struct {
		name  string
		point transactionCrashPoint
	}{{"after-target-before-applied-phase", crashBeforePendingApplied}, {"after-applied-phase-before-reservation-release", crashBeforeReservationRelease}, {"after-reservation-release-before-pending-delete", crashBeforePendingDelete}}
	for _, mutationCase := range crashMutationCases() {
		for _, point := range points {
			t.Run(mutationCase.name+"/"+point.name, func(t *testing.T) {
				base := newMemoryStore()
				setupVault := openTransactionCrashVault(t, base, 71)
				if mutationCase.setup != nil {
					mutationCase.setup(t, setupVault)
				}
				store := &transactionCrashStore{Store: base}
				v := openTransactionCrashVault(t, store, 72)
				mutation := mutationCase.make()
				if err := stageCrashMutation(v, mutation); err != nil {
					t.Fatal(err)
				}
				store.arm(point.point)
				if err := v.Promote(mutation.OperationID); !errors.Is(err, ErrVaultInaccessible) {
					t.Fatalf("crash promote = %v", err)
				}
				if !store.didFire() {
					t.Fatal("crash point did not fire")
				}
				if status, err := v.PendingStatus(mutation.OperationID); err != nil || status != mutationCase.action {
					t.Fatalf("pending recovery handle = %v, %v", status, err)
				}
				pending, err := v.readPending(mutation.OperationID)
				if err != nil {
					t.Fatal(err)
				}
				wantPhase := pendingPrepared
				if point.point != crashBeforePendingApplied {
					wantPhase = pendingApplied
				}
				if pending.Phase != wantPhase {
					t.Fatalf("pending phase = %v, want %v", pending.Phase, wantPhase)
				}
				if point.point == crashBeforePendingDelete {
					if _, err := base.Read(pending.ReservationAccount); !errors.Is(err, ErrVaultMissing) {
						t.Fatalf("reservation not released before pending cleanup: %v", err)
					}
				}
				clear(pending.Envelope)
				reopened := openTransactionCrashVault(t, store, 73)
				if err := reopened.Promote(mutation.OperationID); err != nil {
					t.Fatalf("resume = %v", err)
				}
				mutationCase.check(t, reopened)
			})
		}
	}
}

func TestAppliedCreateRetryCannotResurrectAfterNewerDelete(t *testing.T) {
	tests := []struct {
		name       string
		create     Mutation
		remove     Mutation
		assertGone func(*testing.T, *Vault)
	}{
		{
			name:       "credential",
			create:     Mutation{Kind: ImportCredentialMutation, OperationID: "018f0000-0000-7000-8000-000000000111", Scope: testScope(), SelectedPath: "/synthetic/credential.p12", Password: []byte("pw")},
			remove:     Mutation{Kind: RemoveCredentialMutation, OperationID: "018f0000-0000-7000-8000-000000000112", Scope: testScope()},
			assertGone: crashMutationCases()[2].check,
		},
		{
			name:       "product-id",
			create:     Mutation{Kind: ImportProductIDMutation, OperationID: "018f0000-0000-7000-8000-000000000113", ProductScope: ProductScope{Product: "SBR", Service: "EVTE.BAS"}, ProductID: []byte("product")},
			remove:     Mutation{Kind: RemoveProductIDMutation, OperationID: "018f0000-0000-7000-8000-000000000114", ProductScope: ProductScope{Product: "SBR", Service: "EVTE.BAS"}},
			assertGone: crashMutationCases()[4].check,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			base := newMemoryStore()
			store := &transactionCrashStore{Store: base}
			oldVault := openTransactionCrashVault(t, store, 101)
			if err := stageCrashMutation(oldVault, testCase.create); err != nil {
				t.Fatal(err)
			}
			store.arm(crashBeforePendingDelete)
			if err := oldVault.Promote(testCase.create.OperationID); !errors.Is(err, ErrVaultInaccessible) {
				t.Fatalf("old create crash = %v", err)
			}
			pending, err := oldVault.readPending(testCase.create.OperationID)
			if err != nil {
				t.Fatal(err)
			}
			if pending.Phase != pendingApplied {
				t.Fatalf("old pending phase = %v", pending.Phase)
			}
			clear(pending.Envelope)
			newVault := openTransactionCrashVault(t, store, 102)
			if err := stageCrashMutation(newVault, testCase.remove); err != nil {
				t.Fatal(err)
			}
			if err := newVault.Promote(testCase.remove.OperationID); err != nil {
				t.Fatal(err)
			}
			testCase.assertGone(t, newVault)

			restartedOld := openTransactionCrashVault(t, store, 103)
			if err := restartedOld.Promote(testCase.create.OperationID); err != nil {
				t.Fatalf("old cleanup retry = %v", err)
			}
			testCase.assertGone(t, restartedOld)
		})
	}
}

func TestPendingAppliedPhaseIsAuthenticated(t *testing.T) {
	store := newMemoryStore()
	v := openTransactionCrashVault(t, store, 111)
	mutation := crashMutationCases()[0].make()
	if err := stageCrashMutation(v, mutation); err != nil {
		t.Fatal(err)
	}
	account := v.pendingAccount(mutation.OperationID)
	store.mu.Lock()
	tampered := append([]byte(nil), store.items[account]...)
	tampered[len(tampered)-1] ^= 1
	store.items[account] = tampered
	store.mu.Unlock()
	if err := v.Promote(mutation.OperationID); !errors.Is(err, ErrVaultAuthentication) {
		t.Fatalf("tampered pending phase accepted: %v", err)
	}
}

func TestPendingWithoutReservationCanBeAborted(t *testing.T) {
	base := newMemoryStore()
	store := &transactionCrashStore{Store: base}
	v := openTransactionCrashVault(t, store, 81)
	mutation := crashMutationCases()[0].make()
	store.arm(crashBeforeReservationCreate)
	if err := stageCrashMutation(v, mutation); !errors.Is(err, ErrVaultInaccessible) {
		t.Fatalf("crash stage = %v", err)
	}
	if err := v.Abort(mutation.OperationID); err != nil {
		t.Fatalf("abort pending-only operation = %v", err)
	}
	if status, err := v.PendingStatus(mutation.OperationID); err != nil || status != PendingNone {
		t.Fatalf("pending after abort = %v, %v", status, err)
	}
}

func TestOldRetryAfterReservationReleaseCannotClobberNewerOperation(t *testing.T) {
	base := newMemoryStore()
	setup := openTransactionCrashVault(t, base, 91)
	crashMutationCases()[1].setup(t, setup)
	store := &transactionCrashStore{Store: base}
	oldVault := openTransactionCrashVault(t, store, 92)
	oldMutation := crashMutationCases()[1].make()
	if err := stageCrashMutation(oldVault, oldMutation); err != nil {
		t.Fatal(err)
	}
	store.arm(crashBeforePendingDelete)
	if err := oldVault.Promote(oldMutation.OperationID); !errors.Is(err, ErrVaultInaccessible) {
		t.Fatalf("old promote crash = %v", err)
	}
	account, err := oldVault.credentialAccount(testScope())
	if err != nil {
		t.Fatal(err)
	}

	newVault := openTransactionCrashVault(t, store, 93)
	newMutation := Mutation{Kind: ReplaceCredentialMutation, OperationID: "018f0000-0000-7000-8000-000000000106", Scope: testScope(), SelectedPath: "/synthetic/credential.p12", Password: []byte("pw")}
	if err := stageCrashMutation(newVault, newMutation); err != nil {
		t.Fatal(err)
	}
	if err := newVault.Promote(newMutation.OperationID); err != nil {
		t.Fatal(err)
	}
	newer, err := base.Read(account)
	if err != nil {
		t.Fatal(err)
	}
	newerDigest := hashValue(newer)
	clear(newer)

	restartedOld := openTransactionCrashVault(t, store, 94)
	if err := restartedOld.Promote(oldMutation.OperationID); err != nil {
		t.Fatalf("stale retry = %v", err)
	}
	unchanged, err := base.Read(account)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(unchanged)
	if hashValue(unchanged) != newerDigest {
		t.Fatal("stale retry clobbered newer target")
	}
	future := Mutation{Kind: ReplaceCredentialMutation, OperationID: "018f0000-0000-7000-8000-000000000107", Scope: testScope(), SelectedPath: "/synthetic/replacement.p12", Password: []byte("pw")}
	if err := stageCrashMutation(restartedOld, future); err != nil {
		t.Fatalf("reservation remained fenced after abort: %v", err)
	}
	if err := restartedOld.Abort(future.OperationID); err != nil {
		t.Fatal(err)
	}
}
