//go:build darwin && (!arm64 || !cgo)

package main

import (
	"context"
	"errors"
)

type unsupportedParentMonitor struct{}

func newParentLifetimeMonitor() (parentLifetimeMonitor, error) {
	return nil, errors.New("SBR_PARENT_MONITOR_UNAVAILABLE")
}
func (unsupportedParentMonitor) Wait(context.Context) error {
	return errors.New("SBR_PARENT_MONITOR_UNAVAILABLE")
}
func (unsupportedParentMonitor) Close() error { return nil }
