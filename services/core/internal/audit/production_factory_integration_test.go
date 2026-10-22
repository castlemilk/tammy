//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	tammyv1connect "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1/tammyv1connect"
	"github.com/tammyapp/tammy/services/core/internal/idempotency"
	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
)

func TestProductionFactoryEncryptedConnectBoundaryUsesConcreteVerifierProviderAndDestination(t *testing.T) {
	ctx := context.Background()
	database := newEncryptedAuditDatabase(t)
	workspaceID := "01890f60-4d6d-7c12-8f02-6c9129d5b001"
	salt := bytes.Repeat([]byte{0x23}, sha256.Size)
	genesis, err := Genesis(workspaceID, salt)
	if err != nil {
		t.Fatal(err)
	}
	if err := InitializeChain(ctx, database, ChainHeader{WorkspaceID: workspaceID, Generation: 1,
		ChainSalt: salt, GenesisHash: genesis, CurrentHead: genesis,
		CreatedAt: time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	event, payload := integrationAuditEvent("01890f60-4d6d-7c12-8f02-6c9129d5b093")
	if _, err := appendStoredEventForTest(ctx, database, event, payload); err != nil {
		t.Fatal(err)
	}

	coreClock := clock.NewFixed(time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC))
	elector, err := idempotency.NewElector(idempotency.Config{Clock: coreClock,
		Observe: func(context.Context, idempotency.Scope) (idempotency.Record, error) {
			return idempotency.Record{}, idempotency.ErrRepository
		}})
	if err != nil {
		t.Fatal(err)
	}
	cursors, err := paging.NewCodec(bytes.Repeat([]byte{0x71}, sha256.Size))
	if err != nil {
		t.Fatal(err)
	}
	transactions := &auditServiceTransactions{database: database, workspaceID: workspaceID}
	proofActor := &tammyv1.AuthenticationContext{
		ActorUserId: "01890f60-4d6d-7c12-8f02-6c9129d5b006",
		SessionId:   "01890f60-4d6d-7c12-8f02-6c9129d5b007",
	}
	factory, err := NewProductionFactory(ProductionFactoryConfig{
		Access: factoryAccess{}, Transactions: transactions,
		Elector: elector, Clock: coreClock,
		NewID:   func() (string, error) { return "01890f60-4d6d-7c12-8f02-6c9129d5b094", nil },
		Cursors: cursors, SchemaFingerprint: bytes.Repeat([]byte{0x44}, sha256.Size), Mirror: factoryMirror{},
		TrustProof: trustProofPortFunc(func(context.Context, Executor, TrustProofKind) (AuthenticatedTrustProof, error) {
			return AuthenticatedTrustProof{Kind: TrustProofNormal, Actor: proofActor, PassphraseVerified: true,
				AdministratorPasswordVerified: true, FreshTOTPVerified: true}, nil
		}),
		EvidenceProviders: []EvidenceProviderRegistration{{Name: "audit_chain", Provider: evidencePortFunc(func(
			context.Context, ExportJob,
		) ([]EvidenceObject, error) {
			return []EvidenceObject{{Path: "provider/receipt.json", Bytes: []byte(`{"verified":true}`)}}, nil
		})}},
		DEKProvider: registryWorkerDEKProvider{},
		Destinations: ApprovedDestinationConfig{BaseDirectory: t.TempDir(), Capacity: 1,
			NewID: func() (string, error) { return "01890f60-4d6d-7c12-8f02-6c9129d5b095", nil }},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = factory.Close() })

	server := httptest.NewServer(factory.AuditHandler)
	t.Cleanup(server.Close)
	client := tammyv1connect.NewAuditServiceClient(server.Client(), server.URL)
	generation := uint64(1)
	response, err := client.VerifyChain(ctx, connect.NewRequest(&tammyv1.VerifyChainRequest{
		Authentication: &tammyv1.AuthenticationContext{
			ActorUserId: "01890f60-4d6d-7c12-8f02-6c9129d5b006",
			SessionId:   "01890f60-4d6d-7c12-8f02-6c9129d5b007",
		},
		WorkspaceId: workspaceID, Generation: &generation,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if response.Msg.Integrity != tammyv1.AuditChainIntegrity_AUDIT_CHAIN_INTEGRITY_VALID ||
		response.Msg.VerifiedThroughSequence != 1 {
		t.Fatalf("VerifyChain response=%#v", response.Msg)
	}
	if err := transactions.Read(ctx, func(transaction ServiceTransaction) error {
		approval, verifyErr := factory.TrustCoordinator.proof.Verify(ctx, transaction, TrustProofNormal)
		if verifyErr != nil || !validTrustApproval(TrustProofNormal, approval) || approval.Actor == proofActor {
			t.Fatalf("trust approval=%#v error=%v", approval, verifyErr)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	reference, err := factory.ApprovedDestinations.Approve("evidence.zip")
	if err != nil {
		t.Fatal(err)
	}
	destination, err := factory.ApprovedDestinations.Resolve(reference)
	if err != nil {
		t.Fatal(err)
	}
	if err := destination.AtomicCommit(ctx, []byte("factory destination boundary")); err != nil {
		t.Fatal(err)
	}
	if _, err := factory.EvidenceProviders.workerProviders()["audit_chain"].Collect(ctx, ExportJob{}); err != nil {
		t.Fatal(err)
	}
}
