package relay

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Only numeric loopback listeners can serve cleartext without an explicit proxy.
func TestTransport_should_failClosed_whenListenerIsPublicOrConfigurationInvalid(t *testing.T) {
	for _, tc := range []struct {
		addr   string
		config Transport
		valid  bool
	}{
		{"127.0.0.1:0", Transport{}, true}, {"[::1]:0", Transport{}, true},
		{":8443", Transport{}, false}, {"0.0.0.0:8443", Transport{}, false},
		{"localhost:8443", Transport{}, false}, {"192.0.2.1:8443", Transport{}, false},
		{":8443", Transport{CertFile: "cert", KeyFile: "key"}, true},
		{"127.0.0.1:0", Transport{CertFile: "cert"}, false},
		{":8443", Transport{ReverseProxy: true}, false},
		{":8443", Transport{ReverseProxy: true, TrustedProxies: []string{"127.0.0.1"}}, true},
		{":8443", Transport{ReverseProxy: true, TrustedProxies: []string{"0.0.0.0/0"}}, false},
		{":8443", Transport{ReverseProxy: true, TrustedProxies: []string{"::ffff:0.0.0.0/96"}}, false},
		{":8443", Transport{ReverseProxy: true, TrustedProxies: []string{"::ffff:127.0.0.1/80"}}, false},
		{"127.0.0.1:0", Transport{TrustedProxies: []string{"proxy.example"}}, false},
		{"127.0.0.1:0", Transport{AllowedOrigins: []string{"https://*.example"}}, false},
		{"127.0.0.1:0", Transport{AllowedOrigins: []string{"https://example/"}}, false},
	} {
		if err := tc.config.Validate(tc.addr); (err == nil) != tc.valid {
			t.Errorf("%s %+v: %v", tc.addr, tc.config, err)
		}
	}
}

// Mapped IPv6 trusted prefixes and socket peers share IPv4 normalization;
// actual proxied upgrades and per-client budgets must work for either notation.
func TestProxy_should_attributeQuotaToClient_whenMappedIPv6PrefixIsConfigured(t *testing.T) {
	for _, prefix := range []string{"::ffff:127.0.0.1/128", "::ffff:127.0.0.0/120"} {
		t.Run(prefix, func(t *testing.T) {
			config := Transport{ReverseProxy: true, TrustedProxies: []string{prefix}}
			if err := config.Validate("127.0.0.1:0"); err != nil {
				t.Fatal(err)
			}
			p, err := config.policy()
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("GET", "/health", nil)
			r.RemoteAddr = "[::ffff:127.0.0.1]:123"
			r.Header.Set("X-Forwarded-Proto", "https")
			if !p.allowTransport(r) {
				t.Fatal("mapped socket peer not trusted")
			}
			hub := NewHub(newMemStore(), nil, nil, nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
			srv := httptest.NewServer(handlerPolicy(hub, nil, NewLimiter(Quota{Enabled: true, HelloPerMinute: 1}), p))
			defer srv.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			for i, forwarded := range []string{"198.51.100.1", "198.51.100.2", "192.0.2.200, 198.51.100.1"} {
				c, _, err := websocket.Dial(ctx, "ws"+srv.URL[4:], &websocket.DialOptions{HTTPHeader: http.Header{"X-Forwarded-For": {forwarded}, "X-Forwarded-Proto": {"https"}}})
				if err != nil {
					t.Fatal(err)
				}
				defer c.CloseNow()
				if err := wsjson.Write(ctx, c, protocol.Message{Type: protocol.TypeHelloDevice, DeviceID: forwarded, Secret: "test-secret"}); err != nil {
					t.Fatal(err)
				}
				var result protocol.Message
				if err := wsjson.Read(ctx, c, &result); err != nil {
					t.Fatal(err)
				}
				if i < 2 && result.Type != protocol.TypeHelloOK {
					t.Fatal(result)
				}
				if i == 2 && result.Code != protocol.QuotaExceeded {
					t.Fatal("spoofed leftmost IP bypassed budget", result)
				}
			}
		})
	}
}

// Right-to-left attribution preserves the first untrusted hop, including IPv6;
// malformed chains fall back to the socket instead of creating attacker keys.
func TestClientIP_should_stopAtUntrustedHop_whenForwardingChainIsSupplied(t *testing.T) {
	p, err := (Transport{TrustedProxies: []string{"127.0.0.1", "10.0.0.0/8", "::1"}}).policy()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ remote, header, want string }{
		{"192.0.2.1:1", "198.51.100.1", "192.0.2.1"},
		{"127.0.0.1:1", "198.51.100.1, 10.0.0.2", "198.51.100.1"},
		{"127.0.0.1:1", "198.51.100.1, 192.0.2.2", "192.0.2.2"},
		{"127.0.0.1:1", "garbage, 192.0.2.2", "127.0.0.1"},
		{"127.0.0.1:1", "", "127.0.0.1"},
		{"[::1]:1", "2001:db8::1", "2001:db8::1"},
		{"[::ffff:192.0.2.1]:1", "198.51.100.1", "192.0.2.1"},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = tc.remote
		r.Header.Set("X-Forwarded-For", tc.header)
		if got := p.clientIP(r); got != tc.want {
			t.Errorf("%+v got %s", tc, got)
		}
	}
}

