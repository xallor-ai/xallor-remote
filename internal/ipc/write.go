package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func (c *Conn) WriteFrame(frame Frame) error { return c.writeFrame(context.Background(), frame, false) }

// WriteFrameContext bounds both serialization wait and transport delivery.
// Deadline setup, cancellation callback completion and clearing share one write
// gate, so another frame can never overwrite or inherit this frame's deadline.
func (c *Conn) WriteFrameContext(ctx context.Context, frame Frame) error {
	if ctx == nil {
		return errors.New("IPC write context required")
	}
	return c.writeFrame(ctx, frame, true)
}

func (c *Conn) writeFrame(ctx context.Context, frame Frame, managedDeadline bool) error {
	data, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	if len(data) > protocol.MaxIPCMessageBytes {
		_ = c.Close()
		return ErrMessageTooLarge
	}
	return c.write(ctx, append(data, '\n'), managedDeadline)
}

func (c *Conn) writeBytes(data []byte) error { return c.write(context.Background(), data, false) }

func (c *Conn) write(ctx context.Context, data []byte, managedDeadline bool) (result error) {
	select {
	case c.writes <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-c.writes }()
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.closed.Load() {
		return net.ErrClosed
	}
	if managedDeadline {
		deadline, _ := ctx.Deadline()
		if err := c.c.SetWriteDeadline(deadline); err != nil {
			_ = c.Close()
			return err
		}
		completed := make(chan error, 1)
		stop := context.AfterFunc(ctx, func() {
			err := c.c.SetWriteDeadline(time.Now())
			if err != nil {
				err = errors.Join(err, c.Close())
			}
			completed <- err
		})
		defer func() {
			if !stop() {
				result = errors.Join(result, <-completed)
			}
			if err := c.c.SetWriteDeadline(time.Time{}); err != nil {
				result = errors.Join(result, err)
				_ = c.Close()
			}
		}()
	}
	written, err := c.c.Write(data)
	if err == nil && written != len(data) {
		err = io.ErrShortWrite
	}
	// Never append a new JSON frame to a failed partial frame.
	if err != nil && written > 0 {
		_ = c.Close()
	}
	return errors.Join(err, ctx.Err())
}
