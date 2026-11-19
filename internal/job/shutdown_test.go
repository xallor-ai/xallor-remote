package job

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Shutdown observation shares startup's strict decoder, but must not run its
// state recovery or remove even a provably unstarted reservation.
func TestCheckReleased_should_observeWithoutRecoveringOrRewritingRecords(t *testing.T) {
	for _, state := range []string{"reserved", "occupied", "uncertain"} {
		t.Run(state, func(t *testing.T) {
			dir := t.TempDir()
			id := strings.Repeat("a", 32)
			raw, err := json.Marshal(diskState{Version: 1, Jobs: map[string]Record{id: {ID: id, Scope: testScope, Kind: "exec", Key: "shutdown", State: state}}})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "jobs.json")
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			err = CheckReleased(dir)
			if state == "reserved" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, ErrUncertain) {
				t.Fatal("lost occupancy was treated as released", err)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != string(raw) {
				t.Fatal("observation mutated ledger", err)
			}
		})
	}
}

// Missing profile has no ledger to inspect; observation must not create one.
func TestCheckReleased_should_notCreateAnAbsentProfile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	if err := CheckReleased(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("observation created profile", err)
	}
}
