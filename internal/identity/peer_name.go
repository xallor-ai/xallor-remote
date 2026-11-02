package identity

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// RenamePeer changes display metadata only; device ID and authorization are untouched.
func (s *Store) RenamePeer(id, name string) error {
	name = strings.TrimSpace(name)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > 80 {
		return fmt.Errorf("设备备注最多 80 个字符。")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, p := range s.Peers {
		if p.DeviceID != id {
			continue
		}
		previous := p.Name
		s.Peers[i].Name = name
		if err := s.persistPeers(); err != nil {
			s.Peers[i].Name = previous
			return err
		}
		return nil
	}
	return fmt.Errorf("找不到这台设备。")
}
