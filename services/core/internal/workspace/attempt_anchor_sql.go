package workspace

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const sqlAnchorScope = "tammy.attempt-journal-anchor.v1"

type anchorSQLDatabase interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// SQLAnchorStore persists post-unlock identity attempt anchors inside the
// authenticated SQLCipher workspace. It must not be used for workspace unlock
// or recovery journals, which need OS-protected installation state before this
// database can be opened.
type SQLAnchorStore struct {
	database anchorSQLDatabase
}

func NewSQLAnchorStore(database anchorSQLDatabase) (*SQLAnchorStore, error) {
	if database == nil {
		return nil, ErrAttemptJournalAuthentication
	}
	return &SQLAnchorStore{database: database}, nil
}

func (store *SQLAnchorStore) AcquireAttemptJournalLock(label string, timeout time.Duration) (attemptJournalLease, error) {
	if store == nil || store.database == nil {
		return nil, ErrAttemptJournalAuthentication
	}
	path, err := platformAttemptJournalLockPath(label)
	if err != nil {
		return nil, err
	}
	lock, err := acquireAttemptJournalFileLock(path, timeout)
	if err != nil {
		return nil, err
	}
	lock.label = label
	return lock, nil
}

func (store *SQLAnchorStore) Load(label string, lease attemptJournalLease) ([]byte, bool, error) {
	if store == nil || store.database == nil || label == "" || !validPlatformAttemptJournalLease(lease, label) {
		return nil, false, ErrAttemptJournalAuthentication
	}
	var sequence int64
	var payload []byte
	err := store.database.QueryRowContext(context.Background(), `
		SELECT sequence, chain_hmac FROM attempt_journal_anchors
		WHERE scope = ? AND subject_hash = ?`, sqlAnchorScope, label).Scan(&sequence, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil || sequence < 0 {
		return nil, false, ErrAttemptJournalAuthentication
	}
	anchor, err := decodeAttemptJournalAnchor(payload)
	if err != nil || anchor.Sequence != uint64(sequence) {
		return nil, true, ErrAttemptJournalAuthentication
	}
	return append([]byte(nil), payload...), true, nil
}

func (store *SQLAnchorStore) Initialize(label string, payload []byte, lease attemptJournalLease) error {
	if store == nil || store.database == nil || label == "" || !validPlatformAttemptJournalLease(lease, label) {
		return ErrAttemptJournalAuthentication
	}
	anchor, err := decodeAttemptJournalAnchor(payload)
	if err != nil || anchor.Sequence != 0 {
		return ErrAttemptJournalAuthentication
	}
	_, err = store.database.ExecContext(context.Background(), `
		INSERT INTO attempt_journal_anchors(
			scope, subject_hash, sequence, chain_hmac, attempt_count, cooldown_until, updated_at
		) VALUES (?, ?, 0, ?, 0, NULL, ?)`, sqlAnchorScope, label, append([]byte(nil), payload...), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return ErrAttemptJournalAuthentication
	}
	return nil
}

func (store *SQLAnchorStore) Save(label string, payload []byte, lease attemptJournalLease) error {
	if store == nil || store.database == nil || label == "" || !validPlatformAttemptJournalLease(lease, label) {
		return ErrAttemptJournalAuthentication
	}
	anchor, err := decodeAttemptJournalAnchor(payload)
	if err != nil || anchor.Sequence == 0 {
		return ErrAttemptJournalAuthentication
	}
	result, err := store.database.ExecContext(context.Background(), `
		UPDATE attempt_journal_anchors
		SET sequence = ?, chain_hmac = ?, updated_at = ?
		WHERE scope = ? AND subject_hash = ? AND sequence = ?`,
		anchor.Sequence, append([]byte(nil), payload...), time.Now().UTC().Format(time.RFC3339Nano),
		sqlAnchorScope, label, anchor.Sequence-1)
	if err != nil {
		return ErrAttemptJournalAuthentication
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return ErrAttemptJournalAuthentication
	}
	return nil
}
