package runtimeowner

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/release"
)

// Explicit synthetic native-observer outcomes exercise journal mechanics only.
func releasedStopObservation() release.StopObservation {
	return release.StopObservation{AttemptID: strings.Repeat("3", 32), Dispatch: "acknowledged", Process: "exit_observed", RuntimeResource: "released_under_lock", Jobs: "empty", Observers: "released", Result: "released_peer"}
}
func undispatchedStopObservation() release.StopObservation {
	o := releasedStopObservation()
	o.Dispatch = "not_sent"
	o.Process = "still_running_or_unknown"
	o.RuntimeResource = "unknown"
	o.Jobs = "unreleased_or_unknown"
	o.Result = "not_dispatched"
	return o
}
func testStop(tx *Transaction, run func() (release.StopObservation, error)) error {
	i := fixtureIntent(tx, "stop", "")
	return applyFixtureStop(tx, i, run, func() (release.SelectionEvidence, error) { return i.Before, nil })
}

// The actual captured peer and attempt must be durable before Run; mutating the
// caller's intent cannot alter either classification or the recorded capture.
func TestPreparedStop_should_ownCapturedIntent_beforeDispatch(t *testing.T) {
	j := journal(t)
	tx, err := j.Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	bindFixture(t, tx, boundIntent("stop", "").Before, strings.Repeat("b", 64))
	prefixFixture(t, tx, "stop")
	defer tx.Close()
	i := fixtureIntent(tx, "stop", "")
	captured := *i.Stop
	err = applyFixtureStop(tx, i, func() (release.StopObservation, error) {
		r, e := j.Query(request("1").ID)
		if e != nil || len(r.Effects) != 2 || r.Effects[len(r.Effects)-1].Stop == nil {
			t.Fatalf("missing durable capture: %+v %v", r, e)
		}
		if *r.Effects[len(r.Effects)-1].Stop != captured || r.Effects[len(r.Effects)-1].State != EffectPending || r.Effects[len(r.Effects)-1].StopObserved != nil {
			t.Fatal("capture was not pending", r)
		}
		i.Stop.AttemptID = strings.Repeat("4", 32)
		i.Stop.PeerPID = 456
		return releasedStopObservation(), nil
	}, func() (release.SelectionEvidence, error) { return i.Before, nil })
	if err != nil {
		t.Fatal("caller mutation changed owned attempt", err)
	}
	r, e := j.Query(request("1").ID)
	if e != nil || r.Effects[len(r.Effects)-1].State != EffectConfirmed || *r.Effects[len(r.Effects)-1].Stop != captured {
		t.Fatal(r, e)
	}
}

// A crash after durable capture must leave pending evidence and refuse takeover.
func TestPreparedStop_should_retainPending_whenRunPanics(t *testing.T) {
	j := journal(t)
	tx, err := j.Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	bindFixture(t, tx, boundIntent("stop", "").Before, strings.Repeat("b", 64))
	prefixFixture(t, tx, "stop")
	func() {
		defer func() {
			if recover() == nil {
				t.Error("missing injected panic")
			}
		}()
		_ = testStop(tx, func() (release.StopObservation, error) { panic("after capture") })
	}()
	if err := tx.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := j.Query(request("1").ID)
	if err != nil || r.Outcome != Pending || r.Effects[len(r.Effects)-1].State != EffectPending || r.Effects[len(r.Effects)-1].Stop == nil || r.Effects[len(r.Effects)-1].StopObserved != nil {
		t.Fatal(r, err)
	}
	if _, err := j.Admit(request("2")); !errors.Is(err, ErrPending) {
		t.Fatal("interrupted capture replayed", err)
	}
}

