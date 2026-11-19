package cli

import (
	"os"
	"testing"
)

// A real empty profile/absent IPC can confirm no current peer, but cannot settle
// an earlier startup whose identity/lifetime was never observed. Keep evidence
// and the cleanup obligation instead of declaring that missing IPC proves exit.
func TestIndependentRuntimeCleanup_shouldRetainUnknownStartup_whenIPCIsAbsent(t *testing.T) {
	dir := t.TempDir()
	p := &independentProfile{dir: dir, attempted: true, current: &independentLaunch{StartupUncertain: true}}
	h := &independentHarness{t: t, relayURL: "ws://127.0.0.1:1/remote"}
	if err := h.stop(p); err == nil {
		t.Fatal("absent IPC falsely settled unknown startup")
	}
	if !p.attempted || !p.current.EndpointAbsent || p.current.KernelExit || p.current.Reaped {
		t.Fatal("partial launch evidence promoted to cleanup success")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal("unknown profile removed", err)
	}
}
