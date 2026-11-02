// job-reservations is an owned crash fixture, never an installed product command.
// It fills the real ledger without launching a child, publishes readiness, and
// waits to be killed so Runtime recovery is exercised without a clean Close.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/invocation"
	"github.com/xallor-ai/xallor-remote/internal/job"
)

func main() {
	if len(os.Args) == 3 && os.Args[1] == "sequence" {
		verifySequence(os.Args[2])
		return
	}
	if len(os.Args) == 3 && (os.Args[1] == "wait" || os.Args[1] == "done") {
		if err := os.WriteFile(os.Args[2], []byte("started"), 0600); err != nil {
			panic(err)
		}
		if os.Args[1] == "wait" {
			time.Sleep(time.Minute)
		}
		return
	}
	if len(os.Args) != 3 || !filepath.IsAbs(os.Args[1]) || !filepath.IsAbs(os.Args[2]) {
		panic("expected absolute profile and readiness file")
	}
	l := job.New(os.Args[1])
	defer l.Close()
	scope := invocation.Scope{Authority: strings.Repeat("a", 32), DeviceUID: strings.Repeat("b", 32), GrantID: strings.Repeat("c", 32)}
	for i := 0; i < job.Capacity; i++ {
		if _, err := l.ReserveExec(scope, strconv.Itoa(i)); err != nil {
			panic(err)
		}
	}
	rows, err := l.Snapshot()
	if err != nil {
		panic(err)
	}
	b, err := json.Marshal(rows)
	if err != nil {
		panic(err)
	}
	tmp := os.Args[2] + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		panic(err)
	}
	if err := os.Rename(tmp, os.Args[2]); err != nil {
		panic(err)
	}
	fmt.Println("reservations persisted; no child started")
	for {
		time.Sleep(time.Hour)
	}
}
