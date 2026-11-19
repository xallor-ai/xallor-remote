// runtime-observer is a read-only acceptance helper, not a product launcher.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/release"
)

type observation struct {
	PeerPID int                        `json:"peer_pid"`
	Runtime release.RuntimeObservation `json:"runtime"`
}

func observe(profile string) (result observation, resultErr error) {
	err := queryPeer(profile, false, "signed-cycle-owner", "runtime.status", map[string]any{}, func(frame ipc.Frame, peer int) error {
		if err := json.Unmarshal(frame.Result, &result); err != nil {
			return err
		}
		if err := result.Runtime.ValidateReady(buildinfo.ProfileFormat(), buildinfo.Protocol); err != nil {
			return err
		}
		if result.Runtime.PID != peer {
			return errors.New("owner PID differs from kernel IPC peer")
		}
		result.PeerPID = peer
		return nil
	})
	return result, err
}

func main() {
	profile := flag.String("profile", "", "existing isolated acceptance profile")
	operation := flag.String("operation", "", "existing lifecycle operation ID")
	bundle := flag.String("bundle", "", "expected absolute installation input")
	journal := flag.Bool("journal", false, "read the same operation journal without claiming a live peer")
	flag.Parse()
	var result any
	var err error
	if *operation != "" {
		result, err = observeOperation(*profile, *operation, *bundle, *journal)
	} else if *bundle != "" || *journal {
		err = errors.New("operation ID required for transaction observation")
	} else {
		result, err = observe(*profile)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
