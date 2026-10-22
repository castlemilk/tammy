package workspace

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
)

var (
	ErrAttemptJournalAuthentication = errors.New("workspace: attempt journal authentication failed")
	ErrAttemptPolicy                = errors.New("workspace: invalid attempt policy")
)

const attemptJournalAnchorSize = 1 + 8 + sha256.Size + sha256.Size

// AnchorStore is the durable, non-rollbackable installation-state boundary for
// attempt journals. Load's initialized result must be monotonic: once
// Initialize has created a label it must remain true even if the anchor value
// is missing.
//
// Workspace and recovery journals are consulted before a workspace is open, so
// their implementations must use OS-protected installation state (macOS
// Keychain or Windows Credential Manager), never a sidecar, workspace file, or
// table inside the rollbackable workspace. A memory implementation is suitable
// only for tests. Every operation requires the live label-bound lease selected
// by the store: Load may repair a crash-left initialization marker, Initialize
// must atomically reject an existing label, and Save must reject a missing label
// or any sequence other than the current sequence plus one.
type AnchorStore interface {
	Load(label string, lease attemptJournalLease) (value []byte, initialized bool, err error)
	Initialize(label string, value []byte, lease attemptJournalLease) error
	Save(label string, value []byte, lease attemptJournalLease) error
}

type processLocalAttemptJournalAnchorStore interface {
	allowProcessLocalAttemptJournalLock()
}

type memoryAnchorRecord struct {
	initialized bool
	value       []byte
}

// MemoryAnchorStore is deterministic installation state for tests and local
// harnesses. It is not a production fallback for OS-protected anchor state.
type MemoryAnchorStore struct {
	mu      sync.Mutex
	records map[string]memoryAnchorRecord
}

func NewMemoryAnchorStore() *MemoryAnchorStore {
	return &MemoryAnchorStore{records: make(map[string]memoryAnchorRecord)}
}

func (*MemoryAnchorStore) allowProcessLocalAttemptJournalLock() {}

