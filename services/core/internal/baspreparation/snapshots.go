package baspreparation

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"math"

	"buf.build/go/protovalidate"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var (
	ErrSnapshotCorrupt = errors.New("baspreparation: invalid or inconsistent retained snapshot")
	ErrSnapshotBound   = errors.New("baspreparation: complete snapshot exceeds supported storage capacity")
)

const (
	scalarSnapshotLimit      = 64 * 1024
	calculationSnapshotLimit = 4 * 1024 * 1024
	frozenSnapshotLimit      = 8 * 1024 * 1024
)

// snapshotSpec is private: each repository supplies a fixed typed resource
// specification, never a caller-selected table or protobuf descriptor.
type snapshotSpec[T proto.Message] struct {
	resource     string
	maxBytes     int
	externalHash bool // Messages without an own-hash field retain the whole-body digest in SQL.
	newMessage   func() T
	identity     func(T) (id, organisation string, revision uint64)
	getHash      func(T) []byte
	setHash      func(T, []byte)
	bind         func(T) bool
}

func sealSnapshot[T proto.Message](value T, spec snapshotSpec[T]) ([]byte, []byte, error) {
	if !value.ProtoReflect().IsValid() || hasUnknownSnapshotFields(value.ProtoReflect()) {
		return nil, nil, ErrSnapshotCorrupt
	}
	if proto.Size(value) > spec.maxBytes {
		return nil, nil, ErrSnapshotBound
	}
	retained := proto.Clone(value).(T)
	claimed := bytes.Clone(spec.getHash(retained))
	spec.setHash(retained, nil)
	unsigned, err := (proto.MarshalOptions{Deterministic: true}).Marshal(retained)
	if err != nil {
		return nil, nil, ErrSnapshotCorrupt
	}
	digest := sha256.Sum256(unsigned)
	if len(claimed) != 0 && (len(claimed) != sha256.Size || subtle.ConstantTimeCompare(claimed, digest[:]) != 1) {
		return nil, nil, ErrSnapshotCorrupt
	}
	spec.setHash(retained, bytes.Clone(digest[:]))
	if err := protovalidate.Validate(retained); err != nil {
		return nil, nil, ErrSnapshotCorrupt
	}
	if spec.bind != nil && !spec.bind(retained) {
		return nil, nil, ErrSnapshotCorrupt
	}
	id, org, revision := spec.identity(retained)
	if !ids.IsCanonicalV7(id) || !ids.IsCanonicalV7(org) || revision == 0 || revision > math.MaxInt64 {
		return nil, nil, ErrSnapshotCorrupt
	}
	wire, err := (proto.MarshalOptions{Deterministic: true}).Marshal(retained)
	if err != nil {
		return nil, nil, ErrSnapshotCorrupt
	}
	if len(wire) > spec.maxBytes {
		return nil, nil, ErrSnapshotBound
	}
	return wire, bytes.Clone(digest[:]), nil
}

func openSnapshot[T proto.Message](wire []byte, organisation string, exact *tammyv1.SourceRef, spec snapshotSpec[T]) (T, error) {
	var absent T
	if len(wire) > spec.maxBytes {
		return absent, ErrSnapshotBound
	}
	if len(wire) == 0 || !ids.IsCanonicalV7(organisation) || exact == nil || exact.Type != spec.resource || !ids.IsCanonicalV7(exact.Id) || exact.Revision == 0 || exact.Revision > math.MaxInt64 || len(exact.ContentHash) != sha256.Size {
		return absent, ErrSnapshotCorrupt
	}
	value := spec.newMessage()
	if proto.Unmarshal(wire, value) != nil || hasUnknownSnapshotFields(value.ProtoReflect()) || protovalidate.Validate(value) != nil {
		return absent, ErrSnapshotCorrupt
	}
	if spec.bind != nil && !spec.bind(value) {
		return absent, ErrSnapshotCorrupt
	}
	canonical, err := (proto.MarshalOptions{Deterministic: true}).Marshal(value)
	if err != nil || !bytes.Equal(wire, canonical) {
		return absent, ErrSnapshotCorrupt
	}
	id, org, revision := spec.identity(value)
	if id != exact.Id || org != organisation || revision != exact.Revision {
		return absent, ErrSnapshotCorrupt
	}
	claimed := bytes.Clone(spec.getHash(value))
	unsigned := proto.Clone(value).(T)
	spec.setHash(unsigned, nil)
	body, err := (proto.MarshalOptions{Deterministic: true}).Marshal(unsigned)
	if err != nil {
		return absent, ErrSnapshotCorrupt
	}
	digest := sha256.Sum256(body)
	if (!spec.externalHash && (len(claimed) != sha256.Size || subtle.ConstantTimeCompare(claimed, digest[:]) != 1)) || subtle.ConstantTimeCompare(exact.ContentHash, digest[:]) != 1 {
		return absent, ErrSnapshotCorrupt
	}
	return value, nil
}

