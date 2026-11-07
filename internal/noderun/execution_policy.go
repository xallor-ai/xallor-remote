package noderun

import (
	"path/filepath"
	"strings"

	"github.com/xallor-ai/xallor-remote/internal/execution"
)

// This text is only for the existing conservative policy heuristic and UI
// preview. It is never executed or passed to a shell in program mode.
func executionPolicyText(r execution.Request) string {
	if r.Script != nil {
		return r.Script.Text
	}
	name := filepath.Base(r.Program.Executable)
	return strings.Join(append([]string{name}, r.Program.Args...), " ")
}
