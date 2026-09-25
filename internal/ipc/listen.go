package ipc

import (
	"net"
	"os"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/appdata"
)

func Listen() (net.Listener, error) {
	path, err := appdata.IPCPath()
	if err != nil {
		return nil, err
	}
	return listen(path)
}

func Dial(timeout time.Duration) (net.Conn, error) {
	path, err := appdata.IPCPath()
	if err != nil {
		return nil, err
	}
	return dial(path, timeout)
}

func MustRemoveStale() {
	path, err := appdata.IPCPath()
	if err != nil {
		return
	}
	_ = os.Remove(path)
}
