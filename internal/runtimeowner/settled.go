package runtimeowner

import (
	"path/filepath"

	"github.com/xallor-ai/xallor-remote/internal/filelock"
)

// QuerySettled distinguishes a terminal record from release of the transaction
// writer's lifecycle resources. Queries never create a missing journal/profile.
func (j Journal) QuerySettled(id string) (Record, bool, error) {
	record, err := j.Query(id)
	if err != nil {
		return record, false, err
	}
	if record.active() {
		return record, false, nil
	}
	unlock, err := filelock.Acquire(filepath.Join(j.Dir, "lifecycle.lock"))
	if err != nil {
		return record, false, nil
	}
	defer unlock()
	latest, err := j.Query(id)
	if err != nil {
		return latest, false, err
	}
	// Writer exit and a terminal outcome do not settle durable resource debt.
	// Revalidate retained selection receipts while holding lifecycle admission;
	// malformed or unreadable resource rows must keep the public view unsettled.
	return latest, !latest.active() && !j.HasUnsettledResources(id), nil
}
