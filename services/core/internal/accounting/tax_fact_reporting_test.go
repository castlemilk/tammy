package accounting

import (
	"context"
	"errors"
	"testing"
)

func TestTaxFactReportingFingerprintHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fingerprint, err := taxFactReportingFingerprint(ctx, "organisation", nil, nil)
	if !errors.Is(err, context.Canceled) || fingerprint != nil {
		t.Fatalf("canceled fingerprint = %x, %v; want canceled error and nil output", fingerprint, err)
	}
}
