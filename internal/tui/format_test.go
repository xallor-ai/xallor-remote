package tui

import (
	"strings"
	"testing"
)

func TestFormatHomeShowsDeviceAndInboundOff(t *testing.T) {
	s := formatHome(map[string]any{
		"device_id": "andy",
		"workspace": `C:\ws`,
		"relay":     "ws://127.0.0.1:18443",
		"inbound":   false,
		"has_grant": false,
		"online":    true,
		"version":   "0.1.0-dev",
	})
	if !strings.Contains(s, "andy") || !strings.Contains(s, "关（还没有授权码）") {
		t.Fatal(s)
	}
}

func TestFormatPeersEmpty(t *testing.T) {
	if !strings.Contains(formatPeers(nil), "还没有对方设备") {
		t.Fatal("empty hint")
	}
}

func TestFormatGrantEmpty(t *testing.T) {
	if !strings.Contains(formatGrant(""), "按 i 签发") {
		t.Fatal("empty grant")
	}
}
