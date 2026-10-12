package baspreparation

import (
	"strings"
	"unicode/utf8"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

const (
	basSourceIdentityMaximum = 20
)

// validBASRawSourceRef validates a BAS-local raw identity without imposing the
// UUID, positive-revision, or mandatory-original-hash rules of SourceRef.
func validBASRawSourceRef(value *tammyv1.BasRawSourceRef) bool {
	if value == nil || !utf8.ValidString(value.Type) || !utf8.ValidString(value.Id) ||
		len(value.Type) == 0 || len(value.Type) > 128 || len(value.Id) == 0 ||
		strings.TrimSpace(value.Id) == "" || len(value.ComputedProofHash) != 32 {
		return false
	}
	for index, char := range []byte(value.Type) {
		if (index == 0 && (char < 'a' || char > 'z')) ||
			(index > 0 && !((char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '_')) {
			return false
		}
	}
	if value.OriginalRevision != nil && *value.OriginalRevision < 0 {
		return false
	}
	if value.OriginalContentHash != nil && len(value.OriginalContentHash) != 32 {
		return false
	}
	switch value.ProofScope {
	case tammyv1.BasSourceProofScope_BAS_SOURCE_PROOF_SCOPE_SOURCE_ROW_PROJECTION,
		tammyv1.BasSourceProofScope_BAS_SOURCE_PROOF_SCOPE_COMPLETE_JOURNAL,
		tammyv1.BasSourceProofScope_BAS_SOURCE_PROOF_SCOPE_REFERENCE_ONLY_PROJECTION:
		return true
	default:
		return false
	}
}

// validBASSourceIdentity preserves strict canonical SourceRef rules while
// allowing the explicitly nullable, opaque BAS raw-source representation.
func validBASSourceIdentity(value *tammyv1.BasSourceIdentity) bool {
	if value == nil || (value.CanonicalSource == nil) == (value.RawSource == nil) {
		return false
	}
	if value.CanonicalSource != nil {
		return validRef(value.CanonicalSource, value.CanonicalSource.Type) && len(value.CanonicalSource.Type) <= 128
	}
	return validBASRawSourceRef(value.RawSource)
}

// validBASIdentityProjection enforces that a populated mixed-identity list has
// exactly the same ordered canonical projection as its legacy SourceRef field.
// An empty new list deliberately retains the historical meaning of the old
// list without manufacturing a second representation.
func validBASIdentityProjection(identities []*tammyv1.BasSourceIdentity, canonical []*tammyv1.SourceRef) bool {
	if len(identities) > basSourceIdentityMaximum {
		return false
	}
	for _, identity := range identities {
		if !validBASSourceIdentity(identity) {
			return false
		}
	}
	if len(identities) == 0 {
		return true
	}
	projection := make([]*tammyv1.SourceRef, 0, len(identities))
	for _, identity := range identities {
		if identity.CanonicalSource != nil {
			projection = append(projection, identity.CanonicalSource)
		}
	}
	if len(projection) != len(canonical) {
		return false
	}
	for index := range projection {
		if !sameBASSourceRef(projection[index], canonical[index]) {
			return false
		}
	}
	return true
}
