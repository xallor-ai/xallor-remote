package cli

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/release"
)

// Only a definite absent endpoint reaches the unfinished no-owner branch;
// handshake refusal, permission failure and timeouts cannot fall back to install.
func TestOwnerLifecycleDoesNotFallbackOnUnknownEndpoint(t *testing.T) {
	for _, test := range []struct {
		err     error
		handled bool
	}{{os.ErrNotExist, false}, {os.ErrPermission, true}, {context.DeadlineExceeded, true}} {
		handled, err := ownerLifecycleWithDial(context.Background(), "rollback", "", func(time.Duration) (net.Conn, error) { return nil, test.err }, nil)
		if handled != test.handled || (handled && !errors.Is(err, test.err)) {
			t.Fatal(handled, err)
		}
	}
}

type appearedOwnerControl struct{ runtimeControl }

func (appearedOwnerControl) Running() (*buildinfo.Executable, error) {
	return &buildinfo.Executable{PID: 123}, nil
}

// A serving owner may appear after the initial absent dial. Under the common
// lifecycle lock, that observation must stop the transitional local path.
func TestNoOwnerLifecycleGuardRefusesOwnerThatAppeared(t *testing.T) {
	store := release.Store{Dir: filepath.Join(t.TempDir(), "runtime")}
	called := false
	err := noOwnerLifecycleGuard(store, appearedOwnerControl{})(func() error { called = true; return nil })
	if err == nil || called {
		t.Fatal("local installer took over newly appeared owner", called, err)
	}
}

// A reachable but unverified Runtime must not cause a local mutation fallback.
func TestOwnerLifecycleHandshakeRefusalIsHandled(t *testing.T) {
	a, b := net.Pipe()
	go func() {
		defer a.Close()
		var data [4096]byte
		a.Read(data[:])
		a.Write([]byte("{\"id\":\"runtime-owner\",\"ok\":false,\"code\":\"runtime_source_unverified\"}\n"))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	handled, err := ownerLifecycleWithDial(ctx, "rollback", "", func(time.Duration) (net.Conn, error) { return b, nil }, nil)
	if !handled || err == nil {
		t.Fatal(handled, err)
	}
}
