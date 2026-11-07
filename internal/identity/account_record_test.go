package identity

import (
	"os"
	"path/filepath"
	"testing"
)

// A present but incomplete/null/ambiguous record is damaged configuration, not
// an explicit account clear. Local recovery must remain available.
func TestAccount_should_rejectDamagedRecord_withoutSharedFallback(t *testing.T) {
	for _, raw := range []string{
		`null`, `{}`, `{"relay":""}`, `{"token":""}`, `{"relay":null,"token":""}`,
		`{"relay":"","token":null}`, `{"relay":"wss://saved.invalid/remote","token":""}`,
		`{"relay":"","token":"","unknown":true}`, `{"relay":"","token":"","token":""}`,
		`{"Relay":"","token":""}`, `{"relay":"","token":false}`,
	} {
		t.Run(raw, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "account.json")
			if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			store, err := LoadFrom(dir)
			if err != nil {
				t.Fatal("local recovery unavailable", err)
			}
			if _, _, err := store.Credentials(); err == nil || !store.AccountConfigured() {
				t.Fatal("damaged account silently became shared access")
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != raw {
				t.Fatal("loader changed damaged credentials", err)
			}
			if err := store.SetAccount(""); err != nil {
				t.Fatal(err)
			}
			reloaded, err := LoadFrom(dir)
			if err != nil {
				t.Fatal(err)
			}
			if _, token, err := reloaded.Credentials(); err != nil || token != "" || reloaded.AccountConfigured() {
				t.Fatal("explicit canonical clear did not survive reload", err)
			}
		})
	}
}
