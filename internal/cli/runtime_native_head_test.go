package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/runtimeowner"
)

// The actual CLI entry points are heads: each running-owner operation creates a
// durable owner transaction, replaces the service and offers correlated status.
// Initial setup uses the existing backend; proof is the controlled native fixture.
func TestNativeCLILifecycleUsesOwnerTransactions(t *testing.T) {
	profile, binary, first := nativeOwnerBundle(t)
	nextBundle, next := nativeCandidate(t, binary, first, "cli-owner")
	s, err := runtimeStore()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := stopNativeFixtureRuntime(); err != nil {
			t.Error(err)
		}
	}()
	if err := s.Install(filepath.Dir(binary), runtimeControl{}); err != nil {
		logNativeTransactionFailure(t, profile)
		t.Fatal(err)
	}
	identity := nativeIdentityDigest(t, profile)
	for _, step := range []struct {
		args []string
		sha  string
	}{{[]string{"install", "--bundle", nextBundle}, next.Artifact.SHA256}, {[]string{"rollback"}, first.Artifact.SHA256}} {
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		cmd := cmdRuntime()
		var output, notices bytes.Buffer
		cmd.SetOut(&output)
		cmd.SetErr(&notices)
		cmd.SetArgs(step.args)
		err := cmd.ExecuteContext(ctx)
		cancel()
		if err != nil {
			logNativeTransactionFailure(t, profile)
			t.Fatal(step.args[0], err, notices.String())
		}
		id := regexp.MustCompile(`[a-f0-9]{32}`).FindString(notices.String())
		if id == "" {
			t.Fatal("CLI did not expose durable operation identity")
		}
		status := cmdRuntime()
		var observed bytes.Buffer
		status.SetOut(&observed)
		status.SetArgs([]string{"status", "--operation", id})
		if err := status.Execute(); err != nil {
			t.Fatal(err)
		}
		var result struct {
			Operation runtimeowner.Record `json:"operation"`
			Settled   bool                `json:"settled"`
		}
		if err := json.Unmarshal(observed.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Operation.Request.ID != id || result.Operation.Outcome != runtimeowner.Succeeded || !result.Settled {
			t.Fatal(observed.String())
		}
		live, err := (runtimeControl{}).Running()
		if err != nil || live == nil || live.SHA256 != step.sha {
			t.Fatal(live, err)
		}
		if identity != nativeIdentityDigest(t, profile) {
			t.Fatal("CLI owner path changed identity")
		}
	}
}
