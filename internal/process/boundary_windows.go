//go:build windows

package process

import (
	"fmt"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type job struct{ handle, root windows.Handle }

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
