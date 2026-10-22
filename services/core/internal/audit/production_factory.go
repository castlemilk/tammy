package audit

import (
	"crypto/sha256"
	"errors"
	"net/http"

	"connectrpc.com/connect"
	tammyv1connect "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1/tammyv1connect"
	"github.com/tammyapp/tammy/services/core/internal/idempotency"
	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
)

var ErrProductionFactory = errors.New("audit: production factory failed")

// ProductionFactoryConfig contains only audit-owned composition inputs. The
// caller still owns database opening, transaction lifecycles, and route
// registration.
type ProductionFactoryConfig struct {
	Access               ServiceAccess
	Transactions         ServiceTransactions
	Elector              *idempotency.Elector
	Clock                clock.Clock
	NewID                func() (string, error)
	Cursors              *paging.Codec
	SchemaFingerprint    []byte
	Mirror               MirrorStore
	TrustProof           AuthenticatedTrustProofPort
	EvidenceProviders    []EvidenceProviderRegistration
	DEKProvider          EvidenceExportDEKProvider
	Destinations         ApprovedDestinationConfig
	SpoolParentDirectory string
	HandlerOptions       []connect.HandlerOption
}

type ProductionFactory struct {
	Service              *Service
	SQLMirrorVerifier    *SQLMirrorVerifier
	TrustCoordinator     *TrustCoordinator
	MirrorReconciler     *MirrorReconciler
	EvidenceProviders    *EvidenceProviderRegistry
	ApprovedDestinations *ApprovedDestinationRegistry
	EvidenceExportWorker *EvidenceExportWorker
	WriteGate            *WriteGate
	Appender             *Appender
	AuditHandlerPath     string
	AuditHandler         http.Handler
}

func NewProductionFactory(config ProductionFactoryConfig) (*ProductionFactory, error) {
	if err := validateProductionFactoryConfig(config); err != nil {
		return nil, err
	}

	verifier, err := NewSQLMirrorVerifier(config.Transactions)
	if err != nil {
		return nil, ErrProductionFactory
	}
	trustProof, err := NewAuthenticatedTrustProofVerifier(config.TrustProof)
	if err != nil {
		return nil, ErrProductionFactory
	}
	providers, err := NewEvidenceProviderRegistry(config.EvidenceProviders)
	if err != nil {
		return nil, ErrProductionFactory
	}
	gate := NewWriteGate()
	appender, err := NewMirroringAppender(config.Mirror, gate)
	if err != nil {
		return nil, ErrProductionFactory
	}
	service, err := NewService(ServiceConfig{
		Access: config.Access, Transactions: config.Transactions, Elector: config.Elector,
		Clock: config.Clock, NewID: config.NewID, Cursors: config.Cursors,
		SchemaFingerprint: append([]byte(nil), config.SchemaFingerprint...), Appender: appender,
	})
	if err != nil {
		return nil, ErrProductionFactory
	}
	destinations, err := NewApprovedDestinationRegistry(config.Destinations)
	if err != nil {
		return nil, ErrProductionFactory
	}
	complete := false
	defer func() {
		if !complete {
			_ = destinations.Close()
		}
	}()
	worker, err := NewEvidenceExportWorker(EvidenceExportWorkerConfig{
		Transactions: config.Transactions, Destinations: destinations, ProviderRegistry: providers,
		DEKProvider: config.DEKProvider, Clock: config.Clock, Gate: gate,
		SpoolParentDirectory: config.SpoolParentDirectory,
	})
	if err != nil {
		return nil, ErrProductionFactory
	}
	path, handler := tammyv1connect.NewAuditServiceHandler(service, append([]connect.HandlerOption(nil), config.HandlerOptions...)...)
	factory := &ProductionFactory{
		Service: service, SQLMirrorVerifier: verifier,
		TrustCoordinator:  NewTrustCoordinator(trustProof, verifier, appender),
		MirrorReconciler:  NewMirrorReconciler(config.Mirror, verifier, gate),
		EvidenceProviders: providers, ApprovedDestinations: destinations, EvidenceExportWorker: worker,
		WriteGate: gate, Appender: appender, AuditHandlerPath: path, AuditHandler: handler,
	}
	complete = true
	return factory, nil
}

func validateProductionFactoryConfig(config ProductionFactoryConfig) error {
	if nilInterface(config.Access) || nilInterface(config.Transactions) || !ids.IsCanonicalV7(config.Transactions.WorkspaceID()) ||
		nilInterface(config.Elector) || nilInterface(config.Clock) || nilInterface(config.NewID) || nilInterface(config.Cursors) ||
		len(config.SchemaFingerprint) != sha256.Size || nilInterface(config.Mirror) || nilInterface(config.TrustProof) ||
		len(config.EvidenceProviders) == 0 || nilInterface(config.DEKProvider) || !validApprovedDestinationConfig(config.Destinations) {
		return ErrProductionFactory
	}
	for _, option := range config.HandlerOptions {
		if nilInterface(option) {
			return ErrProductionFactory
		}
	}
	if _, err := validatedEvidenceSpoolParent(config.SpoolParentDirectory); err != nil {
		return ErrProductionFactory
	}
	return nil
}

func (factory *ProductionFactory) Close() error {
	if factory == nil || factory.ApprovedDestinations == nil {
		return nil
	}
	if err := factory.ApprovedDestinations.Close(); err != nil {
		return ErrProductionFactory
	}
	return nil
}
