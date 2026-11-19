package runtimeowner

import (
	"context"
	"errors"
	"io"
	"os"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/nativepipe"
	"github.com/xallor-ai/xallor-remote/internal/release"
)

// ReadHandoffPipe owns its input and uses nonblocking native reads, so context
// expiry never depends on EOF or another goroutine successfully closing a read.
func ReadHandoffPipe(ctx context.Context, input *os.File) (raw []byte, result error) {
	defer func() {
		if err := input.Close(); err != nil {
			result = errors.Join(result, release.ErrCleanupUncertain, err)
		}
	}()
	info, err := input.Stat()
	if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		return nil, ErrInvalid
	}
	// Capture once: os.File.Fd can restore blocking mode on Unix pipe files.
	fd := input.Fd()
	if err := nativepipe.Prepare(fd); err != nil {
		return nil, err
	}
	buffer := make([]byte, 4096)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, err := nativepipe.ReadAvailable(fd, buffer)
		if n > 0 {
			raw = append(raw, buffer[:n]...)
			if len(raw) > MaxHandoffBytes {
				return nil, ErrInvalid
			}
		}
		if errors.Is(err, io.EOF) {
			return raw, nil
		}
		if err != nil {
			return nil, err
		}
		if n == 0 {
			timer := time.NewTimer(10 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
	}
}