// Proxy mode requires the configured socket peer and one HTTPS marker, even on health.
func TestProxy_should_rejectRequest_whenPeerOrHTTPSMarkerIsUntrusted(t *testing.T) {
	p, _ := (Transport{ReverseProxy: true, TrustedProxies: []string{"127.0.0.1"}}).policy()
	var logs bytes.Buffer
	h := handlerPolicy(nil, slog.New(slog.NewTextHandler(&logs, nil)), nil, p)
	for _, tc := range []struct {
		peer, proto string
		want        int
	}{
		{"192.0.2.1:1", "https", 403}, {"127.0.0.1:1", "", 403},
		{"127.0.0.1:1", "http", 403}, {"127.0.0.1:1", "https,http", 403},
		{"127.0.0.1:1", "https", 200},
	} {
		r := httptest.NewRequest("GET", "/health", nil)
		r.RemoteAddr = tc.peer
		r.Header.Set("X-Forwarded-Proto", tc.proto)
		r.Header.Set("X-Forwarded-For", "198.51.100.1")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("%+v: %d", tc, w.Code)
		}
	}
	if !strings.Contains(strings.SplitN(logs.String(), "\n", 2)[0], "client_ip=192.0.2.1") {
		t.Fatal("untrusted source changed denial attribution", logs.String())
	}
}

// IPv6 brackets are literal Origin characters, not path.Match character classes.
// A real upgrade must accept the allowlisted address and reject a different port.
func TestOrigin_should_upgradeIPv6Origin_whenExactlyAllowlisted(t *testing.T) {
	p, err := (Transport{AllowedOrigins: []string{"https://[::1]:3000"}}).policy()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handlerPolicy(nil, nil, nil, p))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, origin := range []string{"https://[::1]:3000", "https://[::1]:3001"} {
		c, resp, err := websocket.Dial(ctx, "ws"+srv.URL[4:], &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {origin}}})
		if origin == "https://[::1]:3000" {
			if err != nil {
				t.Fatal("allowlisted IPv6 Origin rejected", err)
			}
			_ = c.CloseNow()
		} else if err == nil {
			_ = c.CloseNow()
			t.Fatal("wrong Origin port accepted")
		} else if resp == nil || resp.StatusCode != 403 {
			t.Fatalf("want 403: %v", resp)
		}
	}
}

// Browser origins require exact scheme/host/port allowlisting; native peers work
// without Origin. Exercise the real upgrade rather than just the predicate.
func TestOrigin_should_rejectUnknownBrowsers_whenNativeConnectionsRemainAllowed(t *testing.T) {
	p, _ := (Transport{AllowedOrigins: []string{"https://console.example:443"}}).policy()
	srv := httptest.NewServer(handlerPolicy(nil, nil, nil, p))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, origin := range []string{"", "https://console.example:443", "https://evil.example", "https://console.example", "http://console.example:443", "null", "https://console.example:443/path"} {
		header := http.Header{}
		if origin != "" {
			header.Set("Origin", origin)
		}
		c, resp, err := websocket.Dial(ctx, "ws"+srv.URL[4:], &websocket.DialOptions{HTTPHeader: header})
		want := origin == "" || origin == "https://console.example:443"
		if (err == nil) != want {
			t.Fatalf("%q: %v", origin, err)
		}
		if c != nil {
			_ = c.CloseNow()
		} else if resp == nil || resp.StatusCode != 403 {
			t.Fatalf("expected HTTP 403: %v", resp)
		}
	}
}

// Altering XFF on a direct connection cannot create a fresh hello quota bucket.
func TestQuota_should_shareSocketBudget_whenDirectClientsForgeForwardedFor(t *testing.T) {
	hub := NewHub(newMemStore(), nil, nil, nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests}))
	lim := NewLimiter(Quota{Enabled: true, HelloPerMinute: 1})
	srv := httptest.NewServer(HandlerQuota(hub, nil, lim))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for i, forged := range []string{"198.51.100.1", "198.51.100.2"} {
		c, _, err := websocket.Dial(ctx, "ws"+srv.URL[4:], &websocket.DialOptions{HTTPHeader: http.Header{"X-Forwarded-For": {forged}}})
		if err != nil {
			t.Fatal(err)
		}
		defer c.CloseNow()
		if err := wsjson.Write(ctx, c, protocol.Message{Type: protocol.TypeHelloDevice, DeviceID: forged, Secret: "secret"}); err != nil {
			t.Fatal(err)
		}
		var result protocol.Message
		if err := wsjson.Read(ctx, c, &result); err != nil {
			t.Fatal(err)
		}
		if i == 0 && result.Type != protocol.TypeHelloOK {
			t.Fatal(result)
		}
		if i == 1 && result.Code != protocol.QuotaExceeded {
			t.Fatal(result)
		}
	}
}
