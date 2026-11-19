package runtimeowner

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
)

// NamespaceID binds observations to the configured absolute journal location.
// It does not create or resolve directories, follow aliases, or grant execution
// authority. Relocation requires explicit migration, never automatic rebinding.
func (j Journal) NamespaceID() (string, error) {
	if !filepath.IsAbs(j.Dir) {
		return "", ErrInvalid
	}
	path, err := filepath.Abs(j.Dir)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte("xallor-runtime-lifecycle-v2\x00" + filepath.ToSlash(filepath.Clean(path))))
	return hex.EncodeToString(digest[:]), nil
}

func initializeJournal(db *sql.DB, namespace string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(fmt.Sprintf(`CREATE TABLE journal_identity(
 singleton INTEGER PRIMARY KEY CHECK(singleton=1),
 namespace_sha256 TEXT NOT NULL CHECK(length(namespace_sha256)=64)
);
CREATE TABLE operations(
 id TEXT PRIMARY KEY,
 record BLOB NOT NULL CHECK(length(record)<=%d AND json_valid(record))
  CHECK(json_extract(record,'$.outcome') IS NOT NULL AND json_extract(record,'$.outcome') IN ('pending','uncertain','succeeded','failed','rejected')),
 revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0),
 resolution BLOB CHECK(resolution IS NULL OR (length(resolution)<=%d AND json_valid(resolution))),
 active INTEGER GENERATED ALWAYS AS (CASE WHEN json_extract(record,'$.outcome') IN ('pending','uncertain') AND (resolution IS NULL OR json_extract(resolution,'$.disposition') <> 'resolved') THEN 1 ELSE 0 END) STORED
);
CREATE UNIQUE INDEX one_active_operation ON operations(active) WHERE active=1;
CREATE TABLE resource_obligations(
 operation_id TEXT NOT NULL,
 namespace_sha256 TEXT NOT NULL CHECK(length(namespace_sha256)=64),
 resource_id TEXT NOT NULL CHECK(length(resource_id)=32),
 phase TEXT NOT NULL CHECK(length(phase)<=256),
 kind TEXT NOT NULL CHECK(length(kind)<=256),
 status TEXT NOT NULL CHECK(status IN ('allocated','retained','closed','unknown')),
 cause TEXT NOT NULL CHECK(length(cause)<=256),
 attempt_id TEXT NOT NULL CHECK(length(attempt_id)=32),
 generation INTEGER NOT NULL CHECK(generation>0),
 activation_id TEXT NOT NULL CHECK(length(activation_id)<=256),
 launch_id TEXT NOT NULL CHECK(length(launch_id)<=256),
 image_sha256 TEXT NOT NULL CHECK(length(image_sha256)=0 OR length(image_sha256)=64),
 source_snapshot_sha256 TEXT NOT NULL CHECK(length(source_snapshot_sha256)=0 OR length(source_snapshot_sha256)=64),
 observer_scope TEXT NOT NULL CHECK(length(observer_scope)>0 AND length(observer_scope)<=256),
 retention TEXT NOT NULL CHECK(retention IN ('release_required','retain_selected')),
 revision INTEGER NOT NULL CHECK(revision>0),
 selection_receipt BLOB CHECK(selection_receipt IS NULL OR (length(selection_receipt)<=4096 AND json_valid(selection_receipt))),
 PRIMARY KEY(operation_id,resource_id),
 FOREIGN KEY(operation_id) REFERENCES operations(id)
);
CREATE INDEX resource_obligations_operation ON resource_obligations(operation_id,revision);
PRAGMA user_version=8;`, maxRecordBytes, maxRecordBytes))
	if err != nil {
		return err
	}
	if _, err := tx.Exec("INSERT INTO journal_identity(singleton,namespace_sha256) VALUES(1,?)", namespace); err != nil {
		return err
	}
	return tx.Commit()
}

func validateJournalNamespace(db *sql.DB, expected string) error {
	var count int
	var actual string
	if err := db.QueryRow("SELECT count(*) FROM journal_identity").Scan(&count); err != nil || count != 1 {
		return errors.Join(ErrInvalid, err)
	}
	if err := db.QueryRow("SELECT namespace_sha256 FROM journal_identity WHERE singleton=1").Scan(&actual); err != nil {
		return errors.Join(ErrInvalid, err)
	}
	if !shaPattern.MatchString(actual) || actual != expected {
		return ErrInvalid
	}
	return nil
}
