package release

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/releasetrust"
)

// This injects only the verifier's cleanup observation, not native cleanup or
// cryptographic proof. Unknown verifier resources keep the source receipt open.
func TestSourceAcquisition_shouldRetainUnknown_whenVerifierCleanupUncertain(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(map[bool]string{false: "clean-refusal", true: "unknown-family"}[uncertain], func(t *testing.T) {
			dir, m := sourceBundle(t, "verifier-settlement")
			r := &resourceTrace{}
			cause := releasetrust.ErrUnverified
			if uncertain {
				cause = errors.Join(cause, releasetrust.ErrCleanupUncertain)
			}
			var snapshot string
			store := Store{Dir: filepath.Join(t.TempDir(), "absent"), sourceVerifier: func(_ context.Context, file string, _ releasetrust.Identity, _, _ string) error {
				snapshot = filepath.Dir(file)
				return cause
			}}.WithResources(r)
			c := control(m)
			err := store.Install(dir, c)
			if !errors.Is(err, releasetrust.ErrUnverified) || errors.Is(err, ErrCleanupUncertain) != uncertain {
				t.Fatalf("wrong source result: %v", err)
			}
			if uncertain && (len(r.closed) != 0 || len(r.unknown) != 1) {
				t.Fatalf("unknown family settled: %+v", r)
			}
			if !uncertain && (len(r.closed) != 1 || len(r.unknown) != 0) {
				t.Fatalf("clean refusal unsettled: %+v", r)
			}
			if _, e := os.Stat(snapshot); !os.IsNotExist(e) {
				t.Fatalf("quarantine removal failed: %v", e)
			}
			if _, e := os.Stat(store.Dir); !os.IsNotExist(e) {
				t.Fatalf("install mutated: %v", e)
			}
			if c.checks != 0 || c.stops != 0 || c.runningChecks != 0 {
				t.Fatal("verifier refusal reached control")
			}
		})
	}
}
