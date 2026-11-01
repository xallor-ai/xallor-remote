package identity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A renamed device keeps its routing identity and grant, including after reload and grant replacement.
func TestRenamePeer_should_persistOnlyDisplayMetadata(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XALLOR_REMOTE_DATA_DIR", dir)
	s, err := LoadFrom(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.AddPeer("123456789", "original"); err != nil {
		t.Fatal(err)
	}
	if err = s.RenamePeer("123456789", "  工作电脑  "); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadFrom(dir)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := reloaded.Peer("123456789")
	if !ok || p.Name != "工作电脑" || p.Grant != "original" {
		t.Fatal("rename changed credentials or was lost")
	}
	if err = reloaded.AddPeer("123456789", "updated"); err != nil {
		t.Fatal(err)
	}
	p, _ = reloaded.Peer("123456789")
	if p.Name != "工作电脑" || p.Grant != "updated" {
		t.Fatal("grant update lost display name")
	}
	if reloaded.RenamePeer("123456789", strings.Repeat("字", 81)) == nil {
		t.Fatal("unbounded name accepted")
	}
	if reloaded.RenamePeer("987654321", "unknown") == nil {
		t.Fatal("unknown peer accepted")
	}
	if err = reloaded.RenamePeer("123456789", ""); err != nil {
		t.Fatal(err)
	}
}

// Failure to persist a rename must preserve the in-memory credential and previous label.
func TestRenamePeer_should_preserveState_whenWriteFails(t *testing.T) {
	badDir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(badDir, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	s := &Store{Dir: badDir, Peers: []Peer{{DeviceID: "123456789", Grant: "secret", Name: "before"}}}
	if s.RenamePeer("123456789", "after") == nil {
		t.Fatal("write failure ignored")
	}
	p, _ := s.Peer("123456789")
	if p.Name != "before" || p.Grant != "secret" {
		t.Fatal("failed rename changed state")
	}
}
