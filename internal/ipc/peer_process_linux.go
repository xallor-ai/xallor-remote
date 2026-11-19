//go:build linux

package ipc

import (
	"errors"
	"net"

	"golang.org/x/sys/unix"
)

// PeerProcessID observes kernel credentials on an established Unix connection.
func PeerProcessID(conn net.Conn) (int, error) {
	socket, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, errors.New("IPC connection is not a native Unix socket")
	}
	raw, err := socket.SyscallConn()
	if err != nil {
		return 0, err
	}
	var cred *unix.Ucred
	var observeErr error
	if err := raw.Control(func(fd uintptr) { cred, observeErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) }); err != nil {
		return 0, err
	}
	if observeErr != nil {
		return 0, observeErr
	}
	if cred == nil || cred.Pid <= 0 {
		return 0, errors.New("IPC peer process is unavailable")
	}
	return int(cred.Pid), nil
}
