package release

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

var ErrActivationSelected = errors.New("activation is retained by installation state")

// A creation receipt identifies the staged objects to check before cleanup.
// Loading a path/manifest does not manufacture a receipt. Pathname unlink is
// not atomic with SameFile: mutation by writers outside install.lock remains
// outside this cleanup guarantee, even with rooted traversal.
type activationOwnership struct {
	obligation ResourceObligation
	path       string
	directory  os.FileInfo
	files      map[string]os.FileInfo
}

func (s Store) CleanupActivation(a Activation) error {
	unlock, err := s.Lock()
	if err != nil {
		return err
	}
	defer unlock()
	return s.cleanupActivationLocked(a)
}

// Caller holds install.lock, or is disposing of its own not-yet-published
// StageActivation failure. This never scans/prunes historical directories.
func (s Store) cleanupActivationLocked(a Activation) error {
	if a.PID != 0 {
		return ErrActivationRunning
	}
	owner := a.ownership
	root, err := filepath.Abs(filepath.Join(s.Dir, "activations"))
	if err != nil {
		return err
	}
	if owner == nil || a.Dir != owner.path || a.ID != filepath.Base(owner.path) ||
		filepath.Dir(owner.path) != root || !observedActivationID.MatchString(a.ID) {
		return errors.New("activation cleanup requires the original creation receipt")
	}
	st, err := s.Read()
	if err != nil {
		return err
	}
	for _, ref := range []struct {
		manifest *Manifest
		id       string
	}{
		{st.Current, st.CurrentActivation}, {st.Previous, st.PreviousActivation}, {st.Pending, st.PendingActivation},
	} {
		if (ref.manifest == nil) != (ref.id == "") || (ref.id != "" && !observedActivationID.MatchString(ref.id)) {
			return errors.New("incomplete installation references; activation retained")
		}
	}
	for _, id := range []string{st.CurrentActivation, st.PreviousActivation, st.PendingActivation} {
		if id == a.ID {
			return ErrActivationSelected
		}
	}
	current, err := os.Lstat(owner.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !current.IsDir() || current.Mode()&os.ModeSymlink != 0 || !os.SameFile(current, owner.directory) {
		return errors.New("activation directory ownership changed")
	}
	dir, err := os.OpenRoot(owner.path)
	if err != nil {
		return err
	}
	defer dir.Close()
	current, err = dir.Lstat(".")
	if err != nil || !os.SameFile(current, owner.directory) {
		return errors.New("activation directory changed while opening")
	}
	listing, err := dir.Open(".")
	if err != nil {
		return err
	}
	entries, readErr := listing.ReadDir(3)
	closeErr := listing.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	if len(entries) > len(owner.files) {
		return errors.New("activation has unexpected contents")
	}
	check := func(name string) error {
		original, ok := owner.files[name]
		if !ok {
			return errors.New("activation has unexpected contents")
		}
		current, err := dir.Lstat(name)
		if err != nil {
			return err
		}
		if !current.Mode().IsRegular() || !os.SameFile(current, original) {
			return errors.New("activation file ownership changed")
		}
		return nil
	}
	// Check the whole set before deleting anything. Rooted, non-recursive
	// removal cannot traverse an injected link or remove an unknown subtree.
	// A non-locking writer can still replace a child between check and unlink.
	for _, entry := range entries {
		if err := check(entry.Name()); err != nil {
			return err
		}
	}
	for _, entry := range entries {
		if err := removeActivationFile(func() error { return check(entry.Name()) }, func() error { return dir.Remove(entry.Name()) }); err != nil {
			return err
		}
	}
	current, err = os.Lstat(owner.path)
	if err != nil || !os.SameFile(current, owner.directory) {
		return errors.New("activation directory changed before removal")
	}
	if err := dir.Close(); err != nil {
		return err
	}
	return os.Remove(owner.path) // empty directory only; never recursive
}

func (s Store) discardUnstartedActivation(a Activation, cause error) error {
	if a.ownership == nil {
		return cause
	}
	if errors.Is(cause, ErrCleanupUncertain) {
		return errors.Join(cause, s.settleActivation(a, cause))
	}
	err := s.cleanupActivationLocked(a)
	if errors.Is(err, ErrActivationSelected) {
		return cause
	}
	if err == nil {
		return errors.Join(cause, s.settleActivation(a, nil))
	}
	err = s.settleActivation(a, err)
	return errors.Join(cause, fmt.Errorf("%w: staged activation %s retained: %v", ErrCleanupUncertain, a.ID, err))
}
