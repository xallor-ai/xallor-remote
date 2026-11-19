package noderun

import (
	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"testing"
)

// Ordinary status retains only the process's authenticated launch identity,
// even when the startup gate is pending. It never obtains identity from disk.
func TestPreparedStopStatusRetainsLaunchWhilePending(t *testing.T) {
	d := &Daemon{}
	d.WithActivationIdentity("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").WithLaunchIdentity("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb").WithStartupGate(func() (bool, error) { return false, nil })
	got := d.runningIdentity()
	if got.ActivationID != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" || got.LaunchID != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" || got.PID != buildinfo.Running().PID {
		t.Fatal(got)
	}
	if d.startupReady() {
		t.Fatal("observing launch identity acknowledged startup")
	}
	if unmanaged := (&Daemon{}).runningIdentity(); unmanaged.ActivationID != "" || unmanaged.LaunchID != "" {
		t.Fatal("unmanaged identity fabricated", unmanaged)
	}
}
