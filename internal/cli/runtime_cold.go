package cli

import (
	"context"
	"errors"
	"io"
	"path/filepath"

	"github.com/xallor-ai/xallor-remote/internal/runtimeowner"
)

// coldLifecycle enters only after a definite absent serving endpoint. Normal
// cold installations bootstrap the selected owner and then use its IPC workflow.
// Pending recovery remains an explicit transitional path, never automatic ensure.
func coldLifecycle(ctx context.Context, kind, bundle string, notice io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s, err := runtimeStore()
	if err != nil {
		return err
	}
	state, err := s.Read()
	if err != nil {
		return err
	}
	journal := runtimeowner.Journal{Dir: filepath.Join(s.Dir, "lifecycle")}
	if err := journal.RefuseBootstrap(); err != nil {
		return err
	}
	guarded := s.WithBootstrapGuard(noOwnerLifecycleGuard(s, runtimeControl{}))
	if state.Pending != nil || state.PendingActivation != "" {
		if kind != "rollback" {
			return errors.New("上次版本切换尚未完成，请先核对并恢复原事务。")
		}
		return recoverLegacyPending(ctx, notice)
	}
	if state.Current == nil {
		if kind != "install" {
			return errors.New("没有可启动的已验证 Runtime，请检查原安装与恢复记录。")
		}
		return guarded.InitializeOwner(bundle, runtimeControl{})
	}
	// Do not select the supplied upgrade bundle as a bootstrap replacement.
	// Only the original selected and freshly verified owner may start here.
	if err := guarded.EnsureRunning("", runtimeControl{}); err != nil {
		return err
	}
	handled, err := ownerLifecycle(ctx, kind, bundle, notice)
	if !handled {
		return errors.New("受信 Runtime 启动后不可连接，未提交版本变更，请检查运行状态。")
	}
	return err
}