func (store *MemoryAnchorStore) Load(label string, lease attemptJournalLease) ([]byte, bool, error) {
	if store == nil || label == "" || !validAttemptJournalLease(lease, label) {
		return nil, false, ErrAttemptJournalAuthentication
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	record, ok := store.records[label]
	if !ok {
		return nil, false, nil
	}
	return append([]byte(nil), record.value...), record.initialized, nil
}

func (store *MemoryAnchorStore) Initialize(label string, value []byte, lease attemptJournalLease) error {
	anchor, err := decodeAttemptJournalAnchor(value)
	if store == nil || label == "" || !validAttemptJournalLease(lease, label) || err != nil || anchor.Sequence != 0 {
		return ErrAttemptJournalAuthentication
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if record, ok := store.records[label]; ok && record.initialized {
		return ErrAttemptJournalAuthentication
	}
	store.records[label] = memoryAnchorRecord{initialized: true, value: append([]byte(nil), value...)}
	return nil
}

func (store *MemoryAnchorStore) Save(label string, value []byte, lease attemptJournalLease) error {
	next, err := decodeAttemptJournalAnchor(value)
	if store == nil || label == "" || !validAttemptJournalLease(lease, label) || err != nil || next.Sequence == 0 {
		return ErrAttemptJournalAuthentication
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	record, ok := store.records[label]
	if !ok || !record.initialized || len(record.value) == 0 {
		return ErrAttemptJournalAuthentication
	}
	current, err := decodeAttemptJournalAnchor(record.value)
	if err != nil || next.Sequence != current.Sequence+1 {
		return ErrAttemptJournalAuthentication
	}
	Zero(record.value)
	record.value = append([]byte(nil), value...)
	store.records[label] = record
	return nil
}

func (store *MemoryAnchorStore) Close() {
	if store == nil {
		return
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	for label, record := range store.records {
		Zero(record.value)
		delete(store.records, label)
	}
}

type attemptJournalAnchor struct {
	Sequence    uint64
	TerminalMAC [sha256.Size]byte
}

type AttemptPolicy struct {
	Limit    int
	Window   time.Duration
	Cooldown time.Duration
}

func (policy AttemptPolicy) valid() bool {
	return policy.Limit == 5 && policy.Window > 0 && policy.Cooldown == 15*time.Minute
}

type AttemptDecision struct {
	AttemptCount  int
	CooldownUntil time.Time
}

func (decision AttemptDecision) CoolingDown(at time.Time) bool {
	return !decision.CooldownUntil.IsZero() && at.Before(decision.CooldownUntil)
}

type attemptEntry struct {
	Sequence    uint64 `json:"sequence"`
	Scope       string `json:"scope"`
	SubjectHash string `json:"subject_hash"`
	OccurredAt  string `json:"occurred_at"`
	Failure     bool   `json:"failure"`
	PreviousMAC string `json:"previous_mac"`
	MAC         string `json:"mac"`
}

type AttemptJournal struct {
	mu           sync.Mutex
	path         string
	key          []byte
	clock        clock.Clock
	anchorID     string
	anchorLabel  string
	anchors      AnchorStore
	lockProvider attemptJournalLockProvider
	activeLease  attemptJournalLease
	anchor       attemptJournalAnchor
	entries      []attemptEntry
	fileIdentity os.FileInfo
	fileSize     int64
	poisoned     bool
}

func NewAttemptJournal(path string, key []byte, source clock.Clock, anchorID string, anchors AnchorStore) (*AttemptJournal, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || len(key) != 32 || source == nil ||
		anchorID == "" || len(anchorID) > 1024 || strings.IndexByte(anchorID, 0) >= 0 || anchors == nil {
		return nil, ErrAttemptJournalAuthentication
	}
	lockProvider, ok := anchors.(attemptJournalLockProvider)
	if !ok {
		if _, allowed := anchors.(processLocalAttemptJournalAnchorStore); !allowed {
			return nil, ErrAttemptJournalAuthentication
		}
		lockProvider = processAttemptJournalLockProvider{}
	}
	journal := &AttemptJournal{path: path, key: append([]byte(nil), key...), clock: source, anchorID: anchorID,
		anchors: anchors, lockProvider: lockProvider}
	journal.anchorLabel = journal.deriveAnchorLabel()
	if err := journal.synchronizeLocked(nil); err != nil {
		Zero(journal.key)
		return nil, err
	}
	return journal, nil
}

func (journal *AttemptJournal) Close() {
	if journal == nil {
		return
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	Zero(journal.key)
	journal.key = nil
	journal.entries = nil
	journal.anchors = nil
	journal.lockProvider = nil
	journal.activeLease = nil
	journal.fileIdentity = nil
	journal.poisoned = true
}

func (journal *AttemptJournal) Failure(scope, subject string, policy AttemptPolicy) (AttemptDecision, error) {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if err := journal.validInput(scope, subject, policy); err != nil {
		return AttemptDecision{}, err
	}
	var decision AttemptDecision
	err := journal.synchronizeLocked(func() error {
		now := journal.clock.Now().UTC()
		decision = journal.statusLocked(scope, subject, policy, now)
		if decision.CoolingDown(now) {
			return nil
		}
		if err := journal.appendLocked(scope, subject, true, now); err != nil {
			return err
		}
		decision = journal.statusLocked(scope, subject, policy, now)
		return nil
	})
	if err != nil {
		return AttemptDecision{}, err
	}
	return decision, nil
}

func (journal *AttemptJournal) Success(scope, subject string) error {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if journal.poisoned {
		return ErrAttemptJournalAuthentication
	}
	if scope == "" || subject == "" || len(journal.key) != 32 {
		return ErrAttemptPolicy
	}
	return journal.synchronizeLocked(func() error {
		return journal.appendLocked(scope, subject, false, journal.clock.Now().UTC())
	})
}

func (journal *AttemptJournal) Status(scope, subject string, policy AttemptPolicy) (AttemptDecision, error) {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if err := journal.validInput(scope, subject, policy); err != nil {
		return AttemptDecision{}, err
	}
	var decision AttemptDecision
	err := journal.synchronizeLocked(func() error {
		decision = journal.statusLocked(scope, subject, policy, journal.clock.Now().UTC())
		return nil
	})
	return decision, err
}

func (journal *AttemptJournal) validInput(scope, subject string, policy AttemptPolicy) error {
	if journal == nil || len(journal.key) != 32 || scope == "" || subject == "" || !policy.valid() {
		return ErrAttemptPolicy
	}
	if journal.poisoned {
		return ErrAttemptJournalAuthentication
	}
	return nil
}

func (journal *AttemptJournal) statusLocked(scope, subject string, policy AttemptPolicy, now time.Time) AttemptDecision {
	subjectHash := journal.subjectHash(subject)
	cutoff := now.Add(-policy.Window)
	count := 0
	var newestFailure time.Time
	for index := len(journal.entries) - 1; index >= 0; index-- {
		entry := journal.entries[index]
		if entry.Scope != scope || entry.SubjectHash != subjectHash {
			continue
		}
		instant, err := time.Parse(time.RFC3339Nano, entry.OccurredAt)
		if err != nil || !instant.After(cutoff) {
			break
		}
		if !entry.Failure {
			break
		}
		if newestFailure.IsZero() {
			newestFailure = instant
		}
		count++
	}
	var cooldown time.Time
	if count >= policy.Limit {
		cooldown = newestFailure.Add(policy.Cooldown)
	}
	if !cooldown.After(now) {
		if count >= policy.Limit {
			return AttemptDecision{}
		}
		cooldown = time.Time{}
	}
	return AttemptDecision{AttemptCount: count, CooldownUntil: cooldown}
}

func (journal *AttemptJournal) appendLocked(scope, subject string, failure bool, now time.Time) error {
	entry := attemptEntry{
		Sequence:    uint64(len(journal.entries) + 1),
		Scope:       scope,
		SubjectHash: journal.subjectHash(subject),
		OccurredAt:  now.Format(time.RFC3339Nano),
		Failure:     failure,
	}
	if len(journal.entries) != 0 {
		entry.PreviousMAC = journal.entries[len(journal.entries)-1].MAC
	}
	entry.MAC = journal.entryMAC(entry)
	payload, err := json.Marshal(entry)
	if err != nil {
		return ErrAttemptJournalAuthentication
	}
	payload = append(payload, '\n')
	identity, size, err := appendSecureRegularFile(journal.path, journal.fileIdentity, payload, maxAttemptJournalFileSize)
	if err != nil {
		journal.poisoned = true
		return errors.Join(ErrAttemptJournalAuthentication, err)
	}
	journal.fileIdentity = identity
	journal.fileSize = size
	journal.entries = append(journal.entries, entry)
	nextAnchor, err := journal.anchorForEntry(entry)
	if err != nil || journal.anchors.Save(journal.anchorLabel, journal.encodeAnchor(nextAnchor), journal.activeLease) != nil {
		journal.poisoned = true
		return ErrAttemptJournalAuthentication
	}
	journal.anchor = nextAnchor
	return nil
}

func (journal *AttemptJournal) synchronizeLocked(work func() error) error {
	if journal.lockProvider == nil {
		journal.poisoned = true
		return ErrAttemptJournalAuthentication
	}
	lock, err := journal.lockProvider.AcquireAttemptJournalLock(journal.anchorLabel, attemptJournalLockTimeout)
	if err != nil {
		journal.poisoned = true
		return ErrAttemptJournalAuthentication
	}
	journal.activeLease = lock
	err = journal.reloadLocked()
	if err == nil && work != nil {
		err = work()
	}
	journal.activeLease = nil
	releaseErr := lock.Release()
	if err != nil || releaseErr != nil {
		journal.poisoned = true
		return errors.Join(ErrAttemptJournalAuthentication, err, releaseErr)
	}
	return nil
}

func (journal *AttemptJournal) reloadLocked() error {
	journal.anchor = attemptJournalAnchor{}
	journal.entries = nil
	journal.fileIdentity = nil
	journal.fileSize = 0
	anchorPayload, initialized, err := journal.anchors.Load(journal.anchorLabel, journal.activeLease)
	if err != nil {
		return ErrAttemptJournalAuthentication
	}
	payload, identity, readErr := readSecureRegularFile(journal.path, maxAttemptJournalFileSize)
	journalMissing := errors.Is(readErr, os.ErrNotExist)
	if readErr != nil && !journalMissing {
		return errors.Join(ErrAttemptJournalAuthentication, &os.PathError{Op: "read", Path: journal.path, Err: readErr})
	}
	if !initialized {
		if !journalMissing {
			return ErrAttemptJournalAuthentication
		}
		genesis := attemptJournalAnchor{}
		if err := journal.anchors.Initialize(journal.anchorLabel, journal.encodeAnchor(genesis), journal.activeLease); err != nil {
			return ErrAttemptJournalAuthentication
		}
		journal.anchor = genesis
		return nil
	}
	if len(anchorPayload) == 0 {
		return ErrAttemptJournalAuthentication
	}
	anchor, err := decodeAttemptJournalAnchor(anchorPayload)
	if err != nil || !hmac.Equal(anchorPayload[1+8+sha256.Size:], journal.anchorAuthentication(anchor)) {
		return ErrAttemptJournalAuthentication
	}
	journal.anchor = anchor
	if journalMissing {
		if anchor.Sequence != 0 {
			return ErrAttemptJournalAuthentication
		}
		return nil
	}
	journal.fileIdentity = identity
	journal.fileSize = int64(len(payload))

	previousMAC := ""
	validBytes := 0
	for len(payload) != 0 {
		newline := bytes.IndexByte(payload, '\n')
		if newline < 0 {
			break
		}
		line := payload[:newline]
		if len(line) == 0 || len(line) > 64*1024 {
			break
		}
		var entry attemptEntry
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&entry); err != nil || entry.Sequence != uint64(len(journal.entries)+1) ||
			entry.PreviousMAC != previousMAC || !journal.validEntryMAC(entry) {
			break
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			break
		}
		if _, err := time.Parse(time.RFC3339Nano, entry.OccurredAt); err != nil || entry.Scope == "" || len(entry.SubjectHash) != sha256.Size*2 {
			break
		}
		if subjectHash, err := hex.DecodeString(entry.SubjectHash); err != nil || len(subjectHash) != sha256.Size {
			break
		}
		journal.entries = append(journal.entries, entry)
		previousMAC = entry.MAC
		consumed := newline + 1
		validBytes += consumed
		payload = payload[consumed:]
	}
	if uint64(len(journal.entries)) < anchor.Sequence {
		return ErrAttemptJournalAuthentication
	}
	if anchor.Sequence != 0 {
		anchoredMAC, err := hex.DecodeString(journal.entries[anchor.Sequence-1].MAC)
		if err != nil || !hmac.Equal(anchoredMAC, anchor.TerminalMAC[:]) {
			return ErrAttemptJournalAuthentication
		}
	}
	if uint64(len(journal.entries)) > anchor.Sequence+1 {
		return ErrAttemptJournalAuthentication
	}
	if uint64(len(journal.entries)) == anchor.Sequence+1 {
		recovered, err := journal.anchorForEntry(journal.entries[len(journal.entries)-1])
		if err != nil || journal.anchors.Save(journal.anchorLabel, journal.encodeAnchor(recovered), journal.activeLease) != nil {
			return ErrAttemptJournalAuthentication
		}
		journal.anchor = recovered
	}
	if validBytes != int(journal.fileSize) {
		if err := truncateSecureRegularFile(journal.path, journal.fileIdentity, int64(validBytes), maxAttemptJournalFileSize); err != nil {
			return ErrAttemptJournalAuthentication
		}
		journal.fileSize = int64(validBytes)
	}
	return nil
}

func (journal *AttemptJournal) deriveAnchorLabel() string {
	digest := hmac.New(sha256.New, journal.key)
	_, _ = digest.Write([]byte("tammy.attempt-anchor-label.v1\x00"))
	_, _ = digest.Write([]byte(journal.anchorID))
	return "tammy.attempt-journal-anchor.v1/" + hex.EncodeToString(digest.Sum(nil))
}

func (journal *AttemptJournal) anchorForEntry(entry attemptEntry) (attemptJournalAnchor, error) {
	terminal, err := hex.DecodeString(entry.MAC)
	if err != nil || len(terminal) != sha256.Size {
		return attemptJournalAnchor{}, ErrAttemptJournalAuthentication
	}
	anchor := attemptJournalAnchor{Sequence: entry.Sequence}
	copy(anchor.TerminalMAC[:], terminal)
	return anchor, nil
}

func (journal *AttemptJournal) encodeAnchor(anchor attemptJournalAnchor) []byte {
	payload := make([]byte, attemptJournalAnchorSize)
	payload[0] = 1
	binary.BigEndian.PutUint64(payload[1:9], anchor.Sequence)
	copy(payload[9:9+sha256.Size], anchor.TerminalMAC[:])
	copy(payload[9+sha256.Size:], journal.anchorAuthentication(anchor))
	return payload
}

func decodeAttemptJournalAnchor(payload []byte) (attemptJournalAnchor, error) {
	if len(payload) != attemptJournalAnchorSize || payload[0] != 1 {
		return attemptJournalAnchor{}, ErrAttemptJournalAuthentication
	}
	anchor := attemptJournalAnchor{Sequence: binary.BigEndian.Uint64(payload[1:9])}
	copy(anchor.TerminalMAC[:], payload[9:9+sha256.Size])
	if anchor.Sequence == 0 && !hmac.Equal(anchor.TerminalMAC[:], make([]byte, sha256.Size)) {
		return attemptJournalAnchor{}, ErrAttemptJournalAuthentication
	}
	if anchor.Sequence != 0 && hmac.Equal(anchor.TerminalMAC[:], make([]byte, sha256.Size)) {
		return attemptJournalAnchor{}, ErrAttemptJournalAuthentication
	}
	return anchor, nil
}

func (journal *AttemptJournal) anchorAuthentication(anchor attemptJournalAnchor) []byte {
	payload := make([]byte, 8+sha256.Size)
	binary.BigEndian.PutUint64(payload[:8], anchor.Sequence)
	copy(payload[8:], anchor.TerminalMAC[:])
	digest := hmac.New(sha256.New, journal.key)
	_, _ = digest.Write([]byte("tammy.attempt-anchor.v1\x00"))
	_, _ = digest.Write([]byte(journal.anchorID))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write(payload)
	return digest.Sum(nil)
}

func (journal *AttemptJournal) validEntryMAC(entry attemptEntry) bool {
	actual, err := hex.DecodeString(entry.MAC)
	if err != nil || len(actual) != sha256.Size {
		return false
	}
	expected, err := hex.DecodeString(journal.entryMAC(entry))
	return err == nil && hmac.Equal(actual, expected)
}

func (journal *AttemptJournal) subjectHash(subject string) string {
	digest := hmac.New(sha256.New, journal.key)
	_, _ = digest.Write([]byte("tammy.attempt-subject.v1\x00"))
	_, _ = digest.Write([]byte(subject))
	return hex.EncodeToString(digest.Sum(nil))
}

func (journal *AttemptJournal) entryMAC(entry attemptEntry) string {
	entry.MAC = ""
	payload, _ := json.Marshal(entry)
	digest := hmac.New(sha256.New, journal.key)
	_, _ = digest.Write([]byte("tammy.attempt-entry.v1\x00"))
	_, _ = digest.Write(payload)
	return hex.EncodeToString(digest.Sum(nil))
}
