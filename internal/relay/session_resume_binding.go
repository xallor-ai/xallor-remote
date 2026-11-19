package relay

import "github.com/xallor-ai/xallor-remote/internal/accesscode"

// Only server-derived identity and authorization facts contribute to the
// binding. Credentials and connection addresses are never digest inputs.
// This identifies the snapshot; it does not replace current-state revalidation.
func sessionResumeBinding(slot clientSlot, ok bool) (string, string) {
	if !ok {
		return "", ""
	}
	facts := accesscode.ResumeBinding{
		TargetID: slot.targetID, Access: accesscode.ResumeAccessBinding(slot.access), Identity: accesscode.ResumeIdentity(slot.identity),
		Authorization: accesscode.ResumeAuthorization{Binding: accesscode.ResumeAccessBinding(slot.authorization.Binding), ShareID: slot.authorization.ShareID, Permissions: uint64(slot.authorization.Permissions), ExpiresAt: slot.authorization.ExpiresAt},
	}
	return facts.Digest()
}
