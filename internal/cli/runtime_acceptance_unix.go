//go:build !windows

package cli

import (
	"errors"
	"os"
	"runtime"

	"golang.org/x/sys/unix"
)

// transactionAcceptanceWriter borrows the inherited acknowledgement file. A
// failed delivery must not terminate admitted execution: os.File.Write raises
// SIGPIPE for stdout/stderr EPIPE, whereas this scoped syscall returns the error.
// No signal policy or descriptor ownership is changed for other worker code.
type transactionAcceptanceWriter struct{ file *os.File }

func (w transactionAcceptanceWriter) Write(p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		n, err := unix.Write(int(w.file.Fd()), p)
		runtime.KeepAlive(w.file)
		if n > 0 {
			total += n
			p = p[n:]
		}
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return total, err
		}
	}
	return total, nil
}
