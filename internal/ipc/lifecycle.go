package ipc

import (
	"net"
	"path/filepath"
	"runtime"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/appdata"
)

func lifecyclePath() (string, error) {
	base, err := appdata.IPCPath()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		return base + "-lifecycle", nil
	}
	name := "txn.sock"
	if filepath.Base(base) != "ipc.sock" {
		name = "xallor-txn.sock"
	}
	return filepath.Join(filepath.Dir(base), name), nil
}

// ListenLifecycle is reserved for the admitted transaction holder. Holding the
// lifecycle lock excludes another owner before Unix stale-socket replacement.
func ListenLifecycle() (net.Listener, error) {
	path, err := lifecyclePath()
	if err != nil {
		return nil, err
	}
	return listen(path)
}
func DialLifecycle(timeout time.Duration) (net.Conn, error) {
	path, err := lifecyclePath()
	if err != nil {
		return nil, err
	}
	return dial(path, timeout)
}
