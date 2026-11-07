package identity

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// 目的：新装机用 9 位数字 device_id，不是登录用户名。
func TestShould_useNumericDeviceID_whenNewStore(t *testing.T) {
	st, err := LoadFrom(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !protocol.IsNumericDeviceID(st.ID()) {
		t.Fatalf("device_id=%q", st.ID())
	}
}

// 目的：已有 9 位数字 ID 升级后保持不变。
func TestShould_keepNumericDeviceID_whenReloaded(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "identity.json"), []byte(`{"device_id":"790442262","inbound":true}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "device_secret"), []byte("secrethex"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := LoadFrom(dir)
	if err != nil {
		t.Fatal(err)
	}
	if st.ID() != "790442262" {
		t.Fatalf("device_id=%q", st.ID())
	}
}

// 目的：用户名 / 旧 dev_ 形态会迁成数字设备 ID。
func TestShould_migrateNonNumericDeviceID(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "identity.json"), []byte(`{"device_id":"andy","inbound":false}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "device_secret"), []byte("secrethex"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := LoadFrom(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !protocol.IsNumericDeviceID(st.ID()) {
		t.Fatalf("device_id=%q", st.ID())
	}
}

func TestInboundOffKeepsGrant(t *testing.T) {
	st, err := LoadFrom(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := st.IssueGrant()
	if err != nil || g == "" {
		t.Fatal(err)
	}
	if _, err := st.SetInbound(false); err != nil {
		t.Fatal(err)
	}
	_, _, grant, _, _, inbound, _ := st.Snapshot()
	if grant != g || inbound {
		t.Fatalf("grant=%q inbound=%v", grant, inbound)
	}
	again, err := LoadFrom(st.Dir)
	if err != nil {
		t.Fatal(err)
	}
	_, _, g2, _, _, in2, _ := again.Snapshot()
	if g2 != g || in2 {
		t.Fatalf("reload grant=%q inbound=%v", g2, in2)
	}
}

// 目的：联调时环境变量覆盖已落盘的 relay_url，不必改 config.json。
// 前置：目录里已有旧 URL。预期：LoadFrom 采用环境变量。
func TestShould_useEnvRelayURL_whenSet(t *testing.T) {
	t.Setenv("XALLOR_REMOTE_RELAY_URL", "wss://api.xallor.com/remote")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"relay_url":"wss://old.example"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := LoadFrom(dir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Config.RelayURL != "wss://api.xallor.com/remote" {
		t.Fatalf("relay=%q", st.Config.RelayURL)
	}
}

func TestWipeRemovesIdentityFiles(t *testing.T) {
	dir := t.TempDir()
	st, err := LoadFrom(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.IssueGrant(); err != nil {
		t.Fatal(err)
	}
	if err := st.Wipe(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "identity.json")); !os.IsNotExist(err) {
		t.Fatal("identity.json should be gone")
	}
}

func TestRemovePeer(t *testing.T) {
	st, err := LoadFrom(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddPeer("dev_b", "xr_grant_x"); err != nil {
		t.Fatal(err)
	}
	if err := st.RemovePeer("dev_b"); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Peer("dev_b"); ok {
		t.Fatal("peer still there")
	}
}
