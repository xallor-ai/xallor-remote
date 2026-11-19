//go:build windows

package process

import (
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type job struct {
	handle, root, thread windows.Handle
	io                   *commandIO
	exits                map[uint32]windows.Handle
	stopPopulation       *uint32
	exitMu               sync.Mutex
	observed             chan struct{}
	observationOverflow  bool
}

func prepare(cmd *exec.Cmd) (boundary, error) {
	if cmd.Err != nil {
		return nil, cmd.Err
	}
	if a := cmd.SysProcAttr; a != nil {
		if a.CmdLine != "" || a.Token != 0 || a.ParentProcess != 0 || a.ProcessAttributes != nil || a.ThreadAttributes != nil || a.NoInheritHandles || a.CreationFlags & ^uint32(windows.CREATE_NO_WINDOW|windows.CREATE_SUSPENDED) != 0 {
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
		return nil, failedStartCleanup(err, windows.CloseHandle(h))
	}
	return &job{handle: h}, nil
}

// Job assignment happened inside CreateProcess. Resume the retained primary
// thread; process enumeration below is read-only exit observation, not assignment.
func (j *job) attach(*exec.Cmd) error {
	// Retain process objects already created with the suspended root (including
	// an OS console host) before user code can exit and disappear from PID lists.
	if err := j.captureExitHandles(); err != nil {
		return err
	}
	count, err := windows.ResumeThread(j.thread)
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("unexpected primary thread suspend count: %d", count)
	}
	j.observeRunningProcesses()
	return j.releaseThread()
}

// Release the owned primary-thread handle once; never retry an uncertain close
// against a numeric handle that the OS may subsequently reuse.
func (j *job) releaseThread() error {
	err := windows.CloseHandle(j.thread)
	j.thread = 0
	return failedStartCleanup(nil, err)
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
func (j *job) stop() error {
	if j.stopPopulation == nil {
		count, err := j.accounting()
		if err != nil {
			return errors.Join(err, windows.TerminateJobObject(j.handle, 1))
		}
		j.stopPopulation = &count.TotalProcesses
	}
	observed := j.captureExitHandles()
	return errors.Join(observed, windows.TerminateJobObject(j.handle, 1))
}

// Accounting zero alone can precede a descendant process handle signalling.
// Require stable cumulative population plus exit of captured process objects.
func (j *job) settle() error {
	j.joinProcessObserver()
	j.exitMu.Lock()
	overflow := j.observationOverflow
	j.exitMu.Unlock()
	if overflow {
		return errors.New("job retained process observation exceeded bound")
	}
	deadline := time.Now().Add(cleanupTimeout)
	for {
		accounting, err := j.accounting()
		if err != nil {
			return err
		}
		if accounting.ActiveProcesses == 0 {
			if j.stopPopulation == nil {
				return errors.New("job termination population was not observed")
			}
			if accounting.TotalProcesses != *j.stopPopulation {
				return fmt.Errorf("job population changed during termination (%d -> %d); process exit observation incomplete", *j.stopPopulation, accounting.TotalProcesses)
			}
			return j.waitExitHandles(deadline)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("job still has %d active processes", accounting.ActiveProcesses)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (j *job) close() error {
	j.joinProcessObserver()
	var errs []error
	for pid, h := range j.exits {
		errs = append(errs, windows.CloseHandle(h))
		delete(j.exits, pid)
	}
	for _, h := range []*windows.Handle{&j.thread, &j.root, &j.handle} {
		if *h != 0 {
			errs = append(errs, windows.CloseHandle(*h))
			*h = 0
		}
	}
	return errors.Join(errs...)
}
