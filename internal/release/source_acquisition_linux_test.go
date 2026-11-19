package release

import (
	"os"
	"path/filepath"
	"testing"
)

// Procfs observes both the original OpenRoot descriptor and binary descriptor.
func assertSourceInputsOpen(t *testing.T, dir, binary string, wantOpen bool) {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{dir, filepath.Join(dir, binary)} {
		found := false
		for _, entry := range entries {
			target, err := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
			if err == nil && target == path {
				found = true
			}
		}
		if found != wantOpen {
			t.Errorf("original input %s open=%v, want %v at source closure", path, found, wantOpen)
		}
	}
}
