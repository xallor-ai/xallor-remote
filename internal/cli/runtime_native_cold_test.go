package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/runtimeowner"
)

// Initial CLI installation is a single verified bootstrap. After the service
// stops, upgrade/rollback must restart the selected owner and create IPC-owned
// transactions rather than executing the installer inside the head.
func TestNativeColdLifecycleBootstrapsOwnerBeforeUpgradeAndRollback(t *testing.T) {
	profile, binary, first := nativeOwnerBundle(t)
	nextBundle, next := nativeCandidate(t, binary, first, "cold-owner")
	defer func() {
		if err := stopNativeFixtureRuntime(); err != nil {
			t.Error(err)
		}
	}()
	var identity string
	steps := []struct {
		args        []string
		sha         string
		transaction bool
	}{
		{[]string{"install", "--bundle", filepath.Dir(binary)}, first.Artifact.SHA256, false},
		{[]string{"install", "--bundle", nextBundle}, next.Artifact.SHA256, true},
		{[]string{"rollback"}, first.Artifact.SHA256, true},
	}
	for _, step := range steps {
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		cmd := cmdRuntime()
		var output, notice bytes.Buffer
		cmd.SetOut(&output)
		cmd.SetErr(&notice)
		cmd.SetArgs(step.args)
		err := cmd.ExecuteContext(ctx)
		cancel()
		if err != nil {
			t.Fatal(step.args[0], err, notice.String())
		}
		live, err := (runtimeControl{}).Running()
		if err != nil || live == nil || live.SHA256 != step.sha {
			t.Fatal(live, err)
		}
		digest := nativeIdentityDigest(t, profile)
		if identity == "" {
			identity = digest
		} else if digest != identity {
			t.Fatal("cold lifecycle replaced device identity")
		}
		if step.transaction {
			id := regexp.MustCompile(`[a-f0-9]{32}`).FindString(notice.String())
			if id == "" {
				t.Fatal("cold lifecycle bypassed owner transaction")
			}
			store, err := runtimeStore()
			if err != nil {
				t.Fatal(err)
			}
			record, settled, err := (runtimeowner.Journal{Dir: filepath.Join(store.Dir, "lifecycle")}).QuerySettled(id)
			if err != nil || !settled || record.Outcome != runtimeowner.Succeeded {
				t.Fatal(record, settled, err)
			}
		}
		if err := (runtimeControl{}).Stop(); err != nil {
			t.Fatal(err)
		}
	}
}
