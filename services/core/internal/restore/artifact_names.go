package restore

import (
	"encoding/hex"
	"strings"

	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

const (
	restoreStagePrefix    = ".tammy-restore-stage-"
	restoreRollbackPrefix = ".tammy-restore-rollback-"
	restoreArtifactSuffix = ".db"
)

func validRestoreArtifactBasenames(operationID, workspaceID, stageName, rollbackName string) bool {
	return validRestoreArtifactBasename(restoreStagePrefix, operationID, workspaceID, stageName) &&
		validRestoreArtifactBasename(restoreRollbackPrefix, operationID, workspaceID, rollbackName)
}

func validRestoreArtifactBasename(prefix, operationID, workspaceID, name string) bool {
	if !ids.IsCanonicalV7(operationID) || !ids.IsCanonicalV7(workspaceID) {
		return false
	}
	wantPrefix := prefix + operationID + "-" + workspaceID + "-"
	if !strings.HasPrefix(name, wantPrefix) || !strings.HasSuffix(name, restoreArtifactSuffix) {
		return false
	}
	tag := strings.TrimSuffix(strings.TrimPrefix(name, wantPrefix), restoreArtifactSuffix)
	decoded, err := hex.DecodeString(tag)
	return err == nil && len(decoded) == 32 && tag == strings.ToLower(tag)
}

func restoreOperationFromStageBasename(name string) (string, bool) {
	if !strings.HasPrefix(name, restoreStagePrefix) || !strings.HasSuffix(name, restoreArtifactSuffix) {
		return "", false
	}
	remainder := strings.TrimSuffix(strings.TrimPrefix(name, restoreStagePrefix), restoreArtifactSuffix)
	if len(remainder) < 36 || remainder[36] != '-' {
		return "", false
	}
	operationID := remainder[:36]
	return operationID, ids.IsCanonicalV7(operationID)
}
