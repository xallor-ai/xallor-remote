package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/accesscode"
	"modernc.org/sqlite"
)

var codeCommitDriverID atomic.Uint64
var errCommittedCodeReply = errors.New("fixture actual commit completed but acknowledgement lost")

type codeCommitDriver struct {
	base    driver.Driver
	failed  atomic.Bool
	commits atomic.Int32
}

func (d *codeCommitDriver) Open(name string) (driver.Conn, error) {
	c, err := d.base.Open(name)
	if err != nil {
		return nil, err
	}
	return &codeCommitConn{Conn: c, owner: d}, nil
}

type codeCommitConn struct {
	driver.Conn
	owner *codeCommitDriver
}

func (c *codeCommitConn) Begin() (driver.Tx, error) {
	tx, err := c.Conn.Begin()
	if err != nil {
		return nil, err
	}
	return &codeCommitTx{Tx: tx, owner: c.owner}, nil
}
func (c *codeCommitConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if inner, ok := c.Conn.(driver.ConnBeginTx); ok {
		tx, err := inner.BeginTx(ctx, opts)
		if err != nil {
			return nil, err
		}
		return &codeCommitTx{Tx: tx, owner: c.owner}, nil
	}
	if opts.Isolation != 0 || opts.ReadOnly {
		return nil, errors.New("fixture unsupported transaction options")
	}
	return c.Begin()
}

type codeCommitTx struct {
	driver.Tx
	owner *codeCommitDriver
}

func (t *codeCommitTx) Commit() error {
	if err := t.Tx.Commit(); err != nil {
		return err
	}
	t.owner.commits.Add(1)
	if t.owner.failed.CompareAndSwap(false, true) {
		return errCommittedCodeReply
	}
	return nil
}

// The real SQLite COMMIT succeeds before the driver reports failure. The
// production transaction loop must return that error once, not replay rotation;
// a separate ordinary connection can read the durable receipt afterwards.
func TestCodeManagement_shouldNotReplay_whenActualCommitReportsError(t *testing.T) {
	dir := t.TempDir()
	s := codeManagementFixtureAt(t, dir)
	wrapped := &codeCommitDriver{base: &sqlite.Driver{}}
	name := fmt.Sprintf("code-commit-fault-%d", codeCommitDriverID.Add(1))
	sql.Register(name, wrapped)
	path := filepath.ToSlash(filepath.Join(dir, "relay.db"))
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: "_pragma=foreign_keys%3D1&_pragma=busy_timeout%3D50"}).String()
	db, err := sql.Open(name, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	faultStore := &SQLStore{db: db}
	ctx := context.Background()
	calls := 0
	err = faultStore.WriteAccessCode(ctx, func(tx accesscode.Transaction) error {
		calls++
		_, err := tx.RotateCode(ctx, "resume-code", "replacement", strings.Repeat("b", 64), resumeStorageNow)
		return err
	})
	if !errors.Is(err, errCommittedCodeReply) || calls != 1 || wrapped.commits.Load() != 1 {
		t.Fatalf("unknown commit replayed/masked: calls=%d commits=%d err=%v", calls, wrapped.commits.Load(), err)
	}
	if err := s.ReadAccessCode(ctx, func(r accesscode.Reader) error {
		receipt, err := r.Rotation(ctx, "resume-code")
		if err != nil {
			return err
		}
		code, err := r.Code(ctx, receipt.ReplacementCodeID)
		if err != nil {
			return err
		}
		if code.ID != "replacement" || code.State != accesscode.StateActive || code.Generation != 2 {
			t.Error("actual committed receipt missing")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
