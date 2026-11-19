package release

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type incompatibleRestoreControl struct {
	*fakeControl
	failCandidate, baseline string
	starts                  []string
}

func (c *incompatibleRestoreControl) Start(path, sha string) error {
	c.starts = append(c.starts, sha)
	if sha == c.failCandidate {
		c.unsupportedOwnerSHA = c.baseline
		return errors.New("candidate startup failed")
	}
	return c.fakeControl.Start(path, sha)
}

// Recovery rechecks capability after a failed candidate. A later incompatible
// baseline response must retain pending rather than claim successful restore.
func TestRestore_should_retainPending_withoutStartingIncompatibleBaseline(t *testing.T) {
	first, old := bundle(t, "owner-restore-old")
	second, next := bundle(t, "owner-restore-next")
	s := mechanicsStore(t)
	c := &incompatibleRestoreControl{fakeControl: control(old, next)}
	if err := s.Install(first, c); err != nil {
		t.Fatal(err)
	}
	c.failCandidate, c.baseline = next.Artifact.SHA256, old.Artifact.SHA256
	c.starts = nil
	if err := s.Install(second, c); err == nil {
		t.Fatal("incompatible restore reported success")
	}
	if len(c.starts) != 1 || c.starts[0] != next.Artifact.SHA256 || c.live != nil {
		t.Fatal("incompatible baseline was started", c.starts, c.live)
	}
	st, err := s.Read()
	if err != nil || st.Pending == nil || *st.Pending != next || st.Current == nil || *st.Current != old {
		t.Fatal("recovery state lost", st, err)
	}
}

// Candidate and recovery baseline compatibility is required before disturbing
// an existing process or publishing pending. This includes rollback and a
// stopped selected image, not just first-time installation.
func TestLifecycle_should_rejectUnsupportedOwner_beforeStateOrProcessMutation(t *testing.T) {
	for _, scenario := range []string{"install-candidate", "install-baseline", "stopped-baseline", "rollback-target", "rollback-baseline", "selected-bootstrap"} {
		t.Run(scenario, func(t *testing.T) {
			first, old := bundle(t, "owner-preflight-old")
			second, next := bundle(t, "owner-preflight-next")
			s := mechanicsStore(t)
			c := &bootstrapControl{fakeControl: control(old, next)}
			if err := s.Install(first, c); err != nil {
				t.Fatal(err)
			}
			if scenario == "rollback-target" || scenario == "rollback-baseline" {
				if err := s.Install(second, c); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "selected-bootstrap" || scenario == "stopped-baseline" {
				if err := c.Stop(); err != nil {
					t.Fatal(err)
				}
			}
			c.unsupportedOwnerSHA = old.Artifact.SHA256
			if scenario == "install-candidate" || scenario == "rollback-baseline" {
				c.unsupportedOwnerSHA = next.Artifact.SHA256
			}
			before, err := os.ReadFile(filepath.Join(s.Dir, "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			live, stops, starts := c.live, c.stops, c.starts
			switch scenario {
			case "rollback-target", "rollback-baseline":
				err = s.Rollback(c)
			case "selected-bootstrap":
				err = s.EnsureRunning("no-fallback", c)
			default:
				err = s.Install(second, c)
			}
			if err == nil {
				t.Fatal("unsupported owner was accepted")
			}
			after, readErr := os.ReadFile(filepath.Join(s.Dir, "state.json"))
			if readErr != nil || string(before) != string(after) || c.live != live || c.stops != stops || c.starts != starts {
				t.Fatalf("rejected preflight changed service/state: %v %v", err, readErr)
			}
		})
	}
}

// A verified fresh bundle with no owner capability is still unsuitable for
// starting a new profile. Source proof alone cannot authorize a pending launch.
func TestEnsureRunning_should_rejectFreshBundleWithoutOwner_beforePendingOrStart(t *testing.T) {
	dir, m := bundle(t, "unsupported-owner-bootstrap")
	s := mechanicsStore(t)
	c := &bootstrapControl{fakeControl: control(m)}
	c.unsupportedOwnerSHA = m.Artifact.SHA256
	if err := s.EnsureRunning(dir, c); err == nil {
		t.Fatal("unsupported bootstrap accepted")
	}
	if c.starts != 0 || c.stops != 0 {
		t.Fatal("unsupported bootstrap reached lifecycle control")
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "state.json")); !os.IsNotExist(err) {
		t.Fatal("unsupported bootstrap published state", err)
	}
}
