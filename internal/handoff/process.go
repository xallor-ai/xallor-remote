package handoff

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"time"

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
	readDone := make(chan error, 1)
	go func() {
		_, err := io.Copy(b, reader)
		readDone <- err
	}()
	waitErr := child.Wait()
	readErr := finishProbeRead(ctx, reader, readDone)
	if err := errors.Join(waitErr, readErr, ctx.Err()); err != nil {
		return "", err
	}
	if b.overflow {
		return "", errors.New("output_limit")
	}
	return string(b.data), nil
}

// Process exit does not mean its pipe has been drained. Wait for EOF first;
// only close early for cancellation or an escaped inherited writer.
func finishProbeRead(ctx context.Context, reader io.ReadCloser, done <-chan error) error {
	defer reader.Close()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = reader.Close()
		<-done
		return ctx.Err()
	case <-timer.C:
		_ = reader.Close()
		<-done
		return exec.ErrWaitDelay
	}
}
