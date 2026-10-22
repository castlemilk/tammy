//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package accounting_test

import (
	"bytes"
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/proto"
)

func TestTaxFactReportingCaptureRetainsCompleteUniqueJournalProofs(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	// Insert in descending order to prove the capture emits the sorted
	// reservation order, independently of fixture insertion order.
	for _, seed := range []int{14211, 14201} {
		journalID := taxFactReportingID(seed)
		seedTaxFactReportingDraft(t, fixture.tx, fixture.accounts, fixture.now,
			journalID, "SYSTEM", 0, "raw-draft-source-"+journalID,
			taxFactReportingID(seed+1), taxFactReportingID(seed+2),
			"nullable-fact-hash-"+journalID, "ADJUSTMENT", nil)
	}

	probe := &taxFactCaptureJournalProofProbe{Transaction: fixture.tx}
	repository, err := accounting.NewJournalRepository(probe)
	if err != nil {
		t.Fatalf("NewJournalRepository(probe) = %v", err)
	}
	capture, err := repository.CaptureTaxFactsForReporting(fixture.ctx, accountTestOrganisationID)
	if err != nil || capture == nil {
		t.Fatalf("CaptureTaxFactsForReporting() = %#v, %v", capture, err)
	}
	wantIDs := []string{taxFactReportingID(14201), taxFactReportingID(14211)}
	if capture.JournalCount != len(wantIDs) || len(capture.JournalIDs) != len(wantIDs) ||
		len(capture.Journals) != len(wantIDs) {
		t.Fatalf("capture parent proofs: count=%d ids=%v journals=%d; want one complete proof for each selected fact parent %v",
			capture.JournalCount, capture.JournalIDs, len(capture.Journals), wantIDs)
	}
	for index, journalID := range wantIDs {
		proof := capture.Journals[index]
		if capture.JournalIDs[index] != journalID || proof == nil || proof.Journal == nil ||
			proof.Journal.Id != journalID || len(proof.ContentHash) != 32 || proof.PayloadBytes <= 0 ||
			capture.JournalPayloadBytes[journalID] != proof.PayloadBytes {
			t.Fatalf("capture proof[%d] = %#v; want complete parent %q in sorted JournalIDs order", index, proof, journalID)
		}
		if proof.SourceType != "SYSTEM" || proof.SourceKind != 0 || proof.SourceID != "raw-draft-source-"+journalID ||
			proof.SourceRevision != 1 || proof.State != "DRAFT" || proof.CreatedAt != fixture.now.Format(time.RFC3339Nano) {
			t.Fatalf("capture raw DRAFT proof[%d] metadata = %#v", index, proof)
		}
		want, readErr := fixture.repository.GetForReporting(fixture.ctx, accountTestOrganisationID, journalID)
		if readErr != nil || want == nil {
			t.Fatalf("independent GetForReporting(%q) = %#v, %v", journalID, want, readErr)
		}
		if !proto.Equal(proof.Journal, want.Journal) || proof.SourceType != want.SourceType ||
			proof.SourceKind != want.SourceKind || proof.SourceID != want.SourceID ||
			proof.SourceRevision != want.SourceRevision || proof.CreatedAt != want.CreatedAt ||
			proof.State != want.State || proof.PayloadBytes != want.PayloadBytes ||
			!bytes.Equal(proof.ContentHash, want.ContentHash) {
			t.Fatalf("capture proof[%d] differs from independent full reporting proof: got=%#v want=%#v",
				index, proof, want)
		}
	}
	for _, fact := range capture.Facts {
		if fact.SourceContentHashPresent || len(fact.SourceContentHash) != 0 {
			t.Fatalf("fixture should retain SQL NULL source_content_hash, got present=%t hash=%x",
				fact.SourceContentHashPresent, fact.SourceContentHash)
		}
	}
	if probe.headerBodyQueries != len(wantIDs) || probe.lineBodyQueries != len(wantIDs) {
		t.Fatalf("capture hydrated %d header bodies and %d line bodies; want exactly one existing cached read per unique parent (%d each)",
			probe.headerBodyQueries, probe.lineBodyQueries, len(wantIDs))
	}
	if !fixture.tx.Authenticated() {
		t.Fatal("capture or proof comparison closed the caller-owned SQLCipher transaction")
	}
}

type taxFactCaptureJournalProofProbe struct {
	*sqlcipher.Transaction
	headerBodyQueries int
	lineBodyQueries   int
}

func (probe *taxFactCaptureJournalProofProbe) QueryContext(
	ctx context.Context, query string, arguments ...any,
) (*sql.Rows, error) {
	if strings.Contains(query, journalBodyMaskHeaderMarker) {
		probe.headerBodyQueries++
	}
	if strings.Contains(query, journalBodyMaskLinesMarker) {
		probe.lineBodyQueries++
	}
	return probe.Transaction.QueryContext(ctx, query, arguments...)
}
