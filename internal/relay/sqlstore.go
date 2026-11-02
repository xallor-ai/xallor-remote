package relay

import (
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

type SQLStore struct {
	db *sql.DB
}

func OpenSQLStore(dir string) (*SQLStore, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	filename, err := filepath.Abs(filepath.Join(dir, "relay.db"))
	if err != nil {
		return nil, err
	}
	// Apply the short busy wait to every pooled connection, including replacements
	// after cancellation. A long native busy handler delays context cancellation.
	uriPath := filepath.ToSlash(filename)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	dsn := (&url.URL{Scheme: "file", Path: uriPath, RawQuery: "_pragma=busy_timeout%3D50&_pragma=foreign_keys%3D1"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &SQLStore{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *SQLStore) Close() error {
	return s.db.Close()
}
