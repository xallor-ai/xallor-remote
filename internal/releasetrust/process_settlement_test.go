//go:build windows || linux

package releasetrust

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/runtimeprocess"
)

// The copied test executable is a private process fixture, never signed proof.
func init() {
	dir := os.Getenv("XALLOR_PROOF_PROCESS_FIXTURE")
	if dir == "" {
		return
	}
	if os.Getenv("XALLOR_PROOF_DESCENDANT") == "1" {
		if os.Getenv("XALLOR_PROOF_DELAY_READY") == "1" {
			time.Sleep(6 * time.Second)
		}
		if os.WriteFile(filepath.Join(dir, "pid"), []byte(strconv.Itoa(os.Getpid())), 0600) != nil {
			os.Exit(2)
		}
		// Outlive the test's 15-second context: a natural exit must never be
		// mistaken for verifier cleanup. This is only an orphan watchdog.
		time.Sleep(30 * time.Second)
		os.Exit(3)
	}
	child := exec.Command(os.Args[0])
	child.Env = append(os.Environ(), "XALLOR_PROOF_DESCENDANT=1")
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	if child.Start() != nil {
		os.Exit(2)
	}
	_ = os.WriteFile(filepath.Join(dir, "spawned"), []byte(strconv.Itoa(child.Process.Pid)), 0600)
	// Only the test's release handshake should cause normal root exit. The
	// verifier context owns the tighter overall timeout and process cleanup.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, "release")); err == nil {
			os.Exit(0)
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = os.WriteFile(filepath.Join(dir, "root-timeout"), nil, 0600)
	os.Exit(3)
}

// Root exit with an inherited stdout descendant must settle promptly and prove
// descendant exit. The output is deliberately invalid: no crypto acceptance.
func TestVerify_shouldSettleDescendant_whenRootExitsWithInheritedStdout(t *testing.T) {
	verifyDescendantSettlement(t, false)
}

// A descendant delayed beyond the fixture's old five-second root timeout must
// still reach readiness before the test deliberately releases the verifier root.
func TestVerify_shouldSettleDescendant_whenDescendantReadinessIsDelayed(t *testing.T) {
	verifyDescendantSettlement(t, true)
}

func verifyDescendantSettlement(t *testing.T, delayed bool) {
	t.Helper()
	toolDir, fixture, candidate := t.TempDir(), t.TempDir(), t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	name := "gh"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err = os.WriteFile(filepath.Join(toolDir, name), data, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", toolDir)
	t.Setenv("XALLOR_PROOF_PROCESS_FIXTURE", fixture)
	t.Setenv("XALLOR_PROOF_DELAY_READY", "")
	if delayed {
		t.Setenv("XALLOR_PROOF_DELAY_READY", "1")
	}
	// The copied Go test executable can take more than one second to start
	// under the native process boundary on Windows. Keep the verifier context
	// bounded while allowing the fixture to publish its descendant identity.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	settled := make(chan struct{})
	var verifyErr error
	go func() {
		verifyErr = Verify(ctx, filepath.Join(candidate, "artifact"), Identity{Version: "0.2.2", Revision: strings.Repeat("a", 40)}, "", candidate, strings.Repeat("b", 64))
		close(settled)
	}()
	// Even an assertion before readiness must join verifier cleanup before
	// TempDir cleanup removes paths still in use by the supervised processes.
	defer func() {
		cancel()
		select {
		case <-settled:
		case <-time.After(7 * time.Second):
			t.Error("verifier cleanup did not settle after test cancellation")
		}
	}()
	var pid int
	// Process creation on a loaded Windows host can exceed three seconds when
	// the copied test binary is first scanned. Keep the observation budget
	// below the verifier context while avoiding a false negative before the
	// descendant can publish its PID.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-settled:
			spawned, _ := os.ReadFile(filepath.Join(fixture, "spawned"))
			_, timeoutErr := os.Stat(filepath.Join(fixture, "root-timeout"))
			t.Fatalf("verifier ended before readiness: spawned=%s root_timed_out=%v result=%v", spawned, timeoutErr == nil, verifyErr)
		default:
		}
		data, e := os.ReadFile(filepath.Join(fixture, "pid"))
		if e == nil {
			pid, _ = strconv.Atoi(string(data))
			if pid > 0 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		spawned, _ := os.ReadFile(filepath.Join(fixture, "spawned"))
		_, timeoutErr := os.Stat(filepath.Join(fixture, "root-timeout"))
		t.Fatalf("descendant did not become ready: spawned=%s root_timed_out=%v", spawned, timeoutErr == nil)
	}
	watch, err := runtimeprocess.WatchExit(pid)
	if err != nil {
		t.Fatal(err)
	}
	defer watch.Close()
	if exited, e := watch.Exited(); e != nil || exited {
		t.Fatalf("descendant was not live before releasing root: exited=%v error=%v", exited, e)
	}
	if err = os.WriteFile(filepath.Join(fixture, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	<-settled
	elapsed := time.Since(start)
	if !errors.Is(verifyErr, ErrUnverified) || errors.Is(verifyErr, ErrCleanupUncertain) {
		t.Errorf("expected proof refusal with confirmed cleanup: %v", verifyErr)
	}
	if ctx.Err() != nil {
		t.Errorf("context expiry substituted for root-exit cleanup: %v", ctx.Err())
	}
	if elapsed > 3*time.Second {
		t.Errorf("verification exceeded context after root exit: %v", elapsed)
	}
	exited, e := watch.Exited()
	if e != nil || !exited {
		t.Errorf("descendant not proven exited: %v %v", exited, e)
	}
}
