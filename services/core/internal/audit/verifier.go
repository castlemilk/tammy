package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

const (
	compatibilityOpeningEventLimit = 1024
	openingDigestDomain            = "tammy-audit-opening-registry-v1\x00"
)

type commitmentOpeningAccumulator interface {
	Add(sequence uint64, headBefore [sha256.Size]byte, openings *tammyv1.AuditCommitmentOpenings) error
	FirstDuplicate() (sequence uint64, headBefore [sha256.Size]byte, err error)
	Close() error
}

// StoredChainVerifier incrementally verifies an exact chain snapshot. Opening
// uniqueness is delegated so production consumers can use a bounded external
// accumulator without changing verification semantics.
type StoredChainVerifier struct {
	header      ChainHeader
	previous    [sha256.Size]byte
	verified    uint64
	mismatch    uint64
	invalid     bool
	finished    bool
	closed      bool
	openings    commitmentOpeningAccumulator
	result      *tammyv1.VerifyChainResponse
	terminalErr error
}

// NewStoredChainVerifier currently uses a strictly capped digest accumulator.
// No production paging consumer is wired to this constructor until the
// external-sort accumulator has completed its own adversarial test tranche.
func NewStoredChainVerifier(header ChainHeader) (*StoredChainVerifier, error) {
	return newStoredChainVerifier(header, newBoundedMemoryOpeningAccumulator(compatibilityOpeningEventLimit*5))
}

