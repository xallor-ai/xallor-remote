//go:build windows

package runtimeprocess

import (
	"errors"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func openProcessImage(pid int, selectedPath string) (result *os.File, resultErr error) {
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := windows.CloseHandle(process); err != nil {
			if result != nil {
				resultErr = errors.Join(resultErr, result.Close())
				result = nil
			}
			resultErr = errors.Join(resultErr, err)
		}
	}()
	path, err := windows.UTF16PtrFromString(selectedPath)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(path, windows.GENERIC_READ|windows.FILE_EXECUTE|windows.SYNCHRONIZE,
		windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(h), selectedPath)
	// This query consumes a file HANDLE and compares its SectionObjectPointer
	// with the process's original image, rather than trusting an image pathname.
	// https://github.com/m417z/ntdoc/blob/main/descriptions/processinfoclass.md#processimagefilemapping-44
	err = windows.NtQueryInformationProcess(process, windows.ProcessImageFileMapping, unsafe.Pointer(&h), uint32(unsafe.Sizeof(h)), nil)
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	return f, nil
}
