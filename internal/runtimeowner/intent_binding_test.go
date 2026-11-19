package runtimeowner

import (
	"github.com/xallor-ai/xallor-remote/internal/release"
	"strings"
	"testing"
)

// A last commit/ready pair without the original target cannot prove requested success.
func TestShouldRejectRestoredSuccessWithoutOriginalBinding(t *testing.T) {
	a, b := boundIntent("commit", strings.Repeat("b", 64)), boundIntent("ready", strings.Repeat("b", 64))
	r := Record{Request: request("1"), NamespaceSHA256: strings.Repeat("a", 64), Outcome: Succeeded, Effects: []Effect{
		{EffectIntent: a, State: EffectConfirmed, Observed: a.Expected},
		{EffectIntent: b, State: EffectConfirmed, Observed: &b.Before},
	}}
	if r.valid() {
		t.Fatal("restored commit and ready accepted without requested target")
	}
}

// An admitted pathname request cannot dispatch before its authenticated target is durable.
func TestShouldRejectEffectBeforeResolvedBinding(t *testing.T) {
	tx, err := journal(t).Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Close()
	ran := false
	i := boundIntent("prepare", strings.Repeat("b", 64))
	err = tx.Effect(i, func() (EffectState, *release.SelectionEvidence, error) {
		ran = true
		return EffectConfirmed, i.Expected, nil
	})
	if err == nil || ran {
		t.Fatal("unbound request dispatched", err, ran)
	}
}

// Bound restoration must never become successful requested installation, even
// when both sides use otherwise internally consistent commit/ready evidence.
func TestShouldClassifyRestorationAgainstOriginalCandidate(t *testing.T) {
	r := lifecycleFixture(t, strings.Repeat("a", 64), true)
	if !r.valid() {
		t.Fatal("valid restoration rejected")
	}
	r.Outcome = Succeeded
	if r.valid() {
		t.Fatal("restoration mislabeled success")
	}
	n := len(r.Effects)
	r.Effects[n-2].Purpose = "requested"
	r.Effects[n-1].Purpose = "requested"
	if r.valid() {
		t.Fatal("wrong candidate labeled requested succeeded")
	}
	r.Outcome = Failed
	r.Effects[n-2].Purpose = "restore"
	r.Effects[n-1].Purpose = "restore"
	r.Effects[n-2].Target.LaunchID = strings.Repeat("9", 32)
	if r.valid() {
		t.Fatal("restoration nonce substitution accepted")
	}

}

// Binding takes a value snapshot; caller mutation and later raw digest changes
// cannot change an original operation, including after closing and reopening it.
func TestShouldPersistImmutableBindingAndRefuseRebinding(t *testing.T) {
	j := journal(t)
	tx, err := j.Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Close()
	binding := *resolvedFixture(release.ObserveSelection(release.State{}), strings.Repeat("b", 64))
	original := binding
	if err = tx.Bind(binding); err != nil {
		t.Fatal(err)
	}
	binding.ManifestSHA256 = strings.Repeat("c", 64)
	binding.Manifest.Build.Version = "9.9.9"
	if err = tx.Bind(binding); err == nil {
		t.Fatal("rebind accepted")
	}
	got, err := j.Query(request("1").ID)
	if err != nil || got.Binding == nil || *got.Binding != original {
		t.Fatal(got, err)
	}
	got.Binding.ManifestSHA256 = strings.Repeat("d", 64)
	got, err = j.Query(request("1").ID)
	if err != nil || *got.Binding != original {
		t.Fatal("query alias mutated binding", got, err)
	}
}

// A database failure during binding must precede any effect callback.
func TestShouldRefuseDispatchAfterBindingPersistenceFailure(t *testing.T) {
	tx, err := journal(t).Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Close()
	tx.db.Close()
	if err = tx.Bind(*resolvedFixture(release.ObserveSelection(release.State{}), strings.Repeat("b", 64))); err == nil {
		t.Fatal("lost binding accepted")
	}
	ran := false
	i := boundIntent("prepare", strings.Repeat("b", 64))
	_ = tx.Effect(i, func() (EffectState, *release.SelectionEvidence, error) {
		ran = true
		return EffectConfirmed, i.Expected, nil
	})
	if ran {
		t.Fatal("dispatched after lost binding")
	}
}

