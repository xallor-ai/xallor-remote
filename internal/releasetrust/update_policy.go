package releasetrust

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/xallor-ai/xallor-remote/internal/atomicfile"
)

type MetadataVersions struct {
	Root      int `json:"root"`
	Timestamp int `json:"timestamp"`
	Snapshot  int `json:"snapshot"`
	Targets   int `json:"targets"`
}

type metadataPolicyState struct {
	Version  int              `json:"version"`
	Accepted MetadataVersions `json:"accepted"`
	Root     *RootMetadata    `json:"root,omitempty"`
}

type MetadataPolicy struct {
	path  string
	state metadataPolicyState
}

func OpenMetadataPolicy(path string) *MetadataPolicy {
	p := &MetadataPolicy{path: path, state: metadataPolicyState{Version: 1}}
	if !isAbsolute(path) {
		p.state.Version = 0
		return p
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return p
	}
	var envelope struct {
		Version  int               `json:"version"`
		Accepted *MetadataVersions `json:"accepted"`
		Root     *RootMetadata     `json:"root,omitempty"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	decodeErr := decoder.Decode(&envelope)
	if err != nil || decodeErr != nil || envelope.Accepted == nil || envelope.Version != 1 || envelope.Accepted.Root < 0 || envelope.Accepted.Timestamp < 0 || envelope.Accepted.Snapshot < 0 || envelope.Accepted.Targets < 0 {
		p.state.Version = 0
	} else {
		p.state = metadataPolicyState{Version: envelope.Version, Accepted: *envelope.Accepted, Root: envelope.Root}
		if p.state.Root != nil && validateRoot(*p.state.Root) != nil {
			p.state.Version = 0
		}
	}
	return p
}

func (p *MetadataPolicy) State() (MetadataVersions, error) {
	if p.state.Version != 1 {
		return MetadataVersions{}, ErrMetadataInvalid
	}
	return p.state.Accepted, nil
}

// Accept verifies a bundle against the current root and atomically persists
// accepted role versions. It has no installation or execution side effects.
func (p *MetadataPolicy) Accept(bundle MetadataBundle, root RootMetadata, now time.Time) (MetadataVersions, error) {
	current, err := p.State()
	if err != nil {
		return MetadataVersions{}, err
	}
	if p.state.Root != nil {
		left, _ := json.Marshal(*p.state.Root)
		right, _ := json.Marshal(root)
		if !bytes.Equal(left, right) {
			return MetadataVersions{}, ErrMetadataInvalid
		}
	}
	verified, err := VerifyBundle(bundle, root, current.Root, current.Timestamp, current.Snapshot, current.Targets, now)
	if err != nil {
		return MetadataVersions{}, err
	}
	next := MetadataVersions{Root: verified.Root.Version, Timestamp: verified.Timestamp.Version, Snapshot: verified.Snapshot.Version, Targets: verified.Targets.Version}
	state := metadataPolicyState{Version: 1, Accepted: next, Root: &verified.Root}
	raw, err := json.Marshal(state)
	if err != nil {
		return MetadataVersions{}, err
	}
	if err := atomicfile.Write(p.path, raw); err != nil {
		return MetadataVersions{}, err
	}
	p.state = state
	return next, nil
}

func isAbsolute(path string) bool {
	if path == "" || path[0] == '/' || path[0] == '\\' {
		return true
	}
	return len(path) > 1 && path[1] == ':'
}
