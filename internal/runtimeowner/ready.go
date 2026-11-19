package runtimeowner

import (
	"context"
	"errors"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"time"
)

// ApplyReady closes exact observer debt under effect's existing mutex. Native
// callbacks return value observations only; none can call ResourceRecorder.
func (t *Transaction) ApplyReady(intent release.EffectIntent, request release.ReadyRequest, attempt release.ReadyAttempt, observe func() (release.SelectionEvidence, error)) (result release.ReadyObservation, resultErr error) {
	observer := ResourceObligation(request.Observer)
	target := request.Selection.Current
	if attempt == nil || observe == nil || intent.Name != "ready" || intent.Before != request.Selection || !target.Complete() || !validResourceIdentity(observer) || observer.Kind != ResourceKindObserver || observer.Phase != ResourcePhaseEffects || observer.Retention != ResourceReleaseRequired || observer.ActivationID != target.ActivationID || observer.LaunchID != target.LaunchID || observer.ImageSHA256 != target.SHA256 || request.Manifest.Artifact.SHA256 != target.SHA256 {
		return result, errors.Join(release.ErrCleanupUncertain, ErrInvalid)
	}
	ran := false
	resultErr = t.effect(intent, func() (EffectState, *release.SelectionEvidence, error) {
		ran = true
		ctx, cancel := context.WithTimeout(context.Background(), 65*time.Second)
		defer cancel()
		observation, runErr := attempt.Run(ctx)
		result = observation
		closures, closeErr := attempt.Close()
		if len(closures) != 1 || closures[0].Resource != request.Observer || !closures[0].Closed {
			closeErr = errors.Join(closeErr, ErrUncertain)
		}
		status, cause := ResourceClosed, CauseClosed
		if closeErr != nil {
			status, cause = ResourceUnknown, CauseCleanupUnknown
		}
		identity := ResourceObligation(request.Observer)
		durableErr := t.updateResourceLocked(identity.ID, identity, status, ResourceClosure{Cause: cause})
		selected, selectionErr := observe()
		if runErr != nil || closeErr != nil || durableErr != nil || selectionErr != nil || !observation.Valid(request) || selected != observation.Selection {
			return EffectUnknown, &selected, errors.Join(ErrUncertain, runErr, closeErr, durableErr, selectionErr)
		}
		if intent.Purpose == "requested" && (t.record.Binding == nil || observation.ManifestSHA256 != t.record.Binding.ManifestSHA256) {
			return EffectUnknown, &selected, ErrInvalid
		}
		return EffectConfirmed, &selected, nil
	}, nil)
	if !ran {
		// The prepared attempt has no native allocations until Run, but still must
		// supply its exact non-allocation cleanup receipt.
		closures, err := attempt.Close()
		if len(closures) == 1 && closures[0].Resource == request.Observer && closures[0].Closed && err == nil {
			err = t.CloseResource(request.Observer.ID, ResourceObligation(request.Observer), ResourceClosure{Cause: CauseClosed})
		}
		resultErr = errors.Join(resultErr, err)
	}
	if resultErr != nil {
		resultErr = errors.Join(release.ErrCleanupUncertain, resultErr)
	}
	return
}
