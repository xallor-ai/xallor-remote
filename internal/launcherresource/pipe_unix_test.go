//go:build !windows

package launcherresource

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

// A permanently invalid descriptor exercises the actual Reader error boundary
// without closing a real descriptor that the OS might recycle concurrently.
func TestPipeReader_should_preserveReaderContract_whenNativeReadFails(t *testing.T) {
	reader := &PipeReader{ctx: context.Background(), fd: ^uintptr(0)}
	n, err := reader.Read(make([]byte, 1))
	if n != 0 || !errors.Is(err, unix.EBADF) {
		t.Fatalf("reader returned n=%d err=%v; want 0, EBADF", n, err)
	}
}
