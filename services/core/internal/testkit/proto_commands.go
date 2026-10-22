package testkit

import (
	"crypto/sha256"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	ActorUserID = "018f0000-0000-7000-8000-000000000001"
	SessionID   = "018f0000-0000-7000-8000-000000000002"
)

func CommandContext(idempotencyKey string) *tammyv1.CommandContext {
	return &tammyv1.CommandContext{
		IdempotencyKey: idempotencyKey,
		Authentication: &tammyv1.AuthenticationContext{
			ActorUserId: ActorUserID,
			SessionId:   SessionID,
		},
	}
}

func VerificationEvidence(mimeType string, content []byte) *tammyv1.VerificationEvidence {
	digest := sha256.Sum256(content)
	return &tammyv1.VerificationEvidence{
		MimeType:    mimeType,
		Content:     append([]byte(nil), content...),
		ContentHash: append([]byte(nil), digest[:]...),
	}
}

func EntityVerification(id, organisationID, evidenceID string, recordedAt time.Time) *tammyv1.EntityVerification {
	digest := sha256.Sum256([]byte("abr-source"))
	return &tammyv1.EntityVerification{
		Id:             id,
		OrganisationId: organisationID,
		State:          tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_VERIFIED,
		SourceMethod:   tammyv1.VerificationSourceMethod_VERIFICATION_SOURCE_METHOD_ABR_EXTRACT_MANUAL,
		Source: &tammyv1.SourceRef{
			Type:        "abr_extract",
			Id:          "018f0000-0000-7000-8000-000000000010",
			Revision:    1,
			ContentHash: digest[:],
		},
		VerifiedLegalName:  "Tammy Pty Ltd",
		VerifiedEntityType: "Australian Private Company",
		EvidenceObjectId:   evidenceID,
		RecordedAt:         timestamppb.New(recordedAt.UTC()),
		ExpiresAt:          timestamppb.New(recordedAt.UTC().Add(365 * 24 * time.Hour)),
	}
}
