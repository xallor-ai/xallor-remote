package noderun

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

var (
	reRmRoot  = regexp.MustCompile(`(?i)rm\s+-[rf]+\s+(/|/\*|\\\\[a-z]:\\?)\s*$`)
	reWinRoot = regexp.MustCompile(`(?i)remove-item\s+.*-recurse.*[a-z]:\\?\s*$`)
	reDDDev   = regexp.MustCompile(`(?i)dd\s+.*of=/dev/`)
	reMkfs    = regexp.MustCompile(`(?i)\bmkfs(\.\w+)?\b`)
	reFormat  = regexp.MustCompile(`(?i)format-volume`)
)

func denyCommand(command string) bool {
	return hardDenyCommand(command) || needsApprovalCommand(command)
}

// hardDenyCommand: always policy_deny (no approval path).
func hardDenyCommand(command string) bool {
	s := strings.TrimSpace(command)
	if s == "" {
		return false
	}
	lower := strings.ToLower(s)
	if strings.Contains(lower, `\\.`) || strings.Contains(s, `\\.\`) {
		return true
	}
	if reRmRoot.MatchString(s) || reWinRoot.MatchString(s) || reDDDev.MatchString(s) || reMkfs.MatchString(s) || reFormat.MatchString(s) {
		return true
	}
	return false
}

// needsApprovalCommand: wait for human UI if subscribed; else policy_deny.
func needsApprovalCommand(command string) bool {
	s := strings.TrimSpace(command)
	if s == "" {
		return false
	}
	lower := strings.ToLower(s)
	if osName() == "windows" {
		for _, p := range []string{"stop-computer", "restart-computer", "shutdown"} {
			if containsWord(lower, p) {
				return true
			}
		}
		return false
	}
	trim := strings.TrimLeftFunc(s, unicode.IsSpace)
	if strings.HasPrefix(strings.ToLower(trim), "sudo ") || strings.HasPrefix(strings.ToLower(trim), "sudo\t") {
		return true
	}
	for _, p := range []string{"shutdown", "reboot", "halt", "poweroff"} {
		if containsWord(lower, p) {
			return true
		}
	}
	if strings.Contains(lower, "/etc/shadow") || strings.Contains(lower, "/dev/sd") {
		return true
	}
	return false
}

func containsWord(lower, word string) bool {
	i := strings.Index(lower, word)
	if i < 0 {
		return false
	}
	if i > 0 {
		c := lower[i-1]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			return false
		}
	}
	end := i + len(word)
	if end < len(lower) {
		c := lower[end]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

func denySensitivePath(abs string) bool {
	if abs == "" {
		return false
	}
	clean := filepath.Clean(abs)
	lower := strings.ToLower(clean)
	if strings.Contains(clean, `\\.\`) || strings.HasPrefix(clean, `\\.\`) || strings.HasPrefix(lower, `//./`) {
		return true
	}
	for _, root := range sensitiveRoots() {
		if root == "" {
			continue
		}
		if pathHasPrefix(clean, root) {
			return true
		}
	}
	if osName() != "windows" {
		if strings.HasPrefix(clean, "/etc/shadow") || strings.HasPrefix(clean, "/dev/sd") || strings.HasPrefix(clean, "/dev/nvme") {
			return true
		}
	}
	return false
}

func sensitiveRoots() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	if osName() == "windows" {
		return []string{
			filepath.Join(home, ".ssh"),
			filepath.Join(home, "AppData", "Roaming", "Microsoft", "Credentials"),
			filepath.Join(home, "AppData", "Local", "Google", "Chrome", "User Data"),
			filepath.Join(home, "AppData", "Local", "Microsoft", "Edge", "User Data"),
		}
	}
	return []string{
		filepath.Join(home, ".ssh"),
		filepath.Join(home, ".gnupg"),
	}
}

func pathHasPrefix(target, root string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	if err != nil {
		return false
	}
	return rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}
