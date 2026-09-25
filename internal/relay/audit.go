package relay

import (
	"encoding/json"
	"strings"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func (h *Hub) record(deviceID, op, execID, decision, code string, payload []byte) {
	as, ok := h.store.(AuditStore)
	if !ok || as == nil {
		return
	}
	preview, digest := auditPreview(op, payload)
	go as.AppendAudit(AuditEntry{
		DeviceID:    deviceID,
		Op:          op,
		ExecID:      execID,
		Decision:    decision,
		Code:        code,
		ArgsPreview: preview,
		ArgsDigest:  digest,
	})
}

func auditPreview(op string, payload []byte) (string, string) {
	if len(payload) == 0 {
		return "", ""
	}
	digest := protocol.SHA256Hex(string(payload))
	switch op {
	case protocol.OpExec:
		p, err := protocol.ParseExecPayload(payload)
		if err != nil {
			return "", digest
		}
		return clip64(p.Command), digest
	case protocol.OpRead:
		var p protocol.ReadPayload
		if json.Unmarshal(payload, &p) == nil {
			return clip64(p.Path), digest
		}
	case protocol.OpWrite:
		var p protocol.WritePayload
		if json.Unmarshal(payload, &p) == nil {
			return clip64(p.Path), digest
		}
	}
	return "", digest
}

func clip64(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 64 {
		return s
	}
	return s[:64]
}
