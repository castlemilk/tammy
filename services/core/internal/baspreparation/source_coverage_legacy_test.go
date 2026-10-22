package baspreparation

import (
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
)

func TestBASLegacyCoverageNeverUsesCandidateGSTAsEligibleClaim(t *testing.T) {
	clear := tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR
	unresolved := tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNRESOLVED_LEGACY
	unsupported := tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNSUPPORTED
	positiveDiagnostic := int64(1000)

	tests := []struct {
		name       string
		unit       purchases.BASPurchaseFinancialUnit
		want       tammyv1.BasCoverageDisposition
		wantCode   string
		mustReject bool // impossible clear/non-excluded positive-GST adapter state
	}{
		{
			name: "no applicable review keeps positive diagnostic unresolved",
			unit: purchases.BASPurchaseFinancialUnit{Status: unresolved, CandidateGST: positiveDiagnostic},
			want: tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_UNRESOLVED,
			wantCode: "LEGACY_REVIEW_REQUIRED",
		},
		{
			name: "stale or contradictory review remains unresolved",
			unit: purchases.BASPurchaseFinancialUnit{Status: unresolved, CandidateGST: positiveDiagnostic},
			want: tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_UNRESOLVED,
			wantCode: "LEGACY_REVIEW_REQUIRED",
		},
		{
			name: "known current payment remains unsupported",
			unit: purchases.BASPurchaseFinancialUnit{Status: unsupported, CandidateGST: positiveDiagnostic},
			want: tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_UNSUPPORTED,
			wantCode: "LEGACY_SETTLEMENT_UNSUPPORTED",
		},
		{
			name: "explicit unsupported settlement remains unsupported",
			unit: purchases.BASPurchaseFinancialUnit{Status: unsupported, CandidateGST: positiveDiagnostic},
			want: tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_UNSUPPORTED,
			wantCode: "LEGACY_SETTLEMENT_UNSUPPORTED",
		},
		{
			name: "confirmed unpaid positive diagnostic is excluded",
			unit: purchases.BASPurchaseFinancialUnit{Status: clear, Excluded: true, CandidateGST: positiveDiagnostic},
			want: tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_EXCLUDED_WITH_REASON,
			wantCode: "LEGACY_EXCLUDED_BY_RETAINED_DECISION",
		},
		{
			name: "corroborated already claimed positive diagnostic is excluded",
			unit: purchases.BASPurchaseFinancialUnit{Status: clear, Excluded: true, CandidateGST: positiveDiagnostic},
			want: tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_EXCLUDED_WITH_REASON,
			wantCode: "LEGACY_EXCLUDED_BY_RETAINED_DECISION",
		},
		{
			name: "noncreditable positive diagnostic is excluded",
			unit: purchases.BASPurchaseFinancialUnit{Status: clear, Excluded: true, CandidateGST: positiveDiagnostic},
			want: tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_EXCLUDED_WITH_REASON,
			wantCode: "LEGACY_EXCLUDED_BY_RETAINED_DECISION",
		},
		{
			name: "duplicate positive diagnostic is excluded",
			unit: purchases.BASPurchaseFinancialUnit{Status: clear, Excluded: true, CandidateGST: positiveDiagnostic},
			want: tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_EXCLUDED_WITH_REASON,
			wantCode: "LEGACY_EXCLUDED_BY_RETAINED_DECISION",
		},
		{
			name: "evidence-only positive diagnostic is excluded",
			unit: purchases.BASPurchaseFinancialUnit{Status: clear, EvidenceOnly: true, CandidateGST: positiveDiagnostic},
			want: tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_EXCLUDED_WITH_REASON,
			wantCode: "LEGACY_EXCLUDED_BY_RETAINED_DECISION",
		},
		{
			name: "clear non-excluded positive diagnostic cannot become a claim",
			unit: purchases.BASPurchaseFinancialUnit{Status: clear, CandidateGST: positiveDiagnostic},
			mustReject: true,
		},
		{
			name: "clear zero candidate stays reviewed zero",
			unit: purchases.BASPurchaseFinancialUnit{Status: clear},
			want: tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_REVIEWED_ZERO,
			wantCode: "LEGACY_REVIEWED_ZERO",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, code, _ := basCoverageLegacyDisposition(test.unit)
				if test.mustReject {
					// No real mature mapper path emits this combination: all positive
					// legacy candidates are unresolved, unsupported, or explicitly
					// excluded. Fail closed if that invariant is ever broken.
					if got == tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_ELIGIBLE ||
						code == "LEGACY_RETAINED_GST" {
						t.Fatalf("diagnostic CandidateGST was promoted to eligible BAS GST: disposition=%s code=%q", got, code)
					}
					return
				}
			if got != test.want || code != test.wantCode {
				t.Fatalf("legacy disposition=%s code=%q; want %s code=%q", got, code, test.want, test.wantCode)
			}
			if got == tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_ELIGIBLE {
				t.Fatalf("legacy diagnostic CandidateGST must never be an eligible source-line claim")
			}
		})
	}
}
