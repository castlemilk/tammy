// Package sbr owns durable, redacted SBR readiness state.
package sbr

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"time"
)

var (
	ErrRepository            = errors.New("sbr: repository failure")
	ErrInvalid               = errors.New("sbr: invalid redacted state")
	ErrNotFound              = errors.New("sbr: binding not found")
	ErrPermissionDenied      = errors.New("sbr: binding permission denied")
	ErrConflict              = errors.New("sbr: durable state conflict")
	ErrInvalidTransition     = errors.New("sbr: invalid state transition")
	ErrIdempotencyConflict   = errors.New("sbr: idempotency conflict")
	ErrUncertainTransport    = errors.New("sbr: uncertain transport outcome")
	ErrHelperDeadlineExpired = errors.New("SBR_DEADLINE_EXPIRED")
)

type BindingKey struct {
	WorkspaceID           string
	OrganisationID        string
	CanonicalABN          string
	SchemaVersion         uint32
	CredentialFingerprint [sha256.Size]byte
}

type MutationKind string

const (
	MutationImportCredential  MutationKind = "IMPORT_CREDENTIAL"
	MutationReplaceCredential MutationKind = "REPLACE_CREDENTIAL"
	MutationRemoveCredential  MutationKind = "REMOVE_CREDENTIAL"
	MutationImportProductID   MutationKind = "IMPORT_PRODUCT_ID"
	MutationRemoveProductID   MutationKind = "REMOVE_PRODUCT_ID"
)

type TransportState string

const (
	TransportPrepared         TransportState = "PREPARED"
	TransportDispatching      TransportState = "DISPATCHING"
	TransportNotStarted       TransportState = "NOT_STARTED"
	TransportMaybeSent        TransportState = "MAYBE_SENT"
	TransportResponseReceived TransportState = "RESPONSE_RECEIVED"
	TransportAccepted         TransportState = "ACCEPTED"
	TransportFailed           TransportState = "FAILED"
	TransportUnknown          TransportState = "UNKNOWN"
)

type SimulatorCase string

const (
	SimulatorCasePreDispatchFailure SimulatorCase = "PRE_DISPATCH_FAILURE"
	SimulatorCaseUncertainWrite     SimulatorCase = "UNCERTAIN_WRITE"
	SimulatorCaseHelperDeath        SimulatorCase = "HELPER_DEATH"
	SimulatorCaseTimeout            SimulatorCase = "TIMEOUT"
	SimulatorCaseSyntacticResponse  SimulatorCase = "SYNTACTIC_RESPONSE"
	SimulatorCaseMalformedResponse  SimulatorCase = "MALFORMED_RESPONSE"
	SimulatorCaseAccepted           SimulatorCase = "ACCEPTED"
)

type HelperDispatchState string

const (
	HelperDispatching       HelperDispatchState = "DISPATCHING"
	HelperDispatchCompleted HelperDispatchState = "COMPLETED"
	HelperDispatchFailed    HelperDispatchState = "FAILED"
	HelperDispatchUnknown   HelperDispatchState = "UNKNOWN"
)

type HelperDispatchRecord struct {
	OperationID    string
	ActorUserID    string
	Key            BindingKey
	IdempotencyKey string
	SemanticHash   [sha256.Size]byte
	State          HelperDispatchState
	CreatedAt      string
	UpdatedAt      string
}

func validHelperDispatchTerminal(state HelperDispatchState) bool {
	return state == HelperDispatchCompleted || state == HelperDispatchFailed || state == HelperDispatchUnknown
}

