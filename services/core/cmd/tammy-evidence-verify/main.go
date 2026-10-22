// Command tammy-evidence-verify verifies a signed Tammy audit evidence ZIP
// without opening a workspace database or requiring workspace secrets.
package main

import (
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"github.com/tammyapp/tammy/services/core/internal/audit"
)

const maxStandaloneArchiveBytes = 512 << 20

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) != 1 {
		_, _ = fmt.Fprintln(stderr, "usage: tammy-evidence-verify <evidence.zip>")
		return 2
	}
	file, err := os.Open(arguments[0])
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "INVALID: unable to read archive")
		return 1
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxStandaloneArchiveBytes {
		_, _ = fmt.Fprintln(stderr, "INVALID: archive size or type is not allowed")
		return 1
	}
	archive, err := io.ReadAll(io.LimitReader(file, maxStandaloneArchiveBytes+1))
	if err != nil || int64(len(archive)) != info.Size() {
		_, _ = fmt.Fprintln(stderr, "INVALID: unable to read complete archive")
		return 1
	}
	result, err := audit.VerifyEvidenceArchive(archive)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "INVALID: evidence verification failed")
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "VALID workspace=%s generation=%d events=%d head=%s\n",
		result.Manifest.WorkspaceId, result.Manifest.Generation, result.EventCount,
		hex.EncodeToString(result.Manifest.VerifiedHead))
	return 0
}
