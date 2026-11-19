package runtimeowner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"runtime"
	"strings"
	"testing"
)

// Synthetic bounded observations for transaction-only tests. Native lifecycle
// tests exercise real Store capture; these helpers confer no production authority.
func boundIntent(name, sha string) release.EffectIntent {
	empty := release.ObserveSelection(release.State{})
	target := release.SelectionIdentity{SHA256: sha, ActivationID: strings.Repeat("1", 32), LaunchID: strings.Repeat("2", 32)}
	selected := release.SelectionEvidence{StateSHA256: strings.Repeat("c", 64), Current: target}
	pending := release.SelectionEvidence{StateSHA256: strings.Repeat("d", 64), Pending: target}
	intent := release.EffectIntent{Name: name, Purpose: "requested", TargetSHA256: sha, Target: target, Before: pending}
	switch name {
	case "stop":
		intent.Target = release.SelectionIdentity{}
		intent.TargetSHA256 = ""
		intent.Before = empty
		intent.Stop = &release.StopIntent{Mode: "peer", AttemptID: strings.Repeat("3", 32), PeerPID: 123, ImageSHA256: target.SHA256, Selection: target, IdentityScope: "process_image_and_launch"}
		if sha == "" {
			intent.Stop.ImageSHA256 = strings.Repeat("b", 64)
			intent.Stop.Selection.SHA256 = intent.Stop.ImageSHA256
		}
		intent.Before = release.SelectionEvidence{StateSHA256: strings.Repeat("c", 64), Current: intent.Stop.Selection}
	case "start":
	case "prepare":
		intent.Before = empty
		intent.Expected = &pending
	case "commit":
		intent.Expected = &selected
	case "ready":
		intent.Before = selected
	case "clear":
		intent.Target = release.SelectionIdentity{}
		intent.TargetSHA256 = ""
		intent.Before = empty
		intent.Expected = &empty
	}
	return intent
}

// Explicit synthetic authenticated-source binding for journal mechanics tests.
// The digest is calculated from actual fixture manifest bytes, never a source path.
func resolvedFixture(baseline release.SelectionEvidence, sha string) *release.ResolvedIntent {
	name := "xallor-remote"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	m := release.Manifest{Schema: 1, Build: buildinfo.Info{Version: "1.2.3", Revision: strings.Repeat("a", 40), Protocol: buildinfo.Protocol, ProfileFormat: buildinfo.ProfileFormat(), OS: runtime.GOOS, Arch: runtime.GOARCH}, Artifact: release.Artifact{Filename: name, SHA256: sha, Size: 1}}
	raw, _ := json.Marshal(m)
	sum := sha256.Sum256(raw)
	return &release.ResolvedIntent{Baseline: baseline, Requested: release.SelectionIdentity{SHA256: sha, ActivationID: strings.Repeat("1", 32)}, Manifest: m, ManifestSHA256: hex.EncodeToString(sum[:])}
}

func bindFixture(t *testing.T, tx *Transaction, baseline release.SelectionEvidence, sha string) {
	t.Helper()
	if err := tx.Bind(*resolvedFixture(baseline, sha)); err != nil {
		t.Fatal("bind fixture", err)
	}
}

func testEffect(tx *Transaction, name, sha string, apply func() (EffectState, error)) error {
	intent := fixtureIntent(tx, name, sha)
	return tx.Effect(intent, func() (EffectState, *release.SelectionEvidence, error) {
		state, err := apply()
		observed := intent.Before
		if intent.Expected != nil {
			observed = *intent.Expected
		}
		return state, &observed, err
	})
}

func testApply(tx *Transaction, name, sha string, apply func() error) error {
	intent := fixtureIntent(tx, name, sha)
	return tx.Apply(intent, apply, func() (release.SelectionEvidence, error) {
		if intent.Expected != nil {
			return *intent.Expected, nil
		}
		return intent.Before, nil
	})
}

