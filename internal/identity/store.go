package identity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/xallor-ai/xallor-remote/internal/appdata"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
)

type Peer struct {
	DeviceID string `json:"device_id"`
	Grant    string `json:"grant"`
}

type Config struct {
	RelayURL  string `json:"relay_url"`
	Workspace string `json:"workspace"`
	Shell     string `json:"shell,omitempty"`
}

type Store struct {
	mu       sync.Mutex
	Dir      string
	DeviceID string
	Secret   string
	Grant    string
	Inbound  bool
	Config   Config
	Peers    []Peer
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
	idPath := filepath.Join(dir, "identity.json")
	var inboundOverride *bool
	if b, err := os.ReadFile(idPath); err == nil {
		var wrap struct {
			DeviceID string `json:"device_id"`
			Inbound  *bool  `json:"inbound"`
		}
		_ = json.Unmarshal(b, &wrap)
		s.DeviceID = wrap.DeviceID
		inboundOverride = wrap.Inbound
	}
	if b, err := os.ReadFile(filepath.Join(dir, "device_secret")); err == nil {
		s.Secret = string(b)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "grant")); err == nil {
		s.Grant = string(b)
		s.Inbound = s.Grant != ""
	}
	if inboundOverride != nil {
		s.Inbound = *inboundOverride
	}
	if b, err := os.ReadFile(filepath.Join(dir, "config.json")); err == nil {
		_ = json.Unmarshal(b, &s.Config)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "peers.json")); err == nil {
		_ = json.Unmarshal(b, &s.Peers)
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
	if s.DeviceID == "" || s.Secret == "" {
		id, err := protocol.NewDeviceID()
		if err != nil {
			return nil, err
		}
		sec, err := protocol.NewSecret()
		if err != nil {
			return nil, err
		}
		s.DeviceID = id
		s.Secret = sec
		if err := s.persistIdentity(); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(s.Config.Workspace, 0o700); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) persistIdentity() error {
	b, _ := json.MarshalIndent(map[string]any{"device_id": s.DeviceID, "inbound": s.Inbound}, "", "  ")
	if err := os.WriteFile(filepath.Join(s.Dir, "identity.json"), b, 0o600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.Dir, "device_secret"), []byte(s.Secret), 0o600)
}

func (s *Store) persistGrant() error {
	if s.Grant == "" {
		_ = os.Remove(filepath.Join(s.Dir, "grant"))
		return nil
	}
	return os.WriteFile(filepath.Join(s.Dir, "grant"), []byte(s.Grant), 0o600)
}

func (s *Store) persistConfig() error {
	b, _ := json.MarshalIndent(s.Config, "", "  ")
	return os.WriteFile(filepath.Join(s.Dir, "config.json"), b, 0o600)
}

func (s *Store) persistPeers() error {
	b, _ := json.MarshalIndent(s.Peers, "", "  ")
	return os.WriteFile(filepath.Join(s.Dir, "peers.json"), b, 0o600)
}

func (s *Store) IssueGrant() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, err := protocol.NewGrant()
	if err != nil {
		return "", err
	}
	s.Grant = g
	s.Inbound = true
	if err := s.persistGrant(); err != nil {
		return "", err
	}
	if err := s.persistIdentity(); err != nil {
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
	return s.DeviceID, s.Secret, s.Grant, s.Config.Workspace, s.Config.RelayURL, s.Inbound, cp
}

func Hostname() string {
	h, _ := os.Hostname()
	return h
}

func OSArch() (string, string) {
	return runtime.GOOS, runtime.GOARCH
}
