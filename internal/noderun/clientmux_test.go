package noderun

import (
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func TestClientLink_should_route_by_exec_id(t *testing.T) {
	cl := &clientLink{waiters: map[string]*execWaiter{}}
	a := cl.subscribe("ex_a")
	b := cl.subscribe("ex_b")
	t.Cleanup(func() {
		cl.unsubscribe("ex_a")
		cl.unsubscribe("ex_b")
	})
	cl.dispatch(protocol.Message{Type: protocol.TypeStdout, ExecID: "ex_a", Data: "A"})
	cl.dispatch(protocol.Message{Type: protocol.TypeStdout, ExecID: "ex_b", Data: "B"})
	gotA, okA := a.next()
	gotB, okB := b.next()
	if !okA || !okB || gotA.Data != "A" || gotB.Data != "B" {
		t.Fatalf("crossed: a=%q b=%q", gotA.Data, gotB.Data)
	}
}

func TestExecWaiter_should_deliver_other_exit_when_slow_consumer_fills_buffer(t *testing.T) {
	cl := &clientLink{waiters: map[string]*execWaiter{}}
	a := cl.subscribe("ex_a")
	b := cl.subscribe("ex_b")
	for i := 0; i < waiterDataSlots+8; i++ {
		cl.dispatch(protocol.Message{Type: protocol.TypeStdout, ExecID: "ex_a", Data: "x"})
	}
	code := 0
	cl.dispatch(protocol.Message{Type: protocol.TypeExit, ExecID: "ex_b", ExitCode: &code, Status: protocol.ExitCompleted})
	got, ok := b.next()
	if !ok || got.Type != protocol.TypeExit {
		t.Fatalf("B should receive exit while A is unread: %+v ok=%v", got, ok)
	}
	_ = a
}

func TestExecWaiter_should_mark_truncated_when_data_dropped(t *testing.T) {
	w := newExecWaiter()
	for i := 0; i < waiterDataSlots+4; i++ {
		w.push(protocol.Message{Type: protocol.TypeStdout, Data: "x"})
	}
	code := 0
	w.push(protocol.Message{Type: protocol.TypeExit, ExitCode: &code, Status: protocol.ExitCompleted})
	var last protocol.Message
	for {
		msg, ok := w.next()
		if !ok {
			t.Fatal("expected terminal")
		}
		if msg.Type == protocol.TypeExit {
			last = msg
			break
		}
	}
	if last.Truncated == nil || !*last.Truncated {
		t.Fatal("dropped data should set truncated on exit")
	}
}

func TestClientLink_should_finish_waiters_when_link_fails(t *testing.T) {
	cl := &clientLink{waiters: map[string]*execWaiter{}}
	a := cl.subscribe("ex_a")
	b := cl.subscribe("ex_b")
	cl.failWaiters()
	gotA, okA := a.next()
	gotB, okB := b.next()
	if !okA || !okB || gotA.Type != protocol.TypeError || gotB.Type != protocol.TypeError {
		t.Fatalf("both waiters should get error: a=%+v b=%+v", gotA, gotB)
	}
}

// 目的：exit 已被 next() 取走后，断线清理不得再次 close(done)。
// 前置：仍 subscribe；term 已清空。预期：failWaiters 不 panic，后续 next 返回 false。
func TestExecWaiter_should_not_panic_when_fail_after_term_consumed(t *testing.T) {
	cl := &clientLink{waiters: map[string]*execWaiter{}}
	w := cl.subscribe("ex")
	code := 0
	cl.dispatch(protocol.Message{Type: protocol.TypeExit, ExecID: "ex", ExitCode: &code, Status: protocol.ExitCompleted})
	got, ok := w.next()
	if !ok || got.Type != protocol.TypeExit {
		t.Fatalf("want exit: %+v ok=%v", got, ok)
	}
	cl.failWaiters()
	if msg, more := w.next(); more {
		t.Fatalf("consumed terminal should stay final, got %+v", msg)
	}
}

func TestDropClient_should_keep_new_link_when_old_instance_drops(t *testing.T) {
	oldClosed := false
	newClosed := false
	old := &clientLink{cancel: func() { oldClosed = true }}
	neu := &clientLink{cancel: func() { newClosed = true }}
	d := &Daemon{clients: map[string]*clientLink{"dev": neu}}
	d.dropClient("dev", old)
	if d.clients["dev"] != neu {
		t.Fatal("new link was removed")
	}
	if newClosed {
		t.Fatal("new link was cancelled")
	}
	if !oldClosed {
		t.Fatal("old link should still be closed")
	}
}
