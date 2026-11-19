package release

import (
	"context"
	"errors"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/releasetrust"
)

// Fresh source authentication precedes profile-lock creation, while the guard
// encloses all candidate probing and startup under lifecycle -> install order.
func TestBootstrapGuardRunsAfterSourceProofBeforeCandidateProbe(t *testing.T) {
	for _, refuse := range []bool{false, true} {
		bundle, m := bundle(t, "guarded-bootstrap")
		s := mechanicsStore(t)
		c := control(m)
		proofs := 0
		s.sourceVerifier = func(context.Context, string, releasetrust.Identity, string, string) error { proofs++; return nil }
		denied := errors.New("lifecycle admission unavailable")
		called := false
		s = s.WithBootstrapGuard(func(run func() error) error {
			called = true
			if proofs < 2 || c.checks != 0 || c.live != nil {
				t.Fatal("guard outside intended boundary", proofs, c.checks)
			}
			if refuse {
				return denied
			}
			return run()
		})
		err := s.EnsureRunning(bundle, c)
		if !called || (refuse && !errors.Is(err, denied)) || (!refuse && err != nil) {
			t.Fatal(called, err)
		}
		if refuse && (c.checks != 0 || c.live != nil) {
			t.Fatal("denied bootstrap reached candidate")
		}
	}
}
