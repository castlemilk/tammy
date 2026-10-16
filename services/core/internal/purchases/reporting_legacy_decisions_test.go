package purchases

import (
	"context"
	"errors"
	"testing"
)

func TestPurchaseBASLegacyDecisionsNilRepositoryReturnsNoResult(t *testing.T) {
	const org = "018f0000-0000-7000-8000-000000001001"
	var repository *Repository
	summary, err := repository.SummarizeBASLegacyDecisions(context.Background(), org)
	if summary != nil || !errors.Is(err, ErrRepository) {
		t.Fatalf("nil repository summary=%v err=%v, want no result and ErrRepository", summary, err)
	}
	read, err := repository.ReadBASLegacyDecisions(context.Background(), org, BASLegacyDecisionSummary{OrganisationID: org}, 0, 0)
	if read != nil || !errors.Is(err, ErrRepository) {
		t.Fatalf("nil repository decision read=%v err=%v, want no result and ErrRepository", read, err)
	}
}
