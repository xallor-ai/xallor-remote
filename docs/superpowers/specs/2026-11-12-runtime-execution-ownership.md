# Runtime as the Single Local Execution Owner

Status: approved design, implementation in progress
Scope: non-GUI Runtime, CLI, MCP, TUI and release lifecycle. GUI is excluded from this slice.

## Goal

Make the Go Runtime the only local component that selects, authenticates, activates, starts, observes and cleans up a Runtime binary. MCP, CLI, TUI and GUI are thin IPC clients and never independently select or spawn a Runtime candidate.

## Invariants

1. One profile has one Runtime owner and one activation state machine.
2. A head requests runtime.ensure through local IPC; only the Go Runtime owner starts or switches a binary.
3. Provenance verification, manifest/state binding, compatibility, readiness, PID/hash/build checks and pending recovery live in the owner.
4. A head never receives a mutable candidate path as authority to execute.
5. A verified candidate is copied into an owner-controlled activation snapshot before process start. The owner retains it until child exit and cleanup.
6. Source, snapshot, readiness, identity and cleanup failures fail closed. No candidate retry.
7. A baseline without original proof is rejected before the old process is stopped.
8. MCP/CLI/TUI/GUI status comes from the same IPC status response.
9. Device identity, grants, authorization, process occupancy and entitlements remain separate.

## IPC contract

Add runtime.ensure to docs/ipc.md. Request: {id, method: runtime.ensure, params:{required_profile_format, required_protocol}}.
Success returns runtime state, version, revision, protocol, profile_format, sha256, pid and opaque activation_id/launch_id.
Errors: runtime_source_unverified, runtime_profile_mismatch, runtime_pending_recovery, runtime_start_failed, runtime_cleanup_uncertain.
runtime.ensure is idempotent; reconnects query runtime.status and never replay an old ensure request.

## Owner activation lifecycle

1. Lock profile state and authenticate original manifest and binary.
2. Copy verified bytes and original manifest bytes into a unique owner activation directory.
3. Recheck size, digest, state binding and build identity.
4. Start only the activation copy with the profile-format guard.
5. Wait for IPC readiness and verify PID, path, SHA, protocol and profile format.
6. Persist pending/current/previous atomically.
7. Keep the activation directory until process exit and cleanup are confirmed.
8. On failure stop only the owned candidate and restore one independently authenticated previous activation, or return cleanup uncertain.

## Head behavior

There is one bootstrap exception: when no owner is running, a trusted launcher may start the owner exactly once from a private, source-verified snapshot. That bootstrap is not a second Runtime or a second state machine; once IPC readiness is reached, all heads use the owner contract. The bootstrap must never return the candidate path to another head, select a fallback after an unknown result, or own remote execution.

Fresh installation requires completely empty selection references and one
authenticated requested bundle. Recheck both absence and empty selection under
the lifecycle/install locks; a concurrently appearing owner or selection is a
refusal, not success for the requested bundle. If a complete Current already
exists but is stopped, bootstrap that original verified selection before sending
upgrade/rollback through owner IPC. Never use the requested upgrade as a fallback
bootstrap image. Pending or unfinished operation history cannot enter this normal
cold path and requires explicit recovery.

For legacy pending state without an unfinished operation journal, explicit
rollback bootstraps a source-authenticated recovery role. It uses the retained
Current baseline; if the first initialization failed with no Current, only the
original Pending image may run clear-only recovery. There is no cache fallback
and lost original proof refuses before any process change. The role verifies its
actual image, original source, request/profile/state and parent binding, then
checks absent serving IPC, released Runtime/job resources and unchanged selection
again under lifecycle admission. It never automatically resumes an unfinished
journal transaction. The bootstrap head waits on its exact child process and
observes the correlated settled journal after exit; this permits a truthful
clear-only result when no serving Runtime remains, without running Rollback in
the head. Process or resource uncertainty still returns an uncertain result.

Inherited input for service, transaction and recovery roles has a five-second
read deadline and bounded payload. Native nonblocking pipe reads obey context
cancellation and close their owned input end; EOF is not the time bound. Source
and parent checks retain their separate admission budget. No operation or
identity is initialized by an incomplete input message.

Recovery and service entry share the Runtime lifetime mutex. After confirming
Stop, recovery retains it while clearing old selection or publishing a new
pending launch; it releases the mutex only before replacement Start or on role
exit. A service rechecks its launch gate while holding that mutex, before loading
identity or opening IPC. Each launch has a fresh identity retained through commit,
so a delayed old child cannot regain permission when the same activation is
selected again. Normal public bootstrap refuses incomplete records; explicit
recovery revokes or refreshes their launch authority before releasing the mutex.

- CLI ensure is a wrapper for runtime.ensure.
- MCP ensureRuntime connects to the fixed profile IPC endpoint and asks the owner to ensure. It never spawns findBinary output.
- runtime-bin is a thin local Runtime forwarder; it has no candidate selection authority.
- TUI and future GUI use the same owner contract.
- runtime install and rollback share the activation implementation.

## Testing

### Explicit lifecycle handoff implementation boundary

