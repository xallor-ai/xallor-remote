package runtimeowner

import (
	"errors"

	"github.com/xallor-ai/xallor-remote/internal/release"
)

var _ release.EffectRecorder = (*Transaction)(nil)

// Apply connects the release algorithm to the admitted owner's journal. Start
// errors without ErrCleanupUncertain rely on Control's explicit cleanup contract;
// they mean resources are quiescent, not that no process ever executed.
func (t *Transaction) Apply(intent release.EffectIntent, run func() error, observe func() (release.SelectionEvidence, error)) error {
	if run == nil || observe == nil {
		return errors.Join(release.ErrCleanupUncertain, ErrInvalid)
	}
	err := t.Effect(intent, func() (EffectState, *release.SelectionEvidence, error) {
		cause := run()
		observed, captureErr := observe()
		if captureErr != nil {
			return EffectUnknown, nil, errors.Join(cause, captureErr)
		}
		if cause == nil {
			return EffectConfirmed, &observed, nil
		}
		if errors.Is(cause, release.ErrCleanupUncertain) {
			return EffectUnknown, &observed, cause
		}
		switch intent.Name {
		case "stop":
			return EffectNotApplied, &observed, cause
		case "start":
			return EffectQuiescentFailure, &observed, cause
		default:
			return EffectUnknown, &observed, cause
		}
	})
	if errors.Is(err, ErrUncertain) || errors.Is(err, ErrInvalid) {
		return errors.Join(release.ErrCleanupUncertain, err)
	}
	return err
}
