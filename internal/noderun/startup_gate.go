package noderun

import (
	"sync"
	"time"
)

type startupAdmission struct {
	mu       sync.Mutex
	check    func() (bool, error)
	admitted bool
}

// WithLaunchIdentity is set before Run from the authenticated inherited permit.
// It is immutable during service lifetime, never supplied by an IPC caller or
// reread from the mutable selection state.
func (d *Daemon) WithLaunchIdentity(id string) *Daemon {
	d.launchID = id
	return d
}

// WithStartupGate is configured before Run by the authenticated launch entry.
// Status/stop stay available while the owner commits its selected activation.
func (d *Daemon) WithStartupGate(check func() (bool, error)) *Daemon {
	d.startup = &startupAdmission{check: check}
	return d
}

func (d *Daemon) startupReady() bool {
	if d.startup == nil {
		return true
	}
	d.startup.mu.Lock()
	defer d.startup.mu.Unlock()
	if d.startup.admitted {
		return true
	}
	ready, err := d.startup.check()
	if err != nil {
		d.log.Error("runtime startup selection invalid", "error", err)
		d.Stop()
		return false
	}
	d.startup.admitted = ready
	return ready
}

func (d *Daemon) connectSelectedDevice() {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for !d.startupReady() {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
		}
	}
	if d.ctx.Err() == nil {
		d.keepDevice()
	}
}

func startupObservation(method string) bool {
	switch method {
	case "status", "doctor", "runtime.status", "runtime.ensure", "runtime.operation.status", "runtime.operation.reconcile", "stop":
		return true
	default:
		return false
	}
}
