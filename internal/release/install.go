package release

import (
	"context"
	"errors"
	"time"
)

// Install authenticates the same private snapshot as runtime verify before
// taking a lock, creating an install directory, probing or stopping processes.
func (s Store) Install(bundle string, c Control) error {
	s, c = s.transition("requested", c)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_, err := s.withSourceSnapshot(ctx, bundle, "", func(binary string, next Manifest, raw []byte) error {
		if guard := s.bootstrapGuard; guard != nil {
			s.bootstrapGuard = nil
			return guard(func() error { return s.installAuthenticatedSnapshot(binary, next, raw, c) })
		}
		return s.installAuthenticatedSnapshot(binary, next, raw, c)
	})
	return err
}

func (s Store) installAuthenticatedSnapshot(binary string, next Manifest, raw []byte, c Control) (resultErr error) {
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
	original := st
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	live, err := c.Running()
	if err != nil {
		return err
	}
	if s.effects != nil && live != nil && !ObserveSelection(st).Current.Complete() {
		return ErrUnmanagedBaseline
	}
	var adopted *Activation
	st, adopted, err = s.prepareBaseline(ctx, st, live, c)
	if adopted != nil {
		defer func() { resultErr = s.discardUnstartedActivation(*adopted, resultErr) }()
	}
	if err != nil {
		return err
	}
	if err := s.copyAuthenticatedSnapshot(binary, next, raw); err != nil {
		return err
	}
	candidateActivation, err := s.StageActivation(ctx, binary, next, raw)
	if err != nil {
		return err
	}
	// Only this fresh candidate's receipt is eligible for cleanup. Once pending
	// or selected, persisted state retains it; an uncertain probe retains it too.
	defer func() { resultErr = s.discardUnstartedActivation(candidateActivation, resultErr) }()
	if err := s.preflightActivation(c, candidateActivation); err != nil {
		return err
	}
	current, err := c.Running()
	if err != nil {
		return err
	}
	if (live == nil) != (current == nil) || (live != nil && *current != *live) {
		return errors.New("running baseline changed during installation")
	}
	if st.Current != nil && st.Current.Artifact.SHA256 == next.Artifact.SHA256 {
		return errors.New("this version is already selected")
	}
	if st.Current != nil {
		if _, err := s.Activation(st.CurrentActivation, *st.Current); err != nil {
			return err
		}
	}
	if err := s.bind(resolvedIntent(original, candidateActivation)); err != nil {
		return err
	}
	baselineStore, _ := s.transition("baseline_prepare", c)
	if err := baselineStore.save(st); err != nil {
		return err
	}
	return s.activate(st, next, candidateActivation, c)
}
