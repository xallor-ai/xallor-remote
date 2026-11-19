package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/process"
)

// Entered only by the native test-binary dispatcher. Both fixture processes
// outlive the IPC observation deadline, so normal exit cannot satisfy revoke.
// The actual target Runtime owns their process group/Job Object; no test code
// kills a PID or substitutes a terminal result.
func runIndependentExecFixture(mode string) int {
	role := "leaf"
	if mode == "--xr-independent-exec-root" {
		role = "root"
		self, err := os.Executable()
		if err != nil {
			return 91
		}
		child := exec.Command(self, "--xr-independent-exec-leaf")
		process.HideConsole(child)
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			return 92
		}
		defer child.Wait()
	}
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"fixture": "independent-exec", "role": role, "pid": os.Getpid(), "parent_pid": os.Getppid()}); err != nil {
		return 93
	}
	time.Sleep(60 * time.Second)
	return 0
}
