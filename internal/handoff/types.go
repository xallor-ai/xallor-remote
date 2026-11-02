// Package handoff owns bounded, durable Agent handoff receipts. It never routes
// traffic, interprets Relay envelopes, or reads an Agent's private session store.
package handoff

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

const MaxReceipts = 128
const MaxResponse = 64 * 1024
const maxReceiptBytes = 512 * 1024

var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
var sessionPattern = regexp.MustCompile(`^[a-fA-F0-9]{8}(-[a-fA-F0-9]{4}){3}-[a-fA-F0-9]{12}$`)
var revisionPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)

type Attachment struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type Request struct {
	ID             string       `json:"request_id"`
	Workspace      string       `json:"workspace"`
	Revision       string       `json:"revision"`
	Agent          string       `json:"agent"`
	Session        string       `json:"session_id"`
	Mode           string       `json:"mode"`
	Prompt         string       `json:"prompt"`
	Attachments    []Attachment `json:"attachments,omitempty"`
	TrustWorkspace bool         `json:"trust_workspace"`
	TimeoutSeconds int          `json:"timeout_seconds"`
}

type Receipt struct {
	ID          string `json:"request_id"`
	Fingerprint string `json:"fingerprint"`
	Workspace   string `json:"workspace"`
	Revision    string `json:"revision"`
	Agent       string `json:"agent"`
	Session     string `json:"session_id"`
	Mode        string `json:"mode"`
	State       string `json:"state"`
	Code        string `json:"code,omitempty"`
	Created     string `json:"created_at"`
	Updated     string `json:"updated_at"`
	CLIVersion  string `json:"cli_version,omitempty"`
	Result      string `json:"result,omitempty"`
	Truncated   bool   `json:"truncated"`
}

func (r Request) validate() error {
	if !idPattern.MatchString(r.ID) || !sessionPattern.MatchString(r.Session) || !revisionPattern.MatchString(r.Revision) {
		return fmt.Errorf("需要明确的请求 ID、会话 UUID 和完整提交 SHA。")
	}
	if r.Agent != "cursor" || (r.Mode != "ask" && r.Mode != "plan" && r.Mode != "agent") {
		return fmt.Errorf("需要 cursor 与明确的 ask / plan / agent 模式。")
	}
	if !filepath.IsAbs(r.Workspace) || len(r.Workspace) > 1024 || strings.ContainsRune(r.Workspace, 0) {
		return fmt.Errorf("需要目标仓库的绝对路径。")
	}
	if len(r.Prompt) == 0 || len(r.Prompt) > 16*1024 || !utf8.ValidString(r.Prompt) || strings.ContainsRune(r.Prompt, 0) {
		return fmt.Errorf("交接内容为空或超过 16 KiB。")
	}
	if r.TimeoutSeconds < 1 || r.TimeoutSeconds > 600 || len(r.Attachments) > 8 {
		return fmt.Errorf("超时需为 1–600 秒，附件最多 8 个。")
	}
	for _, a := range r.Attachments {
		if a.Path == "" || len(a.Path) > 1024 || filepath.IsAbs(a.Path) || strings.ContainsRune(a.Path, 0) || len(a.SHA256) != 64 {
			return fmt.Errorf("附件需要仓库内相对路径和 SHA256。")
		}
		if _, err := hex.DecodeString(a.SHA256); err != nil {
			return fmt.Errorf("附件 SHA256 无效。")
		}
	}
	return nil
}

func fingerprint(r Request) string {
	b, _ := json.Marshal(r)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func terminal(state string) bool {
	return state == "completed" || state == "failed" || state == "unknown"
}
