package handoff

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/invocation"
)

var testScope = invocation.Scope{Authority: strings.Repeat("a", 32), DeviceUID: strings.Repeat("b", 32), GrantID: strings.Repeat("c", 32)}

func testRequest(workspace string) Request {
	return Request{ID: "request-one", Workspace: workspace, Revision: strings.Repeat("a", 40), Agent: "cursor", Session: testSession, Mode: "ask", Prompt: "review", TimeoutSeconds: 30}
}

// Same request returns the durable receipt; conflicting reuse is never another execution.
func TestManager_should_deduplicate_and_rejectChangedRequest(t *testing.T) {
	m := New(context.Background(), t.TempDir())
	defer m.Close()
	r := testRequest(t.TempDir())
	first, err := m.Submit(testScope, r, r.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.Submit(testScope, r, r.Workspace)
	if err != nil || first.Fingerprint != second.Fingerprint {
		t.Fatal(err)
	}
	r.Prompt = "changed"
	if _, err = m.Submit(testScope, r, r.Workspace); err == nil {
		t.Fatal("conflicting reuse accepted")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		receipt, err := m.Get(testScope, r.ID)
		if err != nil {
			t.Fatal(err)
		}
		if terminal(receipt.State) {
			if receipt.State != "failed" {
				t.Fatal(receipt)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("preflight did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Restart marks unfinished work unknown and never launches/replays it; capacity fails closed.
func TestManager_should_recoverUnknown_and_preserveDedupeAtCapacity(t *testing.T) {
	dir := t.TempDir()
	m := New(context.Background(), dir)
	r := testRequest(t.TempDir())
	receipt := Receipt{ID: r.ID, Fingerprint: fingerprint(r), State: "running", Session: r.Session}
	if err := m.persist(record{Scope: testScope, Receipt: receipt}); err != nil {
		t.Fatal(err)
	}
	m.Close()
	m = New(context.Background(), dir)
	defer m.Close()
	got, err := m.Get(testScope, r.ID)
	if err != nil || got.State != "unknown" || got.Code != "runtime_restarted" || got.Updated == "" {
		t.Fatalf("%+v %v", got, err)
	}
	if again, err := m.Submit(testScope, r, r.Workspace); err != nil || again.State != "unknown" {
		t.Fatalf("replayed: %+v %v", again, err)
	}
	for len(m.items) < MaxReceipts {
		id := fmt.Sprintf("item-%d", len(m.items))
		m.items[id] = record{Scope: testScope, Receipt: Receipt{ID: id}}
	}
	r.ID = "new-request"
	if _, err = m.Submit(testScope, r, r.Workspace); err == nil {
		t.Fatal("capacity was silently evicted")
	}
}

// Closing concurrently with first submission must wait for accepted work and reject later work.
func TestManager_should_serializeCloseWithSubmit(t *testing.T) {
	for i := 0; i < 20; i++ {
		m := New(context.Background(), t.TempDir())
		r := testRequest(t.TempDir())
		var workers sync.WaitGroup
		workers.Add(2)
		go func() { defer workers.Done(); _, _ = m.Submit(testScope, r, r.Workspace) }()
		go func() { defer workers.Done(); m.Close() }()
		workers.Wait()
		if _, err := m.Submit(testScope, r, r.Workspace); err == nil {
			t.Fatal("closed manager accepted work")
		}
		m.mu.Lock()
		active := len(m.active)
		m.mu.Unlock()
		if active != 0 {
			t.Fatal("Close returned before worker cleanup")
		}
	}
}

// Corrupt receipts are surfaced; a second request cannot overwrite the evidence.
func TestManager_should_refuseCorruptStore(t *testing.T) {
	dir := t.TempDir()
	receipts := filepath.Join(dir, "handoff", "requests")
	_ = os.MkdirAll(receipts, 0700)
	_ = os.WriteFile(filepath.Join(receipts, "broken.json"), []byte("broken"), 0600)
	m := New(context.Background(), dir)
	defer m.Close()
	if _, err := m.Get(testScope, "broken"); err == nil {
		t.Fatal("corruption hidden")
	}
	r := testRequest(t.TempDir())
	if _, err := m.Submit(testScope, r, r.Workspace); err == nil {
		t.Fatal("corrupt store accepted new work")
	}
	b, _ := os.ReadFile(filepath.Join(receipts, "broken.json"))
	if json.Valid(b) {
		t.Fatal("corrupt evidence replaced")
	}
}
