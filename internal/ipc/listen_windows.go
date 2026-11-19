//go:build windows

package ipc

import (
	"fmt"
	"net"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

func listen(path string) (net.Listener, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("get IPC listener process user: %w", err)
	}
	// Protect the DACL from inheritance and grant only the process TokenUser.
	// A nil config uses Windows' broader default named-pipe ACL. Token lookup
	// and SDDL conversion errors must fail creation without falling back to it.
	return winio.ListenPipe(path, &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;FA;;;" + user.User.Sid.String() + ")",
	})
}

func dial(path string, timeout time.Duration) (net.Conn, error) {
	return winio.DialPipe(path, &timeout)
}
