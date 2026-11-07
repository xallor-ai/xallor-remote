//go:build windows

package process

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// Even a successful parent must not leave its owned descendants running after
// supervision closes. A file gate ensures the child is born after job assignment.
func TestJob_should_reclaimDescendant_whenParentExitsNormally(t *testing.T) {
	dir := os.Getenv("XR_JOB_LIFETIME_DIR")
	switch os.Getenv("XR_JOB_LIFETIME_ROLE") {
	case "child":
		time.Sleep(60 * time.Second)
		os.Exit(0)
	case "parent":
		for {
			if _, err := os.Stat(filepath.Join(dir, "release")); err == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		child := exec.Command(os.Args[0], "-test.run=^TestJob_should_reclaimDescendant_whenParentExitsNormally$")
		child.Env = append(os.Environ(), "XR_JOB_LIFETIME_ROLE=child")
		if child.Start() != nil {
			os.Exit(2)
		}
		if os.WriteFile(filepath.Join(dir, "child.pid"), []byte(strconv.Itoa(child.Process.Pid)), 0600) != nil {
			os.Exit(3)
		}
		for {
			if _, err := os.Stat(filepath.Join(dir, "parent-exit")); err == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		os.Exit(0)
	}
	dir = t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestJob_should_reclaimDescendant_whenParentExitsNormally$")
	cmd.Env = append(os.Environ(), "XR_JOB_LIFETIME_ROLE=parent", "XR_JOB_LIFETIME_DIR="+dir)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	child, err := Start(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer child.Stop()
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "child.pid")); err == nil {
			break
		}
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
		time.Sleep(10 * time.Millisecond)
	}
	b, err := os.ReadFile(filepath.Join(dir, "child.pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(b))
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	defer windows.TerminateProcess(h, 1)
	if err := os.WriteFile(filepath.Join(dir, "parent-exit"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err != nil {
		t.Fatal(err)
	}
	state, err := windows.WaitForSingleObject(h, 2000)
	if err != nil || state != windows.WAIT_OBJECT_0 {
		t.Fatalf("owned descendant survived normal parent completion: wait=%d err=%v", state, err)
	}
}
