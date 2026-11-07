package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
)

// A newly copied executable can take longer to start than an already running
// Runtime takes to answer IPC. Probe it once, before stopping the current one.
const runtimeIdentityProbeTimeout = 30 * time.Second

func (runtimeControl) Check(path string) (buildinfo.Executable, error) {
	ctx, cancel := context.WithTimeout(context.Background(), runtimeIdentityProbeTimeout)
	defer cancel()
	return probeRuntimeIdentity(ctx, path)
}

func probeRuntimeIdentity(ctx context.Context, path string) (buildinfo.Executable, error) {
	var info buildinfo.Executable
	b, err := exec.CommandContext(ctx, path, "version", "--json").Output()
	if err != nil {
		if ctx.Err() != nil {
			return info, fmt.Errorf("Runtime 版本探测未在期限内完成：%w", ctx.Err())
		}
		return info, err
	}
	err = json.Unmarshal(b, &info)
	return info, err
}
