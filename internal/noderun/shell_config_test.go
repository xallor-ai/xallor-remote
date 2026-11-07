package noderun

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/identity"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
)

// The local configuration API must roundtrip an explicit shell selection so
// heads and Agents can distinguish configuration from a guessed default.
func TestConfig_should_returnConfiguredShell(t *testing.T) {
	store, err := identity.LoadFrom(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetConfig("", "", "pwsh"); err != nil {
		t.Fatal(err)
	}
	d := New(store, nil)
	defer d.Stop()
	a, b := net.Pipe()
	defer b.Close()
	_ = b.SetDeadline(time.Now().Add(3 * time.Second))
	go d.serveIPC(ipc.NewConn(a))
	client := ipc.NewConn(b)
	if err := ipc.Call(client, "cfg", "config.get", nil); err != nil {
		t.Fatal(err)
	}
	frame, err := client.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Shell string `json:"shell"`
	}
	if err := json.Unmarshal(frame.Result, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Shell != "pwsh" {
		t.Fatalf("configured shell lost: %s", frame.Result)
	}
	// A type error can still populate earlier JSON fields. Do not publish that
	// partially decoded configuration after reporting malformed input.
	before := store.ConfigSnapshot()
	if err := ipc.Call(client, "bad", "config.set", json.RawMessage(`{"relay_url":"ws://changed.invalid","shell":7}`)); err != nil {
		t.Fatal(err)
	}
	frame, err = client.ReadFrame()
	if err != nil || frame.OK == nil || *frame.OK {
		t.Fatal("malformed config accepted", frame, err)
	}
	if store.ConfigSnapshot() != before {
		t.Fatal("partially decoded settings were published")
	}
}
