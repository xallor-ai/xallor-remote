package cli

import (
	"fmt"
	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"os"
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
	live, err := (runtimeControl{}).Running()
	if err != nil {
		return err
	}
	if live != nil {
		if live.ProfileFormat != buildinfo.ProfileFormat() || live.Protocol != buildinfo.Protocol {
			return fmt.Errorf("正在运行的 Runtime 与当前数据格式不匹配，请先停止服务并检查配套安装与备份。")
		}
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
