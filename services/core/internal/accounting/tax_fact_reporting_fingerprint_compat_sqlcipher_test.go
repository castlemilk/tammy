//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package accounting_test

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestTaxFactReportingPreBudgetFingerprintCompatibility(t *testing.T) {
	fixture := newTaxFactReportingFixture(t)
	fixture.post(t, 16001, taxFactReportingID(16005), bytes.Repeat([]byte{0x31}, 32), bytes.Repeat([]byte{0x42}, 32))
	seedTaxFactReportingDraft(t, fixture.tx, fixture.accounts, fixture.now,
		taxFactReportingID(16011), "SYSTEM", 0, "compat-null-source", taxFactReportingID(16012),
		taxFactReportingID(16013), "compat-null-evidence", "ADJUSTMENT", nil)
	capture, err := fixture.repository.CaptureTaxFactsForReporting(fixture.ctx, accountTestOrganisationID)
	if err != nil || capture == nil || len(capture.Facts) != 2 {
		t.Fatalf("legacy fingerprint fixture = %#v, %v", capture, err)
	}
	// Pinned against the original reader before the additive budget APIs.
	if got := hex.EncodeToString(capture.Fingerprint); got != "16190bc61821278bb8d91593ecf34c12194f2e96c14eb1dfd3dafc2d3a8eef7a" {
		t.Fatalf("legacy fingerprint changed: %s", got)
	}
}
