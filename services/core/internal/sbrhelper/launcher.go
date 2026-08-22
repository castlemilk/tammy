package sbrhelper

import (
	"context"
	"os"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/sbrprofile"
)

type Launcher struct {
	locator sbrprofile.ResourceLocator
	now     func() time.Time
	run     processRun
	capture identityCapture
	verify  processVerifier
}

type codeIdentity struct {
	cdHash     []byte
	identifier string
	team       string
}

type identityCapture func(context.Context, *sbrprofile.StagedResources) (codeIdentity, error)
type childVerifier func(context.Context, int, bool) error
type processVerifier func(context.Context, int, *sbrprofile.StagedResources, codeIdentity, bool) error
type processRun func(context.Context, string, []string, []byte, []*os.File, func() error, childVerifier) ([]byte, error)

func NewLauncher(locator sbrprofile.ResourceLocator) *Launcher {
	return &Launcher{locator: locator, now: time.Now, run: runSandboxedProcess, capture: captureAuthenticatedStagedCodeIdentity, verify: verifyAuthenticatedHelperProcess}
}

func (l *Launcher) Launch(ctx context.Context, profilePath string, request Request) (Response, error) {
	defer request.ClearSecrets()
	if l == nil || l.locator == nil || ctx == nil {
		return Response{}, protocolError("LAUNCHER_INVALID")
	}
	return l.launch(ctx, profilePath, request)
}
