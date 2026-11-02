package cli

import (
	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"os"
	"time"
)

func ensureRuntime() error {
	s, err := runtimeStore()
	if err != nil {
		return err
	}
	unlock, err := s.Lock()
	if err != nil {
		return err
	}
	defer unlock()
	if c, err := ipc.Dial(500 * time.Millisecond); err == nil {
		_ = c.Close()
		return nil
	}
	bin, err := s.ActivePath()
	if err != nil {
		return err
	}
	if bin == "" {
		bin, err = os.Executable()
		if err != nil {
			return err
		}
	}
	sum, err := buildinfo.HashFile(bin)
	if err != nil {
		return err
	}
	return (runtimeControl{}).Start(bin, sum)
}
