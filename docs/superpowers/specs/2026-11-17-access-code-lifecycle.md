# Access-code lifecycle design

Status: design confirmed for the three top-level code semantics; protocol,
database and Runtime implementation are not started by this document.

This design separates four things that must not be represented by one boolean:

1. **Code validity** — whether a bearer code may be considered for a new
   session.
2. **Code consumption** — whether a one-time code has been atomically used by
   a successfully approved logical session.
3. **Session authorization** — the lifetime and scope of an already established
   session.
4. **Operation approval** — whether a particular command or handoff may run.

A valid code never grants more operation scope than the code and current Relay
authorization snapshot allow. A long-lived code does not bypass recipient
approval, and a one-time code does not mean one command.

## Confirmed semantics

| Type | New logical session | Time basis | Existing session |
| --- | --- | --- | --- |
| `one_time` | The first session that is finally approved consumes the code. A later connection, including after disconnect, cannot redeem it again. | No automatic expiry unless an explicit reservation deadline is added. | Continues under its separately issued session authorization. |
| `expiring` | Allowed only while the authoritative `expires_at` has not passed. | UTC `issued_at`; expiry is calculated by the authority, never by client wall-clock time. | Continues until its own session authorization ends; code expiry does not silently cancel it. |
| `long_lived` | Allowed until explicit revoke, rotation, ownership/scope invalidation, or other authority decision. | No automatic expiry (`expires_at` is absent). | Existing session follows its own authorization and revocation rules. |

The one-time rule is defined at the logical session layer, not at TCP,
WebSocket, `info`, `doctor`, connection-pool, or probe granularity. Read-only
probes and rejected/expired/unapproved attempts do not consume the code.

## Proposed records

The exact schema is still subject to the protocol review, but the authority
must retain equivalent records:

```text
access_code
  code_id
  device_uid / ownership_id
  type: one_time | expiring | long_lived
  secret_hash
  issued_at (UTC)
  expires_at (UTC or null)
  generation
  scope / allowed_operations
  require_recipient_approval
  state: active | revoked | expired | consumed

code_consumption
  code_id
  reservation_id
  session_id
  state: reserved | consumed | released | unknown
  reserved_at / reservation_expires_at
  consumed_at
  authorization_generation
  terminal_reason

session_authorization
  session_id
  code_id / consumption_id
  scope snapshot
  authorization_generation
  issued_at / expires_at
  state: pending | active | ended | revoked | unknown
```

`code_consumption` is separate from `session_authorization`; a lost response
after consumption remains `consumed`/`unknown` and must never refund a one-time
code or invite automatic re-redemption. Session recovery, if added later, must
use a separate session-bound mechanism with its own replay protection.

## Atomic redemption

The authority performs the following under one durable transaction or an
equivalent compare-and-swap boundary:

1. Verify the code hash, device/ownership binding, scope, generation and
   authoritative time.
2. Reject `revoked`, `expired`, `consumed`, or already-invalid generations.
3. Create a short-lived `reserved` consumption record before waiting for any
   recipient approval; a reservation has a bounded deadline and does not count
   as consumption.
4. Obtain recipient approval for the requested session scope when required.
5. On final approval, atomically change the reservation to `consumed` for
   `one_time`, and create the session authorization. For `expiring` and
   `long_lived`, retain a non-consuming redemption/session record.
6. If approval is denied, cancelled, or times out, release the reservation
   without consuming the one-time code.

At most one concurrent redemption can win a one-time code. A disconnect during
the final commit does not roll back consumption. A duplicate request must
query the original reservation/session result rather than retrying redemption.

## Expiry, revoke and rotation

- Authority time is UTC and authoritative. Client clocks and cached countdowns
  cannot extend validity.
- Code expiry blocks new sessions and new requests; it does not by itself claim
  that an existing process or job has stopped.
- Existing session expiry/revocation must produce an explicit session state and
  a separate cancellation/cleanup path. `revoked` or `ended` is not proof of
  OS process cleanup.
- Rotation creates a new `generation` and invalidates the prior code. Reusing
  the old value must not restore an old session or ownership scope.
- The explicit revoke/rotation policy and transaction receipts are specified in
  [code management](2026-11-18-code-management.md). A consumed or uncertain
  one-time authorization cannot regain eligibility through rotation; issuance
  of a new authorization is a separate action. Consumption history remains
  authoritative even when the code's current metadata state becomes revoked.
- Ownership transfer and device revoke invalidate old code generations and
  existing session authorization according to the same durable authority. A
  reconnect cannot revive the old scope.

## Head and Runtime boundaries

- Runtime owns local signing/configuration and recipient approval transport.
- Relay or the future management authority owns global redemption count,
  expiry, generation and cross-Relay consistency.
- CLI, MCP and GUI are thin heads. They cannot mint, extend, consume, approve,
  or restore a code independently.
- Logs and diagnostics contain `code_id`, type, generation and lifecycle
  outcome only; never the plaintext code, device secret or account token.
- Commercial entitlement, transport admission, local job capacity and code
  consumption remain separate counters.

## Required acceptance matrix before implementation is called complete

- one-time: normal approval, duplicate redemption, concurrent race, rejection,
  cancellation, approval timeout, lost response, Runtime restart and reconnect;
  `info`/`doctor`/probe must not consume it.
- expiring: before boundary, exact boundary, after boundary, client clock
  skew, cached connection, session continuation, revoke and rotation.
- long-lived: repeated sessions, explicit revoke, rotation, ownership change,
  and confirmation still required.
- all types: scope narrowing, generation mismatch, tenant/device isolation,
  unknown result preservation, no automatic replay, Windows/Linux and Relay
  restart behavior.

This specification intentionally does not authorize a schema/protocol change;
those changes require a follow-up implementation plan derived from this state
machine.
