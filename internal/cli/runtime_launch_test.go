package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"github.com/xallor-ai/xallor-remote/internal/runtimeprocess"
)

// This private native child models the already-admitted daemon side only.
// It validates pipe wiring/parent identity; source proof is tested separately.
func runCapturedLaunchFixture() int {
	input, err := os.Stdin.Stat()
	if err != nil || input.Mode()&os.ModeNamedPipe == 0 {
		return 60
	}
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 1025))
	if err != nil || len(raw) > 1024 {
		return 61
	}
	var permit release.LaunchPermit
	if json.Unmarshal(raw, &permit) != nil || permit.ParentPID != os.Getppid() {
		return 62
	}
	if err := os.WriteFile(filepath.Join(os.Getenv("XALLOR_REMOTE_DATA_DIR"), "launch-observed.json"), raw, 0600); err != nil {
		return 63
	}
	if err := runRuntimeService(nil, "", ""); err != nil {
		return 64
	}
	return 0
}

// A real native child receives launch context only through stdin. The
// foreground waiter remains pending until that child has actually exited.
func TestStart_should_transferLaunchPipe_andRetainForegroundLifetime(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("XALLOR_REMOTE_DATA_DIR", profile)
	t.Setenv("XALLOR_REMOTE_RELAY_URL", "ws://127.0.0.1:1/remote")
	t.Setenv("XR_TEST_CAPTURE_LAUNCH", "1")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	image, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(image)
	sha := hex.EncodeToString(sum[:])
	filename := "xallor-remote"
	if runtime.GOOS == "windows" {
		filename += ".exe"
	}
	id := strings.Repeat("a", 32)
	dir := filepath.Join(profile, "runtime", "activations", id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, filename)
	if err := os.WriteFile(binary, image, 0700); err != nil {
		t.Fatal(err)
	}
	m := release.Manifest{Schema: 1, Build: buildinfo.Info{Version: buildinfo.Version(), Revision: strings.Repeat("a", 40), Protocol: buildinfo.Protocol, ProfileFormat: buildinfo.ProfileFormat(), OS: runtime.GOOS, Arch: runtime.GOARCH},
		Artifact: release.Artifact{Filename: filename, Size: int64(len(image)), SHA256: sha}}
	raw, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	state, _ := json.Marshal(release.State{Pending: &m, PendingActivation: id})
	if err := os.WriteFile(filepath.Join(profile, "runtime", "state.json"), state, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	fg := &foregroundRuntime{stdout: &stdout, stderr: &stderr}
	control := runtimeControl{foreground: fg}
	defer func() { _ = control.Stop() }()
	if err := control.Start(binary, sha); err != nil {
		t.Fatal(err)
	}
	if fg.done == nil {
		t.Fatal("foreground child wait was discarded")
	}
	select {
	case result := <-fg.done:
		t.Fatal("foreground completed before stop", result)
	default:
	}
	received, err := os.ReadFile(filepath.Join(profile, "launch-observed.json"))
	if err != nil {
		t.Fatal(err)
	}
	var permit release.LaunchPermit
	if json.Unmarshal(received, &permit) != nil || permit.ParentPID != os.Getpid() || permit.ActivationID != id || permit.SHA256 != sha {
		t.Fatal("launch context not bound", permit)
	}
	// Exercise the same prepared public stop as control.Stop, retaining its
	// first observation so later cleanup cannot rewrite an uncertain result.
	stop, prepareErr := control.PrepareStop(context.Background(), release.StopRequest{AbsencePolicy: "public_observation"})
	if stop == nil {
		t.Fatal("no stop observer", prepareErr)
	}
	defer stop.Close()
	if prepareErr != nil {
		t.Fatal("stop preparation", prepareErr)
	}
	intent := stop.Intent()
	independent, watchErr := runtimeprocess.WatchExit(intent.PeerPID)
	if independent != nil {
		defer independent.Close()
	}
	observation, runErr := stop.Run(context.Background())
	closures, closeErr := stop.Close()
	stopErr := errors.Join(runErr, release.StopCleanup(release.ResourceObligation{}, closures, closeErr))
	if stopErr != nil {
		t.Logf("first stop pid=%d observation=%+v independent_watch_error=%v", intent.PeerPID, observation, watchErr)
		if independent != nil {
			exited, err := independent.Exited()
			t.Logf("independent process exit=%v error=%v", exited, err)
		}
		select {
		case result := <-fg.done:
			// Buffer writers have settled only after the foreground wait result.
			t.Logf("foreground settled: wait=%v cleanup=%v stderr=%q", result.waitErr, result.cleanupError(), stderr.String())
		default:
			t.Log("foreground wait still pending; stderr not read concurrently")
		}
		t.Fatal("first stop failed", stopErr)
	}
	if independent == nil || watchErr != nil {
		t.Fatal("independent process exit watch unavailable", watchErr)
	}
	if exited, err := independent.Exited(); err != nil || !exited {
		t.Fatal("first stop returned before independent process exit", err)
	}
	select {
	case result := <-fg.done:
		if result.waitErr != nil || result.cleanupError() != nil {
			t.Fatal(result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("foreground exit not observed")
	}
}
