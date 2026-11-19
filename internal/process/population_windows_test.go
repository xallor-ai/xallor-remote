//go:build windows

package process

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Hold the native parent at its child-creation gate, snapshot the population,
// then let it create a descendant. Even if that descendant is caught by a
// later snapshot, termination must not certify the earlier incomplete fence.
func TestJob_should_refuseCleanupConfirmation_whenChildIsBornAfterPopulationSnapshot(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestJob_should_reclaimDescendant_whenParentExitsNormally$")
	cmd.Env = append(os.Environ(), "XR_JOB_LIFETIME_ROLE=parent", "XR_JOB_LIFETIME_DIR="+dir)
	raw, err := prepare(cmd)
	if err != nil {
		t.Fatal(err)
	}
	j := raw.(*job)
	defer j.close()
	if err := j.start(cmd); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		_ = j.stop()
		_ = j.waitRoot()
		if !waited {
			_ = j.wait(cmd)
		}
	}()
	if err := j.attach(cmd); err != nil {
		t.Fatal(err)
	}
	population, err := j.accounting()
	if err != nil || population.TotalProcesses != 1 {
		t.Fatal(population, err)
	}
	j.stopPopulation = &population.TotalProcesses
	if err := j.captureExitHandles(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "child.pid")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("gated descendant did not start")
		}
		time.Sleep(time.Millisecond)
	}
	if err := j.stop(); err != nil {
		t.Fatal(err)
	}
	if err := j.waitRoot(); err != nil {
		t.Fatal(err)
	}
	if err := j.settle(); err == nil || !strings.Contains(err.Error(), "population changed") {
		t.Fatal("concurrent birth incorrectly certified", err)
	}
	// The expected uncertainty is not an excuse to leave native test processes.
	if err := j.waitExitHandles(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	_ = j.wait(cmd)
	waited = true
}
