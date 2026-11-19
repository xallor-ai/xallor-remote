package runtimeowner

import (
	"os"
	"path/filepath"

	"github.com/xallor-ai/xallor-remote/internal/filelock"
)

// WithBootstrap holds lifecycle before the release install lock. The caller
// authenticates a fresh bootstrap source before this may create the lock folder.
func (j Journal) WithBootstrap(run func() error) error {
	if run == nil {
		return ErrInvalid
	}
	if err := os.MkdirAll(j.Dir, 0700); err != nil {
		return err
	}
	unlock, err := filelock.Acquire(filepath.Join(j.Dir, "lifecycle.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	if err := j.RefuseBootstrap(); err != nil {
		return err
	}
	return run()
}
