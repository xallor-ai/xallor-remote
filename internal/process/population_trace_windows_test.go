//go:build windows

package process

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Opt-in diagnostic, scoped to one owned job. Completion-port notifications
// are investigative evidence, not a substitute for held process exit handles.
func TestJobTrace_should_identifyOwnedPopulationDuringStop(t *testing.T) {
	if os.Getenv("XR_JOB_TRACE") != "1" {
		t.Skip("owned native diagnostic is opt-in")
	}
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestJob_should_reclaimDescendant_whenParentExitsNormally$")
	cmd.Env = append(os.Environ(), "XR_JOB_LIFETIME_ROLE=parent", "XR_JOB_LIFETIME_DIR="+dir)
	raw, err := prepare(cmd)
	if err != nil {
		t.Fatal(err)
	}
	j := raw.(*job)
	defer j.close()
	port, err := windows.CreateIoCompletionPort(windows.InvalidHandle, 0, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(port)
	association := struct {
		key  uintptr
		port windows.Handle
	}{1, port}
	if _, err := windows.SetInformationJobObject(j.handle, windows.JobObjectAssociateCompletionPortInformation, uintptr(unsafe.Pointer(&association)), uint32(unsafe.Sizeof(association))); err != nil {
		t.Fatal(err)
	}
	if err := j.start(cmd); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = j.stop(); _ = j.waitRoot(); _ = j.wait(cmd) }()
	if err := j.attach(cmd); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	stopped := false
	for time.Now().Before(deadline) {
		if !stopped {
			if _, err := os.Stat(filepath.Join(dir, "child.pid")); err == nil {
				count, _ := j.accounting()
				t.Logf("before-stop total=%d active=%d", count.TotalProcesses, count.ActiveProcesses)
				if err := j.stop(); err != nil {
					t.Log("stop", err)
				}
				stopped = true
			}
		}
		var message uint32
		var key uintptr
		var processID *windows.Overlapped
		err := windows.GetQueuedCompletionStatus(port, &message, &key, &processID, 10)
		if err != nil {
			continue
		}
		pid := uint32(uintptr(unsafe.Pointer(processID)))
		name := ""
		if message == 6 { // JOB_OBJECT_MSG_NEW_PROCESS
			h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
			if err == nil {
				var buf [32768]uint16
				size := uint32(len(buf))
				if windows.QueryFullProcessImageName(h, 0, &buf[0], &size) == nil {
					name = filepath.Base(windows.UTF16ToString(buf[:size]))
				}
				_ = windows.CloseHandle(h)
			}
		}
		t.Logf("job-message=%d pid=%d image=%q", message, pid, name)
		if message == 4 && stopped { // ACTIVE_PROCESS_ZERO
			count, _ := j.accounting()
			t.Logf("after-stop total=%d active=%d settle=%v", count.TotalProcesses, count.ActiveProcesses, j.settle())
			return
		}
	}
	t.Fatal("owned job did not finish")
}
