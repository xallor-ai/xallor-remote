package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/runtimeprocess"
)

func observeIndependentHandoffDescendant(t *testing.T, h *independentHarness, target *independentProfile, executable string, parentPID int) runtimeprocess.ExitWatch {
	t.Helper()
	var ready struct {
		PID       int `json:"pid"`
		ParentPID int `json:"parent_pid"`
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		raw, err := os.ReadFile(filepath.Join(target.dir, "independent-handoff-leaf-ready.json"))
		if err == nil && json.Unmarshal(raw, &ready) == nil && ready.PID > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("handoff descendant never announced readiness")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if ready.PID == parentPID || ready.ParentPID != parentPID {
		t.Fatal("descendant parent identity mismatch")
	}
	watch, err := runtimeprocess.WatchImage(ready.PID, executable, h.manifest.Artifact.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	h.afterStop = append(h.afterStop, func() error {
		exited, err := watch.Exited()
		h.observations["descendant_exited_at_cleanup"] = exited && err == nil
		if !exited {
			err = errors.Join(err, errors.New("handoff descendant cleanup unconfirmed"))
		}
		return errors.Join(err, watch.Close())
	})
	if exited, err := watch.Exited(); err != nil || exited {
		t.Fatal("descendant was not alive before revoke", err)
	}
	h.observations["descendant_pid"] = ready.PID
	h.observations["descendant_image_bound"] = true
	h.observations["descendant_initially_live"] = true
	return watch
}
