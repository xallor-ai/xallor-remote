package noderun

import (
	"context"
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"github.com/xallor-ai/xallor-remote/internal/release"
)

func (d *Daemon) handleRuntimeOwner(c *ipc.Conn, req ipc.Request) bool {
	live := buildinfo.Running()
	if req.Method == "runtime.ensure" {
		var p struct {
			RequiredProfileFormat *int `json:"required_profile_format"`
			RequiredProtocol      *int `json:"required_protocol"`
		}
		if json.Unmarshal(req.Params, &p) != nil || p.RequiredProfileFormat == nil || p.RequiredProtocol == nil ||
			*p.RequiredProfileFormat != live.ProfileFormat || *p.RequiredProtocol != live.Protocol {
			_ = c.WriteFrame(ipc.Fail(req.ID, protocol.RuntimeProfileMismatch, "Runtime 数据格式或协议不匹配，请检查配套安装。"))
			return true
		}
	}
	return d.startOwnerObservation(c, req, func(ctx context.Context) (release.RuntimeObservation, error) {
		s := release.Store{Dir: filepath.Join(d.store.Dir, "runtime")}
		return s.ObserveRuntime(ctx, live)
	})
}

func (d *Daemon) startOwnerObservation(c *ipc.Conn, req ipc.Request, observe func(context.Context) (release.RuntimeObservation, error)) bool {
	d.mu.Lock()
	if d.ctx.Err() != nil || c.Closed() {
		d.mu.Unlock()
		_ = c.Close()
		return true
	}
	d.ownerObservationWG.Add(1)
	d.mu.Unlock()
	// Source verification can involve a bounded external verifier. Keep it off
	// the IPC reader so cancellation and status traffic can still be processed.
	// Coalesce only concurrent observations; never cache successful source proof.
	go func() {
		defer d.ownerObservationWG.Done()
		// A head that stopped reading must not hold shutdown behind a reply
		// write. Cancel the transport, then wait for the verifier itself below.
		stopClose := context.AfterFunc(d.ctx, func() { _ = c.Close() })
		defer stopClose()
		value, _, _ := d.ownerObservation.Do("selected-runtime", func() (any, error) {
			ctx, cancel := context.WithTimeout(d.ctx, 60*time.Second)
			defer cancel()
			view, err := observe(ctx)
			return view, err
		})
		view := value.(release.RuntimeObservation)
		if d.ctx.Err() != nil || c.Closed() {
			return
		}
		// Returning ready acknowledges that this process has latched its
		// startup selection, even if background Relay polling has not run yet.
		// The installer holds install.lock until this acknowledgement.
		if view.State == "ready" && !d.startupReady() {
			view.State, view.Code, view.ActivationID = "pending", protocol.RuntimePendingRecovery, ""
		}
		if view.State == "ready" && view.ValidateLaunch(d.launchID) != nil {
			view.State, view.Code, view.ActivationID = "unknown", protocol.RuntimeStartFailed, ""
		}
		if view.State != "ready" {
			view.LaunchID = ""
		}
		if req.Method == "runtime.ensure" && view.State != "ready" {
			_ = c.WriteFrame(ipc.Fail(req.ID, view.Code, runtimeOwnerMessage(view.Code)))
			return
		}
		_ = c.WriteFrame(ipc.OK(req.ID, map[string]any{"runtime": view}))
	}()
	return true
}

func (d *Daemon) waitOwnerObservations() {
	// Serialize cancellation with admission's Add, avoiding Add/Wait races.
	d.mu.Lock()
	d.cancel()
	d.mu.Unlock()
	d.ownerObservationWG.Wait()
}

func runtimeOwnerMessage(code string) string {
	switch code {
	case protocol.RuntimeProfileMismatch:
		return "当前 Runtime 不支持所需的本机生命周期协议，请检查配套的受信安装版本。"
	case protocol.RuntimePendingRecovery:
		return "上次升级尚未完成，请先执行 xallor-remote runtime rollback。"
	case protocol.RuntimeSourceUnverified:
		return "当前运行版本未通过安装验证，请检查已验证的安装版本。"
	default:
		return "运行版本与安装记录不一致，请先检查 xallor-remote runtime status。"
	}
}
