package identity

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

var deviceImportFiles = []string{"identity.json", "device_secret", "grant"}

// Import is a one-time storage transaction. After user_version=1 is committed,
// the database is authoritative even if cleanup was interrupted; old files are
// never read as fallback and never written again.
func importDeviceFiles(dir string) (deviceRecord, error) {
	raw := make(map[string][]byte)
	for _, name := range deviceImportFiles {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil && !os.IsNotExist(err) {
			return deviceRecord{}, err
		}
		if err == nil {
			raw[name] = b
		}
	}
	if len(raw) == 0 {
		return freshDevice()
	}
	var old struct {
		DeviceID string `json:"device_id"`
		Inbound  *bool  `json:"inbound"`
	}
	if len(raw["identity.json"]) == 0 || json.Unmarshal(raw["identity.json"], &old) != nil || old.DeviceID == "" || len(raw["device_secret"]) == 0 {
		return deviceRecord{}, errors.New("本机身份文件不完整，未生成替代身份")
	}
	r := deviceRecord{DeviceID: old.DeviceID, Secret: string(raw["device_secret"]), Grant: string(raw["grant"]), Active: true, Cleanup: true, Revision: 1}
	if !protocol.IsNumericDeviceID(r.DeviceID) {
		id, err := protocol.NewDeviceID()
		if err != nil {
			return deviceRecord{}, err
		}
		r.DeviceID = id
	}
	r.Inbound = r.Grant != "" && (old.Inbound == nil || *old.Inbound)
	return r, nil
}

func finishDeviceImport(dir string, db *sql.DB) error {
	for _, name := range deviceImportFiles {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	_, err := db.Exec(`UPDATE identity SET cleanup=0 WHERE singleton=1`)
	return err
}
