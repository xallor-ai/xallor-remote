package handoff

import (
	"context"
	"errors"

	"github.com/xallor-ai/xallor-remote/internal/invocation"
)

var errDeviceContextChanged = errors.New("device execution context changed")

type worker struct {
	scope  invocation.Scope
	cancel context.CancelCauseFunc
}

// Ticket captures the local device generation when the invoke is received.
// Its private fields prevent a delayed goroutine from acquiring a new permit
// after an off/on transition returns to an otherwise identical device scope.
type Ticket struct {
	manager    *Manager
	scope      invocation.Scope
	generation uint64
}

func (m *Manager) Capture(scope invocation.Scope) Ticket {
	m.mu.Lock()
	defer m.mu.Unlock()
	return Ticket{manager: m, scope: scope, generation: m.generation}
}

// SyncDevice serializes with Submit. Already received work loses its worker
// context when the device incarnation changes or inbound is disabled. This
// only requests cancellation: the worker must still confirm process cleanup
// and persist resource release. Repeating a snapshot does not interrupt work.
func (m *Manager) SyncDevice(current invocation.DeviceContext) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.device != current {
		m.generation++
		m.device = current
	}
	for _, worker := range m.active {
		if !current.Allows(worker.scope) {
			worker.cancel(errDeviceContextChanged)
		}
	}
	if !current.Valid() {
		return errDeviceContextChanged
	}
	return nil
}
