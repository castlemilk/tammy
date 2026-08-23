//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package organisations_test

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/organisations"
	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
	"github.com/tammyapp/tammy/services/core/internal/revisions"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
)

const (
	testOrganisationID = "018f0000-0000-7000-8000-000000000020"
	testVerificationID = "018f0000-0000-7000-8000-000000000030"
	testEvidenceID     = "018f0000-0000-7000-8000-000000000040"
	testOperationID    = "018f0000-0000-7000-8000-000000000080"
)

func TestEvidenceRepositoryRoundTripsBoundedProtobufEvidence(t *testing.T) {
	for _, mimeType := range []string{"application/pdf", "image/jpeg", "image/png"} {
		t.Run(mimeType, func(t *testing.T) {
			workspace := testkit.NewEncryptedWorkspace(t)
			seedIdentityAndOrganisation(t, workspace)
			tx, err := workspace.Database.BeginEncryptedTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			repository, err := organisations.NewEvidenceRepository(tx)
			if err != nil {
				t.Fatal(err)
			}
			content := []byte("tammy-verification-evidence-" + mimeType)
			record := validRecord(mimeType, content)
			seedEvidenceOperation(t, tx, record.OperationKey)
			if err := repository.Save(context.Background(), record); err != nil {
				t.Fatal(err)
			}
			loaded, err := repository.Get(context.Background(), testVerificationID)
			if err != nil {
				t.Fatal(err)
			}
			if !proto.Equal(loaded.Verification, record.Verification) || !proto.Equal(loaded.Evidence, record.Evidence) {
				t.Fatalf("round trip mismatch\nloaded=%#v\nwant=%#v", loaded, record)
			}
			summary, err := repository.GetCurrentSummary(context.Background(), testOrganisationID)
			if err != nil {
				t.Fatal(err)
			}
			if summary == nil || summary.State != record.Verification.State ||
				!summary.ExpiresAt.AsTime().Equal(record.Verification.ExpiresAt.AsTime()) {
				t.Fatalf("current verification summary = %#v", summary)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestEvidenceRepositoryRejectsInvalidMimeSizeHashAndExpiry(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspace(t)
	seedIdentityAndOrganisation(t, workspace)
	for _, mutate := range []func(*organisations.VerificationRecord){
		func(record *organisations.VerificationRecord) { record.Evidence.MimeType = "text/plain" },
		func(record *organisations.VerificationRecord) { record.Evidence.Content = nil },
		func(record *organisations.VerificationRecord) {
			record.Evidence.Content = make([]byte, organisations.MaxVerificationEvidenceBytes+1)
		},
		func(record *organisations.VerificationRecord) { record.Evidence.ContentHash[0] ^= 0xff },
		func(record *organisations.VerificationRecord) {
			record.Evidence = testkit.VerificationEvidence("application/pdf", evidenceBytes("image/png", nil))
		},
		func(record *organisations.VerificationRecord) {
			record.Evidence = testkit.VerificationEvidence("application/pdf", []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n%%EOF\n"))
		},
		func(record *organisations.VerificationRecord) {
			content := evidenceBytes("image/jpeg", nil)
			record.Evidence = testkit.VerificationEvidence("image/jpeg", content[:len(content)-2])
		},
		func(record *organisations.VerificationRecord) {
			content := evidenceBytes("image/png", nil)
			record.Evidence = testkit.VerificationEvidence("image/png", content[:len(content)-8])
		},
		func(record *organisations.VerificationRecord) {
			content := append(evidenceBytes("application/pdf", nil), []byte("<script>polyglot</script>")...)
			record.Evidence = testkit.VerificationEvidence("application/pdf", content)
		},
		func(record *organisations.VerificationRecord) {
			record.Evidence = testkit.VerificationEvidence("application/pdf", corruptPDFRootXRef(evidenceBytes("application/pdf", nil)))
		},
		func(record *organisations.VerificationRecord) {
			content := append(evidenceBytes("image/png", nil), []byte("PK\x03\x04polyglot")...)
			record.Evidence = testkit.VerificationEvidence("image/png", content)
		},
		func(record *organisations.VerificationRecord) {
			content := append(evidenceBytes("image/jpeg", nil), []byte("polyglot")...)
			content = append(content, 0xff, 0xd9)
			record.Evidence = testkit.VerificationEvidence("image/jpeg", content)
		},
		func(record *organisations.VerificationRecord) {
			content := append(evidenceBytes("image/png", nil), []byte("polyglot")...)
			content = append(content, []byte{0, 0, 0, 0, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82}...)
			record.Evidence = testkit.VerificationEvidence("image/png", content)
		},
		func(record *organisations.VerificationRecord) {
			record.Evidence = testkit.VerificationEvidence("image/png", oversizedCompressedPNG(10_000, 5_000))
		},
		func(record *organisations.VerificationRecord) {
			record.Verification.ExpiresAt.Seconds += int64(366 * 24 * time.Hour / time.Second)
		},
	} {
		tx, err := workspace.Database.BeginEncryptedTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		repository, err := organisations.NewEvidenceRepository(tx)
		if err != nil {
			t.Fatal(err)
		}
		record := validRecord("application/pdf", []byte("valid-content"))
		mutate(&record)
		if err := repository.Save(context.Background(), record); !errors.Is(err, organisations.ErrEvidenceInvalid) {
			t.Fatalf("invalid evidence error = %v", err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEvidenceRowsAreImmutableAndReadDetectsStoredByteTampering(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspace(t)
	seedIdentityAndOrganisation(t, workspace)
	ctx := context.Background()
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := organisations.NewEvidenceRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	immutableRecord := validRecord("application/pdf", []byte("immutable-pdf-evidence"))
	seedEvidenceOperation(t, tx, immutableRecord.OperationKey)
	if err := repository.Save(ctx, immutableRecord); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`UPDATE organisation_evidence_objects SET mime_type='image/png' WHERE id='` + testEvidenceID + `'`,
		`DELETE FROM organisation_evidence_objects WHERE id='` + testEvidenceID + `'`,
		`UPDATE organisation_verifications SET state=4 WHERE id='` + testVerificationID + `'`,
		`DELETE FROM organisation_verifications WHERE id='` + testVerificationID + `'`,
	} {
		if _, err := workspace.Database.ExecContext(ctx, statement); err == nil {
			t.Fatalf("immutable statement succeeded: %s", statement)
		}
	}
	if _, err := workspace.Database.ExecContext(ctx, `DROP TRIGGER organisation_verification_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Database.ExecContext(ctx, `UPDATE organisation_verifications SET state=3 WHERE id=?`, testVerificationID); err != nil {
		t.Fatal(err)
	}
	tamperedMetadataTx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	tamperedMetadataRepository, err := organisations.NewEvidenceRepository(tamperedMetadataTx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tamperedMetadataRepository.GetCurrentSummary(ctx, testOrganisationID); !errors.Is(err, organisations.ErrEvidenceTampered) {
		t.Fatalf("valid-looking metadata tamper error = %v", err)
	}
	if err := tamperedMetadataTx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Database.ExecContext(ctx, `UPDATE organisation_verifications SET state=? WHERE id=?`, immutableRecord.Verification.State, testVerificationID); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Database.ExecContext(ctx, `DROP TRIGGER organisation_evidence_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Database.ExecContext(ctx, `UPDATE organisation_evidence_objects SET encrypted_bytes=? WHERE id=?`, bytes.Repeat([]byte{'x'}, len(immutableRecord.Evidence.Content)), testEvidenceID); err != nil {
		t.Fatal(err)
	}
	tx, err = workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	repository, err = organisations.NewEvidenceRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Get(ctx, testVerificationID); !errors.Is(err, organisations.ErrEvidenceTampered) {
		t.Fatalf("tampered read error = %v", err)
	}
	if _, err := repository.GetCurrentSummary(ctx, testOrganisationID); !errors.Is(err, organisations.ErrEvidenceTampered) {
		t.Fatalf("tampered current summary error = %v", err)
	}
}

func TestEvidenceSupersessionIsImmutableAndFailedPairLeavesNoOrphan(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspace(t)
	seedIdentityAndOrganisation(t, workspace)
	ctx := context.Background()
	first := validRecord("application/pdf", []byte("first"))
	saveRecord(t, workspace, first)

	second := validRecord("image/png", []byte("second"))
	second.Verification.Id = "018f0000-0000-7000-8000-000000000031"
	second.Verification.EvidenceObjectId = "018f0000-0000-7000-8000-000000000041"
	second.Verification.Source.Id = "018f0000-0000-7000-8000-000000000011"
	second.OperationKey = "018f0000-0000-7000-8000-000000000081"
	second.SupersedesVerificationID = first.Verification.Id
	second.SupersedesEvidenceID = first.Verification.EvidenceObjectId
	saveRecord(t, workspace, second)

	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := organisations.NewEvidenceRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := repository.Get(ctx, second.Verification.Id)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SupersedesEvidenceID != first.Verification.EvidenceObjectId || loaded.SupersedesVerificationID != first.Verification.Id {
		t.Fatalf("supersession links = %#v", loaded)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	orphanAttempt := validRecord("image/jpeg", []byte("must-roll-back"))
	orphanAttempt.Verification.Id = "018f0000-0000-7000-8000-000000000032"
	orphanAttempt.Verification.EvidenceObjectId = "018f0000-0000-7000-8000-000000000042"
	orphanAttempt.OperationKey = "018f0000-0000-7000-8000-000000000082"
	// Reusing the original source tuple fails only after the evidence insert.
	tx, err = workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	repository, err = organisations.NewEvidenceRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	seedEvidenceOperation(t, tx, orphanAttempt.OperationKey)
	if err := repository.Save(ctx, orphanAttempt); err == nil {
		t.Fatal("duplicate source tuple succeeded")
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := workspace.Database.QueryRowContext(ctx, `SELECT count(*) FROM organisation_evidence_objects`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("evidence object count = %d, want 2", count)
	}

	mismatched := validRecord("image/png", []byte("mismatched-predecessor"))
	mismatched.Verification.Id = "018f0000-0000-7000-8000-000000000033"
	mismatched.Verification.EvidenceObjectId = "018f0000-0000-7000-8000-000000000043"
	mismatched.Verification.Source.Id = "018f0000-0000-7000-8000-000000000013"
	mismatched.OperationKey = "018f0000-0000-7000-8000-000000000083"
	mismatched.SupersedesVerificationID = first.Verification.Id
	mismatched.SupersedesEvidenceID = second.Verification.EvidenceObjectId
	tx, err = workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	repository, err = organisations.NewEvidenceRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	seedEvidenceOperation(t, tx, mismatched.OperationKey)
	if err := repository.Save(ctx, mismatched); !errors.Is(err, organisations.ErrEvidenceInvalid) {
		t.Fatalf("mismatched supersession error = %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestEvidenceBytesExistOnlyInsideEncryptedWorkspace(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspace(t)
	seedIdentityAndOrganisation(t, workspace)
	ctx := context.Background()
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := organisations.NewEvidenceRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	sentinel := bytes.Repeat([]byte("PLAINTEXT-EVIDENCE-SENTINEL-"), 32)
	record := validRecord("application/pdf", sentinel)
	seedEvidenceOperation(t, tx, record.OperationKey)
	if err := repository.Save(ctx, record); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Database.Checkpoint(ctx); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Database.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Dir(workspace.Path))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		contents, err := os.ReadFile(filepath.Join(filepath.Dir(workspace.Path), entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(contents, sentinel) {
			t.Fatalf("plaintext evidence appeared in workspace file %q", entry.Name())
		}
	}
}

func TestEvidenceSaveBumpsOrganisationRevisionOnceAndRollbackDoesNot(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspace(t)
	seedIdentityAndOrganisation(t, workspace)
	ctx := context.Background()
	record := validRecord("application/pdf", []byte("revisioned"))
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	seedEvidenceOperation(t, tx, record.OperationKey)
	repository, err := organisations.NewEvidenceRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Save(ctx, record); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	assertEvidenceRevisions(t, workspace, 1, 1, 1)

	replayTx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	replayRepository, err := organisations.NewEvidenceRepository(replayTx)
	if err != nil {
		t.Fatal(err)
	}
	if err := replayRepository.Save(ctx, record); err != nil {
		t.Fatalf("exact replay: %v", err)
	}
	if err := replayTx.Commit(); err != nil {
		t.Fatal(err)
	}
	assertEvidenceRevisions(t, workspace, 1, 1, 1)

	rolledBack := validRecord("image/png", []byte("rolled-back"))
	rolledBack.OperationKey = "018f0000-0000-7000-8000-000000000084"
	rolledBack.Verification.Id = "018f0000-0000-7000-8000-000000000034"
	rolledBack.Verification.EvidenceObjectId = "018f0000-0000-7000-8000-000000000044"
	rolledBack.Verification.Source.Id = "018f0000-0000-7000-8000-000000000014"
	rollbackTx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	seedEvidenceOperation(t, rollbackTx, rolledBack.OperationKey)
	rollbackRepository, err := organisations.NewEvidenceRepository(rollbackTx)
	if err != nil {
		t.Fatal(err)
	}
	if err := rollbackRepository.Save(ctx, rolledBack); err != nil {
		t.Fatal(err)
	}
	if err := rollbackTx.Rollback(); err != nil {
		t.Fatal(err)
	}
	assertEvidenceRevisions(t, workspace, 1, 1, 1)
}

func TestEvidenceReplayRejectsChangedPayloadAndUnrelatedProfileClaim(t *testing.T) {
	workspace := testkit.NewEncryptedWorkspace(t)
	seedIdentityAndOrganisation(t, workspace)
	ctx := context.Background()
	record := validRecord("application/pdf", []byte("original"))
	saveRecord(t, workspace, record)

	changed := record
	changed.Evidence = testkit.VerificationEvidence("application/pdf", evidenceBytes("application/pdf", []byte("changed")))
	changedTx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	changedRepository, err := organisations.NewEvidenceRepository(changedTx)
	if err != nil {
		t.Fatal(err)
	}
	if err := changedRepository.Save(ctx, changed); err == nil {
		t.Fatal("changed evidence replay succeeded")
	}
	if err := changedTx.Rollback(); err != nil {
		t.Fatal(err)
	}

	unrelatedOperation := "018f0000-0000-7000-8000-000000000085"
	claimTx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	seedEvidenceOperation(t, claimTx, unrelatedOperation)
	revisionRepository, err := revisions.NewWithExecutor(
		claimTx.ExecContext,
		func(ctx context.Context, query string, arguments ...any) revisions.RowScanner {
			return claimTx.QueryRowContext(ctx, query, arguments...)
		},
		clock.NewFixed(time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, replay, err := revisionRepository.Bump(ctx, unrelatedOperation, revisions.Domains{OrganisationProfile: true}); err != nil || replay {
		t.Fatalf("unrelated profile claim = replay:%t error:%v", replay, err)
	}
	if err := claimTx.Commit(); err != nil {
		t.Fatal(err)
	}
	unrelated := validRecord("image/png", []byte("unrelated"))
	unrelated.OperationKey = unrelatedOperation
	unrelated.Verification.Id = "018f0000-0000-7000-8000-000000000035"
	unrelated.Verification.EvidenceObjectId = "018f0000-0000-7000-8000-000000000045"
	unrelated.Verification.Source.Id = "018f0000-0000-7000-8000-000000000015"
	unrelatedTx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	unrelatedRepository, err := organisations.NewEvidenceRepository(unrelatedTx)
	if err != nil {
		t.Fatal(err)
	}
	if err := unrelatedRepository.Save(ctx, unrelated); err == nil {
		t.Fatal("unrelated organisation-profile claim was accepted as evidence replay")
	}
	if err := unrelatedTx.Rollback(); err != nil {
		t.Fatal(err)
	}
	assertEvidenceRevisions(t, workspace, 2, 2, 1)
}

func TestEvidenceRepositoryRejectsUnauthenticatedTransaction(t *testing.T) {
	if _, err := organisations.NewEvidenceRepository(&sqlcipher.Transaction{}); err == nil {
		t.Fatal("zero-value transaction was accepted")
	}
	workspace := testkit.NewEncryptedWorkspace(t)
	forgedDatabase := &sqlcipher.Database{DB: workspace.Database.DB}
	forgedTransaction, err := forgedDatabase.BeginEncryptedTx(context.Background(), nil)
	if err == nil {
		defer forgedTransaction.Rollback()
		if _, repositoryErr := organisations.NewEvidenceRepository(forgedTransaction); repositoryErr == nil {
			t.Fatal("transaction minted by an unauthenticated database wrapper was accepted")
		}
	}
	otherWorkspace := testkit.NewEncryptedWorkspace(t)
	authenticatedDatabase := workspace.Database.DB
	workspace.Database.DB = otherWorkspace.Database.DB
	if swappedTransaction, err := workspace.Database.BeginEncryptedTx(context.Background(), nil); err == nil {
		_ = swappedTransaction.Rollback()
		t.Fatal("transaction minted after authenticated database handle substitution")
	}
	workspace.Database.DB = authenticatedDatabase
	issued, err := workspace.Database.BeginEncryptedTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	copyOfIssued := reflect.New(reflect.TypeOf(issued).Elem()).Interface().(*sqlcipher.Transaction)
	reflect.ValueOf(copyOfIssued).Elem().Set(reflect.ValueOf(issued).Elem())
	if _, err := organisations.NewEvidenceRepository(copyOfIssued); err == nil {
		t.Fatal("copied transaction capability was accepted")
	}
	if err := issued.Rollback(); err != nil {
		t.Fatal(err)
	}
}

func validRecord(mimeType string, content []byte) organisations.VerificationRecord {
	recordedAt := time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)
	return organisations.VerificationRecord{
		OperationKey:    testOperationID,
		Verification:    testkit.EntityVerification(testVerificationID, testOrganisationID, testEvidenceID, recordedAt),
		Evidence:        testkit.VerificationEvidence(mimeType, evidenceBytes(mimeType, content)),
		CreatedByUserID: testkit.ActorUserID,
	}
}

func evidenceBytes(mimeType string, marker []byte) []byte {
	switch mimeType {
	case "application/pdf":
		var document bytes.Buffer
		document.WriteString("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
		offsets := make([]int, 4)
		offsets[1] = document.Len()
		document.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
		offsets[2] = document.Len()
		document.WriteString("2 0 obj\n<< /Type /Pages /Count 0 /Kids [] >>\nendobj\n")
		offsets[3] = document.Len()
		fmt.Fprintf(&document, "3 0 obj\n<< /Length %d >>\nstream\n", len(marker))
		document.Write(marker)
		document.WriteString("\nendstream\nendobj\n")
		xrefOffset := document.Len()
		document.WriteString("xref\n0 4\n0000000000 65535 f \n")
		for object := 1; object <= 3; object++ {
			fmt.Fprintf(&document, "%010d 00000 n \n", offsets[object])
		}
		fmt.Fprintf(&document, "trailer\n<< /Size 4 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xrefOffset)
		return document.Bytes()
	case "image/jpeg", "image/png":
		picture := image.NewNRGBA(image.Rect(0, 0, 1, 1))
		picture.Set(0, 0, color.NRGBA{R: 0x54, G: 0x41, B: 0x4d, A: 0xff})
		var encoded bytes.Buffer
		var err error
		if mimeType == "image/jpeg" {
			err = jpeg.Encode(&encoded, picture, &jpeg.Options{Quality: 90})
		} else {
			err = png.Encode(&encoded, picture)
		}
		if err != nil {
			panic(err)
		}
		return encoded.Bytes()
	default:
		return append([]byte(nil), marker...)
	}
}

func corruptPDFRootXRef(content []byte) []byte {
	corrupted := append([]byte(nil), content...)
	xref := bytes.Index(corrupted, []byte("xref\n0 4\n"))
	if xref < 0 {
		panic("test PDF has no xref")
	}
	rootEntry := xref
	for newline := 0; newline < 3; newline++ {
		next := bytes.IndexByte(corrupted[rootEntry:], '\n')
		if next < 0 {
			panic("test PDF has incomplete xref")
		}
		rootEntry += next + 1
	}
	copy(corrupted[rootEntry:rootEntry+10], "0000000000")
	return corrupted
}

func oversizedCompressedPNG(width, height uint32) []byte {
	var document bytes.Buffer
	document.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], width)
	binary.BigEndian.PutUint32(ihdr[4:8], height)
	ihdr[8] = 1
	writePNGChunk(&document, "IHDR", ihdr)
	var compressed bytes.Buffer
	encoder := zlib.NewWriter(&compressed)
	row := make([]byte, 1+(width+7)/8)
	for y := uint32(0); y < height; y++ {
		if _, err := encoder.Write(row); err != nil {
			panic(err)
		}
	}
	if err := encoder.Close(); err != nil {
		panic(err)
	}
	writePNGChunk(&document, "IDAT", compressed.Bytes())
	writePNGChunk(&document, "IEND", nil)
	return document.Bytes()
}

func writePNGChunk(document *bytes.Buffer, chunkType string, data []byte) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(data)))
	document.Write(length[:])
	document.WriteString(chunkType)
	document.Write(data)
	var checksum [4]byte
	binary.BigEndian.PutUint32(checksum[:], crc32.ChecksumIEEE(append([]byte(chunkType), data...)))
	document.Write(checksum[:])
}

func saveRecord(t *testing.T, workspace *testkit.EncryptedWorkspace, record organisations.VerificationRecord) {
	t.Helper()
	tx, err := workspace.Database.BeginEncryptedTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := organisations.NewEvidenceRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	seedEvidenceOperation(t, tx, record.OperationKey)
	if err := repository.Save(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func seedEvidenceOperation(t *testing.T, transaction *sqlcipher.Transaction, operationKey string) {
	t.Helper()
	if _, err := transaction.ExecContext(context.Background(), `
		INSERT INTO idempotency_records(
			operation_key, command_type, semantic_sha256, result_type,
			result_proto, state, created_at
		) VALUES (?, 'tammy.v1.RecordOrganisationVerificationCommand', ?,
			'tammy.v1.EntityVerification', ?, 'ELECTED', ?)`,
		operationKey, "0000000000000000000000000000000000000000000000000000000000000000",
		[]byte{1}, "2026-08-04T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
}

func assertEvidenceRevisions(t *testing.T, workspace *testkit.EncryptedWorkspace, financial, organisation, evidenceCount int) {
	t.Helper()
	var gotFinancial, gotOrganisation, gotEvidence int
	if err := workspace.Database.QueryRow(`
		SELECT financial_revision, organisation_profile_revision,
		       (SELECT count(*) FROM organisation_verifications)
		FROM financial_revisions WHERE id=1`).Scan(&gotFinancial, &gotOrganisation, &gotEvidence); err != nil {
		t.Fatal(err)
	}
	if gotFinancial != financial || gotOrganisation != organisation || gotEvidence != evidenceCount {
		t.Fatalf("revision/evidence state = financial:%d organisation:%d evidence:%d", gotFinancial, gotOrganisation, gotEvidence)
	}
}

func seedIdentityAndOrganisation(t *testing.T, workspace *testkit.EncryptedWorkspace) {
	t.Helper()
	ctx := context.Background()
	if _, err := workspace.Database.ExecContext(ctx, `INSERT INTO users(id,email,display_name,status,created_at,updated_at) VALUES (?,?,?,?,?,?)`,
		testkit.ActorUserID, "owner@tammy.test", "Owner", "ACTIVE", "2026-08-04T00:00:00Z", "2026-08-04T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Database.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status,created_at) VALUES (?,?,?,?)`,
		testOrganisationID, "Tammy Pty Ltd", "ACTIVE", "2026-08-04T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
}
