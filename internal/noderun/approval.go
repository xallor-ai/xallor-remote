package noderun

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/execution"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

const approvalWait = 60 * time.Second
const approvalWriteWait = 2 * time.Second

type approvalHub struct {
	mu      sync.Mutex
	subs    map[frameWriter]struct{}
	pending map[string]*pendingApproval // key = approval request id
	byExec  map[string]string           // exec_id → approval id
	seq     atomic.Uint64
}

type frameWriter interface {
	WriteFrameContext(context.Context, ipc.Frame) error
}

type pendingApproval struct {
	execID  string
	details approvalDetails
	digest  string
	created string
	expires string
	ch      chan decide
}

// approvalDetails is the exact, immutable operation fact shown to a human
// before the owner starts a child. It contains no derived preview or shell
// re-interpretation; the digest binds every field to the response.
type approvalDetails struct {
	Backend    string          `json:"backend,omitempty"`
	Kind       string          `json:"kind"`
	Script     string          `json:"script,omitempty"`
	Shell      string          `json:"shell,omitempty"`
	Executable string          `json:"executable,omitempty"`
	Args       []string        `json:"args,omitempty"`
	Cwd        string          `json:"cwd"`
	Target     string          `json:"target,omitempty"`
	Runtime    approvalRuntime `json:"runtime"`
}

type approvalRuntime struct {
	PID          int    `json:"pid"`
	SHA256       string `json:"sha256"`
	ActivationID string `json:"activation_id,omitempty"`
	LaunchID     string `json:"launch_id,omitempty"`
}

type approvalRequest struct {
	ExecID  string
	Details approvalDetails
}

func approvalDigest(execID string, details approvalDetails) (string, error) {
	raw, err := json.Marshal(struct {
		ExecID  string          `json:"exec_id"`
		Details approvalDetails `json:"details"`
	}{ExecID: execID, Details: details})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func detailsForExecution(req execution.Request, cmd *exec.Cmd, cwd, backend, target string) approvalDetails {
	d := approvalDetails{Backend: backend, Cwd: cwd, Target: target, Runtime: approvalRuntimeFromBuild()}
	if req.Script != nil {
		d.Kind = "script"
		d.Script = req.Script.Text
		d.Shell = req.Script.Shell
		if cmd != nil {
			d.Executable = cmd.Path
			if len(cmd.Args) > 1 {
				d.Args = append([]string(nil), cmd.Args[1:]...)
			}
		}
		return d
	}
	d.Kind = "program"
	if cmd != nil {
		d.Executable = cmd.Path
		if len(cmd.Args) > 1 {
			d.Args = append([]string(nil), cmd.Args[1:]...)
		}
	}
	return d
}

func approvalRuntimeFromBuild() approvalRuntime {
	running := buildinfo.Running()
	return approvalRuntime{PID: running.PID, SHA256: running.SHA256, ActivationID: running.ActivationID, LaunchID: running.LaunchID}
}

type decide struct {
	allow bool
	code  string // if !allow: policy_deny | approval_timeout | cancelled
}

func newApprovalHub() *approvalHub {
	return &approvalHub{
		subs:    map[frameWriter]struct{}{},
		pending: map[string]*pendingApproval{},
		byExec:  map[string]string{},
	}
}

func (h *approvalHub) subscribe(c frameWriter) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.subs[c] = struct{}{}
}

func (h *approvalHub) unsubscribe(c frameWriter) {
	h.mu.Lock()
	delete(h.subs, c)
	var pending []*pendingApproval
	if len(h.subs) == 0 {
		for id, p := range h.pending {
			delete(h.pending, id)
			pending = append(pending, p)
		}
	}
	h.mu.Unlock()
	for _, p := range pending {
		select {
		case p.ch <- decide{allow: false, code: protocol.Cancelled}:
		default:
		}
	}
}

// ask broadcasts to human UI heads. No subscriber → policy_deny without waiting.
func (h *approvalHub) ask(ctx context.Context, request approvalRequest) (allow bool, code string) {
	if ctx.Err() != nil {
		return false, protocol.Cancelled
	}
	digest, err := approvalDigest(request.ExecID, request.Details)
	if err != nil {
		return false, protocol.PolicyDeny
	}
	h.mu.Lock()
	if len(h.subs) == 0 {
		h.mu.Unlock()
		return false, protocol.PolicyDeny
	}
	id := "a" + itoa(h.seq.Add(1))
	now := time.Now().UTC()
	p := &pendingApproval{execID: request.ExecID, details: request.Details, digest: digest,
		created: now.Format(time.RFC3339), expires: now.Add(approvalWait).Format(time.RFC3339), ch: make(chan decide, 1)}
	h.pending[id] = p
	h.byExec[request.ExecID] = id
	subs := make([]frameWriter, 0, len(h.subs))
	for c := range h.subs {
		subs = append(subs, c)
	}
	h.mu.Unlock()

	waitContext, cancelWait := context.WithDeadline(ctx, now.Add(approvalWait))
	defer cancelWait()
	defer h.clear(id, request.ExecID)
	params := approvalParams(id, p)
	fr := ipc.EventParams(id, "approval", params)
	for _, c := range subs {
		if err := writeApproval(waitContext, c, fr); err != nil {
			if waitContext.Err() != nil {
				break
			}
			h.unsubscribe(c)
		}
	}
	select {
	case <-waitContext.Done():
		return false, approvalContextCode(ctx)
	case d := <-p.ch:
		if waitContext.Err() != nil {
			return false, approvalContextCode(ctx)
		}
		if d.allow {
			return true, ""
		}
		if d.code == "" {
			return false, protocol.PolicyDeny
		}
		return false, d.code
	}
}

func writeApproval(ctx context.Context, c frameWriter, frame ipc.Frame) error {
	writeContext, cancel := context.WithTimeout(ctx, approvalWriteWait)
	defer cancel()
	return c.WriteFrameContext(writeContext, frame)
}

func approvalContextCode(parent context.Context) string {
	if parent.Err() != nil {
		return protocol.Cancelled
	}
	return protocol.ApprovalTimeout
}

func (h *approvalHub) respond(id, digest string, allow bool) bool {
	h.mu.Lock()
	p := h.pending[id]
	if p != nil && p.digest != digest {
		p = nil
	}
	// Claim once, before the waiting goroutine can consume and clear its channel.
	if p != nil {
		delete(h.pending, id)
	}
	h.mu.Unlock()
	if p == nil {
		return false
	}
	code := protocol.PolicyDeny
	if allow {
		code = ""
	}
	select {
	case p.ch <- decide{allow: allow, code: code}:
		return true
	default:
		return false
	}
}

func approvalParams(id string, p *pendingApproval) map[string]any {
	return map[string]any{"approval_id": id, "exec_id": p.execID, "request_sha256": p.digest,
		"created": p.created, "expires": p.expires, "details": p.details}
}

func (h *approvalHub) cancelExec(execID string) {
	h.mu.Lock()
	id, ok := h.byExec[execID]
	p := h.pending[id]
	h.mu.Unlock()
	if !ok || p == nil {
		return
	}
	select {
	case p.ch <- decide{allow: false, code: protocol.Cancelled}:
	default:
	}
}

func (h *approvalHub) clear(id, execID string) {
	h.mu.Lock()
	delete(h.pending, id)
	if h.byExec[execID] == id {
		delete(h.byExec, execID)
	}
	h.mu.Unlock()
}

func itoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
