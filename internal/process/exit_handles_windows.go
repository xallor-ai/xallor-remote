//go:build windows

package process

import (
	"errors"
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var isProcessInJob = windows.NewLazySystemDLL("kernel32.dll").NewProc("IsProcessInJob")

const jobProcessCapacity = 4096

type jobProcessList struct {
	assigned, count uint32
	pids            [jobProcessCapacity]uintptr
}

type jobAccounting struct {
	UserTime, KernelTime, PeriodUserTime, PeriodKernelTime           int64
	PageFaults, TotalProcesses, ActiveProcesses, TerminatedProcesses uint32
}

func (j *job) accounting() (jobAccounting, error) {
	var count jobAccounting
	err := windows.QueryInformationJobObject(j.handle, windows.JobObjectBasicAccountingInformation, uintptr(unsafe.Pointer(&count)), uint32(unsafe.Sizeof(count)), nil)
	return count, err
}

// Capture read-only kernel handles before terminating the job: a PID list or
// accounting count alone is not evidence that these process objects signalled.
// Never terminate a process by an enumerated PID.
func (j *job) captureExitHandles() error {
	var list jobProcessList
	if err := windows.QueryInformationJobObject(j.handle, windows.JobObjectBasicProcessIdList, uintptr(unsafe.Pointer(&list)), uint32(unsafe.Sizeof(list)), nil); err != nil {
		return err
	}
	return j.observeExitHandles(list)
}

func (j *job) observeExitHandles(list jobProcessList) error {
	j.exitMu.Lock()
	defer j.exitMu.Unlock()
	if j.observationOverflow {
		return errors.New("job retained process observation exceeded bound")
	}
	if list.assigned > jobProcessCapacity || list.count > jobProcessCapacity || list.count > list.assigned {
		return fmt.Errorf("job process observation exceeded bound (assigned=%d listed=%d capacity=%d)", list.assigned, list.count, jobProcessCapacity)
	}
	if list.count < list.assigned {
		// A partial snapshot is insufficient by itself. It can only be ignored
		// when native cumulative accounting proves that every process ever in
		// this private job already has a retained, identity-checked exit handle.
		// Held handles prevent PID recycling, so the map counts distinct objects.
		// settle still requires the fixed stop population and waits these handles.
		count, err := j.accounting()
		if err == nil && count.TotalProcesses > 0 && uint64(count.TotalProcesses) == uint64(len(j.exits)) {
			return nil
		}
		return fmt.Errorf("job process snapshot incomplete (assigned=%d listed=%d retained=%d): %v", list.assigned, list.count, len(j.exits), err)
	}
	if j.exits == nil {
		j.exits = make(map[uint32]windows.Handle)
	}
	for _, value := range list.pids[:list.count] {
		if value == 0 || uint64(value) > uint64(^uint32(0)) {
			return errors.New("invalid job process identity")
		}
		pid := uint32(value)
		if _, ok := j.exits[pid]; ok {
			continue
		} // Held handle prevents PID recycling.
		if len(j.exits) >= jobProcessCapacity {
			j.observationOverflow = true
			return errors.New("job retained process observation exceeded bound")
		}
		handle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			continue
		} // process object already gone
		if err != nil {
			return err
		}
		var member int32
		ok, _, callErr := isProcessInJob.Call(uintptr(handle), uintptr(j.handle), uintptr(unsafe.Pointer(&member)))
		if ok == 0 || member == 0 {
			state, waitErr := windows.WaitForSingleObject(handle, 0)
			_ = windows.CloseHandle(handle)
			if ok != 0 && waitErr == nil && state == windows.WAIT_OBJECT_0 {
				continue
			}
			return fmt.Errorf("job member identity unconfirmed: %v", callErr)
		}
		j.exits[pid] = handle
	}
	return nil
}

func (j *job) waitExitHandles(deadline time.Time) error {
	j.exitMu.Lock()
	defer j.exitMu.Unlock()
	for _, handle := range j.exits {
		remaining := time.Until(deadline)
		var millis uint32
		if remaining > 0 {
			millis = uint32((remaining + time.Millisecond - 1) / time.Millisecond)
		}
		state, err := windows.WaitForSingleObject(handle, millis)
		if err != nil {
			return err
		}
		if state != windows.WAIT_OBJECT_0 {
			return errors.New("job member process exit unconfirmed")
		}
	}
	return nil
}