// NewStreamingStoredChainVerifier uses a disk-backed accumulator bounded by
// the fixed snapshot size. The multiplication is checked before temp creation.
func NewStreamingStoredChainVerifier(ctx context.Context, header ChainHeader) (*StoredChainVerifier, error) {
	if ctx == nil || header.CurrentSequence > ExternalOpeningRecordLimit/5 {
		return nil, ErrInvalidChainInput
	}
	openings, err := newExternalOpeningAccumulator(externalOpeningAccumulatorConfig{
		Context: ctx, RecordLimit: header.CurrentSequence * 5,
		ChunkRecords: externalOpeningChunkRecords, MergeFanIn: externalOpeningMergeFanIn,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: create opening accumulator", ErrRepository)
	}
	return newStoredChainVerifier(header, openings)
}

func newStoredChainVerifier(header ChainHeader, openings commitmentOpeningAccumulator) (*StoredChainVerifier, error) {
	genesis, err := Genesis(header.WorkspaceID, header.ChainSalt)
	if err != nil || openings == nil || header.Generation == 0 || genesis != header.GenesisHash ||
		header.CurrentHead == ([sha256.Size]byte{}) || header.CurrentSequence == 0 && header.CurrentHead != genesis {
		if openings != nil {
			_ = openings.Close()
		}
		return nil, ErrInvalidChainInput
	}
	return &StoredChainVerifier{header: header, previous: genesis, openings: openings}, nil
}

func (verifier *StoredChainVerifier) AcceptPage(events []StoredEvent) error {
	if verifier == nil || verifier.openings == nil || verifier.finished || verifier.closed || verifier.invalid {
		return ErrInvalidEvent
	}
	for index := range events {
		stored := events[index]
		expectedSequence := verifier.verified + 1
		if expectedSequence > verifier.header.CurrentSequence || stored.Event == nil ||
			stored.Event.WorkspaceId != verifier.header.WorkspaceID || stored.Event.Generation != verifier.header.Generation ||
			stored.Event.Sequence != expectedSequence || !bytes.Equal(stored.Event.PreviousHash, verifier.previous[:]) ||
			!validCommitmentOpenings(stored.Event.CommitmentOpenings) {
			verifier.markInvalid(expectedSequence)
			return ErrInvalidEvent
		}
		if err := verifier.openings.Add(expectedSequence, verifier.previous, stored.Event.CommitmentOpenings); err != nil {
			verifier.markInvalid(expectedSequence)
			verifier.terminalErr = err
			return fmt.Errorf("%w: record commitment opening", ErrRepository)
		}
		prepared, err := reconstructEventWithStoredOpenings(verifier.previous, stored.Event, stored.PayloadProto)
		if err != nil || prepared.PayloadType != stored.PayloadType ||
			!bytes.Equal(prepared.PayloadProto, stored.PayloadProto) ||
			!bytes.Equal(prepared.PayloadJSON, stored.PayloadJSON) ||
			!bytes.Equal(prepared.CanonicalEvent, stored.CanonicalEvent) ||
			!bytes.Equal(prepared.EventProto, stored.EventProto) ||
			!bytes.Equal(prepared.Event.EventHash, stored.Event.EventHash) {
			verifier.markInvalid(expectedSequence)
			return ErrInvalidEvent
		}
		copy(verifier.previous[:], stored.Event.EventHash)
		verifier.verified = expectedSequence
	}
	return nil
}

func (verifier *StoredChainVerifier) Checkpoint() StoredEventCheckpoint {
	if verifier == nil {
		return StoredEventCheckpoint{}
	}
	return StoredEventCheckpoint{AfterSequence: verifier.verified, Head: verifier.previous}
}

// TerminalError exposes accumulator I/O/capacity failures separately from a
// cryptographic INVALID result so streaming callers can fail closed.
func (verifier *StoredChainVerifier) TerminalError() error {
	if verifier == nil {
		return ErrRepository
	}
	return verifier.terminalErr
}

func (verifier *StoredChainVerifier) Finish() *tammyv1.VerifyChainResponse {
	if verifier == nil {
		return invalidVerification(0, 1, [sha256.Size]byte{})
	}
	if verifier.finished {
		return verifier.result
	}
	verifier.finished = true
	defer verifier.cleanup()
	if verifier.invalid || verifier.terminalErr != nil {
		verifier.result = invalidVerification(verifier.verified, verifier.mismatch, verifier.previous)
		return verifier.result
	}
	if verifier.verified != verifier.header.CurrentSequence || verifier.previous != verifier.header.CurrentHead {
		mismatch := verifier.verified + 1
		if verifier.header.CurrentSequence < mismatch {
			mismatch = verifier.header.CurrentSequence + 1
		}
		verifier.result = invalidVerification(verifier.verified, mismatch, verifier.previous)
		return verifier.result
	}
	duplicateSequence, headBeforeDuplicate, err := verifier.openings.FirstDuplicate()
	if err != nil {
		verifier.terminalErr = err
		verifier.result = invalidVerification(verifier.verified, verifier.verified+1, verifier.previous)
		return verifier.result
	}
	if duplicateSequence != 0 {
		verifier.result = invalidVerification(duplicateSequence-1, duplicateSequence, headBeforeDuplicate)
		return verifier.result
	}
	verifier.result = &tammyv1.VerifyChainResponse{
		Integrity:               tammyv1.AuditChainIntegrity_AUDIT_CHAIN_INTEGRITY_VALID,
		VerifiedThroughSequence: verifier.verified,
		VerifiedHead:            append([]byte(nil), verifier.previous[:]...),
	}
	return verifier.result
}

func (verifier *StoredChainVerifier) Close() error {
	if verifier == nil {
		return nil
	}
	verifier.closed = true
	return verifier.cleanup()
}

func (verifier *StoredChainVerifier) cleanup() error {
	if verifier.openings == nil {
		return nil
	}
	err := verifier.openings.Close()
	verifier.openings = nil
	return err
}

func (verifier *StoredChainVerifier) markInvalid(sequence uint64) {
	verifier.invalid = true
	verifier.mismatch = sequence
}

func VerifyStoredChain(header ChainHeader, events []StoredEvent) *tammyv1.VerifyChainResponse {
	verifier, err := newStoredChainVerifier(header,
		newBoundedMemoryOpeningAccumulator(compatibilityOpeningEventLimit*5))
	if err != nil {
		return invalidVerification(0, 1, [sha256.Size]byte{})
	}
	defer verifier.Close()
	_ = verifier.AcceptPage(events)
	return verifier.Finish()
}

func invalidVerification(verifiedThrough, mismatch uint64, head [sha256.Size]byte) *tammyv1.VerifyChainResponse {
	return &tammyv1.VerifyChainResponse{
		Integrity:               tammyv1.AuditChainIntegrity_AUDIT_CHAIN_INTEGRITY_INVALID,
		VerifiedThroughSequence: verifiedThrough,
		MismatchSequence:        &mismatch,
		VerifiedHead:            append([]byte(nil), head[:]...),
	}
}

type boundedMemoryOpeningAccumulator struct {
	limit     int
	seen      map[[sha256.Size]byte]uint64
	duplicate uint64
	priorHead [sha256.Size]byte
	closed    bool
}

func newBoundedMemoryOpeningAccumulator(limit int) *boundedMemoryOpeningAccumulator {
	return &boundedMemoryOpeningAccumulator{limit: limit, seen: make(map[[sha256.Size]byte]uint64)}
}

func (accumulator *boundedMemoryOpeningAccumulator) Add(
	sequence uint64,
	headBefore [sha256.Size]byte,
	openings *tammyv1.AuditCommitmentOpenings,
) error {
	if accumulator == nil || accumulator.closed || sequence == 0 || !validCommitmentOpenings(openings) {
		return ErrInvalidEvent
	}
	values := commitmentOpeningBytes(openings)
	if len(accumulator.seen) > accumulator.limit-len(values) {
		return ErrRepository
	}
	for _, opening := range values {
		digest := sha256.New()
		_, _ = digest.Write([]byte(openingDigestDomain))
		_, _ = digest.Write(opening)
		var key [sha256.Size]byte
		copy(key[:], digest.Sum(nil))
		if _, exists := accumulator.seen[key]; exists {
			if accumulator.duplicate == 0 || sequence < accumulator.duplicate {
				accumulator.duplicate = sequence
				accumulator.priorHead = headBefore
			}
			continue
		}
		accumulator.seen[key] = sequence
	}
	return nil
}

func (accumulator *boundedMemoryOpeningAccumulator) FirstDuplicate() (uint64, [sha256.Size]byte, error) {
	if accumulator == nil || accumulator.closed {
		return 0, [sha256.Size]byte{}, ErrRepository
	}
	return accumulator.duplicate, accumulator.priorHead, nil
}

func (accumulator *boundedMemoryOpeningAccumulator) Close() error {
	if accumulator == nil || accumulator.closed {
		return nil
	}
	accumulator.closed = true
	for digest := range accumulator.seen {
		delete(accumulator.seen, digest)
	}
	return nil
}
