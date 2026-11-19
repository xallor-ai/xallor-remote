//go:build windows

package process

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

type lateOverflowBoundary struct{ *job }

func (b lateOverflowBoundary) stop() error {
	err := b.job.stop()
	if err == nil {
		// Gate the same post-stop state as an observer completing a previously
		// acquired snapshot. Stop already returned successful capture evidence.
		b.joinProcessObserver()
		b.exitMu.Lock()
		b.observationOverflow = true
		b.exitMu.Unlock()
	}
	return err
}

// An observer can finish after the final stop capture. Its sticky overflow
// must still reach Child.Wait as ErrCleanup; waiting a subset is insufficient.
func TestProcess_should_reportCleanupUncertainty_whenObserverOverflowsAfterStop(t *testing.T) {
	cmd := helper("output")
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	raw, err := prepare(cmd)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	child, err := startPrepared(ctx, cmd, lateOverflowBoundary{raw.(*job)})
	if err != nil {
		t.Fatal(err)
	}
	defer child.Stop()
	if err := child.Wait(); !errors.Is(err, ErrCleanup) {
		t.Fatal("late observer overflow was forgotten", err)
	}
}

func startSnapshotJob(t *testing.T, cmd *exec.Cmd) *job {
	t.Helper()
	raw, err := prepare(cmd)
	if err != nil {
		t.Fatal(err)
	}
	j := raw.(*job)
	if err := j.start(cmd); err != nil {
		_ = j.close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = j.stop()
		_ = j.waitRoot()
		_ = j.waitExitHandles(time.Now().Add(5 * time.Second))
		_ = j.wait(cmd)
		_ = j.close()
	})
	if err := j.attach(cmd); err != nil {
		t.Fatal(err)
	}
	return j
}

// Replay the actual successful OS reply (assigned=1/listed=0) from the input
// E2E failure. Early native handles cover the root and OS console host; a
// partial later PID list must not lose that already retained exit evidence.
func TestJob_should_acceptPartialList_whenAllHistoricalProcessesAreHeld(t *testing.T) {
	// Use the same plain Go program as input E2E, including its console host.
	fixture := filepath.Join(t.TempDir(), "argv-report.exe")
	build := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go.exe"), "build", "-o", fixture, "../../tests/fixtures/argv-report")
	HideConsole(build)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build native fixture: %v: %s", err, out)
	}
	cmd := exec.Command(fixture)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	j := startSnapshotJob(t, cmd)
	if err := j.waitRoot(); err != nil {
		t.Fatal(err)
	}
	count, err := j.accounting()
	if err != nil || count.TotalProcesses == 0 {
		t.Fatal(count, err)
	}
	j.stopPopulation = &count.TotalProcesses
	if err := j.observeExitHandles(jobProcessList{assigned: 1, count: 0}); err != nil {
		t.Fatal("retained process handles incorrectly discarded", err)
	}
	if err := j.stop(); err != nil {
		t.Fatal(err)
	}
	if err := j.settle(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-j.observed:
	default:
		t.Fatal("process observer survived final settlement")
	}
}

// The live observer must not retain unbounded handles for a long-running
// command. Once its observation budget is exceeded, even a later empty
// snapshot cannot erase the incomplete observation.
func TestJob_should_keepObservationUncertain_whenRetentionLimitExceeded(t *testing.T) {
	j := &job{exits: make(map[uint32]windows.Handle)}
	for i := uint32(1); i <= jobProcessCapacity; i++ {
		// Pure validation: no native calls or close() use these sentinel values.
		j.exits[i] = 0
	}
	list := jobProcessList{assigned: 1, count: 1}
	list.pids[0] = jobProcessCapacity + 1
	if err := j.observeExitHandles(list); err == nil {
		t.Fatal("retained handle limit was not enforced")
	}
	if err := j.observeExitHandles(jobProcessList{}); err == nil {
		t.Fatal("empty snapshot erased prior observation overflow")
	}
}

// Retaining the native root/console handles is not proof that they signalled.
func TestJob_should_refuseExitConfirmation_whenHeldRootStillRunning(t *testing.T) {
	cmd := helper("wait")
	cmd.Env = append(cmd.Env, "XR_PROCESS_READY="+filepath.Join(t.TempDir(), "ready"))
	j := startSnapshotJob(t, cmd)
	count, err := j.accounting()
	if err != nil || count.TotalProcesses == 0 {
		t.Fatal(count, err)
	}
	j.stopPopulation = &count.TotalProcesses
	if err := j.waitExitHandles(time.Now()); err == nil {
		t.Fatal("live held processes were certified exited")
	}
}

// A signalled root cannot stand in for a child that remains in the job. Feed
// the identical partial reply while holding a real child process exit handle.
func TestJob_should_refuseOmittedDescendant_whenRootHasExited(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestJob_should_reclaimDescendant_whenParentExitsNormally$")
	cmd.Env = append(os.Environ(), "XR_JOB_LIFETIME_ROLE=parent", "XR_JOB_LIFETIME_DIR="+dir)
	j := startSnapshotJob(t, cmd)
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	var childPID int
	for {
		data, readErr := os.ReadFile(filepath.Join(dir, "child.pid"))
		pid, parseErr := strconv.Atoi(string(data))
		if readErr == nil && parseErr == nil && pid > 0 {
			childPID = pid
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child was not created")
		}
		time.Sleep(time.Millisecond)
	}
	childHandle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(childPID))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(childHandle)
	if err := os.WriteFile(filepath.Join(dir, "parent-exit"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := j.waitRoot(); err != nil {
		t.Fatal(err)
	}
	count, _ := j.accounting()
	j.joinProcessObserver()
	// Model an observation gap even if the background sampler caught this
	// child on this run. Keep the independent test handle for real cleanup.
	if h, ok := j.exits[uint32(childPID)]; ok {
		delete(j.exits, uint32(childPID))
		_ = windows.CloseHandle(h)
	}
	j.stopPopulation = &count.TotalProcesses
	if err := j.observeExitHandles(jobProcessList{assigned: 1}); err == nil {
		t.Fatal("root exit concealed an unobserved descendant")
	}
	if err := j.stop(); err != nil {
		t.Fatal(err)
	}
	if err := j.waitExitHandles(deadline); err != nil {
		t.Fatal(err)
	}
	if state, err := windows.WaitForSingleObject(childHandle, 0); err != nil || state != windows.WAIT_OBJECT_0 {
		t.Fatal("native test child did not exit", state, err)
	}
}
