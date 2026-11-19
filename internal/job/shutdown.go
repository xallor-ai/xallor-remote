package job

import (
	"os"
	"path/filepath"

	"github.com/xallor-ai/xallor-remote/internal/filelock"
)

// CheckReleased observes durable job evidence without performing restart
// recovery or rewriting records. The caller must first exclude a live Runtime
// using its lifetime lock. This is not a permanent admission credential.
func CheckReleased(dir string) error { _, err := ObserveReleased(dir); return err }

// ObserveReleased classifies the same locked ledger read without changing it.
func ObserveReleased(dir string) (string, error) {
	if !filepath.IsAbs(dir) {
		return "unreleased_or_unknown", ErrUnavailable
	}
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		return "absent", nil
	}
	if err != nil || !info.IsDir() {
		return "unreleased_or_unknown", ErrUnavailable
	}
	unlock, err := filelock.Acquire(filepath.Join(dir, "jobs.lock"))
	if err != nil {
		return "unreleased_or_unknown", ErrUnavailable
	}
	defer unlock()
	disk, err := readDiskState(filepath.Join(dir, "jobs.json"))
	if err != nil {
		return "unreleased_or_unknown", err
	}
	for _, record := range disk.Jobs {
		// Start durably publishes occupied before creating a process. A lost
		// reservation therefore proves no child started; do not erase it here.
		if record.State != "reserved" {
			return "unreleased_or_unknown", ErrUncertain
		}
	}
	if len(disk.Jobs) > 0 {
		return "reserved_only", nil
	}
	return "empty", nil
}
