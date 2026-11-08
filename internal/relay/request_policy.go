package relay

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

type requestPolicy struct {
	proxies []netip.Prefix
	origins map[string]bool
	reverse bool
}

func (t Transport) policy() (requestPolicy, error) {
	p := requestPolicy{origins: map[string]bool{}, reverse: t.ReverseProxy}
	for _, value := range t.TrustedProxies {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			ip, e := netip.ParseAddr(value)
			if e != nil {
				return p, errors.New("--trusted-proxy 需要 IP 或 CIDR")
			}
			ip = ip.Unmap()
			prefix = netip.PrefixFrom(ip, ip.BitLen())
		}
		if prefix.Addr().Is4In6() {
			if prefix.Bits() < 96 {
				return p, errors.New("IPv4-mapped 代理 CIDR 的前缀长度必须至少为 96")
			}
			prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-96)
		}
		if prefix.Bits() == 0 {
			return p, errors.New("--trusted-proxy 不能信任所有来源")
		}
		p.proxies = append(p.proxies, prefix)
	}
	for _, value := range t.AllowedOrigins {
		origin, ok := canonicalOrigin(value)
		if !ok {
			return p, errors.New("--allowed-origin 需要完整 http(s) Origin，不能包含路径或通配符")
		}
		p.origins[origin] = true
	}
	return p, nil
}

func canonicalOrigin(value string) (string, bool) {
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
		u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || value != u.Scheme+"://"+u.Host || strings.ContainsAny(u.Host, "* ,\\\t\r\n") {
		return "", false
	}
	return u.Scheme + "://" + strings.ToLower(u.Host), true
}

func remoteIP(r *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip, _ := netip.ParseAddr(host)
	return ip.Unmap()
}

func (p requestPolicy) trusted(ip netip.Addr) bool {
	for _, network := range p.proxies {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// clientIP peels from the socket inward. Values left of the first untrusted
// hop are client-controlled and cannot change quota attribution.
func (p requestPolicy) clientIP(r *http.Request) string {
	peer := remoteIP(r)
	if !peer.IsValid() {
		return r.RemoteAddr
	}
	if !p.trusted(peer) {
		return peer.String()
	}
	values := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	ips := make([]netip.Addr, len(values))
	for i, value := range values {
		ip, err := netip.ParseAddr(strings.TrimSpace(value))
		if err != nil {
			return peer.String()
		}
		ips[i] = ip.Unmap()
	}
	current := peer
	for i := len(ips) - 1; i >= 0 && p.trusted(current); i-- {
		current = ips[i]
	}
	return current.String()
}

func (p requestPolicy) allowTransport(r *http.Request) bool {
	if !p.reverse {
		return true
	}
	values := r.Header.Values("X-Forwarded-Proto")
	return p.trusted(remoteIP(r)) && len(values) == 1 && values[0] == "https"
}

func (p requestPolicy) allowOrigin(r *http.Request) bool {
	values := r.Header.Values("Origin")
	if len(values) == 0 {
		return true
	}
	if len(values) != 1 {
		return false
	}
	origin, ok := canonicalOrigin(values[0])
	return ok && p.origins[origin]
}

// ClientIP defaults to socket attribution; forwarded headers need policy.
func ClientIP(r *http.Request) string { return (requestPolicy{}).clientIP(r) }

func originPatterns(p requestPolicy) []string {
	patterns := make([]string, 0, len(p.origins))
	for origin := range p.origins {
		// coder/websocket uses path.Match, so IPv6 brackets need literal escaping.
		patterns = append(patterns, strings.NewReplacer("[", "\\[", "]", "\\]", "?", "\\?").Replace(origin))
	}
	return patterns
}
