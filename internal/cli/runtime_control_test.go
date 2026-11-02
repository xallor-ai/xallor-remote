package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// A missing socket/pipe is a stopped Runtime, including ENOENT nested in net.OpError on Unix.
func TestRunning_should_returnStopped_whenProfileDoesNotExist(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "absent")
	t.Setenv("XALLOR_REMOTE_DATA_DIR", profile)
	live, err := (runtimeControl{}).Running()
	if err != nil || live != nil {
		t.Fatalf("live=%v err=%v", live, err)
	}
	if _, err := os.Stat(profile); !os.IsNotExist(err) {
		t.Fatal("read-only check created profile")
	}
}
