//go:build darwin && arm64 && cgo

package main

import (
	"context"
	"errors"
	"os"
	"sync"
	"syscall"
	"time"
)

var errParentMonitorUnavailable = errors.New("SBR_PARENT_MONITOR_UNAVAILABLE")

type kqueueParentMonitor struct {
	descriptor int
	once       sync.Once
}

func newParentLifetimeMonitor() (parentLifetimeMonitor, error) {
	parent := os.Getppid()
	if parent <= 1 {
		return nil, errParentMonitorUnavailable
	}
	descriptor, err := syscall.Kqueue()
	if err != nil {
		return nil, errParentMonitorUnavailable
	}
	var event syscall.Kevent_t
	syscall.SetKevent(&event, parent, syscall.EVFILT_PROC, syscall.EV_ADD|syscall.EV_ONESHOT)
	event.Fflags = syscall.NOTE_EXIT
	if _, err := syscall.Kevent(descriptor, []syscall.Kevent_t{event}, nil, nil); err != nil {
		_ = syscall.Close(descriptor)
		return nil, errParentMonitorUnavailable
	}
	return &kqueueParentMonitor{descriptor: descriptor}, nil
}

func (m *kqueueParentMonitor) Wait(ctx context.Context) error {
	for ctx.Err() == nil {
		events := make([]syscall.Kevent_t, 1)
		timeout := syscall.NsecToTimespec((100 * time.Millisecond).Nanoseconds())
		count, err := syscall.Kevent(m.descriptor, nil, events, &timeout)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, syscall.EINTR) {
				continue
			}
			return errParentMonitorUnavailable
		}
		if count > 0 {
			return nil
		}
	}
	return ctx.Err()
}

func (m *kqueueParentMonitor) Close() error {
	var err error
	m.once.Do(func() { err = syscall.Close(m.descriptor) })
	return err
}
