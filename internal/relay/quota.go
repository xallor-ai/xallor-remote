package relay

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type Quota struct {
	Enabled             bool
	MaxDeviceConnsPerIP int
	HelloPerMinute      int
	BytesPerMinute      int64
}

func (q Quota) withDefaults() Quota {
	if q.MaxDeviceConnsPerIP <= 0 {
		q.MaxDeviceConnsPerIP = 8
	}
	if q.HelloPerMinute <= 0 {
		q.HelloPerMinute = 30
	}
	if q.BytesPerMinute <= 0 {
		q.BytesPerMinute = 64 << 20
	}
	return q
}

type ipStat struct {
	hellos  []time.Time
	devices int
	bytes   int64
	window  time.Time
}

type Limiter struct {
	mu  sync.Mutex
	cfg Quota
	ips map[string]*ipStat
}

func NewLimiter(q Quota) *Limiter {
	return &Limiter{cfg: q.withDefaults(), ips: map[string]*ipStat{}}
}

func ClientIP(r *http.Request) string {
	if x := r.Header.Get("X-Forwarded-For"); x != "" {
		return strings.TrimSpace(strings.Split(x, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (l *Limiter) AllowDeviceHello(ip string) string {
	if l == nil || !l.cfg.Enabled {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	st := l.stat(ip)
	now := time.Now()
	cut := now.Add(-time.Minute)
	kept := st.hellos[:0]
	for _, t := range st.hellos {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	st.hellos = append(kept, now)
	if len(st.hellos) > l.cfg.HelloPerMinute {
		return protocol.QuotaExceeded
	}
	if st.devices >= l.cfg.MaxDeviceConnsPerIP {
		return protocol.QuotaExceeded
	}
	return ""
}

func (l *Limiter) OnDeviceUp(ip string) {
	if l == nil || !l.cfg.Enabled {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.stat(ip).devices++
}

func (l *Limiter) OnDeviceDown(ip string) {
	if l == nil || !l.cfg.Enabled {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	st := l.stat(ip)
	if st.devices > 0 {
		st.devices--
	}
}

func (l *Limiter) AddBytes(ip string, n int) string {
	if l == nil || !l.cfg.Enabled || n <= 0 {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	st := l.stat(ip)
	now := time.Now()
	if st.window.IsZero() || now.Sub(st.window) >= time.Minute {
		st.window = now
		st.bytes = 0
	}
	st.bytes += int64(n)
	if st.bytes > l.cfg.BytesPerMinute {
		return protocol.QuotaExceeded
	}
	return ""
}

func (l *Limiter) stat(ip string) *ipStat {
	st := l.ips[ip]
	if st == nil {
		st = &ipStat{}
		l.ips[ip] = st
	}
	return st
}
