package runtimeowner

import (
	"database/sql"
	"errors"
	"os"
)

// Active observes one consistent read-only snapshot. Missing records are not
// repaired or created, and an invalid/unreadable journal is never treated as idle.
func (j Journal) Active() (Record, error) {
	db, err := j.open(false)
	if os.IsNotExist(err) {
		return Record{}, ErrUnknown
	}
	if err != nil {
		return Record{}, err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return Record{}, err
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRow("SELECT id FROM operations WHERE active=1").Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrUnknown
	}
	if err != nil {
		return Record{}, err
	}
	namespace, err := j.NamespaceID()
	if err != nil {
		return Record{}, err
	}
	record, _, err := query(tx, id, namespace)
	if err == nil && !record.active() {
		err = ErrInvalid
	}
	return record, err
}
