//go:build windows

package process

import "golang.org/x/sys/windows"

// Retain identity-checked process handles while the root is running. In
// particular, Windows may create a console host after the suspended-start
// snapshot, then remove it from a later PID list during root exit. Sampling
// never certifies cleanup: stop/settle still require native exit signals and a
// stable cumulative population, and incomplete final observations fail closed.
func (j *job) observeRunningProcesses() {
	j.observed = make(chan struct{})
	go func() {
		defer close(j.observed)
		for {
			_ = j.captureExitHandles()
			state, err := windows.WaitForSingleObject(j.root, 2)
			if err != nil || state != uint32(windows.WAIT_TIMEOUT) {
				_ = j.captureExitHandles()
				return
			}
		}
	}()
}

// Called only after termination/root exit, before settlement or handle release.
// No observer may add a new handle after the final wait or race handle closure.
func (j *job) joinProcessObserver() {
	if j.observed != nil {
		<-j.observed
	}
}