// Nil errors never compensate for missing, unbound or incomplete release proof;
// failed post-selection capture and close uncertainty must block later effects.
func TestPreparedStop_should_retainUnknown_whenEvidenceIncomplete(t *testing.T) {
	for _, mode := range []string{"missing", "wrong_attempt", "close", "process", "runtime", "jobs", "after_read", "after_mismatch", "after_send_error"} {
		t.Run(mode, func(t *testing.T) {
			j := journal(t)
			tx, err := j.Admit(request("1"))
			if err != nil {
				t.Fatal(err)
			}
			bindFixture(t, tx, boundIntent("stop", "").Before, strings.Repeat("b", 64))
			prefixFixture(t, tx, "stop")
			defer tx.Close()
			i := fixtureIntent(tx, "stop", "")
			o := releasedStopObservation()
			var cause error
			switch mode {
			case "missing":
				o = release.StopObservation{}
			case "wrong_attempt":
				o.AttemptID = strings.Repeat("4", 32)
			case "close":
				o.Observers = "unknown"
				cause = release.ErrCleanupUncertain
			case "process":
				o.Process = "still_running_or_unknown"
			case "runtime":
				o.RuntimeResource = "unknown"
			case "jobs":
				o.Jobs = "unreleased_or_unknown"
			case "after_send_error":
				o.Dispatch = "may_have_been_sent"
				cause = errors.New("lost send response")
			}
			err = applyFixtureStop(tx, i, func() (release.StopObservation, error) { return o, cause }, func() (release.SelectionEvidence, error) {
				if mode == "after_read" {
					return release.SelectionEvidence{}, errors.New("read failure")
				}
				if mode == "after_mismatch" {
					return boundIntent("ready", strings.Repeat("f", 64)).Before, nil
				}
				return i.Before, nil
			})
			r, e := j.Query(request("1").ID)
			if !errors.Is(err, release.ErrCleanupUncertain) || e != nil || r.Outcome != Uncertain || r.Effects[len(r.Effects)-1].State != EffectUnknown {
				t.Fatal(r, err, e)
			}
			if (mode == "missing" || mode == "wrong_attempt") && r.Effects[len(r.Effects)-1].StopObserved != nil {
				t.Fatal("malformed evidence retained as bound observation", r)
			}
			called := false
			if err := testApply(tx, "start", strings.Repeat("b", 64), func() error { called = true; return nil }); !errors.Is(err, release.ErrCleanupUncertain) || called {
				t.Fatal("restoration dispatched after uncertainty", err)
			}
			if tx.Finish(Succeeded) == nil || tx.Finish(Failed) == nil {
				t.Fatal("incomplete stop terminalized")
			}
		})
	}
}

// Fresh and explicit recovery absence are recorded as quiescence without any
// invented PID or process exit, and retain their distinct admission basis.
func TestPreparedStop_should_recordNoPeer_withoutInventingProcessExit(t *testing.T) {
	for _, basis := range []string{"fresh", "explicit_legacy_recovery"} {
		t.Run(basis, func(t *testing.T) {
			j := journal(t)
			req := request("1")
			if basis == "explicit_legacy_recovery" {
				req.Kind = "rollback"
			}
			tx, err := j.Admit(req)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Close()
			i := boundIntent("stop", "")
			i.Stop = &release.StopIntent{Mode: "no_peer", AttemptID: strings.Repeat("3", 32), AbsenceBasis: basis}
			i.Before = release.ObserveSelection(release.State{})
			o := releasedStopObservation()
			o.Dispatch, o.Process, o.Result = "not_sent", "not_observed", "quiescent_no_peer"
			if basis == "explicit_legacy_recovery" {
				i.Before = boundIntent("start", strings.Repeat("b", 64)).Before
				o.RuntimeResource = "held_by_recovery"
			}
			if basis == "explicit_legacy_recovery" {
				if err := tx.Bind(release.ResolvedIntent{Baseline: i.Before, ClearOnly: true}); err != nil {
					t.Fatal(err)
				}
			} else {
				bindFixture(t, tx, i.Before, strings.Repeat("b", 64))
				prefixFixture(t, tx, "stop")
				i.Before = fixtureIntent(tx, "stop", "").Before
			}
			err = applyFixtureStop(tx, i, func() (release.StopObservation, error) { return o, nil }, func() (release.SelectionEvidence, error) { return i.Before, nil })
			if err != nil {
				t.Fatal(err)
			}
			r, err := j.Query(request("1").ID)
			if err != nil {
				t.Fatal(err)
			}
			e := r.Effects[len(r.Effects)-1]
			if e.State != EffectConfirmed || e.Stop == nil || e.StopObserved == nil || e.Stop.PeerPID != 0 || e.Stop.ImageSHA256 != "" || e.Stop.AbsenceBasis != basis || e.StopObserved.Process != "not_observed" || e.StopObserved.Result != "quiescent_no_peer" {
				t.Fatal("no-peer proof became peer exit", r)
			}
		})
	}
}

