package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"github.com/xallor-ai/xallor-remote/internal/runtimeowner"
)

type noBootstrapControl struct {
	runtimeControl
	calls int
}

func (c *noBootstrapControl) Running() (*buildinfo.Executable, error) { c.calls++; return nil, nil }
func (c *noBootstrapControl) Start(string, string) error {
	c.calls++
	return errors.New("unexpected start")
}
func (c *noBootstrapControl) PrepareProbe(release.ProbeRequest) (release.ProbeAttempt, error) {
	c.calls++
	return nil, errors.New("unexpected probe")
}

// The serving IPC can disappear during replacement; an unfinished durable
// operation must then refuse bootstrap before source selection or process work.
func TestEnsureRefusesUnfinishedOperationWhenIPCIsAbsent(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("XALLOR_REMOTE_DATA_DIR", profile)
	j := runtimeowner.Journal{Dir: filepath.Join(profile, "runtime", "lifecycle")}
	tx, err := j.Admit(runtimeowner.Request{ID: strings.Repeat("1", 32), Kind: "install", IntentSHA256: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	tx.Close()
	control := &noBootstrapControl{}
	if err := ensureRuntimeWithControl(control); !errors.Is(err, runtimeowner.ErrPending) {
		t.Fatal(err)
	}
	if control.calls != 0 {
		t.Fatal("unfinished operation reached bootstrap", control.calls)
	}
}

// Offline diagnostics must expose the unfinished operation without starting a
// process or pretending that an absent serving endpoint means an idle profile.
func TestRuntimeStatusReportsActiveOperationWithoutOwner(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("XALLOR_REMOTE_DATA_DIR", profile)
	id := strings.Repeat("1", 32)
	j := runtimeowner.Journal{Dir: filepath.Join(profile, "runtime", "lifecycle")}
	tx, err := j.Admit(runtimeowner.Request{ID: id, Kind: "install", IntentSHA256: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	tx.Close()
	cmd := cmdRuntime()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"status"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Operation runtimeowner.Record   `json:"operation"`
		Running   *buildinfo.Executable `json:"running"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Operation.Request.ID != id || result.Operation.Outcome != runtimeowner.Pending || result.Running != nil {
		t.Fatal(output.String())
	}
}
