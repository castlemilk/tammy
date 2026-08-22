package vault

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
)

type memoryStore struct {
	mu              sync.Mutex
	items           map[string][]byte
	inaccessible    bool
	lastRead        []byte
	policies        []AccessPolicy
	failCreateAfter int
	createCount     int
}

func newMemoryStore() *memoryStore { return &memoryStore{items: make(map[string][]byte)} }

func (s *memoryStore) Read(account string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inaccessible {
		return nil, ErrVaultInaccessible
	}
	value, ok := s.items[account]
	if !ok {
		return nil, ErrVaultMissing
	}
	s.lastRead = append([]byte(nil), value...)
	return s.lastRead, nil
}
func (s *memoryStore) Create(account string, value []byte, policy AccessPolicy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createCount++
	if s.failCreateAfter > 0 && s.createCount > s.failCreateAfter {
		return ErrVaultInaccessible
	}
	if s.inaccessible {
		return ErrVaultInaccessible
	}
	if _, exists := s.items[account]; exists {
		return ErrVaultCollision
	}
	s.items[account] = append([]byte(nil), value...)
	s.policies = append(s.policies, policy)
	return nil
}
func (s *memoryStore) Replace(account string, value []byte, policy AccessPolicy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inaccessible {
		return ErrVaultInaccessible
	}
	if _, exists := s.items[account]; !exists {
		return ErrVaultMissing
	}
	s.items[account] = append([]byte(nil), value...)
	s.policies = append(s.policies, policy)
	return nil
}
func (s *memoryStore) Delete(account string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inaccessible {
		return ErrVaultInaccessible
	}
	if _, exists := s.items[account]; !exists {
		return ErrVaultMissing
	}
	delete(s.items, account)
	return nil
}

func (s *memoryStore) CompareAndReplace(account, expectedDigest string, value []byte, policy AccessPolicy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.items[account]
	if !exists {
		return ErrVaultMissing
	}
	if hashValue(current) != expectedDigest {
		return ErrVaultCASConflict
	}
	s.items[account] = append([]byte(nil), value...)
	s.policies = append(s.policies, policy)
	return nil
}

func (s *memoryStore) CompareAndDelete(account, expectedDigest string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.items[account]
	if !exists {
		return ErrVaultMissing
	}
	if hashValue(current) != expectedDigest {
		return ErrVaultCASConflict
	}
	delete(s.items, account)
	return nil
}

type deterministicReader byte

func (r *deterministicReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(*r)
		(*r)++
	}
	return len(p), nil
}

type syntheticComponent struct{ imported, unlocked []byte }

func (c *syntheticComponent) Import(original, password []byte) (CredentialRecord, error) {
	c.imported = append([]byte(nil), original...)
	return CredentialRecord{Opaque: []byte("component-record"), Metadata: CredentialMetadata{Fingerprint: "credential-fingerprint", CanonicalABN: "11000000560", ComponentVersion: "sim-v1"}}, nil
}
func (c *syntheticComponent) Unlock(opaque, password []byte) ([]byte, error) {
	c.unlocked = append([]byte(nil), opaque...)
	return []byte("decrypted-private-key"), nil
}

type syntheticOpener struct{ files map[string][]byte }

func (o syntheticOpener) Open(path string, maximum int) ([]byte, error) {
	value, ok := o.files[path]
	if !ok || len(value) > maximum {
		return nil, ErrVaultInvalidInput
	}
	return append([]byte(nil), value...), nil
}