// Only a bound not-sent observation with released observers can retain the old
// clean-refusal behavior; send/close uncertainty must never authorize restore.
func TestPreparedStop_should_preserveNotApplied_onlyAfterUndispatchedCleanup(t *testing.T) {
	for _, mode := range []string{"refused", "nil_error", "observer_unknown", "wrong_attempt", "possibly_sent", "cleanup_error"} {
		t.Run(mode, func(t *testing.T) {
			j := journal(t)
			tx, err := j.Admit(request("1"))
			if err != nil {
				t.Fatal(err)
			}
			bindFixture(t, tx, boundIntent("stop", "").Before, strings.Repeat("b", 64))
			prefixFixture(t, tx, "stop")
			defer tx.Close()
			o := undispatchedStopObservation()
			cause := errors.New("before dispatch refusal")
			switch mode {
			case "nil_error":
				cause = nil
			case "observer_unknown":
				o.Observers = "unknown"
			case "wrong_attempt":
				o.AttemptID = strings.Repeat("4", 32)
			case "possibly_sent":
				o.Dispatch = "may_have_been_sent"
			case "cleanup_error":
				cause = release.ErrCleanupUncertain
			}
			err = testStop(tx, func() (release.StopObservation, error) { return o, cause })
			r, e := j.Query(request("1").ID)
			if e != nil {
				t.Fatal(e)
			}
			clean := mode == "refused"
			if clean {
				if !errors.Is(err, cause) || errors.Is(err, release.ErrCleanupUncertain) || r.Effects[len(r.Effects)-1].State != EffectNotApplied || r.Outcome != Pending {
					t.Fatal("clean refusal lost", r, err)
				}
				if err := tx.Finish(Failed); err == nil {
					t.Fatal("prepared selection was never cancelled")
				}
				for _, name := range []string{"commit", "ready"} {
					intent := fixtureIntent(tx, name, strings.Repeat("b", 64))
					intent.Purpose = "cancel"
					if name == "commit" {
						baseline := tx.record.Binding.Baseline
						intent.Expected = &baseline
					}
					if err := tx.Apply(intent, func() error { return nil }, func() (release.SelectionEvidence, error) {
						if intent.Expected != nil {
							return *intent.Expected, nil
						}
						return intent.Before, nil
					}); err != nil {
						t.Fatal("cancel refused operation", err)
					}
				}
				if err := tx.Finish(Failed); err != nil {
					t.Fatal("cancelled refusal cannot finish", err)
				}
			} else if !errors.Is(err, release.ErrCleanupUncertain) || r.Outcome != Uncertain || r.Effects[len(r.Effects)-1].State != EffectUnknown {
				t.Fatal("unproven refusal accepted", r, err)
			}
		})
	}
}

// A generic callback does not own native evidence, even if it claims success.
func TestPreparedStop_should_refuseGenericSuccess_withoutStopObservation(t *testing.T) {
	j := journal(t)
	tx, err := j.Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	bindFixture(t, tx, boundIntent("stop", "").Before, strings.Repeat("b", 64))
	prefixFixture(t, tx, "stop")
	defer tx.Close()
	i := fixtureIntent(tx, "stop", "")
	err = tx.Effect(i, func() (EffectState, *release.SelectionEvidence, error) { return EffectConfirmed, &i.Before, nil })
	r, e := j.Query(request("1").ID)
	if !errors.Is(err, ErrUncertain) || e != nil || r.Outcome != Uncertain || r.Effects[len(r.Effects)-1].State != EffectUnknown {
		t.Fatal(r, err, e)
	}
}

