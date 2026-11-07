package protocol

import (
	"encoding/json"
	"fmt"

	"github.com/xallor-ai/xallor-remote/internal/invocation"
)

// Message is the WSS JSON envelope. Unused fields stay omitted.
type Message struct {
	Type          string                    `json:"type"`
	Role          string                    `json:"role,omitempty"`
	DeviceID      string                    `json:"device_id,omitempty"`
	TargetID      string                    `json:"target_device_id,omitempty"`
	Secret        string                    `json:"secret,omitempty"`
	Grant         string                    `json:"grant,omitempty"`
	AccountToken  string                    `json:"account_token,omitempty"`
	ClientAuth    *ClientAuthentication     `json:"client_auth,omitempty"`
	RequestID     string                    `json:"request_id,omitempty"`
	Delegation    string                    `json:"delegation,omitempty"`
	LeaseMS       int64                     `json:"lease_ms,omitempty"`
	Scope         *invocation.Scope         `json:"scope,omitempty"`
	DeviceContext *invocation.DeviceContext `json:"device_context,omitempty"`
	GrantHash     string                    `json:"grant_hash,omitempty"`
	OS            string                    `json:"os,omitempty"`
	Arch          string                    `json:"arch,omitempty"`
	AgentVersion  string                    `json:"agent_version,omitempty"`
	Workspace     string                    `json:"workspace,omitempty"`
	Inbound       *bool                     `json:"inbound,omitempty"`
	ExecID        string                    `json:"exec_id,omitempty"`
	Op            string                    `json:"op,omitempty"`
	Payload       json.RawMessage           `json:"payload,omitempty"`
	Code          string                    `json:"code,omitempty"`
	Data          string                    `json:"data,omitempty"`
	ExitCode      *int                      `json:"exit_code,omitempty"`
	DurationMS    *int64                    `json:"duration_ms,omitempty"`
	Truncated     *bool                     `json:"truncated,omitempty"`
	Status        string                    `json:"status,omitempty"`
}

type ReadPayload struct {
	Path   string `json:"path"`
	Head   int    `json:"head,omitempty"`
	Tail   int    `json:"tail,omitempty"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type WritePayload struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (m Message) Bytes() ([]byte, error) {
	return json.Marshal(m)
}

func Parse(b []byte) (Message, error) {
	var m Message
	if err := json.Unmarshal(b, &m); err != nil {
		return Message{}, err
	}
	if m.Type == "" {
		return Message{}, fmt.Errorf("missing type")
	}
	return m, nil
}

func Bool(v bool) *bool { return &v }

func Int(v int) *int { return &v }

func Int64(v int64) *int64 { return &v }

func Nack(execID, code string) Message {
	return Message{Type: TypeInvokeNack, ExecID: execID, Code: code}
}

func Err(execID, code string) Message {
	return Message{Type: TypeError, ExecID: execID, Code: code}
}
