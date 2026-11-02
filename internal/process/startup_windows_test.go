//go:build windows

package process

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// The owner is killed after creation, before attach. The OS must already own
// the child in its job, so this crash cannot leave a suspended orphan behind.
func TestJob_should_reclaimChild_whenOwnerDiesBeforeAttachment(t *testing.T) {
	dir := os.Getenv("XR_STARTUP_CRASH_DIR")
	switch os.Getenv("XR_STARTUP_CRASH_ROLE") {
	case "child":
		time.Sleep(time.Minute)
		os.Exit(0)
	case "owner":
		cmd := exec.Command(os.Args[0], "-test.run=^TestJob_should_reclaimChild_whenOwnerDiesBeforeAttachment$")
		cmd.Env = append(os.Environ(), "XR_STARTUP_CRASH_ROLE=child")
		if os.Getenv("XR_STARTUP_CRASH_COPY") == "1" {
			cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
		}
		g, err := prepare(cmd)
		if err != nil {
			os.Exit(2)
		}
		if err = g.start(cmd); err != nil {
			os.Exit(3)
		}
		if os.WriteFile(filepath.Join(dir, "child.pid"), []byte(strconv.Itoa(cmd.Process.Pid)), 0600) != nil {
			os.Exit(4)
		}
		// Do not attach, close the boundary or run defers: simulate an abrupt owner crash.
		time.Sleep(time.Minute)
		os.Exit(5)
	}
	for _, generic := range []string{"0", "1"} {
		t.Run("generic-streams-"+generic, func(t *testing.T) { checkStartupCrash(t, generic) })
	}
}

func checkStartupCrash(t *testing.T, generic string) {
	t.Helper()
	dir := t.TempDir()
	owner := exec.Command(os.Args[0], "-test.run=^TestJob_should_reclaimChild_whenOwnerDiesBeforeAttachment$")
	owner.Env = append(os.Environ(), "XR_STARTUP_CRASH_ROLE=owner", "XR_STARTUP_CRASH_DIR="+dir, "XR_STARTUP_CRASH_COPY="+generic)
	if err := owner.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.Process.Kill(); _ = owner.Wait() }()
	deadline := time.Now().Add(15 * time.Second)
	var pid int
	for pid == 0 {
		data, err := os.ReadFile(filepath.Join(dir, "child.pid"))
		if err == nil {
			pid, _ = strconv.Atoi(string(data))
		}
		if time.Now().After(deadline) {
			t.Fatal("owner did not create fixture child")
		}
		time.Sleep(10 * time.Millisecond)
	}
	h, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	defer windows.TerminateProcess(h, 1)
	if state, err := windows.WaitForSingleObject(h, 0); err != nil || state != uint32(windows.WAIT_TIMEOUT) {
		t.Fatal("fixture child was not alive", state, err)
	}
	if err := owner.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = owner.Wait()
	if state, err := windows.WaitForSingleObject(h, 3000); err != nil || state != uint32(windows.WAIT_OBJECT_0) {
		t.Fatalf("child survived owner crash between creation and attachment: %d %v", state, err)
	}
}
