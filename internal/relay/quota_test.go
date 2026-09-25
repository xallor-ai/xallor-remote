package relay

import (
	"net/http"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

func TestLimiterDisabledAlwaysAllows(t *testing.T) {
	lim := NewLimiter(Quota{})
	if lim.AllowDeviceHello("1.1.1.1") != "" {
		t.Fatal("disabled quota must allow")
	}
}

func TestLimiterHelloPerMinute(t *testing.T) {
	lim := NewLimiter(Quota{Enabled: true, HelloPerMinute: 2, MaxDeviceConnsPerIP: 8})
	if lim.AllowDeviceHello("10.0.0.1") != "" || lim.AllowDeviceHello("10.0.0.1") != "" {
		t.Fatal("first two hellos")
	}
	if lim.AllowDeviceHello("10.0.0.1") != protocol.QuotaExceeded {
		t.Fatal("third hello must quota")
	}
	if lim.AllowDeviceHello("10.0.0.2") != "" {
		t.Fatal("other IP must be independent")
	}
}

func TestLimiterDeviceConnsPerIP(t *testing.T) {
	lim := NewLimiter(Quota{Enabled: true, HelloPerMinute: 30, MaxDeviceConnsPerIP: 1})
	if lim.AllowDeviceHello("9.9.9.9") != "" {
		t.Fatal("first")
	}
	lim.OnDeviceUp("9.9.9.9")
	if lim.AllowDeviceHello("9.9.9.9") != protocol.QuotaExceeded {
		t.Fatal("second device conn")
	}
	lim.OnDeviceDown("9.9.9.9")
	if lim.AllowDeviceHello("9.9.9.9") != "" {
		t.Fatal("after down")
	}
}

func TestClientIPPrefersXForwardedFor(t *testing.T) {
	r := &http.Request{RemoteAddr: "127.0.0.1:9", Header: http.Header{"X-Forwarded-For": []string{"8.8.8.8, 1.1.1.1"}}}
	if got := ClientIP(r); got != "8.8.8.8" {
		t.Fatalf("got %s", got)
	}
}
