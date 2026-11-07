package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
)

// The real subprocess accepts the production version arguments without a shell.
func TestMain(m *testing.M) {
	if mode := os.Getenv("XR_TEST_VERSION_PROBE"); mode != "" {
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
		_ = json.NewEncoder(os.Stdout).Encode(buildinfo.Running())
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// A healthy cold executable taking six seconds must report its real identity;
// the former five-second probe killed it before it could return any JSON.
func TestCheck_should_acceptSlowHealthyCandidate(t *testing.T) {
	t.Setenv("XR_TEST_VERSION_PROBE", "slow")
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	info, err := (runtimeControl{}).Check(path)
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
	_, err = probeRuntimeIdentity(ctx, path)
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
	_, err = probeRuntimeIdentity(ctx, path)
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 23 {
		t.Fatalf("expected exit 23, got %v", err)
	}
}
