# Access Code Lifecycle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement one-time, expiring, and long-lived access-code semantics in the Runtime/Relay authority while preserving recipient approval, session authorization, and unknown-result safety.

**Architecture:** `internal/accesscode` owns pure lifecycle decisions and atomic redemption invariants. The Relay persistence layer owns code generations, reservations, consumption and session authorization in the same transaction boundary as device ownership. Runtime/CLI expose thin management and approval transport; no head mints, extends, consumes or restores a code independently.

**Tech Stack:** Go 1.25, existing `internal/access` transaction interfaces, SQLite Relay store, existing Runtime IPC and WebSocket protocol, deterministic UTC clocks in tests, native Windows/Linux acceptance harnesses.

**Spec:** `docs/superpowers/specs/2026-11-17-access-code-lifecycle.md`

## Global Constraints

- A one-time code is consumed only by the first finally approved logical session; TCP connections, `info`, `doctor`, probes, rejected attempts and approval timeouts never consume it.
- Expiring codes use authority UTC `expires_at`; expiry rejects new sessions and does not silently claim that an existing process stopped.
- Long-lived codes have no automatic expiry and still require recipient approval when the session policy requires it.
- Code validity, code consumption, session authorization and per-operation approval are separate records and state machines.
- Lost commit responses remain `consumed` or `unknown`; no automatic refund, replay or re-redemption is allowed.
- Plaintext codes, device secrets and account credentials never enter logs, audit payloads, JSON examples or persisted Relay state; only high-entropy hashes and opaque IDs are stored.
- GUI is outside this plan. CLI, MCP and TUI remain thin Runtime heads.
- No compatibility dual-write is added for unpublished clients; protocol/schema changes use the final shape.

---

### Task 1: Pure access-code lifecycle domain

**Files:**
- Create: `internal/accesscode/types.go`
- Create: `internal/accesscode/state.go`
- Create: `internal/accesscode/state_test.go`

**Interfaces:**
- Consumes: code type, generation, scope and authority time supplied by the caller.
- Produces: `CodeType`, `CodeState`, `ReservationState`, `SessionState`, `CodeSnapshot`, `Reservation`, `SessionAuthorization`, and pure transition functions used by persistence adapters.

- [x] **Step 1: Write failing tests** for code type validation, UTC expiry boundaries, generation mismatch, one-time consumed/released/unknown transitions, and session continuation after code expiry.
- [x] **Step 2: Run the focused package test** with `go test ./internal/accesscode`; confirm failures are caused by missing lifecycle transitions rather than test setup.
- [x] **Step 3: Implement the pure reducer** so invalid transitions return typed errors and no transition mutates its input. Require a non-empty opaque code ID, positive generation, explicit scope and authority timestamp.
- [x] **Step 4: Add concurrency-independent predicates** for reservation/finalization, unknown preservation, revoke and session operation checks; make all time comparisons UTC.
- [x] **Step 5: Run `gofmt`, `go test ./internal/accesscode` and `go vet ./internal/accesscode` (all pass).**

### Task 2: Durable access transaction contract and SQLite schema

**Current checkpoint (2026-11-17):** The schema-only slice is implemented in
`internal/storage/sqlstore_access_code.go`: migration version 9 creates the
three lifecycle tables and indexes, checks conflicting object kinds before DDL,
and rolls back cleanly. The SQL adapter now exposes `accesscode.Store` read/write
transactions for insert, reservation, final approval/rejection, unknown
preservation, revoke and independent session end; storage tests cover one-time
uniqueness, expiry release and session creation/termination. Relay protocol
wiring and secret-hash redemption by a real client are still separate tasks.

