package ipc

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// Handoff authorization needs the actual endpoint process, never a PID claimed
// inside a response. This exercises the native pipe/socket descriptor.
func TestPeerProcessIDUsesKernelEndpointIdentity(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Skip("platform refuses peer identity")
	}
	path := filepath.Join(t.TempDir(), "owner.sock")
	if runtime.GOOS == "windows" {
		path = fmt.Sprintf(`\\.\pipe\XallorPeerTest-%d-%d`, os.Getpid(), time.Now().UnixNano())
	}
	ln, err := listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err == nil {
			accepted <- c
		}
	}()
	conn, err := dial(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	select {
	case server := <-accepted:
		defer server.Close()
	case <-time.After(time.Second):
		t.Fatal("accept timed out")
	}
	pid, err := PeerProcessID(conn)
	if err != nil || pid != os.Getpid() {
		t.Fatal("endpoint identity differs", pid, err)
	}
}

// Unsupported transports cannot manufacture a process identity from payloads.
func TestPeerProcessIDRejectsNonNativeConnection(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	if _, err := PeerProcessID(a); err == nil {
		t.Fatal("memory transport accepted as an OS peer")
	}
}
