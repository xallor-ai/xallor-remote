//go:build windows

package cli

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Read-only failure evidence, restricted to this test's owned activations.
// PIDs/names are observations, never authority to terminate another process.
func diagnoseNativeActivationLocks(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, "activations"))
	if err != nil {
		t.Log("activation diagnostic", err)
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(dir, "activations", entry.Name(), "xallor-remote.exe")
		name, err := windows.UTF16PtrFromString(path)
		if err != nil {
			continue
		}
		h, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
		if err != nil {
			t.Logf("activation %s diagnostic open: %v", entry.Name(), err)
			continue
		}
		var list struct {
			count uint32
			pids  [128]uintptr
		}
		var iosb windows.IO_STATUS_BLOCK
		queryErr := windows.NtQueryInformationFile(h, &iosb, (*byte)(unsafe.Pointer(&list)), uint32(unsafe.Sizeof(list)), 47)
		var info windows.ByHandleFileInformation
		_ = windows.GetFileInformationByHandle(h, &info)
		windows.CloseHandle(h)
		t.Logf("activation %s attributes=%x owner-count=%d query=%v", entry.Name(), info.FileAttributes, list.count, queryErr)
		if queryErr != nil || list.count > 128 {
			continue
		}
		for _, pid := range list.pids[:list.count] {
			image := ""
			process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(pid))
			var imageErr error
			var waitState uint32
			var waitErr error
			if err == nil {
				var buffer [32768]uint16
				n := uint32(len(buffer))
				imageErr = windows.QueryFullProcessImageName(process, 0, &buffer[0], &n)
				if imageErr == nil {
					image = filepath.Base(windows.UTF16ToString(buffer[:n]))
				}
				waitState, waitErr = windows.WaitForSingleObject(process, 0)
				windows.CloseHandle(process)
			}
			t.Logf("activation owner pid=%d image=%q open=%v query=%v wait=%d waitError=%v", pid, image, err, imageErr, waitState, waitErr)
		}
	}
}
