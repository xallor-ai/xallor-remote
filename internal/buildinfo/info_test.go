package buildinfo

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// A running artifact must expose a usable identity, not a hardcoded dev marker.
func TestCurrent_should_identifyExecutingArtifact(t *testing.T) {
	i := Current()
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(i.Version) || i.Protocol != 1 || i.Revision == "" || i.OS == "" {
		t.Fatalf("%+v", i)
	}
	e := Running()
	if e.PID != os.Getpid() || len(e.SHA256) != 64 || e.Path == "" {
		t.Fatalf("%+v", e)
	}
}

// A modified artifact must not keep the same checksum.
func TestHashFile_should_change_whenBytesChange(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bin")
	os.WriteFile(p, []byte("one"), 0600)
	a, err := HashFile(p)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(p, []byte("two"), 0600)
	b, _ := HashFile(p)
	if a == b {
		t.Fatal("unchanged digest")
	}
}
