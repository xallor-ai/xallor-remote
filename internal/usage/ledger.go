// Package usage stores privacy-minimized, idempotent usage observations. It is
// separate from local activity audit, job safety capacity and commercial
// billing; callers must only append after independently observing a terminal.
package usage

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/atomicfile"
)

const MaxEvents = 5000
const maxBytes = 4 * 1024 * 1024

var (
	ErrInvalidEvent = errors.New("invalid usage event")
	ErrConflict     = errors.New("usage idempotency conflict")
)

type Event struct {
	ID             string `json:"id"`
	At             string `json:"at"`
	IdempotencyKey string `json:"idempotency_key"`
	TenantID       string `json:"tenant_id"`
	MembershipID   string `json:"membership_id"`
	DeviceUID      string `json:"device_uid"`
	Operation      string `json:"operation"`
	Outcome        string `json:"outcome"`
	Units          int64  `json:"units"`
}

type Ledger struct {
	mu     sync.Mutex
	path   string
	events []Event
	byKey  map[string]Event
	err    error
}

func Open(dir string) *Ledger {
	l := &Ledger{path: filepath.Join(dir, "usage.json"), events: []Event{}, byKey: map[string]Event{}}
	if !filepath.IsAbs(dir) {
		l.err = ErrInvalidEvent
		return l
	}
	f, err := os.Open(l.path)
	if os.IsNotExist(err) {
		return l
	}
	if err != nil {
		l.err = err
		return l
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil || len(data) > maxBytes || json.Unmarshal(data, &l.events) != nil || len(l.events) > MaxEvents {
		l.err = fmt.Errorf("usage ledger is invalid")
		l.events = nil
		return l
	}
	for _, event := range l.events {
		if err := validate(event); err != nil || l.byKey[event.IdempotencyKey].ID != "" {
			l.err = fmt.Errorf("usage ledger is invalid")
			l.events = nil
			return l
		}
		l.byKey[event.IdempotencyKey] = event
	}
	return l
}

func (l *Ledger) Append(event Event) (Event, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return Event{}, l.err
	}
	if err := validate(event); err != nil {
		return Event{}, err
	}
	if old, ok := l.byKey[event.IdempotencyKey]; ok {
		if event.At == "" {
			event.At = old.At
		}
		if old == event {
			return old, nil
		}
		return Event{}, ErrConflict
	}
	if event.At == "" {
		event.At = time.Now().UTC().Format(time.RFC3339Nano)
	}
	next := append(append([]Event(nil), l.events...), event)
	if len(next) > MaxEvents {
		next = next[len(next)-MaxEvents:]
	}
	data, err := json.Marshal(next)
	if err != nil || len(data) > maxBytes {
		return Event{}, fmt.Errorf("usage ledger exceeds limit")
	}
	if err := atomicfile.Write(l.path, data); err != nil {
		l.err = err
		return Event{}, err
	}
	l.events = next
	l.byKey = make(map[string]Event, len(next))
	for _, row := range next {
		l.byKey[row.IdempotencyKey] = row
	}
	return event, nil
}

func (l *Ledger) Events() ([]Event, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return nil, l.err
	}
	return append([]Event(nil), l.events...), nil
}

// CompletedUnits is an observation helper, not a bill. Unknown and failed
// outcomes contribute zero even when they contain a nonzero measured amount.
func (l *Ledger) CompletedUnits(tenant, membership string) (int64, error) {
	rows, err := l.Events()
	if err != nil {
		return 0, err
	}
	var total int64
	for _, row := range rows {
		if row.TenantID == tenant && row.MembershipID == membership && row.Outcome == "completed" {
			total += row.Units
		}
	}
	return total, nil
}

func validate(event Event) error {
	if !bounded(event.ID) || !bounded(event.IdempotencyKey) || !bounded(event.TenantID) || !bounded(event.MembershipID) || !bounded(event.DeviceUID) || !bounded(event.Operation) || event.At != "" && len(event.At) > 64 || event.Units < 0 {
		return ErrInvalidEvent
	}
	if event.Outcome != "completed" && event.Outcome != "failed" && event.Outcome != "unknown" {
		return ErrInvalidEvent
	}
	if event.Outcome == "completed" && (event.TenantID == "" || event.MembershipID == "" || event.DeviceUID == "" || event.Operation == "") {
		return ErrInvalidEvent
	}
	return nil
}

func bounded(value string) bool { return value != "" && len(value) <= 128 }
