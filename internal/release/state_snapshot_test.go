package release

import (
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

// A concurrent reader keeps a consistent old snapshot while an atomic state
// replacement succeeds. Windows must not deny the writer's rename operation.
func TestStateSnapshotReaderAllowsAtomicSelectionReplacement(t *testing.T) {
	_, old := bundle(t, "snapshot-old")
	_, next := bundle(t, "snapshot-next")
	s := mechanicsStore(t)
	if err := s.save(State{Current: &old, CurrentActivation: strings.Repeat("a", 32)}); err != nil {
		t.Fatal(err)
	}
	f, err := openStateSnapshot(filepath.Join(s.Dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := s.save(State{Current: &next, CurrentActivation: strings.Repeat("b", 32)}); err != nil {
		t.Fatal("reader prevented atomic replacement", err)
	}
	raw, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	var observed State
	if err := json.Unmarshal(raw, &observed); err != nil {
		t.Fatal(err)
	}
	if observed.Current == nil || *observed.Current != old {
		t.Fatal("reader did not retain old snapshot")
	}
	latest, err := s.Read()
	if err != nil || latest.Current == nil || *latest.Current != next {
		t.Fatal("new readers did not observe committed state", latest, err)
	}
}
