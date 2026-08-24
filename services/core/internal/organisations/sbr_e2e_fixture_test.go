//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package organisations

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPackagedSBRReadinessSyntheticPDFIsBoundedAndAccepted(t *testing.T) {
	fixture := filepath.Join(
		"..", "..", "..", "..", "apps", "desktop", "tests", "e2e", "assets", "synthetic-abr-evidence.pdf",
	)
	content, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("read packaged SBR readiness evidence: %v", err)
	}
	if len(content) == 0 || len(content) > MaxVerificationEvidenceBytes {
		t.Fatalf("packaged SBR readiness evidence has invalid size %d", len(content))
	}
	if !validPDF(content) {
		t.Fatal("packaged SBR readiness evidence is not accepted by the production PDF validator")
	}
}
