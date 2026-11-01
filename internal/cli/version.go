package cli

import (
	"encoding/json"
	"fmt"
	"github.com/spf13/cobra"
	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
)

func cmdVersion() *cobra.Command {
	var jsonOutput bool
	c := &cobra.Command{Use: "version", Short: "查看当前程序版本", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		i := buildinfo.Running()
		if jsonOutput {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(i)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "XallorRemote %s\nCommit: %s\nBuilt: %s\n", i.Version, i.Revision, i.BuiltAt)
		return nil
	}}
	c.Flags().BoolVar(&jsonOutput, "json", false, "输出 JSON")
	return c
}
