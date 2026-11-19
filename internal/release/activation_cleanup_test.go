package release

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
)

func stagedCleanupFixture(t *testing.T) (Store, Activation) {
	t.Helper()
	dir, m := bundle(t, "cleanup-owner")
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := mechanicsStore(t)
	a, err := s.StageActivation(context.Background(), filepath.Join(dir, m.Artifact.Filename), m, raw)
	if err != nil {
		t.Fatal(err)
	}
	return s, a
}

// Adoption makes a private recovery copy, but preflight can still reject the
// candidate before publishing that baseline. Reclaim the copy, not the live image.
func TestInstall_should_reclaimUnpublishedAdoptedBaseline_whenProbeRejects(t *testing.T) {
	first, old := bundle(t, "unmanaged-baseline-cleanup")
	second, next := bundle(t, "unmanaged-candidate-cleanup")
	s := mechanicsStore(t)
	c := control(old, next)
	original := filepath.Join(first, old.Artifact.Filename)
	c.live = &buildinfo.Executable{Info: old.Build, Path: original, SHA256: old.Artifact.SHA256, PID: 123}
	c.badMetadata = true
	if err := s.Install(second, c); err == nil {
		t.Fatal("bad build accepted")
	}
	entries, err := os.ReadDir(filepath.Join(s.Dir, "activations"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("unpublished recovery copy leaked: %v %v", entries, err)
	}
	if c.stops != 0 || c.live == nil || c.live.Path != original {
		t.Fatal("original runtime disturbed")
	}
	if _, err := os.Stat(original); err != nil {
		t.Fatal("original image removed", err)
	}
}

// Default PID zero is not evidence that a selected or pending image is free.
// Every persisted recovery reference must survive a cleanup attempt.
func TestCleanupActivation_should_preserveEverySelectedReference(t *testing.T) {
	for _, role := range []string{"current", "previous", "pending"} {
		t.Run(role, func(t *testing.T) {
			s, a := stagedCleanupFixture(t)
			st := State{}
			switch role {
			case "current":
				st.Current, st.CurrentActivation = &a.Manifest, a.ID
			case "previous":
				st.Previous, st.PreviousActivation = &a.Manifest, a.ID
			case "pending":
				st.Pending, st.PendingActivation = &a.Manifest, a.ID
			}
			if err := s.save(st); err != nil {
				t.Fatal(err)
			}
			if err := s.CleanupActivation(a); err == nil {
				t.Error("selected activation removed")
			}
			if _, err := os.Stat(a.Binary); err != nil {
				t.Fatal("referenced image lost", err)
			}
		})
	}
}

// Cleanup owns only the original staged objects. Unknown files or a replaced
// directory must be retained, including their contents, without recursive rm.
func TestCleanupActivation_should_refuseChangedOwnership(t *testing.T) {
	for _, kind := range []string{"unknown-entry", "replaced-directory", "replaced-binary", "forged-receipt"} {
		t.Run(kind, func(t *testing.T) {
			s, a := stagedCleanupFixture(t)
			if kind == "replaced-directory" {
				if err := os.Rename(a.Dir, a.Dir+"-original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(a.Dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "replaced-binary" {
				if err := os.Rename(a.Binary, filepath.Join(s.Dir, "original-binary")); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(a.Binary, []byte("unowned"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			sentinel := filepath.Join(a.Dir, "unowned.txt")
			if kind == "unknown-entry" || kind == "replaced-directory" {
				if err := os.WriteFile(sentinel, []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				sentinel = a.Binary
			}
			if kind == "forged-receipt" {
				a = Activation{ID: a.ID, Dir: a.Dir, Binary: a.Binary, Manifest: a.Manifest}
			}
			before, err := os.ReadFile(sentinel)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.CleanupActivation(a); err == nil {
				t.Error("changed ownership accepted")
			}
			after, err := os.ReadFile(sentinel)
			if err != nil || string(before) != string(after) {
				t.Fatal("cleanup modified unowned content", err)
			}
		})
	}
}

// The no-owner bootstrap uses the same staging rules as installation; a
// rejected version probe must not leave an activation with no state reference.
func TestEnsureRunning_should_reclaimUnstartedCandidate_whenProbeRejects(t *testing.T) {
	dir, m := bundle(t, "bootstrap-probe-cleanup")
	s := mechanicsStore(t)
	c := control(m)
	c.badMetadata = true
	if err := s.EnsureRunning(dir, c); err == nil {
		t.Fatal("bad bootstrap accepted")
	}
	entries, err := os.ReadDir(filepath.Join(s.Dir, "activations"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("bootstrap snapshot leaked: %v %v", entries, err)
	}
}

// A confirmed failed candidate may be removed after the old state is restored;
// an unconfirmed candidate remains reachable through pending and is retained.
func TestInstall_should_reclaimOnlyConfirmedFailedCandidate(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(map[bool]string{false: "confirmed", true: "uncertain"}[uncertain], func(t *testing.T) {
			first, old := bundle(t, "cleanup-old")
			second, next := bundle(t, "cleanup-next")
			s := mechanicsStore(t)
			c := &uncertainControl{fakeControl: control(old, next)}
			if err := s.Install(first, c); err != nil {
				t.Fatal(err)
			}
			if uncertain {
				c.uncertainSHA = next.Artifact.SHA256
			} else {
				c.fail = next.Artifact.SHA256
			}
			err := s.Install(second, c)
			if err == nil || errors.Is(err, ErrCleanupUncertain) != uncertain {
				t.Fatal(err)
			}
			st, readErr := s.Read()
			if readErr != nil {
				t.Fatal(readErr)
			}
			entries, readErr := os.ReadDir(filepath.Join(s.Dir, "activations"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			want := 1
			if uncertain {
				want = 2
			}
			if len(entries) != want {
				t.Fatalf("wrong retained snapshots: %v", entries)
			}
			if uncertain && (st.Pending == nil || st.PendingActivation == "") {
				t.Fatal("uncertain image lost recovery reference", st)
			}
			if _, err := s.Activation(st.CurrentActivation, *st.Current); err != nil {
				t.Fatal("baseline removed", err)
			}
		})
	}
}

// A candidate rejected by version probing never reaches Start/Stop and has no
// durable recovery role. Its temporary activation must be reclaimed.
func TestInstall_should_reclaimUnstartedCandidate_whenProbeRejects(t *testing.T) {
	dir, m := bundle(t, "probe-rejected-cleanup")
	s := mechanicsStore(t)
	c := control(m)
	c.badMetadata = true
	if err := s.Install(dir, c); err == nil {
		t.Fatal("bad build accepted")
	}
	if c.stops != 0 {
		t.Fatal("stopped baseline before failed preflight")
	}
	entries, err := os.ReadDir(filepath.Join(s.Dir, "activations"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("unstarted activation leaked: %v", entries)
	}
}

// An uncertain probe may still own the staged image even though installation
// has not published pending. Never delete it or stop an unrelated baseline.
type cleanupProbeControl struct {
	*fakeControl
	foreign   bool
	candidate string
}

func (c *cleanupProbeControl) check(path, sha string) (buildinfo.Executable, error) {
	c.candidate = path
	if c.foreign {
		if err := os.WriteFile(filepath.Join(filepath.Dir(path), "unowned.txt"), []byte("keep"), 0600); err != nil {
			return buildinfo.Executable{}, err
		}
		return buildinfo.Executable{}, errors.New("probe failed")
	}
	return buildinfo.Executable{}, ErrCleanupUncertain
}

func TestInstall_should_retainCandidate_whenProbeOrCleanupIsUncertain(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		t.Run(map[bool]string{false: "probe", true: "foreign-content"}[foreign], func(t *testing.T) {
			dir, m := bundle(t, "uncertain-probe-cleanup")
			s := mechanicsStore(t)
			c := &cleanupProbeControl{fakeControl: control(m), foreign: foreign}
			err := s.Install(dir, c)
			if !errors.Is(err, ErrCleanupUncertain) {
				t.Fatal("cleanup uncertainty lost", err)
			}
			if _, err := os.Stat(c.candidate); err != nil {
				t.Fatal("uncertain candidate removed", err)
			}
			if foreign {
				if got, err := os.ReadFile(filepath.Join(filepath.Dir(c.candidate), "unowned.txt")); err != nil || string(got) != "keep" {
					t.Fatal("unowned content removed", err)
				}
			}
			if c.stops != 0 {
				t.Fatal("probe failure stopped baseline")
			}
		})
	}
}

// Incomplete state cannot establish which image is still reserved for recovery.
func TestCleanupActivation_should_refuseIncompleteInstallationState(t *testing.T) {
	s, a := stagedCleanupFixture(t)
	if err := s.save(State{Current: &a.Manifest}); err != nil {
		t.Fatal(err)
	}
	if err := s.CleanupActivation(a); err == nil {
		t.Fatal("incomplete state authorized cleanup")
	}
	if _, err := os.Stat(a.Binary); err != nil {
		t.Fatal(err)
	}
}
