//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

// Package organisations owns organisation profile and verification persistence.
package organisations

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"regexp"
	"strconv"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/revisions"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	maxEvidenceImageDimension = 12_000
	maxEvidenceImagePixels    = 40_000_000
)

const evidenceTimestampLayout = "2006-01-02T15:04:05.000000000Z"

var sourceTypePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

var (
	pdfStartXRefPattern = regexp.MustCompile(`(?s)startxref[\t\r\n ]+([0-9]+)[\t\r\n ]+%%EOF[\t\r\n ]*$`)
	pdfRootPattern      = regexp.MustCompile(`/Root[\t\r\n ]+([1-9][0-9]*)[\t\r\n ]+([0-9]+)[\t\r\n ]+R`)
	pdfXRefPattern      = regexp.MustCompile(`(?m)^xref[\t ]*\r?\n[0-9]+[\t ]+[1-9][0-9]*[\t ]*\r?$`)
)

// EvidenceRepository is scoped to its caller-owned transaction.
type EvidenceRepository struct {
	tx *sqlcipher.Transaction
}

func NewEvidenceRepository(tx *sqlcipher.Transaction) (*EvidenceRepository, error) {
	if tx == nil || !tx.Authenticated() {
		return nil, errors.New("organisations: transaction is required")
	}
	return &EvidenceRepository{tx: tx}, nil
}

func (repository *EvidenceRepository) Save(ctx context.Context, record VerificationRecord) error {
	if err := validateRecord(record); err != nil {
		return err
	}
	semanticHash, err := evidenceSemanticHash(record)
	if err != nil {
		return ErrEvidenceInvalid
	}
	if _, err := repository.tx.ExecContext(ctx, `SAVEPOINT organisation_evidence_save`); err != nil {
		return fmt.Errorf("organisations: begin evidence save: %w", err)
	}
	fail := func(saveErr error) error {
		_, rollbackErr := repository.tx.ExecContext(context.WithoutCancel(ctx), `ROLLBACK TO organisation_evidence_save`)
		_, releaseErr := repository.tx.ExecContext(context.WithoutCancel(ctx), `RELEASE organisation_evidence_save`)
		return errors.Join(saveErr, rollbackErr, releaseErr)
	}
	verification := record.Verification
	evidence := record.Evidence
	revisionRepository, err := revisions.NewWithExecutor(
		repository.tx.ExecContext,
		func(ctx context.Context, query string, arguments ...any) revisions.RowScanner {
			return repository.tx.QueryRowContext(ctx, query, arguments...)
		},
		clock.NewFixed(verification.RecordedAt.AsTime()),
	)
	if err != nil {
		return fail(err)
	}
	_, replay, err := revisionRepository.Bump(ctx, record.OperationKey, revisions.Domains{OrganisationProfile: true})
	if err != nil {
		return fail(fmt.Errorf("organisations: bump verification revision: %w", err))
	}
	if replay {
		var retainedHash []byte
		if err := repository.tx.QueryRowContext(ctx, `
			SELECT semantic_sha256 FROM organisation_verifications
			WHERE operation_key = ?`, record.OperationKey).Scan(&retainedHash); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fail(ErrEvidenceReplayConflict)
			}
			return fail(fmt.Errorf("organisations: load evidence replay identity: %w", err))
		}
		if !bytes.Equal(retainedHash, semanticHash) {
			return fail(ErrEvidenceReplayConflict)
		}
		if _, err := repository.tx.ExecContext(ctx, `RELEASE organisation_evidence_save`); err != nil {
			return fail(fmt.Errorf("organisations: commit evidence replay: %w", err))
		}
		return nil
	}
	if record.SupersedesVerificationID != "" {
		var predecessorCount int
		if err := repository.tx.QueryRowContext(ctx, `
			SELECT count(*)
			FROM organisation_verifications
			WHERE id = ? AND evidence_object_id = ? AND organisation_id = ?`,
			record.SupersedesVerificationID, record.SupersedesEvidenceID,
			verification.OrganisationId).Scan(&predecessorCount); err != nil {
			return fail(fmt.Errorf("organisations: verify supersession predecessor: %w", err))
		}
		if predecessorCount != 1 {
			return fail(ErrEvidenceInvalid)
		}
	}
	recordedAt := verification.RecordedAt.AsTime().UTC().Format(evidenceTimestampLayout)
	expiresAt := verification.ExpiresAt.AsTime().UTC().Format(evidenceTimestampLayout)
	if _, err := repository.tx.ExecContext(ctx, `
		INSERT INTO organisation_evidence_objects(
			id, mime_type, byte_length, sha256, encrypted_bytes,
			created_by_user_id, created_at, supersedes_evidence_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''))`,
		verification.EvidenceObjectId, evidence.MimeType, len(evidence.Content),
		evidence.ContentHash, evidence.Content, record.CreatedByUserID, recordedAt,
		record.SupersedesEvidenceID,
	); err != nil {
		return fail(fmt.Errorf("organisations: store evidence object: %w", err))
	}
	if _, err := repository.tx.ExecContext(ctx, `
		INSERT INTO organisation_verifications(
			id, operation_key, semantic_sha256, organisation_id, evidence_object_id,
			source_type, source_id, source_revision, source_content_hash,
			source_method, state, verified_legal_name, verified_entity_type,
			recorded_at, expires_at, supersedes_verification_id,
			created_by_user_id, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?)`,
		verification.Id, record.OperationKey, semanticHash,
		verification.OrganisationId, verification.EvidenceObjectId,
		verification.Source.Type, verification.Source.Id, verification.Source.Revision,
		verification.Source.ContentHash, int32(verification.SourceMethod), int32(verification.State),
		verification.VerifiedLegalName, verification.VerifiedEntityType, recordedAt, expiresAt,
		record.SupersedesVerificationID, record.CreatedByUserID, recordedAt,
	); err != nil {
		return fail(fmt.Errorf("organisations: store verification: %w", err))
	}
	if _, err := repository.tx.ExecContext(ctx, `RELEASE organisation_evidence_save`); err != nil {
		return fail(fmt.Errorf("organisations: commit evidence save: %w", err))
	}
	return nil
}

