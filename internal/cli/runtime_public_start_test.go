package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A plain start from an unsigned native bootstrap must not initialize an
// identity or listen to IPC. The old direct service loop did both.
func TestPublicStart_should_refuseUnverifiedBootstrap_beforeProfileMutation(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "profile")
	t.Setenv("XALLOR_REMOTE_DATA_DIR", profile)
	t.Setenv("XALLOR_REMOTE_RELAY_URL", "ws://127.0.0.1:1/remote")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, self)
	configureDetached(child)
	child.Env = append(os.Environ(), "XR_TEST_PUBLIC_START=1")
	err = child.Run()
	if err == nil {
		t.Error("unverified public start succeeded")
	}
	if _, err := os.Stat(filepath.Join(profile, "identity.db")); !os.IsNotExist(err) {
		t.Fatal("public start initialized identity before source admission", err)
	}
	if ctx.Err() != nil {
		t.Fatal("public start entered service loop instead of refusing")
	}
}

// Supplying the hidden command name and a fabricated pipe payload cannot
// initialize an owner without a matching parent and authenticated transition.
func TestRuntimeServe_should_refuseInvalidLaunchPipe_beforeIdentity(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "profile")
	t.Setenv("XALLOR_REMOTE_DATA_DIR", profile)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, self)
	configureDetached(child)
	child.Env = append(os.Environ(), "XR_TEST_INTERNAL_SERVE=1")
	child.Stdin = strings.NewReader(`{"parent_pid":-1,"activation_id":"fabricated"}`)
	if err := child.Run(); err == nil || ctx.Err() != nil {
		t.Fatal("invalid service launch did not refuse", err, ctx.Err())
	}
	if _, err := os.Stat(profile); !os.IsNotExist(err) {
		t.Fatal("invalid service launch changed profile", err)
	}
}
