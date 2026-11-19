package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
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

func ownerLifecycle(ctx context.Context, kind, bundle string, notice io.Writer) (bool, error) {
	return ownerLifecycleWithDial(ctx, kind, bundle, ipc.Dial, notice)
}

// Transitional no-owner backend is never allowed to take over a serving owner
// that appeared after the first dial. Bootstrap and admission share this lock.
func noOwnerLifecycleGuard(store release.Store, control release.Control) func(func() error) error {
	journal := runtimeowner.Journal{Dir: filepath.Join(store.Dir, "lifecycle")}
	return func(run func() error) error {
		return journal.WithBootstrap(func() error {
			live, err := control.Running()
			if err != nil {
				return err
			}
			if live != nil {
				return errors.New("Runtime 已在运行，请重新通过其版本事务入口提交；本次未启动本地安装。")
			}
			return run()
		})
	}
}

func ownerLifecycleWithDial(ctx context.Context, kind, bundle string, dial func(time.Duration) (net.Conn, error), notice io.Writer) (bool, error) {
	raw, err := dial(300 * time.Millisecond)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) {
			return false, nil
		}
		return true, err
	}
	defer raw.Close()
	bounded, cancel := context.WithTimeout(ctx, 240*time.Second)
	defer cancel()
	stop := context.AfterFunc(bounded, func() { _ = raw.Close() })
	defer stop()
	conn, err := runtimehead.Handshake(raw)
	if err != nil {
		return true, err
	}
	command, err := runtimeowner.NewCommand(kind, bundle)
	if err != nil {
		return true, err
	}
	if notice != nil {
		if _, err := fmt.Fprintf(notice, "版本事务：%s；可用 runtime status --operation %s 查询。\n", command.ID, command.ID); err != nil {
			return true, err
		}
	}
	_, err = runtimehead.SubmitLifecycle(bounded, raw, conn, command, runtimehead.QueryLifecycle)
	return true, err
}
