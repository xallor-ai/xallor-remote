package cli

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"github.com/xallor-ai/xallor-remote/internal/runtimehead"
	"github.com/xallor-ai/xallor-remote/internal/runtimeowner"
)

func ensureRuntime() error {
	return ensureRuntimeWithControl(runtimeControl{})
}

func ensureRuntimeWithControl(control release.Control) error {
	if handled, err := ownerEnsure(); handled {
		return err
	}
	s, err := runtimeStore()
	if err != nil {
		return err
	}
	journal := runtimeowner.Journal{Dir: filepath.Join(s.Dir, "lifecycle")}
	if err := journal.RefuseBootstrap(); err != nil {
		return fmt.Errorf("本机升级事务尚未确认，拒绝重复启动；请检查 runtime status：%w", err)
	}
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	bundle, err := bootstrapSourceDir(bin, os.Args[0])
	if err != nil {
		return err
	}
	if err := s.WithBootstrapGuard(journal.WithBootstrap).EnsureRunning(bundle, control); err != nil {
		return err
	}
	// Even a newly started (or concurrently appearing) process must satisfy the
	// same owner contract as an already connected Runtime.
	if handled, err := ownerEnsure(); handled {
		return err
	}
	return fmt.Errorf("Runtime 启动后未能确认就绪，请检查 xallor-remote runtime status。")
}

// ownerEnsure is the common-head path. A running Runtime is the sole owner of
// readiness and activation; the CLI only bootstraps when IPC is absent.
func ownerEnsure() (bool, error) {
	return ownerEnsureWithDial(ipc.Dial)
}

func ownerEnsureWithDial(dial func(time.Duration) (net.Conn, error)) (bool, error) {
	raw, err := dial(300 * time.Millisecond)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) {
			return false, nil
		}
		// Timeout/access errors do not prove absence and cannot authorize a
		// second bootstrap alongside an owner whose state is unknown.
		return true, err
	}
	defer raw.Close()
	_, err = runtimehead.Handshake(raw)
	return true, err
}
