package cli

import (
	"encoding/json"
	"strings"

	"github.com/xallor-ai/xallor-remote/internal/ipc"
)

type independentMCPToolResult struct {
	IsError bool `json:"isError"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

func (c *independentMCPClient) tool(name string, args any) independentMCPToolResult {
	c.t.Helper()
	return c.toolResult(c.send("tools/call", map[string]any{"name": name, "arguments": args}))
}

func (c *independentMCPClient) toolResult(id string) independentMCPToolResult {
	c.t.Helper()
	var result independentMCPToolResult
	if json.Unmarshal(c.receive(id), &result) != nil || len(result.Content) != 1 || result.Content[0].Type != "text" {
		c.t.Fatal("MCP tool response shape incorrect")
	}
	if strings.Contains(result.Content[0].Text, "independent-exec-secret") {
		c.t.Fatal("MCP tool leaked the authorization secret")
	}
	return result
}

func (c *independentMCPClient) success(result independentMCPToolResult) string {
	c.t.Helper()
	if result.IsError {
		c.t.Fatal("MCP unexpectedly refused an approved operation")
	}
	return result.Content[0].Text
}

func (c *independentMCPClient) denied(result independentMCPToolResult, code string) {
	c.t.Helper()
	if !result.IsError || !strings.Contains(result.Content[0].Text, ipc.Human(code)) {
		c.t.Fatal("MCP did not preserve the expected authorization refusal", code)
	}
}

func (c *independentMCPClient) sameOwner(p *independentProfile) {
	c.t.Helper()
	text := c.success(c.tool("remote.devices", map[string]any{}))
	var status struct {
		Build struct {
			PID int    `json:"pid"`
			SHA string `json:"sha256"`
		} `json:"build"`
	}
	if json.Unmarshal([]byte(text), &status) != nil || status.Build.PID != p.current.PID || status.Build.SHA != p.current.SHA256 {
		c.t.Fatal("MCP did not reuse the independently observed Runtime owner")
	}
}
