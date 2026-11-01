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
	s := &identity.Store{DeviceID: "123456789", Secret: "local-secret", Grant: "local-grant",
		Config: identity.Config{RelayURL: "wss://user:pass@example.test/private-token?key=query-secret#fragment-secret"},
		Peers:  []identity.Peer{{DeviceID: "987654321", Grant: "peer-secret"}}}
	d := New(s, slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer d.Stop()
	before, _ := json.Marshal(s)
	status, _ := json.Marshal(d.status())
	r, _ := json.Marshal(d.diagnose("111111111"))
	for _, secret := range []string{"local-secret", "local-grant", "peer-secret", "user", "pass", "private-token", "query-secret", "fragment-secret"} {
		if strings.Contains(string(status), secret) || strings.Contains(string(r), secret) {
			t.Fatalf("leaked %s", secret)
		}
	}
	after, _ := json.Marshal(s)
	if string(before) != string(after) {
		t.Fatal("observation changed store")
	}
}
