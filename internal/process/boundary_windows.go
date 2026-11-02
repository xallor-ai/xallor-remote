//go:build windows

package process

import (
	"errors"
	"fmt"
	"os/exec"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type job struct {
	handle, root, thread windows.Handle
	io                   *commandIO
}

func prepare(cmd *exec.Cmd) (boundary, error) {
	if cmd.Err != nil {
		return nil, cmd.Err
	}
	if a := cmd.SysProcAttr; a != nil {
		if a.CmdLine != "" || a.Token != 0 || a.ParentProcess != 0 || a.ProcessAttributes != nil || a.ThreadAttributes != nil || a.NoInheritHandles || len(a.AdditionalInheritedHandles) != 0 || a.CreationFlags & ^uint32(windows.CREATE_NO_WINDOW|windows.CREATE_SUSPENDED) != 0 {
			return nil, errors.New("unsupported supervised process attributes")
		}
	}
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		_ = windows.CloseHandle(h)
		return nil, err
	}
	return &job{handle: h}, nil
}

// Job assignment happened inside CreateProcess. Only resume the retained
// primary thread here; no PID/thread enumeration or post-create assignment.
func (j *job) attach(*exec.Cmd) error {
	count, err := windows.ResumeThread(j.thread)
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("unexpected primary thread suspend count: %d", count)
	}
	err = windows.CloseHandle(j.thread)
	j.thread = 0
	return err
}

func (j *job) wait(cmd *exec.Cmd) error {
	state, err := cmd.Process.Wait()
	cmd.ProcessState = state
	if err == nil && !state.Success() {
		err = &exec.ExitError{ProcessState: state}
	}
	return errors.Join(err, j.io.finish(cmd.WaitDelay))
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

// TerminateJobObject initiates termination; only zero active processes proves
// that the supervised job no longer consumes execution capacity.
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
	var errs []error
	for _, h := range []*windows.Handle{&j.thread, &j.root, &j.handle} {
		if *h != 0 {
			errs = append(errs, windows.CloseHandle(*h))
			*h = 0
		}
	}
	return errors.Join(errs...)
}
