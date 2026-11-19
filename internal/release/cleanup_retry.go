package release

import "time"

// Only an explicit Windows access/sharing denial is retried. The original
// creation receipt is rechecked before each attempt under install.lock. Never
// retry an unknown error or replay a lifecycle/process effect.
func removeActivationFile(check func() error, remove func() error) error {
	deadline := time.Now().Add(time.Second)
	for {
		if err := check(); err != nil {
			return err
		}
		err := remove()
		if err == nil || !retryableActivationDelete(err) || !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(25 * time.Millisecond)
	}
}
