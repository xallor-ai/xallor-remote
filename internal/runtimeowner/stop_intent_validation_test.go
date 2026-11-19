package runtimeowner

import (
	"errors"
	"strings"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/release"
)

// Public observational scopes remain valid at the public API boundary, but
// cannot enter any durable lifecycle state or certify a clear-only receipt.
func TestPreparedStop_should_rejectWeakDurableIntent_inEveryRecordState(t *testing.T) {
	for _, mode := range []string{"public_absence", "process_image", "unbound_peer", "fresh_current", "fresh_previous", "recovery_without_pending"} {
		t.Run(mode, func(t *testing.T) {
			i := boundIntent("stop", "")
			o := releasedStopObservation()
			switch mode {
			case "process_image":
				i.Stop.IdentityScope = "process_image"
				i.Stop.Selection = release.SelectionIdentity{}
			case "unbound_peer":
				i.Stop.Selection.LaunchID = strings.Repeat("4", 32)
			default:
				basis := "fresh"
				if mode == "public_absence" {
					basis = "public_observation"
				}
				if mode == "recovery_without_pending" {
					basis = "explicit_legacy_recovery"
				}
				i.Stop = &release.StopIntent{Mode: "no_peer", AttemptID: o.AttemptID, AbsenceBasis: basis}
				o.Dispatch = "not_sent"
				o.Process = "not_observed"
				o.Result = "quiescent_no_peer"
				o.RuntimeResource = "held_by_recovery"
				if mode == "public_absence" {
					i.Before = release.ObserveSelection(release.State{})
				}
				if mode == "fresh_previous" {
					i.Before.Previous = i.Before.Current
					i.Before.Previous.LaunchID = ""
					i.Before.Current = release.SelectionIdentity{}
				}
			}
			if !i.Stop.Valid() {
				t.Fatal("fixture no longer describes a structurally valid public stop", i.Stop)
			}
			if i.Valid() {
				t.Error("weak intent admitted into durable effect", i)
			}
			for _, state := range []EffectState{EffectPending, EffectUnknown, EffectConfirmed} {
				e := Effect{EffectIntent: i, State: state}
				outcome := Pending
				if state != EffectPending {
					e.Observed = &i.Before
					e.StopObserved = &o
				}
				if state == EffectUnknown {
					outcome = Uncertain
				}
				req := request("1")
				req.Kind = "rollback"
				baseline := boundIntent("prepare", strings.Repeat("b", 64)).Before
				baseline.Previous = boundIntent("ready", strings.Repeat("b", 64)).Target
				baseline.Previous.LaunchID = ""
				r := Record{Binding: resolvedFixture(baseline, strings.Repeat("b", 64)), Request: req, NamespaceSHA256: req.IntentSHA256, Outcome: outcome, Effects: []Effect{e}}
				if state == EffectConfirmed {
					clear := boundIntent("clear", "")
					clear.Before = i.Before
					clear.Purpose = "restore"
					r.Outcome = Succeeded
					r.Effects = append(r.Effects, Effect{EffectIntent: clear, State: EffectConfirmed, Observed: clear.Expected})
				}
				if e.valid() {
					t.Errorf("weak effect accepted in %s state", state)
				}
				if !r.Binding.Valid() || !sameTarget(r.Binding.Requested, r.Binding.Baseline.Previous) {
					t.Fatal("negative fixture has unrelated invalid resolved source")
				}
				if r.valid() || r.ValidateFor(req) == nil {
					t.Errorf("weak intent accepted in %s record", state)
				}
			}
			j := journal(t)
			tx, err := j.Admit(request("1"))
			if err != nil {
				t.Fatal(err)
			}
			bindFixture(t, tx, boundIntent("stop", "").Before, strings.Repeat("b", 64))
			defer tx.Close()
			called := false
			err = applyFixtureStop(tx, i, func() (release.StopObservation, error) { called = true; return o, nil }, func() (release.SelectionEvidence, error) { return i.Before, nil })
			if !errors.Is(err, ErrInvalid) || !errors.Is(err, release.ErrCleanupUncertain) || called {
				t.Fatal("weak capture dispatched", called, err)
			}
			r, err := j.Query(request("1").ID)
			if err != nil || len(r.Effects) != 0 || r.Outcome != Pending {
				t.Fatal("weak intent was journaled", r, err)
			}
		})
	}
}
