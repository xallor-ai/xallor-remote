package handoff

import (
	"context"
	"errors"
	"os/exec"

	"github.com/xallor-ai/xallor-remote/internal/process"
)

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
	child, err := process.Start(ctx, cmd)
	if err != nil {
		return "", err
	}
	if err = child.Wait(); err != nil {
		return "", err
	}
	if b.overflow {
		return "", errors.New("output_limit")
	}
	return string(b.data), nil
}
