package purchases

import (
	"crypto/sha256"
	"embed"
	"encoding/binary"
)

//go:embed reporting_financial.go claim_projection.go claim_timing.go
var basFinancialImplementation embed.FS

// BASFinancialImplementationFingerprint binds the source-to-label purchase
// projection and the timing rules it reuses to the retained BAS calculator.
func BASFinancialImplementationFingerprint() ([]byte, error) {
	h := sha256.New()
	for _, name := range []string{"reporting_financial.go", "claim_projection.go", "claim_timing.go"} {
		body, err := basFinancialImplementation.ReadFile(name)
		if err != nil {
			return nil, err
		}
		for _, frame := range [][]byte{[]byte(name), body} {
			var size [8]byte
			binary.BigEndian.PutUint64(size[:], uint64(len(frame)))
			_, _ = h.Write(size[:])
			_, _ = h.Write(frame)
		}
	}
	return h.Sum(nil), nil
}
