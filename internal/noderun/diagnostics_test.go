package noderun

import (
	"encoding/json"
	"github.com/xallor-ai/xallor-remote/internal/identity"
	"io"
	"log/slog"
	"strings"
	"testing"
)

// Status and failed diagnostics must not expose local/peer secrets or credentials in URLs.
func TestDiagnostics_should_redactCredentials_and_preserveStore(t *testing.T) {
	s, err := identity.LoadFrom(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	grant, err := s.IssueGrant()
	if err != nil {
		t.Fatal(err)
	}
	_, secret, _, _, _, _, _ := s.Snapshot()
	if err := s.SetConfig("wss://user:pass@example.test/private-token?key=query-secret#fragment-secret", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.AddPeer("987654321", "peer-secret"); err != nil {
		t.Fatal(err)
	}
	d := New(s, slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer d.Stop()
	before, _ := json.Marshal(s)
	idBefore, secretBefore, grantBefore, _, _, inboundBefore, _ := s.Snapshot()
	status, _ := json.Marshal(d.status())
	r, _ := json.Marshal(d.diagnose("111111111"))
	for _, credential := range []string{secret, grant, "peer-secret", "user", "pass", "private-token", "query-secret", "fragment-secret"} {
		if strings.Contains(string(status), credential) || strings.Contains(string(r), credential) {
			t.Fatal("diagnostics leaked a credential")
		}
	}
	after, _ := json.Marshal(s)
	idAfter, secretAfter, grantAfter, _, _, inboundAfter, _ := s.Snapshot()
	if string(before) != string(after) || idBefore != idAfter || secretBefore != secretAfter || grantBefore != grantAfter || inboundBefore != inboundAfter {
		t.Fatal("observation changed store")
	}
}
