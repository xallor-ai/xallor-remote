package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/process"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"github.com/xallor-ai/xallor-remote/internal/runtimeowner"
	"github.com/xallor-ai/xallor-remote/internal/runtimeprocess"
)

// Explicit native fixtures check release even after delayed startup. The
// twenty-second watchdog is a fallback, never a successful settlement signal.
func init() {
	dir := os.Getenv("XR_LAUNCHER_TEST_DIR")
	if len(os.Args) == 2 && os.Args[1] == "--launcher-output-holder" {
		if os.WriteFile(filepath.Join(dir, "ready"), []byte(strconv.Itoa(os.Getpid())), 0600) != nil {
			os.Exit(11)
		}
		waitLauncherFixtureMarker(dir, "release")
		os.Exit(0)
	}
	if len(os.Args) == 4 && os.Args[3] == "launcher-inherited-output" {
		if os.WriteFile(filepath.Join(dir, "parent-pid"), []byte(strconv.Itoa(os.Getpid())), 0600) != nil {
			os.Exit(8)
		}
		if _, err := os.Stat(filepath.Join(dir, "hold-before-child")); err == nil {
			waitLauncherFixtureMarker(dir, "release")
			os.Exit(0)
		}
		if _, err := os.Stat(filepath.Join(dir, "release")); err == nil {
			os.Exit(0)
		}
		child := exec.Command(os.Getenv("XR_LAUNCHER_TEST_EXE"), "--launcher-output-holder")
		process.HideConsole(child)
		child.Stdout = os.Stdout
		if child.Start() != nil {
			os.Exit(9)
		}
		if os.WriteFile(filepath.Join(dir, "pid"), []byte(strconv.Itoa(child.Process.Pid)), 0600) != nil {
			os.Exit(10)
		}
		waitLauncherFixtureMarker(dir, "root-release")
		os.Exit(0)
	}
}

func waitLauncherFixtureMarker(dir, name string) {
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return
		}
		if _, err := os.Stat(filepath.Join(dir, "release")); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type launcherFixtureResult struct {
	attempt runtimeowner.Attempt
	err     error
}
type nativeLauncherFixture struct {
	dir           string
	result        <-chan launcherFixtureResult
	observed      *launcherFixtureResult
	parent, child runtimeprocess.ExitWatch
}

