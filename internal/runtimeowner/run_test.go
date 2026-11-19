package runtimeowner

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/release"
)

type untouchedControl struct{ calls int }

func (c *untouchedControl) Running() (*buildinfo.Executable, error) { c.calls++; return nil, nil }
func (c *untouchedControl) PrepareStop(context.Context, release.StopRequest) (release.StopAttempt, error) {
	c.calls++
	return nil, errors.New("unexpected stop capture")
}
func (c *untouchedControl) Start(string, string) error { c.calls++; return nil }
func (c *untouchedControl) PrepareProbe(release.ProbeRequest) (release.ProbeAttempt, error) {
	c.calls++
	return nil, nil
}
func (c *untouchedControl) PrepareReady(release.ReadyRequest) (release.ReadyAttempt, error) {
	c.calls++
	return nil, nil
}

// The coordinator rejects a different command before source probing/control
// effects and cannot execute the accepted transaction a second time.
func TestRunRefusesChangedCommandBeforeEffects(t *testing.T) {
	command := Command{ID: strings.Repeat("1", 32), Kind: "install", Bundle: t.TempDir()}
	req, err := command.Request()
	if err != nil {
		t.Fatal(err)
	}
	j := journal(t)
	tx, err := j.Admit(req)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Close()
	command.Bundle = filepath.Join(command.Bundle, "changed")
	control := &untouchedControl{}
	if err := tx.Run(command, release.Store{Dir: t.TempDir()}, control); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if err := tx.Run(command, release.Store{Dir: t.TempDir()}, control); !errors.Is(err, ErrRecorded) {
		t.Fatal(err)
	}
	if control.calls != 0 {
		t.Fatal("mismatched intent reached process controls")
	}
	r, err := j.Query(req.ID)
	if err != nil || r.Outcome != Rejected {
		t.Fatal(r, err)
	}
}

// Source rejection is a completed rejected request; absent IPC does not permit
// a separate bootstrap while another admitted request remains unfinished.
func TestRunSourceRejectionAndBootstrapGuard(t *testing.T) {
	command := Command{ID: strings.Repeat("1", 32), Kind: "install", Bundle: t.TempDir()}
	req, _ := command.Request()
	j := journal(t)
	if err := j.RefuseBootstrap(); err != nil {
		t.Fatal(err)
	}
	tx, err := j.Admit(req)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Close()
	if err := j.RefuseBootstrap(); !errors.Is(err, ErrPending) {
		t.Fatal(err)
	}
	control := &untouchedControl{}
	if err := tx.Run(command, release.Store{Dir: t.TempDir()}, control); err == nil {
		t.Fatal("unsigned empty source accepted")
	}
	if control.calls != 0 {
		t.Fatal("source denial reached process controls")
	}
	if err := j.RefuseBootstrap(); err != nil {
		t.Fatal(err)
	}
	r, err := j.Query(req.ID)
	if err != nil || r.Outcome != Rejected {
		t.Fatal(r, err)
	}
}

// Invalid input arriving after another runner claimed the transaction must not
// complete or cancel that accepted operation while its backend is still active.
func TestRunInvalidSecondCommandCannotCompleteClaimedTransaction(t *testing.T) {
	j := journal(t)
	tx, err := j.Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Close()
	tx.running = true
	if err := tx.Run(Command{}, release.Store{Dir: t.TempDir()}, &untouchedControl{}); !errors.Is(err, ErrRecorded) {
		t.Fatal(err)
	}
	r, err := j.Query(request("1").ID)
	if err != nil || r.Outcome != Pending {
		t.Fatal("second command changed active outcome", r, err)
	}
}
