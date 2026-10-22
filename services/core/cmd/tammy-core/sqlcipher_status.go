//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package main

import (
	"encoding/json"
	"io"

	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
)

func reportSQLCipher(arguments []string, stdout io.Writer, stderr io.Writer) (bool, int) {
	if len(arguments) != 1 || arguments[0] != "--sqlcipher-status" {
		return false, 0
	}
	report, err := sqlcipher.Report()
	if err != nil {
		_ = json.NewEncoder(stderr).Encode(map[string]string{"error": "SQLCIPHER_PROVENANCE_UNAVAILABLE"})
		return true, 1
	}
	if err := json.NewEncoder(stdout).Encode(report); err != nil {
		return true, 1
	}
	return true, 0
}
