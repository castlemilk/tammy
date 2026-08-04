package audit

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"reflect"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
)

type EvidenceProvider interface {
	Collect(context.Context, ExportJob) ([]EvidenceObject, error)
}

// EvidenceExportDEKProvider issues short-lived, workspace-scoped leases for
// evidence archive signing. Implementations must not return a lease after the
// workspace has been locked or its unlock authority has been revoked.
type EvidenceExportDEKProvider interface {
	Acquire(context.Context, string) (EvidenceExportDEKLease, error)
}

// EvidenceExportDEKLease confines plaintext DEK access to a synchronous
// callback. Close revokes the lease and zeros any plaintext key material held
// by the lease; it must be safe to call exactly once after Acquire succeeds.
type EvidenceExportDEKLease interface {
	WithDEK(func([]byte) error) error
	Close()
}

type EvidenceExportWorkerConfig struct {
	Transactions         ExportTransactions
	Destinations         ExportDestinationResolver
	EvidenceProviders    map[string]EvidenceProvider
	ProviderRegistry     *EvidenceProviderRegistry
	DEKProvider          EvidenceExportDEKProvider
	Clock                clock.Clock
	Gate                 *WriteGate
	SpoolParentDirectory string
}

type EvidenceExportWorker struct {
	transactions ExportTransactions
	destinations ExportDestinationResolver
	providers    map[string]EvidenceProvider
	dekProvider  EvidenceExportDEKProvider
	clock        clock.Clock
	gate         *WriteGate
	spoolParent  string
}

func NewEvidenceExportWorker(config EvidenceExportWorkerConfig) (*EvidenceExportWorker, error) {
	registryProvided := !nilInterface(config.ProviderRegistry)
	if config.Transactions == nil || config.Destinations == nil ||
		(len(config.EvidenceProviders) == 0 && !registryProvided) || (len(config.EvidenceProviders) != 0 && registryProvided) ||
		nilInterface(config.DEKProvider) || config.Clock == nil || config.Gate == nil {
		return nil, ErrExportJob
	}
	var providers map[string]EvidenceProvider
	if registryProvided {
		providers = config.ProviderRegistry.workerProviders()
		if len(providers) == 0 {
			return nil, ErrExportJob
		}
	} else {
		providers = make(map[string]EvidenceProvider, len(config.EvidenceProviders))
		for name, provider := range config.EvidenceProviders {
			if name == "" || nilInterface(provider) {
				return nil, ErrExportJob
			}
			providers[name] = provider
		}
	}
	spoolParent, err := validatedEvidenceSpoolParent(config.SpoolParentDirectory)
	if err != nil {
		return nil, ErrExportJob
	}
	return &EvidenceExportWorker{transactions: config.Transactions, destinations: config.Destinations,
		providers: providers, dekProvider: config.DEKProvider, clock: config.Clock, gate: config.Gate,
		spoolParent: spoolParent}, nil
}

func validatedEvidenceSpoolParent(configured string) (string, error) {
	parent := configured
	if parent == "" {
		parent = os.TempDir()
	} else if !filepath.IsAbs(parent) || filepath.Clean(parent) != parent {
		return "", ErrExportJob
	}
	info, err := os.Lstat(parent)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", ErrExportJob
	}
	return parent, nil
}

