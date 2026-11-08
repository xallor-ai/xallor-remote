package relay

import (
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"time"
)

// Transport owns listener security; routing and authorization stay in Hub.
type Transport struct {
	CertFile       string
	KeyFile        string
	ReverseProxy   bool
	TrustedProxies []string
	AllowedOrigins []string
}

func (t Transport) Validate(addr string) error {
	if _, err := t.policy(); err != nil {
		return err
	}
	if (t.CertFile == "") != (t.KeyFile == "") {
		return errors.New("请同时设置 --tls-cert 和 --tls-key")
	}
	if t.ReverseProxy && (t.CertFile != "" || len(t.TrustedProxies) == 0) {
		return errors.New("--reverse-proxy 需要 --trusted-proxy，且不能与内置 TLS 同用")
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	ip, err := netip.ParseAddr(host)
	if t.CertFile == "" && !t.ReverseProxy && (err != nil || !ip.IsLoopback()) {
		return errors.New("非本机监听需要 --tls-cert/--tls-key 或 --reverse-proxy/--trusted-proxy")
	}
	return nil
}

func Serve(addr string, hub *Hub, log *slog.Logger, quota Quota, transport Transport) error {
	srv, err := transport.server(addr, hub, log, quota)
	if err != nil {
		return err
	}
	if log != nil {
		log.Info("relay listen", "addr", addr, "tls", transport.CertFile != "", "reverse_proxy", transport.ReverseProxy)
	}
	if transport.CertFile != "" {
		return srv.ListenAndServeTLS(transport.CertFile, transport.KeyFile)
	}
	return srv.ListenAndServe()
}

func (transport Transport) server(addr string, hub *Hub, log *slog.Logger, quota Quota) (*http.Server, error) {
	if err := transport.Validate(addr); err != nil {
		return nil, err
	}
	p, err := transport.policy()
	if err != nil {
		return nil, err
	}
	var lim *Limiter
	if quota.Enabled {
		lim = NewLimiter(quota)
	}
	srv := &http.Server{Addr: addr, Handler: handlerPolicy(hub, log, lim, p), ReadHeaderTimeout: 10 * time.Second,
		TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	return srv, nil
}
