package cli

import (
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

// Keep Cobra's parser authoritative. Capture its selected command's original
// arguments so JSONL can still be selected after a flag that stops parsing.
func executeCLI(root *cobra.Command, args []string) error {
	root.SetArgs(args)
	command, raw, err := root.Find(args)
	if err == nil && command.Name() == "exec" && command.Parent() == root {
		command.SetFlagErrorFunc(func(command *cobra.Command, cause error) error {
			return localExecError(command.OutOrStdout(), execJSONLRequested(command.Flags(), raw), protocol.InvalidExecution, cause)
		})
	}
	return root.Execute()
}

// This reads format intent only; it never changes flags or execution input.
// Respect known value-consuming flags and -- so literal program/script input
// cannot select JSONL. The last explicit --jsonl value wins, as with pflag.
func execJSONLRequested(flags *pflag.FlagSet, args []string) bool {
	selected := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if strings.HasPrefix(arg, "--") {
			name, value, hasValue := strings.Cut(arg[2:], "=")
			if name == "jsonl" {
				selected = true
				if hasValue {
					parsed, err := strconv.ParseBool(value)
					selected = parsed || err != nil // A malformed explicit format value is itself a JSONL error.
				}
				continue
			}
			if flag := flags.Lookup(name); flag != nil && flag.NoOptDefVal == "" && !hasValue {
				i++
			}
			continue
		}
		if len(arg) > 1 && arg[0] == '-' {
			for j := 1; j < len(arg); j++ {
				if flag := flags.ShorthandLookup(arg[j : j+1]); flag != nil && flag.NoOptDefVal == "" {
					if j == len(arg)-1 {
						i++
					}
					break // Remaining characters, if any, are this flag's value.
				}
			}
		}
	}
	return selected
}
