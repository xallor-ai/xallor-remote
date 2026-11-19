package runtimeowner

import (
	"errors"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"strings"
	"testing"
)

// SQLite closure is required before confirming either a stopped or cleanly
// refused attempt; malformed receipts retain debt without reentrant recorder calls.
func TestStopPreparation_shouldSettleExactGroup_beforeStopDisposition(t *testing.T) {
	for _, mode := range []string{"exact", "refused", "missing", "duplicate", "mismatch", "unknown", "durable_error", "selection_error", "identity_mismatch", "unrecorded"} {
		t.Run(mode, func(t *testing.T) {
			j := journal(t)
			tx, err := j.Admit(request("1"))
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Close()
			bindFixture(t, tx, boundIntent("stop", "").Before, strings.Repeat("b", 64))
			prefixFixture(t, tx, "stop")
			i := fixtureIntent(tx, "stop", "")
			r := release.ResourceObligation{ID: strings.Repeat("7", 32), AttemptID: i.Stop.AttemptID, Generation: 1, Phase: ResourcePhaseEffects, Kind: ResourceKindObserver, ObserverScope: ObserverScopeOwnerProcess, Retention: ResourceReleaseRequired}
			if mode != "unrecorded" {
				if err := tx.RecordResourceIntent(ResourceObligation(r)); err != nil {
					t.Fatal(err)
				}
			}
			original := r
			if mode == "identity_mismatch" {
				r.Generation++
			}
			called := false
			err = tx.ApplyStop(i, r, func() (release.StopObservation, []release.StopClosure, error) {
				called = true
				c := release.StopClosure{Resource: r, Closed: true}
				closures := []release.StopClosure{c}
				switch mode {
				case "missing":
					closures = nil
				case "duplicate":
					closures = append(closures, c)
				case "mismatch":
					closures[0].Resource.Generation++
				case "unknown":
					closures[0].Closed = false
				case "durable_error":
					if e := tx.db.Close(); e != nil {
						t.Fatal(e)
					}
				}
				if mode == "refused" {
					return undispatchedStopObservation(), closures, errors.New("refused")
				}
				return releasedStopObservation(), closures, nil
			}, func() (release.SelectionEvidence, error) {
				if mode == "selection_error" {
					return release.SelectionEvidence{}, errors.New("selection capture failed")
				}
				return i.Before, nil
			})
			clean := mode == "exact" || mode == "refused"
			if clean {
				if errors.Is(err, release.ErrCleanupUncertain) || (mode == "exact" && err != nil) {
					t.Fatal(err)
				}
			} else if !errors.Is(err, release.ErrCleanupUncertain) {
				t.Fatal("uncertainty lost", err)
			}
			if (mode == "identity_mismatch" || mode == "unrecorded") && called {
				t.Fatal("dispatched without exact allocated preparation")
			}
			rows, e := j.Resources(request("1").ID)
			if e != nil {
				t.Fatal(e)
			}
			if mode == "unrecorded" {
				return
			}
			if len(rows) != 1 || rows[0].ResourceObligation != ResourceObligation(original) {
				t.Fatal(rows)
			}
			closed := clean || mode == "selection_error"
			if (rows[0].Status == ResourceClosed) != closed {
				t.Fatal("incorrect closure", rows, err)
			}
			record, e := j.Query(request("1").ID)
			if e != nil {
				t.Fatal(e)
			}
			effect := record.Effects[len(record.Effects)-1]
			if clean && effect.State != EffectConfirmed && effect.State != EffectNotApplied {
				t.Fatal(effect)
			}
			if !clean && effect.State != EffectUnknown && effect.State != EffectPending {
				t.Fatal(effect)
			}
		})
	}
}
