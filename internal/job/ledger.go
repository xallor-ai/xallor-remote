// Package job owns durable local resource occupancy independently of transport
// requests and human-facing execution receipts. It is not a commercial quota.
package job

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/xallor-ai/xallor-remote/internal/atomicfile"
	"github.com/xallor-ai/xallor-remote/internal/filelock"
	"github.com/xallor-ai/xallor-remote/internal/invocation"
)

// Capacity bounds durable local jobs, including occupied and uncertain entries.
// Transport requests and service entitlements do not change this safety limit.
const Capacity = 4
const HandoffCapacity = 2
const maxLedgerBytes = 16 * 1024

var ErrCapacity = errors.New("local job capacity exhausted")
var ErrUncertain = errors.New("job resource release unconfirmed")
var ErrUnavailable = errors.New("local job ledger unavailable")

type Record struct {
	ID    string           `json:"id"`
	Scope invocation.Scope `json:"scope"`
	Kind  string           `json:"kind"`
	Key   string           `json:"key"`
	State string           `json:"state"`
}

type diskState struct {
	Version int               `json:"version"`
	Jobs    map[string]Record `json:"jobs"`
}

type Ledger struct {
	launch sync.RWMutex // Close must not hand off the file lock during OS startup.
	mu     sync.Mutex
	dir    string
	jobs   map[string]Record
	unlock func()
	closed bool
	err    error
	write  func(string, []byte) error
}

// New defers acquisition until use, so constructing a Runtime does not mutate
// a profile. The OS lock prevents two Runtime instances publishing stale state.
func New(dir string) *Ledger { return &Ledger{dir: dir, write: atomicfile.Write} }

func (l *Ledger) initializeLocked() error {
	if l.closed || l.err != nil {
		return ErrUnavailable
	}
	if l.jobs != nil {
		return nil
	}
	if !filepath.IsAbs(l.dir) {
		l.err = ErrUnavailable
		return l.err
	}
	if err := os.MkdirAll(l.dir, 0700); err != nil {
		l.err = err
		return ErrUnavailable
	}
	unlock, err := filelock.Acquire(filepath.Join(l.dir, "jobs.lock"))
	if err != nil {
		l.err = err
		return ErrUnavailable
	}
	l.unlock = unlock
	l.jobs = map[string]Record{}
	path := filepath.Join(l.dir, "jobs.json")
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		l.err = err
		return ErrUnavailable
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxLedgerBytes {
		l.err = ErrUnavailable
		return l.err
	}
	var disk diskState
	decoder := json.NewDecoder(io.LimitReader(f, maxLedgerBytes+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&disk); err != nil || disk.Version != 1 || disk.Jobs == nil || len(disk.Jobs) > Capacity {
		l.err = ErrUnavailable
		return l.err
	}
	if decoder.Decode(new(any)) != io.EOF {
		l.err = ErrUnavailable
		return l.err
	}
	// Windows cannot atomically replace a file while this read handle is open.
	if err := f.Close(); err != nil {
		l.err = err
		return ErrUnavailable
	}
	for id, r := range disk.Jobs {
		if !validRecord(id, r) {
			l.err = ErrUnavailable
			return l.err
		}
		// Start must persist occupied before it can launch any child. A reserved
		// record therefore proves that this lost owner never started a process.
		if r.State == "reserved" {
			continue
		}
		// Prior process disappearance is not evidence that its descendants died.
		r.State = "uncertain"
		l.jobs[id] = r
	}
	if len(disk.Jobs) > 0 {
		return l.saveLocked(l.jobs)
	}
	return nil
}

func validRecord(id string, r Record) bool {
	b, err := hex.DecodeString(id)
	return err == nil && len(b) == 16 && hex.EncodeToString(b) == id && id == r.ID && r.Scope.Valid() &&
		Kind(r.Kind).valid() && len(r.Key) > 0 && len(r.Key) <= 128 &&
		(r.State == "reserved" || r.State == "occupied" || r.State == "uncertain")
}

func (l *Ledger) saveLocked(jobs map[string]Record) error {
	b, err := json.Marshal(diskState{Version: 1, Jobs: jobs})
	if err == nil {
		err = l.write(filepath.Join(l.dir, "jobs.json"), b)
	}
	if err != nil {
		l.err = err
		return ErrUnavailable
	}
	l.jobs = jobs
	return nil
}

func (l *Ledger) copyLocked() map[string]Record {
	jobs := make(map[string]Record, len(l.jobs))
	for id, r := range l.jobs {
		jobs[id] = r
	}
	return jobs
}

// ReserveExec persists occupancy for a foreground or remote exec before any
// target child can start. Active and uncertain records both consume capacity.
func (l *Ledger) ReserveExec(scope invocation.Scope, execID string) (*Owner, error) {
	return l.reserve(scope, KindExec, execID)
}

// ReserveHandoff persists occupancy for a durable Agent session. Its smaller
// process-management ceiling is enforced independently of exec capacity.
func (l *Ledger) ReserveHandoff(scope invocation.Scope, sessionID string) (*Owner, error) {
	return l.reserve(scope, KindHandoff, sessionID)
}

// reserve is the one implementation for the two explicit lifecycle entries.
// It is unexported so callers cannot invent a third resource category.
func (l *Ledger) reserve(scope invocation.Scope, kind Kind, key string) (*Owner, error) {
	if !scope.Valid() || !kind.valid() || len(key) == 0 || len(key) > 128 {
		return nil, fmt.Errorf("invalid job identity")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.initializeLocked(); err != nil {
		return nil, err
	}
	count := 0
	for _, r := range l.jobs {
		if r.Scope == scope && Kind(r.Kind) == kind && r.Key == key {
			return nil, ErrUncertain
		}
		if Kind(r.Kind) == KindHandoff {
			count++
		}
	}
	if len(l.jobs) >= Capacity || (kind == "handoff" && count >= HandoffCapacity) {
		return nil, ErrCapacity
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, err
	}
	id := hex.EncodeToString(raw[:])
	if _, ok := l.jobs[id]; ok {
		return nil, ErrUnavailable
	}
	jobs := l.copyLocked()
	jobs[id] = Record{ID: id, Scope: scope, Kind: string(kind), Key: key, State: "reserved"}
	if err := l.saveLocked(jobs); err != nil {
		return nil, err
	}
	return &Owner{ledger: l, id: id}, nil
}

func (l *Ledger) finish(id string, uncertain bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.initializeLocked(); err != nil {
		return err
	}
	r, ok := l.jobs[id]
	if !ok {
		return ErrUnavailable
	}
	jobs := l.copyLocked()
	if uncertain {
		r.State = "uncertain"
		jobs[id] = r
	} else {
		delete(jobs, id)
	}
	if err := l.saveLocked(jobs); err != nil {
		return err
	}
	if uncertain {
		return ErrUncertain
	}
	return nil
}

// Snapshot is for local diagnostics. Callers must authorize before exposing
// identity-bearing records to a remote caller; it contains no command/output.
func (l *Ledger) Snapshot() ([]Record, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.initializeLocked(); err != nil {
		return nil, err
	}
	rows := make([]Record, 0, len(l.jobs))
	for _, r := range l.jobs {
		rows = append(rows, r)
	}
	return rows, nil
}

// Close waits for in-progress OS startup before handing off the file lock. It
// invalidates further launches but never proves active processes have stopped.
// The Runtime still waits for all Owners before closing its shared Ledger.
func (l *Ledger) Close() {
	l.launch.Lock()
	defer l.launch.Unlock()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	l.closed = true
	if l.unlock != nil {
		l.unlock()
		l.unlock = nil
	}
}
