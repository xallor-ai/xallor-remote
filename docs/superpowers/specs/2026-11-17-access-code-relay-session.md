# Access-code Relay session protocol

Status: non-GUI architecture decision for the next Runtime/Relay slice. The
existing reusable v0 grant path remains unchanged until this protocol is
implemented and accepted; this document does not claim that the new messages
are available today.

## Boundary

Transport authentication and logical session authorization are separate:

1. `hello_client` authenticates the transport using the existing account or
   reusable v0 device path.
2. A client requests a logical access session with an opaque `code_id`, the
   plaintext code in memory only, its generation, requested operation scope,
   and a client-generated `request_id`.
3. Relay verifies the code hash, device/ownership binding, generation, scope and
   authority time in one transaction and creates a bounded reservation.
4. Relay sends an approval request to the authenticated target Runtime. The
   request contains no plaintext code and is bound to the exact requested scope
   and session digest.
5. The target Runtime's local approval head explicitly accepts or rejects the
   request. A GUI is not required; CLI/TUI/MCP heads use the same local Runtime
   approval IPC.
6. Relay finalizes the reservation and creates `session_authorization` in one
   transaction. Only then does it return an active session to the client and
   target Runtime.

The existing v0 grant remains a separate transport authorization. A new
code-session route must not silently inherit all permissions from that grant;
the client slot carries an explicit session authorization and operation checks
must require it when the request selects this protocol.

## Messages

The final protocol adds these envelopes without putting secrets in responses:

```text
session.request
  request_id
  session_id
  target_device_id
  code_id
  code_secret       # inbound only; never echoed or persisted
  generation
  requested_scope
  session_expires_at (optional)

session.approval.request
  request_id
  session_id
  code_id
  code_type
  generation
  requested_scope
  request_sha256

session.approval.result
  request_id
  session_id
  request_sha256
  allow

session.result
  request_id
  session_id
  state: active | rejected | expired | unknown
  code
  scope
  generation
```

`request_sha256` covers the session ID, code ID, generation, requested scope,
target device UID and session expiry. Approval responses with a different
digest, duplicate decisions or a missing request are rejected. The plaintext
code is never included in an approval request, result, audit record or error.

## Relay state and idempotency

`Hub.clientSlot` gains an optional logical session authorization. A pending
session is held separately from the transport client and has a bounded
reservation deadline. While pending, the requested session route cannot invoke
operations. A duplicate `session.request` with the same request ID and digest
returns the original pending/terminal result; a conflicting reuse is rejected.

The Relay owns all transitions:

- reservation creation and final approval are serialized by the access-code
  transaction store;
- one-time consumption is committed with session creation;
- rejection, cancellation and timeout release a reservation;
- a lost final response is retained as `consumed`/`unknown` and is never
  refunded or automatically re-redeemed;
- revoke, rotation, ownership transfer and device disconnect invalidate new
  operations through the session authorization, while process cleanup remains a
  separate Runtime/resource state.
- Until a separately authenticated reconnect proof exists, client transport
  disconnect also revokes its active logical session. The system does not keep
  an unbindable active authorization merely to make a future reconnect appear
  seamless.

## Runtime approval boundary

The target Runtime receives the approval request only after Relay has validated
the code reservation. The Runtime approval head displays the exact scope,
target, code type, generation and expiry, and returns the digest-bound decision
through local IPC. It cannot mint, extend, consume or restore a code. If the
head disconnects, Relay releases or marks the reservation according to the
terminal state; it never treats a missing response as approval.

## Acceptance matrix

- one-time normal approval, duplicate request, concurrent race, rejection,
  cancellation, timeout, lost final response, Runtime restart and reconnect;
- expiring before/exactly/after the authoritative boundary, session
  continuation, revoke and rotation;
- long-lived repeated sessions, explicit revoke, rotation and ownership change;
- scope narrowing, generation mismatch, tenant/device isolation, unknown
  preservation and no automatic replay;
- transport disconnect and target Runtime restart without consuming a second
  time or creating a second active session;
- Windows and Linux Relay/Runtime tests, then MCP/CLI JSONL checks.

Implementation order is the corresponding Task 3 section in
`docs/superpowers/plans/2026-11-17-access-code-lifecycle-implementation.md`.
