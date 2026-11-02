package filelock

import (
	"path/filepath"
	"testing"
)

// A live lock excludes another caller; closing it permits recovery.
func TestAcquire_should_excludeConcurrentOwner_andRelease(t *testing.T) {
	p := filepath.Join(t.TempDir(), "lock")
	done, err := Acquire(p)
	if err != nil {
		t.Fatal(err)
	}
	if other, err := Acquire(p); err == nil {
		other()
		done()
		t.Fatal("second owner acquired lock")
	}
	done()
	next, err := Acquire(p)
	if err != nil {
		t.Fatal(err)
	}
	next()
}
