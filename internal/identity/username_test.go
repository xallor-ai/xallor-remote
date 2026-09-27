package identity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestShould_stripDomain_whenWindowsUser(t *testing.T) {
	if got := stripDomain(`ANDY\andy`); got != "andy" {
		t.Fatalf("got %q", got)
	}
	if got := stripDomain(`DOMAIN/Alice`); got != "Alice" {
		t.Fatalf("got %q", got)
	}
	if got := stripDomain("bob"); got != "bob" {
		t.Fatalf("got %q", got)
	}
}

// 目的：新装机用登录用户名作 device_id，而不是随机 dev_…。
// 前置：空数据目录。预期：DeviceID == Username()（本机能取到用户名时）。
func TestShould_useUsernameAsDeviceID_whenNewStore(t *testing.T) {
	want := Username()
	if want == "" {
		t.Skip("no username in this environment")
	}
	st, err := LoadFrom(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if st.DeviceID != want {
		t.Fatalf("device_id=%q want %q", st.DeviceID, want)
	}
}

// 目的：已落盘的随机 dev_… 在加载时迁到登录用户名。
// 前置：identity.json 为旧随机 ID，且有 secret。预期：重载后 DeviceID 为用户名。
func TestShould_migrateRandomDevID_toUsername(t *testing.T) {
	want := Username()
	if want == "" {
		t.Skip("no username in this environment")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "identity.json"), []byte(`{"device_id":"dev_aabbccddee12","inbound":false}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "device_secret"), []byte("secrethex"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := LoadFrom(dir)
	if err != nil {
		t.Fatal(err)
	}
	if st.DeviceID != want {
		t.Fatalf("device_id=%q want %q", st.DeviceID, want)
	}
	again, err := LoadFrom(dir)
	if err != nil {
		t.Fatal(err)
	}
	if again.DeviceID != want {
		t.Fatalf("reload device_id=%q", again.DeviceID)
	}
}
