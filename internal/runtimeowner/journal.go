package runtimeowner

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/xallor-ai/xallor-remote/internal/filelock"
	_ "modernc.org/sqlite"
)

const maxRecordBytes = 131072

type Journal struct{ Dir string }

// Admit durably reserves a request while retaining the OS writer lock for the
// entire transaction. Lock release on process exit is not permission to resume.
func (j Journal) Admit(req Request) (*Transaction, error) {
	if !req.valid() {
		return nil, ErrInvalid
	}
	namespace, err := j.NamespaceID()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(j.Dir, 0700); err != nil {
		return nil, err
	}
	unlock, err := filelock.Acquire(filepath.Join(j.Dir, "lifecycle.lock"))
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			unlock()
		}
	}()
	db, err := j.open(true)
	if err != nil {
		return nil, err
	}
	defer func() {
		if !keep {
			_ = db.Close()
		}
	}()
	old, _, err := query(db, req.ID, namespace)
	if err == nil {
		if old.Request != req {
			return nil, ErrConflict
		}
		return nil, ErrRecorded
	}
	if !errors.Is(err, ErrUnknown) {
		return nil, err
	}
	var unfinished int
	if err := db.QueryRow("SELECT count(*) FROM operations WHERE active=1").Scan(&unfinished); err != nil {
		return nil, err
	}
	if unfinished != 0 {
		return nil, ErrPending
	}
	if err := checkResourceAdmission(db, namespace); err != nil {
		return nil, err
	}
	record := Record{Request: req, NamespaceSHA256: namespace, Outcome: Pending, Effects: []Effect{}}
	raw, _ := json.Marshal(record)
	if _, err := db.Exec("INSERT INTO operations(id,record,revision) VALUES(?,?,1)", req.ID, raw); err != nil {
		return nil, err
	}
	keep = true
	return &Transaction{db: db, unlock: unlock, record: record, namespace: namespace, last: raw, revision: 1}, nil
}

// Query never creates a database, profile, process or execution authority.
func (j Journal) Query(id string) (Record, error) {
	if !idPattern.MatchString(id) {
		return Record{}, ErrInvalid
	}
	db, err := j.open(false)
	if os.IsNotExist(err) {
		return Record{}, ErrUnknown
	}
	if err != nil {
		return Record{}, err
	}
	defer db.Close()
	namespace, err := j.NamespaceID()
	if err != nil {
		return Record{}, err
	}
	r, _, err := query(db, id, namespace)
	return r, err
}

type rowQueryer interface{ QueryRow(string, ...any) *sql.Row }

func query(db rowQueryer, id, namespace string) (Record, []byte, error) {
	var raw, resolution []byte
	var active int
	var revision int64
	err := db.QueryRow("SELECT record,resolution,revision,active FROM operations WHERE id=? AND length(record)<=?", id, maxRecordBytes).Scan(&raw, &resolution, &revision, &active)
	if errors.Is(err, sql.ErrNoRows) {
		var count int
		if e := db.QueryRow("SELECT count(*) FROM operations WHERE id=?", id).Scan(&count); e != nil {
			return Record{}, nil, e
		}
		if count != 0 {
			return Record{}, nil, ErrInvalid
		}
		return Record{}, nil, ErrUnknown
	}
	if err != nil {
		return Record{}, nil, err
	}
	var r Record
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&r) != nil || d.Decode(new(any)) != io.EOF || !r.valid() || r.NamespaceSHA256 != namespace || r.Request.ID != id || (active != 0 && active != 1) {
		return Record{}, nil, ErrInvalid
	}
	expectedActive := r.active()
	if len(resolution) > 0 {
		var resolved Resolution
		rd := json.NewDecoder(bytes.NewReader(resolution))
		rd.DisallowUnknownFields()
		if len(resolution) > maxRecordBytes || rd.Decode(&resolved) != nil || rd.Decode(new(any)) != io.EOF || !resolved.valid() || resolved.OriginalOperationID != id || resolved.OriginalNamespaceSHA != namespace || resolved.OriginalRecordSHA256 != digestRecord(raw) || resolved.OriginalRevision != revision-1 {
			return Record{}, nil, ErrInvalid
		}
		expectedActive = false
	}
	if expectedActive != (active == 1) {
		return Record{}, nil, ErrInvalid
	}
	return r, raw, nil
}

func (j Journal) open(create bool) (*sql.DB, error) {
	return j.openMode(create, create)
}

// openExistingWritable opens an already-created journal for an atomic
// resolution append without creating a profile or database as a side effect.
func (j Journal) openExistingWritable() (*sql.DB, error) {
	return j.openMode(false, true)
}

func (j Journal) openMode(create, writable bool) (*sql.DB, error) {
	namespace, err := j.NamespaceID()
	if err != nil {
		return nil, err
	}
	path, err := filepath.Abs(filepath.Join(j.Dir, "operations.db"))
	if err != nil {
		return nil, err
	}
	fresh := false
	if create {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			fresh = true
			err = f.Close()
		}
		if err != nil && !os.IsExist(err) {
			return nil, err
		}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, ErrInvalid
	}
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	mode := "ro"
	if writable {
		mode = "rw"
	}
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=" + mode + "&_pragma=busy_timeout%3D100&_pragma=synchronous%3DFULL"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if fresh {
		err = initializeJournal(db, namespace)
	}
	var version int
	if err == nil {
		err = db.QueryRow("PRAGMA user_version").Scan(&version)
	}
	if err == nil && version != 8 {
		err = ErrInvalid
	}
	if err == nil {
		err = validateJournalNamespace(db, namespace)
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
