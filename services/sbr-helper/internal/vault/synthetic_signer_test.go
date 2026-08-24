package vault

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/sbr-helper/internal/evte"
	"github.com/tammyapp/tammy/services/sbr-helper/internal/protocol"
	"github.com/tammyapp/tammy/services/sbr-helper/internal/runner"
	"github.com/tammyapp/tammy/services/sbr-helper/internal/simulator"
)

func TestCommittedDesktopFixtureAuthenticatesDuringTaskSixPlanWindow(t *testing.T) {
	assetRoot := filepath.Join("..", "..", "..", "..", "apps", "desktop", "tests", "e2e", "assets")
	credential, err := os.ReadFile(filepath.Join(assetRoot, "synthetic-machine-credential.p12"))
	if err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := os.ReadFile(filepath.Join(assetRoot, "synthetic-machine-credential.fixture.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Password     string `json:"password"`
		CanonicalABN string `json:"canonical_abn"`
		ExpiresAt    string `json:"expires_at"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, manifest.ExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	record, err := (SyntheticCredentialComponent{}).Import(credential, []byte(manifest.Password))
	if err != nil {
		t.Fatal(err)
	}
	planNow := time.Date(2026, time.August, 24, 0, 0, 0, 0, time.UTC)
	if record.Metadata.CanonicalABN != manifest.CanonicalABN ||
		record.Metadata.ExpiresUnixMillis != expiresAt.UnixMilli() ||
		time.UnixMilli(record.Metadata.CreatedUnixMillis).After(planNow.Add(5*time.Minute)) ||
		!expiresAt.After(planNow) {
		t.Fatalf("committed fixture metadata is outside the Task 6 plan window: %#v", record.Metadata)
	}
}

const (
	syntheticTestRequestID    = "018bcfe5-6800-7000-8000-000000000001"
	syntheticTestWorkspaceID  = "018bcfe5-6800-7000-8000-000000000002"
	syntheticTestOrganisation = "018bcfe5-6800-7000-8000-000000000003"
	syntheticTestABN          = "11000000560"
)

func TestSyntheticSignerCredentialLifecycle(t *testing.T) {
	now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	expires := now.AddDate(2, 0, 0).UnixMilli()
	password := []byte("synthetic-password")
	credential := encodeSyntheticTestCredential(t, syntheticTestABN, expires, password, 0x31)
	credentialPath := filepath.Join(t.TempDir(), "tammy-synthetic-credential-v1.bin")
	if err := os.WriteFile(credentialPath, credential, 0o600); err != nil {
		t.Fatal(err)
	}
	wantFingerprint := sha256.Sum256(credential)
	clear(credential)

	store := newMemoryStore()
	entropy := deterministicReader(1)
	v, err := newVault(vaultConfig{Store: store, Channel: developmentChannel(), Component: SyntheticCredentialComponent{}}, &entropy)
	if err != nil {
		t.Fatal(err)
	}
	signer := newSyntheticSigner(v)

	prepare := syntheticScopedRequest(now, protocol.OperationPrepareMutation)
	prepare.OperationID = "018bcfe5-6800-7000-8000-000000000011"
	prepare.MutationKind = protocol.MutationImportCredential
	prepare.SelectedLocalPath = credentialPath
	prepare.TransientPassword = bytes.Clone(password)
	prepared := signer.Execute(context.Background(), prepare)
	assertPendingCredential(t, prepared, prepare.OperationID, wantFingerprint, expires)
	assertZeroed(t, prepare.TransientPassword)

	reconcile := syntheticMutationRequest(now, protocol.OperationReconcileMutation, prepare.OperationID, protocol.MutationImportCredential)
	reconciled := signer.Execute(context.Background(), reconcile)
	if reconciled.Outcome != protocol.OutcomePending || reconciled.RedactedResult != protocol.ResultRecoveryRequired || reconciled.PendingItemID != prepare.OperationID {
		t.Fatalf("pending reconcile = %#v", reconciled)
	}

	commit := syntheticMutationRequest(now, protocol.OperationCommitMutation, prepare.OperationID, protocol.MutationImportCredential)
	committed := signer.Execute(context.Background(), commit)
	if committed.Outcome != protocol.OutcomeOK || committed.RedactedResult != protocol.ResultMutationCommitted || !bytes.Equal(committed.CredentialFingerprint, wantFingerprint[:]) {
		t.Fatalf("commit = %#v", committed)
	}

	status := signer.Execute(context.Background(), syntheticScopedRequest(now, protocol.OperationStatus))
	if status.Outcome != protocol.OutcomeOK || status.RedactedResult != protocol.ResultCredentialLocked || !bytes.Equal(status.CredentialFingerprint, wantFingerprint[:]) {
		t.Fatalf("status = %#v", status)
	}

	unlock := syntheticScopedRequest(now, protocol.OperationUnlock)
	unlock.TransientPassword = bytes.Clone(password)
	unlocked := signer.Execute(context.Background(), unlock)
	if unlocked.Outcome != protocol.OutcomeOK || unlocked.RedactedResult != protocol.ResultReady || !bytes.Equal(unlocked.CredentialFingerprint, wantFingerprint[:]) {
		t.Fatalf("unlock = %#v", unlocked)
	}
	assertZeroed(t, unlock.TransientPassword)

	reconciledCommit := signer.Execute(context.Background(), syntheticMutationRequest(now, protocol.OperationReconcileMutation, prepare.OperationID, protocol.MutationImportCredential))
	if reconciledCommit.Outcome != protocol.OutcomeOK || reconciledCommit.RedactedResult != protocol.ResultMutationCommitted ||
		!bytes.Equal(reconciledCommit.CredentialFingerprint, wantFingerprint[:]) || reconciledCommit.PendingItemID != "" {
		t.Fatalf("completed reconcile = %#v", reconciledCommit)
	}
	repeatedCommit := signer.Execute(context.Background(), syntheticMutationRequest(now, protocol.OperationCommitMutation, prepare.OperationID, protocol.MutationImportCredential))
	if repeatedCommit.Outcome != protocol.OutcomeOK || repeatedCommit.RedactedResult != protocol.ResultMutationCommitted ||
		!bytes.Equal(repeatedCommit.CredentialFingerprint, wantFingerprint[:]) {
		t.Fatalf("repeated commit = %#v", repeatedCommit)
	}
}

func TestSyntheticSignerRestartReconcilesCrashAfterVaultPromotionFromReceipt(t *testing.T) {
	now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	password := []byte("synthetic-password")
	path, fingerprint := writeSyntheticTestCredential(t, syntheticTestABN, now.AddDate(2, 0, 0).UnixMilli(), password, 0x3a)
	store := newMemoryStore()
	firstEntropy := deterministicReader(91)
	first, err := newVault(vaultConfig{Store: store, Channel: developmentChannel(), Component: SyntheticCredentialComponent{}}, &firstEntropy)
	if err != nil {
		t.Fatal(err)
	}
	operationID := "018bcfe5-6800-7000-8000-00000000001a"
	prepare := syntheticMutationRequest(now, protocol.OperationPrepareMutation, operationID, protocol.MutationImportCredential)
	prepare.SelectedLocalPath, prepare.TransientPassword = path, bytes.Clone(password)
	prepared := newSyntheticSigner(first).Execute(context.Background(), prepare)
	assertPendingCredential(t, prepared, operationID, fingerprint, now.AddDate(2, 0, 0).UnixMilli())
	if err := first.Promote(operationID); err != nil {
		t.Fatalf("helper promotion before simulated core crash: %v", err)
	}
	first.Close()

	secondEntropy := deterministicReader(92)
	restarted, err := newVault(vaultConfig{Store: store, Channel: developmentChannel(), Component: SyntheticCredentialComponent{}}, &secondEntropy)
	if err != nil {
		t.Fatal(err)
	}
	reconciled := newSyntheticSigner(restarted).Execute(context.Background(),
		syntheticMutationRequest(now, protocol.OperationReconcileMutation, operationID, protocol.MutationImportCredential))
	if reconciled.Outcome != protocol.OutcomeOK || reconciled.RedactedResult != protocol.ResultMutationCommitted ||
		!bytes.Equal(reconciled.CredentialFingerprint, fingerprint[:]) || reconciled.PendingItemID != "" {
		t.Fatalf("restart receipt reconciliation = %#v", reconciled)
	}
}

func TestSyntheticSignerReplaceRemoveAbortAndTamper(t *testing.T) {
	now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	password := []byte("synthetic-password")
	firstPath, firstFingerprint := writeSyntheticTestCredential(t, syntheticTestABN, now.AddDate(2, 0, 0).UnixMilli(), password, 0x41)
	secondPath, secondFingerprint := writeSyntheticTestCredential(t, syntheticTestABN, now.AddDate(3, 0, 0).UnixMilli(), password, 0x42)
	signer := newSyntheticTestSigner(t)

	createID := "018bcfe5-6800-7000-8000-000000000021"
	prepareAndCommitCredential(t, signer, now, createID, protocol.MutationImportCredential, firstPath, password)

	replaceID := "018bcfe5-6800-7000-8000-000000000022"
	replace := syntheticMutationRequest(now, protocol.OperationPrepareMutation, replaceID, protocol.MutationReplaceCredential)
	replace.SelectedLocalPath, replace.TransientPassword = secondPath, bytes.Clone(password)
	prepared := signer.Execute(context.Background(), replace)
	assertPendingCredential(t, prepared, replaceID, secondFingerprint, now.AddDate(3, 0, 0).UnixMilli())
	beforeCommit := signer.Execute(context.Background(), syntheticScopedRequest(now, protocol.OperationStatus))
	if !bytes.Equal(beforeCommit.CredentialFingerprint, firstFingerprint[:]) {
		t.Fatalf("replacement became visible before commit: %#v", beforeCommit)
	}
	committed := signer.Execute(context.Background(), syntheticMutationRequest(now, protocol.OperationCommitMutation, replaceID, protocol.MutationReplaceCredential))
	if !bytes.Equal(committed.CredentialFingerprint, secondFingerprint[:]) {
		t.Fatalf("replacement commit = %#v", committed)
	}

	removeAbortID := "018bcfe5-6800-7000-8000-000000000023"
	remove := signer.Execute(context.Background(), syntheticMutationRequest(now, protocol.OperationPrepareMutation, removeAbortID, protocol.MutationRemoveCredential))
	if remove.Outcome != protocol.OutcomePending || remove.PendingItemID != removeAbortID {
		t.Fatalf("remove prepare = %#v", remove)
	}
	aborted := signer.Execute(context.Background(), syntheticMutationRequest(now, protocol.OperationAbortMutation, removeAbortID, protocol.MutationRemoveCredential))
	if aborted.Outcome != protocol.OutcomeOK || aborted.RedactedResult != protocol.ResultMutationAborted {
		t.Fatalf("remove abort = %#v", aborted)
	}
	stillPresent := signer.Execute(context.Background(), syntheticScopedRequest(now, protocol.OperationStatus))
	if !bytes.Equal(stillPresent.CredentialFingerprint, secondFingerprint[:]) {
		t.Fatalf("abort removed active credential: %#v", stillPresent)
	}

	removeCommitID := "018bcfe5-6800-7000-8000-000000000024"
	_ = signer.Execute(context.Background(), syntheticMutationRequest(now, protocol.OperationPrepareMutation, removeCommitID, protocol.MutationRemoveCredential))
	removed := signer.Execute(context.Background(), syntheticMutationRequest(now, protocol.OperationCommitMutation, removeCommitID, protocol.MutationRemoveCredential))
	if removed.Outcome != protocol.OutcomeOK || removed.RedactedResult != protocol.ResultMutationCommitted {
		t.Fatalf("remove commit = %#v", removed)
	}
	missing := signer.Execute(context.Background(), syntheticScopedRequest(now, protocol.OperationStatus))
	if missing.Outcome != protocol.OutcomeOK || missing.RedactedResult != protocol.ResultRegistrationRequired {
		t.Fatalf("removed status = %#v", missing)
	}

	tampered := encodeSyntheticTestCredential(t, syntheticTestABN, now.AddDate(2, 0, 0).UnixMilli(), password, 0x51)
	tampered[len(tampered)-1] ^= 0xff
	tamperedPath := filepath.Join(t.TempDir(), "tampered-synthetic-credential.bin")
	if err := os.WriteFile(tamperedPath, tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	clear(tampered)
	tamperRequest := syntheticMutationRequest(now, protocol.OperationPrepareMutation, "018bcfe5-6800-7000-8000-000000000025", protocol.MutationImportCredential)
	tamperRequest.SelectedLocalPath, tamperRequest.TransientPassword = tamperedPath, bytes.Clone(password)
	tamperResponse := signer.Execute(context.Background(), tamperRequest)
	if tamperResponse.Outcome != protocol.OutcomeError || tamperResponse.StableErrorCode != protocol.StableErrorCredentialIncompatible {
		t.Fatalf("tampered response = %#v", tamperResponse)
	}
	assertZeroed(t, tamperRequest.TransientPassword)
}

func TestSyntheticSignerProductIDLifecycle(t *testing.T) {
	now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	signer := newSyntheticTestSigner(t)
	productValue := []byte("synthetic-product-id")
	createID := "018bcfe5-6800-7000-8000-000000000031"
	prepare := syntheticMutationRequest(now, protocol.OperationPrepareMutation, createID, protocol.MutationImportProductID)
	prepare.ProductScope, prepare.ServiceID = "tammy-synthetic-product", "sbr.synthetic.readiness"
	prepare.TransientProductID = bytes.Clone(productValue)
	prepared := signer.Execute(context.Background(), prepare)
	if prepared.Outcome != protocol.OutcomePending || prepared.ProductState != protocol.ProductPresent || len(prepared.ProductFingerprint) != sha256.Size {
		t.Fatalf("product prepare = %#v", prepared)
	}
	wantFingerprint := bytes.Clone(prepared.ProductFingerprint)
	assertZeroed(t, prepare.TransientProductID)

	commit := syntheticMutationRequest(now, protocol.OperationCommitMutation, createID, protocol.MutationImportProductID)
	commit.ProductScope, commit.ServiceID = prepare.ProductScope, prepare.ServiceID
	committed := signer.Execute(context.Background(), commit)
	if committed.Outcome != protocol.OutcomeOK || committed.RedactedResult != protocol.ResultMutationCommitted || committed.ProductState != protocol.ProductPresent || !bytes.Equal(committed.ProductFingerprint, wantFingerprint) {
		t.Fatalf("product commit = %#v", committed)
	}

	removeID := "018bcfe5-6800-7000-8000-000000000032"
	remove := syntheticMutationRequest(now, protocol.OperationPrepareMutation, removeID, protocol.MutationRemoveProductID)
	remove.ProductScope, remove.ServiceID = prepare.ProductScope, prepare.ServiceID
	preparedRemove := signer.Execute(context.Background(), remove)
	if preparedRemove.Outcome != protocol.OutcomePending || preparedRemove.ProductState != protocol.ProductMissing {
		t.Fatalf("product remove prepare = %#v", preparedRemove)
	}
	removeCommit := syntheticMutationRequest(now, protocol.OperationCommitMutation, removeID, protocol.MutationRemoveProductID)
	removeCommit.ProductScope, removeCommit.ServiceID = prepare.ProductScope, prepare.ServiceID
	removed := signer.Execute(context.Background(), removeCommit)
	if removed.Outcome != protocol.OutcomeOK || removed.RedactedResult != protocol.ResultMutationCommitted || removed.ProductState != protocol.ProductMissing || removed.ProductFingerprint != nil {
		t.Fatalf("product remove commit = %#v", removed)
	}
}

func TestSyntheticSignedCredentialOverStrictPrivateProtocol(t *testing.T) {
	now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	password := []byte("signed-protocol-password")
	path, fingerprint := writeSyntheticTestCredential(t, syntheticTestABN, now.AddDate(2, 0, 0).UnixMilli(), password, 0x61)
	signer := newSyntheticTestSigner(t)
	deps := runner.Dependencies{Clock: syntheticFixedClock{now: now}, RandomSource: syntheticUnusedRandom{},
		Dialer: simulator.DenyDialer{}, CredentialSigner: signer, ComponentClient: evte.Adapter{}}

	operationID := "018bcfe5-6800-7000-8000-000000000041"
	prepare := syntheticMutationRequest(now, protocol.OperationPrepareMutation, operationID, protocol.MutationImportCredential)
	prepare.SelectedLocalPath, prepare.TransientPassword = path, bytes.Clone(password)
	prepared := executeSyntheticProtocolRequest(t, deps, prepare)
	if prepared.Outcome != protocol.OutcomePending || prepared.PendingItemID != operationID || !bytes.Equal(prepared.CredentialFingerprint, fingerprint[:]) {
		t.Fatalf("framed prepare = %#v", prepared)
	}

	reconcile := executeSyntheticProtocolRequest(t, deps, syntheticMutationRequest(now, protocol.OperationReconcileMutation, operationID, protocol.MutationImportCredential))
	if reconcile.Outcome != protocol.OutcomePending || reconcile.RedactedResult != protocol.ResultRecoveryRequired || reconcile.PendingItemID != operationID {
		t.Fatalf("framed reconcile = %#v", reconcile)
	}

	committed := executeSyntheticProtocolRequest(t, deps, syntheticMutationRequest(now, protocol.OperationCommitMutation, operationID, protocol.MutationImportCredential))
	if committed.Outcome != protocol.OutcomeOK || committed.RedactedResult != protocol.ResultMutationCommitted {
		t.Fatalf("framed commit = %#v", committed)
	}

	unlock := syntheticScopedRequest(now, protocol.OperationUnlock)
	unlock.TransientPassword = bytes.Clone(password)
	unlocked := executeSyntheticProtocolRequest(t, deps, unlock)
	if unlocked.Outcome != protocol.OutcomeOK || unlocked.RedactedResult != protocol.ResultReady || !bytes.Equal(unlocked.CredentialFingerprint, fingerprint[:]) ||
		unlocked.CredentialCreatedMillis <= 0 || unlocked.CredentialCreatedMillis >= unlocked.CredentialExpiresMillis ||
		!bytes.Equal(unlocked.ProfileFingerprint, unlock.ProfileFingerprint) ||
		!bytes.Equal(unlocked.RegistrationFingerprint, unlock.RegistrationFingerprint) ||
		!bytes.Equal(unlocked.ComponentFingerprint, unlock.ComponentFingerprint) || unlocked.ComponentVersion != unlock.ComponentVersion {
		t.Fatalf("framed unlock = %#v", unlocked)
	}
}

type syntheticFixedClock struct{ now time.Time }

func (clock syntheticFixedClock) Now() time.Time { return clock.now }

type syntheticUnusedRandom struct{}

func (syntheticUnusedRandom) Read([]byte) (int, error) { return 0, nil }

func executeSyntheticProtocolRequest(t *testing.T, deps runner.Dependencies, request protocol.Request) protocol.Response {
	t.Helper()
	payload, err := protocol.EncodeRequest(request, deps.Clock.Now())
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}
	defer clear(payload)
	var input bytes.Buffer
	if err := protocol.WriteFrame(&input, payload); err != nil {
		t.Fatal(err)
	}
	var output, lifecycle bytes.Buffer
	if code := runner.RunOne(context.Background(), &input, &output, &lifecycle, deps); code != 0 || lifecycle.Len() != 0 {
		t.Fatalf("runner code=%d lifecycle=%q output=%x", code, lifecycle.String(), output.Bytes())
	}
	responsePayload, err := protocol.ReadFrame(&output)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(responsePayload)
	response, err := protocol.DecodeResponse(responsePayload)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func newSyntheticTestSigner(t *testing.T) *SyntheticSigner {
	t.Helper()
	entropy := deterministicReader(1)
	v, err := newVault(vaultConfig{Store: newMemoryStore(), Channel: developmentChannel(), Component: SyntheticCredentialComponent{}}, &entropy)
	if err != nil {
		t.Fatal(err)
	}
	return newSyntheticSigner(v)
}

func writeSyntheticTestCredential(t *testing.T, abn string, expires int64, password []byte, keyByte byte) (string, [sha256.Size]byte) {
	t.Helper()
	credential := encodeSyntheticTestCredential(t, abn, expires, password, keyByte)
	fingerprint := sha256.Sum256(credential)
	path := filepath.Join(t.TempDir(), "tammy-synthetic-credential-v1.bin")
	if err := os.WriteFile(path, credential, 0o600); err != nil {
		t.Fatal(err)
	}
	clear(credential)
	return path, fingerprint
}

func prepareAndCommitCredential(t *testing.T, signer *SyntheticSigner, now time.Time, operationID string, kind protocol.MutationKind, path string, password []byte) {
	t.Helper()
	prepare := syntheticMutationRequest(now, protocol.OperationPrepareMutation, operationID, kind)
	prepare.SelectedLocalPath, prepare.TransientPassword = path, bytes.Clone(password)
	if response := signer.Execute(context.Background(), prepare); response.Outcome != protocol.OutcomePending {
		t.Fatalf("prepare = %#v", response)
	}
	if response := signer.Execute(context.Background(), syntheticMutationRequest(now, protocol.OperationCommitMutation, operationID, kind)); response.Outcome != protocol.OutcomeOK {
		t.Fatalf("commit = %#v", response)
	}
}

func syntheticScopedRequest(now time.Time, operation protocol.Operation) protocol.Request {
	return protocol.Request{ProtocolVersion: protocol.ProtocolVersion, RequestID: syntheticTestRequestID,
		Operation: operation, DeadlineMillis: now.Add(time.Minute).UnixMilli(), Environment: protocol.EnvironmentSimulator,
		WorkspaceID: syntheticTestWorkspaceID, OrganisationID: syntheticTestOrganisation, CanonicalABN: syntheticTestABN,
		OpaqueScope: bytes.Repeat([]byte{0x51}, 32), ProfileFingerprint: bytes.Repeat([]byte{0x61}, 32),
		RegistrationFingerprint: bytes.Repeat([]byte{0x62}, 32), ComponentFingerprint: bytes.Repeat([]byte{0x63}, 32),
		ComponentVersion: SyntheticComponentVersion}
}

func syntheticMutationRequest(now time.Time, operation protocol.Operation, operationID string, kind protocol.MutationKind) protocol.Request {
	request := syntheticScopedRequest(now, operation)
	request.OperationID, request.MutationKind = operationID, kind
	return request
}

func encodeSyntheticTestCredential(t *testing.T, abn string, expires int64, password []byte, keyByte byte) []byte {
	t.Helper()
	if len(abn) != 11 {
		t.Fatal("test ABN must be canonical")
	}
	prefix := []byte("TAMMY-SBR-SYNTHETIC-CREDENTIAL-V1\x00")
	payload := append([]byte(nil), prefix...)
	payload = append(payload, []byte(abn)...)
	var expiry [8]byte
	binary.BigEndian.PutUint64(expiry[:], uint64(expires))
	payload = append(payload, expiry[:]...)
	payload = append(payload, bytes.Repeat([]byte{0x22}, 16)...)
	payload = append(payload, bytes.Repeat([]byte{keyByte}, 32)...)
	mac := hmac.New(sha256.New, password)
	_, _ = mac.Write([]byte("tammy-sbr-synthetic-password-v1\x00"))
	_, _ = mac.Write(payload)
	payload = append(payload, mac.Sum(nil)...)
	seed := sha256.Sum256([]byte("tammy-sbr-synthetic-fixture-signing-seed-v1"))
	privateKey := ed25519.NewKeyFromSeed(seed[:])
	signature := ed25519.Sign(privateKey, payload)
	clear(privateKey)
	return append(payload, signature...)
}

func assertPendingCredential(t *testing.T, response protocol.Response, pendingID string, fingerprint [sha256.Size]byte, expires int64) {
	t.Helper()
	if response.RequestID != syntheticTestRequestID || response.Outcome != protocol.OutcomePending || response.PendingItemID != pendingID ||
		response.CanonicalABN != syntheticTestABN || !bytes.Equal(response.CredentialFingerprint, fingerprint[:]) ||
		response.CredentialCreatedMillis != expires-syntheticCredentialLifetimeMillis || response.CredentialExpiresMillis != expires || response.ComponentVersion != SyntheticComponentVersion {
		t.Fatalf("prepared = %#v", response)
	}
}

func assertZeroed(t *testing.T, value []byte) {
	t.Helper()
	for _, b := range value {
		if b != 0 {
			t.Fatalf("secret not zeroed: %x", value)
		}
	}
}
