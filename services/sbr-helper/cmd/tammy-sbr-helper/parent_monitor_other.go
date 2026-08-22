//go:build !darwin

package main

import "context"

type portableParentMonitor struct{}

func newParentLifetimeMonitor() (parentLifetimeMonitor, error) { return portableParentMonitor{}, nil }
func (portableParentMonitor) Wait(ctx context.Context) error   { <-ctx.Done(); return ctx.Err() }
func (portableParentMonitor) Close() error                     { return nil }
