package admission

import (
	"errors"
	"sync"
	"testing"
)

// Overlapping terminal cleanup returns a slot once and cannot decrement a later
// lease's occupancy when a stale completion repeats.
func TestService_should_releaseExactlyOnce_whenLeaseRacesTerminalPaths(t *testing.T) {
	s := New(Limits{PerDevice: 1})
	l, err := s.Acquire(Request{DeviceUID: "device"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Acquire(Request{DeviceUID: "device"}); !errors.Is(err, ErrLimit) {
		t.Fatal("second lease bypassed device limit")
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() { defer wg.Done(); l.Release() }()
	}
	wg.Wait()
	if s.Active("device") != 0 {
		t.Fatal("lease release leaked capacity")
	}
	if _, err := s.Acquire(Request{DeviceUID: "device"}); err != nil {
		t.Fatal("released capacity was not reusable", err)
	}
	l.Release()
	if s.Active("device") != 1 {
		t.Fatal("stale release decremented its successor")
	}
}

// All competing callers see one atomic budget; excess acquisitions fail and
// releasing every winner removes the idle device entry rather than leaking it.
func TestService_should_admitOnlyCapacity_whenAcquisitionsRace(t *testing.T) {
	s := New(Limits{PerDevice: 4})
	start := make(chan struct{})
	winners := make(chan *Lease, 32)
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			lease, err := s.Acquire(Request{DeviceUID: "device"})
			if err == nil {
				winners <- lease
			} else if !errors.Is(err, ErrLimit) {
				t.Errorf("unexpected acquisition error: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	close(winners)
	if len(winners) != 4 || s.Active("device") != 4 {
		t.Fatal("concurrent acquisition exceeded or lost capacity")
	}
	for lease := range winners {
		lease.Release()
	}
	if len(s.active) != 0 {
		t.Fatal("idle device retained accounting state")
	}
}

// Each device has an independent transport budget; a full device cannot block
// another device or permit an empty resource identity to allocate capacity.
func TestService_should_isolateBudgets_whenDevicesDiffer(t *testing.T) {
	s := New(Limits{PerDevice: 2})
	for _, req := range []Request{{DeviceUID: "device"}, {DeviceUID: "device"}, {DeviceUID: "other"}} {
		if _, err := s.Acquire(req); err != nil {
			t.Fatal(err)
		}
	}
	if s.Active("device") != 2 || s.Active("other") != 1 {
		t.Fatal("independent device counters collapsed")
	}
	if _, err := s.Acquire(Request{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal("empty device admission accepted")
	}
}
