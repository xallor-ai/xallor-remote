package release

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/xallor-ai/xallor-remote/internal/filelock"
	"os"
	"path/filepath"
	"time"
)

type Store struct {
	Dir string
	// Production always uses nil (real provenance). Private injection is only
	// available to package tests for lifecycle mechanics, never through CLI/env.
	sourceVerifier sourceVerifier
	// Tests may replace kernel image observation independently of provenance.
	// Production never accepts this dependency from heads or environment.
	handoffImageVerifier handoffImageVerifier
	purpose              string
	effects              EffectRecorder
	resources            ResourceRecorder
	bootstrapGuard       func(func() error) error
}
type State struct {
	Current            *Manifest `json:"current,omitempty"`
	Previous           *Manifest `json:"previous,omitempty"`
	Pending            *Manifest `json:"pending,omitempty"`
	CurrentActivation  string    `json:"current_activation,omitempty"`
	PreviousActivation string    `json:"previous_activation,omitempty"`
	PendingActivation  string    `json:"pending_activation,omitempty"`
	CurrentLaunchID    string    `json:"current_launch_id,omitempty"`
	PendingLaunchID    string    `json:"pending_launch_id,omitempty"`
}

func (s Store) Lock() (func(), error) {
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		return nil, err
	}
	return filelock.Acquire(filepath.Join(s.Dir, "install.lock"))
}
func (s Store) Path(m Manifest) string {
	return filepath.Join(s.Dir, "versions", m.Artifact.SHA256, m.Artifact.Filename)
}
func (s Store) Read() (State, error) {
	var state State
	err := readStateJSON(filepath.Join(s.Dir, "state.json"), &state)
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	for _, m := range []*Manifest{state.Current, state.Previous, state.Pending} {
		if m != nil {
			if err = m.validate(); err != nil {
				return state, err
			}
		}
	}
	if (state.CurrentLaunchID != "" && (state.Current == nil || !observedActivationID.MatchString(state.CurrentLaunchID))) ||
		(state.PendingLaunchID != "" && (state.Pending == nil || !observedActivationID.MatchString(state.PendingLaunchID))) {
		return state, errors.New("invalid Runtime launch selection identity")
	}
	return state, nil
}
func (s Store) save(state State) error {
	if s.effects != nil {
		return s.recordSave(state)
	}
	return s.saveRaw(state)
}
func (s Store) saveRaw(state State) error {
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(s.Dir, "state-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	_, err = f.Write(b)
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
	// Root.Rename uses handle-based POSIX replacement semantics on supported
	// Windows filesystems, keeping existing snapshot readers valid. os.Rename's
	// MoveFileEx path can deny replacement merely because a reader is open.
	root, err := os.OpenRoot(s.Dir)
	if err != nil {
		return err
	}
	return errors.Join(root.Rename(filepath.Base(name), "state.json"), root.Close())
}

// ActivePath fails closed on corrupt state/artifacts rather than launching a fallback.
func (s Store) ActivePath() (string, error) {
	st, err := s.Read()
	if err != nil {
		return "", err
	}
	if st.Pending != nil {
		return "", errors.New("interrupted install; run runtime rollback")
	}
	if st.Current == nil {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	activation, err := s.authenticatedActivation(ctx, st.CurrentActivation, *st.Current)
	if err != nil {
		return "", err
	}
	return activation.Binary, nil
}
