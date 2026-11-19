package release

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/releasetrust"
)

type bootstrapControl struct {
	*fakeControl
	starts        int
	beforeRunning func(int)
}

func (c *bootstrapControl) Start(path, sha string) error {
	c.starts++
	return c.fakeControl.Start(path, sha)
}

func (c *bootstrapControl) Running() (*buildinfo.Executable, error) {
	if c.beforeRunning != nil {
		c.beforeRunning(c.runningChecks + 1)
	}
	return c.fakeControl.Running()
}

// First bootstrap authenticates before any profile write or candidate probe.
// A denied source cannot inherit trust merely because the launcher requested it.
func TestEnsureRunning_should_refuseUnverifiedBootstrap_beforeWritesOrExecution(t *testing.T) {
	dir, m := bundle(t, "bootstrap-denied")
	s := Store{Dir: filepath.Join(t.TempDir(), "runtime"), sourceVerifier: func(context.Context, string, releasetrust.Identity, string, string) error {
		return releasetrust.ErrUnverified
	}}
	c := &bootstrapControl{fakeControl: control(m)}
	if err := s.EnsureRunning(dir, c); !errors.Is(err, releasetrust.ErrUnverified) {
		t.Fatal(err)
	}
	if c.starts != 0 || c.checks != 0 || c.stops != 0 {
		t.Fatal("unverified bootstrap executed", c)
	}
	if _, err := os.Stat(s.Dir); !os.IsNotExist(err) {
		t.Fatal("denied bootstrap wrote installation directory", err)
	}
}

// Bootstrap creates one private activation and starts it once. A later ensure
// observes that owner without stopping it, probing it again or using a fallback.
func TestEnsureRunning_should_bootstrapOnce_thenReuseVerifiedOwner(t *testing.T) {
	dir, m := bundle(t, "bootstrap-valid")
	s := mechanicsStore(t)
	c := &bootstrapControl{fakeControl: control(m)}
	if err := s.EnsureRunning(dir, c); err != nil {
		t.Fatal(err)
	}
	st, err := s.Read()
	if err != nil || st.Current == nil || *st.Current != m || st.CurrentActivation == "" || st.Pending != nil {
		t.Fatal(st, err)
	}
	if c.starts != 1 || c.stops != 0 || c.live == nil || filepath.Dir(c.live.Path) == dir {
		t.Fatal("bootstrap did not use private activation", c)
	}
	checks := c.checks
	if err := s.EnsureRunning("missing-fallback-must-not-be-read", c); err != nil {
		t.Fatal(err)
	}
	if c.starts != 1 || c.stops != 0 || c.checks != checks {
		t.Fatal("existing owner was restarted/probed", c)
	}
}

// A start with an uncertain/failed result is persisted as pending and cannot be
// retried by another automatic ensure. Recovery remains an explicit operation.
func TestEnsureRunning_should_keepPending_withoutRetryAfterStartFailure(t *testing.T) {
	dir, m := bundle(t, "bootstrap-failed")
	s := mechanicsStore(t)
	c := &bootstrapControl{fakeControl: control(m)}
	c.fail = m.Artifact.SHA256
	if err := s.EnsureRunning(dir, c); err == nil {
		t.Fatal("failed start reported ready")
	}
	st, err := s.Read()
	if err != nil || st.Pending == nil || st.PendingActivation == "" || st.Current != nil {
		t.Fatal(st, err)
	}
	if err := s.EnsureRunning(dir, c); err == nil {
		t.Fatal("pending start was retried")
	}
	if c.starts != 1 || c.stops != 0 {
		t.Fatal("automatic recovery changed process state", c)
	}
}

// Replace the selected bytes after source verification but before Start. The
// supplied digest must stay bound to the manifest, never be refreshed from disk.
func TestEnsureRunning_should_preserveManifestDigest_whenSelectionChangesBeforeStart(t *testing.T) {
	dir, m := bundle(t, "bootstrap-swap")
	s := mechanicsStore(t)
	f := control(m)
	if err := s.Install(dir, f); err != nil {
		t.Fatal(err)
	}
	path := f.live.Path
	if err := f.Stop(); err != nil {
		t.Fatal(err)
	}
	stops := f.stops
	f.runningChecks = 0
	c := &bootstrapControl{fakeControl: f}
	c.beforeRunning = func(call int) {
		if call == 3 {
			if err := os.WriteFile(path, []byte("replacement"), 0700); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := s.EnsureRunning("unverified-fallback", c); err == nil {
		t.Fatal("replacement was accepted")
	}
	if c.live != nil || c.starts != 1 || c.stops != stops {
		t.Fatal("replacement was executed or fallback attempted", c)
	}
	st, err := s.Read()
	if err != nil || st.Pending == nil || st.Pending.Artifact.SHA256 != m.Artifact.SHA256 {
		t.Fatal("expected digest was replaced", st, err)
	}
}

// A Runtime appearing after the first absent observation must pass the same
// verification gate, and must never be stopped just to complete bootstrap.
func TestEnsureRunning_should_reuseOwnerAppearingDuringBootstrap(t *testing.T) {
	dir, m := bundle(t, "bootstrap-race")
	s := mechanicsStore(t)
	f := control(m)
	if err := s.Install(dir, f); err != nil {
		t.Fatal(err)
	}
	live := f.live
	f.live, f.runningChecks = nil, 0
	stops := f.stops
	c := &bootstrapControl{fakeControl: f}
	c.beforeRunning = func(call int) {
		if call == 2 {
			f.live = live
		}
	}
	if err := s.EnsureRunning("unverified-fallback", c); err != nil {
		t.Fatal(err)
	}
	if c.starts != 0 || c.stops != stops || c.live != live {
		t.Fatal("concurrent owner replaced", c)
	}
}
