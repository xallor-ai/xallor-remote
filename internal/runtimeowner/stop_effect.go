package runtimeowner

import (
	"errors"
	"github.com/xallor-ai/xallor-remote/internal/release"
)

func (t *Transaction) ApplyStop(intent release.EffectIntent, observer release.ResourceObligation, run func() (release.StopObservation, []release.StopClosure, error), observe func() (release.SelectionEvidence, error)) error {
	identity := ResourceObligation(observer)
	if intent.Stop == nil || run == nil || observe == nil || !validResourceIdentity(identity) || observer.AttemptID != intent.Stop.AttemptID || observer.Kind != ResourceKindObserver || observer.Phase != ResourcePhaseEffects || observer.Retention != ResourceReleaseRequired || observer.ActivationID != "" || observer.LaunchID != "" || observer.ImageSHA256 != "" || observer.SourceSnapshotSHA256 != "" {
		return errors.Join(release.ErrCleanupUncertain, ErrInvalid)
	}
	captured := *intent.Stop
	intent.Stop = &captured
	var stop *release.StopObservation
	err := t.effect(intent, func() (EffectState, *release.SelectionEvidence, error) {
		rows, readErr := readResourceRows(t.db, t.record, t.revision)
		found := false
		for _, row := range rows {
			if row.ResourceObligation == identity && row.Status == ResourceAllocated {
				found = true
			}
		}
		if readErr != nil || !found {
			return EffectUnknown, nil, errors.Join(ErrUncertain, readErr)
		}
		observation, closures, cause := run()
		closeErr := release.StopCleanup(observer, closures, nil)
		status, closureCause := ResourceClosed, CauseClosed
		if closeErr != nil {
			status, closureCause = ResourceUnknown, CauseCleanupUnknown
		}
		durableErr := t.updateResourceLocked(identity.ID, identity, status, ResourceClosure{Cause: closureCause})
		if closeErr != nil || durableErr != nil {
			cause = errors.Join(cause, release.ErrCleanupUncertain, closeErr, durableErr)
		}
		stop = &observation
		selected, captureErr := observe()
		if captureErr != nil {
			return EffectUnknown, nil, errors.Join(cause, captureErr)
		}
		if cause != nil && !errors.Is(cause, release.ErrCleanupUncertain) && observation.NotDispatched(*intent.Stop) {
			return EffectNotApplied, &selected, cause
		}
		if cause != nil || !observation.Confirms(*intent.Stop) {
			return EffectUnknown, &selected, errors.Join(cause, ErrUncertain)
		}
		return EffectConfirmed, &selected, nil
	}, &stop)
	if errors.Is(err, ErrUncertain) || errors.Is(err, ErrInvalid) {
		return errors.Join(release.ErrCleanupUncertain, err)
	}
	return err
}
