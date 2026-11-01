package identity

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
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
	if !protocol.IsNumericDeviceID(st.DeviceID) {
		t.Fatalf("device_id=%q", st.DeviceID)
	}
}
