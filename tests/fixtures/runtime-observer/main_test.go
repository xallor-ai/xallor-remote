package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/release"
)

// A native IPC endpoint is mandatory; a self-reported PID cannot replace its
// kernel peer identity. This fixture tests observation, not source attestation.
func TestObserve_shouldBindReadyReplyToNativePeer(t *testing.T) {
	for _, spoof := range []bool{false, true} {
		t.Run(map[bool]string{false: "matching", true: "spoofed"}[spoof], func(t *testing.T) {
			profile, err := os.MkdirTemp("", "xr-observer-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.Remove(profile); err != nil {
					t.Error(err)
				}
			})
			t.Setenv("XALLOR_REMOTE_DATA_DIR", profile)
			listener, err := ipc.Listen()
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			done := make(chan error, 1)
			go func() {
				raw, err := listener.Accept()
				if err != nil {
					done <- err
					return
				}
				defer raw.Close()
				_ = raw.SetDeadline(time.Now().Add(5 * time.Second))
				conn := ipc.NewConn(raw)
				request, err := conn.ReadRequest()
				if err != nil {
					done <- err
					return
				}
				pid := os.Getpid()
				if spoof {
					pid++
				}
				view := release.RuntimeObservation{State: "ready", Version: buildinfo.Version(), Revision: strings.Repeat("a", 40), Protocol: 1, ProfileFormat: 1, SHA256: strings.Repeat("b", 64), PID: pid, ActivationID: strings.Repeat("c", 32), LaunchID: strings.Repeat("d", 32)}
				done <- conn.WriteFrame(ipc.OK(request.ID, map[string]any{"runtime": view}))
			}()
			result, err := observe(profile)
			if serverErr := <-done; serverErr != nil {
				t.Fatal(serverErr)
			}
			if spoof {
				if err == nil {
					t.Fatal("spoofed peer accepted")
				}
				return
			}
			if err != nil || result.PeerPID != os.Getpid() || result.Runtime.LaunchID != strings.Repeat("d", 32) {
				t.Fatal("ready peer not observed", err)
			}
		})
	}
}

// Never fall back to a user's default Runtime when the explicit profile is absent.
func TestObserve_shouldRejectMissingOrRelativeProfile(t *testing.T) {
	for _, profile := range []string{"", "relative"} {
		if _, err := observe(profile); err == nil {
			t.Fatal("implicit profile accepted")
		}
	}
}
