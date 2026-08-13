package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/authorisation"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/idempotency"
	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
)

type factoryTransactions struct{ workspaceID string }

func (transactions factoryTransactions) WorkspaceID() string { return transactions.workspaceID }
func (factoryTransactions) Read(context.Context, func(ServiceTransaction) error) error {
	return errors.New("not called by construction")
}
func (factoryTransactions) Mutate(context.Context, func(ServiceTransaction) error) error {
	return errors.New("not called by construction")
}

type factoryAccess struct{}

func (factoryAccess) Require(context.Context, ServiceTransaction, *tammyv1.AuthenticationContext, authorisation.Action) error {
	return nil
}

type factoryMirror struct{}

func (factoryMirror) Load(context.Context, string) (*tammyv1.AuditMirrorBaseline, error) {
	return nil, ErrMirrorMissing
}
func (factoryMirror) CompareAndSwap(context.Context, *tammyv1.AuditMirrorBaseline, *tammyv1.AuditMirrorBaseline) error {
	return nil
}

func validProductionFactoryConfig(t *testing.T) ProductionFactoryConfig {
	t.Helper()
	instant := time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC)
	coreClock := clock.NewFixed(instant)
	elector, err := idempotency.NewElector(idempotency.Config{
		Clock: coreClock,
		Observe: func(context.Context, idempotency.Scope) (idempotency.Record, error) {
			return idempotency.Record{}, idempotency.ErrRepository
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	cursors, err := paging.NewCodec(bytes.Repeat([]byte{0x71}, sha256.Size))
	if err != nil {
		t.Fatal(err)
	}
	return ProductionFactoryConfig{
		Access:       factoryAccess{},
		Transactions: factoryTransactions{workspaceID: "01890f60-4d6d-7c12-8f02-6c9129d5b001"},
		Elector:      elector, Clock: coreClock,
		NewID:             func() (string, error) { return "01890f60-4d6d-7c12-8f02-6c9129d5b091", nil },
		Cursors:           cursors,
		SchemaFingerprint: bytes.Repeat([]byte{0x44}, sha256.Size),
		Mirror:            factoryMirror{},
		TrustProof: trustProofPortFunc(func(context.Context, Executor, TrustProofKind) (AuthenticatedTrustProof, error) {
			return AuthenticatedTrustProof{}, ErrTrustProof
		}),
		EvidenceProviders: []EvidenceProviderRegistration{{Name: "audit_chain", Provider: evidencePortFunc(func(
			context.Context, ExportJob,
		) ([]EvidenceObject, error) {
			return nil, nil
		})}},
		DEKProvider: registryWorkerDEKProvider{},
		Destinations: ApprovedDestinationConfig{BaseDirectory: t.TempDir(), Capacity: 2,
			NewID: func() (string, error) { return "01890f60-4d6d-7c12-8f02-6c9129d5b092", nil }},
	}
}

func TestProductionFactoryConstructsAuditOwnedHandlerBoundary(t *testing.T) {
	factory, err := NewProductionFactory(validProductionFactoryConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = factory.Close() })
	if factory.Service == nil || factory.SQLMirrorVerifier == nil || factory.TrustCoordinator == nil ||
		factory.MirrorReconciler == nil || factory.EvidenceProviders == nil || factory.ApprovedDestinations == nil ||
		factory.EvidenceExportWorker == nil || factory.AuditHandler == nil || factory.AuditHandlerPath == "" {
		t.Fatalf("factory missing production component: %#v", factory)
	}
}

type typedNilTrustProofPort struct{}

func (*typedNilTrustProofPort) VerifyTrustProof(context.Context, Executor, TrustProofKind) (AuthenticatedTrustProof, error) {
	return AuthenticatedTrustProof{}, nil
}

func TestProductionFactoryRejectsTypedNilBeforeOpeningDestination(t *testing.T) {
	config := validProductionFactoryConfig(t)
	var trustProof *typedNilTrustProofPort
	config.TrustProof = trustProof
	openCalls := 0
	config.Destinations.hooks = &destinationFSHooks{openRoot: func(name string) (*os.Root, error) {
		openCalls++
		return os.OpenRoot(name)
	}}
	if _, err := NewProductionFactory(config); !errors.Is(err, ErrProductionFactory) || openCalls != 0 {
		t.Fatalf("factory error=%v destination opens=%d, want pre-side-effect rejection", err, openCalls)
	}
}

func TestProductionFactoryClosesOpenedRootOnPartialConstructionFailure(t *testing.T) {
	config := validProductionFactoryConfig(t)
	spoolParent := filepath.Join(t.TempDir(), "spool")
	if err := os.Mkdir(spoolParent, 0o700); err != nil {
		t.Fatal(err)
	}
	spoolReplacement := t.TempDir()
	config.SpoolParentDirectory = spoolParent
	var opened *os.Root
	config.Destinations.hooks = &destinationFSHooks{openRoot: func(name string) (*os.Root, error) {
		root, err := os.OpenRoot(name)
		if err != nil {
			return nil, err
		}
		opened = root
		if err := os.Rename(spoolParent, spoolParent+"-original"); err != nil {
			return nil, err
		}
		if err := os.Symlink(spoolReplacement, spoolParent); err != nil {
			return nil, err
		}
		return root, nil
	}}
	if _, err := NewProductionFactory(config); !errors.Is(err, ErrProductionFactory) {
		t.Fatalf("factory partial error=%v, want ErrProductionFactory", err)
	}
	if opened == nil {
		t.Fatal("factory did not reach destination open boundary")
	}
	if _, err := opened.Stat("."); err == nil {
		t.Fatal("factory left destination root open after partial construction")
	}
}
