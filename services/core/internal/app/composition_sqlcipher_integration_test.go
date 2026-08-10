//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/audit"
	"github.com/tammyapp/tammy/services/core/internal/authorisation"
	"github.com/tammyapp/tammy/services/core/internal/buildinfo"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	tammyv1connect "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1/tammyv1connect"
	"github.com/tammyapp/tammy/services/core/internal/idempotency"
	"github.com/tammyapp/tammy/services/core/internal/identity"
	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/transport"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
)

type compositionIdentityAudit struct{}

func (compositionIdentityAudit) Record(ctx context.Context, executor workspace.MutationExecutor, mutation, subject string) error {
	if mutation == "" || subject == "" {
		return errors.New("invalid identity audit record")
	}
	_, err := executor.ExecContext(ctx, `INSERT INTO composition_identity_audit(mutation,subject) VALUES(?,?)`, mutation, subject)
	return err
}

type failClosedSessionLifecycle struct{}

func (failClosedSessionLifecycle) SessionStartedWithin(context.Context, workspace.MutationExecutor, string) error {
	return errors.New("test composition does not authorize session mutation")
}
func (failClosedSessionLifecycle) SessionStartedAuditedWithin(context.Context, workspace.MutationExecutor) error {
	return errors.New("test composition does not authorize session mutation")
}
func (failClosedSessionLifecycle) SessionStartedCommitted(context.Context) error {
	return errors.New("test composition does not authorize session mutation")
}

type failClosedAuditMirror struct{}

func (failClosedAuditMirror) Load(context.Context, string) (*tammyv1.AuditMirrorBaseline, error) {
	return nil, audit.ErrMirrorMissing
}
func (failClosedAuditMirror) CompareAndSwap(context.Context, *tammyv1.AuditMirrorBaseline, *tammyv1.AuditMirrorBaseline) error {
	return audit.ErrMirrorMissing
}

type realAuditTransactions struct {
	database    *sqlcipher.Database
	workspaceID string
}

func (transactions realAuditTransactions) WorkspaceID() string { return transactions.workspaceID }
func (transactions realAuditTransactions) Read(ctx context.Context, work func(audit.ServiceTransaction) error) error {
	return transactions.run(ctx, true, work)
}
func (transactions realAuditTransactions) Mutate(ctx context.Context, work func(audit.ServiceTransaction) error) error {
	return transactions.run(ctx, false, work)
}
func (transactions realAuditTransactions) run(ctx context.Context, readOnly bool, work func(audit.ServiceTransaction) error) error {
	raw, err := transactions.database.BeginEncryptedTx(ctx, &sql.TxOptions{ReadOnly: readOnly})
	if err != nil {
		return err
	}
	transaction := &realAuditTransaction{Transaction: raw}
	if err := work(transaction); err != nil {
		_ = raw.Rollback()
		return err
	}
	return raw.Commit()
}

type realAuditTransaction struct{ *sqlcipher.Transaction }

func (*realAuditTransaction) TransactionID() string { return "composition-real-sqlcipher" }
func (*realAuditTransaction) AfterCommit(func(context.Context) error) error {
	return errors.New("test composition has no writable mirror")
}

type failClosedAuditAccess struct{}

func (failClosedAuditAccess) Require(context.Context, audit.ServiceTransaction, *tammyv1.AuthenticationContext, authorisation.Action) error {
	return errors.New("test composition denies audit access")
}

