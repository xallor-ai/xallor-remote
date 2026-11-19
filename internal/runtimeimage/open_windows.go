//go:build windows

package runtimeimage

import (
	"errors"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func openImage(path string) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return nil, joinCleanup(err, windows.CloseHandle(h))
	}
	if info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 {
		return nil, joinCleanup(errors.New("Runtime image cannot be a directory or reparse point"), windows.CloseHandle(h))
	}
	return os.NewFile(uintptr(h), path), nil
}

// The Runtime retains a kernel copy even after its short-lived CLI launcher
// exits. The child's OS closes it on exit; other children do not receive it
// unless their explicit inherited-handle list includes it.
func inheritImage(cmd *exec.Cmd, f *os.File) error {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	if cmd.SysProcAttr.NoInheritHandles {
		return errors.New("Runtime image protection requires handle inheritance")
	}
	if err := windows.SetHandleInformation(windows.Handle(f.Fd()), windows.HANDLE_FLAG_INHERIT, windows.HANDLE_FLAG_INHERIT); err != nil {
		return err
	}
	cmd.SysProcAttr.AdditionalInheritedHandles = append(cmd.SysProcAttr.AdditionalInheritedHandles, syscall.Handle(f.Fd()))
	return nil
}
