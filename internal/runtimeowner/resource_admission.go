package runtimeowner

import (
	"database/sql"
	"errors"
	"os"
)

// An inactive operation can still have unknown or malformed resource evidence.
// Bootstrap observes the same gate as Admit, without creating a missing journal.
// WithBootstrap holds lifecycle.lock across this check and the launch callback.
func (j Journal) refuseBootstrapResources() error {
	db, err := j.open(false)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer db.Close()
	namespace, err := j.NamespaceID()
	if err != nil {
		return err
	}
	return checkResourceAdmission(db, namespace)
}

// checkResourceAdmission runs under the lifecycle writer lock. Terminal outcome
// and resolved active flags do not supersede durable resource obligations. Page
// through every represented operation ID one at a time, closing each query before the
// bounded per-operation resource read (the database has one connection).
func checkResourceAdmission(db *sql.DB, namespace string) error {
	after := ""
	for {
		var id string
		err := db.QueryRow(`SELECT operation_id FROM resource_obligations
 WHERE (? OR operation_id>?)
 ORDER BY operation_id LIMIT 1`, after == "", after).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return errors.Join(ErrPending, err)
		}
		record, _, err := query(db, id, namespace)
		if err != nil {
			return errors.Join(ErrPending, err)
		}
		var revision int64
		if err := db.QueryRow("SELECT revision FROM operations WHERE id=?", id).Scan(&revision); err != nil {
			return errors.Join(ErrPending, err)
		}
		resources, err := readResourceRows(db, record, revision)
		if err != nil {
			return errors.Join(ErrPending, err)
		}
		if resourcesUnsettled(resources) {
			return ErrPending
		}
		after = id
	}
}