func TestRealSQLCipherWorkspaceCompositionBootsCurrentHandlersAndOmitsFutureServices(t *testing.T) {
	database := newCompositionSQLCipherDatabase(t)
	now := clock.NewFixed(time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC))
	passwords, err := workspace.NewPasswordPolicy(nil, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	attempts, err := workspace.NewAttemptJournal(filepath.Join(t.TempDir(), "attempts.json"), make([]byte, 32), now,
		"composition", workspace.NewMemoryAnchorStore())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(attempts.Close)
	identifierGenerator, err := ids.NewGenerator(now, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	identityRepository, err := identity.NewDatabaseRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	identityService, err := identity.NewService(identity.Config{
		Repository: identityRepository, Passwords: passwords, Clock: now, Random: rand.Reader,
		IDs: identifierGenerator, Attempts: attempts, FactorEncryptionKey: make([]byte, 32),
		Audit: compositionIdentityAudit{}, SessionLifecycle: failClosedSessionLifecycle{},
	})
	if err != nil {
		t.Fatal(err)
	}

	transactions := realAuditTransactions{database: database, workspaceID: commandWorkspaceID}
	observer, err := idempotency.NewObserver(database)
	if err != nil {
		t.Fatal(err)
	}
	elector, err := idempotency.NewElector(idempotency.Config{Clock: now, Observe: observer})
	if err != nil {
		t.Fatal(err)
	}
	cursors, err := paging.NewCodec(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	gate := audit.NewWriteGate()
	appender, err := audit.NewMirroringAppender(failClosedAuditMirror{}, gate)
	if err != nil {
		t.Fatal(err)
	}
	auditService, err := audit.NewService(audit.ServiceConfig{
		Access: failClosedAuditAccess{}, Transactions: transactions, Elector: elector, Clock: now,
		NewID: identifierGenerator.New, Cursors: cursors, SchemaFingerprint: make([]byte, sha256.Size), Appender: appender,
	})
	if err != nil {
		t.Fatal(err)
	}
	composition, err := NewWorkspaceComposition(WorkspaceCompositionConfig{
		Info: buildinfo.Info{Version: "real-workspace"}, Identity: identityService, Audit: auditService,
		Resources: []ResourceCloser{database, identityService},
	})
	if err != nil {
		t.Fatalf("NewWorkspaceComposition() error = %v", err)
	}
	server, err := transport.NewServer(composition.Registrar(), io.Discard)
	if err != nil {
		t.Fatalf("transport.NewServer() error = %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatalf("server.Start() error = %v", err)
	}
	ready := server.Ready()
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(ready.CAPEM)) {
		t.Fatal("invalid server CA")
	}
	httpClient := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, RootCAs: roots, ServerName: "127.0.0.1",
	}}}
	baseURL := fmt.Sprintf("https://127.0.0.1:%d", ready.Port)
	t.Cleanup(func() {
		shutdownContext, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownContext)
	})

	systemClient := tammyv1connect.NewSystemServiceClient(httpClient, baseURL)
	diagnosticsRequest := connect.NewRequest(&tammyv1.GetDiagnosticsRequest{})
	diagnosticsRequest.Header().Set(transport.CapabilityHeader, ready.Capability)
	if response, err := systemClient.GetDiagnostics(context.Background(), diagnosticsRequest); err != nil || response.Msg.CoreVersion != "real-workspace" {
		t.Fatalf("GetDiagnostics() = %#v, %v", response, err)
	}
	for _, path := range []string{
		tammyv1connect.IdentityServiceGetSessionProcedure,
		tammyv1connect.AuditServiceVerifyChainProcedure,
	} {
		assertHTTPStatusNot(t, httpClient, baseURL+path, http.StatusNotFound)
	}
	for _, path := range []string{
		tammyv1connect.WorkspaceServiceGetWorkspaceStateProcedure,
		tammyv1connect.OrganisationServiceGetOrganisationProcedure,
		tammyv1connect.AccountingServiceGetAccountProcedure,
		tammyv1connect.OverviewServiceGetAttentionSummaryProcedure,
		"/tammy.v1.UndeclaredService/Get",
	} {
		assertHTTPStatus(t, httpClient, baseURL+path, http.StatusNotFound)
	}
	shutdownContext, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		t.Fatalf("server.Shutdown() error = %v", err)
	}
	if err := composition.Close(); err != nil {
		t.Fatalf("composition.Close() error = %v", err)
	}
}

func newCompositionSQLCipherDatabase(t *testing.T) *sqlcipher.Database {
	t.Helper()
	path := filepath.Join(t.TempDir(), "composition.db")
	key := make([]byte, sqlcipher.KeySize)
	for index := range key {
		key[index] = byte(index + 31)
	}
	if _, err := sqlcipher.MigrateWorkspace(context.Background(), path, key, 4); err != nil {
		t.Fatalf("MigrateWorkspace() error = %v", err)
	}
	database, err := sqlcipher.Open(context.Background(), path, key)
	clear(key)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if _, err := database.ExecContext(context.Background(),
		`CREATE TABLE composition_identity_audit(mutation TEXT NOT NULL,subject TEXT NOT NULL)`); err != nil {
		_ = database.Close()
		t.Fatalf("create identity audit fixture: %v", err)
	}
	return database
}