// Rollback is constrained to the original selected recovery target; installs
// cannot bind over a pending operation, and clear-only never hides Previous.
func TestShouldRejectInvalidResolvedOperationTargets(t *testing.T) {
	sha := strings.Repeat("b", 64)
	b := *resolvedFixture(release.ObserveSelection(release.State{}), sha)
	r := Record{Request: request("1"), NamespaceSHA256: strings.Repeat("a", 64), Binding: &b, Outcome: Pending}
	for _, mutate := range []func(){
		func() { b.Manifest.Build.Dirty = true },
		func() { b.Manifest.Build.Revision = "short" },
		func() { b.Baseline = boundIntent("start", sha).Before },
		func() { r.Request.Kind = "rollback" },
	} {
		original := b
		req := r.Request
		mutate()
		if r.valid() {
			t.Fatal("invalid binding accepted", r)
		}
		b = original
		r.Request = req
	}
	b = release.ResolvedIntent{ClearOnly: true, Baseline: boundIntent("start", sha).Before}
	r.Request.Kind = "rollback"
	if !r.valid() {
		t.Fatal("valid clear-only recovery rejected")
	}
	b.Baseline.Previous = b.Baseline.Pending
	b.Baseline.Previous.LaunchID = ""
	if r.valid() {
		t.Fatal("orphan previous hidden by clear-only")
	}
}

// A fabricated fresh pre-state must not erase an original managed baseline.
func TestShouldRejectEffectsDetachedFromOriginalBaseline(t *testing.T) {
	sha := strings.Repeat("b", 64)
	baseline := boundIntent("ready", strings.Repeat("a", 64)).Before
	b := resolvedFixture(baseline, sha)
	i := boundIntent("prepare", sha)
	r := Record{Request: request("1"), NamespaceSHA256: strings.Repeat("a", 64), Binding: b, Outcome: Pending, Effects: []Effect{{EffectIntent: i, State: EffectConfirmed, Observed: i.Expected}}}
	if r.valid() {
		t.Fatal("effect erased bound original baseline")
	}
}

// Proven stop refusal before a fresh launch restores the original empty state
// without pretending either a requested install or process termination succeeded.
func TestShouldAcceptFreshCancellationOnlyAsFailed(t *testing.T) {
	sha := strings.Repeat("b", 64)
	empty := release.ObserveSelection(release.State{})
	binding := resolvedFixture(empty, sha)
	prep := boundIntent("prepare", sha)
	stop := boundIntent("stop", sha)
	stop.Before = *prep.Expected
	stop.Stop.Selection = stop.Before.Pending
	stop.Stop = &release.StopIntent{Mode: "no_peer", AttemptID: strings.Repeat("3", 32), AbsenceBasis: "fresh"}
	clear := boundIntent("clear", "")
	clear.Purpose = "cancel"
	clear.Before = *prep.Expected
	r := Record{Request: request("1"), NamespaceSHA256: strings.Repeat("a", 64), Binding: binding, Outcome: Failed, Effects: []Effect{{EffectIntent: prep, State: EffectConfirmed, Observed: prep.Expected}, {EffectIntent: stop, State: EffectNotApplied, Observed: &stop.Before}, {EffectIntent: clear, State: EffectConfirmed, Observed: clear.Expected}}}
	// Stop fixture fills the independently observed refusal fields below.
	o := undispatchedStopObservation()
	o.Process = "not_observed"
	r.Effects[1].StopObserved = &o
	if !r.valid() {
		t.Fatal("fresh exact cancellation not settled", r)
	}
	r.Outcome = Succeeded
	if r.valid() {
		t.Fatal("cancelled fresh install reported success")
	}
}
