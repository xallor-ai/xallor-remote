package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/filelock"
)

// An already source-authorized child can arrive here after recovery revoked
// pending selection. Recheck under the service lock before identity or IPC.
func TestServiceRechecksRevokedAdmissionBeforeInitializingIdentity(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("XALLOR_REMOTE_DATA_DIR", profile)
	revoked := errors.New("pending launch was revoked by recovery")
	calls := 0
	err := runRuntimeService(func() (bool, error) {
		calls++
		if unlock, err := filelock.Acquire(filepath.Join(profile, "runtime.lock")); err == nil {
			unlock()
			t.Error("admission check did not own service lock")
		}
		return false, revoked
	}, "", "")
	if !errors.Is(err, revoked) || calls != 1 {
		t.Fatal("revoked launch was not rejected at entry", calls, err)
	}
	for _, name := range []string{"identity.db", "workspace", "ipc.sock"} {
		if _, err := os.Stat(filepath.Join(profile, name)); !os.IsNotExist(err) {
			t.Fatal("revoked launch created state", name, err)
		}
	}
}
