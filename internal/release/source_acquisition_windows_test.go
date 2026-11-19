package release

import (
	"errors"
	"path/filepath"
	"syscall"
	"testing"
)

// No-sharing access detects the real original binary descriptor independently
// of the recorder. Windows does not expose directory-root descriptors here.
func assertSourceInputsOpen(t *testing.T, dir, binary string, wantOpen bool) {
	t.Helper()
	path, err := syscall.UTF16PtrFromString(filepath.Join(dir, binary))
	if err != nil {
		t.Fatal(err)
	}
	h, err := syscall.CreateFile(path, syscall.GENERIC_READ, 0, nil, syscall.OPEN_EXISTING, 0, 0)
	if err == nil {
		if err := syscall.CloseHandle(h); err != nil {
			t.Fatal(err)
		}
	}
	if wantOpen {
		if !errors.Is(err, syscall.Errno(32)) {
			t.Errorf("original input should be open, exclusive access returned %v", err)
		}
	} else if err != nil {
		t.Errorf("original input still open at source closure: %v", err)
	}
}