func nilInterface(candidate any) bool {
	if candidate == nil {
		return true
	}
	value := reflect.ValueOf(candidate)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func (worker *EvidenceExportWorker) Run(ctx context.Context, jobID string) (ExportJob, error) {
	if worker == nil || !exportReferencePattern.MatchString(jobID) {
		return ExportJob{}, ErrExportJob
	}
	if !worker.gate.EvidenceExportAllowed() {
		return ExportJob{}, ErrWriteGate
	}
	now := worker.clock.Now().UTC()
	var job ExportJob
	if err := worker.transactions.Mutate(ctx, func(executor ServiceTransaction) error {
		var err error
		job, err = ClaimExportJob(ctx, executor, jobID, now)
		return err
	}); err != nil {
		return ExportJob{}, err
	}

	provider := worker.providers[job.EvidenceProvider]
	if provider == nil {
		return worker.fail(ctx, job, "EVIDENCE_PROVIDER_UNAVAILABLE")
	}
	evidence, err := provider.Collect(ctx, job)
	if err != nil {
		return worker.fail(ctx, job, "EVIDENCE_COLLECTION_FAILED")
	}
	if err := preflightEvidenceObjects(evidence, workerEvidenceReservedMembers, workerEvidenceReservedBytes); err != nil {
		return worker.fail(ctx, job, "ARCHIVE_BUILD_FAILED")
	}
	spool, err := newStreamingEvidenceSpool(worker.spoolParent)
	if err != nil {
		return worker.fail(ctx, job, "ARCHIVE_BUILD_FAILED")
	}
	defer spool.cleanup()
	var prepared *preparedStreamingEvidenceArchive
	if err := worker.transactions.Read(ctx, func(executor ServiceTransaction) error {
		var prepareErr error
		prepared, prepareErr = prepareStreamingEvidenceArchive(ctx, executor, job, evidence, spool)
		return prepareErr
	}); err != nil {
		return worker.fail(ctx, job, "SNAPSHOT_COLLECTION_FAILED")
	}
	archive, err := prepared.build(func(record SigningKeyRecord, manifestHash [sha256.Size]byte) ([]byte, error) {
		return worker.signEvidenceManifest(ctx, job.WorkspaceID, record, manifestHash)
	})
	if err != nil {
		return worker.fail(ctx, job, "ARCHIVE_BUILD_FAILED")
	}
	reference := job.ResultRef
	if reference == "" {
		reference = job.DestinationCapability
	}
	destination, err := worker.destinations.Resolve(reference)
	if err != nil || destination == nil || destination.Reference() != reference {
		return worker.fail(ctx, job, "DESTINATION_UNAVAILABLE")
	}
	if err := worker.transactions.Mutate(ctx, func(executor ServiceTransaction) error {
		var authorizeErr error
		job, authorizeErr = AuthorizeExportDestination(ctx, executor, job, archive, reference,
			jobProgress("ARCHIVE_VERIFIED", job.Progress), worker.clock.Now().UTC())
		return authorizeErr
	}); err != nil {
		return ExportJob{}, err
	}
	return CommitAuthorizedExport(ctx, worker.transactions, worker.gate, job.ID, archive, destination, worker.clock.Now().UTC())
}

func (worker *EvidenceExportWorker) buildSignedEvidenceArchive(
	ctx context.Context,
	workspaceID string,
	input EvidenceArchiveInput,
) ([]byte, error) {
	return buildSignedEvidenceArchiveWithSigner(input, func(record SigningKeyRecord, manifestHash [sha256.Size]byte) ([]byte, error) {
		return worker.signEvidenceManifest(ctx, workspaceID, record, manifestHash)
	})
}

func (worker *EvidenceExportWorker) signEvidenceManifest(
	ctx context.Context,
	workspaceID string,
	record SigningKeyRecord,
	manifestHash [sha256.Size]byte,
) ([]byte, error) {
	lease, err := worker.dekProvider.Acquire(ctx, workspaceID)
	if nilInterface(lease) {
		return nil, ErrExportJob
	}
	defer lease.Close()
	if err != nil {
		return nil, ErrExportJob
	}

	var signature []byte
	callbackCalls := 0
	err = lease.WithDEK(func(dek []byte) error {
		callbackCalls++
		if callbackCalls != 1 || ctx.Err() != nil {
			return ErrExportJob
		}
		var signErr error
		signature, signErr = SignManifestHash(record, dek, manifestHash)
		dek = nil
		return signErr
	})
	if err != nil || callbackCalls != 1 || ctx.Err() != nil || len(signature) == 0 {
		return nil, ErrExportJob
	}
	return signature, nil
}

func (worker *EvidenceExportWorker) fail(ctx context.Context, job ExportJob, stage string) (ExportJob, error) {
	var failed ExportJob
	err := worker.transactions.Mutate(context.WithoutCancel(ctx), func(executor ServiceTransaction) error {
		var failErr error
		failed, failErr = FailExportJob(context.WithoutCancel(ctx), executor, job.ID, job.Version, true, stage, worker.clock.Now().UTC())
		return failErr
	})
	if err != nil {
		return ExportJob{}, errors.Join(ErrExportJob, err)
	}
	return failed, ErrExportJob
}

func jobProgress(stage string, existing *tammyv1.JobProgress) *tammyv1.JobProgress {
	return &tammyv1.JobProgress{Stage: stage, TotalUnits: existing.GetTotalUnits(), CompletedUnits: existing.GetTotalUnits()}
}
