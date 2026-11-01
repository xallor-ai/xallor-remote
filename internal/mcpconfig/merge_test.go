package mcpconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// should_add_server_when_missing: empty/missing file gets xallor-remote once.
func TestShouldAddServerWhenMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	r, err := Merge(path, Options{DeviceID: "dev_a", Grant: "xr_grant_a", Command: "xallor-remote-mcp"})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Changed || !r.Created {
		t.Fatalf("changed=%v created=%v", r.Changed, r.Created)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	servers := root["mcpServers"].(map[string]any)
	srv := servers[ServerKey].(map[string]any)
	if srv["command"] != "xallor-remote-mcp" {
		t.Fatalf("command=%v", srv["command"])
	}
	env := srv["env"].(map[string]any)
	if env["XALLOR_REMOTE_DEVICE_ID"] != "dev_a" {
		t.Fatalf("id=%v", env["XALLOR_REMOTE_DEVICE_ID"])
	}
}

// should_leave_existing_when_present: second merge is a no-op.
func TestShouldLeaveExistingWhenPresent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	if _, err := Merge(path, Options{DeviceID: "dev_a", Grant: "xr_grant_a"}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	r, err := Merge(path, Options{DeviceID: "dev_b", Grant: "xr_grant_b"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Changed {
		t.Fatal("second merge should not change")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("file mutated")
	}
}

// should_preserve_other_servers: host-execution entry stays.
func TestShouldPreserveOtherServers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	seed := []byte(`{"mcpServers":{"other":{"command":"other-bin"}}}`)
	if err := os.WriteFile(path, seed, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Merge(path, Options{}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var root map[string]any
	_ = json.Unmarshal(raw, &root)
	servers := root["mcpServers"].(map[string]any)
	if _, ok := servers["other"]; !ok {
		t.Fatal("other server removed")
	}
	if _, ok := servers[ServerKey]; !ok {
		t.Fatal("xallor-remote missing")
	}
}

// 目的：未指定 ID 时模板占位必须是 9 位设备 ID，不能再写「对方用户名」。
func TestShould_useNumericDeviceIDPlaceholder_whenMergeEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	if _, err := Merge(path, Options{}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	env := root["mcpServers"].(map[string]any)[ServerKey].(map[string]any)["env"].(map[string]any)
	got, _ := env["XALLOR_REMOTE_DEVICE_ID"].(string)
	if got != "对方的9位设备ID" {
		t.Fatalf("placeholder=%q", got)
	}
}
