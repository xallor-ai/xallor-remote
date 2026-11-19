package release

import (
	"context"
	"errors"
)

// mechanicsStop represents synthetic process/resource observations explicitly.
// It never authenticates real OS handles or signed source provenance.
type mechanicsStop struct {
	observer     ResourceObligation
	intent       StopIntent
	run          func() error
	used, closed bool
}

func (a *mechanicsStop) Intent() StopIntent { return a.intent }
func (a *mechanicsStop) Close() ([]StopClosure, error) {
	a.closed = true
	return []StopClosure{{Resource: a.observer, Closed: true}}, nil
}
func (a *mechanicsStop) Run(context.Context) (StopObservation, error) {
	o := StopObservation{AttemptID: a.intent.AttemptID, Dispatch: "not_sent", Process: "not_observed", RuntimeResource: "released_under_lock", Jobs: "empty", Observers: "released", Result: "quiescent_no_peer"}
	if a.used || a.closed {
		return o, ErrCleanupUncertain
	}
	a.used = true
	if a.intent.AbsenceBasis == "explicit_legacy_recovery" {
		o.RuntimeResource = "held_by_recovery"
	}
	if a.intent.Mode == "peer" {
		o.Dispatch = "acknowledged"
		o.Process = "exit_observed"
		o.Result = "released_peer"
	}
	err := a.run()
	if err != nil {
		o.Dispatch = "not_sent"
		o.Result = "not_dispatched"
		if a.intent.Mode == "peer" {
			o.Process = "still_running_or_unknown"
		}
		if errors.Is(err, ErrCleanupUncertain) {
			o.Dispatch = "may_have_been_sent"
			o.Result = "unknown"
		}
	}
	return o, err
}
func (f *fakeControl) PrepareStop(_ context.Context, r StopRequest) (StopAttempt, error) {
	return f.prepareMechanicsStop(r, f.Stop)
}
func (f *fakeControl) prepareMechanicsStop(r StopRequest, run func() error) (StopAttempt, error) {
	i := StopIntent{AttemptID: r.Observer.AttemptID, Mode: "no_peer", AbsenceBasis: r.AbsencePolicy}
	if f.live != nil {
		i = StopIntent{AttemptID: i.AttemptID, Mode: "peer", PeerPID: f.live.PID, ImageSHA256: f.live.SHA256, IdentityScope: "process_image_and_launch", Selection: SelectionIdentity{SHA256: f.live.SHA256, ActivationID: f.live.ActivationID, LaunchID: f.live.LaunchID}}
	}
	if !i.Valid() {
		return nil, errors.New("mechanics peer has no captured managed launch or no absence policy")
	}
	return &mechanicsStop{observer: r.Observer, intent: i, run: run}, nil
}
func (c *uncertainControl) PrepareStop(_ context.Context, r StopRequest) (StopAttempt, error) {
	return c.fakeControl.prepareMechanicsStop(r, c.Stop)
}
func (e *effectTrace) ApplyStop(i EffectIntent, _ ResourceObligation, run func() (StopObservation, []StopClosure, error), observe func() (SelectionEvidence, error)) error {
	return e.Apply(i, func() error { _, _, err := run(); return err }, observe)
}
