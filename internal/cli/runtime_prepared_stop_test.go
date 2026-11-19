package cli

import (
	"context"
	"errors"
	"github.com/xallor-ai/xallor-remote/internal/filelock"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"net"
	"os"
	"path/filepath"
	"testing"
)

type preparedWatch struct {
	waited, closed bool
	closeErr       error
}

// A prepared public absence observation retains its original profile even when
// ambient configuration changes; an absent original profile remains absent.
func TestPreparedStopRetainsCapturedProfileAcrossEnvironmentChange(t *testing.T) {
	for _, absent := range []bool{false, true} {
		t.Run(map[bool]string{false: "existing", true: "absent"}[absent], func(t *testing.T) {
			original := t.TempDir()
			if absent {
				original = filepath.Join(original, "absent")
			}
			t.Setenv("XALLOR_REMOTE_DATA_DIR", original)
			attempt, err := (runtimeControl{}).PrepareStop(context.Background(), release.StopRequest{AbsencePolicy: "public_observation"})
			if err != nil {
				t.Fatal(err)
			}
			defer attempt.Close()
			replacement := t.TempDir()
			unlock, err := filelock.Acquire(filepath.Join(replacement, "runtime.lock"))
			if err != nil {
				t.Fatal(err)
			}
			defer unlock()
			t.Setenv("XALLOR_REMOTE_DATA_DIR", replacement)
			observed, err := attempt.Run(context.Background())
			if err != nil || !observed.Confirms(attempt.Intent()) {
				t.Fatal(observed, err)
			}
			if absent {
				if observed.RuntimeResource != "absent_profile" {
					t.Fatal("fabricated lifetime lock evidence", observed)
				}
				if _, err := os.Stat(original); !os.IsNotExist(err) {
					t.Fatal("absence observation created original profile", err)
				}
			} else if observed.RuntimeResource != "released_under_lock" {
				t.Fatal(observed)
			}
			if _, err := os.Stat(filepath.Join(replacement, "jobs.lock")); !os.IsNotExist(err) {
				t.Fatal("observed replacement profile", err)
			}
		})
	}
}

func (w *preparedWatch) Exited() (bool, error)      { return false, nil }
func (w *preparedWatch) Wait(context.Context) error { w.waited = true; return nil }
func (w *preparedWatch) Close() error               { w.closed = true; return w.closeErr }

// Lost acknowledgement remains unknown, but the retained process and resource
// observers still run, so durable evidence does not erase physical observations.
func TestPreparedStopObservesExitAfterLostAck(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	go func() { c := ipc.NewConn(server); _, _ = c.ReadRequest(); _ = server.Close() }()
	watch := &preparedWatch{}
	resources := false
	a := &runtimeStopAttempt{intent: release.StopIntent{Mode: "peer", AttemptID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PeerPID: 123, ImageSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", IdentityScope: "process_image"}, raw: client, conn: ipc.NewConn(client), watch: watch, resources: func() (string, string, error) { resources = true; return "released_under_lock", "empty", nil }}
	o, err := a.Run(context.Background())
	if !errors.Is(err, release.ErrCleanupUncertain) || !watch.waited || !watch.closed || !resources || o.Process != "exit_observed" || o.Dispatch != "may_have_been_sent" || o.Result != "unknown" {
		t.Fatal(o, err, watch, resources)
	}
}

// Close failures and a changed durable selection never permit confirmation.
func TestPreparedStopRejectsCleanupFailureAndSelectionChange(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "close", true: "selection"}[changed], func(t *testing.T) {
			watch := &preparedWatch{closeErr: errors.New("close failed")}
			called := false
			a := &runtimeStopAttempt{intent: release.StopIntent{Mode: "no_peer", AttemptID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", AbsenceBasis: "fresh"}, watch: watch, resources: func() (string, string, error) { called = true; return "released_under_lock", "empty", nil }}
			if changed {
				a.managed = true
				a.store = release.Store{Dir: t.TempDir()}
				a.before = release.ObserveSelection(release.State{})
				if err := os.WriteFile(filepath.Join(a.store.Dir, "state.json"), []byte("invalid"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			o, err := a.Run(context.Background())
			if !errors.Is(err, release.ErrCleanupUncertain) || o.Observers != "unknown" || o.Result != "unknown" || called == changed {
				t.Fatal(o, err, called)
			}
			if _, err := a.Close(); err == nil {
				t.Fatal("close error not retained")
			}
		})
	}
}
