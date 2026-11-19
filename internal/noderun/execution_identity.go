package noderun

import "github.com/xallor-ai/xallor-remote/internal/buildinfo"

// WithActivationIdentity latches the authenticated permit before Run.
func (d *Daemon) WithActivationIdentity(id string) *Daemon { d.activationID = id; return d }
func (d *Daemon) runningIdentity() buildinfo.Executable {
	live := buildinfo.Running()
	live.ActivationID = d.activationID
	live.LaunchID = d.launchID
	return live
}
