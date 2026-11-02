package noderun

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/identity"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"github.com/xallor-ai/xallor-remote/internal/shell"
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
	st, err := statWorkspaceFile(workspace, p.Path)
	if err != nil {
		nackReadErr(link, msg.ExecID, err)
		return
	}
	if st.IsDir() {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.PolicyDeny))
		return
	}
	if readTooLarge(st.Size(), p) {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.TooLarge))
		return
	}
	raw, err := readWorkspaceFile(workspace, p.Path)
	if err != nil {
		nackReadErr(link, msg.ExecID, err)
		return
	}
	limit := p.Limit
	if limit <= 0 {
		limit = protocol.DefaultReadBytes
	}
	if limit > protocol.MaxReadBytes {
		limit = protocol.MaxReadBytes
	}
	if bytes.IndexByte(raw, 0) >= 0 {
		_ = link.send(protocol.Nack(msg.ExecID, protocol.PolicyDeny))
		return
	}
	out, trunc := sliceRead(raw, p, limit)
	if err := sendStdoutFrames(link, msg.ExecID, out); err != nil {
		if err == errMsgTooLarge {
			_ = link.send(protocol.Nack(msg.ExecID, protocol.TooLarge))
			return
		}
		_ = link.send(protocol.Nack(msg.ExecID, protocol.AgentError))
		return
	}
	d.sendOKExit(link, msg.ExecID, start, trunc)
}

func splitLines(raw []byte) [][]byte {
	if len(raw) == 0 {
		return nil
	}
	lines := bytes.Split(raw, []byte("\n"))
	if len(lines) > 0 && len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func sliceRead(raw []byte, p protocol.ReadPayload, limit int) ([]byte, bool) {
	if p.Head > 0 {
		lines := splitLines(raw)
		if p.Head < len(lines) {
			raw = bytes.Join(lines[:p.Head], []byte("\n"))
		}
	} else if p.Tail > 0 {
		lines := splitLines(raw)
		if p.Tail < len(lines) {
			raw = bytes.Join(lines[len(lines)-p.Tail:], []byte("\n"))
		}
	} else if p.Offset > 0 || p.Limit > 0 {
		off := snapUTF8Start(raw, p.Offset)
		if off >= len(raw) {
			return nil, false
		}
		end := len(raw)
		if p.Limit > 0 && off+p.Limit < end {
			end = off + p.Limit
		}
		raw = raw[off:end]
	}
	cut := false
	if len(raw) > limit {
		raw = raw[:limit]
		cut = true
	}
	out := clampUTF8Prefix(raw)
	if cut || len(out) < len(raw) {
		return out, true
	}
	return out, false
}

func clampUTF8Prefix(b []byte) []byte {
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	return b
}

// snapUTF8Start moves a byte offset back to the start of the rune that contains it.
func snapUTF8Start(raw []byte, off int) int {
	if off <= 0 {
		return 0
	}
	if off >= len(raw) {
		return len(raw)
	}
	for off > 0 && !utf8.RuneStart(raw[off]) {
		off--
	}
	return off
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
	if err := writeWorkspaceFile(workspace, p.Path, p.Content); err != nil {
		code := protocol.AgentError
		if pathDenied(err) {
			code = protocol.PolicyDeny
		}
		_ = link.send(protocol.Nack(msg.ExecID, code))
		return
	}
	d.sendOKExit(link, msg.ExecID, start, false)
}

func readTooLarge(size int64, p protocol.ReadPayload) bool {
	if p.Head != 0 || p.Tail != 0 || p.Limit != 0 || p.Offset != 0 {
		return false
	}
	return size > int64(protocol.MaxReadBytes)
}

func pathDenied(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "outside workspace") ||
		strings.Contains(s, "path too long") ||
		strings.Contains(s, "sensitive path") ||
		strings.Contains(s, "path escapes from parent")
}

func nackReadErr(link *wsConn, execID string, err error) {
	if os.IsNotExist(err) {
		_ = link.send(protocol.Nack(execID, protocol.AgentError))
		return
	}
	_ = link.send(protocol.Nack(execID, protocol.PolicyDeny))
}

func stdoutFrameBudget() int {
	max := protocol.MaxFrameBytes
	wire := protocol.MaxWSMessageBytes - protocol.MaxEnvelopeBytes
	if wire > 0 && max > wire/6 {
		max = wire / 6
	}
	if max < 1 {
		return 1
	}
	return max
}

func nextUTF8Frame(data []byte, max int) (frame, rest []byte) {
	i := 0
	for i < len(data) && !utf8.RuneStart(data[i]) {
		i++
	}
	data = data[i:]
	if len(data) == 0 {
		return nil, nil
	}
	if max > len(data) {
		max = len(data)
	}
	for max > 0 && !utf8.Valid(data[:max]) {
		max--
	}
	if max == 0 {
		return nil, nil
	}
	return data[:max], data[max:]
}

func sendStdoutFrames(link *wsConn, execID string, data []byte) error {
	max := stdoutFrameBudget()
	for len(data) > 0 {
		frame, rest := nextUTF8Frame(data, max)
		if len(frame) == 0 {
			return nil
		}
		if err := link.send(protocol.Message{Type: protocol.TypeStdout, ExecID: execID, Data: string(frame)}); err != nil {
			if err == errMsgTooLarge && max > 1 {
				max /= 2
				continue
			}
			return err
		}
		data = rest
	}
	return nil
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
	d.sendOKExit(link, msg.ExecID, start, false)
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
		"device_id":       id,
		"os":              goos,
		"arch":            arch,
		"hostname":        identity.Hostname(),
		"version":         buildinfo.Version(),
		"build":           buildinfo.Current(),
		"workspace":       workspace,
		"shell":           shell.Describe(d.store.ConfigSnapshot().Shell),
		"execution_modes": []string{"script", "program"},
		"docker":          false,
		"nvidia":          false,
	})
	_ = link.send(protocol.Message{Type: protocol.TypeStdout, ExecID: msg.ExecID, Data: string(body)})
	d.sendOKExit(link, msg.ExecID, start, false)
}

func (d *Daemon) sendOKExit(link *wsConn, execID string, start time.Time, truncated bool) {
	code := 0
	dur := time.Since(start).Milliseconds()
	_ = link.send(protocol.Message{
		Type: protocol.TypeExit, ExecID: execID, ExitCode: &code,
		DurationMS: &dur, Truncated: protocol.Bool(truncated), Status: protocol.ExitCompleted,
	})
}

func listProcesses() ([]byte, error) {
	cmd := exec.Command("tasklist")
	if osName() != "windows" {
		cmd = exec.Command("ps", "-u", os.Getenv("USER"), "-o", "pid,comm")
	}
	return cmd.Output()
}