The approved owner takeover includes explicit install/rollback; calling the same
release package from CLI is not sufficient. `internal/runtimeowner` owns durable
request admission and transaction progress. `internal/release` remains the one
activation algorithm. An owner-launched internal transaction process must survive
replacement of the serving Runtime; heads only submit and observe.

Each explicit request has a random 128-bit correlation ID and an intent digest.
Owner admission holds a profile lifecycle lock and persists the request before
dispatch. IDs and outcomes are retained; a repeated ID is observation-only and
changed intent under the same ID is rejected. Another unfinished transaction
blocks admission even when its process has exited. Querying an absent journal
does not create a profile or start anything.

The journal uses the existing SQLite dependency, full synchronous writes, and
bounded individual records. Before each stop/start/commit/ready effect, the owner
persists its intent; afterward it records confirmed, proven not applied, or
unknown. Generic errors are not proof of non-execution. An interrupted or unknown
effect cannot be repeated automatically. A completed install requires confirmed
selection commit and ready acknowledgement of the same artifact; losing the
acknowledgement retains uncertainty for explicit reconciliation. The journal
records observations, not source-verification or process-control authority.

Journal schema 5 binds both database metadata and each record to the configured
absolute lifecycle namespace. Heads capture that namespace before submission;
the serving endpoint checks it before launching a worker and subsequent receipts
must match the original namespace even if ambient profile selection changes.
Earlier schema 1/2/3/4, transplanted databases/rows and missing metadata refuse without
automatic repair. This correlation is not a MAC, host identity or process-release
proof. The exact binding format is specified only in docs/protocol.md.

Each effect now retains a bounded exact pre-selection digest and typed slot
identities, an intended post-selection for writes, and its target activation and
launch before dispatch. An independent post-read records the observed selection.
Missing or mismatched post-observation retains uncertainty even when the callback
returned success. Commit/ready completion requires the same activation, launch,
and exact committed selection, explicitly tied to the authenticated original
requested target. Schema 5 adds immutable resolved source/manifest/activation
binding and requested/baseline_prepare/restore/cancel purposes. Restoration can
settle the original request as failed, never as requested success. Schema 4 stop
evidence is retained: prepared process/image/launch identity and independent
stop observation are required; disk selection alone is not release proof. This step does not reconcile or
terminalize earlier uncertain operations and grants no replay authority.

The release adapter distinguishes a failed Start whose owned resources were
confirmed released (`quiescent_failure`) from a proven undispatched action and
an unknown outcome. This allows one independently verified baseline restoration
without claiming the candidate never ran. Preparation, selection commit and
ready errors cannot be guessed into clean failures. The coordinator must inspect
the whole release result when recording its terminal outcome, including cleanup
uncertainty that occurred before the first journaled effect.

An owner-to-worker handoff uses a private inherited pipe with operation ID,
intent digest, profile/state digests and selected activation/image identity.
The worker reauthenticates the original release proof and its own loaded image.
The serving process retains its launch ID from the authenticated inherited permit.
Ready observations and lifecycle dispatch must match that process-bound ID with
the stable selected launch. The handoff carries OwnerLaunchID and its fresh
comparison includes this value; changing the disk nonce for identical artifact
bytes cannot authorize a replacement launch or bypass ensure through direct IPC.
This is launch identity evidence, not durable journal reconciliation by itself.
Its OS parent PID must equal the kernel-observed server PID of the same IPC
connection used to query the owner. Self-reported owner metadata is insufficient:
independently bind the parent's original executable object and bytes to the
selected image (Windows process-image file mapping; Linux /proc/PID/exe).
Hold the initial parent process observer across admission and reject exit or
selection changes; do not turn a successful observation into a reusable permit.
This does not attest mutable process memory or defend against OS compromise.

This module is a prerequisite, not an exposed IPC capability. Before exposing
install/rollback, complete the authenticated owner-to-transaction-process handoff,
fixed profile query endpoint, authoritative state reconciliation and strict
no-owner recovery entry described in the implementation plan. A head must never
fall back to executing the journal's paths or directly resuming a transaction.

The first present-state reconciliation tranche (R1) remains owner-internal and
purely classificatory. Its typed evidence requires the immutable requested
binding, a confirmed requested commit, and a current owner observation bound to
the same activation and launch: kernel IPC peer PID, authenticated loaded image
and source proof, exact stable selection, and settled jobs/runtime/source/probe/
observer debt with no original worker or reconciler active. A baseline restore,
same-SHA identity mismatch, stale namespace/record digest, missing or unstable
ready observation, or unknown debt is unresolved. R1 does not expose IPC/CLI,
append a journal resolution, replay an effect, or grant authority. A future
owner-only resolution must preserve the original record/outcome/effects and mark
the historical ready acknowledgement as uncertain with evaluator capability
`runtimeowner/reconciliation-r1/v1`.

Unit tests cover activation, binding, pending refusal, identity, cleanup and replacement.
Go integration covers two signed versions, readiness failure, restore and missing baseline proof.
MCP tests prove no direct candidate spawn and preserve structured errors.
Windows/Linux use isolated profiles; production remains read-only.