// fixtureIntent captures the preceding observation but never invents effects.
func fixtureIntent(tx *Transaction, name, sha string) release.EffectIntent {
	i := boundIntent(name, sha)
	i.Before = tx.record.Binding.Baseline
	if n := len(tx.record.Effects); n > 0 && tx.record.Effects[n-1].Observed != nil {
		i.Before = *tx.record.Effects[n-1].Observed
	}
	if name == "prepare" {
		expected := i.Before
		expected.Pending = i.Target
		expected.StateSHA256 = strings.Repeat("d", 64)
		i.Expected = &expected
	}
	if name == "commit" {
		expected := i.Before
		expected.Current = i.Target
		baseline := tx.record.Binding.Baseline
		if sha == tx.record.Binding.Requested.SHA256 && baseline.Current.SHA256 != "" && baseline.Current.SHA256 != sha {
			expected.Previous = baseline.Current
			expected.Previous.LaunchID = ""
		}
		expected.Pending = release.SelectionIdentity{}
		expected.StateSHA256 = strings.Repeat("c", 64)
		i.Expected = &expected
	}
	if name == "stop" {
		baseline := tx.record.Binding.Baseline
		if baseline == release.ObserveSelection(release.State{}) {
			i.Stop = &release.StopIntent{Mode: "no_peer", AttemptID: strings.Repeat("3", 32), AbsenceBasis: "fresh"}
			return i
		}
		target := i.Before.Current
		if target.SHA256 == "" {
			target = i.Before.Pending
		}
		i.Stop.Selection = target
		i.Stop.ImageSHA256 = target.SHA256
	}
	return i
}

// prefixFixture dispatches explicit confirmed synthetic observations needed to
// reach the tested boundary; Bind remains the caller's deliberate responsibility.
func prefixFixture(t *testing.T, tx *Transaction, next string) {
	t.Helper()
	for _, name := range []string{"prepare", "stop", "start", "commit"} {
		if name == next {
			return
		}
		var err error
		if name == "stop" {
			err = testStop(tx, func() (release.StopObservation, error) {
				o := releasedStopObservation()
				if tx.record.Binding.Baseline == release.ObserveSelection(release.State{}) {
					o.Dispatch = "not_sent"
					o.Process = "not_observed"
					o.Result = "quiescent_no_peer"
				}
				return o, nil
			})
		} else {
			err = testApply(tx, name, tx.record.Binding.Requested.SHA256, func() error { return nil })
		}
		if err != nil {
			t.Fatal("prefix", name, err)
		}
	}
}

// lifecycleFixture constructs a complete observed sequence through real journal writes.
func lifecycleFixture(t *testing.T, baselineSHA string, failed bool) Record {
	t.Helper()
	tx, err := journal(t).Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Close()
	baseline := release.ObserveSelection(release.State{})
	if baselineSHA != "" {
		baseline = boundIntent("ready", baselineSHA).Before
	}
	bindFixture(t, tx, baseline, strings.Repeat("b", 64))
	prefixFixture(t, tx, "start")
	if failed {
		if err := testApply(tx, "start", strings.Repeat("b", 64), func() error { return fmt.Errorf("candidate failed") }); err == nil {
			t.Fatal("missing failure")
		}
		for _, name := range []string{"start", "commit", "ready"} {
			i := fixtureIntent(tx, name, baselineSHA)
			i.Purpose = "restore"
			if err := tx.Apply(i, func() error { return nil }, func() (release.SelectionEvidence, error) {
				if i.Expected != nil {
					return *i.Expected, nil
				}
				return i.Before, nil
			}); err != nil {
				t.Fatal(err)
			}
		}
		if err := tx.Finish(Failed); err != nil {
			t.Fatal(err)
		}
	} else {
		for _, name := range []string{"start", "commit", "ready"} {
			if err := testApply(tx, name, strings.Repeat("b", 64), func() error { return nil }); err != nil {
				t.Fatal(err)
			}
		}
		if err := tx.Finish(Succeeded); err != nil {
			t.Fatal(err)
		}
	}
	return tx.record
}
