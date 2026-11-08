// Package usage stores privacy-minimized, idempotent usage observations. It is
// separate from local activity audit, job safety capacity and commercial
// billing; callers must only append after independently observing a terminal.
package usage

import (
	"encoding/json"
	"errors"
	"math"
	"path/filepath"
	"sync"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/atomicfile"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

const MaxEvents = protocol.MaxUsageEvents
const MaxRetiredKeys = protocol.MaxUsageRetiredKeys
const maxBytes = protocol.MaxUsageBytes

var (
	ErrInvalidEvent  = errors.New("invalid usage event")
	ErrConflict      = errors.New("usage idempotency conflict")
	ErrInvalidLedger = errors.New("usage ledger is invalid")
	ErrRetentionFull = errors.New("usage retention metadata limit reached")
	ErrOverflow      = errors.New("completed usage total overflows int64")
	ErrLimit         = errors.New("usage ledger exceeds limit")
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
	mu          sync.Mutex
	path        string
	events      []Event
	byKey       map[string]Event
	retiredKeys []string
	retired     map[string]struct{}
	write       func(string, []byte) error
	err         error
}

func Open(dir string) *Ledger {
	l := &Ledger{path: filepath.Join(dir, "usage.json"), events: []Event{}, byKey: map[string]Event{}, retiredKeys: []string{}, retired: map[string]struct{}{}, write: atomicfile.Write}
	if !filepath.IsAbs(dir) {
		l.err = ErrInvalidEvent
		return l
	}
	l.err = l.load()
	return l
}

func (l *Ledger) Append(event Event) (Event, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return Event{}, l.err
	}
	old, exists := l.byKey[event.IdempotencyKey]
	if event.At == "" {
		if exists {
			event.At = old.At
		} else {
			event.At = time.Now().UTC().Format(time.RFC3339Nano)
		}
	}
	if err := validate(event); err != nil {
		return Event{}, err
	}
	if exists {
		if old == event {
			return old, nil
		}
		return Event{}, ErrConflict
	}
	if _, exists := l.retired[keyDigest(event.IdempotencyKey)]; exists {
		return Event{}, ErrConflict
	}
	next := append(append([]Event(nil), l.events...), event)
	nextKeys := l.retiredKeys
	if len(next) > MaxEvents {
		if len(nextKeys) >= MaxRetiredKeys {
			return Event{}, ErrRetentionFull
		}
		nextKeys = append(append([]string(nil), nextKeys...), keyDigest(next[0].IdempotencyKey))
		next = next[len(next)-MaxEvents:]
	}
	data, err := json.Marshal(diskState{Version: 1, Events: next, RetiredKeys: nextKeys})
	if err != nil || len(data) > maxBytes {
		return Event{}, ErrLimit
	}
	if err := l.write(l.path, data); err != nil {
		l.err = err
		return Event{}, err
	}
	l.events = next
	l.retiredKeys = nextKeys
	if len(nextKeys) > len(l.retired) {
		l.retired[nextKeys[len(nextKeys)-1]] = struct{}{}
	}
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
			if row.Units > math.MaxInt64-total {
				return 0, ErrOverflow
			}
			total += row.Units
		}
	}
	return total, nil
}