func (repository *EvidenceRepository) Get(ctx context.Context, verificationID string) (VerificationRecord, error) {
	var (
		record                VerificationRecord
		verification          tammyv1.EntityVerification
		evidence              tammyv1.VerificationEvidence
		state, sourceMethod   int32
		sourceRevision        uint64
		recordedAt, expiresAt string
		byteLength            int
		storedHash            []byte
		storedSemanticHash    []byte
	)
	verification.Source = &tammyv1.SourceRef{}
	err := repository.tx.QueryRowContext(ctx, `
		SELECT v.id, v.organisation_id, v.evidence_object_id,
		       v.source_type, v.source_id, v.source_revision, v.source_content_hash,
		       v.source_method, v.state, v.verified_legal_name, v.verified_entity_type,
		       v.recorded_at, v.expires_at, COALESCE(v.supersedes_verification_id, ''),
		       v.created_by_user_id, v.operation_key, v.semantic_sha256,
		       e.mime_type, e.byte_length, e.sha256, e.encrypted_bytes,
		       COALESCE(e.supersedes_evidence_id, '')
		FROM organisation_verifications v
		JOIN organisation_evidence_objects e ON e.id = v.evidence_object_id
		WHERE v.id = ?`, verificationID).Scan(
		&verification.Id, &verification.OrganisationId, &verification.EvidenceObjectId,
		&verification.Source.Type, &verification.Source.Id, &sourceRevision,
		&verification.Source.ContentHash, &sourceMethod, &state,
		&verification.VerifiedLegalName, &verification.VerifiedEntityType,
		&recordedAt, &expiresAt, &record.SupersedesVerificationID,
		&record.CreatedByUserID, &record.OperationKey, &storedSemanticHash,
		&evidence.MimeType, &byteLength, &storedHash,
		&evidence.Content, &record.SupersedesEvidenceID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return VerificationRecord{}, ErrEvidenceNotFound
	}
	if err != nil {
		return VerificationRecord{}, fmt.Errorf("organisations: load verification evidence: %w", err)
	}
	verification.Source.Revision = sourceRevision
	verification.SourceMethod = tammyv1.VerificationSourceMethod(sourceMethod)
	verification.State = tammyv1.OrganisationVerificationState(state)
	verification.RecordedAt, err = parseTimestamp(recordedAt)
	if err != nil {
		return VerificationRecord{}, ErrEvidenceTampered
	}
	verification.ExpiresAt, err = parseTimestamp(expiresAt)
	if err != nil {
		return VerificationRecord{}, ErrEvidenceTampered
	}
	digest := sha256.Sum256(evidence.Content)
	if byteLength != len(evidence.Content) || !bytes.Equal(storedHash, digest[:]) {
		return VerificationRecord{}, ErrEvidenceTampered
	}
	evidence.ContentHash = append([]byte(nil), storedHash...)
	record.Verification = proto.Clone(&verification).(*tammyv1.EntityVerification)
	record.Evidence = proto.Clone(&evidence).(*tammyv1.VerificationEvidence)
	semanticHash, err := evidenceSemanticHash(record)
	if err != nil || !bytes.Equal(storedSemanticHash, semanticHash) {
		return VerificationRecord{}, ErrEvidenceTampered
	}
	return record, nil
}

