package ipc

import (
	"fmt"
	"io"
)

// ConsumeExec presents validated events for human-facing CLI and TUI heads.
func ConsumeExec(requestID string, read func() (Frame, error), stdout, stderr io.Writer) error {
	return ReadExec(requestID, read, func(event ExecEvent) error {
		switch event.Type {
		case "stdout":
			_, err := fmt.Fprint(stdout, event.Data)
			return err
		case "stderr":
			_, err := fmt.Fprint(stderr, event.Data)
			return err
		case "exit":
			if event.Truncated != nil && *event.Truncated {
				_, err := fmt.Fprintln(stderr, "输出超过上限，已截断。")
				return err
			}
		}
		return nil
	})
}