func newNativeLauncherFixture(t *testing.T, ctx context.Context, hold bool) *nativeLauncherFixture {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XR_LAUNCHER_TEST_EXE", self)
	dir := t.TempDir()
	t.Setenv("XR_LAUNCHER_TEST_DIR", dir)
	if hold {
		if err := os.WriteFile(filepath.Join(dir, "hold-before-child"), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	command, err := runtimeowner.NewCommand("rollback", "")
	if err != nil {
		t.Fatal(err)
	}
	request, _ := command.Request()
	result := make(chan launcherFixtureResult, 1)
	f := &nativeLauncherFixture{dir: dir, result: result}
	t.Cleanup(func() { f.cleanup(t) })
	go func() {
		a, err := launchTransactionImage(ctx, command, release.OwnerHandoff{OperationID: request.ID, IntentSHA256: request.IntentSHA256, ImageSHA256: hex.EncodeToString(sum[:])}, release.Activation{Binary: self}, "launcher-inherited-output", dir)
		result <- launcherFixtureResult{a, err}
	}()
	return f
}

func (f *nativeLauncherFixture) waitReady(t *testing.T, ctx context.Context, child bool) {
	t.Helper()
	for {
		parent, _ := os.ReadFile(filepath.Join(f.dir, "parent-pid"))
		parentPID, _ := strconv.Atoi(string(parent))
		ready, _ := os.ReadFile(filepath.Join(f.dir, "ready"))
		childPID, _ := strconv.Atoi(string(ready))
		if parentPID > 0 && (!child || childPID > 0) {
			var err error
			f.parent, err = runtimeprocess.WatchExit(parentPID)
			if err != nil {
				t.Fatal("parent not observable before release", err)
			}
			if exited, err := f.parent.Exited(); err != nil || exited {
				t.Fatal("parent exited before release", err)
			}
			if child {
				f.child, err = runtimeprocess.WatchExit(childPID)
				if err != nil {
					t.Fatal("descendant not observable before release", err)
				}
				if exited, err := f.child.Exited(); err != nil || exited {
					t.Fatal("descendant not alive before parent release", err)
				}
			}
			if err := ctx.Err(); err != nil {
				t.Fatal("readiness arrived after original deadline", err)
			}
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("fixture startup readiness not observed; no inherited-output diagnosis", ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func (f *nativeLauncherFixture) receive(ctx context.Context) (*launcherFixtureResult, error) {
	if f.observed != nil {
		return f.observed, nil
	}
	select {
	case result := <-f.result:
		f.observed = &result
		return f.observed, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *nativeLauncherFixture) cleanup(t *testing.T) {
	// Write before reading PIDs: late helpers must see cleanup as well.
	for _, name := range []string{"release", "root-release"} {
		if err := os.WriteFile(filepath.Join(f.dir, name), nil, 0600); err != nil {
			t.Error("fixture release marker", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// Extra waiting is cleanup after a recorded failure, never an enlarged test
	// assertion. One fixed grace deadline spans all late-resource observations.
	grace, stopGrace := context.WithTimeout(context.Background(), 23*time.Second)
	defer stopGrace()
	result, err := f.receive(ctx)
	if err != nil {
		t.Error("launcher result not joined during cleanup", err)
		result, err = f.receive(grace)
	}
	if err != nil {
		t.Error("late launcher still unobserved", err)
	}
	if err == nil && result.attempt != nil {
		select {
		case <-result.attempt.Done():
		case <-ctx.Done():
			t.Error("owned launcher exceeded cleanup observation")
			select {
			case <-result.attempt.Done():
			case <-grace.Done():
				t.Error("late launcher still unsettled")
			}
		}
		select {
		case <-result.attempt.Done():
			if err := result.attempt.Result(); err != nil {
				t.Error("launcher cleanup", err)
			}
		default:
		}
	}
	readLateWatch := func(name string) runtimeprocess.ExitWatch {
		raw, err := os.ReadFile(filepath.Join(f.dir, name))
		if err != nil {
			if !os.IsNotExist(err) {
				t.Error("late PID unavailable", name, err)
			}
			return nil
		}
		pid, err := strconv.Atoi(string(raw))
		if err != nil {
			t.Error("late PID malformed", name, err)
			return nil
		}
		watch, err := runtimeprocess.WatchExit(pid)
		if err != nil && !errors.Is(err, runtimeprocess.ErrProcessGone) {
			t.Error("late process observation unavailable", name, err)
		}
		return watch
	}
	if f.parent == nil {
		f.parent = readLateWatch("parent-pid")
	}
	if f.child == nil {
		f.child = readLateWatch("pid")
	}
	if f.parent == nil {
		t.Error("parent process was never observed or explicitly proven absent")
	}
	if f.child == nil {
		if _, err := os.Stat(filepath.Join(f.dir, "hold-before-child")); err != nil {
			t.Error("child process was never observed or explicitly proven absent")
		}
	}
	for _, watch := range []runtimeprocess.ExitWatch{f.parent, f.child} {
		if watch == nil {
			continue
		}
		if err := watch.Wait(ctx); err != nil {
			t.Error("fixture process exceeded cleanup observation", err)
			if err := watch.Wait(grace); err != nil {
				t.Error("fixture process cleanup unconfirmed", err)
			}
		}
		if err := watch.Close(); err != nil {
			t.Error(err)
		}
	}
}

// Retain kernel watches before releasing the parent. Measure parent-exit ->
// settlement, not binary preparation/startup. The child must still hold stdout
// and the original context must not have timed out to make the reader finish.
func TestLauncher_should_joinAcceptance_whenDescendantHoldsOutput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	f := newNativeLauncherFixture(t, ctx, false)
	f.waitReady(t, ctx, true)
	if err := os.WriteFile(filepath.Join(f.dir, "root-release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := f.parent.Wait(ctx); err != nil {
		t.Fatal("parent exit not observed", err)
	}
	settle, finish := context.WithTimeout(context.Background(), 2*time.Second)
	defer finish()
	result, err := f.receive(settle)
	if err != nil || result.attempt == nil || result.err == nil {
		t.Fatal("expected owned uncertain acceptance after parent exit", err)
	}
	select {
	case <-result.attempt.Done():
	case <-settle.Done():
		t.Fatal("acceptance did not settle after confirmed parent exit")
	}
	if err := ctx.Err(); err != nil {
		t.Fatal("context expiry masked parent-exit settlement", err)
	}
	if err := result.attempt.Result(); err != nil {
		t.Fatal("launcher cleanup failed", err)
	}
	if exited, err := f.child.Exited(); err != nil || exited {
		t.Fatal("test did not preserve descendant-held stdout", err)
	}
}

// Cancelling observation is not process exit. The owned fixture is released by
// cleanup and joined without retrying the launch or stopping a real service.
func TestLauncher_shouldJoinFixture_whenObservationCancelledBeforeChildStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := newNativeLauncherFixture(t, ctx, true)
	ready, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	f.waitReady(t, ready, false)
	cancel()
	result, err := f.receive(ready)
	if err != nil || result.attempt == nil || result.err == nil {
		t.Fatal("cancelled observation lost owned attempt", err)
	}
	if exited, err := f.parent.Exited(); err != nil || exited {
		t.Fatal("observation cancellation killed fixture", err)
	}
	select {
	case <-result.attempt.Done():
		t.Fatal("live parent reported settled")
	default:
	}
}
