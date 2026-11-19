# Logical session reconnect design

Status: non-GUI WIP. Domain, persistence, message validation, issuance and
Runtime/Relay entrypoints exist; this is not security acceptance. The credential
check was missing from the initial consume implementation and is now enforced
inside the consume transaction. Cross-principal binding is now covered by
negative tests; target/session binding,
concurrent rebind/revoke and lost-commit-response cases still require audit and
acceptance, in addition to the cross-platform matrix.

## Credential-check correction (2026-11-18)

The original handler accepted any nonempty capability when the public IDs and
transport binding matched: the secret never reached `ConsumeResume`. A negative
test reproduced `active` for both a wrong secret and the stored hash submitted
as a bearer. The handler now hashes the supplied plaintext and the transaction
compares it to the stored SHA-256 with a constant-time comparison before any
state update. Rejection returns `unauthorized`, does not bind a session, does
not return a new capability and leaves the real capability usable.

The Runtime integration test uses two Daemon objects, `net.Pipe` and a live
loopback WebSocket Relay in one Go test process. It proves component integration,
not independent Runtime processes, authenticated OS IPC or signed deployment.

## Authenticated binding correction (2026-11-18)

The initial binding used only the device grant or ownership ID. Two authenticated
principals accessing the same device therefore shared a binding. Tests with a
real SQLite account/access service reproduced Bob resuming Alice's session
using Alice's resume capability, for both an owned/shared device and an
unclaimed device with an account-authenticated grant connection.

The binding is now a versioned deterministic digest of the server-derived
target ID, device/access incarnation, complete principal identity (authority,
tenant, membership/version, credential ID/expiry), and authorization snapshot
(share ID, permissions and expiry). Account-to-anonymous downgrade and
replacement credentials cannot inherit the original binding. Anonymous grants
remain possible only for unowned devices. Incomplete identity or inconsistent
authorization snapshots cannot yield a binding. No legacy digest fallback is
provided for these unpublished WIP capabilities.

An invoke explicitly naming a session must use that connection's bound session.
Tests also reproduced a refused caller sending `info` with the original session
ID and obtaining a route via ordinary transport permissions. The session gate
now rejects an explicit session on an unbound connection, instead of falling
back to grant/share authorization. Requests that do not select a session retain
the separate transport authorization path.

This digest binds the authenticated snapshot; it alone does not prove that a
credential or share remains authorized. The consume transaction now performs
the live revalidation described below. Post-commit routing races and uncertain
commit outcomes remain separate acceptance work.

## Transactional authority revalidation (2026-11-18)

Eight stale-state cases originally resumed successfully after credential revoke/
expiry, member disable/restore, share removal/replacement/expiry or inbound-off.
The consume transaction now reads these current facts through `ResumeAuthority`,
reuses the device access policy via an existing transaction reader, and compares
the canonical binding before consuming or rotating proof. The shared digest
value types live in `internal/accesscode` without transport dependencies.

Tests also cover a credential revocation committed by a second SQLite connection
after the resume transaction's read. The stale write cannot commit; a confirmed
rollback retry re-reads the revocation and refuses consumption. Two further
tests cross credential/share expiry after the initial read; the handler checks
both deadlines immediately before consumption. Rejection leaves the resume
capability unconsumed and creates no successful rebind. These tests do not prove
all post-commit in-memory publication or lost-response behavior.

## Publication after a confirmed commit (2026-11-18)

Tests reproduced active results after client loss, target loss/replacement and
inbound-off between commit and publication; a refused result queue also left
the session bound. Publication now checks the original client/target connection
incarnation, current access/identity snapshot, readiness, inbound and deadlines
under the routing mutex. The nonblocking result enqueue and client binding are
ordered under that same mutex. Failed enqueue cannot create a new binding.

If confirmed consumption cannot be published, the handler withholds the rotated
secret, attempts to revoke the session and active resume proofs in a separate
bounded transaction, reports unknown and closes the connection. It never refunds
the consumed proof. An injected SQL failure in retirement leaves unknown, not
a false cleanup/rollback claim. Acceptance into the outbound queue is not proof
of delivery; lost responses and uncertain database commits remain separate
acceptance work.

