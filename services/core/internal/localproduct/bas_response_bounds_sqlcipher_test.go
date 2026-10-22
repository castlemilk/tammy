//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestBASQueryFrameFitsStoredFrozenShapeAboveLegacyFourMiB(t *testing.T) {
	calculation := nearLimitBASResponseCalculation(t)
	if size := proto.Size(calculation); size > 4*1024*1024 || size < 4*1024*1024-36*1024 {
		t.Fatalf("calculation fixture must fit the retained 4 MiB body cap near its boundary: %d", size)
	}

	frozen := &tammyv1.BasFrozenBas{
		CalculationSnapshot: calculation,
		Acknowledgements:    make([]*tammyv1.BasWarningAcknowledgement, 200),
	}
	for index := range frozen.Acknowledgements {
		frozen.Acknowledgements[index] = basResponseBoundAcknowledgement(index)
	}
	if size := proto.Size(frozen); size > 8*1024*1024 || size <= 4*1024*1024 {
		t.Fatalf("frozen fixture must fit the retained 8 MiB body cap and exceed the old 4 MiB response cap: %d", size)
	}
	response := &tammyv1.GetFrozenBasResponse{FrozenBas: frozen}
	frame, err := anypb.New(response)
	if err != nil {
		t.Fatal(err)
	}
	frameBytes := proto.Size(frame)
	if frameBytes <= 4*1024*1024 || frameBytes > 16*1024*1024 {
		t.Fatalf("wrapped response must exceed the old limit and fit the new finite ceiling: %d", frameBytes)
	}
	if !basQueryFrameFits(response) {
		t.Fatalf("stored-shape response of %d Any bytes was rejected", frameBytes)
	}
}

func TestBASQueryFrameFitsRejectsAboveSixteenMiB(t *testing.T) {
	calculation := basQuerySizedCalculation(17 * 1024)
	if size := proto.Size(calculation); size <= 16*1024*1024 {
		t.Fatalf("oversize BAS body fixture must exceed the 16 MiB transport ceiling: %d", size)
	}
	if basQueryFrameFits(&tammyv1.GetBasCalculationResponse{Calculation: calculation}) {
		t.Fatal("oversize exact calculation response unexpectedly fit the finite BAS query ceiling")
	}
}

func nearLimitBASResponseCalculation(t *testing.T) *tammyv1.BasCalculation {
	t.Helper()
	const (
		retainedCalculationLimit = 4 * 1024 * 1024
		candidateCount           = 1000
		target                   = retainedCalculationLimit - 32*1024
	)
	value := basQuerySizedCalculation(4096)
	if len(value.Coverage) != candidateCount {
		t.Fatalf("coverage fixture has %d candidates, want %d", len(value.Coverage), candidateCount)
	}
	idBytes := 4096
	for {
		id := strings.Repeat("x", idBytes)
		for _, candidate := range value.Coverage {
			candidate.RawSource.Id = id
		}
		size := proto.Size(value)
		if size <= target {
			if size < target-2*1024 {
				t.Fatalf("bounded raw-ID fixture did not reach the storage boundary: got %d, target %d", size, target)
			}
			return value
		}
		step := (size - target + candidateCount - 1) / candidateCount
		if step < 1 {
			step = 1
		}
		idBytes -= step
		if idBytes <= 0 {
			t.Fatal("could not fit large raw-identity fixture beneath the retained calculation limit")
		}
	}
}

func basQuerySizedCalculation(rawIDBytes int) *tammyv1.BasCalculation {
	const candidateCount = 1000
	value := &tammyv1.BasCalculation{Coverage: make([]*tammyv1.BasSourceCoverage, candidateCount)}
	for index := range value.Coverage {
		value.Coverage[index] = &tammyv1.BasSourceCoverage{
			CandidateKind:      tammyv1.BasCoverageCandidateKind_BAS_COVERAGE_CANDIDATE_KIND_MANUAL_TAX_FACT,
			Disposition:        tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_UNSUPPORTED,
			ClassificationCode: "LEGACY_UNSUPPORTED",
			Reason:             strings.Repeat("r", 512),
			RawSource: &tammyv1.BasRawSourceRef{
				Type:              "legacy_manual_tax_fact",
				Id:                strings.Repeat("x", rawIDBytes),
				ComputedProofHash: make([]byte, 32),
				ProofScope:        tammyv1.BasSourceProofScope_BAS_SOURCE_PROOF_SCOPE_SOURCE_ROW_PROJECTION,
			},
		}
	}
	return value
}

func basResponseBoundAcknowledgement(index int) *tammyv1.BasWarningAcknowledgement {
	id := func(value int) string { return fmt.Sprintf("01900000-0000-7000-8000-%012x", value+1) }
	ref := func(sourceType string, value int) *tammyv1.SourceRef {
		return &tammyv1.SourceRef{Type: sourceType, Id: id(value), Revision: 1, ContentHash: make([]byte, 32)}
	}
	return &tammyv1.BasWarningAcknowledgement{
		AcknowledgementId:  id(1000 + index),
		Validation:         ref("bas_validation", 2000+index),
		Finding:            ref("bas_validation_finding", 3000+index),
		SourceSnapshotHash: make([]byte, 32),
		ActorUserId:        id(4000 + index),
		AcknowledgedAt:     timestamppb.New(time.Unix(1, 0).UTC()),
	}
}