func evidenceSemanticHash(record VerificationRecord) ([]byte, error) {
	verification, err := proto.MarshalOptions{Deterministic: true}.Marshal(record.Verification)
	if err != nil {
		return nil, err
	}
	evidence, err := proto.MarshalOptions{Deterministic: true}.Marshal(record.Evidence)
	if err != nil {
		return nil, err
	}
	digest := sha256.New()
	_, _ = digest.Write([]byte("tammy.organisations.verification-evidence.v1\x00"))
	for _, value := range [][]byte{
		verification,
		evidence,
		[]byte(record.CreatedByUserID),
		[]byte(record.SupersedesEvidenceID),
		[]byte(record.SupersedesVerificationID),
	} {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(value)))
		_, _ = digest.Write(length[:])
		_, _ = digest.Write(value)
	}
	return digest.Sum(nil), nil
}

func validateRecord(record VerificationRecord) error {
	verification, evidence := record.Verification, record.Evidence
	if verification == nil || evidence == nil || !ids.IsCanonicalV7(record.OperationKey) ||
		!ids.IsCanonicalV7(record.CreatedByUserID) ||
		!ids.IsCanonicalV7(verification.Id) || !ids.IsCanonicalV7(verification.OrganisationId) ||
		!ids.IsCanonicalV7(verification.EvidenceObjectId) || verification.Source == nil {
		return ErrEvidenceInvalid
	}
	if (record.SupersedesEvidenceID == "") != (record.SupersedesVerificationID == "") {
		return ErrEvidenceInvalid
	}
	if record.SupersedesEvidenceID != "" &&
		(!ids.IsCanonicalV7(record.SupersedesEvidenceID) ||
			!ids.IsCanonicalV7(record.SupersedesVerificationID) ||
			record.SupersedesEvidenceID == verification.EvidenceObjectId ||
			record.SupersedesVerificationID == verification.Id) {
		return ErrEvidenceInvalid
	}
	if evidence.MimeType != "application/pdf" && evidence.MimeType != "image/jpeg" && evidence.MimeType != "image/png" {
		return ErrEvidenceInvalid
	}
	if len(evidence.Content) == 0 || len(evidence.Content) > MaxVerificationEvidenceBytes || len(evidence.ContentHash) != sha256.Size {
		return ErrEvidenceInvalid
	}
	if !validEvidenceContent(evidence.MimeType, evidence.Content) {
		return ErrEvidenceInvalid
	}
	digest := sha256.Sum256(evidence.Content)
	if !bytes.Equal(evidence.ContentHash, digest[:]) {
		return ErrEvidenceInvalid
	}
	source := verification.Source
	if !sourceTypePattern.MatchString(source.Type) || !ids.IsCanonicalV7(source.Id) ||
		source.Revision == 0 || len(source.ContentHash) != sha256.Size {
		return ErrEvidenceInvalid
	}
	if verification.SourceMethod != tammyv1.VerificationSourceMethod_VERIFICATION_SOURCE_METHOD_ABR_ONLINE &&
		verification.SourceMethod != tammyv1.VerificationSourceMethod_VERIFICATION_SOURCE_METHOD_ABR_EXTRACT_MANUAL {
		return ErrEvidenceInvalid
	}
	if verification.State <= tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_UNSPECIFIED ||
		verification.State > tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_SUPERSEDED {
		return ErrEvidenceInvalid
	}
	if len(verification.VerifiedLegalName) > 256 || len(verification.VerifiedEntityType) > 96 {
		return ErrEvidenceInvalid
	}
	if verification.RecordedAt == nil || verification.ExpiresAt == nil ||
		!verification.RecordedAt.IsValid() || !verification.ExpiresAt.IsValid() {
		return ErrEvidenceInvalid
	}
	recordedAt, expiresAt := verification.RecordedAt.AsTime(), verification.ExpiresAt.AsTime()
	if expiresAt.Before(recordedAt) || expiresAt.After(recordedAt.AddDate(1, 0, 0)) {
		return ErrEvidenceInvalid
	}
	return nil
}

