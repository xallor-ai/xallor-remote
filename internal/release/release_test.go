package release

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func bundle(t *testing.T, label string) (string, Manifest) {
	t.Helper()
	dir := t.TempDir()
	content := []byte("fake-test-artifact-" + label)
	sum := sha256.Sum256(content)
	name := "xallor-remote"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	m := Manifest{Schema: 1, Build: buildinfo.Info{Version: "0.2.0", Revision: label, Protocol: buildinfo.Protocol, ProfileFormat: buildinfo.ProfileFormat(), OS: runtime.GOOS, Arch: runtime.GOARCH}, Artifact: Artifact{Filename: name, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(content))}}
	os.WriteFile(filepath.Join(dir, name), content, 0700)
	writeManifest(t, dir, m)
	return dir, m
}
func writeManifest(t *testing.T, dir string, m Manifest) {
	t.Helper()
	b, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
}

type fakeControl struct {
	builds      map[string]buildinfo.Info
	live        *buildinfo.Executable
	fail        string
	badMetadata bool
	stops       int
}

func (f *fakeControl) Running() (*buildinfo.Executable, error) { return f.live, nil }
func (f *fakeControl) Stop() error                             { f.stops++; f.live = nil; return nil }
func (f *fakeControl) Check(path string) (buildinfo.Executable, error) {
	sum, err := buildinfo.HashFile(path)
	if err != nil {
		return buildinfo.Executable{}, err
	}
	i := f.builds[sum]
	if f.badMetadata {
		i.Revision = "wrong"
	}
	return buildinfo.Executable{Info: i, Path: path, SHA256: sum, PID: 123}, nil
}
func (f *fakeControl) Start(path, sum string) error {
	if sum == f.fail {
		return errors.New("candidate failed health check")
	}
	e, err := f.Check(path)
	if err == nil {
		f.live = &e
	}
	return err
}
func control(manifests ...Manifest) *fakeControl {
	f := &fakeControl{builds: map[string]buildinfo.Info{}}
	for _, m := range manifests {
		f.builds[m.Artifact.SHA256] = m.Build
	}
	return f
}

// Installation, upgrade and rollback must select verified artifacts without editing user data.
func TestInstall_should_upgradeAndRollback(t *testing.T) {
	a, ma := bundle(t, "a")
	b, mb := bundle(t, "b")
	s := Store{Dir: t.TempDir()}
	c := control(ma, mb)
	sentinel := filepath.Join(s.Dir, "identity-sentinel")
	os.WriteFile(sentinel, []byte("unchanged"), 0600)
	for _, d := range []string{a, b} {
		if err := s.Install(d, c); err != nil {
			t.Fatal(err)
		}
	}
	st, _ := s.Read()
	if st.Current.Artifact.SHA256 != mb.Artifact.SHA256 || st.Previous.Artifact.SHA256 != ma.Artifact.SHA256 || st.Pending != nil {
		t.Fatalf("%+v", st)
	}
	if err := s.Rollback(c); err != nil {
		t.Fatal(err)
	}
	st, _ = s.Read()
	if st.Current.Artifact.SHA256 != ma.Artifact.SHA256 || c.live.SHA256 != ma.Artifact.SHA256 {
		t.Fatal("rollback did not restore a")
	}
	got, _ := os.ReadFile(sentinel)
	if string(got) != "unchanged" {
		t.Fatal("user data changed")
	}
}

// Corrupt archives, traversal and unsupported metadata must fail before stopping a live Runtime.
func TestInstall_should_rejectInvalidBundleBeforeStop(t *testing.T) {
	for _, kind := range []string{"checksum", "size", "path", "platform", "protocol", "metadata"} {
		t.Run(kind, func(t *testing.T) {
			dir, m := bundle(t, "invalid")
			c := control(m)
			switch kind {
			case "checksum":
				os.WriteFile(filepath.Join(dir, m.Artifact.Filename), []byte("tampered"), 0700)
			case "size":
				m.Artifact.Size++
			case "path":
				m.Artifact.Filename = "../escape"
			case "platform":
				m.Build.OS = "unsupported"
			case "protocol":
				m.Build.Protocol++
			case "metadata":
				c.badMetadata = true
			}
			writeManifest(t, dir, m)
			if err := (Store{Dir: t.TempDir()}).Install(dir, c); err == nil {
				t.Fatal("invalid release accepted")
			}
			if c.stops != 0 {
				t.Fatal("invalid release interrupted running service")
			}
		})
	}
}

