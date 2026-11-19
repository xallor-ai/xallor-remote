package release

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type StopRequest struct {
	Observer         ResourceObligation
	Before           SelectionEvidence
	Allowed          []SelectionIdentity
	AbsencePolicy    string
	ObserveResources func() (string, string, error)
}

type StopAttempt interface {
	Intent() StopIntent
	Run(context.Context) (StopObservation, error)
	Close() ([]StopClosure, error)
}

type StopClosure struct {
	Resource ResourceObligation
	Closed   bool
}

func (s Store) stop(c Control, original State) (result error) {
	before, err := s.observeSelection()
	if err != nil {
		return errors.Join(ErrCleanupUncertain, err)
	}
	id, attemptID, err := resourceIdentity()
	if err != nil {
		return errors.Join(ErrCleanupUncertain, err)
	}
	request := StopRequest{Before: before, Observer: ResourceObligation{ID: id, AttemptID: attemptID, Generation: 1, Phase: "effects", Kind: "observer", ObserverScope: resourceScopeOwner, Retention: resourceRelease}}
	old := ObserveSelection(original)
	for _, id := range []SelectionIdentity{old.Current, old.Pending} {
		if id.SHA256 != "" {
			request.Allowed = append(request.Allowed, id)
		}
	}
	if old == ObserveSelection(State{}) {
		request.AbsencePolicy = "fresh"
	}
	if original.Pending != nil {
		request.AbsencePolicy = "explicit_legacy_recovery"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	capturedRequest := request
	capturedRequest.Allowed = append([]SelectionIdentity(nil), request.Allowed...)
	if s.resources != nil {
		if err := s.resources.RecordIntent(request.Observer); err != nil {
			return errors.Join(ErrCleanupUncertain, err)
		}
	}
	attempt, err := c.PrepareStop(ctx, capturedRequest)
	ran := false
	defer func() {
		if !ran {
			cleanup := error(ErrCleanupUncertain)
			if attempt != nil {
				closures, closeErr := attempt.Close()
				cleanup = StopCleanup(request.Observer, closures, closeErr)
			}
			result = errors.Join(result, resourceCleanup(s.resources, request.Observer, cleanup))
		}
	}()
	if err != nil || attempt == nil {
		return errors.Join(ErrCleanupUncertain, err)
	}
	intent := attempt.Intent()
	if intent.AttemptID != request.Observer.AttemptID || !intent.Valid() || intent.AbsenceBasis == "public_observation" || (intent.Mode == "peer" && intent.IdentityScope != "process_image_and_launch") {
		return errors.Join(ErrCleanupUncertain, errors.New("invalid prepared stop intent"))
	}
	if intent.Mode == "no_peer" && intent.AbsenceBasis != request.AbsencePolicy {
		return errors.Join(ErrCleanupUncertain, errors.New("unadmitted absence"))
	}
	if intent.Mode == "peer" {
		allowed := false
		for _, id := range request.Allowed {
			allowed = allowed || id == intent.Selection
		}
		if !allowed {
			return errors.Join(ErrCleanupUncertain, errors.New("unexpected stop selection"))
		}
	}
	run := func() (StopObservation, []StopClosure, error) {
		ran = true
		runContext, stopRun := context.WithTimeout(context.Background(), 8*time.Second)
		defer stopRun()
		observation, cause := attempt.Run(runContext)
		closures, closeErr := attempt.Close()
		closeErr = StopCleanup(request.Observer, closures, closeErr)
		if closeErr != nil {
			closures = nil // Failed physical cleanup cannot produce a closed ledger receipt.
			observation.Observers = "unknown"
			observation.Result = "unknown"
			cause = errors.Join(cause, ErrCleanupUncertain, closeErr)
		}
		if cause != nil && !errors.Is(cause, ErrCleanupUncertain) && observation.NotDispatched(intent) {
			return observation, closures, cause
		}
		if !observation.Confirms(intent) {
			return observation, closures, errors.Join(cause, fmt.Errorf("%w: incomplete stop evidence", ErrCleanupUncertain))
		}
		return observation, closures, cause
	}
	effect := EffectIntent{Purpose: s.purpose, Name: "stop", Before: before, Stop: &intent}
	if s.effects != nil {
		return s.effects.ApplyStop(effect, request.Observer, run, s.observeSelection)
	}
	_, closures, runErr := run()
	return errors.Join(runErr, resourceCleanup(s.resources, request.Observer, StopCleanup(request.Observer, closures, nil)))
}

// StopCleanup requires one exact original preparation group, even on partial failure.
func StopCleanup(expected ResourceObligation, closures []StopClosure, err error) error {
	if err != nil || len(closures) != 1 || closures[0].Resource != expected || !closures[0].Closed {
		return errors.Join(ErrCleanupUncertain, err)
	}
	return nil
}
