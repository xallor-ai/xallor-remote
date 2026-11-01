package noderun

import (
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func TestClientLink_should_route_by_exec_id(t *testing.T) {
	cl := &clientLink{waiters: map[string]chan protocol.Message{}}
	a := cl.subscribe("ex_a")
	b := cl.subscribe("ex_b")
	t.Cleanup(func() {
		cl.unsubscribe("ex_a")
		cl.unsubscribe("ex_b")
	})

	a <- protocol.Message{Type: protocol.TypeStdout, ExecID: "ex_a", Data: "A"}
	b <- protocol.Message{Type: protocol.TypeStdout, ExecID: "ex_b", Data: "B"}

	gotA := <-a
	gotB := <-b
	if gotA.Data != "A" || gotB.Data != "B" {
		t.Fatalf("crossed: a=%q b=%q", gotA.Data, gotB.Data)
	}
}

func TestClientLink_should_close_waiters_when_link_fails(t *testing.T) {
	cl := &clientLink{waiters: map[string]chan protocol.Message{}}
	a := cl.subscribe("ex_a")
	b := cl.subscribe("ex_b")
	cl.failWaiters()
	if _, ok := <-a; ok {
		t.Fatal("waiter a should close")
	}
	if _, ok := <-b; ok {
		t.Fatal("waiter b should close")
	}
}

func TestClientLink_should_ignore_unknown_exec_id(t *testing.T) {
	cl := &clientLink{waiters: map[string]chan protocol.Message{}}
	ch := cl.subscribe("ex_keep")
	cl.mu.Lock()
	got := cl.waiters["missing"]
	cl.mu.Unlock()
	if got != nil {
		t.Fatal("unknown exec should have no waiter")
	}
	select {
	case <-ch:
		t.Fatal("kept waiter should stay idle")
	default:
	}
	cl.unsubscribe("ex_keep")
}