// Failed activation must restore the last verified process and clear pending state.
func TestInstall_should_restorePrevious_whenCandidateFails(t *testing.T) {
	a, ma := bundle(t, "a")
	b, mb := bundle(t, "b")
	s := Store{Dir: t.TempDir()}
	c := control(ma, mb)
	if err := s.Install(a, c); err != nil {
		t.Fatal(err)
	}
	c.fail = mb.Artifact.SHA256
	if err := s.Install(b, c); err == nil {
		t.Fatal("failed activation reported success")
	}
	st, _ := s.Read()
	if st.Pending != nil || st.Current.Artifact.SHA256 != ma.Artifact.SHA256 || c.live.SHA256 != ma.Artifact.SHA256 {
		t.Fatal("previous version not recovered")
	}
}

// Corruption of installed bytes must never fall back silently to another executable.
func TestActivePath_should_failClosed_whenArtifactIsChanged(t *testing.T) {
	a, m := bundle(t, "a")
	s := Store{Dir: t.TempDir()}
	c := control(m)
	if err := s.Install(a, c); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(s.Path(m), []byte("corrupt"), 0700)
	if _, err := s.ActivePath(); err == nil {
		t.Fatal("corrupt artifact accepted")
	}
}

// An interrupted transition must be explicitly recoverable without retrying the candidate.
func TestRollback_should_recoverPendingActivation(t *testing.T) {
	a, ma := bundle(t, "a")
	_, mb := bundle(t, "b")
	s := Store{Dir: t.TempDir()}
	c := control(ma, mb)
	if err := s.Install(a, c); err != nil {
		t.Fatal(err)
	}
	st, _ := s.Read()
	st.Pending = &mb
	s.save(st)
	if err := s.Rollback(c); err != nil {
		t.Fatal(err)
	}
	st, _ = s.Read()
	if st.Pending != nil || st.Current.Artifact.SHA256 != ma.Artifact.SHA256 {
		t.Fatal("pending transition not recovered")
	}
}

// Live installer locks must reject concurrent mutations.
func TestInstall_should_rejectConcurrentInstaller(t *testing.T) {
	a, m := bundle(t, "a")
	s := Store{Dir: t.TempDir()}
	unlock, err := s.Lock()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err = s.Install(a, control(m)); err == nil {
		t.Fatal("concurrent install accepted")
	}
}

// Even an interrupted first install has a recovery path without fabricating a previous version.
func TestRollback_should_clearInterruptedFirstInstall(t *testing.T) {
	_, m := bundle(t, "initial")
	s := Store{Dir: t.TempDir()}
	if err := s.save(State{Pending: &m}); err != nil {
		t.Fatal(err)
	}
	if err := s.Rollback(control(m)); err != nil {
		t.Fatal(err)
	}
	st, err := s.Read()
	if err != nil || st.Pending != nil || st.Current != nil {
		t.Fatalf("%+v %v", st, err)
	}
}

// Cache hits must never bypass validation of a newly supplied release bundle.
func TestInstall_should_rejectCorruptInput_evenWhenArtifactIsCached(t *testing.T) {
	a, ma := bundle(t, "a")
	b, mb := bundle(t, "b")
	s := Store{Dir: t.TempDir()}
	c := control(ma, mb)
	for _, dir := range []string{a, b} {
		if err := s.Install(dir, c); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Rollback(c); err != nil {
		t.Fatal(err)
	}
	stops := c.stops
	if err := os.WriteFile(filepath.Join(b, mb.Artifact.Filename), []byte("tampered"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.Install(b, c); err == nil {
		t.Fatal("corrupt cached bundle accepted")
	}
	if c.stops != stops || c.live.SHA256 != ma.Artifact.SHA256 {
		t.Fatal("corrupt bundle interrupted healthy runtime")
	}
}
