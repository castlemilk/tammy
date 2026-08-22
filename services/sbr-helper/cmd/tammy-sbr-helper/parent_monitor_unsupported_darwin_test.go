//go:build darwin && (!arm64 || !cgo)

package main

import "testing"

const compiledHelperSupported = false

func TestUnsupportedDarwinParentMonitorFailsClosed(t *testing.T) {
	monitor, err := newParentLifetimeMonitor()
	if err == nil || monitor != nil || err.Error() != "SBR_PARENT_MONITOR_UNAVAILABLE" {
		t.Fatalf("monitor=%#v error=%v", monitor, err)
	}
}

func TestUnsupportedBuiltHelperFailsClosed(t *testing.T) {
	binary := buildHelper(t)
	stdout, stderr, err := runHelper(t, binary, nil)
	if err == nil || len(stdout) != 0 || string(stderr) != "{\"code\":\"SBR_HELPER_UNAVAILABLE\"}\n" {
		t.Fatalf("error=%v stdout=%x stderr=%q", err, stdout, stderr)
	}
}