// SanitizeBackupState disconnects a fixed backup copy from helper-owned
// pending items. It operates only on caller-owned staged storage and performs
// no helper, Keychain, file, or network operation.
func SanitizeBackupState(ctx context.Context, executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}) error {
	if ctx == nil || executor == nil || ctx.Err() != nil {
		return ErrInvalid
	}
	present, err := sbrSchemaPresent(ctx, executor)
	if err != nil || !present {
		return err
	}
	for _, statement := range []string{
		`UPDATE sbr_mutations_v1 SET mutation_state='ABORTED',pending_id=NULL WHERE mutation_state IN ('PREPARED','STAGED','CORE_COMMITTED','RECONCILE_REQUIRED','ABORT_REQUIRED','ABORTING')`,
		`UPDATE sbr_simulator_transports_v1 SET state='UNKNOWN' WHERE state IN ('DISPATCHING','MAYBE_SENT')`,
		`UPDATE sbr_helper_dispatches_v1 SET state='UNKNOWN' WHERE state='DISPATCHING'`,
	} {
		if _, err := executor.ExecContext(ctx, statement); err != nil {
			return ErrRepository
		}
	}
	return nil
}

func VerifyBackupState(ctx context.Context, executor interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}) error {
	if ctx == nil || executor == nil || ctx.Err() != nil {
		return ErrInvalid
	}
	present, err := sbrSchemaPresent(ctx, executor)
	if err != nil || !present {
		return err
	}
	for _, query := range []string{
		`SELECT COUNT(*) FROM sbr_mutations_v1 WHERE mutation_state IN ('PREPARED','STAGED','CORE_COMMITTED','RECONCILE_REQUIRED','ABORT_REQUIRED','ABORTING') OR (mutation_state IN ('ABORTED','HELPER_COMMITTED') AND pending_id IS NOT NULL)`,
		`SELECT COUNT(*) FROM sbr_simulator_transports_v1 WHERE state IN ('DISPATCHING','MAYBE_SENT')`,
		`SELECT COUNT(*) FROM sbr_helper_dispatches_v1 WHERE state='DISPATCHING'`,
	} {
		rows, queryErr := executor.QueryContext(ctx, query)
		if queryErr != nil {
			return ErrRepository
		}
		var count int64
		valid := rows.Next() && rows.Scan(&count) == nil && count == 0 && !rows.Next() && rows.Err() == nil
		_ = rows.Close()
		if !valid {
			return ErrRepository
		}
	}
	return nil
}

// MarkRestoredState invalidates restored opaque bindings without opening the
// helper or vault. A user must explicitly re-import before SBR can be ready.
func MarkRestoredState(ctx context.Context, executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, updatedAt string) error {
	if ctx == nil || executor == nil || ctx.Err() != nil || !validTimestamp(updatedAt) {
		return ErrInvalid
	}
	present, err := sbrSchemaPresent(ctx, executor)
	if err != nil || !present {
		return err
	}
	if err := SanitizeBackupState(ctx, executor); err != nil {
		return err
	}
	if _, err := executor.ExecContext(ctx, `UPDATE sbr_credential_bindings_v1
SET binding_state='REIMPORT_REQUIRED',revision=revision+1,updated_at=? WHERE binding_state='ACTIVE'`, updatedAt); err != nil {
		return ErrRepository
	}
	return nil
}

func sbrSchemaPresent(ctx context.Context, executor interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}) (bool, error) {
	rows, err := executor.QueryContext(ctx, `SELECT COUNT(*) FROM sqlite_schema WHERE type='table' AND name IN (
'sbr_credential_bindings_v1','sbr_authenticated_profiles_v1','sbr_readiness_transitions_v1',
'sbr_mutations_v1','sbr_idempotency_v1','sbr_simulator_transports_v1','sbr_commands_v1','sbr_product_states_v1',
'sbr_audit_events_v1','sbr_helper_dispatches_v1','sbr_pending_mutation_effects_v1')`)
	if err != nil {
		return false, ErrRepository
	}
	defer rows.Close()
	var count int64
	if !rows.Next() || rows.Scan(&count) != nil || rows.Next() || rows.Err() != nil || (count != 0 && count != 11) {
		return false, ErrRepository
	}
	return count == 11, nil
}

func validTimestamp(value string) bool {
	if len(value) != 30 {
		return false
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && parsed.UTC().Format("2006-01-02T15:04:05.000000000Z") == value
}
