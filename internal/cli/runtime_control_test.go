package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/release"
)

// Startup/shutdown can hold the Runtime lifetime lock before/after IPC exists.
// Endpoint absence cannot certify termination while that lock remains held.
func TestStop_should_notClaimStopped_whenRuntimeLockIsHeldWithoutIPC(t *testing.T) {
	t.Setenv("XALLOR_REMOTE_DATA_DIR", t.TempDir())
	unlock, err := lockRuntime()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err := (runtimeControl{}).Stop(); !errors.Is(err, release.ErrCleanupUncertain) {
		t.Fatal("missing IPC incorrectly certified process stop", err)
	}
}

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

// A digest known before launch must be enforced before the candidate executes.
// Reporting a later readiness mismatch is too late to undo candidate effects.
func TestStart_should_refuseChangedImage_beforeExecutingCandidate(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("XALLOR_REMOTE_DATA_DIR", profile)
	t.Setenv("XR_TEST_VERSION_PROBE", "launch-marker")
	marker := filepath.Join(profile, "executed")
	t.Setenv("XR_TEST_LAUNCH_MARKER", marker)
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := (runtimeControl{}).Start(bin, strings.Repeat("0", 64)); err == nil {
		t.Fatal("changed image was accepted")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("candidate executed before expected digest was enforced", err)
	}
}

// An absent owner must not make the calling executable an implicitly trusted
// bootstrap. The test binary has no signed manifest; its marker must not run.
func TestEnsureRuntime_should_refuseUnverifiedBootstrap_beforeExecution(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("XALLOR_REMOTE_DATA_DIR", profile)
	t.Setenv("XR_TEST_VERSION_PROBE", "launch-marker")
	marker := filepath.Join(profile, "executed")
	t.Setenv("XR_TEST_LAUNCH_MARKER", marker)
	if err := ensureRuntime(); err == nil {
		t.Fatal("unverified bootstrap was accepted")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("unverified bootstrap executed", err)
	}
	if _, err := os.Stat(filepath.Join(profile, "runtime", "state.json")); !os.IsNotExist(err) {
		t.Fatal("unverified bootstrap created selection state", err)
	}
}