func hasUnknownSnapshotFields(message protoreflect.Message) bool {
	if len(message.GetUnknown()) != 0 {
		return true
	}
	found := false
	message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		switch {
		case field.IsMap():
			if field.MapValue().Kind() == protoreflect.MessageKind {
				value.Map().Range(func(_ protoreflect.MapKey, child protoreflect.Value) bool {
					found = hasUnknownSnapshotFields(child.Message())
					return !found
				})
			}
		case field.IsList() && field.Kind() == protoreflect.MessageKind:
			list := value.List()
			for index := 0; index < list.Len() && !found; index++ {
				found = hasUnknownSnapshotFields(list.Get(index).Message())
			}
		case field.Kind() == protoreflect.MessageKind:
			found = hasUnknownSnapshotFields(value.Message())
		}
		return !found
	})
	return found
}

var profileSnapshotSpec = snapshotSpec[*tammyv1.BasObligationsProfile]{
	resource: "bas_obligations_profile", maxBytes: scalarSnapshotLimit,
	newMessage: func() *tammyv1.BasObligationsProfile { return &tammyv1.BasObligationsProfile{} },
	identity: func(value *tammyv1.BasObligationsProfile) (string, string, uint64) {
		return value.ProfileId, value.OrganisationId, value.Revision
	},
	getHash: func(value *tammyv1.BasObligationsProfile) []byte { return value.ContentHash },
	setHash: func(value *tammyv1.BasObligationsProfile, hash []byte) { value.ContentHash = hash },
}

func sealProfile(value *tammyv1.BasObligationsProfile) ([]byte, []byte, error) {
	return sealSnapshot(value, profileSnapshotSpec)
}
func openProfile(wire []byte, org string, exact *tammyv1.SourceRef) (*tammyv1.BasObligationsProfile, error) {
	return openSnapshot(wire, org, exact, profileSnapshotSpec)
}

var preparationSnapshotSpec = snapshotSpec[*tammyv1.BasPreparation]{
	resource: "bas_preparation", maxBytes: scalarSnapshotLimit,
	newMessage: func() *tammyv1.BasPreparation { return &tammyv1.BasPreparation{} },
	identity: func(value *tammyv1.BasPreparation) (string, string, uint64) {
		return value.PreparationId, value.OrganisationId, value.Version
	},
	getHash: func(value *tammyv1.BasPreparation) []byte { return value.ContentHash },
	setHash: func(value *tammyv1.BasPreparation, hash []byte) { value.ContentHash = hash },
}

func sealPreparation(value *tammyv1.BasPreparation) ([]byte, []byte, error) {
	return sealSnapshot(value, preparationSnapshotSpec)
}
func openPreparation(wire []byte, org string, exact *tammyv1.SourceRef) (*tammyv1.BasPreparation, error) {
	return openSnapshot(wire, org, exact, preparationSnapshotSpec)
}

