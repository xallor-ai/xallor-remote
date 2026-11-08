package release

import (
	"errors"
	"fmt"
	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
)

// Control must bound requests and verify the launched process's digest before success.
type Control interface {
	Running() (*buildinfo.Executable, error)
	Stop() error
	Start(path, sha256 string) error
	Check(path string) (buildinfo.Executable, error)
}

func (s Store) Rollback(c Control) error {
	unlock, err := s.Lock()
	if err != nil {
		return err
	}
	defer unlock()
	st, err := s.Read()
	if err != nil {
		return err
	}
	target := st.Previous
	if st.Pending != nil {
		target = st.Current
	}
	if st.Pending != nil && target == nil {
		live, err := c.Running()
		if err != nil {
			return err
		}
		if live != nil && live.SHA256 != st.Pending.Artifact.SHA256 {
			return errors.New("another Runtime is active; recovery aborted")
		}
		if err = c.Stop(); err != nil {
			return err
		}
		return s.save(State{})
	}
	if target == nil {
		return errors.New("no previous verified version is available")
	}
	if err = s.Verify(*target); err != nil {
		return err
	}
	return s.activate(st, *target, c)
}
func (s Store) activate(st State, next Manifest, c Control) error {
	original := st
	original.Pending = nil
	st.Pending = &next
	if err := s.save(st); err != nil {
		return err
	}
	if err := c.Stop(); err != nil {
		if saveErr := s.save(original); saveErr != nil {
			return fmt.Errorf("stop failed (%v); recovery state could not be saved: %w", err, saveErr)
		}
		return fmt.Errorf("could not stop Runtime; activation cancelled: %w", err)
	}
	if err := c.Start(s.Path(next), next.Artifact.SHA256); err != nil {
		return s.restore(original, c, err)
	}
	applied := State{Current: &next, Previous: original.Current}
	if original.Current != nil && original.Current.Artifact.SHA256 == next.Artifact.SHA256 {
		applied.Previous = original.Previous
	}
	if err := s.save(applied); err != nil {
		if stopErr := c.Stop(); stopErr != nil {
			return fmt.Errorf("save failed (%v), stopping candidate failed (%v); recovery required", err, stopErr)
		}
		return s.restore(original, c, err)
	}
	return nil
}
func (s Store) restore(original State, c Control, cause error) error {
	if original.Current != nil {
		if err := s.Verify(*original.Current); err != nil {
			return fmt.Errorf("activation failed (%v); rollback verification failed: %w", cause, err)
		}
		if err := c.Start(s.Path(*original.Current), original.Current.Artifact.SHA256); err != nil {
			return fmt.Errorf("activation failed (%v); rollback failed: %w", cause, err)
		}
	}
	if err := s.save(original); err != nil {
		return fmt.Errorf("activation failed (%v); recovery state could not be saved: %w", cause, err)
	}
	return fmt.Errorf("activation failed; previous state restored: %w", cause)
}
