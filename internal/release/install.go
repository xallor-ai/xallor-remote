package release

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Install authenticates the same private snapshot as runtime verify before
// taking a lock, creating an install directory, probing or stopping processes.
func (s Store) Install(bundle string, c Control) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_, err := withSourceSnapshot(ctx, bundle, "", s.sourceVerifier, func(binary string, next Manifest, raw []byte) error {
		return s.installAuthenticatedSnapshot(binary, next, raw, c)
	})
	return err
}

func (s Store) installAuthenticatedSnapshot(binary string, next Manifest, raw []byte, c Control) error {
	unlock, err := s.Lock()
	if err != nil {
		return err
	}
	defer unlock()
	st, err := s.Read()
	if err != nil {
		return err
	}
	if st.Pending != nil {
		return errors.New("previous install was interrupted; run runtime rollback first")
	}
	if err := s.copyAuthenticatedSnapshot(binary, next, raw); err != nil {
		return err
	}
	info, err := c.Check(s.Path(next))
	if err != nil {
		return fmt.Errorf("candidate cannot report build identity: %w", err)
	}
	if info.Info != next.Build || info.SHA256 != next.Artifact.SHA256 {
		return errors.New("candidate identity does not match manifest")
	}
	live, err := c.Running()
	if err != nil {
		return err
	}
	if live != nil {
		baseline, err := s.snapshot(*live)
		if err != nil {
			return fmt.Errorf("cannot preserve running version: %w", err)
		}
		st.Current = &baseline
	}
	if st.Current != nil && st.Current.Artifact.SHA256 == next.Artifact.SHA256 {
		return errors.New("this version is already selected")
	}
	if st.Current != nil {
		if err := s.Verify(*st.Current); err != nil {
			return err
		}
	}
	if err := s.save(st); err != nil {
		return err
	}
	return s.activate(st, next, c)
}
