package handoff

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/invocation"
)

// Two callers may choose the same request ID, but cannot see or overwrite each
// other's result. The binding and session ownership survive process restart.
func TestScope_should_isolateReceiptsAndSessions_whenCallersReuseRequestID(t *testing.T) {
	dir := t.TempDir()
	a := testScope
	a.PrincipalID, a.TenantID = strings.Repeat("d", 32), strings.Repeat("e", 32)
	b := a
	b.PrincipalID = strings.Repeat("f", 32)
	m := New(context.Background(), dir)
	r := testRequest(t.TempDir())
	saved := record{Scope: a, Receipt: Receipt{ID: r.ID, Fingerprint: fingerprint(r), Session: r.Session, State: "completed", Result: "alice-private"}}
	if err := m.persist(saved); err != nil {
		t.Fatal(err)
	}
	m.Close()
	m = New(context.Background(), dir)
	if _, err := m.Get(b, r.ID); err == nil {
		t.Fatal("other caller read receipt")
	}
	if _, err := m.Submit(b, r, r.Workspace); err == nil {
		t.Fatal("other caller resumed bound session")
	}
	if got, err := m.Submit(a, r, r.Workspace); err != nil || got.Result != "alice-private" {
		t.Fatal("same caller replayed", got, err)
	}
	r.Session = "22222222-2222-2222-2222-222222222222"
	if _, err := m.Submit(b, r, r.Workspace); err != nil {
		t.Fatal("independent ID rejected", err)
	}
	m.Close()
	m = New(context.Background(), dir)
	defer m.Close()
	if got, err := m.Get(a, r.ID); err != nil || got.Result != "alice-private" {
		t.Fatal(got, err)
	}
	if got, err := m.Get(b, r.ID); err != nil || got.Result == "alice-private" || got.Session != r.Session {
		t.Fatal(got, err)
	}
	if len(m.items) != 2 {
		t.Fatal("records collided")
	}
}

// Authority, resource incarnation, ownership, bearer and tenant are independent
// namespace dimensions. Invalid contexts must never fall back to device scope.
func TestScope_should_rejectOtherNamespacesAndMissingIdentity(t *testing.T) {
	s := testScope
	s.PrincipalID, s.TenantID = strings.Repeat("a", 32), strings.Repeat("b", 32)
	r := testRequest(t.TempDir())
	m := New(context.Background(), t.TempDir())
	defer m.Close()
	m.items[requestKey(s, r.ID)] = record{Scope: s, Receipt: Receipt{ID: r.ID, Session: r.Session, State: "completed"}}
	for _, change := range []func(*invocation.Scope){
		func(v *invocation.Scope) { v.Authority = strings.Repeat("d", 32) },
		func(v *invocation.Scope) { v.DeviceUID = strings.Repeat("d", 32) },
		func(v *invocation.Scope) { v.GrantID = strings.Repeat("d", 32) },
		func(v *invocation.Scope) { v.PrincipalID = strings.Repeat("d", 32) },
		func(v *invocation.Scope) { v.TenantID = strings.Repeat("e", 32) },
		func(v *invocation.Scope) { v.GrantID = ""; v.OwnershipID = strings.Repeat("d", 32) },
	} {
		other := s
		change(&other)
		if _, err := m.Get(other, r.ID); err == nil {
			t.Fatal("cross-namespace receipt")
		}
	}
	if _, err := m.Submit(invocation.Scope{}, r, r.Workspace); err == nil {
		t.Fatal("unscoped submission")
	}
}

// Unscoped historical data cannot be assigned a trusted owner by guessing. It
// stays untouched, with no API fallback or automatic replay into the new store.
func TestScope_should_preserveButNeverReadUnscopedFiles(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "handoff", "receipts")
	if err := os.MkdirAll(old, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(old, "request-one.json")
	data := []byte(`{"request_id":"request-one","result":"unowned-secret"}`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	m := New(context.Background(), dir)
	defer m.Close()
	if m.err != nil {
		t.Fatal(m.err)
	}
	if _, err := m.Get(testScope, "request-one"); err == nil {
		t.Fatal("guessed owner of unscoped result")
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(data) {
		t.Fatal("old data modified", err)
	}
}

// Simultaneous callers cannot both reserve one session, even if the first
// preflight fails quickly. The accepted record retains the ownership decision.
func TestScope_should_reserveSessionAtomically_whenSubjectsRace(t *testing.T) {
	m := New(context.Background(), t.TempDir())
	defer m.Close()
	r := testRequest(t.TempDir())
	b := testScope
	b.PrincipalID, b.TenantID = strings.Repeat("d", 32), strings.Repeat("e", 32)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, scope := range []invocation.Scope{testScope, b} {
		wg.Add(1)
		go func(s invocation.Scope) { defer wg.Done(); _, err := m.Submit(s, r, r.Workspace); results <- err }(scope)
	}
	wg.Wait()
	close(results)
	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatalf("accepted %d different subjects", accepted)
	}
}

// A scope changed on disk must not silently acquire the old filename or bind
// another caller's receipt. Preserve evidence and fail the store closed.
func TestScope_should_rejectTamperedRecordBinding_whenReopened(t *testing.T) {
	dir := t.TempDir()
	m := New(context.Background(), dir)
	r := testRequest(t.TempDir())
	saved := record{Scope: testScope, Receipt: Receipt{ID: r.ID, Session: r.Session, Fingerprint: fingerprint(r), State: "completed"}}
	if err := m.persist(saved); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(m.receiptDir(), requestKey(testScope, r.ID)+".json")
	m.Close()
	saved.Scope.Authority = strings.Repeat("f", 32)
	data, _ := json.Marshal(saved)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	m = New(context.Background(), dir)
	defer m.Close()
	if m.err == nil {
		t.Fatal("tampered binding accepted")
	}
	if _, err := m.Get(saved.Scope, r.ID); err == nil {
		t.Fatal("tampered result exposed")
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(data) {
		t.Fatal("evidence modified", err)
	}
}
