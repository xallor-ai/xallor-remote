package runtimeowner

import (
	"errors"
	"github.com/xallor-ai/xallor-remote/internal/release"
)

func applyFixtureStop(tx *Transaction, i release.EffectIntent, run func() (release.StopObservation, error), observe func() (release.SelectionEvidence, error)) error {
	r := release.ResourceObligation{ID: i.Stop.AttemptID, AttemptID: i.Stop.AttemptID, Generation: 1, Phase: ResourcePhaseEffects, Kind: ResourceKindObserver, ObserverScope: ObserverScopeOwnerProcess, Retention: ResourceReleaseRequired}
	if err := tx.RecordResourceIntent(ResourceObligation(r)); err != nil {
		return errors.Join(ErrUncertain, release.ErrCleanupUncertain, err)
	}
	return tx.ApplyStop(i, r, func() (release.StopObservation, []release.StopClosure, error) {
		o, e := run()
		return o, []release.StopClosure{{Resource: r, Closed: true}}, e
	}, observe)
}
