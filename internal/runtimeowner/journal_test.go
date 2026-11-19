package runtimeowner

import (
	"errors"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func request(id string) Request {
	return Request{ID: strings.Repeat(id, 32), Kind: "install", IntentSHA256: strings.Repeat("a", 64)}
}
func journal(t *testing.T) Journal {
	t.Helper()
	return Journal{Dir: filepath.Join(t.TempDir(), "lifecycle")}
}

// Missing observation must remain read-only, including the containing directory.
func TestQueryAbsentDoesNotCreateProfile(t *testing.T) {
	j := journal(t)
	if _, err := j.Query(request("1").ID); !errors.Is(err, ErrUnknown) {
		t.Fatal(err)
	}
	if _, err := os.Stat(j.Dir); !os.IsNotExist(err) {
		t.Fatal("query created directory", err)
	}
}

// An admitted request survives the writer and prevents a new transaction after exit.
func TestAcceptedRequestSurvivesWriterExitAndBlocksReplacement(t *testing.T) {
	j := journal(t)
	tx, err := j.Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := j.Admit(request("2")); err == nil {
		t.Fatal("concurrent transaction admitted")
	}
	if err := tx.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := j.Query(request("1").ID)
	if err != nil || got.Outcome != Pending {
		t.Fatal(got, err)
	}
	if _, err := j.Admit(request("2")); !errors.Is(err, ErrPending) {
		t.Fatal("unfinished transaction replaced", err)
	}
	if _, err := j.Admit(request("1")); !errors.Is(err, ErrRecorded) {
		t.Fatal("request replayed", err)
	}
}

// A recorded intent is durable before the callback; uncertainty cannot dispatch twice.
func TestEffectIsDurableBeforeDispatchAndUnknownBlocksFurtherEffects(t *testing.T) {
	j := journal(t)
	tx, err := j.Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	bindFixture(t, tx, boundIntent("stop", "").Before, strings.Repeat("b", 64))
	prefixFixture(t, tx, "stop")
	defer tx.Close()
	calls := 0
	err = testStop(tx, func() (release.StopObservation, error) {
		calls++
		got, err := j.Query(request("1").ID)
		if err != nil || len(got.Effects) != 2 || got.Effects[len(got.Effects)-1].State != EffectPending {
			t.Fatalf("not durable: %+v %v", got, err)
		}
		return release.StopObservation{}, errors.New("lost acknowledgement")
	})
	if !errors.Is(err, ErrUncertain) {
		t.Fatal(err)
	}
	err = testStop(tx, func() (release.StopObservation, error) { calls++; return releasedStopObservation(), nil })
	if !errors.Is(err, ErrUncertain) || calls != 1 {
		t.Fatal("effect replayed", calls, err)
	}
	if err := tx.Finish(Succeeded); err == nil {
		t.Fatal("unknown effect reported success")
	}
}

// Even a callback panic leaves the durable pre-dispatch marker and blocks takeover.
func TestInterruptedEffectRemainsPendingWithoutAutomaticResume(t *testing.T) {
	j := journal(t)
	tx, err := j.Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	bindFixture(t, tx, release.ObserveSelection(release.State{}), strings.Repeat("b", 64))
	prefixFixture(t, tx, "start")
	func() {
		defer func() { _ = recover() }()
		_ = testEffect(tx, "start", strings.Repeat("b", 64), func() (EffectState, error) { panic("process interruption") })
	}()
	if err := tx.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := j.Query(request("1").ID)
	if err != nil || got.Effects[len(got.Effects)-1].State != EffectPending {
		t.Fatal(got, err)
	}
	if _, err := j.Admit(request("2")); !errors.Is(err, ErrPending) {
		t.Fatal(err)
	}
}

// Closed outcomes retain their IDs; a new request is allowed but replay/conflict is not.
func TestTerminalReceiptIsRetainedAndIntentCannotChange(t *testing.T) {
	j := journal(t)
	tx, err := j.Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Finish(Rejected); err != nil {
		t.Fatal(err)
	}
	tx.Close()
	if _, err := j.Admit(request("1")); !errors.Is(err, ErrRecorded) {
		t.Fatal(err)
	}
	changed := request("1")
	changed.Kind = "rollback"
	if _, err := j.Admit(changed); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	next, err := j.Admit(request("2"))
	if err != nil {
		t.Fatal(err)
	}
	next.Close()
}

// Commit and ready observations must name the same artifact before a success receipt.
func TestSuccessRequiresMatchingCommitAndReady(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		t.Run(map[bool]string{false: "matching", true: "mismatched"}[mismatch], func(t *testing.T) {
			j := journal(t)
			tx, err := j.Admit(request("1"))
			if err != nil {
				t.Fatal(err)
			}
			bindFixture(t, tx, release.ObserveSelection(release.State{}), strings.Repeat("b", 64))
			prefixFixture(t, tx, "start")
			defer tx.Close()
			if err := tx.Finish(Succeeded); err == nil {
				t.Fatal("success without observation")
			}
			sha := strings.Repeat("b", 64)
			for _, name := range []string{"start", "commit", "ready"} {
				target := sha
				if name == "ready" && mismatch {
					target = strings.Repeat("c", 64)
				}
				called := false
				err := testEffect(tx, name, target, func() (EffectState, error) { called = true; return EffectConfirmed, nil })
				if name == "ready" && mismatch {
					if err == nil || called {
						t.Fatal("wrong candidate ready dispatched", err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
			}
			err = tx.Finish(Succeeded)
			if (err != nil) != mismatch {
				t.Fatal("unexpected outcome", err)
			}
		})
	}
}
