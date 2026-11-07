//go:build windows

package process

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// PROC_THREAD_ATTRIBUTE_JOB_LIST: documented Windows 10+ process creation
// attribute. x/sys exposes the attribute-list API but not this constant.
const procThreadAttributeJobList = 0x0002000d

func (j *job) start(cmd *exec.Cmd) (err error) {
	app, line, dir, env, err := nativeArguments(cmd)
	if err != nil {
		return err
	}
	j.io, err = prepareCommandIO(cmd)
	if err != nil {
		return err
	}
	defer func() {
		if err == nil {
			return
		}
		if j.root != 0 {
			cleanup := errors.Join(j.stop(), j.settle())
			if cleanup != nil {
				err = errors.Join(err, fmt.Errorf("%w: %v", ErrCleanup, cleanup))
			}
			if cmd.Process != nil {
				cmd.ProcessState, _ = cmd.Process.Wait()
			}
		}
		j.io.closeAll()
	}()
	var handles [3]windows.Handle
	for n, f := range j.io.files {
		if f == nil {
			return errors.New("nil standard file")
		}
		if err := windows.DuplicateHandle(windows.CurrentProcess(), windows.Handle(f.Fd()), windows.CurrentProcess(), &handles[n], 0, true, windows.DUPLICATE_SAME_ACCESS); err != nil {
			return err
		}
		defer windows.CloseHandle(handles[n])
	}
	attrs, err := windows.NewProcThreadAttributeList(2)
	if err != nil {
		return err
	}
	defer attrs.Delete()
	if err := attrs.Update(procThreadAttributeJobList, unsafe.Pointer(&j.handle), unsafe.Sizeof(j.handle)); err != nil {
		return err
	}
	// Restrict inheritance: concurrent launches must not acquire each other's
	// pipes, or any unrelated handle marked inheritable in the Runtime.
	if err := attrs.Update(windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST, unsafe.Pointer(&handles[0]), unsafe.Sizeof(handles)); err != nil {
		return err
	}
	si := windows.StartupInfoEx{
		StartupInfo:             windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfoEx{})), Flags: windows.STARTF_USESTDHANDLES, StdInput: handles[0], StdOutput: handles[1], StdErr: handles[2]},
		ProcThreadAttributeList: attrs.List(),
	}
	flags := uint32(windows.CREATE_SUSPENDED | windows.CREATE_NO_WINDOW | windows.CREATE_UNICODE_ENVIRONMENT | windows.EXTENDED_STARTUPINFO_PRESENT)
	if a := cmd.SysProcAttr; a != nil {
		flags |= a.CreationFlags
		if a.HideWindow {
			si.Flags |= windows.STARTF_USESHOWWINDOW
			si.ShowWindow = windows.SW_HIDE
		}
	}
	var pi windows.ProcessInformation
	if err := windows.CreateProcess(app, line, nil, nil, true, flags, &env[0], dir, &si.StartupInfo, &pi); err != nil {
		return err
	}
	runtime.KeepAlive(handles)
	j.root, j.thread = pi.Process, pi.Thread
	// Keep the original native handle alive while opening the handle that
	// supplies os.ProcessState and the public exec.ExitError result.
	cmd.Process, err = os.FindProcess(int(pi.ProcessId))
	if err != nil {
		return err
	}
	j.io.startCopies()
	return nil
}

func nativeArguments(cmd *exec.Cmd) (app, line, dir *uint16, env []uint16, err error) {
	if cmd.Err != nil {
		err = cmd.Err
		return
	}
	if cmd.Path == "" {
		err = errors.New("empty executable")
		return
	}
	path := cmd.Path
	if !filepath.IsAbs(path) {
		path, err = filepath.Abs(filepath.Join(cmd.Dir, path))
		if err != nil {
			return
		}
	}
	app, err = windows.UTF16PtrFromString(path)
	if err != nil {
		return
	}
	args := cmd.Args
	if len(args) == 0 {
		args = []string{cmd.Path}
	}
	line, err = windows.UTF16PtrFromString(windows.ComposeCommandLine(args))
	if err != nil {
		return
	}
	if cmd.Dir != "" {
		dir, err = windows.UTF16PtrFromString(cmd.Dir)
		if err != nil {
			return
		}
	}
	// Cmd.Environ omits malformed entries while discarding its internal error;
	// reject the raw input first instead of silently launching a changed request.
	for _, value := range cmd.Env {
		if strings.IndexByte(value, 0) >= 0 {
			err = errors.New("environment contains NUL")
			return
		}
	}
	values := cmd.Environ() // Windows case-insensitive deduplication and SystemRoot.
	env = utf16.Encode([]rune(strings.Join(values, "\x00") + "\x00\x00"))
	return
}
