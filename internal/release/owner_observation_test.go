package release

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"github.com/xallor-ai/xallor-remote/internal/releasetrust"
)

// Readiness requires both provenance subjects on each observation and returns
// the selected opaque ID. This is state-machine evidence with a private verifier.
func TestObserveRuntime_should_requireFreshProof_andPreserveSelection(t *testing.T) {
	dir, m := bundle(t, "observed")
	s := mechanicsStore(t)
	c := control(m)
	if err := s.Install(dir, c); err != nil {
		t.Fatal(err)
	}
	state, err := s.Read()
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(s.Dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	proofs := 0
	s.sourceVerifier = func(context.Context, string, releasetrust.Identity, string, string) error { proofs++; return nil }
	for range 2 {
		view, err := s.ObserveRuntime(context.Background(), *c.live)
		if err != nil || view.State != "ready" || view.Code != "" || view.ActivationID != state.CurrentActivation || view.SHA256 != m.Artifact.SHA256 || view.PID != c.live.PID {
			t.Fatal("verified selected runtime not observed", view, err)
		}
	}
	after, err := os.ReadFile(filepath.Join(s.Dir, "state.json"))
	if err != nil || string(before) != string(after) || proofs != 4 {
		t.Fatal("observation mutated selection or cached source proof", proofs, err)
	}
}

// A healthy IPC process cannot hide proof failure, changed bytes, wrong loaded
// identity or an installation that changed while the verifier was running.
func TestObserveRuntime_should_refuseUncertainOrUnverifiedSelection(t *testing.T) {
	for _, kind := range []string{"proof", "bytes", "digest", "path", "build", "owner-protocol", "pending-during-proof", "changed-during-proof"} {
		t.Run(kind, func(t *testing.T) {
			dir, m := bundle(t, "observe-"+kind)
			s := mechanicsStore(t)
			c := control(m)
			if err := s.Install(dir, c); err != nil {
				t.Fatal(err)
			}
			st, err := s.Read()
			if err != nil {
				t.Fatal(err)
			}
			live := *c.live
			wantState, wantCode := "unverified", protocol.RuntimeSourceUnverified
			switch kind {
			case "owner-protocol":
				live.OwnerProtocol = 0
				wantState, wantCode = "unknown", protocol.RuntimeProfileMismatch
			case "proof":
				s.sourceVerifier = func(context.Context, string, releasetrust.Identity, string, string) error {
					return releasetrust.ErrUnverified
				}
			case "bytes":
				if err := os.WriteFile(live.Path, []byte("changed"), 0700); err != nil {
					t.Fatal(err)
				}
			case "digest":
				live.SHA256 = strings.Repeat("0", 64)
				wantState, wantCode = "unknown", protocol.RuntimeStartFailed
			case "path":
				live.Path = filepath.Join(t.TempDir(), m.Artifact.Filename)
				wantState, wantCode = "unknown", protocol.RuntimeStartFailed
			case "build":
				live.Revision = strings.Repeat("0", 40)
				wantState, wantCode = "unknown", protocol.RuntimeStartFailed
			case "pending-during-proof":
				s.sourceVerifier = func(context.Context, string, releasetrust.Identity, string, string) error {
					st.Pending, st.PendingActivation = &m, strings.Repeat("c", 32)
					return s.save(st)
				}
				wantState, wantCode = "pending", protocol.RuntimePendingRecovery
			case "changed-during-proof":
				s.sourceVerifier = func(context.Context, string, releasetrust.Identity, string, string) error {
					st.CurrentActivation = strings.Repeat("c", 32)
					return s.save(st)
				}
				wantState, wantCode = "unknown", protocol.RuntimeStartFailed
			}
			view, err := s.ObserveRuntime(context.Background(), live)
			if err == nil || view.State != wantState || view.Code != wantCode || view.ActivationID != "" {
				t.Fatal("observation certified uncertain state", view, err)
			}
			if kind == "proof" && !errors.Is(err, releasetrust.ErrUnverified) {
				t.Fatal("source error was hidden", err)
			}
		})
	}
}
