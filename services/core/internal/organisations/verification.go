package organisations

import (
	"context"
	"crypto/sha256"
	"errors"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const MaxVerificationEvidenceBytes = 1 << 20

var (
	ErrEvidenceInvalid        = errors.New("organisations: invalid verification evidence")
	ErrEvidenceNotFound       = errors.New("organisations: verification evidence not found")
	ErrEvidenceReplayConflict = errors.New("organisations: evidence operation replay conflict")
	ErrEvidenceTampered       = errors.New("organisations: evidence integrity failure")
)

// VerificationRecord is one immutable capability-supplied evidence object and
// retained metadata. It contains bytes, never a filesystem path.
type VerificationRecord struct {
	OperationKey             string
	Verification             *tammyv1.EntityVerification
	Evidence                 *tammyv1.VerificationEvidence
	CreatedByUserID          string
	SupersedesEvidenceID     string
	SupersedesVerificationID string
}

type OrganisationEvidenceIntakePort interface {
	Save(context.Context, VerificationRecord) error
}

func BuildVerificationRecord(
	request *tammyv1.RecordEntityVerificationRequest,
	verificationID, evidenceID, actorUserID string,
	now time.Time,
) (VerificationRecord, error) {
	if request == nil || request.CommandContext == nil || request.Evidence == nil || request.Source == nil ||
		!ids.IsCanonicalV7(request.OrganisationId) || !ids.IsCanonicalV7(verificationID) ||
		!ids.IsCanonicalV7(evidenceID) || !ids.IsCanonicalV7(actorUserID) ||
		(request.Outcome != tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_VERIFIED &&
			request.Outcome != tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_FAILED) ||
		(request.SourceMethod != tammyv1.VerificationSourceMethod_VERIFICATION_SOURCE_METHOD_ABR_ONLINE &&
			request.SourceMethod != tammyv1.VerificationSourceMethod_VERIFICATION_SOURCE_METHOD_ABR_EXTRACT_MANUAL) ||
		!validSourceRef(request.Source) || !boundedCanonicalText(request.VerifiedLegalName, 256) ||
		!boundedCanonicalText(request.VerifiedEntityType, 96) || request.LookupTime == nil ||
		request.LookupTime.AsTime().After(now) || now.Sub(request.LookupTime.AsTime()) > 24*time.Hour ||
		len(request.Evidence.Content) == 0 || len(request.Evidence.Content) > MaxVerificationEvidenceBytes ||
		len(request.Evidence.ContentHash) != sha256.Size {
		return VerificationRecord{}, ErrEvidenceInvalid
	}
	digest := sha256.Sum256(request.Evidence.Content)
	if !proto.Equal(&tammyv1.VerificationEvidence{MimeType: request.Evidence.MimeType,
		Content: request.Evidence.Content, ContentHash: digest[:]}, request.Evidence) {
		return VerificationRecord{}, ErrEvidenceInvalid
	}
	verification := &tammyv1.EntityVerification{Id: verificationID, OrganisationId: request.OrganisationId,
		State: request.Outcome, SourceMethod: request.SourceMethod, Source: proto.Clone(request.Source).(*tammyv1.SourceRef),
		VerifiedLegalName: request.VerifiedLegalName, VerifiedEntityType: request.VerifiedEntityType,
		EvidenceObjectId: evidenceID, RecordedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.AddDate(1, 0, 0))}
	return VerificationRecord{OperationKey: request.CommandContext.IdempotencyKey, Verification: verification,
		Evidence: proto.Clone(request.Evidence).(*tammyv1.VerificationEvidence), CreatedByUserID: actorUserID}, nil
}

func ValidVerificationTransition(from, to tammyv1.OrganisationVerificationState) bool {
	switch from {
	case tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_UNVERIFIED:
		return to == tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_VERIFIED ||
			to == tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_FAILED
	case tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_FAILED:
		return to == tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_VERIFIED ||
			to == tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_EXPIRED
	case tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_VERIFIED:
		return to == tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_EXPIRED ||
			to == tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_SUPERSEDED
	default:
		return false
	}
}
