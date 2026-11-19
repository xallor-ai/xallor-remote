package runtimeowner

import (
	"context"
	"errors"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"strings"
	"testing"
	"time"
)

type readyFixtureAttempt struct {
	observation      release.ReadyObservation
	closures         []release.ReadyClosure
	runErr, closeErr error
	run              func()
}

func (a *readyFixtureAttempt) Run(context.Context) (release.ReadyObservation, error) {
	if a.run != nil {
		a.run()
	}
	return a.observation, a.runErr
}
func (a *readyFixtureAttempt) Close() ([]release.ReadyClosure, error) { return a.closures, a.closeErr }

// Real SQLite settlement within ready must not recursively lock the transaction;
// a retained candidate is admitted only after exact observer closure is durable.
func TestPreparedReady_shouldCloseObserversAndTransfer_withoutRecursiveLock(t *testing.T) {
	j, tx, candidate, evidence := selectionFixture(t, false)
	i := fixtureIntent(tx, "ready", candidate.ImageSHA256)
	observer := candidate
	observer.ID = strings.Repeat("7", 32)
	observer.Kind = ResourceKindObserver
	observer.Phase = ResourcePhaseEffects
	if err := tx.RecordResourceIntent(observer); err != nil {
		t.Fatal(err)
	}
	request := release.ReadyRequest{Manifest: tx.record.Binding.Manifest, Selection: i.Before, Observer: release.ResourceObligation(observer)}
	a := &readyFixtureAttempt{observation: release.ReadyObservation{Selection: i.Before, PeerPID: 123, ImageSHA256: candidate.ImageSHA256, ManifestSHA256: tx.record.Binding.ManifestSHA256, SourceSnapshotSHA256: evidence.SourceSnapshotSHA256, Authenticated: true}, closures: []release.ReadyClosure{{Resource: request.Observer, Closed: true}}}
	done := make(chan error, 1)
	go func() {
		o, err := tx.ApplyReady(i, request, a, func() (release.SelectionEvidence, error) { return i.Before, nil })
		if err == nil {
			err = (releaseResources{tx}).RetainSelected(release.ResourceObligation(candidate), o)
		}
		if err == nil {
			err = tx.Finish(Succeeded)
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ready recursively locked transaction")
	}
	rows, err := j.Resources(tx.record.Request.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Kind == ResourceKindObserver && row.Status != ResourceClosed || row.Kind == ResourceKindActivation && row.Status != ResourceRetained {
			t.Fatal(rows)
		}
	}
}

// Observation, physical closure, duplicate receipts and durable-close failure
// cannot produce confirmed readiness or a transferable candidate.
func TestPreparedReady_shouldKeepDebt_whenAnyObservationOrClosureFails(t *testing.T) {
	for _, fault := range []string{"observation", "close", "missing", "duplicate", "durable", "manifest", "selection", "observer-kind"} {
		t.Run(fault, func(t *testing.T) {
			_, tx, candidate, evidence := selectionFixture(t, false)
			i := fixtureIntent(tx, "ready", candidate.ImageSHA256)
			observer := candidate
			observer.ID = strings.Repeat("7", 32)
			observer.Kind = ResourceKindObserver
			observer.Phase = ResourcePhaseEffects
			if fault == "observer-kind" {
				observer.Kind = ResourceKindActivation
			}
			if err := tx.RecordResourceIntent(observer); err != nil {
				t.Fatal(err)
			}
			request := release.ReadyRequest{Manifest: tx.record.Binding.Manifest, Selection: i.Before, Observer: release.ResourceObligation(observer)}
			a := &readyFixtureAttempt{observation: release.ReadyObservation{Selection: i.Before, PeerPID: 123, ImageSHA256: candidate.ImageSHA256, ManifestSHA256: tx.record.Binding.ManifestSHA256, SourceSnapshotSHA256: evidence.SourceSnapshotSHA256, Authenticated: true}, closures: []release.ReadyClosure{{Resource: request.Observer, Closed: true}}}
			switch fault {
			case "observation":
				a.runErr = errors.New("lost reply")
			case "close":
				a.closeErr = errors.New("handle close failed")
			case "missing":
				a.closures = nil
			case "duplicate":
				a.closures = append(a.closures, a.closures[0])
			case "durable":
				a.run = func() {
					_, err := tx.db.Exec("CREATE TRIGGER refuse_ready_close BEFORE UPDATE ON resource_obligations BEGIN SELECT RAISE(ABORT,'close refused'); END")
					if err != nil {
						t.Error(err)
					}
				}
			case "manifest":
				a.observation.ManifestSHA256 = strings.Repeat("f", 64)
			case "selection":
				a.observation.Selection.StateSHA256 = strings.Repeat("f", 64)
			}
			_, err := tx.ApplyReady(i, request, a, func() (release.SelectionEvidence, error) { return i.Before, nil })
			if !errors.Is(err, release.ErrCleanupUncertain) {
				t.Fatal(err)
			}
			if _, err := tx.SelectionReceipt(candidate, 1, evidence); err == nil {
				t.Fatal("failure allowed transfer")
			}
		})
	}
}
