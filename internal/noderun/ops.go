package noderun

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/identity"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func (d *Daemon) runLocalRead(link *wsConn, msg protocol.Message) {
	start := time.Now()
	var p protocol.ReadPayload
	if err := json.Unmarshal(msg.Payload, &p); err != nil || p.Path == "" {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.AgentError))
		return
	}
	_, _, _, workspace, _, inbound, _ := d.store.Snapshot()
	if !inbound {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.InboundDisabled))
		return
	}
	path, err := resolveInWorkspace(workspace, p.Path)
	if err != nil {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.PolicyDeny))
		return
	}
	st, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			_ = link.send(protocol.Nack(msg.ExecID, protocol.AgentError))
			return
		}
		_ = link.send(protocol.Nack(msg.ExecID, protocol.AgentError))
		return
	}
	if st.IsDir() {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.PolicyDeny))
		return
	}
	limit := p.Limit
	if limit <= 0 {
		limit = protocol.DefaultReadBytes
	}
	if limit > protocol.MaxReadBytes {
		limit = protocol.MaxReadBytes
	}
	if st.Size() > int64(protocol.MaxReadBytes) && p.Head == 0 && p.Tail == 0 && p.Limit == 0 {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.TooLarge))
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.AgentError))
		return
	}
	if bytes.IndexByte(raw, 0) >= 0 {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.PolicyDeny))
		return
	}
	out := sliceRead(raw, p, limit)
	if len(out) > protocol.MaxReadBytes {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.TooLarge))
		return
	}
	_ = link.send(protocol.Message{Type: protocol.TypeStdout, ExecID: msg.ExecID, Data: string(out)})
	d.sendOKExit(link, msg.ExecID, start)
}

func sliceRead(raw []byte, p protocol.ReadPayload, limit int) []byte {
	if p.Head > 0 {
		lines := bytes.Split(raw, []byte("\n"))
		if p.Head < len(lines) {
			raw = bytes.Join(lines[:p.Head], []byte("\n"))
		}
	} else if p.Tail > 0 {
		lines := bytes.Split(raw, []byte("\n"))
		if p.Tail < len(lines) {
			raw = bytes.Join(lines[len(lines)-p.Tail:], []byte("\n"))
		}
	} else if p.Offset > 0 || p.Limit > 0 {
		off := p.Offset
		if off > len(raw) {
			return nil
		}
		end := len(raw)
		if p.Limit > 0 && off+p.Limit < end {
			end = off + p.Limit
		}
		raw = raw[off:end]
	}
	if len(raw) > limit {
		return raw[:limit]
	}
	return raw
}

func (d *Daemon) runLocalWrite(link *wsConn, msg protocol.Message) {
	start := time.Now()
	var p protocol.WritePayload
	if err := json.Unmarshal(msg.Payload, &p); err != nil || p.Path == "" {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.AgentError))
		return
	}
	if len(p.Content) > protocol.MaxWriteBytes {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.TooLarge))
		return
	}
	_, _, _, workspace, _, inbound, _ := d.store.Snapshot()
	if !inbound {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.InboundDisabled))
		return
	}
	path, err := resolveInWorkspace(workspace, p.Path)
	if err != nil {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.PolicyDeny))
		return
	}
	if err := os.MkdirAll(filepathDir(path), 0o700); err != nil {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.AgentError))
		return
	}
	tmp, err := os.CreateTemp(filepathDir(path), ".xr-write-*")
	if err != nil {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.AgentError))
		return
	}
	tmpName := tmp.Name()
	_, werr := tmp.Write([]byte(p.Content))
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmpName)
		_ = link.send(protocol.Nack(msg.ExecID, protocol.AgentError))
		return
	}
	if err := replaceFile(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		_ = link.send(protocol.Nack(msg.ExecID, protocol.AgentError))
		return
	}
	d.sendOKExit(link, msg.ExecID, start)
}

func filepathDir(p string) string {
	d := ""
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		d = p[:i]
	}
	if d == "" {
		return "."
	}
	return d
}

func (d *Daemon) runLocalProcesses(link *wsConn, msg protocol.Message) {
	start := time.Now()
	_, _, _, _, _, inbound, _ := d.store.Snapshot()
	if !inbound {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.InboundDisabled))
		return
	}
	out, err := listProcesses()
	if err != nil {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.AgentError))
		return
	}
	if len(out) > protocol.MaxFrameBytes {
		out = out[:protocol.MaxFrameBytes]
	}
	_ = link.send(protocol.Message{Type: protocol.TypeStdout, ExecID: msg.ExecID, Data: string(out)})
	d.sendOKExit(link, msg.ExecID, start)
}

func (d *Daemon) runLocalInfo(link *wsConn, msg protocol.Message) {
	start := time.Now()
	id, _, _, workspace, _, inbound, _ := d.store.Snapshot()
	if !inbound {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.InboundDisabled))
		return
	}
	goos, arch := identity.OSArch()
	body, _ := json.Marshal(map[string]any{
		"device_id": id,
		"os":        goos,
		"arch":      arch,
		"hostname":  identity.Hostname(),
		"version":   protocol.Version,
		"workspace": workspace,
		"docker":    false,
		"nvidia":    false,
	})
	_ = link.send(protocol.Message{Type: protocol.TypeStdout, ExecID: msg.ExecID, Data: string(body)})
	d.sendOKExit(link, msg.ExecID, start)
}

func (d *Daemon) sendOKExit(link *wsConn, execID string, start time.Time) {
	code := 0
	dur := time.Since(start).Milliseconds()
	_ = link.send(protocol.Message{
		Type: protocol.TypeExit, ExecID: execID, ExitCode: &code,
		DurationMS: &dur, Truncated: protocol.Bool(false), Status: protocol.ExitCompleted,
	})
}

func listProcesses() ([]byte, error) {
	cmd := exec.Command("tasklist")
	if osName() != "windows" {
		cmd = exec.Command("ps", "-u", os.Getenv("USER"), "-o", "pid,comm")
	}
	return cmd.Output()
}
