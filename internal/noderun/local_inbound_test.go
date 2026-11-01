package noderun

import (
	"encoding/json"
	"net"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/identity"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
)

// 目的：inbound on 的 IPC 结果必须带上授权码，CLI 才能打印给对方。
func TestHandleLocal_should_returnGrant_whenInboundOn(t *testing.T) {
	st, err := identity.LoadFrom(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	d := &Daemon{store: st}
	srv, cli := net.Pipe()
	t.Cleanup(func() {
		_ = srv.Close()
		_ = cli.Close()
	})
	sc := ipc.NewConn(srv)
	cc := ipc.NewConn(cli)
	done := make(chan bool, 1)
	go func() {
		done <- d.handleLocal(sc, ipc.Request{
			ID:     "1",
			Method: "inbound.set",
			Params: json.RawMessage(`{"enabled":true}`),
		})
	}()
	fr, err := cc.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	if !<-done {
		t.Fatal("handleLocal should claim inbound.set")
	}
	if fr.OK == nil || !*fr.OK {
		t.Fatalf("frame=%+v", fr)
	}
	var res struct {
		Inbound bool   `json:"inbound"`
		Grant   string `json:"grant"`
	}
	if err := json.Unmarshal(fr.Result, &res); err != nil {
		t.Fatal(err)
	}
	if !res.Inbound || res.Grant == "" {
		t.Fatalf("inbound=%v grant=%q", res.Inbound, res.Grant)
	}
}