func newTestVault(t *testing.T, mode Namespace, store *memoryStore) *Vault {
	t.Helper()
	random := deterministicReader(1)
	channel := developmentChannel()
	if mode == ProductionNamespace {
		channel, _ = productionChannel("TEAM123456")
	}
	v, err := newVault(vaultConfig{Store: store, Channel: channel, Component: &syntheticComponent{}, Opener: syntheticOpener{files: map[string][]byte{"/synthetic/credential.p12": []byte("synthetic-p12"), "/synthetic/replacement.p12": []byte("replacement"), "/synthetic/duplicate.p12": []byte("duplicate"), "/synthetic/fixture.p12": []byte("fixture")}}}, &random)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func testScope() Scope {
	return Scope{WorkspaceID: "workspace-1", OrganisationID: "organisation-1", CanonicalABN: "11 000 000 560"}
}

func TestInitialiseCreatesSeparateInstallationSecretsAndNamespaces(t *testing.T) {
	productionStore := newMemoryStore()
	production := newTestVault(t, ProductionNamespace, productionStore)
	if len(production.installationID) != 16 || len(production.installationKey) != 32 || len(production.wrappingSecret) != 32 {
		t.Fatal("installation material not initialised")
	}
	if len(productionStore.items) != 4 {
		t.Fatalf("production items = %d", len(productionStore.items))
	}
	for account := range productionStore.items {
		if !strings.HasPrefix(account, "tammy.sbr.production/") {
			t.Fatalf("production account %q", account)
		}
	}
	developmentStore := newMemoryStore()
	_ = newTestVault(t, DevelopmentNamespace, developmentStore)
	for account := range developmentStore.items {
		if !strings.HasPrefix(account, "tammy.sbr.development/") {
			t.Fatalf("development account %q", account)
		}
		if _, found := productionStore.items[account]; found {
			t.Fatalf("namespace collision %q", account)
		}
	}
	for _, policy := range productionStore.policies {
		if policy.Identifier != "com.tammy.desktop.sbr-helper" || policy.TeamID != "TEAM123456" || policy.Namespace != ProductionNamespace || policy.AccessGroup != "TEAM123456.com.tammy.desktop.sbr" || policy.CurrentProcess {
			t.Fatalf("policy = %+v", policy)
		}
	}
}

func TestDeriveScopeCanonicalisesABNAndSeparatesEveryBoundary(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	base, err := DeriveScope(key, testScope())
	if err != nil {
		t.Fatal(err)
	}
	formatted, err := DeriveScope(key, Scope{WorkspaceID: "workspace-1", OrganisationID: "organisation-1", CanonicalABN: "11 000 000 560"})
	if err != nil || base != formatted {
		t.Fatalf("canonical form mismatch %q %q %v", base, formatted, err)
	}
	variants := []Scope{
		{WorkspaceID: "workspace-2", OrganisationID: "organisation-1", CanonicalABN: "11000000560"},
		{WorkspaceID: "workspace-1", OrganisationID: "organisation-2", CanonicalABN: "11000000560"},
		{WorkspaceID: "workspace-1", OrganisationID: "organisation-1", CanonicalABN: "53004085616"},
		{WorkspaceID: "a", OrganisationID: "b-c", CanonicalABN: "11000000560"},
		{WorkspaceID: "a-b", OrganisationID: "c", CanonicalABN: "11000000560"},
	}
	for _, variant := range variants {
		got, err := DeriveScope(key, variant)
		if err != nil {
			t.Fatal(err)
		}
		if got == base {
			t.Fatalf("scope collision for %+v", variant)
		}
	}
	if _, err := DeriveScope(key, Scope{WorkspaceID: "a\x00b", OrganisationID: "c", CanonicalABN: "11000000560"}); !errors.Is(err, ErrVaultInvalidInput) {
		t.Fatalf("NUL scope error = %v", err)
	}
	if _, err := DeriveScope(key, Scope{WorkspaceID: "a", OrganisationID: "c", CanonicalABN: "11000000561"}); !errors.Is(err, ErrVaultInvalidInput) {
		t.Fatalf("invalid ABN checksum error = %v", err)
	}
}

func TestCredentialCreatePromoteUnlockReplaceDeleteAndAbort(t *testing.T) {
	store := newMemoryStore()
	v := newTestVault(t, DevelopmentNamespace, store)
	scope := testScope()
	password := []byte("fixture-password")
	operation := "018f0000-0000-7000-8000-000000000001"
	metadata, err := v.StageCreate(Mutation{Kind: ImportCredentialMutation, OperationID: operation, Scope: scope, SelectedPath: "/synthetic/credential.p12", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Credential.Fingerprint != "credential-fingerprint" {
		t.Fatalf("metadata = %+v", metadata)
	}
	if !allZero(password) {
		t.Fatal("password buffer not cleared")
	}
	if status, err := v.PendingStatus(operation); err != nil || status != PendingCreate {
		t.Fatalf("pending = %v, %v", status, err)
	}
	if _, err := v.Unlock(scope, []byte("password")); !errors.Is(err, ErrVaultMissing) {
		t.Fatalf("unlock before commit = %v", err)
	}
	if err := v.Promote(operation); err != nil {
		t.Fatal(err)
	}
	if status, err := v.PendingStatus(operation); err != nil || status != PendingNone {
		t.Fatalf("promoted pending = %v, %v", status, err)
	}
	got, err := v.ReadMetadata(scope)
	if err != nil || got.Fingerprint != metadata.Credential.Fingerprint {
		t.Fatalf("read metadata = %+v, %v", got, err)
	}
	unlockPassword := []byte("unlock-password")
	if _, err := v.Unlock(scope, unlockPassword); err != nil {
		t.Fatal(err)
	}
	if !allZero(unlockPassword) {
		t.Fatal("unlock password buffer not cleared")
	}

	if _, err := v.StageCreate(Mutation{Kind: ImportCredentialMutation, OperationID: "018f0000-0000-7000-8000-000000000002", Scope: scope, SelectedPath: "/synthetic/duplicate.p12", Password: []byte("pw")}); !errors.Is(err, ErrVaultCollision) {
		t.Fatalf("duplicate create = %v", err)
	}
	replace := "018f0000-0000-7000-8000-000000000003"
	if _, err := v.StageReplace(Mutation{Kind: ReplaceCredentialMutation, OperationID: replace, Scope: scope, SelectedPath: "/synthetic/replacement.p12", Password: []byte("pw")}); err != nil {
		t.Fatal(err)
	}
	before, err := v.ReadMetadata(scope)
	if err != nil || before.Fingerprint != metadata.Credential.Fingerprint {
		t.Fatalf("old credential lost before commit: %+v %v", before, err)
	}
	if err := v.Abort(replace); err != nil {
		t.Fatal(err)
	}
	if _, err := v.PendingStatus(replace); err != nil {
		t.Fatal(err)
	}

	remove := "018f0000-0000-7000-8000-000000000004"
	if err := v.StageDelete(Mutation{Kind: RemoveCredentialMutation, OperationID: remove, Scope: scope}); err != nil {
		t.Fatal(err)
	}
	if _, err := v.ReadMetadata(scope); err != nil {
		t.Fatalf("delete happened before commit: %v", err)
	}
	if err := v.Promote(remove); err != nil {
		t.Fatal(err)
	}
	if _, err := v.ReadMetadata(scope); !errors.Is(err, ErrVaultMissing) {
		t.Fatalf("credential after delete = %v", err)
	}
}

func TestPendingOperationsAreOwnedAndInputsAreCopied(t *testing.T) {
	store := newMemoryStore()
	v := newTestVault(t, DevelopmentNamespace, store)
	op := "018f0000-0000-7000-8000-000000000011"
	if _, err := v.StageCreate(Mutation{Kind: ImportCredentialMutation, OperationID: op, Scope: testScope(), SelectedPath: "/synthetic/credential.p12", Password: []byte("pw")}); err != nil {
		t.Fatal(err)
	}
	if _, err := v.StageCreate(Mutation{Kind: ImportCredentialMutation, OperationID: op, Scope: testScope(), SelectedPath: "/synthetic/credential.p12", Password: []byte("pw")}); !errors.Is(err, ErrVaultCollision) {
		t.Fatalf("duplicate pending operation = %v", err)
	}
	if !allZero(store.lastRead) {
		t.Fatal("duplicate pending store read not cleared")
	}
	if err := v.Promote("018f0000-0000-7000-8000-000000000012"); !errors.Is(err, ErrVaultMissing) {
		t.Fatalf("foreign promote = %v", err)
	}
	if err := v.Abort("018f0000-0000-7000-8000-000000000012"); !errors.Is(err, ErrVaultMissing) {
		t.Fatalf("foreign abort = %v", err)
	}
	if err := v.Promote(op); err != nil {
		t.Fatal(err)
	}
	if _, err := v.ReadMetadata(testScope()); err != nil {
		t.Fatal(err)
	}
	other := Scope{WorkspaceID: "workspace-2", OrganisationID: "organisation-1", CanonicalABN: "11000000560"}
	if _, err := v.ReadMetadata(other); !errors.Is(err, ErrVaultMissing) {
		t.Fatalf("cross-workspace read = %v", err)
	}
}

func TestPendingPromotionRejectsTargetOutsideOwnedNamespaceAndKind(t *testing.T) {
	store := newMemoryStore()
	v := newTestVault(t, DevelopmentNamespace, store)
	op := "018f0000-0000-7000-8000-000000000013"
	if err := v.stagePending(pendingMutation{OperationID: op, Action: PendingCreate, Kind: CredentialKind, Account: "tammy.sbr.production/credential/foreign", Envelope: []byte("ciphertext")}); err != nil {
		t.Fatal(err)
	}
	if err := v.Promote(op); !errors.Is(err, ErrVaultAuthentication) {
		t.Fatalf("foreign namespace promotion = %v", err)
	}
	if _, exists := store.items["tammy.sbr.production/credential/foreign"]; exists {
		t.Fatal("foreign namespace item created")
	}
}

func TestEnvelopeRejectsTamperAADAndVersionAndClearsStoreRead(t *testing.T) {
	store := newMemoryStore()
	v := newTestVault(t, DevelopmentNamespace, store)
	op := "018f0000-0000-7000-8000-000000000021"
	if _, err := v.StageCreate(Mutation{Kind: ImportCredentialMutation, OperationID: op, Scope: testScope(), SelectedPath: "/synthetic/fixture.p12", Password: []byte("pw")}); err != nil {
		t.Fatal(err)
	}
	if err := v.Promote(op); err != nil {
		t.Fatal(err)
	}
	account, _ := v.credentialAccount(testScope())
	original := append([]byte(nil), store.items[account]...)
	store.items[account][len(store.items[account])-1] ^= 1
	if _, err := v.ReadMetadata(testScope()); !errors.Is(err, ErrVaultAuthentication) {
		t.Fatalf("tamper = %v", err)
	}
	if !allZero(store.lastRead) {
		t.Fatal("owned store read not cleared")
	}
	store.items[account] = original
	if _, err := v.readCredentialWithAAD(account, Scope{WorkspaceID: "other", OrganisationID: "organisation-1", CanonicalABN: "11000000560"}); !errors.Is(err, ErrVaultAuthentication) {
		t.Fatalf("AAD mismatch = %v", err)
	}
	store.items[account][0] = 2
	if _, err := v.ReadMetadata(testScope()); !errors.Is(err, ErrVaultVersion) {
		t.Fatalf("version = %v", err)
	}
}

func TestInaccessibleStoreAndErrorsNeverExposeSecrets(t *testing.T) {
	store := newMemoryStore()
	v := newTestVault(t, DevelopmentNamespace, store)
	store.inaccessible = true
	secret := "fixture-password-do-not-log"
	_, err := v.StageCreate(Mutation{Kind: ImportCredentialMutation, OperationID: "018f0000-0000-7000-8000-000000000031", Scope: testScope(), SelectedPath: "/synthetic/fixture.p12", Password: []byte(secret)})
	if !errors.Is(err, ErrVaultInaccessible) {
		t.Fatalf("inaccessible = %v", err)
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "fixture") {
		t.Fatalf("secret in error: %v", err)
	}
}

func TestNewRejectsShortRandomSource(t *testing.T) {
	_, err := newTestChannelVault(newMemoryStore(), io.LimitReader(bytes.NewReader([]byte{1}), 1), &syntheticComponent{}, syntheticOpener{files: map[string][]byte{}})
	if err == nil {
		t.Fatal("short random source accepted")
	}
}

func TestUnlockClearsComponentDecryptedKeyAndCloseClearsMasterSecrets(t *testing.T) {
	store := newMemoryStore()
	random := deterministicReader(1)
	component := &observingComponent{}
	v, err := newTestChannelVault(store, &random, component, syntheticOpener{files: map[string][]byte{"/synthetic/credential.p12": []byte("fixture")}})
	if err != nil {
		t.Fatal(err)
	}
	op := "018f0000-0000-7000-8000-000000000061"
	if _, err := v.StageCreate(Mutation{Kind: ImportCredentialMutation, OperationID: op, Scope: testScope(), SelectedPath: "/synthetic/credential.p12", Password: []byte("pw")}); err != nil {
		t.Fatal(err)
	}
	if err := v.Promote(op); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Unlock(testScope(), []byte("pw")); err != nil {
		t.Fatal(err)
	}
	if !allZero(component.returnedKey) {
		t.Fatalf("decrypted key remains: %q", component.returnedKey)
	}
	installationKey := v.installationKey
	wrappingSecret := v.wrappingSecret
	v.Close()
	if !allZero(installationKey) || !allZero(wrappingSecret) {
		t.Fatal("vault master secrets not cleared")
	}
	if _, err := v.ReadMetadata(testScope()); !errors.Is(err, ErrVaultInaccessible) {
		t.Fatalf("read after close = %v", err)
	}
}

type observingComponent struct{ returnedKey []byte }

func (*observingComponent) Import([]byte, []byte) (CredentialRecord, error) {
	return CredentialRecord{Opaque: []byte("opaque"), Metadata: CredentialMetadata{Fingerprint: "fingerprint", CanonicalABN: "11000000560", ComponentVersion: "sim-v1"}}, nil
}
func (c *observingComponent) Unlock([]byte, []byte) ([]byte, error) {
	c.returnedKey = []byte("decrypted-key")
	return c.returnedKey, nil
}

func allZero(value []byte) bool {
	for _, b := range value {
		if b != 0 {
			return false
		}
	}
	return true
}
