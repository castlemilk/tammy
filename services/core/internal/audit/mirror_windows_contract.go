package audit

import (
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"
)

const windowsMirrorMutexAccess uint32 = 0x00100001 // SYNCHRONIZE | MUTEX_MODIFY_STATE

type windowsMirrorMutexContract struct {
	name   string
	sddl   string
	access uint32
}

func newWindowsMirrorMutexContract(userSID, label string) (windowsMirrorMutexContract, error) {
	if !validWindowsUserSID(userSID) || label == "" {
		return windowsMirrorMutexContract{}, ErrMirrorInvalid
	}
	digest := sha256.Sum256([]byte(userSID + "\x00" + label))
	rights := fmt.Sprintf("0x%08x", windowsMirrorMutexAccess)
	return windowsMirrorMutexContract{
		name:   fmt.Sprintf("Global\\TammyAuditMirror-%x", digest[:]),
		sddl:   fmt.Sprintf("O:%sD:P(A;;%s;;;SY)(A;;%s;;;%s)", userSID, rights, rights, userSID),
		access: windowsMirrorMutexAccess,
	}, nil
}

func validWindowsUserSID(value string) bool {
	parts := strings.Split(value, "-")
	if len(parts) < 4 || len(parts) > 18 || parts[0] != "S" {
		return false
	}
	if _, err := strconv.ParseUint(parts[1], 10, 8); err != nil {
		return false
	}
	if _, err := strconv.ParseUint(parts[2], 10, 48); err != nil {
		return false
	}
	for _, part := range parts[3:] {
		if _, err := strconv.ParseUint(part, 10, 32); err != nil {
			return false
		}
	}
	return true
}
