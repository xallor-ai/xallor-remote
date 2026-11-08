package release

import (
	"os"
	"path/filepath"
)

// Only callers holding an authenticated private snapshot may stage these bytes.
func (s Store) copyAuthenticatedSnapshot(binary string, m Manifest, raw []byte) error {
	source, err := os.Open(binary)
	if err != nil {
		return err
	}
	defer source.Close()
	if err := s.copyArtifact(source, m); err != nil {
		return err
	}
	// Retain exact signed manifest bytes, rather than a reserialized substitute.
	dir := filepath.Dir(s.Path(m))
	f, err := os.CreateTemp(dir, "manifest-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	_, err = f.Write(raw)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(dir, "manifest.json"))
}
