package relay

import (
	"encoding/json"
	"net/http"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
)

func writeHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"service": "relay",
		"version": buildinfo.Version(),
		"build":   buildinfo.Current(),
	})
}