func validEvidenceContent(mimeType string, content []byte) bool {
	switch mimeType {
	case "application/pdf":
		return validPDF(content)
	case "image/jpeg", "image/png":
		if (mimeType == "image/jpeg" && !validJPEGStructure(content)) ||
			(mimeType == "image/png" && !validPNGStructure(content)) {
			return false
		}
		configurationReader := newEvidenceCountingReader(content)
		configuration, format, err := image.DecodeConfig(configurationReader)
		if err != nil || configuration.Width <= 0 || configuration.Height <= 0 ||
			configuration.Width > maxEvidenceImageDimension || configuration.Height > maxEvidenceImageDimension ||
			uint64(configuration.Width)*uint64(configuration.Height) > maxEvidenceImagePixels {
			return false
		}
		decodedReader := newEvidenceCountingReader(content)
		decoded, decodedFormat, err := image.Decode(decodedReader)
		if err != nil || decoded.Bounds().Dx() <= 0 || decoded.Bounds().Dy() <= 0 {
			return false
		}
		expectedFormat := "png"
		if mimeType == "image/jpeg" {
			expectedFormat = "jpeg"
		}
		return format == expectedFormat && decodedFormat == expectedFormat && decodedReader.count == len(content)
	default:
		return false
	}
}

type evidenceCountingReader struct {
	reader *bytes.Reader
	count  int
}

func newEvidenceCountingReader(content []byte) *evidenceCountingReader {
	return &evidenceCountingReader{reader: bytes.NewReader(content)}
}

func (reader *evidenceCountingReader) Read(destination []byte) (int, error) {
	count, err := reader.reader.Read(destination)
	reader.count += count
	return count, err
}

func validPNGStructure(content []byte) bool {
	signature := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	if !bytes.HasPrefix(content, signature) {
		return false
	}
	position := len(signature)
	seenHeader := false
	seenImageData := false
	for position < len(content) {
		if len(content)-position < 12 {
			return false
		}
		length := uint64(binary.BigEndian.Uint32(content[position : position+4]))
		if length > uint64(len(content)-position-12) {
			return false
		}
		typeStart := position + 4
		dataEnd := typeStart + 4 + int(length)
		chunkEnd := dataEnd + 4
		chunkType := string(content[typeStart : typeStart+4])
		if crc32.ChecksumIEEE(content[typeStart:dataEnd]) != binary.BigEndian.Uint32(content[dataEnd:chunkEnd]) {
			return false
		}
		position = chunkEnd
		switch chunkType {
		case "IHDR":
			if seenHeader || length != 13 || typeStart != len(signature)+4 {
				return false
			}
			seenHeader = true
		case "IDAT":
			if !seenHeader {
				return false
			}
			seenImageData = true
		case "IEND":
			return seenHeader && seenImageData && length == 0 && position == len(content)
		}
	}
	return false
}

