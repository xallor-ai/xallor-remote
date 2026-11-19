package release

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// A quarantine receipt is created with the directory, never reconstructed from
// an arbitrary pathname. The root stays open through consumption. File IDs come
// from creation descriptors before the first write, including partial writes.
// SameFile/check/unlink are not atomic against a concurrent same-user writer.
type sourceSnapshot struct {
	path      string
	root      *os.Root
	directory os.FileInfo
	files     map[string]os.FileInfo
	closeErr  error
}

// closeFile owns closure of a quarantine output independently of the consumer.
func (s *sourceSnapshot) closeFile(file io.Closer) error {
	err := file.Close()
	s.closeErr = errors.Join(s.closeErr, err)
	return err
}

func sourceCleanupError(cause, cleanup error) error {
	if cleanup == nil {
		return cause
	}
	return errors.Join(cause, ErrCleanupUncertain, cleanup)
}

func newSourceSnapshot() (*sourceSnapshot, error) {
	path, err := os.MkdirTemp("", "xr-provenance-")
	if err != nil {
		return nil, err
	}
	s := &sourceSnapshot{path: path, files: make(map[string]os.FileInfo)}
	s.root, err = os.OpenRoot(path)
	if err != nil {
		return nil, sourceCleanupError(err, fmt.Errorf("snapshot retained without directory receipt: %s", path))
	}
	directory, err := s.root.Open(".")
	if err == nil {
		s.directory, err = directory.Stat()
		err = sourceCleanupError(err, directory.Close())
	}
	if err != nil {
		err = sourceCleanupError(err, s.root.Close())
		return nil, sourceCleanupError(err, fmt.Errorf("snapshot retained without directory receipt: %s", path))
	}
	return s, nil
}

func (s *sourceSnapshot) create(name string) (*os.File, error) {
	if filepath.Base(name) != name || len(s.files) >= 2 {
		return nil, errors.New("invalid snapshot member")
	}
	f, err := s.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	// Register even if Stat fails: nil identity forces retention, never deletion
	// based solely on a file name or a later reconstructed receipt.
	s.files[name] = nil
	info, err := f.Stat()
	if err != nil {
		return nil, sourceCleanupError(err, s.closeFile(f))
	}
	s.files[name] = info
	return f, nil
}

func (s *sourceSnapshot) checkDirectory() error {
	current, err := os.Lstat(s.path)
	if err != nil {
		return err
	}
	if !current.IsDir() || current.Mode()&os.ModeSymlink != 0 || !os.SameFile(current, s.directory) {
		return errors.New("snapshot directory ownership changed")
	}
	return nil
}

func (s *sourceSnapshot) checkFile(name string) error {
	original, ok := s.files[name]
	if !ok || original == nil {
		return errors.New("snapshot has unowned contents")
	}
	current, err := s.root.Lstat(name)
	if err != nil {
		return err
	}
	if !current.Mode().IsRegular() || !os.SameFile(current, original) {
		return errors.New("snapshot file ownership changed")
	}
	return nil
}

func (s *sourceSnapshot) cleanup() (result error) {
	defer func() {
		// Successful unlinking cannot establish that a prior output Close
		// succeeded. Preserve only this snapshot's own close uncertainty.
		result = errors.Join(result, s.closeErr)
		if s.root != nil {
			result = errors.Join(result, s.root.Close())
			s.root = nil
		}
		if result != nil {
			result = fmt.Errorf("snapshot cleanup %s: %w", s.path, result)
		}
	}()
	if err := s.checkDirectory(); err != nil {
		return err
	}
	listing, err := s.root.Open(".")
	if err != nil {
		return err
	}
	entries, readErr := listing.ReadDir(3)
	if errors.Is(readErr, io.EOF) {
		readErr = nil
	}
	if err := errors.Join(readErr, listing.Close()); err != nil {
		return err
	}
	if len(entries) != len(s.files) {
		return errors.New("snapshot contents differ from creation receipt")
	}
	// Validate the whole bounded set before removing anything. Never recurse,
	// follow a link, remove unknown content, or retry the consumer/verifier.
	for _, entry := range entries {
		if err := s.checkFile(entry.Name()); err != nil {
			return err
		}
	}
	for _, entry := range entries {
		if err := s.checkDirectory(); err != nil {
			return err
		}
		if err := s.checkFile(entry.Name()); err != nil {
			return err
		}
		if err := s.root.Remove(entry.Name()); err != nil {
			return err
		}
	}
	if err := s.checkDirectory(); err != nil {
		return err
	}
	closeErr := s.root.Close()
	s.root = nil
	if closeErr != nil {
		return closeErr
	}
	if err := s.checkDirectory(); err != nil {
		return err
	}
	return os.Remove(s.path) // Empty directory only; no computed recursive delete.
}