// No-peer recovery is quiescence under a held guard, never a captured peer exit;
// clear-only receipts must reject missing or contradictory stop evidence.
func TestPreparedStop_should_requireCompleteProof_forClearTerminal(t *testing.T) {
	for _, mode := range []string{"peer", "no_peer", "missing", "wrong_attempt", "incomplete", "invented_exit"} {
		t.Run(mode, func(t *testing.T) {
			i := boundIntent("stop", "")
			i.Before.Pending = i.Before.Current
			i.Before.Current = release.SelectionIdentity{}
			o := releasedStopObservation()
			if mode == "no_peer" || mode == "invented_exit" {
				i.Before = boundIntent("start", strings.Repeat("b", 64)).Before
				i.Stop = &release.StopIntent{Mode: "no_peer", AttemptID: o.AttemptID, AbsenceBasis: "explicit_legacy_recovery"}
				o.Dispatch = "not_sent"
				o.Process = "not_observed"
				o.RuntimeResource = "held_by_recovery"
				o.Result = "quiescent_no_peer"
			}
			if mode == "invented_exit" {
				o.Process = "exit_observed"
				o.Result = "released_peer"
			}
			if mode == "wrong_attempt" {
				o.AttemptID = strings.Repeat("4", 32)
			}
			if mode == "incomplete" {
				o.Observers = "unknown"
			}
			stop := Effect{EffectIntent: i, State: EffectConfirmed, Observed: &i.Before, StopObserved: &o}
			if mode == "missing" {
				stop.StopObserved = nil
			}
			clear := boundIntent("clear", "")
			clear.Before = i.Before
			req := request("1")
			req.Kind = "rollback"
			binding := &release.ResolvedIntent{Baseline: i.Before, ClearOnly: true}
			r := Record{Binding: binding, Request: req, NamespaceSHA256: req.IntentSHA256, Outcome: Succeeded, Effects: []Effect{stop, {EffectIntent: clear, State: EffectConfirmed, Observed: clear.Expected}}}
			if r.valid() != (mode == "peer" || mode == "no_peer") {
				t.Fatal("invalid clear classification", r)
			}
		})
	}
}

// Full typed stop snapshots remain within the serialization budget; repeated
// stops are not a valid lifecycle and cannot certify a journal receipt.
func TestPreparedStop_should_fit32FullObservations_withinRecordBound(t *testing.T) {
	i := boundIntent("stop", "")
	i.Before.Pending = i.Before.Current
	i.Before.Previous = i.Before.Current
	i.Before.Previous.LaunchID = ""
	o := releasedStopObservation()
	req := request("1")
	req.Kind = "rollback"
	r := Record{Request: req, NamespaceSHA256: req.IntentSHA256, Binding: resolvedFixture(i.Before, strings.Repeat("b", 64)), Outcome: Pending}
	for n := 0; n < 32; n++ {
		r.Effects = append(r.Effects, Effect{EffectIntent: i, State: EffectConfirmed, Observed: &i.Before, StopObserved: &o})
	}
	raw, err := json.Marshal(r)
	if err != nil || len(raw) > maxRecordBytes {
		t.Fatal(len(raw), err)
	}
	if r.valid() {
		t.Fatal("repeated stop sequence accepted")
	}
}

// Losing the post-run journal write keeps the durable pending marker even when
// local exit evidence was complete; no later observer may replay that attempt.
func TestPreparedStop_should_retainPending_whenObservationSaveFails(t *testing.T) {
	j := journal(t)
	tx, err := j.Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	bindFixture(t, tx, boundIntent("stop", "").Before, strings.Repeat("b", 64))
	prefixFixture(t, tx, "stop")
	err = testStop(tx, func() (release.StopObservation, error) {
		if e := tx.db.Close(); e != nil {
			t.Fatal(e)
		}
		return releasedStopObservation(), nil
	})
	if !errors.Is(err, release.ErrCleanupUncertain) {
		t.Fatal(err)
	}
	_ = tx.Close()
	r, err := j.Query(request("1").ID)
	if err != nil || r.Effects[len(r.Effects)-1].State != EffectPending || r.Effects[len(r.Effects)-1].StopObserved != nil {
		t.Fatal(r, err)
	}
	if _, err := j.Admit(request("2")); !errors.Is(err, ErrPending) {
		t.Fatal("lost result replayed", err)
	}
}
