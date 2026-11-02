package handoff

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"

	"github.com/xallor-ai/xallor-remote/internal/job"
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
func (m *Manager) output(ctx context.Context, cmd *exec.Cmd, owner *job.Owner) (string, error) {
	b := &boundedOutput{}
	reader, writer, err := os.Pipe()
	if err != nil {
		return "", err
	}
	cmd.Stdout = writer
	child, err := owner.Start(ctx, cmd)
	if err != nil {
		_ = reader.Close()
		_ = writer.Close()
		return "", err
	}
	_ = writer.Close()
	readDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(b, reader)
		close(readDone)
	}()
	stopRead := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = reader.Close()
		case <-stopRead:
		}
	}()
	waitErr := child.Wait()
	close(stopRead)
	// Child.Wait includes descendant cleanup. Closing the read end now releases
	// any malformed process that retained stdout after the supervised boundary.
	_ = reader.Close()
	<-readDone
	if waitErr != nil {
		return "", waitErr
	}
	if b.overflow {
		return "", errors.New("output_limit")
	}
	return string(b.data), nil
}
