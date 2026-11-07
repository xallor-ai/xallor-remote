// Package admission bounds in-flight transport requests. Its leases end when
// routing ends, not when an OS job ends. Durable job or commercial resource
// budgets must use their own lifecycle and must not reuse these leases.
package admission

import (
	"errors"
	"sync"
)

var ErrInvalidRequest = errors.New("invalid admission request")
var ErrLimit = errors.New("admission limit reached")

type Limits struct {
	PerDevice int
}

type Request struct {
	DeviceUID string
}

type Service struct {
	mu     sync.Mutex
	limits Limits
	active map[string]int
}

func New(limits Limits) *Service {
	if limits.PerDevice <= 0 {
		panic("admission: PerDevice must be positive")
	}
	return &Service{limits: limits, active: map[string]int{}}
}

type Lease struct {
	once    sync.Once
	service *Service
	device  string
}

func (s *Service) Acquire(req Request) (*Lease, error) {
	if req.DeviceUID == "" {
		return nil, ErrInvalidRequest
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active[req.DeviceUID] >= s.limits.PerDevice {
		return nil, ErrLimit
	}
	s.active[req.DeviceUID]++
	return &Lease{service: s, device: req.DeviceUID}, nil
}

// Release is idempotent. Terminal and disconnect cleanup may overlap, but one
// accepted lease returns transport capacity once. Sending cancel is not release.
func (l *Lease) Release() {
	if l == nil {
		return
	}
	l.once.Do(func() {
		l.service.mu.Lock()
		defer l.service.mu.Unlock()
		if l.service.active[l.device] > 1 {
			l.service.active[l.device]--
		} else {
			delete(l.service.active, l.device)
		}
	})
}

func (s *Service) Active(device string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active[device]
}
