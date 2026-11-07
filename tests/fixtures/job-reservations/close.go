package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/invocation"
	"github.com/xallor-ai/xallor-remote/internal/job"
	"github.com/xallor-ai/xallor-remote/internal/process"
)

// Keep a real OS child alive through ledger handoff. The replacement retains
// uncertain debt; the old owner cannot erase it or start any later command.
func verifyClose(dir string) {
	l := job.New(dir)
	defer l.Close()
	scope := invocation.Scope{Authority: strings.Repeat("a", 32), DeviceUID: strings.Repeat("b", 32), GrantID: strings.Repeat("c", 32)}
	o, err := l.ReserveExec(scope, "before-close")
	if err != nil {
		panic(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	marker := filepath.Join(dir, "started")
	child, err := o.Start(ctx, exec.Command(os.Args[0], "wait", marker))
	if err != nil {
		panic(err)
	}
	defer func() { cancel(); _ = child.Wait() }()
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if ctx.Err() != nil {
			panic("child did not publish startup")
		}
		time.Sleep(10 * time.Millisecond)
	}
	l.Close()
	fresh := job.New(dir)
	defer fresh.Close()
	checkDebt := func() {
		rows, err := fresh.Snapshot()
		if err != nil || len(rows) != 1 || rows[0].ID != o.ID() || rows[0].State != "uncertain" {
			panic("ledger handoff lost occupied job")
		}
	}
	checkDebt()
	select {
	case <-child.Done():
		panic("fixture child ended before live handoff check")
	default:
	}
	if err := child.Stop(); err != nil {
		panic(err)
	}
	if err := child.Wait(); errors.Is(err, process.ErrCleanup) {
		panic(err)
	}
	if !errors.Is(o.Finish(), job.ErrUnavailable) {
		panic("old owner wrote release after ledger handoff")
	}
	denied := filepath.Join(dir, "denied")
	cmd := exec.Command(os.Args[0], "done", denied)
	if later, err := o.Start(ctx, cmd); later != nil || !errors.Is(err, job.ErrUnavailable) || cmd.Process != nil {
		panic("old owner launched after ledger handoff")
	}
	if _, err := os.Stat(denied); !os.IsNotExist(err) {
		panic("denied command executed")
	}
	checkDebt()
	if _, err := fresh.ReserveExec(scope, "before-close"); !errors.Is(err, job.ErrUncertain) {
		panic("unknown job was replayed")
	}
	newOwner, err := fresh.ReserveExec(scope, "new-job")
	if err != nil {
		panic(err)
	}
	next, err := newOwner.Start(ctx, exec.Command(os.Args[0], "done", filepath.Join(dir, "new")))
	if err != nil {
		panic(err)
	}
	if next.Wait() != nil || newOwner.Finish() != nil {
		panic("replacement ledger could not complete new job")
	}
	checkDebt()
}
