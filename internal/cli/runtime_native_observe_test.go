package cli

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/runtimeowner"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// Observation only: callers must never put lifecycle mutation in this callback.
// The supplied deadline spans all reads rather than resetting on each attempt.
func waitNativeObservation(ctx context.Context, observe func() (bool, error)) error {
	var last error
	for {
		if err := ctx.Err(); err != nil {
			return errors.Join(err, last)
		}
		ready, err := observe()
		last = err
		if err != nil && !nativeReadBusy(err) {
			return err
		}
		if err := ctx.Err(); err != nil {
			return errors.Join(err, last)
		}
		if ready && err == nil {
			return nil
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return errors.Join(ctx.Err(), last)
		}
	}
}

func nativeReadBusy(err error) bool {
	var failure *sqlite.Error
	return errors.As(err, &failure) && failure.Code()&0xff == sqlite3.SQLITE_BUSY
}

// A real exclusive writer makes the ordinary read-only Journal.Query busy.
// Releasing that writer must allow observation of the same immutable request,
// without resubmitting its lifecycle command or enlarging the total deadline.
func TestNativeObservation_shouldWaitForReadBusy_withoutReplayingOperation(t *testing.T) {
	assertNativeJournalContention(t, false)
}

// A busy reader must respect cancellation of the one original observation,
// rather than resetting a timeout or treating uncertain reads as success.
func TestNativeObservation_shouldHonorCancellation_whileWriterRemainsLocked(t *testing.T) {
	assertNativeJournalContention(t, true)
}

func assertNativeJournalContention(t *testing.T, cancelWaiter bool) {
	t.Helper()
	j := runtimeowner.Journal{Dir: filepath.Join(t.TempDir(), "lifecycle")}
	command, err := runtimeowner.NewCommand("rollback", "")
	if err != nil {
		t.Fatal(err)
	}
	req, err := command.Request()
	if err != nil {
		t.Fatal(err)
	}
	tx, err := j.Admit(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Finish(runtimeowner.Rejected); err != nil {
		tx.Close()
		t.Fatal(err)
	}
	if err := tx.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.ToSlash(filepath.Join(j.Dir, "operations.db"))
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=rw"}).String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	writer, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err := writer.ExecContext(ctx, "BEGIN EXCLUSIVE"); err != nil {
		t.Fatal(err)
	}
	defer writer.ExecContext(context.Background(), "ROLLBACK")
	busy := make(chan struct{}, 1)
	done := make(chan error, 1)
	var record runtimeowner.Record
	reads := 0
	go func() {
		done <- waitNativeObservation(ctx, func() (bool, error) {
			reads++
			var err error
			record, err = j.Query(req.ID)
			if nativeReadBusy(err) {
				select {
				case busy <- struct{}{}:
				default:
				}
			}
			return err == nil && record.Outcome != runtimeowner.Pending, err
		})
	}()
	select {
	case <-busy:
	case <-ctx.Done():
		<-done
		t.Fatal("writer did not cause read busy")
	}
	if cancelWaiter {
		cancel()
		err := <-done
		if !errors.Is(err, context.Canceled) || !nativeReadBusy(err) {
			t.Fatal("busy observation ignored cancellation", err)
		}
		return
	}
	if _, err := writer.ExecContext(ctx, "ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal("transient read ended observation", err)
	}
	if reads < 2 || record.Request != req || record.Outcome != runtimeowner.Rejected {
		t.Fatal("did not observe original operation after contention")
	}
}

// Permission/corruption/other errors are not converted to retryable reads.
func TestNativeObservation_shouldReturnPermanentError_withoutAnotherRead(t *testing.T) {
	reads := 0
	err := waitNativeObservation(context.Background(), func() (bool, error) { reads++; return false, runtimeowner.ErrInvalid })
	if !errors.Is(err, runtimeowner.ErrInvalid) || reads != 1 {
		t.Fatal("permanent error was hidden or retried")
	}
}
