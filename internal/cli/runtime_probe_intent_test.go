package cli

import (
	"context"
	"errors"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"os"
	"path/filepath"
	"testing"
)

type refusingProbeLedger struct{ probes int }

func (l *refusingProbeLedger) RecordIntent(r release.ResourceObligation) error {
	if r.Kind == "probe" {
		l.probes++
		return errors.New("durable probe intent unavailable")
	}
	return nil
}
func (*refusingProbeLedger) Close(release.ResourceObligation) error           { return nil }
func (*refusingProbeLedger) Unknown(release.ResourceObligation, string) error { return nil }
func (*refusingProbeLedger) RetainSelected(release.ResourceObligation, release.ReadyObservation) error {
	return nil
}

// The real fixture can write its marker, but failed intent persistence must
// prevent executing that same image. Provenance remains controlled test data.
func TestRuntimeProbe_shouldNotExecute_whenIntentPersistenceFails(t *testing.T) {
	_, binary, m := nativeOwnerBundle(t)
	marker := filepath.Join(t.TempDir(), "executed")
	t.Setenv("XR_TEST_PROBE_INTENT_MARKER", marker)
	if _, err := probeRuntimeIdentity(context.Background(), binary, m.Artifact.SHA256); err != nil {
		t.Fatal("calibration", err)
	}
	if raw, err := os.ReadFile(marker); err != nil || string(raw) != "executed" {
		t.Fatal("marker calibration", err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	s, err := runtimeStore()
	if err != nil {
		t.Fatal(err)
	}
	ledger := &refusingProbeLedger{}
	err = s.WithResources(ledger).Install(filepath.Dir(binary), runtimeControl{})
	if !errors.Is(err, release.ErrCleanupUncertain) || ledger.probes != 1 {
		t.Fatal(err, ledger)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("probe executed despite failed durable intent", err)
	}
	state, err := s.Read()
	if err != nil || state.Current != nil || state.Pending != nil {
		t.Fatal(state, err)
	}
}
