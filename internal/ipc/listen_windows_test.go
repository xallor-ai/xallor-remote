//go:build windows

package ipc

import (
	"net"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Both public listeners must apply the current process user's protected DACL
// to the actual kernel pipe, rather than inheriting the default pipe ACL.
func TestListen_should_allowOnlyCurrentUser_whenCreatingWindowsPipes(t *testing.T) {
	t.Setenv("XALLOR_REMOTE_DATA_DIR", t.TempDir())
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []struct {
		name   string
		listen func() (net.Listener, error)
		dial   func(time.Duration) (net.Conn, error)
	}{
		{"normal", Listen, Dial},
		{"lifecycle", ListenLifecycle, DialLifecycle},
	} {
		t.Run(endpoint.name, func(t *testing.T) {
			server, _ := connectedWindowsPipe(t, endpoint.listen, endpoint.dial)
			pipe, ok := server.(interface{ Fd() uintptr })
			if !ok {
				t.Fatal("accepted connection has no native pipe descriptor")
			}
			sd, err := windows.GetSecurityInfo(windows.Handle(pipe.Fd()), windows.SE_KERNEL_OBJECT, windows.DACL_SECURITY_INFORMATION)
			runtime.KeepAlive(server)
			if err != nil {
				t.Fatal(err)
			}
			control, _, err := sd.Control()
			if err != nil {
				t.Fatal(err)
			}
			if control&windows.SE_DACL_PROTECTED == 0 {
				t.Error("pipe DACL is not protected from inheritance")
			}
			acl, defaulted, err := sd.DACL()
			if err != nil || acl == nil {
				t.Fatalf("pipe must have a non-null DACL: %v", err)
			}
			if defaulted || acl.AceCount != 1 {
				t.Errorf("want one explicit current-user ACE; defaulted=%t count=%d", defaulted, acl.AceCount)
			}
			for i := uint32(0); i < uint32(acl.AceCount); i++ {
				var ace *windows.ACCESS_ALLOWED_ACE
				if err := windows.GetAce(acl, i, &ace); err != nil {
					t.Fatal(err)
				}
				if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
					t.Errorf("ACE %d has unexpected type %d", i, ace.Header.AceType)
					continue
				}
				sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
				if !sid.Equals(user.User.Sid) {
					t.Errorf("ACE %d grants access to a non-current-user SID", i)
				}
				const fileAllAccess = 0x1f01ff // FILE_ALL_ACCESS after kernel generic-rights mapping.
				if ace.Header.AceFlags != 0 || ace.Mask != fileAllAccess {
					t.Errorf("ACE %d must grant explicit full access without inheritance; flags=%d mask=%x", i, ace.Header.AceFlags, ace.Mask)
				}
			}
			runtime.KeepAlive(sd)
		})
	}
}

// Restricting the pipe DACL must preserve same-user NDJSON request/reply access.
func TestListen_should_completeRPC_whenClientIsCurrentWindowsUser(t *testing.T) {
	t.Setenv("XALLOR_REMOTE_DATA_DIR", t.TempDir())
	server, client := connectedWindowsPipe(t, Listen, Dial)
	serverCodec, clientCodec := NewConn(server), NewConn(client)
	completed := make(chan error, 1)
	go func() {
		req, err := serverCodec.ReadRequest()
		if err == nil {
			err = serverCodec.WriteFrame(OK(req.ID, req.Method))
		}
		completed <- err
	}()
	if err := Call(clientCodec, "current-user", "status", nil); err != nil {
		t.Fatal(err)
	}
	frame, err := clientCodec.ReadFrame()
	if err != nil || frame.ID != "current-user" || frame.OK == nil || !*frame.OK || string(frame.Result) != `"status"` {
		t.Fatalf("same-user RPC failed: frame=%+v err=%v", frame, err)
	}
	if err := <-completed; err != nil {
		t.Fatal(err)
	}
}

func connectedWindowsPipe(t *testing.T, listenEndpoint func() (net.Listener, error), dialEndpoint func(time.Duration) (net.Conn, error)) (net.Conn, net.Conn) {
	t.Helper()
	ln, err := listenEndpoint()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	type result struct {
		conn net.Conn
		err  error
	}
	accepted := make(chan result, 1)
	go func() {
		conn, err := ln.Accept()
		accepted <- result{conn, err}
	}()
	client, err := dialEndpoint(3 * time.Second)
	if err != nil {
		_ = ln.Close()
		if r := <-accepted; r.conn != nil {
			_ = r.conn.Close()
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	r := <-accepted
	if r.err != nil {
		t.Fatal(r.err)
	}
	t.Cleanup(func() { _ = r.conn.Close() })
	for _, conn := range []net.Conn{r.conn, client} {
		if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	return r.conn, client
}
