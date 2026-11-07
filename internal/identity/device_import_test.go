package identity

import (
	"os"
	"path/filepath"
	"testing"
)

func writeDeviceFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, value := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

// Migration retains the original numeric identity and exact credentials, then
// removes source files. Reintroduced old files can never override the database.
func TestDevice_should_migrateOnce_withoutCredentialFallback(t *testing.T) {
	dir := t.TempDir()
	writeDeviceFiles(t, dir, map[string]string{"identity.json": `{"device_id":"790442262","inbound":true}`, "device_secret": "original-secret", "grant": "original-grant"})
	s, err := LoadFrom(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.device.DeviceID != "790442262" || s.device.Secret != "original-secret" || s.device.Grant != "original-grant" || !s.device.Inbound {
		t.Fatal("migration changed credentials")
	}
	for _, name := range deviceImportFiles {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatal("migration retained source file", name)
		}
	}
	writeDeviceFiles(t, dir, map[string]string{"identity.json": "malformed", "device_secret": "wrong", "grant": "wrong"})
	again, err := LoadFrom(dir)
	if err != nil {
		t.Fatal(err)
	}
	if again.device != s.device {
		t.Fatal("old file overrode canonical identity")
	}
}

// A committed import with interrupted cleanup resumes deletion only. Even
// malformed leftover source files must not be parsed or reimported.
func TestDevice_should_resumeCleanup_whenImportAlreadyCommitted(t *testing.T) {
	s := newDeviceStore(t)
	expected := s.device
	db := deviceDatabase(t, s.Dir)
	mustExecDevice(t, db, "UPDATE identity SET cleanup=1")
	writeDeviceFiles(t, s.Dir, map[string]string{"identity.json": "malformed", "grant": "obsolete"})
	again, err := LoadFrom(s.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if again.device != expected {
		t.Fatal("cleanup changed imported identity")
	}
	for _, name := range deviceImportFiles {
		if _, err := os.Stat(filepath.Join(s.Dir, name)); !os.IsNotExist(err) {
			t.Fatal("cleanup incomplete", name)
		}
	}
	var pending bool
	if err := db.QueryRow("SELECT cleanup FROM identity").Scan(&pending); err != nil || pending {
		t.Fatal("cleanup marker was not committed", err)
	}
}

// An incomplete source profile is a recovery error, never a new installation.
// Failed imports leave the exact source bytes available for explicit recovery.
func TestDevice_should_preserveSources_whenMigrationInvalid(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"grant_only":     {"grant": "saved-grant"},
		"missing_secret": {"identity.json": `{"device_id":"790442262"}`},
		"bad_json":       {"identity.json": "{", "device_secret": "saved-secret"},
		"empty_id":       {"identity.json": "{}", "device_secret": "saved-secret"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeDeviceFiles(t, dir, files)
			if _, err := LoadFrom(dir); err == nil {
				t.Fatal("invalid source silently replaced")
			}
			for file, want := range files {
				got, err := os.ReadFile(filepath.Join(dir, file))
				if err != nil || string(got) != want {
					t.Fatal("source changed", err)
				}
			}
		})
	}
}

// Reset may span ancillary files; a failed deletion must leave an inactive
// identity and prevent startup from inheriting old peers/account credentials.
func TestDevice_should_resumeResetCleanup_beforeGeneratingIdentity(t *testing.T) {
	s := newDeviceStore(t)
	blocker := filepath.Join(s.Dir, "config.json")
	if err := os.Mkdir(blocker, 0700); err != nil {
		t.Fatal(err)
	}
	writeDeviceFiles(t, blocker, map[string]string{"keep": "test-owned blocker"})
	writeDeviceFiles(t, s.Dir, map[string]string{"account.json": `{"token":"obsolete"}`})
	if err := s.Wipe(); err == nil {
		t.Fatal("reset ignored cleanup failure")
	}
	if s.device.Active {
		t.Fatal("failed cleanup restored the identity")
	}
	if _, err := LoadFrom(s.Dir); err == nil {
		t.Fatal("startup skipped unfinished reset")
	}
	if err := os.Remove(filepath.Join(blocker, "keep")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	fresh, err := LoadFrom(s.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if !fresh.device.Active || fresh.device.Grant != "" {
		t.Fatal("reset did not finish")
	}
	if _, token, err := fresh.Credentials(); err != nil || token != "" {
		t.Fatal("new identity inherited old account", err)
	}
}

// A damaged SQLite file cannot be interpreted as a fresh profile.
func TestDevice_should_preserveCorruptDatabase_withoutReplacement(t *testing.T) {
	dir := t.TempDir()
	raw := "damaged database"
	writeDeviceFiles(t, dir, map[string]string{deviceDBName: raw})
	if _, err := LoadFrom(dir); err == nil {
		t.Fatal("damaged database accepted")
	}
	got, err := os.ReadFile(filepath.Join(dir, deviceDBName))
	if err != nil || string(got) != raw {
		t.Fatal("damaged database replaced", err)
	}
}
