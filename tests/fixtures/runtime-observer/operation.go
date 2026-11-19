package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"

	"github.com/xallor-ai/xallor-remote/internal/buildinfo"
	"github.com/xallor-ai/xallor-remote/internal/ipc"
	"github.com/xallor-ai/xallor-remote/internal/releasetrust"
	"github.com/xallor-ai/xallor-remote/internal/runtimeowner"
)

type operationObservation struct {
	Source    string                `json:"source"`
	PeerPID   int                   `json:"peer_pid,omitempty"`
	Operation runtimeowner.Record   `json:"operation"`
	Settled   bool                  `json:"settled"`
	Observer  *buildinfo.Executable `json:"observer,omitempty"`
}

// Structural/native-peer binding only. The caller separately authenticates
// the expected baseline artifact and compares the worker's actual kernel image.
func observeOperation(profile, id, bundle string, offline bool) (result operationObservation, resultErr error) {
	if err := selectProfile(profile); err != nil {
		return result, err
	}
	request, err := (runtimeowner.Command{ID: id, Kind: "install", Bundle: bundle}).Request()
	if err != nil {
		return result, err
	}
	journal := runtimeowner.Journal{Dir: filepath.Join(filepath.Clean(profile), "runtime", "lifecycle")}
	namespace, err := journal.NamespaceID()
	if err != nil {
		return result, err
	}
	validate := func() error {
		if err := result.Operation.ValidateFor(request); err != nil {
			return err
		}
		return result.Operation.ValidateNamespace(namespace)
	}
	if offline {
		result.Operation, result.Settled, err = journal.QuerySettled(id)
		if err != nil {
			return result, err
		}
		result.Source = "journal_read_only"
		return result, validate()
	}
	err = queryPeer(profile, true, "signed-cycle-operation", "runtime.operation.status", map[string]string{"operation_id": id}, func(frame ipc.Frame, peer int) error {
		if err := json.Unmarshal(frame.Result, &result); err != nil {
			return err
		}
		if err := validate(); err != nil {
			return err
		}
		observer := result.Observer
		if observer == nil || observer.PID != peer || observer.OwnerProtocol != buildinfo.RuntimeOwnerProtocol || observer.Path == "" || observer.Protocol != buildinfo.Protocol || observer.ProfileFormat != buildinfo.ProfileFormat() {
			return errors.New("lifecycle observer identity mismatch")
		}
		if err := (releasetrust.Identity{Version: observer.Version, Revision: observer.Revision, Dirty: observer.Dirty}).Validate(); err != nil {
			return err
		}
		digest, err := hex.DecodeString(observer.SHA256)
		if err != nil || len(digest) != 32 || strings.ToLower(observer.SHA256) != observer.SHA256 {
			return errors.New("lifecycle observer digest invalid")
		}
		result.Source, result.PeerPID = "lifecycle_ipc", peer
		return nil
	})
	return result, err
}
