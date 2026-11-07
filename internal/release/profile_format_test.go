package release

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Missing profile compatibility metadata cannot select or restart an artifact.
// This checks refusal before control mutations; real binary migration/rollback
// is covered separately by the profile-format end-to-end test.
func TestProfileFormat_should_rejectUnknownState_beforeActivation(t *testing.T) {
	for _, action := range []string{"rollback", "install", "ensure"} {
		t.Run(action, func(t *testing.T) {
			a, ma := bundle(t, "a")
			b, mb := bundle(t, "b")
			next, mn := bundle(t, "next")
			store := Store{Dir: t.TempDir()}
			control := control(ma, mb, mn)
			for _, path := range []string{a, b} {
				if err := store.Install(path, control); err != nil {
					t.Fatal(err)
				}
			}
			statePath := filepath.Join(store.Dir, "state.json")
			raw, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			var state map[string]any
			if err := json.Unmarshal(raw, &state); err != nil {
				t.Fatal(err)
			}
			slot := "previous"
			if action != "rollback" {
				slot = "current"
			}
			delete(state[slot].(map[string]any)["build"].(map[string]any), "profile_format")
			raw, err = json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(statePath, raw, 0600); err != nil {
				t.Fatal(err)
			}
			stops := control.stops
			switch action {
			case "rollback":
				err = store.Rollback(control)
			case "install":
				err = store.Install(next, control)
			case "ensure":
				_, err = store.ActivePath()
			}
			if err == nil {
				t.Error("unknown profile format was accepted")
			}
			if control.stops != stops || control.live.SHA256 != mb.Artifact.SHA256 {
				t.Error("unsupported profile format changed the running artifact")
			}
			after, readErr := os.ReadFile(statePath)
			if readErr != nil || string(after) != string(raw) {
				t.Error("unsupported profile format changed selection state", readErr)
			}
		})
	}
}
