package mcpconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const ServerKey = "xallor-remote"

type Options struct {
	DeviceID string
	Grant    string
	Command  string
}

type Result struct {
	Path    string
	Changed bool
	Created bool
}

func DefaultPath() (string, error) {
	if p := os.Getenv("CURSOR_MCP_PATH"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cursor", "mcp.json"), nil
}

// Merge idempotently adds mcpServers.xallor-remote. Existing key is left untouched.
func Merge(path string, opt Options) (Result, error) {
	res := Result{Path: path}
	if opt.Command == "" {
		opt.Command = "xallor-remote-mcp"
	}
	raw, err := os.ReadFile(path)
	var root map[string]any
	if err != nil {
		if !os.IsNotExist(err) {
			return res, err
		}
		root = map[string]any{}
		res.Created = true
	} else {
		if err := json.Unmarshal(raw, &root); err != nil {
			return res, fmt.Errorf("mcp.json 不是合法 JSON")
		}
		if root == nil {
			root = map[string]any{}
		}
	}
	servers, _ := root["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
		root["mcpServers"] = servers
	}
	if _, exists := servers[ServerKey]; exists {
		return res, nil
	}
	env := map[string]string{
		"XALLOR_REMOTE_DEVICE_ID":    placeholder(opt.DeviceID, "对方用户名"),
		"XALLOR_REMOTE_DEVICE_GRANT": placeholder(opt.Grant, "xr_grant_对方授权码"),
	}
	servers[ServerKey] = map[string]any{
		"command": opt.Command,
		"env":     env,
	}
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return res, err
	}
	out = append(out, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return res, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return res, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return res, err
	}
	res.Changed = true
	return res, nil
}

func placeholder(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
