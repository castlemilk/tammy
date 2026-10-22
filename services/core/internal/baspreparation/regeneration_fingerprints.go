package baspreparation

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"sort"

	"github.com/tammyapp/tammy/services/core/internal/artefacts"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/migrations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const (
	basFingerprintSchemaVersion     = "embedded-sqlcipher-migration-set-v1"
	basFingerprintDescriptorVersion = "transitive-compiled-proto-file-descriptors-v1"
)

// Keep this list finite and explicit; additions to the converter/calculator
// must update it and the fingerprint regression test.
//
//go:embed calculation.go calendar.go calculation_proto_adapter.go invoice_financial_mapper.go source_coverage.go regeneration_workpaper.go
var basFingerprintSources embed.FS

type basRegenerationFingerprints struct {
	ruleVersion, calculatorVersion, formVersion, schemaVersion, descriptorVersion string
	ruleHash, calculatorHash, formHash, schemaHash, descriptorHash                []byte
}

func computeBASRegenerationFingerprints(rule *artefacts.RuleBundleReportingRecord) (basRegenerationFingerprints, error) {
	if rule == nil || rule.Bundle.Source == nil || rule.Bundle.Version == "" ||
		len(rule.Bundle.Source.ContentHash) != sha256.Size || len(rule.ContentHash) != sha256.Size ||
		len(rule.Bundle.RetainedBytes) == 0 {
		return basRegenerationFingerprints{}, ErrSourceReservation
	}
	actual := sha256.Sum256(rule.Bundle.RetainedBytes)
	if !bytes.Equal(actual[:], rule.Bundle.Source.ContentHash) {
		return basRegenerationFingerprints{}, ErrSourceReservation
	}

	var implementation [][]byte
	for _, path := range []string{"calculation.go", "calendar.go", "calculation_proto_adapter.go", "invoice_financial_mapper.go", "source_coverage.go", "regeneration_workpaper.go"} {
		body, err := basFingerprintSources.ReadFile(path)
		if err != nil {
			return basRegenerationFingerprints{}, errors.Join(ErrSourceReservation, err)
		}
		implementation = append(implementation, []byte(path), body)
	}
	purchaseHash, err := purchases.BASFinancialImplementationFingerprint()
	if err != nil {
		return basRegenerationFingerprints{}, errors.Join(ErrSourceReservation, err)
	}
	implementation = append(implementation, []byte("purchases/source-to-label-and-timing"), purchaseHash)
	calculatorHash := basTaggedFingerprint("tammy/bas/calculator-implementation/v1", implementation...)
	formHash := basTaggedFingerprint("tammy/bas/form-equations/v1", implementation...)

	ruleHash := basTaggedFingerprint("tammy/bas/retained-rule-and-catalogue/v1",
		rule.Bundle.Source.ContentHash, rule.ContentHash)
	steps, err := migrations.All()
	if err != nil || len(steps) == 0 {
		return basRegenerationFingerprints{}, errors.Join(ErrSourceReservation, err)
	}
	schemaHash := sha256.New()
	basFingerprintFrame(schemaHash, []byte("tammy/bas/compiled-schema-migrations/v1"))
	for _, step := range steps {
		var version [4]byte
		binary.BigEndian.PutUint32(version[:], step.Version)
		basFingerprintFrame(schemaHash, version[:])
		basFingerprintFrame(schemaHash, []byte(step.Name))
		basFingerprintFrame(schemaHash, []byte(step.SHA256))
		basFingerprintFrame(schemaHash, step.SQL)
	}
	descriptorHash, err := basBASDescriptorFingerprint()
	if err != nil {
		return basRegenerationFingerprints{}, errors.Join(ErrSourceReservation, err)
	}
	return basRegenerationFingerprints{
		ruleVersion: rule.Bundle.Version, ruleHash: ruleHash,
		calculatorVersion: CalculatorVersion, calculatorHash: calculatorHash,
		formVersion: FormVersion, formHash: formHash,
		schemaVersion:     fmt.Sprintf("%s-%d", basFingerprintSchemaVersion, steps[len(steps)-1].Version),
		schemaHash:        schemaHash.Sum(nil),
		descriptorVersion: basFingerprintDescriptorVersion, descriptorHash: descriptorHash,
	}, nil
}

func basBASDescriptorFingerprint() ([]byte, error) {
	// Hash every file descriptor transitively referenced by each retained BAS
	// source/result message, not a version string or the top-level message name.
	roots := []proto.Message{
		&tammyv1.BasCalculation{}, &tammyv1.BasCalculationResult{}, &tammyv1.BasLabelTrace{},
		&tammyv1.BasSourceCoverage{}, &tammyv1.BasSourceRevisionVector{}, &tammyv1.BasPreparation{},
		&tammyv1.BasWorkpaper{}, &tammyv1.BasSavedPaygInputs{}, &tammyv1.BasConfirmedPeriod{},
		&tammyv1.BasObligationsProfile{}, &tammyv1.BasFamilyIdentity{}, &tammyv1.BasFamilyState{},
		&tammyv1.Organisation{},
	}
	files := make(map[string]protoreflect.FileDescriptor)
	var visit func(protoreflect.FileDescriptor) error
	visit = func(file protoreflect.FileDescriptor) error {
		if file == nil {
			return ErrSnapshotCorrupt
		}
		name := string(file.Path())
		if _, ok := files[name]; ok {
			return nil
		}
		files[name] = file
		for index := 0; index < file.Imports().Len(); index++ {
			if err := visit(file.Imports().Get(index).FileDescriptor); err != nil {
				return err
			}
		}
		return nil
	}
	for _, root := range roots {
		if root == nil || !root.ProtoReflect().IsValid() {
			return nil, ErrSnapshotCorrupt
		}
		if err := visit(root.ProtoReflect().Descriptor().ParentFile()); err != nil {
			return nil, err
		}
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	h := sha256.New()
	basFingerprintFrame(h, []byte("tammy/bas/proto-descriptors/v1"))
	for _, name := range names {
		encoded, err := (proto.MarshalOptions{Deterministic: true}).Marshal(protodesc.ToFileDescriptorProto(files[name]))
		if err != nil {
			return nil, err
		}
		basFingerprintFrame(h, []byte(name))
		basFingerprintFrame(h, encoded)
	}
	return h.Sum(nil), nil
}

func basTaggedFingerprint(tag string, frames ...[]byte) []byte {
	h := sha256.New()
	basFingerprintFrame(h, []byte(tag))
	for _, frame := range frames {
		basFingerprintFrame(h, frame)
	}
	return h.Sum(nil)
}

func basFingerprintFrame(h hash.Hash, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = h.Write(length[:])
	_, _ = h.Write(value)
}

// Test contract:
//  - same actual retained rule bytes + catalogue => same hash; changing either
//    actual bytes changes hash even with an unchanged Version string;
//  - calculator/form hashes change with the embedded code/material; schema
//    hash covers every ordered `migrations.All()` SQL body; descriptor hash
//    covers the transitively imported compiled proto file descriptors;
//  - all five hashes are 32 bytes and none is computed from a label/version
//    string alone.
