//go:build !windows && !linux

package ipc

import (
	"errors"
	"net"
)

func PeerProcessID(net.Conn) (int, error) {
	return 0, errors.New("IPC process identity is unsupported on this platform")
}
