# Durable recipient connection epochs

Status: implemented WIP; focused tests, native Linux, full Windows Go suite and
design/code review passed. Not a deployed or signed candidate.
Scope: non-GUI, single active Relay routing authority and its SQLite store.

## Evidence and requirement

`session_target_lifecycle_test.go` reproduces three failures against actual SQLite:
after the controller disconnects, target disconnect/replacement and a fresh Hub
using the same store leave the old session and resume proof active and usable.
The current Drop path enumerates live clients; that cannot represent retained
recipient consent. A registered replacement must not inherit prior consent.

An independent read-only design review confirmed the failures and identified
pre-publication routing, early connection clearing, stale pending publication,
and legacy unknown-reservation index conflicts as required integration cases.

## Chosen shape

- `accesscode.TargetBinding{DeviceUID, Epoch}` identifies recipient connection
  authority. Epoch is a Relay-generated random nonce, never a client parameter.
- A single current-target table stores UID, epoch and whether the epoch is
  active. The active flag is authorization state, not a network-online promise.
- Reservations and session authorizations persist the exact UID/epoch that
  requested recipient consent. Resume proofs use their session's binding;
  there is no second copy of the epoch in resume_capabilities.
- On successful device authentication, retire the old slot under the routing
  mutex BEFORE changing the database epoch. No SQL I/O occurs under that mutex.
  Rotate epoch, revoke old active sessions/proofs and mark old reserved attempts
  unknown in one transaction, then publish the new slot only if its connection
  context and greeting enqueue succeed. Any uncertainty leaves routing stopped.
- Drop captures the retiring slot's binding and stops routing under the mutex.
  Persistent retirement uses an expected-epoch condition; delayed old cleanup
  cannot affect a replacement epoch or its newly approved sessions.
- Queue refusal, identity-write failure and device deletion use the same
  retirement boundary even if conn was already cleared. Repeated retirement is
  idempotent. Reconnect must rotate successfully before becoming routable.

## Transaction and publication checks

ReserveCode receives the expected target binding and checks code DeviceUID and
active epoch in its write transaction. FinalizeReservation checks the persisted
reservation binding, never fills an old reservation with the current epoch.
ConsumeResume checks the session binding in the same transaction as consumption.
SQL readers alone are insufficient; a stale read cannot justify a later write.
SQLite WAL refuses promotion of an invalidated snapshot; retry only after a
confirmed rollback and re-read the binding. Commit uncertainty is not replayed.
[SQLite isolation](https://www.sqlite.org/isolation.html),
[transaction rules](https://www.sqlite.org/lang_transaction.html).

Reserve publication must retain pending object identity and enqueue approval
under the routing mutex after checking the original slot/connection/epoch.
Approval publication must check the captured pending object, original controller
state, recipient slot/connection/epoch, ready/inbound flags and context. Only an
accepted terminal can bind a session. Failed publication preserves consumption,
attempts bounded revocation and returns unknown; it never refunds an access code.
Resume publication retains its existing checks and adds the target epoch.
Invoke rechecks the session's target binding at final routing admission, after
database/access checks, so an old read cannot cross a replacement.

## Migration and uncertain reservations

Schema 12 adds target bindings. Legacy active sessions/proofs without a provable
target epoch are revoked; consumed codes and attempt history remain unchanged.
Legacy reserved attempts become unknown. Existing unknown or reserved attempts
for a one-time code keep it unavailable; they are never rewritten as released.
Do not blindly expand the unique partial index to unknown: old data can contain
multiple unknown rows. Retain the reserved uniqueness index and perform a
transactional exclusive-attempt check including unknown before new reservation;
SQLite write serialization and fresh reads after rollback preserve this gate.

## Boundaries

No new client protocol or compatibility fallback. No GUI, production migration,
release or Git commit. Reopening a current schema does not itself rotate epochs;
a new authenticated device registration does. The fresh-Hub test assumes the old
Hub is stopped: shared-database multi-active Relay fencing is not provided.
Unknown commit recovery and restart-safe controller credential custody remain
separate unfinished requirements.

## Required acceptance

1. All three red lifecycle cases; persistent invalidation before reconnect.
2. New epoch remains valid after stale Drop; unrelated devices unaffected.
3. Reserve/finalize/resume stale epochs denied without consuming/refunding.
4. Old invoke cannot pass a replacement between DB read and routing.
5. Old pending approval cannot publish, delete a replacement pending object or
   rebind a changed client; committed but undelivered consent is retired.
6. Queue failures and identity errors retire without relying on a later Drop.
7. Retirement/rotation failure and hello failure keep routing stopped.
8. Migration preserves consumption and multiple unknown histories, rolls back
   atomically on fault, and does not revoke current sessions on repeated open.
9. Windows full Go suite, scoped native Linux, build/vet and independent review.

## Current evidence and limits

- Original three lifecycle failures were reproduced before implementation.
- Storage tests cover conditional stale retirement, replacement consent,
  unknown exclusivity, rollback after proof-update failure and two SQLite
  connections racing rotation with reserve/finalize/consume snapshots.
- Actual schema-11 table shapes with multiple unknown and reserved histories
  migrate without refund; malformed target storage rolls back DDL/state/version.
- Relay tests cover before/after rotation and after-invoke-read windows, stale
  Drop preserving new consent, invoke/settings enqueue failure, identity-write
  failure, device deletion, retirement/rotation failure and greeting refusal.
- Approval post-commit tests cover target drop/replacement, client drop/state
  change, pending replacement and active-result queue refusal. Additional tests
  reproduce pending-key overwrite after two reservations commit, and check
  initial/duplicate pending result order against an immediately approving peer.
- A committed reservation whose acknowledgement is lost returns unknown without
  replay or approval publication. This is not durable unknown-result recovery.
- Independent review's same-scope P2 findings were fixed and rechecked. The
  reviewer did not run tests. Inbound off/on authority generations, multi-active
  Hubs and durable recovery remain separate uncompleted work; do not count them
  as target connection lifecycle acceptance.

Subsequent same-connection settings behavior is implemented and validated
separately in [settings epochs](2026-11-18-session-settings-epoch.md); the target
connection report below does not serve as its verification evidence.

Native Linux report:
`non-gui-release/session-target-linux-20261118-082731/report.json`: 69 top-level
tests (accesscode 12, storage 18, relay 23, noderun 16), matching source/binary
hashes, owned container absent, cleanup_errors empty. Windows vet/CLI build pass.
Final Windows `go test -p 2 ./...` exited 0 (CLI 518.253s, noderun 18.327s,
relay 18.336s, storage 23.035s; unaffected packages cached). Full log:
`dist/verification/session-target-full-go-20261118.log`.
The first failed Windows fixture left
`C:/Users/andy.wang/AppData/Local/Temp/TestSession_shouldMarkPendingUnknown_andDropClient_whenTargetDis4132816029`.
Deletion of that exact temporary directory was rejected by execution policy
(`blocked by policy`); it remains retained, and no alternate deletion was tried.
This local failed-attempt residue is separate from the successful Linux cleanup.