var frozenSnapshotSpec = snapshotSpec[*tammyv1.BasFrozenBas]{
	resource: "bas_frozen", maxBytes: frozenSnapshotLimit,
	newMessage: func() *tammyv1.BasFrozenBas { return &tammyv1.BasFrozenBas{} },
	identity: func(value *tammyv1.BasFrozenBas) (string, string, uint64) {
		return value.FrozenId, value.OrganisationId, 1
	},
	getHash: func(value *tammyv1.BasFrozenBas) []byte { return value.ContentHash },
	setHash: func(value *tammyv1.BasFrozenBas, hash []byte) { value.ContentHash = hash },
}

func sealFrozen(value *tammyv1.BasFrozenBas) ([]byte, []byte, error) {
	return sealSnapshot(value, frozenSnapshotSpec)
}
func openFrozen(wire []byte, org string, exact *tammyv1.SourceRef) (*tammyv1.BasFrozenBas, error) {
	return openSnapshot(wire, org, exact, frozenSnapshotSpec)
}

var periodSnapshotSpec = snapshotSpec[*tammyv1.BasConfirmedPeriod]{
	resource: "bas_period", maxBytes: scalarSnapshotLimit,
	newMessage: func() *tammyv1.BasConfirmedPeriod { return &tammyv1.BasConfirmedPeriod{} },
	identity: func(value *tammyv1.BasConfirmedPeriod) (string, string, uint64) {
		return value.PeriodId, value.OrganisationId, value.Version
	},
	getHash: func(value *tammyv1.BasConfirmedPeriod) []byte { return value.ContentHash },
	setHash: func(value *tammyv1.BasConfirmedPeriod, hash []byte) { value.ContentHash = hash },
}

func sealPeriod(value *tammyv1.BasConfirmedPeriod) ([]byte, []byte, error) {
	return sealSnapshot(value, periodSnapshotSpec)
}
func openPeriod(wire []byte, org string, exact *tammyv1.SourceRef) (*tammyv1.BasConfirmedPeriod, error) {
	return openSnapshot(wire, org, exact, periodSnapshotSpec)
}

var calculationSnapshotSpec = snapshotSpec[*tammyv1.BasCalculation]{
	resource: "bas_calculation", maxBytes: calculationSnapshotLimit,
	newMessage: func() *tammyv1.BasCalculation { return &tammyv1.BasCalculation{} },
	identity: func(value *tammyv1.BasCalculation) (string, string, uint64) {
		return value.CalculationId, value.OrganisationId, value.Version
	},
	getHash: func(value *tammyv1.BasCalculation) []byte { return value.ContentHash },
	setHash: func(value *tammyv1.BasCalculation, hash []byte) { value.ContentHash = hash },
}

func sealCalculation(value *tammyv1.BasCalculation) ([]byte, []byte, error) {
	return sealSnapshot(value, calculationSnapshotSpec)
}
func openCalculation(wire []byte, org string, exact *tammyv1.SourceRef) (*tammyv1.BasCalculation, error) {
	return openSnapshot(wire, org, exact, calculationSnapshotSpec)
}

var observationSnapshotSpec = snapshotSpec[*tammyv1.BasExternalObservation]{
	resource: "bas_external_observation", maxBytes: 128 * 1024,
	newMessage: func() *tammyv1.BasExternalObservation { return &tammyv1.BasExternalObservation{} },
	identity: func(value *tammyv1.BasExternalObservation) (string, string, uint64) {
		return value.ObservationId, value.OrganisationId, 1
	},
	getHash: func(value *tammyv1.BasExternalObservation) []byte { return value.ContentHash },
	setHash: func(value *tammyv1.BasExternalObservation, hash []byte) { value.ContentHash = hash },
}

func sealObservation(value *tammyv1.BasExternalObservation) ([]byte, []byte, error) {
	return sealSnapshot(value, observationSnapshotSpec)
}
func openObservation(wire []byte, org string, exact *tammyv1.SourceRef) (*tammyv1.BasExternalObservation, error) {
	return openSnapshot(wire, org, exact, observationSnapshotSpec)
}

