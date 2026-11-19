//go:build windows

package ipc

import (
	"errors"
	"net"
	"runtime"

	"golang.org/x/sys/windows"
)

// PeerProcessID observes the server of a client-side native pipe connection.
// The caller must serialize connection Close with this descriptor observation.
func PeerProcessID(conn net.Conn) (int, error) {
	pipe, ok := conn.(interface{ Fd() uintptr })
	if !ok {
		return 0, errors.New("IPC connection has no native pipe descriptor")
	}
	var pid uint32
	err := windows.GetNamedPipeServerProcessId(windows.Handle(pipe.Fd()), &pid)
	runtime.KeepAlive(conn)
	if err != nil {
		return 0, err
	}
	if pid == 0 {
		return 0, errors.New("IPC server process is unavailable")
	}
	return int(pid), nil
}
