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

// Exercise an actual Owner and OS children in an isolated profile. A rejected
// second process must never execute its marker-writing entry point.
func verifySequence(dir string) {
	if !filepath.IsAbs(dir) {
		panic("expected absolute test profile")
	}
	l := job.New(dir)
	defer l.Close()
	scope := invocation.Scope{Authority: strings.Repeat("a", 32), DeviceUID: strings.Repeat("b", 32), GrantID: strings.Repeat("c", 32)}
	o, err := l.ReserveHandoff(scope, "sequence")
	if err != nil {
		panic(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer func() { cancel(); _ = o.Finish() }()
	firstMarker := filepath.Join(dir, "first")
	first, err := o.Start(ctx, exec.Command(os.Args[0], "wait", firstMarker))
	if err != nil {
		panic(err)
	}
	for {
		if _, err := os.Stat(firstMarker); err == nil {
			break
		}
		if ctx.Err() != nil {
			panic("first process did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	deniedMarker := filepath.Join(dir, "denied")
	cmd := exec.Command(os.Args[0], "done", deniedMarker)
	if second, err := o.Start(ctx, cmd); second != nil || !errors.Is(err, job.ErrActive) || cmd.Process != nil {
		panic("parallel process started on one slot")
	}
	if _, err := os.Stat(deniedMarker); !os.IsNotExist(err) {
		panic("rejected process executed")
	}
	if err := first.Stop(); err != nil {
		panic(err)
	}
	if err := first.Wait(); errors.Is(err, process.ErrCleanup) {
		panic(err)
	}
	rows, err := l.Snapshot()
	if err != nil || len(rows) != 1 || rows[0].State != "occupied" {
		panic("job released between probes")
	}
	nextMarker := filepath.Join(dir, "next")
	next, err := o.Start(ctx, exec.Command(os.Args[0], "done", nextMarker))
	if err != nil {
		panic(err)
	}
	if err := next.Wait(); err != nil {
		panic(err)
	}
	if _, err := os.Stat(nextMarker); err != nil {
		panic(err)
	}
	if err := o.Finish(); err != nil {
		panic(err)
	}
	rows, err = l.Snapshot()
	if err != nil || len(rows) != 0 {
		panic("confirmed job not released")
	}
}
