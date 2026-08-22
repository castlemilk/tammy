//go:build !darwin || !arm64 || !cgo

package sbrhelper

import (
	"context"
	"os"
	"runtime"

	"github.com/tammyapp/tammy/services/core/internal/sbrprofile"
)

func (l *Launcher) launch(context.Context, string, Request) (Response, error) {
	return Response{}, protocolError("UNSUPPORTED_SBR_TARGET:" + runtime.GOOS + "/" + runtime.GOARCH)
}
func runSandboxedProcess(context.Context, string, []string, []byte, []*os.File, func() error, childVerifier) ([]byte, error) {
	return nil, protocolError("UNSUPPORTED_SBR_TARGET")
}
func captureAuthenticatedStagedCodeIdentity(context.Context, *sbrprofile.StagedResources) (codeIdentity, error) {
	return codeIdentity{}, protocolError("UNSUPPORTED_SBR_TARGET")
}
func verifyAuthenticatedHelperProcess(context.Context, int, *sbrprofile.StagedResources, codeIdentity, bool) error {
	return protocolError("UNSUPPORTED_SBR_TARGET")
}
func ValidateProfilePath(string) error {
	return protocolError("UNSUPPORTED_SBR_TARGET:" + runtime.GOOS + "/" + runtime.GOARCH)
}
