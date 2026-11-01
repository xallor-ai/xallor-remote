// Package diagnostics defines read-only, bounded connection observations.
package diagnostics

import (
	"net/url"
	"time"
)

type Check struct {
	Layer  string `json:"layer"`
	State  string `json:"state"`
	Code   string `json:"code,omitempty"`
	Action string `json:"action,omitempty"`
}

type Report struct {
	Schema     int     `json:"schema"`
	ObservedAt string  `json:"observed_at"`
	DeviceID   string  `json:"device_id,omitempty"`
	OK         bool    `json:"ok"`
	Checks     []Check `json:"checks"`
}

func New(device string) Report {
	r := Report{Schema: 1, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), DeviceID: device}
	for _, layer := range []string{"ipc", "runtime_relay", "peer", "relay_transport", "authorization", "target_online", "inbound", "target_runtime"} {
		r.Checks = append(r.Checks, Check{Layer: layer, State: "skipped"})
	}
	return r
}

func (r *Report) Set(layer, state, code, action string) {
	for i := range r.Checks {
		if r.Checks[i].Layer == layer {
			r.Checks[i] = Check{Layer: layer, State: state, Code: code, Action: action}
			return
		}
	}
}

// Endpoint deliberately omits userinfo, path, query and fragment: all may carry credentials.
func Endpoint(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}