func validJPEGStructure(content []byte) bool {
	if len(content) < 4 || content[0] != 0xff || content[1] != 0xd8 {
		return false
	}
	position := 2
	inScan := false
	for position < len(content) {
		if inScan && content[position] != 0xff {
			position++
			continue
		}
		if content[position] != 0xff {
			return false
		}
		for position < len(content) && content[position] == 0xff {
			position++
		}
		if position >= len(content) {
			return false
		}
		marker := content[position]
		position++
		if inScan {
			switch {
			case marker == 0x00:
				continue
			case marker >= 0xd0 && marker <= 0xd7:
				continue
			case marker == 0xd9:
				return position == len(content)
			}
			inScan = false
		} else if marker == 0x00 {
			return false
		}
		if marker == 0xd9 {
			return position == len(content)
		}
		if marker == 0xd8 {
			return false
		}
		if marker == 0x01 || (marker >= 0xd0 && marker <= 0xd7) {
			continue
		}
		if len(content)-position < 2 {
			return false
		}
		segmentLength := int(binary.BigEndian.Uint16(content[position : position+2]))
		if segmentLength < 2 || segmentLength > len(content)-position {
			return false
		}
		position += segmentLength
		if marker == 0xda {
			inScan = true
		}
	}
	return false
}

func validPDF(content []byte) bool {
	if !bytes.HasPrefix(content, []byte("%PDF-")) {
		return false
	}
	match := pdfStartXRefPattern.FindSubmatchIndex(content)
	if match == nil || match[2] < 0 || match[3] < 0 {
		return false
	}
	xrefOffset, err := strconv.Atoi(string(content[match[2]:match[3]]))
	if err != nil || xrefOffset <= 0 || xrefOffset >= match[0] {
		return false
	}
	xrefSection := content[xrefOffset:match[0]]
	if !pdfXRefPattern.Match(xrefSection) {
		return false
	}
	trailerIndex := bytes.LastIndex(xrefSection, []byte("trailer"))
	if trailerIndex < 0 {
		return false
	}
	root := pdfRootPattern.FindSubmatch(xrefSection[trailerIndex:])
	if len(root) != 3 {
		return false
	}
	rootObjectNumber, err := strconv.Atoi(string(root[1]))
	if err != nil {
		return false
	}
	rootGeneration, err := strconv.Atoi(string(root[2]))
	if err != nil {
		return false
	}
	objectStart, ok := pdfXRefObjectOffset(xrefSection[:trailerIndex], rootObjectNumber, rootGeneration)
	if !ok || objectStart <= 0 || objectStart >= xrefOffset {
		return false
	}
	objectHeader := append(append(append([]byte(nil), root[1]...), ' '), root[2]...)
	objectHeader = append(objectHeader, []byte(" obj")...)
	if !bytes.HasPrefix(content[objectStart:xrefOffset], objectHeader) {
		return false
	}
	objectEnd := bytes.Index(content[objectStart:xrefOffset], []byte("endobj"))
	if objectEnd < 0 {
		return false
	}
	rootObject := content[objectStart : objectStart+objectEnd]
	return bytes.Contains(rootObject, []byte("/Type /Catalog"))
}

func pdfXRefObjectOffset(section []byte, wantedObject, wantedGeneration int) (int, bool) {
	lines := bytes.Split(section, []byte{'\n'})
	if len(lines) == 0 || string(bytes.TrimSpace(lines[0])) != "xref" {
		return 0, false
	}
	for line := 1; line < len(lines); {
		header := bytes.Fields(lines[line])
		line++
		if len(header) == 0 {
			continue
		}
		if len(header) != 2 {
			return 0, false
		}
		first, firstErr := strconv.Atoi(string(header[0]))
		count, countErr := strconv.Atoi(string(header[1]))
		if firstErr != nil || countErr != nil || first < 0 || count <= 0 || line+count > len(lines) {
			return 0, false
		}
		for entryIndex := 0; entryIndex < count; entryIndex++ {
			entry := bytes.Fields(lines[line+entryIndex])
			if len(entry) != 3 {
				return 0, false
			}
			if first+entryIndex != wantedObject {
				continue
			}
			offset, offsetErr := strconv.Atoi(string(entry[0]))
			generation, generationErr := strconv.Atoi(string(entry[1]))
			return offset, offsetErr == nil && generationErr == nil &&
				generation == wantedGeneration && string(entry[2]) == "n"
		}
		line += count
	}
	return 0, false
}

func parseTimestamp(value string) (*timestamppb.Timestamp, error) {
	instant, err := time.Parse(evidenceTimestampLayout, value)
	if err != nil {
		return nil, err
	}
	return timestamppb.New(instant), nil
}
