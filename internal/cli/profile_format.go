package cli

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
)

// Every automatic launcher supplies this assertion before the command. Older
// binaries reject the unknown flag before opening or initializing a profile.
func addProfileFormatGuard(root *cobra.Command) {
	required := buildinfo.ProfileFormat()
	root.PersistentFlags().IntVar(&required, "require-profile-format", required, "Required local profile format")
	_ = root.PersistentFlags().MarkHidden("require-profile-format")
	root.PersistentPreRunE = func(_ *cobra.Command, _ []string) error {
		if required != buildinfo.ProfileFormat() {
			return fmt.Errorf("本机数据格式不匹配，请使用配套的 Runtime 与数据备份。")
		}
		return nil
	}
}

func guardedArgs(command string) []string {
	return []string{"--require-profile-format", strconv.Itoa(buildinfo.ProfileFormat()), command}
}
