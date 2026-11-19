package release

import (
	"context"
	"errors"
	"fmt"
	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"time"
)

// Control must bound requests and verify the launched process's digest before
// success. PrepareProbe reports owner protocol capability; errors with uncertain
// process/image release must wrap ErrCleanupUncertain.
// PrepareStop captures observers without dispatch; StopAttempt owns one dispatch.
// A plain Start error proves
// the attempted process and its image resources are released; it does not prove
// the process never ran. Ready errors never establish a missing commit.
type Control interface {
	Running() (*buildinfo.Executable, error)
	PrepareStop(context.Context, StopRequest) (StopAttempt, error)
	Start(path, sha256 string) error
	PrepareProbe(ProbeRequest) (ProbeAttempt, error)
	PrepareReady(ReadyRequest) (ReadyAttempt, error)
}

func (s Store) Rollback(c Control) error {
	if guard := s.bootstrapGuard; guard != nil {
		s.bootstrapGuard = nil
		return guard(func() error { return s.Rollback(c) })
	}
	s, c = s.transition("requested", c)
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
		if err := s.bind(ResolvedIntent{Baseline: ObserveSelection(st), ClearOnly: true}); err != nil {
			return err
		}
		live, err := c.Running()
		if err != nil {
			return err
		}
		if live != nil && live.SHA256 != st.Pending.Artifact.SHA256 {
			return errors.New("another Runtime is active; recovery aborted")
		}
		if err = s.stop(c, st); err != nil {
			return err
		}
		return s.save(State{})
	}
	if target == nil {
		return errors.New("no previous verified version is available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	activationID := st.PreviousActivation
	if st.Pending != nil {
		activationID = st.CurrentActivation
	}
	activation, err := s.authenticatedActivation(ctx, activationID, *target)
	if err != nil {
		return err
	}
	if err := s.preflightActivation(c, activation); err != nil {
		return err
	}
	if st.Current != nil && st.CurrentActivation != activation.ID {
		baseline, err := s.authenticatedActivation(ctx, st.CurrentActivation, *st.Current)
		if err != nil {
			return err
		}
		if err := s.preflightActivation(c, baseline); err != nil {
			return err
		}
	}
	if err := s.bind(resolvedIntent(st, activation)); err != nil {
		return err
	}
	return s.activate(st, *target, activation, c)
}
func (s Store) activate(st State, next Manifest, activation Activation, c Control) error {
	beforeAttempt := st
	original := st
	original.Pending = nil
	original.PendingActivation = ""
	original.PendingLaunchID = ""
	recovering := st.Pending != nil
	if !recovering {
		st.Pending = &next
		st.PendingActivation = activation.ID
		launchID, err := activationLaunch(activation)
		if err != nil {
			return err
		}
		st.PendingLaunchID = launchID
		if err := s.save(st); err != nil {
			return err
		}
	}
	if err := s.stop(c, beforeAttempt); err != nil {
		if errors.Is(err, ErrCleanupUncertain) {
			return fmt.Errorf("Runtime stop is unconfirmed; pending recovery retained: %w", err)
		}
		cancelStore, cancelControl := s.transition("cancel", c)
		if saveErr := cancelStore.save(beforeAttempt); saveErr != nil {
			return fmt.Errorf("stop failed (%v); recovery state could not be saved: %w", err, saveErr)
		}
		if s.effects != nil && beforeAttempt.Current != nil && beforeAttempt.Pending == nil {
			if _, readyErr := cancelStore.ready(cancelControl); readyErr != nil {
				return fmt.Errorf("stop failed (%v); baseline acknowledgement unconfirmed: %w", err, readyErr)
			}
		}
		return fmt.Errorf("could not stop Runtime; activation cancelled: %w", err)
	}
	// An interrupted candidate's identity remains authoritative until its stop
	// is confirmed. Only then may recovery replace pending with its own target.
	if recovering {
		st.Pending = &next
		st.PendingActivation = activation.ID
		launchID, err := activationID()
		if err != nil {
			return err
		}
		st.PendingLaunchID = launchID
		if err := s.save(st); err != nil {
			return fmt.Errorf("Runtime stopped but recovery transition could not be saved: %w", err)
		}
	}
	if err := c.Start(activation.Binary, next.Artifact.SHA256); err != nil {
		if errors.Is(err, ErrCleanupUncertain) {
			return fmt.Errorf("candidate cleanup is unconfirmed; pending recovery retained: %w", err)
		}
		return s.restore(original, c, err)
	}
	applied := State{Current: &next, Previous: original.Current, CurrentActivation: activation.ID, PreviousActivation: original.CurrentActivation, CurrentLaunchID: st.PendingLaunchID}
	if original.Current != nil && original.Current.Artifact.SHA256 == next.Artifact.SHA256 {
		applied.Previous = original.Previous
		applied.PreviousActivation = original.PreviousActivation
	}
	if err := s.save(applied); err != nil {
		if errors.Is(err, ErrCleanupUncertain) {
			return fmt.Errorf("selection commit is unconfirmed; preserve candidate and inspect state: %w", err)
		}
		if stopErr := s.stop(c, st); stopErr != nil {
			return fmt.Errorf("save failed (%v), stopping candidate failed (%v); recovery required", err, stopErr)
		}
		return s.restore(original, c, err)
	}
	observation, err := s.ready(c)
	if err != nil {
		return fmt.Errorf("selection committed but Runtime acknowledgement unconfirmed; inspect status before further changes: %w", err)
	}
	if s.resources != nil && activation.ownership != nil {
		if err := s.resources.RetainSelected(activation.ownership.obligation, observation); err != nil {
			return errors.Join(ErrCleanupUncertain, err)
		}
	}
	return nil
}
func (s Store) restore(original State, c Control, cause error) error {
	s, c = s.transition("restore", c)
	if original.Current != nil {
		pending, err := s.Read()
		if err != nil {
			return errors.Join(cause, err)
		}
		if pending.Pending == nil || !observedActivationID.MatchString(pending.PendingLaunchID) {
			return errors.Join(cause, errors.New("restoration has no active launch identity"))
		}
		original.CurrentLaunchID = pending.PendingLaunchID
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		activation, verifyErr := s.authenticatedActivation(ctx, original.CurrentActivation, *original.Current)
		cancel()
		if verifyErr != nil {
			return fmt.Errorf("activation failed (%v); rollback verification failed: %w", cause, verifyErr)
		}
		if err := s.preflightActivation(c, activation); err != nil {
			return fmt.Errorf("activation failed (%v); rollback preflight failed: %w", cause, err)
		}
		if err := c.Start(activation.Binary, original.Current.Artifact.SHA256); err != nil {
			return fmt.Errorf("activation failed (%v); rollback failed: %w", cause, err)
		}
	}
	if err := s.save(original); err != nil {
		return fmt.Errorf("activation failed (%v); recovery state could not be saved: %w", cause, err)
	}
	if original.Current != nil {
		if _, err := s.ready(c); err != nil {
			return fmt.Errorf("activation failed (%v); restored selection acknowledgement unconfirmed: %w", cause, err)
		}
	}
	return fmt.Errorf("activation failed; previous state restored: %w", cause)
}