var familyIdentitySnapshotSpec = snapshotSpec[*tammyv1.BasFamilyIdentity]{
	resource: "bas_family", maxBytes: scalarSnapshotLimit,
	newMessage: func() *tammyv1.BasFamilyIdentity { return &tammyv1.BasFamilyIdentity{} },
	identity: func(value *tammyv1.BasFamilyIdentity) (string, string, uint64) {
		return value.FamilyId, value.OrganisationId, 1
	},
	getHash: func(value *tammyv1.BasFamilyIdentity) []byte { return value.ContentHash },
	setHash: func(value *tammyv1.BasFamilyIdentity, hash []byte) { value.ContentHash = hash },
}

func sealFamilyIdentity(value *tammyv1.BasFamilyIdentity) ([]byte, []byte, error) {
	return sealSnapshot(value, familyIdentitySnapshotSpec)
}
func openFamilyIdentity(wire []byte, org string, exact *tammyv1.SourceRef) (*tammyv1.BasFamilyIdentity, error) {
	return openSnapshot(wire, org, exact, familyIdentitySnapshotSpec)
}

// The repository supplies an independently verified immutable family identity.
// A workflow body cannot claim its own organisation through a caller string.
func familyStateSnapshotSpec(identity *tammyv1.BasFamilyIdentity) snapshotSpec[*tammyv1.BasFamilyState] {
	return snapshotSpec[*tammyv1.BasFamilyState]{
		resource: "bas_family_state", maxBytes: scalarSnapshotLimit,
		newMessage: func() *tammyv1.BasFamilyState { return &tammyv1.BasFamilyState{} },
		identity: func(value *tammyv1.BasFamilyState) (string, string, uint64) {
			return value.GetFamily().GetId(), identity.GetOrganisationId(), value.Version
		},
		getHash: func(value *tammyv1.BasFamilyState) []byte { return value.ContentHash },
		setHash: func(value *tammyv1.BasFamilyState, hash []byte) { value.ContentHash = hash },
		bind: func(value *tammyv1.BasFamilyState) bool {
			if identity == nil || len(identity.ContentHash) != sha256.Size {
				return false
			}
			_, hash, err := sealFamilyIdentity(identity)
			return err == nil && sameRef(value.Family, &tammyv1.SourceRef{Type: "bas_family", Id: identity.FamilyId, Revision: 1, ContentHash: hash})
		},
	}
}

func sealFamilyState(value *tammyv1.BasFamilyState, identity *tammyv1.BasFamilyIdentity) ([]byte, []byte, error) {
	return sealSnapshot(value, familyStateSnapshotSpec(identity))
}
func openFamilyState(wire []byte, org string, exact *tammyv1.SourceRef, identity *tammyv1.BasFamilyIdentity) (*tammyv1.BasFamilyState, error) {
	return openSnapshot(wire, org, exact, familyStateSnapshotSpec(identity))
}

var branchIdentitySnapshotSpec = snapshotSpec[*tammyv1.BasBranchIdentity]{
	resource: "bas_branch", maxBytes: scalarSnapshotLimit,
	newMessage: func() *tammyv1.BasBranchIdentity { return &tammyv1.BasBranchIdentity{} },
	identity: func(value *tammyv1.BasBranchIdentity) (string, string, uint64) {
		return value.BranchId, value.OrganisationId, 1
	},
	getHash: func(value *tammyv1.BasBranchIdentity) []byte { return value.ContentHash },
	setHash: func(value *tammyv1.BasBranchIdentity, hash []byte) { value.ContentHash = hash },
}

func sealBranchIdentity(value *tammyv1.BasBranchIdentity) ([]byte, []byte, error) {
	return sealSnapshot(value, branchIdentitySnapshotSpec)
}
func openBranchIdentity(wire []byte, org string, exact *tammyv1.SourceRef) (*tammyv1.BasBranchIdentity, error) {
	return openSnapshot(wire, org, exact, branchIdentitySnapshotSpec)
}

