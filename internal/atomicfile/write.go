package atomicfile

import (
	"os"
	"path/filepath"
)

// Replacing peer metadata must not truncate the credential store on a failed write.
func Write(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".replace-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	defer f.Close()
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
