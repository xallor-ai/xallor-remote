# Explicit code revocation and rotation

Status: domain/storage and management entrypoints are WIP. Domain/storage review
and historical tests do not certify the later Runtime/Relay/CLI/MCP integration.
The latest Runtime response-contract correction has Windows focused regression
evidence, related vet/build and native Linux coverage (8 top-level tests, no
skips, identical source hashes and verified container removal). Independent
read-only review found no new P0-P2 in that correction. Full Windows
`go test -p 2 ./...` exited 0 (CLI 289.842s, noderun 62.786s, Relay 30.309s,
storage 19.960s; unaffected packages cached), recorded in
`dist/verification/device-code-ipc-full-go-20261118.log`. These checks do not
settle the other integration gaps below.
Scope: non-GUI. Natural expiry retains the user's confirmed session semantics.

## Product contract

Natural code expiry blocks new redemption; an already approved session follows
its own expiry. Explicit revoke/rotation invalidates the exact code ID/generation
and its derived active sessions/resume proofs. Unfinished reserved attempts
become unknown; existing consumed/unknown facts remain unchanged. Code metadata
may become revoked after consumption, but consumption history is never deleted,
released or made redeemable. Repeat revoke is idempotent.

This cascade is a product policy, informed by the related-credential revocation
model in [RFC 7009 section 2.1](https://www.rfc-editor.org/rfc/rfc7009.html#section-2.1);
it is not a claim that Xallor implements OAuth revocation.

Rotation replaces only opaque ID, secret hash, generation and issuance time.
Device/ownership binding, type, scope, approval policy and an expiring code's
original deadline are preserved. Expired/revoked/consumed codes cannot rotate.
One-time codes with reserved/consumed/unknown history or any derived session
cannot rotate; released-only history does not restore consumed eligibility.
Issuing a fresh authorization is a separate explicit operation.

## Durable primitive and receipts

`RotateCode(ctx, oldID, newID, newSHA256, now)` allocates the next device-wide
generation inside the write transaction, then atomically revokes the old family,
inserts the replacement and records the rotation. Fresh IDs/hash are held stable
outside retries. `newSHA256` must be canonical lowercase SHA-256 and differ from
the old secret hash; IDs are distinct and bounded. The timestamp is authoritative
execution time, not permission to extend an expiring code's deadline.

Schema 13 adds `code_rotations(old_code_id PRIMARY KEY,new_code_id UNIQUE,
intent_sha256)`. Its versioned canonical intent includes old ID/new ID/hash;
time and assigned generation are outcomes. Read a receipt before validating the
old code's now-revoked state. The same intent returns the original replacement's
CURRENT metadata, never regenerates/revives it or follows later rotation links.
Different intent conflicts. Receipt absence is not permission to retry an
uncertain in-flight operation. No plaintext secret is persisted or returned.

Code rows and their use/rotation history are not hard-deleted by this API.
Generation allocation rejects overflow and is protected by SQLite transactions
and the existing device/generation uniqueness constraint. Only confirmed
rollback permits busy retry; commit error does not automatically replay.

Migration runs each old migration only below its version. v12→v13 must preserve
ordinary active/consumed sessions and target epochs, while invalidating legacy
active children of already-revoked codes. Unknown histories are not released.
DDL/data/version changes roll back together; reopening v13 changes nothing.

## End-to-end work sequence (all remain required)

1. Domain and transactional storage with red/green, fault, concurrency, expiry,
   idempotency and migration tests. This is independently testable infrastructure.
2. Authenticated recipient-side Relay management with per-code publication and
   invocation fencing, pending cleanup and existing execution lease invalidation.
   Storage revocation alone does NOT prove immediate in-memory/OS revocation.
3. Runtime-owned secret custody and management IPC/CLI; heads do not mint or
   mutate authority. Expose unknown outcomes and receipt queries without replay.
4. Independent process Windows/Linux acceptance, signed candidate and final
   documentation/Issue closure. No GUI or production deployment in this work.

## Current management surface

The existing authenticated `device.access` control message now accepts owner-only
`code.revoke` and `code.rotate` actions. A shared member, bearer/controller
connection, unowned device or missing account token is rejected. The request
contains `code_id`; rotation additionally contains caller-chosen bounded
`new_code_id` and `new_secret`. Relay stores only the secret hash. The current
reply returns the original child's full persisted metadata snapshot plus an
internal intent digest, never plaintext. Runtime binds IDs/digest/device context
and metadata, then strips the digest from public IPC/CLI/MCP output. It uses the
same authorized transaction result after confirmed commit, not a separate read.
Repeated intents preserve consumed/revoked/expired state, issuance and deadline;
they never follow later rotation descendants or create new eligibility. Fields
and snapshot semantics are defined in docs/protocol.md. The authority correction
also reads current management authority inside the code write transaction:
storage returns neutral device/credential facts, access policy compares the
original connection proof and full actor identity using Credential.Verify, and
Relay performs this before every revoke/rotate attempt, including receipt reuse.
No inbound/session/share admission requirement is added. Transaction ordering,
time semantics and error classification are specified in docs/dataplane.md.

CLI no longer accepts --new-secret. It uses the process environment or explicit
bounded stdin with mutually exclusive sources and shared request validation
before the Runtime runner. Input behavior is defined in docs/clients.md; errors
do not echo values. MCP still reads environment input and receives only public
metadata. These changes do not claim that process environments are a secret vault.

Current receipt/input evidence: original tests reproduced plaintext response,
missing child state and argv secret acceptance. Windows focused regressions now
cover original child state after consume/revoke/further rotation/natural expiry,
unknown commit without payload, same-transaction snapshot versus later writes,
Runtime metadata/digest/context refusals, and real Cobra input/error boundaries.
Independent read-only review found no new P0-P2. MCP build/tests passed (74 pass,
2 platform skips), native Linux passed 25 top-level tests with no skips, identical
source hashes and verified container removal. Related vet/build and six doc tests
passed. The latest full Windows suite failed in three Runtime launch/settlement
tests (Stop cleanup deadline, inherited-output observation and journal SQLITE_BUSY).
Those three passed in isolation (7.204s), which does not establish the cause or
replace the original failure. See the non-GUI ledger for logs and follow-up;
the failure remained open at that checkpoint. Subsequent fixture hardening and
independent Runtime tests passed full Go again; see the latest non-GUI ledger.
There is still no public CLI/MCP process or signed-package E2E claim.

Sixteen before-transaction cases first reproduced unauthorized mutations; the
current eighteen-case matrix also covers missing device rows. Six real SQLite
snapshot-conflict cases require exactly two authority reads after an independent
connection's intervening commit, then denial without code/receipt mutation.
Additional tests cover expiry during the read, identity-field substitution,
inbound-off owner management, denied receipt reuse, a later credential revoke
after legitimate commit, injected reader error and a real missing-table SQL
failure. Focused Windows checks, related vet/build, six documentation checks and
independent read-only review passed. Native Linux passed 19 top-level tests
(access 1, Relay 16, noderun 2), no skips, identical Go source hashes and verified
container removal; report `non-gui-release/code-authority-linux-20261118-160859/report.json`.
Full Windows `go test -p 2 ./...` exited 0 (CLI 134.217s, noderun 27.211s,
Relay 40.364s, storage 20.314s; unaffected packages cached); log
`dist/verification/code-authority-full-go-20261118.log`.

The latest code-fence correction uses each route's original consent and checks
the actual target incarnation before notification; current client consent is
detached separately. The routing process is specified in docs/dataplane.md.
Four owner revoke/rotate A/B tests first reproduced missed cancellation and
unrelated-route renewal denial after a real resume; they now pass. Runtime cause
tests and native owner management tests first reproduced cancelled instead of
authorization_lost, then passed with both parent/child exit and zero job/transport
occupancy. These use real TLS/SQLite/IPC handlers in one test process. Native
Linux passed 22 top-level tests (5 noderun, 17 Relay), no skips, identical source
hashes and verified container removal; report:
`non-gui-release/code-fence-linux-20261118-154534/report.json`.
Independent read-only review found no new P0-P2 code issues. Related vet/build
and six documentation checks pass. Full Windows `go test -p 2 ./...` exited 0
(CLI 210.392s, noderun 24.825s, Relay 39.753s, storage 18.719s; unaffected
packages cached); log `dist/verification/code-fence-full-go-20261118.log`.

If a code revoke/rotate transaction commits but its acknowledgement is lost, the
device control route is quarantined before returning `relay_error`; cached access
is not continued. A fresh authenticated device registration reloads the durable
state. A real store wrapper test verifies the committed row, cleared route and
no replay. This is conservative unknown handling, not a claim that database
rollback or OS cleanup occurred.

This is a thin management entry on the existing device control plane, not a
general management service. Per-code publication and background handoff
integration remain incomplete. The CLI exposes thin `device code revoke` and
`device code rotate` commands that call the same Runtime IPC action; MCP exposes
`remote.device_code_revoke` and `remote.device_code_rotate`, with MCP rotation
reading `XALLOR_REMOTE_CODE_SECRET` only from the process environment. Neither
head stores policy or accepts secret as an MCP tool parameter. These are not a
general management service or signed release acceptance.

`code_management_wire_test.go` additionally exercises the owner actions over a
real Relay WebSocket/device control channel: device settings and claim complete
first, a different principal is rejected, owner revoke succeeds, and the
revoked code cannot be rotated. This complements Hub/recSender tests; it still
does not prove a published package or production deployment.

The latest Runtime integration regression uses real TLS WebSockets and SQLite,
then reads the caller-visible IPC frame over net.Pipe. Before the correction,
both mutations committed but returned relay_error: successful Relay replies
omitted device_context and Runtime expected ownership State. The corrected
reply uses the shared action-specific types and retains context synchronization;
malformed or mismatched results remain unknown. The following ownership query
must still succeed on the same connection. This does not exercise separate OS
Runtime processes or packaged CLI/MCP invocations. Wire result fields are owned
by docs/protocol.md, not duplicated here.

The shared `access.Request` validator bounds code IDs to 128 UTF-8 bytes and new
secrets to 4096 UTF-8 bytes with no NUL. Oversized or malformed requests fail
before Relay transaction or secret hashing; CLI and MCP therefore share the same
fail-closed input boundary.

## Files and validation for step 1

Domain: `internal/accesscode/management.go` (intent/replacement rules) and the
existing Code.Revoke transition. Persistence:
`internal/storage/sqlstore_code_management.go` and migration module, with Store
interface additions. Keep code management separate from target epoch/settings.
Run focused accesscode/storage tests, related vet/build, full Go and native Linux;
record exact boundaries and independent review. Never present this first step as
a complete management endpoint or verified process cancellation.

## Evidence and outstanding integration

Before-fix tests reproduced active child sessions/resume after revoke, failure
to repeat revoke or revoke consumed codes, and missing rotation support. Current
tests cover exact-family cascade, unrelated siblings/devices, ended-session
active proofs, one-time use/unknown/duplicate-unknown refusal, original expiry,
receipt replay after consume/revoke/further rotation, invalid intent/overflow,
three SQL write-fault stages, real two-connection concurrency and stale snapshots.
The stale-snapshot tests exercise revoke/rotate committing first; other orderings
are supported by cascade tests, not an exhaustive interleaving proof.

A wrapper around the real SQLite driver returns an error AFTER real Commit;
production WriteAccessCode returns that error once and a separate connection
reads the durable receipt. This proves no automatic transaction replay in that
case, not a complete caller-side unknown-result recovery flow.

v12 migration fixtures retain ordinary active consent, consumed one-time history,
multiple unknown records and target epochs. A late failure first observes the
prior resume update inside the migration transaction; after rollback the resume,
DDL and version marker are all restored. Independent review's timestamp mismatch
was reproduced and fixed by sharing the rotation's authoritative time with its
revocation helper. Public integration must supply trusted authority time, never
a caller-controlled timestamp. No plaintext secret is exposed by these APIs.

Steps 2-4 above remain required: per-code routing/publication fence, running-task
lease invalidation and recipient-authorized management, followed by Runtime/CLI
secret custody, process acceptance and clean signed candidate. A revoked SQLite
row is not a claim that an already-running task stopped.

Foreground exec renewal enforcement is now implemented separately in
[session execution leases](2026-11-18-session-execution-lease.md), including a
real native-process component test. It does not complete the public management
fence, background handoff propagation or Runtime/CLI management entrypoints.

Native Linux report:
`non-gui-release/code-management-linux-20261118-095532/report.json`: 93 top-level
tests (accesscode 12, storage 33, relay 32, noderun 16), source/uploaded binary
hashes checked, owned offline container absent, cleanup_errors empty. Windows
focused packages, relevant vet and CLI build passed. Final Windows
`go test -p 2 ./...` exited 0 (CLI 508.996s, noderun 17.674s, relay 19.953s,
storage 29.854s; unaffected packages cached). Full log:
`dist/verification/code-management-full-go-20261118.log`.

Final verification checkpoint (2026-11-18): after fixture-hardening review, Windows `go test -p 2 ./...` exited 0 (`dist/verification/non-gui-final-full-go-20261118.log`); affected CLI 114.055s, other packages passed/cached. `npm test --prefix packages/mcp` exited 0 (74 passed, 2 platform skips). `version --json` orphan probe count is 0. Independent Runtime service-process/IPC acceptance remains explicitly open; current session tests use two Daemon objects in one process with live TLS/net.Pipe.
