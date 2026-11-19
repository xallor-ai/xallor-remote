package release

import (
	"context"
	"errors"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/runtimeimage"
)

// RecoveryActivation authenticates the exact retained baseline for explicit
// pending recovery. A failed first initialization has only a pending image;
// that image may run the recovery role, not serve or replay its failed startup.
func (s Store) RecoveryActivation(ctx context.Context) (Activation, State, error) {
	state, err := s.Read()
	if err != nil {
		return Activation{}, state, err
	}
	if state.Pending == nil || !observedActivationID.MatchString(state.PendingActivation) {
		return Activation{}, state, errors.New("no complete pending recovery selection")
	}
	for _, entry := range []struct {
		m  *Manifest
		id string
	}{{state.Current, state.CurrentActivation}, {state.Previous, state.PreviousActivation}} {
		if (entry.m == nil) != (entry.id == "") || (entry.id != "" && !observedActivationID.MatchString(entry.id)) {
			return Activation{}, state, errors.New("incomplete recovery references")
		}
	}
	manifest, id := state.Current, state.CurrentActivation
	if manifest == nil {
		if state.Previous != nil {
			return Activation{}, state, errors.New("recovery baseline selection is missing")
		}
		manifest, id = state.Pending, state.PendingActivation
	}
	activation, err := s.authenticatedActivation(ctx, id, *manifest)
	if err != nil {
		return Activation{}, state, err
	}
	latest, err := s.Read()
	if err != nil {
		return Activation{}, state, err
	}
	if launchStateDigest(latest) != launchStateDigest(state) {
		return Activation{}, state, errors.New("pending selection changed during recovery authentication")
	}
	if err := ctx.Err(); err != nil {
		return Activation{}, state, err
	}
	return activation, state, nil
}

func (s Store) NewRecoveryHandoff(ctx context.Context, parentPID int, id, intent string) (OwnerHandoff, Activation, error) {
	if parentPID <= 0 || !observedActivationID.MatchString(id) || !digestPattern.MatchString(intent) {
		return OwnerHandoff{}, Activation{}, errors.New("invalid recovery request binding")
	}
	a, state, err := s.RecoveryActivation(ctx)
	if err != nil {
		return OwnerHandoff{}, Activation{}, err
	}
	profile, err := s.launchProfileDigest()
	if err != nil {
		return OwnerHandoff{}, Activation{}, err
	}
	return OwnerHandoff{ParentPID: parentPID, OperationID: id, IntentSHA256: intent, ActivationID: a.ID, ImageSHA256: a.Manifest.Artifact.SHA256,
		ProfileSHA256: profile, StateSHA256: launchStateDigest(state)}, a, nil
}

// The cold bootstrap exception authenticates the worker itself, not an absent
// serving parent. The caller additionally proves no live owner, free service/job
// resources and no unfinished journal under lifecycle admission before mutation.
func (s Store) AuthorizeRecoveryHandoff(ctx context.Context, p OwnerHandoff, worker buildinfo.Executable, parentPID int, id, intent string) error {
	if worker.PID <= 0 || worker.PID == parentPID || p.ParentPID != parentPID || worker.OwnerProtocol != buildinfo.RuntimeOwnerProtocol {
		return errors.New("invalid recovery worker identity")
	}
	fresh, a, err := s.NewRecoveryHandoff(ctx, parentPID, id, intent)
	if err != nil {
		return err
	}
	if fresh != p || worker.Info != a.Manifest.Build || worker.SHA256 != p.ImageSHA256 || !runtimeimage.MatchesPath(worker.Path, a.Binary) {
		return errors.New("recovery worker or request differs from retained source")
	}
	return nil
}