func branchStateSnapshotSpec(identity *tammyv1.BasBranchIdentity) snapshotSpec[*tammyv1.BasBranchState] {
	return snapshotSpec[*tammyv1.BasBranchState]{
		resource: "bas_branch_state", maxBytes: scalarSnapshotLimit,
		newMessage: func() *tammyv1.BasBranchState { return &tammyv1.BasBranchState{} },
		identity: func(value *tammyv1.BasBranchState) (string, string, uint64) {
			return value.GetBranchIdentity().GetId(), identity.GetOrganisationId(), value.Version
		},
		getHash: func(value *tammyv1.BasBranchState) []byte { return value.ContentHash },
		setHash: func(value *tammyv1.BasBranchState, hash []byte) { value.ContentHash = hash },
		bind: func(value *tammyv1.BasBranchState) bool {
			if identity == nil || len(identity.ContentHash) != sha256.Size {
				return false
			}
			_, hash, err := sealBranchIdentity(identity)
			return err == nil && sameRef(value.BranchIdentity, &tammyv1.SourceRef{Type: "bas_branch", Id: identity.BranchId, Revision: 1, ContentHash: hash})
		},
	}
}

func sealBranchState(value *tammyv1.BasBranchState, identity *tammyv1.BasBranchIdentity) ([]byte, []byte, error) {
	return sealSnapshot(value, branchStateSnapshotSpec(identity))
}
func openBranchState(wire []byte, org string, exact *tammyv1.SourceRef, identity *tammyv1.BasBranchIdentity) (*tammyv1.BasBranchState, error) {
	return openSnapshot(wire, org, exact, branchStateSnapshotSpec(identity))
}

var savedPaygSnapshotSpec = snapshotSpec[*tammyv1.BasSavedPaygInputs]{
	resource: "bas_saved_payg_inputs", maxBytes: 2 * 1024 * 1024,
	newMessage: func() *tammyv1.BasSavedPaygInputs { return &tammyv1.BasSavedPaygInputs{} },
	identity: func(value *tammyv1.BasSavedPaygInputs) (string, string, uint64) {
		return value.InputSnapshotId, value.OrganisationId, value.InputRevision
	},
	getHash: func(value *tammyv1.BasSavedPaygInputs) []byte { return value.ContentHash },
	setHash: func(value *tammyv1.BasSavedPaygInputs, hash []byte) { value.ContentHash = hash },
	bind: func(value *tammyv1.BasSavedPaygInputs) bool {
		hash, err := hashPaygInputs(value.PaygwInputs, value.PaygiInputs)
		return err == nil && bytes.Equal(value.InputHash, hash)
	},
}

func sealSavedPaygInputs(value *tammyv1.BasSavedPaygInputs) ([]byte, []byte, error) {
	return sealSnapshot(value, savedPaygSnapshotSpec)
}
func openSavedPaygInputs(wire []byte, org string, exact *tammyv1.SourceRef) (*tammyv1.BasSavedPaygInputs, error) {
	return openSnapshot(wire, org, exact, savedPaygSnapshotSpec)
}

func hashPaygInputs(paygw *tammyv1.BasPaygwInputs, paygi *tammyv1.BasPaygiInputs) ([]byte, error) {
	if paygw == nil || paygi == nil {
		return nil, ErrSnapshotCorrupt
	}
	digest := sha256.New()
	_, _ = digest.Write([]byte("tammy.bas.payg-inputs.v1\x00"))
	for _, value := range []proto.Message{paygw, paygi} {
		if hasUnknownSnapshotFields(value.ProtoReflect()) || protovalidate.Validate(value) != nil {
			return nil, ErrSnapshotCorrupt
		}
		if proto.Size(value) > 1024*1024 {
			return nil, ErrSnapshotBound
		}
		wire, err := (proto.MarshalOptions{Deterministic: true}).Marshal(value)
		if err != nil {
			return nil, ErrSnapshotCorrupt
		}
		_, _ = digest.Write(binary.BigEndian.AppendUint64(nil, uint64(len(wire))))
		_, _ = digest.Write(wire)
	}
	return digest.Sum(nil), nil
}
