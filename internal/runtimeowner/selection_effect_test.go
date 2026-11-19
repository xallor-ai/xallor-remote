package runtimeowner

import (
	"encoding/json"
	"errors"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"strings"
	"testing"
)

// Unbound historical effect fields must not establish a completed operation.
func TestShouldRejectUnboundCompletion(t *testing.T) {
	r := Record{Request: request("1"), NamespaceSHA256: request("1").IntentSHA256, Outcome: Succeeded}
	raw := `[{"name":"commit","target_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","state":"confirmed"},{"name":"ready","target_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","state":"confirmed"}]`
	if err := json.Unmarshal([]byte(raw), &r.Effects); err != nil {
		t.Fatal(err)
	}
	if r.valid() {
		t.Fatal("unbound completion accepted")
	}
}

// The historical capacity budget still fits 32 full selection snapshots. Such
// repeated transitions are deliberately invalid under the stricter lifecycle graph.
func TestShouldFitMaximumBoundedEvidence(t *testing.T) {
	intent := boundIntent("prepare", strings.Repeat("b", 64))
	intent.Before.Current = intent.Target
	intent.Before.Previous = intent.Target
	intent.Before.Previous.LaunchID = ""
	intent.Before.Pending = intent.Target
	intent.Before.StateSHA256 = strings.Repeat("c", 64)
	*intent.Expected = intent.Before
	r := Record{Request: request("1"), NamespaceSHA256: strings.Repeat("a", 64), Binding: resolvedFixture(boundIntent("prepare", strings.Repeat("b", 64)).Before, strings.Repeat("b", 64)), Outcome: Pending}
	for i := 0; i < 32; i++ {
		r.Effects = append(r.Effects, Effect{EffectIntent: intent, State: EffectConfirmed, Observed: intent.Expected})
	}
	raw, err := json.Marshal(r)
	if err != nil || len(raw) <= 16384 || len(raw) > maxRecordBytes {
		t.Fatal("unexpected record size", len(raw), err)
	}
	if r.valid() {
		t.Fatal("repeated prepare transitions accepted")
	}
}

// The same artifact under another launch or activation cannot acknowledge commit.
func TestShouldRejectDifferentIdentityAfterCommit(t *testing.T) {
	for _, field := range []string{"launch", "activation"} {
		r := lifecycleFixture(t, "", false)
		n := len(r.Effects) - 1
		ready := &r.Effects[n]
		observed := *ready.Observed
		ready.Observed = &observed
		if field == "launch" {
			ready.Target.LaunchID = strings.Repeat("4", 32)
		} else {
			ready.Target.ActivationID = strings.Repeat("4", 32)
		}
		ready.Before.Current = ready.Target
		ready.Observed.Current = ready.Target
		if r.valid() {
			t.Fatal("different identity acknowledged", field)
		}
	}
}

// Intent is durable and owned before callback; read failures/mismatch retain uncertainty.
func TestShouldRetainUncertaintyOnPostCaptureFailure(t *testing.T) {
	for _, mode := range []string{"error", "mismatch", "panic"} {
		t.Run(mode, func(t *testing.T) {
			j := journal(t)
			tx, err := j.Admit(request("1"))
			if err != nil {
				t.Fatal(err)
			}
			bindFixture(t, tx, release.ObserveSelection(release.State{}), strings.Repeat("b", 64))
			prefixFixture(t, tx, "commit")
			defer tx.Close()
			intent := fixtureIntent(tx, "commit", strings.Repeat("b", 64))
			func() {
				defer func() {
					if mode == "panic" {
						_ = recover()
					}
				}()
				err = tx.Apply(intent, func() error {
					r, e := j.Query(request("1").ID)
					if e != nil || r.Effects[len(r.Effects)-1].Expected == nil || r.Effects[len(r.Effects)-1].Observed != nil {
						t.Fatal(r, e)
					}
					intent.Expected.StateSHA256 = strings.Repeat("f", 64)
					if mode == "panic" {
						panic("interrupted")
					}
					return nil
				}, func() (release.SelectionEvidence, error) {
					if mode == "error" {
						return release.SelectionEvidence{}, errors.New("read failed")
					}
					return intent.Before, nil
				})
			}()
			r, e := j.Query(request("1").ID)
			if e != nil {
				t.Fatal(e)
			}
			if r.Effects[len(r.Effects)-1].Expected.StateSHA256 == strings.Repeat("f", 64) {
				t.Fatal("intent mutated")
			}
			if mode == "panic" {
				if r.Effects[len(r.Effects)-1].State != EffectPending {
					t.Fatal(r)
				}
			} else if !errors.Is(err, release.ErrCleanupUncertain) || r.Outcome != Uncertain {
				t.Fatal(r, err)
			}
			if tx.Finish(Succeeded) == nil {
				t.Fatal("uncertainty settled")
			}
		})
	}
}
