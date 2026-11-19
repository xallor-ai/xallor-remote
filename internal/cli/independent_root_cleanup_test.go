package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Used only after all owners/tasks and protected handles have settled. It is
// not a recovery/delete authority for historical or user-owned directories.
func removeIndependentRoot(root string, owned os.FileInfo) error {
	return removeIndependentRootUsing(root, owned, os.RemoveAll)
}

func removeIndependentRootUsing(root string, owned os.FileInfo, remove func(string) error) error {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if absolute != filepath.Clean(root) || filepath.Dir(absolute) != filepath.Clean(os.TempDir()) || !strings.HasPrefix(filepath.Base(absolute), "xr-ipc-") || owned == nil {
		return errors.New("owned test root identity missing or invalid")
	}
	deadline := time.Now().Add(time.Second)
	for {
		current, err := os.Lstat(absolute)
		if err != nil {
			return err
		}
		if !current.IsDir() || current.Mode()&os.ModeSymlink != 0 || !os.SameFile(owned, current) {
			return errors.New("owned test root identity changed")
		}
		err = remove(absolute)
		if err == nil || !retryableIndependentRootRemove(err) || !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(25 * time.Millisecond)
	}
}
