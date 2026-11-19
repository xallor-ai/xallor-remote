package release

import (
	"fmt"
	"github.com/xallor-ai/xallor-remote/internal/runtimeimage"
	"path/filepath"
)

// EffectRecorder durably records intent before run and records its observed
// result afterward. Its own failures must wrap ErrCleanupUncertain, including
// failures before dispatch: a lost journal must never authorize restoration.
// It must preserve deterministic Control errors when recording succeeds.
type EffectRecorder interface {
	ApplyReady(EffectIntent, ReadyRequest, ReadyAttempt, func() (SelectionEvidence, error)) (ReadyObservation, error)
	Bind(ResolvedIntent) error
	Apply(EffectIntent, func() error, func() (SelectionEvidence, error)) error
	ApplyStop(EffectIntent, ResourceObligation, func() (StopObservation, []StopClosure, error), func() (SelectionEvidence, error)) error
}

// WithEffects attaches the owner's admitted transaction to the existing single
// activation algorithm. It creates no second selector and performs no admission.
func (s Store) WithEffects(recorder EffectRecorder) Store { s.effects = recorder; return s }

func (s Store) recordControl(c Control) Control {
	if s.effects == nil {
		return c
	}
	return effectControl{Control: c, store: s}
}

type effectControl struct {
	Control
	store Store
}

func (c effectControl) Start(path, sha string) error {
	return c.store.applyEffect("start", path, sha, nil, func() error { return c.Control.Start(path, sha) })
}
func (s Store) recordSave(st State) error {
	name, sha := "clear", ""
	if st.Pending != nil {
		name, sha = "prepare", st.Pending.Artifact.SHA256
	} else if st.Current != nil {
		name, sha = "commit", st.Current.Artifact.SHA256
	}
	return s.applyEffect(name, "", sha, &st, func() error { return s.saveRaw(st) })
}

func (s Store) observeSelection() (SelectionEvidence, error) {
	st, err := s.Read()
	if err != nil {
		return SelectionEvidence{}, err
	}
	evidence := ObserveSelection(st)
	if !evidence.Valid() {
		return SelectionEvidence{}, fmt.Errorf("invalid selection evidence")
	}
	return evidence, nil
}

func (s Store) applyEffect(name, path, sha string, expected *State, run func() error) error {
	st, err := s.Read()
	if err != nil {
		return fmt.Errorf("%w: capture selection: %v", ErrCleanupUncertain, err)
	}
	intent := EffectIntent{Purpose: s.purpose, Name: name, TargetSHA256: sha, Before: ObserveSelection(st)}
	if expected != nil {
		after := ObserveSelection(*expected)
		intent.Expected = &after
		if name == "prepare" {
			intent.Target = after.Pending
		} else if name == "commit" {
			intent.Target = after.Current
		}
	} else if name == "ready" {
		intent.Target = intent.Before.Current
	} else if name == "start" {
		id := filepath.Base(filepath.Dir(path))
		m, e := launchSelection(st, id)
		if e != nil {
			return fmt.Errorf("%w: %v", ErrCleanupUncertain, e)
		}
		a, e := s.Activation(id, *m)
		if e != nil || !runtimeimage.MatchesPath(path, a.Binary) {
			return fmt.Errorf("%w: start target differs from selection", ErrCleanupUncertain)
		}
		intent.Target = selectionIdentity(m, id, st.PendingLaunchID)
	}
	if !intent.Valid() {
		return fmt.Errorf("%w: invalid effect binding", ErrCleanupUncertain)
	}
	return s.effects.Apply(intent, run, s.observeSelection)
}
