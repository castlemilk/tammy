//go:build !darwin || !arm64 || !cgo

package sbrprofile

import (
	"context"
	"time"
)

func AuthenticateAndStage(context.Context, string, ResourceLocator, time.Time) (*StagedResources, error) {
	return nil, UnsupportedTargetError()
}
