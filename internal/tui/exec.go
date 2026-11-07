package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

var execCh chan tea.Msg

func runExecCmd(command string) tea.Cmd {
	execCh = make(chan tea.Msg, 16)
	go func() {
		err := streamExec(command, func(s string) {
			execCh <- execChunk(s)
		})
		execCh <- execDone{err: err}
		close(execCh)
	}()
	return waitExec()
}

func waitExec() tea.Cmd {
	return func() tea.Msg {
		if execCh == nil {
			return execDone{err: fmt.Errorf("未收到任务终态，执行结果未确认。")}
		}
		msg, ok := <-execCh
		if !ok {
			return execDone{err: fmt.Errorf("未收到任务终态，执行结果未确认。")}
		}
		return msg
	}
}
