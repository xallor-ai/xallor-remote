//go:build windows || linux

package release

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type sourceClosureTrace struct {
	resourceTrace
	beforeClose func()
}

func (r *sourceClosureTrace) Close(v ResourceObligation) error {
	r.beforeClose()
	return r.resourceTrace.Close(v)
}

// Real input handles must remain open during consumption and be closed before
// the source receipt settles, including when the consumer returns an error.
func TestSourceAcquisition_shouldCloseInputsBeforeReceipt_whenConsumerReturns(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "consumer-error"}[fail], func(t *testing.T) {
			dir, m := sourceBundle(t, "acquisition")
			cause := errors.Join(ErrCleanupUncertain, errors.New("consumer failed"))
			var snapshot string
			r := &sourceClosureTrace{}
			r.beforeClose = func() {
				assertSourceInputsOpen(t, dir, m.Artifact.Filename, false)
				if _, err := os.Stat(snapshot); !os.IsNotExist(err) {
					t.Errorf("source receipt settled before quarantine removal: %v", err)
				}
			}
			got, err := mechanicsStore(t).WithResources(r).withSourceSnapshot(context.Background(), dir, "", func(binary string, _ Manifest, _ []byte) error {
				snapshot = filepath.Dir(binary)
				assertSourceInputsOpen(t, dir, m.Artifact.Filename, true)
				if fail {
					return cause
				}
				return nil
			})
			assertSourceInputsOpen(t, dir, m.Artifact.Filename, false)
			if fail {
				if !errors.Is(err, cause) || got != (Manifest{}) {
					t.Fatalf("consumer failure lost: %+v %v", got, err)
				}
			} else if err != nil || got != m {
				t.Fatalf("successful consumption: %+v %v", got, err)
			}
			if len(r.closed) != 1 || len(r.unknown) != 0 {
				t.Fatalf("closure observations: %+v", r.resourceTrace)
			}
		})
	}
}
