# Session-bound handoff delegation

Status: implemented WIP; targeted tests, vet/build and independent review in
progress. No GUI, public management service or signed release claim.

## Problem

Handoff delegation proofs previously contained only device/access/principal
snapshots. A logical session could be explicitly revoked while an already-issued
background proof continued to renew under the transport/account grant.

## Contract

`IssueSession` adds the original logical session ID, code ID/generation, target
epoch and approved scope to the signed HMAC claim. Relay injects this path only
when a handoff submit carries a live logical session. Legacy no-session handoff
retains its existing transport authority behavior.

`Renew` verifies the signature, authority, device UID and proof lifetime, then
reads the durable session and requires exact ID/code/generation/target epoch,
active state, expiry and handoff scope. Revoke/rotate, inbound epoch changes,
scope changes and expiry therefore deny old proof renewal. A later session on the
same controller cannot lend authority to the old proof.

The proof contains no bearer secret or command. Handoff receipt state and target
process cleanup remain separate; denied renewal does not pretend the task stopped.
Unknown storage errors stay transient and do not grant a lease.

## Wiring and evidence

Relay `NewHub` supplies its access-code store to the delegation Authority when
available; `hub_invocation.go` chooses `IssueSession` for session-bound handoff.
Handoff submit also acquires the existing per-device identity gate and repeats
session validation after the gate. Owner code management and device settings use
the same gate, so a revoke/rotate/settings mutation cannot race proof issuance
and route publication on that target. Drop/replacement still wins at the final
routing lock.
The existing noderun handoff renewal message remains unchanged. Regression
`handoff_session_binding_test.go` issues a real session-bound proof, renews it,
revokes the session in SQLite, and confirms renewal denial. Relevant delegation,
Relay, noderun, storage and accesscode packages plus vet/build pass.

Remaining limits: no separate Runtime process or signed package acceptance, no
full handoff receipt/OS cleanup E2E, and no instantaneous zero-window revocation
between the final session read and proof publication. These are not claimed.
