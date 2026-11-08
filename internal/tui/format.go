package tui

import (
	"fmt"
	"strings"
)

func inboundLine(inbound bool, hasGrant bool) string {
	if inbound && hasGrant {
		return "开"
	}
	if hasGrant {
		return "关"
	}
	return "关（还没有授权码）"
}

func formatHome(st map[string]any) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Device ID: %v\n", st["device_id"])
	fmt.Fprintf(&b, "Workspace: %v\n", st["workspace"])
	fmt.Fprintf(&b, "Relay:     %v\n", st["relay"])
	inb, _ := st["inbound"].(bool)
	has, _ := st["has_grant"].(bool)
	fmt.Fprintf(&b, "入站:      %s\n", inboundLine(inb, has))
	fmt.Fprintf(&b, "在线:      %v\n", st["online"])
	fmt.Fprintf(&b, "版本:      %v\n", st["version"])
	return b.String()
}

func formatPeers(ids []string) string {
	if len(ids) == 0 {
		return "还没有对方设备。用 CLI：xallor-remote peer add --device … --grant …"
	}
	return strings.Join(ids, "\n")
}

func formatGrant(grant string) string {
	if grant == "" {
		return "还没有授权码。按 i 签发。"
	}
	return "授权码: " + grant + "\n把这一行给对方。"
}
