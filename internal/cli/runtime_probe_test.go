package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
)

// The real subprocess accepts the production version arguments without a shell.
func TestMain(m *testing.M) {
	if os.Getenv("XR_TEST_NATIVE_OWNER_FLOW") == "1" {
		os.Exit(runNativeOwnerFixture())
	}
	if os.Getenv("XR_TEST_STOP_TREE_DIR") != "" && len(os.Args) == 2 && (os.Args[1] == "--xr-tree-parent" || os.Args[1] == "--xr-tree-child") {
		os.Exit(runStopTreeFixture(os.Args[1]))
	}
	if os.Getenv("XR_TEST_STOP_PROCESS") == "1" {
		os.Exit(runStopProcessFixture())
	}
	if os.Getenv("XR_TEST_PUBLIC_START") == "1" {
		cmd := cmdStart()
		if err := cmd.RunE(cmd, nil); err != nil {
			os.Exit(48)
		}
		os.Exit(0)
	}
	if os.Getenv("XR_TEST_CAPTURE_LAUNCH") == "1" {
		os.Exit(runCapturedLaunchFixture())
	}
	if os.Getenv("XR_TEST_INTERNAL_SERVE") == "1" {
		cmd := cmdRuntimeServe()
		if err := cmd.RunE(cmd, nil); err != nil {
			os.Exit(48)
		}
		os.Exit(0)
	}
	if os.Getenv("XR_TEST_ACTUAL_RUNTIME") == "1" {
		// Isolated daemon tests cover stop/task supervision, not signed launch
		// admission. Production enters this loop only through runtime-serve.
		if err := runRuntimeService(nil, "", ""); err != nil {
			os.Exit(48)
		}
		os.Exit(0)
	}
	if mode := os.Getenv("XR_TEST_VERSION_PROBE"); mode != "" {
		if mode == "tree" || mode == "tree-child" || mode == "noisy" {
			os.Exit(runProbeResourceFixture(mode))
		}
		if mode == "launch-marker" {
			if err := os.WriteFile(os.Getenv("XR_TEST_LAUNCH_MARKER"), []byte("executed"), 0600); err != nil {
				os.Exit(25)
			}
			os.Exit(23)
		}
		if len(os.Args) != 3 || os.Args[1] != "version" || os.Args[2] != "--json" {
			os.Exit(24)
		}
		switch mode {
		case "slow":
			time.Sleep(6 * time.Second)
		case "hang":
			time.Sleep(time.Minute)
		case "fail":
			os.Exit(23)
		}
		if mode == "missing-owner" || mode == "wrong-owner" {
			raw, _ := json.Marshal(buildinfo.Running())
			var value map[string]any
			_ = json.Unmarshal(raw, &value)
			delete(value, "runtime_owner_protocol")
			if mode == "wrong-owner" {
				value["runtime_owner_protocol"] = 999
			}
			_ = json.NewEncoder(os.Stdout).Encode(value)
		} else {
			_ = json.NewEncoder(os.Stdout).Encode(buildinfo.Running())
		}
		os.Exit(0)
	}
	// A copied test binary is not a general CLI. Losing its explicit native
	// fixture mode must never turn version/start into another complete test run.
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-test.") && !strings.HasPrefix(os.Args[1], "--test.") {
		os.Exit(24)
	}
	// The dispatch regression isolates m.Run to one harmless sentinel, so a
	// broken version dispatch cannot recursively launch the native test suite.
	if os.Getenv("XR_TEST_DISPATCH_MARKER") != "" {
		if err := flag.Set("test.run", "^TestNativeFixtureDispatchSentinel$"); err != nil {
			os.Exit(25)
		}
	}
	os.Exit(m.Run())
}

// Signed identity/protocol fields alone do not prove that a candidate supports
// owner IPC. Detect old/future incompatible owners during preflight, not after
// installation has stopped the existing Runtime.
func TestCheck_should_rejectMissingOrIncompatibleOwnerProtocol(t *testing.T) {
	for _, mode := range []string{"missing-owner", "wrong-owner"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("XR_TEST_VERSION_PROBE", mode)
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := probeRuntimeIdentity(context.Background(), binary, buildinfo.Running().SHA256); err == nil {
				t.Fatal("candidate without matching owner contract passed preflight")
			}
		})
	}
}

// A healthy cold executable taking six seconds must report its real identity;
// the former five-second probe killed it before it could return any JSON.
func TestCheck_should_acceptSlowHealthyCandidate(t *testing.T) {
	t.Setenv("XR_TEST_VERSION_PROBE", "slow")
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	info, err := probeRuntimeIdentity(context.Background(), path, buildinfo.Running().SHA256)
	if err != nil || info.SHA256 != buildinfo.Running().SHA256 || info.PID == os.Getpid() {
		t.Fatalf("identity=%+v error=%v", info, err)
	}
}

// An unresponsive candidate is still killed and waited, and callers can
// distinguish deadline expiry from a genuine executable exit status.
func TestProbeRuntimeIdentity_should_reportDeadlineWhenCandidateHangs(t *testing.T) {
	t.Setenv("XR_TEST_VERSION_PROBE", "hang")
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = probeRuntimeIdentity(ctx, path, buildinfo.Running().SHA256)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline, got %v", err)
	}
}

// A candidate that exits itself remains a process failure, not a timeout.
func TestProbeRuntimeIdentity_should_preserveCandidateExit(t *testing.T) {
	t.Setenv("XR_TEST_VERSION_PROBE", "fail")
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err = probeRuntimeIdentity(ctx, path, buildinfo.Running().SHA256)
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 23 {
		t.Fatalf("expected exit 23, got %v", err)
	}
}
