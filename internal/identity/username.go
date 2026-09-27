package identity

import (
	"os"
	"os/user"
	"strings"
)

// Username returns the local login name used as device_id (domain prefix stripped).
func Username() string {
	name := ""
	if u, err := user.Current(); err == nil {
		name = u.Username
	}
	if name == "" {
		name = os.Getenv("USERNAME")
	}
	if name == "" {
		name = os.Getenv("USER")
	}
	return stripDomain(strings.TrimSpace(name))
}

func stripDomain(s string) string {
	if i := strings.LastIndexAny(s, `\/`); i >= 0 && i+1 < len(s) {
		return s[i+1:]
	}
	return s
}
