package handoff

import (
	"context"
	"errors"
	"os/exec"
	"time"
)

// start owns process lifetime independently of the short submit/query transport.
// Runtime shutdown or request timeout cancels the owned process tree.
func (m *Manager) start(ctx context.Context, cmd *exec.Cmd) (func(), func() error, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if m.hooks.Prepare != nil {
		m.hooks.Prepare(cmd)
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	kill := func() { _ = cmd.Process.Kill() }
	closeJob := func() {}
	if m.hooks.Watch != nil {
		kill, closeJob = m.hooks.Watch(cmd.Process)
	}
	done := make(chan struct{})
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		select {
		case <-ctx.Done():
			kill()
		case <-done:
		}
	}()
	wait := func() error { err := cmd.Wait(); close(done); <-watchDone; closeJob(); return err }
	return kill, wait, nil
}

type boundedOutput struct {
	data     []byte
	overflow bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	left := 64*1024 - len(b.data)
	if n > left {
		b.overflow = true
		p = p[:left]
	}
	b.data = append(b.data, p...)
	return n, nil
}
func (m *Manager) output(ctx context.Context, cmd *exec.Cmd) (string, error) {
	b := &boundedOutput{}
	cmd.Stdout = b
	cmd.WaitDelay = time.Second
	_, wait, err := m.start(ctx, cmd)
	if err != nil {
		return "", err
	}
	if err = wait(); err != nil {
		return "", err
	}
	if b.overflow {
		return "", errors.New("output_limit")
	}
	return string(b.data), nil
}
