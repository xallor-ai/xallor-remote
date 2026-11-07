package cli

import (
	"strconv"
	"testing"

	"github.com/spf13/cobra"
	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
)

// An incompatible launcher assertion must fail before the command can initialize data.
func TestProfileGuard_should_rejectMismatch_beforeCommand(t *testing.T) {
	for _, format := range []int{0, buildinfo.ProfileFormat(), buildinfo.ProfileFormat() + 1} {
		root := &cobra.Command{Use: "test", SilenceUsage: true, SilenceErrors: true}
		called := false
		root.AddCommand(&cobra.Command{Use: "start", Run: func(*cobra.Command, []string) { called = true }})
		addProfileFormatGuard(root)
		root.SetArgs([]string{"--require-profile-format", strconv.Itoa(format), "start"})
		err := root.Execute()
		compatible := format == buildinfo.ProfileFormat()
		if called != compatible || (err == nil) != compatible {
			t.Fatalf("format=%d called=%v err=%v", format, called, err)
		}
	}
}
