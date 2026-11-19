package launcherresource

import (
	"context"
	"os"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/nativepipe"
)

// PipeReader performs only nonblocking native reads. Unlike closing a blocking
// read from another goroutine, cancellation cannot depend on descendant EOF.
// Its owner must close the file after the last Read has returned.
type PipeReader struct {
	ctx  context.Context
	fd   uintptr
	file *os.File
}

func NewPipeReader(ctx context.Context, file *os.File) (*PipeReader, error) {
	fd := file.Fd() // Capture once: Fd may restore blocking mode on Unix.
	if err := nativepipe.Prepare(fd); err != nil {
		return nil, err
	}
	return &PipeReader{ctx: ctx, fd: fd, file: file}, nil
}
func (r *PipeReader) Read(buffer []byte) (int, error) {
	if len(buffer) == 0 {
		return 0, nil
	}
	for {
		// Try available bytes before checking cancellation, preserving a complete
		// bounded acknowledgement already buffered when the child exits.
		n, err := nativepipe.ReadAvailable(r.fd, buffer)
		if n > 0 || err != nil {
			return n, err
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-r.ctx.Done():
			timer.Stop()
			return 0, r.ctx.Err()
		case <-timer.C:
		}
	}
}
