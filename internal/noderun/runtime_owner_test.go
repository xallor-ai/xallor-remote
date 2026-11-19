package noderun

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/identity"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"github.com/xallor-ai/xallor-remote/internal/release"
)

func ownerTestStore(t *testing.T) *identity.Store {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XALLOR_REMOTE_DATA_DIR", dir)
	store, err := identity.LoadFrom(dir)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

// Pending or unreadable selections must never be projected as ready merely
// because this process answers IPC. Status reports the reason without mutation.
func TestRuntimeOwner_should_reportPendingAndUnknown_withoutInventingActivation(t *testing.T) {
	for _, state := range []string{"pending", "corrupt", "absent"} {
		t.Run(state, func(t *testing.T) {
			store := ownerTestStore(t)
			runtimeDir := filepath.Join(store.Dir, "runtime")
			if err := os.Mkdir(runtimeDir, 0700); err != nil {
				t.Fatal(err)
			}
			statePath := filepath.Join(runtimeDir, "state.json")
			var before []byte
			wantState, wantCode := "unverified", protocol.RuntimeSourceUnverified
			switch state {
			case "pending":
				name := "xallor-remote"
				if runtime.GOOS == "windows" {
					name += ".exe"
				}
				m := release.Manifest{Schema: 1, Build: buildinfo.Current(), Artifact: release.Artifact{Filename: name, Size: 1, SHA256: strings.Repeat("a", 64)}}
				var err error
				before, err = json.Marshal(release.State{Pending: &m, PendingActivation: strings.Repeat("b", 32)})
				if err != nil {
					t.Fatal(err)
				}
				wantState, wantCode = "pending", protocol.RuntimePendingRecovery
			case "corrupt":
				before = []byte(`{"current":`)
				wantState = "unknown"
			}
			if before != nil {
				if err := os.WriteFile(statePath, before, 0600); err != nil {
					t.Fatal(err)
				}
			}
			d := New(store, nil)
			defer d.Stop()
			a, b := net.Pipe()
			defer b.Close()
			_ = b.SetDeadline(time.Now().Add(3 * time.Second))
			go d.serveIPC(ipc.NewConn(a))
			client := ipc.NewConn(b)
			if err := ipc.Call(client, "ensure", "runtime.ensure", map[string]int{"required_profile_format": buildinfo.ProfileFormat(), "required_protocol": buildinfo.Protocol}); err != nil {
				t.Fatal(err)
			}
			frame, err := client.ReadFrame()
			if err != nil || frame.OK == nil || *frame.OK || frame.Code != wantCode {
				t.Fatalf("ensure hid installation state: %+v %v", frame, err)
			}
			if err := ipc.Call(client, "observe", "runtime.status", map[string]any{}); err != nil {
				t.Fatal(err)
			}
			frame, err = client.ReadFrame()
			if err != nil || frame.OK == nil || !*frame.OK {
				t.Fatalf("status could not describe refusal: %+v %v", frame, err)
			}
			var result struct {
				Runtime struct {
					State, Code  string
					ActivationID string `json:"activation_id"`
				} `json:"runtime"`
			}
			if err := json.Unmarshal(frame.Result, &result); err != nil || result.Runtime.State != wantState || result.Runtime.Code != wantCode || result.Runtime.ActivationID != "" {
				t.Fatal("status invented readiness or an activation", result, err)
			}
			after, readErr := os.ReadFile(statePath)
			if before == nil {
				if !os.IsNotExist(readErr) {
					t.Fatal("observation created selection state", readErr)
				}
			} else if readErr != nil || string(after) != string(before) {
				t.Fatal("observation mutated selection state", readErr)
			}
		})
	}
}

// Ensure requirements are mandatory, not optional hints. Missing/null/malformed
// fields must fail before any activation observation or source verification.
func TestRuntimeOwner_should_rejectMissingEnsureRequirements(t *testing.T) {
	for _, params := range []string{`null`, `{}`, `{"required_protocol":1}`, `{"required_profile_format":null,"required_protocol":1}`} {
		t.Run(params, func(t *testing.T) {
			store := ownerTestStore(t)
			d := New(store, nil)
			defer d.Stop()
			a, b := net.Pipe()
			defer b.Close()
			_ = b.SetDeadline(time.Now().Add(3 * time.Second))
			go d.serveIPC(ipc.NewConn(a))
			client := ipc.NewConn(b)
			if err := ipc.Call(client, "missing", "runtime.ensure", json.RawMessage(params)); err != nil {
				t.Fatal(err)
			}
			frame, err := client.ReadFrame()
			if err != nil || frame.OK == nil || *frame.OK || frame.Code != protocol.RuntimeProfileMismatch {
				t.Fatalf("missing requirements accepted: %+v %v", frame, err)
			}
		})
	}
}
