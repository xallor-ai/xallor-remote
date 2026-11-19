package cli

import (
	"errors"

	"github.com/xallor-ai/xallor-remote/internal/release"
)

// Keep the user diagnosis specific while retaining both the cleanup sentinel
// and the underlying ledger cause for machine classification.
type jobCleanupRefusal struct{ cause error }

func (e jobCleanupRefusal) Error() string {
	return "runtime cleanup is uncertain：任务资源释放尚未确认，请保留现有记录并检查任务进程。"
}
func (e jobCleanupRefusal) Unwrap() []error {
	return []error{release.ErrCleanupUncertain, e.cause}
}
func jobCleanupError(cause error) error {
	if cause == nil {
		return nil
	}
	return jobCleanupRefusal{cause: cause}
}

func stopCleanupError(cause error) error {
	if cause == nil || errors.Is(cause, release.ErrCleanupUncertain) {
		return cause
	}
	return errors.Join(release.ErrCleanupUncertain, cause)
}
