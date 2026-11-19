package cli

import (
	"context"
	"errors"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"testing"
)

// Cancellation before Dial still returns an owned, exactly closable preparation;
// it never invents stop disposition or an admitted public transaction.
func TestStopPreparationNative_shouldReturnOwnedAttempt_whenCancelled(t *testing.T) {
	t.Setenv("XALLOR_REMOTE_DATA_DIR", t.TempDir())
	r := release.ResourceObligation{ID: "dddddddddddddddddddddddddddddddd", AttemptID: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", Generation: 1, Phase: "effects", Kind: "observer", ObserverScope: "owner_process", Retention: "release_required"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a, err := (runtimeControl{}).PrepareStop(ctx, release.StopRequest{Observer: r, AbsencePolicy: "fresh"})
	if !errors.Is(err, context.Canceled) || a == nil || a.Intent().AttemptID != r.AttemptID {
		t.Fatal(a, err)
	}
	for n := 0; n < 2; n++ {
		c, e := a.Close()
		if e = release.StopCleanup(r, c, e); e != nil {
			t.Fatal(e)
		}
	}
}

type countedStopWatch struct {
	preparedWatch
	closes int
}

func (w *countedStopWatch) Close() error { w.closes++; return w.closeErr }

// A group closes every native member once, retaining the first physical failure
// and returning identical original receipts on repeated Close calls.
func TestStopPreparationNative_shouldRetainCloseFailure_withoutRetryingMembers(t *testing.T) {
	cause := errors.New("physical observer close failed")
	first := &countedStopWatch{preparedWatch: preparedWatch{closeErr: cause}}
	second := &countedStopWatch{}
	r := release.ResourceObligation{ID: "dddddddddddddddddddddddddddddddd", AttemptID: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"}
	a := &runtimeStopAttempt{observer: r, watch: first, image: second}
	for n := 0; n < 2; n++ {
		c, e := a.Close()
		if !errors.Is(e, cause) || len(c) != 1 || c[0].Resource != r || c[0].Closed {
			t.Fatal(c, e)
		}
	}
	if first.closes != 1 || second.closes != 1 {
		t.Fatal("native members closed more than once", first, second)
	}
}

// This synthetic boundary models an image-capture error containing a hidden
// member's Close failure. It does not claim to force an OS Close failure.
// Unlike the explicit member test, all retained outer members close successfully.
func TestStopPreparationNative_shouldKeepHiddenCaptureDebt_whenOuterMembersClose(t *testing.T) {
	cause := errors.New("hidden image capture cleanup failed")
	outer := &countedStopWatch{}
	group := release.ResourceObligation{ID: "dddddddddddddddddddddddddddddddd", AttemptID: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"}
	a := &runtimeStopAttempt{observer: group, watch: outer, closeErr: cause}
	for n := 0; n < 2; n++ {
		closures, err := a.Close()
		if !errors.Is(err, cause) || len(closures) != 1 || closures[0].Resource != group || closures[0].Closed {
			t.Fatal(closures, err)
		}
	}
	if outer.closes != 1 {
		t.Fatal("outer member was not closed exactly once", outer.closes)
	}
}
