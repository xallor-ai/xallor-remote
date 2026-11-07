package identity

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/xallor-ai/xallor-remote/internal/appdata"
	"github.com/xallor-ai/xallor-remote/internal/atomicfile"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type Peer struct {
	DeviceID string `json:"device_id"`
	Grant    string `json:"grant"`
	Name     string `json:"name,omitempty"`
}

type Config struct {
	RelayURL  string `json:"relay_url"`
	Workspace string `json:"workspace"`
	Shell     string `json:"shell,omitempty"`
}

type Store struct {
	mu         sync.Mutex
	Dir        string
	device     deviceRecord
	Config     Config
	Peers      []Peer
	account    accountCredential
	accountErr error
}

func Load() (*Store, error) {
	dir, err := appdata.DataDir()
	if err != nil {
		return nil, err
	}
	return LoadFrom(dir)
}

func LoadFrom(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{Dir: dir}
	// Parse user-controlled state before creating an identity or workspace. A
	// truncated/corrupt config must fail closed rather than silently selecting
	// the hosted Relay and making a new local profile look valid.
	for name, target := range map[string]any{"config.json": &s.Config, "peers.json": &s.Peers} {
		b, readErr := os.ReadFile(filepath.Join(dir, name))
		if os.IsNotExist(readErr) {
			continue
		}
		if readErr != nil {
			return nil, readErr
		}
		if err := json.Unmarshal(b, target); err != nil {
			return nil, fmt.Errorf("invalid %s: %w", name, err)
		}
	}
	var err error
	s.device, err = loadDeviceRecord(dir, true)
	if err != nil {
		return nil, err
	}
	if u := os.Getenv("XALLOR_REMOTE_RELAY_URL"); u != "" {
		s.Config.RelayURL = u
	} else if s.Config.RelayURL == "" {
		s.Config.RelayURL = appdata.DefaultRelayURL()
	}
	if s.Config.Workspace == "" {
		ws, err := appdata.DefaultWorkspace()
		if err != nil {
			return nil, err
		}
		s.Config.Workspace = ws
	}
	if err := os.MkdirAll(s.Config.Workspace, 0o700); err != nil {
		return nil, err
	}
	// Keep local recovery available while Credentials fails closed on bad data.
	s.accountErr = s.loadAccount()
	return s, nil
}

func (s *Store) persistPeers() error {
	b, _ := json.MarshalIndent(s.Peers, "", "  ")
	return atomicfile.Write(filepath.Join(s.Dir, "peers.json"), b)
}

func (s *Store) IssueGrant() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, err := protocol.NewGrant()
	if err != nil {
		return "", err
	}
	next := s.device
	next.Grant, next.Inbound = g, true
	if err := s.publishDevice(next); err != nil {
		return "", err
	}
	return g, nil
}

func (s *Store) AddPeer(id, grant string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, p := range s.Peers {
		if p.DeviceID == id {
			s.Peers[i].Grant = grant
			return s.persistPeers()
		}
	}
	s.Peers = append(s.Peers, Peer{DeviceID: id, Grant: grant})
	return s.persistPeers()
}

func (s *Store) Peer(id string) (Peer, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "" && len(s.Peers) == 1 {
		return s.Peers[0], true
	}
	for _, p := range s.Peers {
		if p.DeviceID == id {
			return p, true
		}
	}
	return Peer{}, false
}

func (s *Store) Snapshot() (deviceID, secret, grant, workspace, relay string, inbound bool, peers []Peer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := append([]Peer(nil), s.Peers...)
	return s.device.DeviceID, s.device.Secret, s.device.Grant, s.Config.Workspace, s.Config.RelayURL, s.device.Inbound, cp
}

func Hostname() string {
	h, _ := os.Hostname()
	return h
}

func OSArch() (string, string) {
	return runtime.GOOS, runtime.GOARCH
}