Task 3 has a pure `SessionRequest.Digest` primitive in `internal/accesscode`;
it canonicalizes scope and binds request/session/target/code generation and
session expiry. The protocol package now validates structured session request,
approval request/result and terminal result envelopes; Relay Hub approval
wiring now has an optional code-store path: reservation, digest-bound target
approval request, pending-invoke denial and scoped session activation are
covered by a Hub test. Runtime approval-head wiring, disconnect/reconnect and
full revoke propagation remain pending. The Runtime now has a separate local
`session.approval` IPC hub with digest-bound subscribe/list/respond, timeout and
last-head cancellation, and its device websocket adapter validates Relay
approval requests and returns the same digest-bound decision. Reconnect/revoke
session cleanup now conservatively marks pending reservations unknown and drops
session-bound clients on target disconnect; explicit revoke propagation and a
real end-to-end Relay/Runtime acceptance run remain pending. Ownership
invalidation now also persists `session_authorization=revoked` outside the Hub
mutex for each session-bound client. A real TLS Relay wire test now covers
request, target approval, session result and scoped invoke against SQLite. The
Runtime client link and local IPC now carry `session.request` through the same
Runtime path; CLI/MCP presentations are experimental and reconnect policy remains pending.
The returned `session_id` is also accepted by Runtime operation IPC and carried
into each Relay invoke; CLI exposes `exec --session-id` and MCP `remote.exec`
exposes `session_id`, so a granted logical session can actually be used for its
scoped operations. Reconnect remains conservative and pending.
`TestSessionRuntime_shouldRouteApprovedScope_overRealRuntimeRelay` now covers
the cross-daemon path: two real Runtime daemons authenticate to a live Relay
HTTP/WebSocket server, the target Runtime approval head accepts the request,
and the client Runtime completes a scoped `info` invoke with the returned
session ID.

**Files:**
- Modify: `internal/access/store.go`
- Modify: `internal/access/transaction.go` (create if the transaction methods are split during implementation)
- Modify: `internal/storage/sql.go` and the existing migration file that owns the Relay schema version
- Create: `internal/accesscode/store.go`
- Create: `internal/accesscode/store_sql.go`
- Create: `internal/accesscode/store_sql_test.go`

**Interfaces:**
- Consumes: Task 1 transition types and the existing `access.Store`/`access.Transaction` consistent snapshot contract.
- Produces: transactional methods that reserve, approve, consume, release, revoke and rotate codes without exposing plaintext secrets.

- [ ] **Step 1: Write failing SQL tests** for atomic one-time redemption, concurrent redemption with one winner, rollback after approval rejection, lost-result preservation, expiry boundary and rotation invalidation.
- [ ] **Step 2: Run the focused SQL tests** against an owned temporary SQLite profile and verify the new tests fail before schema/method changes.
- [x] **Step 3: Add the durable tables** `access_codes`, `code_consumptions` and `session_authorizations`, keyed by opaque IDs with unique `(code_id, generation)` and a guarded one-time consumption transition.
- [x] **Step 4: Extend the transaction interface** with methods equivalent to `CodeByID`, `ReserveCode`, `FinalizeReservation`, `ReleaseReservation`, `CreateSessionAuthorization`, `EndSessionAuthorization`, `RevokeCode` and `RotateCode`; each method operates inside the caller transaction. Schema 13 adds stable rotation receipts; management endpoints and live revocation propagation are still separate unfinished tasks, tracked in `../specs/2026-11-18-code-management.md`.
- [x] **Step 5: Make schema migration fail closed** for unsupported older profiles, preserve existing device/share records, and ensure failed migrations do not create an active code or session.
- [x] **Step 6: Run `go test ./internal/storage ./internal/accesscode` and `go vet`** with the pinned toolchain.

### Task 3: Relay logical-session redemption and recipient approval

**Files:**
- Modify: `internal/protocol/message.go`
- Modify: `internal/protocol/codes.go`
- Modify: `internal/relay/hub_identity.go`
- Modify: `internal/relay/hub_authorization.go`
- Create: `internal/relay/access_code_session.go`
- Create: `internal/relay/access_code_session_test.go`
- Modify: `internal/relay/server.go` and wire tests

**Interfaces:**
- Consumes: Task 2 transactional redemption methods and the existing `HelloClient`/`HelloOK` authentication flow.
- Produces: an opaque `session_id`, code type/generation metadata, bounded recipient-approval request, and explicit `pending`, `active`, `ended`, `revoked` or `unknown` session states.