## Decision

An access-code session is not reconnectable merely because the same device or
client reconnects. Reconnection requires a separately issued, short-lived,
single-use resume capability that is bound to the logical session and the
authenticated principal. The original access code is never reused as a resume
credential.

Until this capability is implemented for a session, a client transport
disconnect revokes an active session. Once a valid active resume capability
has been issued, the session may remain active for a bounded reconnect window;
an invalid, expired or missing capability still revokes it. A target Runtime
disconnect revokes active sessions and marks pending reservations unknown.

## Capability shape

After an approved session is committed, Relay may issue a resume capability to
the client Runtime. The response contains only the opaque capability once:

```text
session.result
  session_id
  state: active
  generation
  scope
  session_expires_at
  resume_capability       # inbound use only; never echoed or logged
  resume_expires_at
```

Relay persists only:

```text
resume_id
session_id
capability_sha256
binding_kind              # account, ownership, or device grant
binding_digest
issued_at
expires_at
generation
state: active | consumed | revoked | unknown
```

SQLite introduced these records at schema version 10; current schema 11
invalidates legacy active authorizations whose recipient scope was not proven
(see `docs/protocol.md`, logical authorization sessions). The adapter
exposes atomic insert, consume, revoke and unknown transitions through the
access-code transaction boundary. An approved session receives one short-lived
plaintext capability in `session.result`; the Relay handler consumes the old
capability and atomically rotates a new one after a separately authenticated
client reconnects.

The capability is at least 256 bits of randomness. The plaintext is held only
by the Runtime that received it. It is never included in approval events,
diagnostics, usage records, errors, URLs, command arguments or environment
snapshots.

The target Runtime receives the session approval/result facts but never the
client resume capability; the capability is returned only on the authenticated
client result path.

## Resume protocol

Runtime request/result correlation and transport-error mapping follow
[IPC boundaries](../../ipc.md#6-和数据平面的边界). A session ID is not a request
correlation key: concurrent session operations must not share a result slot.

The reconnecting client first completes normal transport authentication. It
then sends a separate `session.resume.request` containing:

```text
request_id
session_id
resume_id
resume_capability       # inbound only
target_uid
```

Relay atomically verifies:

1. the capability hash, state and expiry;
2. the authenticated principal/device/ownership binding;
3. the session state, scope and session expiry;
4. the current target UID, ownership epoch and ready device connection.

On success, the old capability is marked consumed and a new single-use
capability is issued in the same transaction. The client slot is rebound to
the existing session. No operation is replayed and no target approval is
silently repeated. A target-side reconnect must still pass the normal device
identity and settings confirmation before the session can be used.

The result is one of `active`, `rejected`, `expired` or `unknown`. If the
consume/rebind commit or response is uncertain, Relay records `unknown`; it
does not accept the old capability again and the client must not retry
automatically.

## Revocation and expiry

- Explicit session revoke, code rotation, ownership change, device reset and
  target disconnect revoke all outstanding resume capabilities.
- The storage transaction that ends or revokes a session also revokes its
  active resume capability atomically.
- Session expiry takes precedence over capability expiry.
- A consumed capability cannot be used twice, even when the first response was
  lost.
- A client disconnect without a valid resume capability follows the current
  revoke policy.
- Reconnect never extends session scope or session expiry.

## Acceptance matrix

Before implementation is called complete, tests must cover:

- valid resume with the same binding and target UID;
- wrong principal, device, ownership epoch, target UID and session ID;
- expired, revoked, consumed and unknown capabilities;
- concurrent use where exactly one request consumes a capability;
- lost success response followed by a refusal of the old capability;
- target disconnect/reconnect before and after client resume;
- session expiry before capability expiry and capability expiry before session
  expiry;
- code rotation, ownership transfer and device reset invalidation;
- no replay of an invoke, file write, handoff or approval during resume;
- Windows/Linux Runtime and Relay paths plus MCP/CLI unknown-result behavior.

The first implementation should expose this only through Runtime IPC and keep
CLI/MCP surfaces experimental until the durable transaction and cross-process
matrix pass. GUI is outside this design.
