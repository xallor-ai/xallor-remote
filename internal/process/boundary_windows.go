//go:build windows

package process

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

type job struct {
	handle, root windows.Handle
	atomic       bool
}

const procThreadAttributeJobList = 0x0002000d

func (j *job) start(cmd *exec.Cmd) error {
	if !fileStdio(cmd) {
		return cmd.Start()
	}
	var handles [3]windows.Handle
	files := []*os.File{fileOf(cmd.Stdin), fileOf(cmd.Stdout), fileOf(cmd.Stderr)}
	for i, f := range files {
		if f == nil {
			continue
		}
		if err := windows.DuplicateHandle(windows.CurrentProcess(), windows.Handle(f.Fd()), windows.CurrentProcess(), &handles[i], 0, true, windows.DUPLICATE_SAME_ACCESS); err != nil {
			return err
		}
		defer windows.CloseHandle(handles[i])
	}
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return err
	}
	defer attrs.Delete()
	job := []windows.Handle{j.handle}
	if err := attrs.Update(procThreadAttributeJobList, unsafe.Pointer(&job[0]), unsafe.Sizeof(job[0])); err != nil {
		return err
	}
	si := &windows.StartupInfoEx{StartupInfo: windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfoEx{})), Flags: windows.STARTF_USESTDHANDLES, StdInput: handles[0], StdOutput: handles[1], StdErr: handles[2]}, ProcThreadAttributeList: attrs.List()}
	argv := append([]string(nil), cmd.Args...)
	if len(argv) == 0 {
		argv = []string{cmd.Path}
	}
	line, err := windows.UTF16FromString(windows.ComposeCommandLine(argv))
	if err != nil {
		return err
	}
	app, err := windows.UTF16PtrFromString(cmd.Path)
	if err != nil {
		return err
	}
	var dir *uint16
	if cmd.Dir != "" {
		dir, err = windows.UTF16PtrFromString(cmd.Dir)
		if err != nil {
			return err
		}
	}
	env, err := environmentBlock(cmd.Env)
	if err != nil {
		return err
	}
	var pi windows.ProcessInformation
	flags := uint32(windows.CREATE_SUSPENDED | windows.CREATE_UNICODE_ENVIRONMENT | windows.EXTENDED_STARTUPINFO_PRESENT)
	if err := windows.CreateProcess(app, &line[0], nil, nil, true, flags, env, dir, &si.StartupInfo, &pi); err != nil {
		return err
	}
	j.root, j.atomic = pi.Process, true
	cmd.Process, err = os.FindProcess(int(pi.ProcessId))
	if err != nil {
		_ = windows.TerminateProcess(pi.Process, 1)
		_ = windows.CloseHandle(pi.Process)
		_ = windows.CloseHandle(pi.Thread)
		return err
	}
	if _, err = windows.ResumeThread(pi.Thread); err != nil {
		_ = windows.TerminateProcess(pi.Process, 1)
		_ = windows.CloseHandle(pi.Thread)
		return err
	}
	_ = windows.CloseHandle(pi.Thread)
	return nil
}
func (j *job) wait(cmd *exec.Cmd) error { return cmd.Wait() }

func fileOf(v any) *os.File {
	if f, ok := v.(*os.File); ok {
		return f
	}
	return nil
}
func fileStdio(cmd *exec.Cmd) bool {
	return (cmd.Stdin == nil || fileOf(cmd.Stdin) != nil) && (cmd.Stdout == nil || fileOf(cmd.Stdout) != nil) && (cmd.Stderr == nil || fileOf(cmd.Stderr) != nil)
}
func environmentBlock(env []string) (*uint16, error) {
	if env == nil {
		env = os.Environ()
	}
	encoded := utf16.Encode([]rune(strings.Join(env, "\x00") + "\x00\x00"))
	return &encoded[0], nil
}

func prepare(cmd *exec.Cmd) (boundary, error) {
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		_ = windows.CloseHandle(h)
		return nil, err
	}
	attr := syscall.SysProcAttr{}
	if cmd.SysProcAttr != nil {
		attr = *cmd.SysProcAttr
	}
	attr.CreationFlags |= windows.CREATE_SUSPENDED | windows.CREATE_NO_WINDOW
	cmd.SysProcAttr = &attr
	return &job{handle: h}, nil
}

func (j *job) attach(cmd *exec.Cmd) error {
	if j.atomic {
		return nil
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return err
	}
	j.root = h
	if err = windows.AssignProcessToJobObject(j.handle, h); err != nil {
		return err
	}
	return resumePrimary(uint32(cmd.Process.Pid))
}

// os/exec closes the primary thread handle. The suspended process has not run
// user code; locate its primary thread, then resume only after job assignment.
func resumePrimary(pid uint32) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	for err = windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID != pid {
			continue
		}
		thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if err != nil {
			return err
		}
		count, err := windows.ResumeThread(thread)
		_ = windows.CloseHandle(thread)
		if err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("unexpected primary thread suspend count: %d", count)
		}
		return nil
	}
	return fmt.Errorf("primary thread not found: %w", err)
}

func (j *job) waitRoot() error {
	s, err := windows.WaitForSingleObject(j.root, windows.INFINITE)
	if err != nil {
		return err
	}
	if s != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("root wait: %d", s)
	}
	return nil
}
func (j *job) stop() error { return windows.TerminateJobObject(j.handle, 1) }

// Query active processes rather than treating TerminateJobObject's return as
// proof that all associated processes have already finished.
func (j *job) settle() error {
	var accounting struct {
		UserTime, KernelTime, PeriodUserTime, PeriodKernelTime           int64
		PageFaults, TotalProcesses, ActiveProcesses, TerminatedProcesses uint32
	}
	deadline := time.Now().Add(cleanupTimeout)
	for {
		if err := windows.QueryInformationJobObject(j.handle, windows.JobObjectBasicAccountingInformation, uintptr(unsafe.Pointer(&accounting)), uint32(unsafe.Sizeof(accounting)), nil); err != nil {
			return err
		}
		if accounting.ActiveProcesses == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("job still has %d active processes", accounting.ActiveProcesses)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func (j *job) close() error {
	if j.root != 0 {
		_ = windows.CloseHandle(j.root)
	}
	return windows.CloseHandle(j.handle)
}
