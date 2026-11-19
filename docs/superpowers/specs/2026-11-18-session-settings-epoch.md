# Settings changes and recipient consent

Status: implemented WIP; focused regression tests, native Linux, full Windows Go
suite and independent review passed. Not a signed release acceptance.
Scope: non-GUI, one live Relay authority. No new protocol/table or production change.

Inbound off/on currently preserves the connection epoch, old active sessions,
resume proofs and pending approval. The real SQLite red tests demonstrate both
resurrection and late consumption after re-enable. Typed code revoke/rotation is
a distinct lifecycle and remains a separate contract/acceptance task.

## Implementation shape

- `access.Device` gains internal session-epoch snapshot fields; SQLite's device
  read returns these from existing `session_targets`. They are not wire fields.
- `SQLStore.UpdateDevice` updates device settings and existing consent authority
  in one transaction. Effective changes are inbound or actual access Binding
  changes; owned-device unused grant synchronization and identical settings do
  not invalidate consent. Off retires the current epoch; on after retirement or
  an effective binding change activates a new nonce generated outside retries.
  Missing target rows do not implicitly enable session functionality.
- Relay fences new admission/publication (`ready=false`) before effective
  mutation, while preserving socket and inflight routes. It applies the returned
  epoch only to the same incarnation. Lost/failed/unknown writes keep routing
  stopped; a Drop crossing commit must also retire the newly returned epoch.
- Invalidated pending entries receive correlated unknown; active clients lose
  their session binding but keep their transport and session-required mode.
  Once a connection enters logical-session mode, rejection/queue failure never
  restores raw grant fallback. New recipient approval on the same connection is
  allowed when no active session or pending request exists. Recheck both pending
  session key and per-client exclusivity after the reservation transaction.
- Invoke retains and finally compares its pre-verification client snapshot;
  final locked admission rechecks session ID, required mode, scope, expiry and
  epoch. A database check before a complete off/on cycle is insufficient.
- Inbound-off invalidates old foreground renewals permanently, but does not
  remove inflight routes or release their transport capacity. Output and the
  real terminal still arrive on the same connection. No process cleanup is
  inferred from session revocation; the Runtime owns actual execution resources.

## Modules and acceptance

Storage helper: `internal/storage/sqlstore_session_settings.go`; device snapshot
fields/read/update in `internal/access/device.go` and `sqlstore_identity.go`.
Relay consent detachment/new-request gate: `internal/relay/session_settings.go`;
integrate in `hub_authorization.go`, `session.go`, `session_publish.go` and final
invoke admission. Reuse target epoch primitives rather than duplicate tables.

Required regressions: connected/disconnected old consent, pending late approval,
same-connection reapproval, refusal/queue failure without fallback, competing
different session IDs, invoke verification crossing off/on, old exec output and
terminal/capacity/lease behavior, idempotent and owned-unused-grant settings,
atomic rollback, Drop/commit and publish/commit races. Run relevant suites/vet,
CLI build, full Go suite, native Linux and independent read-only review.

Deferred: typed access-code revoke/rotation, unknown result recovery, multi-active
Relay fencing, new signed candidate and production deployment. No GUI or commit.

## Evidence scope

The before-fix SQLite tests show both connected and disconnected controllers can
resume old consent after off/on, and a delayed pending approval can still consume
its code. After-fix tests cover those cases plus per-client pending contention,
invoke verification crossing the complete cycle, settings commit versus Drop,
and approval/resume publication delayed after commit. Settings rollback uses an
actual SQLite trigger; committed-but-error uses an adapter fault only after the
real update commits. That case asserts that DB active may remain while routing
stops, old consent is revoked and fresh registration/approval recovers service.

Hub execution tests preserve target-supplied stdout/authorization_lost terminal,
transport quota and the shared controller connection. These are controlled Hub
frame tests, not proof of OS process-tree cleanup. Owned-account resume tests
also preserve actual consent after an irrelevant grant synchronization.

One old pool-wait fixture used an empty UID, now rejected before I/O. It was
updated to a well-formed binding so the original cancellation/no-mutation
assertions still exercise the SQL pool. No observation timeout was widened.

Independent read-only review found no remaining P0-P2 within this change; the
reviewer did not execute tests. Typed Code.Revoke currently changes code state
while session revocation is separate; its full product contract and acceptance
remain a separate task, not implicitly completed by settings-epoch work.

Native Linux report:
`non-gui-release/session-settings-linux-20261118-090355/report.json`: 84 top-level
tests (access 8, storage 20, relay 39, noderun 17), source and uploaded binary
hashes checked, owned offline container absent, cleanup_errors empty. Windows
focused packages, relevant vet and CLI build also passed.
Final Windows `go test -p 2 ./...` exited 0 (CLI 514.335s, noderun 16.625s,
relay 22.016s, storage 22.380s; unaffected packages cached). Full log:
`dist/verification/session-settings-full-go-20261118.log`.
