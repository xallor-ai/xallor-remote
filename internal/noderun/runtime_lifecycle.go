package noderun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"sync"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"github.com/xallor-ai/xallor-remote/internal/release"
	"github.com/xallor-ai/xallor-remote/internal/runtimeowner"
)

type lifecycleDispatch struct {
	mu         sync.Mutex
	busy       bool
	launch     runtimeowner.Launcher
	launches   sync.WaitGroup
	cleanupErr error
}

func (d *Daemon) WithLifecycleLauncher(launch runtimeowner.Launcher) *Daemon {
	d.lifecycle = &lifecycleDispatch{launch: launch}
	return d
}

func (d *Daemon) handleLifecycle(c *ipc.Conn, req ipc.Request) bool {
	if req.Method != "runtime.install" && req.Method != "runtime.rollback" {
		return false
	}
	var p struct {
		ID        string `json:"operation_id"`
		Bundle    string `json:"bundle,omitempty"`
		Namespace string `json:"namespace_sha256"`
	}
	decoder := json.NewDecoder(bytes.NewReader(req.Params))
	decoder.DisallowUnknownFields()
	if len(req.Params) > 128*1024 || decoder.Decode(&p) != nil || decoder.Decode(new(any)) != io.EOF {
		_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, "升级请求格式无效。"))
		return true
	}
	kind := "install"
	if req.Method == "runtime.rollback" {
		kind = "rollback"
	}
	command := runtimeowner.Command{ID: p.ID, Kind: kind, Bundle: p.Bundle}
	if _, err := command.Request(); err != nil {
		_ = c.WriteFrame(ipc.Fail(req.ID, protocol.AgentError, "需要有效事务 ID 和绝对发行目录；回滚不接受发行目录。"))
		return true
	}
	namespace, err := (runtimeowner.Journal{Dir: filepath.Join(d.store.Dir, "runtime", "lifecycle")}).NamespaceID()
	if err != nil || p.Namespace != namespace {
		_ = c.WriteFrame(ipc.Fail(req.ID, protocol.RuntimeProfileMismatch, "版本事务不属于当前配置目录；本次未创建事务。"))
		return true
	}
	state := d.lifecycle
	if state == nil || state.launch == nil {
		_ = c.WriteFrame(ipc.Fail(req.ID, protocol.RuntimeUnavailable, "当前 Runtime 未配置版本交接入口。"))
		return true
	}
	state.mu.Lock()
	if state.busy || d.ctx.Err() != nil {
		state.mu.Unlock()
		_ = c.WriteFrame(ipc.Fail(req.ID, protocol.RuntimePendingRecovery, "本机版本交接仍在进行，请查询原事务。"))
		return true
	}
	state.busy = true
	state.launches.Add(1)
	state.mu.Unlock()
	go func() {
		defer state.launches.Done()
		stopClose := context.AfterFunc(d.ctx, func() { _ = c.Close() })
		defer stopClose()
		ctx, cancel := context.WithTimeout(d.ctx, 75*time.Second)
		attempt, err := state.launch(ctx, command)
		cancel()
		releaseGate := func(result error) {
			state.mu.Lock()
			defer state.mu.Unlock()
			if errors.Is(result, release.ErrCleanupUncertain) || errors.Is(result, runtimeowner.ErrUncertain) {
				state.cleanupErr = result
				return
			}
			state.busy = false
		}
		if attempt == nil {
			releaseGate(err)
		} else {
			go func() { <-attempt.Done(); releaseGate(attempt.Result()) }()
		}
		// A head disconnect does not cancel an accepted worker. A failed write
		// is delivery uncertainty; the head must query its original operation ID.
		replyContext, stopReply := context.WithTimeout(context.Background(), 3*time.Second)
		defer stopReply()
		stopWrite := context.AfterFunc(replyContext, func() { _ = c.Close() })
		defer stopWrite()
		if err != nil || attempt == nil {
			if attempt == nil && err != nil && !errors.Is(err, release.ErrCleanupUncertain) && !errors.Is(err, runtimeowner.ErrUncertain) {
				_ = c.WriteFrame(ipc.Fail(req.ID, protocol.RuntimeSourceUnverified, "版本交接在创建事务进程前被拒绝，请检查受信安装记录和发行目录。"))
				return
			}
			_ = c.WriteFrame(ipc.Fail(req.ID, protocol.RuntimeCleanupUncertain, "版本交接未能确认，请按原事务 ID 查询；不要重复提交。"))
			return
		}
		_ = c.WriteFrame(ipc.OK(req.ID, map[string]any{"accepted": true, "operation_id": command.ID}))
	}()
	return true
}

// Shutdown joins authentication/launch admission, never the accepted worker:
// the worker must survive this serving process to complete its replacement.
func (d *Daemon) waitLifecycleLaunches() {
	if d.lifecycle == nil {
		return
	}
	d.lifecycle.mu.Lock()
	d.cancel()
	d.lifecycle.mu.Unlock()
	d.lifecycle.launches.Wait()
}
