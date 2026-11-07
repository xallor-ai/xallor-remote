package identity

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
	_ "modernc.org/sqlite"
)

const deviceDBName = "identity.db"

type deviceRecord struct {
	DeviceID, Secret, Grant  string
	Inbound, Active, Cleanup bool
	Revision                 int64
}

var errIdentityChanged = errors.New("本机身份状态已变化，请重新连接本机服务")

func openDeviceDB(dir string, create bool) (*sql.DB, error) {
	path, err := filepath.Abs(filepath.Join(dir, deviceDBName))
	if err != nil {
		return nil, err
	}
	if create {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			err = f.Close()
		} else if os.IsExist(err) {
			err = nil
		}
		if err != nil {
			return nil, err
		}
	}
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=rw&_pragma=busy_timeout%3D100&_pragma=synchronous%3DFULL"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

const selectDeviceRecord = `SELECT device_id, secret, grant, inbound, active, cleanup, revision FROM identity
 WHERE singleton=1 AND (SELECT user_version FROM pragma_user_version)=1`

func readDeviceRecord(row *sql.Row) (deviceRecord, error) {
	var r deviceRecord
	err := row.Scan(&r.DeviceID, &r.Secret, &r.Grant, &r.Inbound, &r.Active, &r.Cleanup, &r.Revision)
	invalidActive := r.Active && (!protocol.IsNumericDeviceID(r.DeviceID) || r.Secret == "" || (r.Inbound && r.Grant == ""))
	invalidReset := !r.Active && (r.DeviceID != "" || r.Secret != "" || r.Grant != "" || r.Inbound)
	if err == nil && (r.Revision < 1 || invalidActive || invalidReset) {
		err = errors.New("本机身份数据无效，未生成替代身份")
	}
	return r, err
}

func freshDevice() (deviceRecord, error) {
	r := deviceRecord{Active: true, Revision: 1}
	var err error
	if r.DeviceID, err = protocol.NewDeviceID(); err != nil {
		return r, err
	}
	r.Secret, err = protocol.NewSecret()
	return r, err
}

func loadDeviceRecord(dir string, initialize bool) (deviceRecord, error) {
	db, err := openDeviceDB(dir, initialize)
	if err != nil {
		return deviceRecord{}, err
	}
	defer db.Close()
	if !initialize {
		r, err := readDeviceRecord(db.QueryRow(selectDeviceRecord))
		if err == nil && !r.Active {
			err = errIdentityChanged
		}
		return r, err
	}
	tx, err := db.Begin()
	if err != nil {
		return deviceRecord{}, err
	}
	defer tx.Rollback()
	var version int
	if err := tx.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return deviceRecord{}, err
	}
	var r deviceRecord
	switch version {
	case 0:
		r, err = importDeviceFiles(dir)
		if err != nil {
			return deviceRecord{}, err
		}
		_, err = tx.Exec(`CREATE TABLE identity (
		 singleton INTEGER PRIMARY KEY CHECK(singleton=1),
		 device_id TEXT NOT NULL, secret TEXT NOT NULL, grant TEXT NOT NULL,
		 inbound INTEGER NOT NULL CHECK(inbound IN(0,1)), active INTEGER NOT NULL CHECK(active IN(0,1)),
		 cleanup INTEGER NOT NULL CHECK(cleanup IN(0,1)), revision INTEGER NOT NULL CHECK(revision>0),
		 CHECK(inbound=0 OR length(grant)>0))`)
		if err == nil {
			_, err = tx.Exec(`INSERT INTO identity VALUES(1,?,?,?,?,?,?,?)`, r.DeviceID, r.Secret, r.Grant, r.Inbound, r.Active, r.Cleanup, r.Revision)
		}
		if err == nil {
			_, err = tx.Exec(`PRAGMA user_version=1`)
		}
	case 1:
		r, err = readDeviceRecord(tx.QueryRow(selectDeviceRecord))
		if err == nil && !r.Active {
			if err := finishProfileReset(dir); err != nil {
				return deviceRecord{}, err
			}
			var next deviceRecord
			next, err = freshDevice()
			if err == nil {
				next.Revision = r.Revision + 1
				_, err = tx.Exec(`UPDATE identity SET device_id=?,secret=?,grant='',inbound=0,active=1,revision=? WHERE singleton=1`, next.DeviceID, next.Secret, next.Revision)
				r = next
			}
		}
	default:
		err = fmt.Errorf("不支持的本机身份格式: %d", version)
	}
	if err != nil {
		return deviceRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return deviceRecord{}, err
	}
	if r.Cleanup {
		if err := finishDeviceImport(dir, db); err != nil {
			return deviceRecord{}, err
		}
		r.Cleanup = false
	}
	return r, nil
}

// A single guarded statement commits the complete device state. A stale Store
// cannot resurrect a reset profile or overwrite another writer's grant.
func replaceDeviceRecord(dir string, before, next deviceRecord) (deviceRecord, error) {
	db, err := openDeviceDB(dir, false)
	if err != nil {
		return before, err
	}
	defer db.Close()
	result, err := db.Exec(`UPDATE identity SET device_id=?,secret=?,grant=?,inbound=?,active=?,revision=revision+1
	 WHERE singleton=1 AND device_id=? AND secret=? AND revision=? AND active=1
	 AND (SELECT user_version FROM pragma_user_version)=1`, next.DeviceID, next.Secret, next.Grant, next.Inbound, next.Active, before.DeviceID, before.Secret, before.Revision)
	if err != nil {
		return before, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return before, err
	}
	if n != 1 {
		return before, errIdentityChanged
	}
	next.Revision = before.Revision + 1
	return next, nil
}

func (s *Store) publishDevice(next deviceRecord) error {
	next, err := replaceDeviceRecord(s.Dir, s.device, next)
	if err != nil {
		return err
	}
	s.device = next
	return nil
}

// Reconnection takes its desired state from durable storage, not an old cache
// after an uncertain write or a concurrent profile change.
func (s *Store) RefreshDevice() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := loadDeviceRecord(s.Dir, false)
	if err == nil && (r.DeviceID != s.device.DeviceID || r.Secret != s.device.Secret) {
		err = errIdentityChanged
	}
	if err == nil {
		s.device = r
	}
	return err
}

func (s *Store) ID() string { s.mu.Lock(); defer s.mu.Unlock(); return s.device.DeviceID }
