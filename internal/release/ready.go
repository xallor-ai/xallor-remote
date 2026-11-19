package release

import (
	"context"
	"errors"
	"time"
)

// ReadyRequest is immutable observation input. Preparing an attempt allocates
// no native handles; Run owns those handles after the durable effect begins.
type ReadyRequest struct {
	StoreDir, Binary string
	Manifest         Manifest
	Selection        SelectionEvidence
	Observer         ResourceObligation
}
type ReadyObservation struct {
	Selection                                         SelectionEvidence
	PeerPID                                           int64
	ImageSHA256, ManifestSHA256, SourceSnapshotSHA256 string
	Authenticated                                     bool
}
type ReadyClosure struct {
	Resource ResourceObligation
	Closed   bool
}
type ReadyAttempt interface {
	Run(context.Context) (ReadyObservation, error)
	Close() ([]ReadyClosure, error)
}

func (o ReadyObservation) Valid(r ReadyRequest) bool {
	return o.Authenticated && o.PeerPID > 0 && o.Selection == r.Selection && o.Selection.Current.Complete() && o.ImageSHA256 == r.Manifest.Artifact.SHA256 && evidenceSHA.MatchString(o.ManifestSHA256) && evidenceSHA.MatchString(o.SourceSnapshotSHA256)
}

func (s Store) ready(c Control) (ReadyObservation, error) {
	st, err := s.Read()
	if err != nil || st.Current == nil || st.Pending != nil {
		return ReadyObservation{}, errors.Join(ErrCleanupUncertain, err)
	}
	a, err := s.Activation(st.CurrentActivation, *st.Current)
	if err != nil {
		return ReadyObservation{}, err
	}
	id, attempt, err := resourceIdentity()
	if err != nil {
		return ReadyObservation{}, err
	}
	request := ReadyRequest{StoreDir: s.Dir, Binary: a.Binary, Manifest: *st.Current, Selection: ObserveSelection(st), Observer: ResourceObligation{ID: id, AttemptID: attempt, Generation: 1, Phase: "effects", Kind: "observer", ActivationID: st.CurrentActivation, LaunchID: st.CurrentLaunchID, ImageSHA256: st.Current.Artifact.SHA256, ObserverScope: resourceScopeOwner, Retention: resourceRelease}}
	if s.resources != nil {
		if err := s.resources.RecordIntent(request.Observer); err != nil {
			return ReadyObservation{}, errors.Join(ErrCleanupUncertain, err)
		}
	}
	prepared, err := c.PrepareReady(request)
	if err != nil || prepared == nil {
		// Partial preparation must return its original cleanup receipt. A missing
		// attempt is unknown debt, never evidence of non-allocation.
		cleanupErr := error(ErrCleanupUncertain)
		if prepared != nil {
			closures, closeErr := prepared.Close()
			if len(closures) == 1 && closures[0].Resource == request.Observer && closures[0].Closed && closeErr == nil {
				cleanupErr = nil
			} else {
				cleanupErr = errors.Join(cleanupErr, closeErr)
			}
		}
		cleanupErr = resourceCleanup(s.resources, request.Observer, cleanupErr)
		return ReadyObservation{}, errors.Join(ErrCleanupUncertain, err, cleanupErr)
	}
	if s.effects != nil {
		intent := EffectIntent{Name: "ready", Purpose: s.purpose, TargetSHA256: st.Current.Artifact.SHA256, Target: request.Selection.Current, Before: request.Selection}
		return s.effects.ApplyReady(intent, request, prepared, s.observeSelection)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 65*time.Second)
	defer cancel()
	observation, runErr := prepared.Run(ctx)
	closures, closeErr := prepared.Close()
	if len(closures) != 1 || closures[0].Resource != request.Observer || !closures[0].Closed {
		closeErr = errors.Join(closeErr, ErrCleanupUncertain)
	}
	closeErr = resourceCleanup(s.resources, request.Observer, closeErr)
	selected, selectErr := s.observeSelection()
	if runErr != nil || closeErr != nil || selectErr != nil || selected != request.Selection || !observation.Valid(request) {
		return observation, errors.Join(ErrCleanupUncertain, runErr, closeErr, selectErr)
	}
	return observation, nil
}
