package runtimeowner

import (
	"errors"
	"testing"
)

// Admission cannot slip between an idle observation and the bootstrap action.
// Both paths use the same OS lock, retained across the actual callback.
func TestBootstrapGateExcludesAdmissionUntilActionReturns(t *testing.T) {
	j := journal(t)
	called := false
	if err := j.WithBootstrap(func() error {
		called = true
		if tx, err := j.Admit(request("1")); err == nil {
			tx.Close()
			t.Fatal("admitted inside bootstrap critical section")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("idle bootstrap not invoked")
	}
	tx, err := j.Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	tx.Close()
	called = false
	if err := j.WithBootstrap(func() error { called = true; return nil }); !errors.Is(err, ErrPending) || called {
		t.Fatal("unfinished admission bypassed", called, err)
	}
}
