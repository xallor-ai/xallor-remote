package runtimeowner

import (
	"errors"
	"strings"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/release"
)

// Recording failure must be visible to the existing release algorithm as
// uncertainty, so it cannot interpret a lost journal as a clean startup failure.
func TestReleaseRecorderRefusesDispatchAfterJournalFailure(t *testing.T) {
	j := journal(t)
	tx, err := j.Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	bindFixture(t, tx, release.ObserveSelection(release.State{}), strings.Repeat("b", 64))
	prefixFixture(t, tx, "start")
	defer tx.Close()
	tx.db.Close()
	called := false
	err = testApply(tx, "start", strings.Repeat("b", 64), func() error { called = true; return nil })
	if !errors.Is(err, release.ErrCleanupUncertain) || called {
		t.Fatal(called, err)
	}
}

// A cleaned-up failed start is distinct from never having started. One verified
// restoration may follow, but the original installation still ends as failed.
func TestReleaseRecorderPreservesQuiescentFailureAndRestoration(t *testing.T) {
	j := journal(t)
	tx, err := j.Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	bindFixture(t, tx, boundIntent("ready", strings.Repeat("a", 64)).Before, strings.Repeat("b", 64))
	prefixFixture(t, tx, "start")
	defer tx.Close()
	sha := strings.Repeat("b", 64)
	cause := errors.New("candidate exited; owned process and image released")
	if err := testApply(tx, "start", sha, func() error { return cause }); !errors.Is(err, cause) {
		t.Fatal(err)
	}
	got, err := j.Query(request("1").ID)
	if err != nil || got.Effects[len(got.Effects)-1].State != EffectQuiescentFailure {
		t.Fatal(got, err)
	}
	if err := tx.Finish(Failed); err == nil {
		t.Fatal("failed start alone implies restored baseline")
	}
	for _, name := range []string{"start", "commit", "ready"} {
		intent := fixtureIntent(tx, name, strings.Repeat("a", 64))
		intent.Purpose = "restore"
		if err := tx.Apply(intent, func() error { return nil }, func() (release.SelectionEvidence, error) {
			if intent.Expected != nil {
				return *intent.Expected, nil
			}
			return intent.Before, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Finish(Failed); err != nil {
		t.Fatal(err)
	}
}

// A state-save or ready failure is always unresolved; unlike a deterministic
// Start failure it never permits an automatic restoration callback.
func TestReleaseRecorderDoesNotGuessOutcomeOfStateOrReadyFailure(t *testing.T) {
	for _, name := range []string{"prepare", "commit", "ready"} {
		t.Run(name, func(t *testing.T) {
			j := journal(t)
			tx, err := j.Admit(request("1"))
			if err != nil {
				t.Fatal(err)
			}
			bindFixture(t, tx, release.ObserveSelection(release.State{}), strings.Repeat("b", 64))
			prefixFixture(t, tx, name)
			defer tx.Close()
			if err := testApply(tx, name, strings.Repeat("b", 64), func() error { return errors.New("lost response") }); !errors.Is(err, release.ErrCleanupUncertain) {
				t.Fatal(err)
			}
			called := false
			if err := testApply(tx, "start", strings.Repeat("a", 64), func() error { called = true; return nil }); !errors.Is(err, release.ErrCleanupUncertain) || called {
				t.Fatal("restored after uncertainty", err)
			}
		})
	}
}