- [ ] **Step 1: Write failing wire tests** proving `info`/`doctor` do not consume one-time codes, concurrent logical-session attempts have one winner, rejection/timeout releases a reservation, and a lost final response never refunds consumption.
- [ ] **Step 2: Run the focused Relay wire tests** and confirm the failures occur at missing session/redeem behavior.
- [ ] **Step 3: Add final protocol fields** for code ID/type/generation, session ID and approval state; reject unknown fields and mismatched device/ownership/generation before any mutation.
- [ ] **Step 4: Implement the Relay redemption transaction**: validate hash and scope, create a bounded reservation, wait for the existing recipient approval head, then atomically consume one-time codes and create the session authorization; expiring/long-lived codes create non-consuming session records.
- [ ] **Step 5: Revalidate session authorization on every new operation** and preserve `unknown` when the authority cannot confirm the final commit; never retry redemption automatically.
- [ ] **Step 6: Run `go test ./internal/relay ./internal/protocol ./internal/access ./internal/accesscode` and the focused wire e2e cases.**

### Task 4: Runtime IPC and CLI management

**Files:**
- Modify: `internal/noderun/device_settings.go`
- Modify: `internal/noderun/ipc.go`
- Modify: `internal/cli/auth.go` and `internal/cli/root.go`
- Modify: `internal/protocol/device_lifecycle.go`
- Create: `internal/cli/access_code_test.go`
- Modify: `docs/credentials.md`, `docs/dataplane.md`, `docs/protocol.md`

**Interfaces:**
- Consumes: Relay session states and opaque code metadata from Task 3.
- Produces: explicit CLI commands for issuing, showing status, revoking and rotating a code; all state changes go through Runtime IPC and wait for device/Relay confirmation.

- [ ] **Step 1: Write failing CLI/IPC tests** for `grant issue --type one_time|expiring|long_lived`, explicit expiry input, revoke/rotate, masked output, and refusal when Runtime readiness or Relay confirmation is unknown.
- [ ] **Step 2: Run focused CLI tests** and confirm missing type/state handling fails before implementing commands.
- [ ] **Step 3: Implement typed request/response envelopes** with opaque code IDs, absolute UTC expiry, generation and lifecycle state; never return a plaintext code from status or diagnostics after issuance.
- [ ] **Step 4: Keep existing `grant issue` behavior explicit** by requiring a type for new protocol calls and refusing silent conversion of an existing reusable v0 grant.
- [ ] **Step 5: Update SSOT docs** once each field is implemented, labeling the old reusable grant as legacy WIP and documenting the new session/consumption distinction.
- [ ] **Step 6: Run the CLI, IPC, identity and Relay test suites plus `python -m unittest discover -s tests -p test_readme_docs.py`.**

### Task 5: Cross-platform acceptance and evidence

**Files:**
- Create: `tests/access_code_lifecycle_e2e.py`
- Modify: `tests/fixtures/runtime_cluster.py`
- Modify: `tests/README.md`
- Modify: `docs/superpowers/plans/2026-11-17-non-gui-todo.md`

**Interfaces:**
- Consumes: completed Runtime/Relay/CLI flow from Tasks 1–4.
- Produces: fixed-hash Windows/Linux reports covering lifecycle, restart, reconnect, approval and unknown-result boundaries.

- [ ] **Step 1: Write the matrix cases** for one-time normal/duplicate/race/reject/cancel/timeout/lost response/restart/reconnect, expiring before/exactly after boundary and clock skew, long-lived repeated/revoke/rotate/ownership change, and all scope/generation isolation cases.
- [ ] **Step 2: Run the matrix first against the controlled owner mechanics fixture** and require explicit `cryptographic_acceptance:false` in that report.
- [ ] **Step 3: Run the same matrix against a clean signed candidate** on native Windows and Linux with no production Relay changes; retain candidate SHA, Runtime SHA, cleanup errors and session terminal states.
- [ ] **Step 4: Run npm/MCP cross-layer checks** to prove MCP cannot mint, extend, approve or restore a code and that unknown sessions are not replayed.
- [ ] **Step 5: Update the evidence ledger and Issue #105** with exact scope and pass/fail status; do not close #105 until all matrix rows pass on both OSes and the clean candidate.

## Verification and handoff

After each task, run its focused tests before moving to the next task. Before any release claim, run the complete Go suite, MCP suite, Python e2e matrix, `git diff --check`, and the clean protected-tag workflow. A draft, dirty checkout or controlled verifier remains L1/L2/L3 evidence and cannot close the signed-release acceptance issues.
