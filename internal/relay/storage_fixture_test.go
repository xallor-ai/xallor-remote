package relay

import (
	"database/sql"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/storage"
)

// The side connection installs fault triggers in an owned temporary database.
// It is not the adapter's private pool; pool tests live in package storage.
type relaySQLFixture struct {
	*storage.SQLStore
	faults *sql.DB
}

func openIdentityTestStore(t *testing.T) *relaySQLFixture {
	t.Helper()
	dir := t.TempDir()
	s, err := storage.OpenSQLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	path := filepath.ToSlash(filepath.Join(dir, "relay.db"))
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: "_pragma=foreign_keys%3D1"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return &relaySQLFixture{SQLStore: s, faults: db}
}
