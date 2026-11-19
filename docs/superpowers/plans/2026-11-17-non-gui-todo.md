# Non-GUI delivery TODO and evidence ledger

Status date: 2026-11-19 (Asia/Shanghai)

This is the execution ledger for the remaining non-GUI work. It does not close
the linked GitHub issues or claim a production release. Every item is classified
so local tests, historical reports, draft candidates and production evidence
cannot be mixed.

## Current integration gate — PR #108

The user explicitly authorized candidate commits, PRs, and integration after
review and CI pass. The main-based branch `codex/non-gui-rc1` is now published
as [PR #108](https://github.com/xallor-ai/xallor-remote/pull/108), starting from
`be9a32c4c9713b2c38b1d54664b1183ef3bb2931`. The original PR97 worktree is retained.
Candidate A is `0.3.0-rc.1`; candidate B remains planned as `0.3.0-rc.2`.
Neither protected tag nor signed A/B acceptance has been created in this batch.
Production deployment and the release environment's human approval are separate.

PR CI is currently failing. Open repairs concern bootstrap snapshot ownership,
the existing GUI development bridge's removed executable-selection import,
explicit native-owner mechanics for the GUI test harness, cross-layer report
producer schemas, and Linux descendant-exit proof. Fixing the existing bridge
does not add GUI product functionality. Do not merge or create release tags
until the final commit passes CI and review.

The initial Windows full Go run failed in native handoff cleanup/readiness/probe
cases. A later full run passed; replacing hardcoded version fixtures does not
establish the cause of those failures. Python discovery initially timed out in
`test_package_stdio`, then passed unchanged (123 tests run, 2 skipped). These
first failures remain evidence. Old local passing runs below do not certify
the new integration commit or replace remote CI.

## Pre-integration evidence checkpoint — 2026-11-19

Integration review covered 239 changed implementation files against HEAD:
127 lifecycle files read in full,33 heads/release files read in full, and79
consent files reviewed as new implementations plus tracked hunks/call context.
Four concrete P2 findings were reproduced and fixed: CLI session reply deadline,
concurrent/cancelled approval writes, Linux cold-recovery acceptance SIGPIPE,
and resume publication after same-code revoke/rotate. Cross-review found no new
substantive issue in those fixes. Windows full Go8288 passed with explicit skips;
MCP74 passed/2 platform skips. Twelve native Linux top-level cases pass with no
skips, stable source/upload hashes and container removal. These are controlled
tests, not signed release acceptance. Full review/test evidence remains distinct.

Read-only publisher probe confirms the existing xallor-agent account has repository
admin and protected-tag bypass. No account switch or permission mutation was made.
A private-index export reconstructs the non-GUI candidate on main rather than
including PR97 GUI commits; this produces a tree/patch, not a commit. Explicit
commit authority was outstanding at that checkpoint and has since been granted;
genuinely signed A/B acceptance remains outstanding.

Candidate A metadata is now0.3.0-rc.1, with B proposed as0.3.0-rc.2. Go/npm version
metadata is aligned; two version-dependent test assumptions were corrected to
the already supported prerelease contract. Production verification was unchanged.
Candidate-A full Go and both native four-case MCP matrices pass with recorded
source/cleanup checks; these are local/controlled evidence, not signed candidates.

Signed-release readiness audit now distinguishes local harness defects from
external gates. Two new owner-protocol-1 signed candidates are required; old
v0.2.5/v0.2.6 cannot be the current running baseline. A separate read-only
Windows/Linux signed lifecycle workflow is now wired locally, not dispatched.
It requires exact A/B tags and commits and runs from the protected B source.
Linux startup-fault now uses the actual
worker/native candidate and same-operation structured restoration, with controlled
native acceptance passed. Windows equivalent now also passes native mechanics;
genuine signed A/B and actual remote workflow acceptance remain open.
The signed npm-installed CLI/MCP entrypoint harness is now locally wired as a
separate B-candidate CI stage. Controlled native head mechanics pass on both OSes;
real old v0.2.6 passes signature/install/payload checks but fails owner-protocol
preflight. Genuine positive acceptance still requires a new compatible candidate.
Npm installation is supervised by the existing Windows Job / Linux process-group
boundary; arbitrary Linux setsid/setpgid escapes and dependency signatures are not claimed.
Windows image/process-exit observations are now implemented and native mechanics
tested; current signed A/B lifecycle still needs genuine candidates. The stale
success-report/setup-resource P2 is fixed with5 passing regressions and CI hooks.
See [current signed-candidate audit](../../attested-candidates.md).

New CI evidence gating binds all six cases to four actual A/B/A/A owner launches,
ready identities, authenticated manifest digests and kernel exits. Recovery reuses
the existing same-operation effect validator; seven missing-identity/effect/parent
counterexamples failed before the gate fix and pass afterward. Windows56304 and
Linux88274 controlled native recovery passed with cleanup; their new snapshot
fields also pass the CI fault validator. These are not signed candidates.

The subsequent installed-package batch adds only acceptance tools/tests and CI,
not product Go/MCP behavior. Windows51844 and Linux74368 head mechanics pass with
cleanup. The new package-supervisor reclaims a live ordinary descendant after a
successful root exit on both OSes.54 Python checks and Actionlint1.7.12 pass.
Full Go1185 exited0:38 packages passed,36 cached,CLI reran395.398s;13 test skips
(including4 opt-in MCP cases) remain explicit in package-entry-summary-20261119.json.
Those4 cases have separate earlier native matrix evidence; this run is not their
rerun. Source hashes remained unchanged. No commit, candidate publication or deployment.

Latest MCP checkpoint (2026-11-19): four-case Windows/Linux stdio matrix passes
request/recipient approval/scoped invoke/revoke, real controller Runtime restart
and one-use MCP resume, plus running-task owner MCP revoke/rotate. Each OS confirms
nine owner launches and ten Node heads cleaned, four long-task processes exited,
all roots removed; Linux restricted container removed and input hashes stable.
Full Go32620 exited0 with36 packages passed (CLI reran409.198s,35 cached),11 without
tests. All four MCP cases explicitly ran/passed; no skip/failure events. Seven
evidence plus nine documentation tests pass; independent review found no new
substantive P0-P2. See `mcp-matrix-summary-20261119.md`. Published-artifact and
signed-release gates remain open; this is current uncommitted WIP evidence.

Preceding descendant checkpoint (2026-11-19): Windows/Linux ordinary handoff child
revocation acceptance passed, with both image-bound tasks live before mutation
and exited at terminal observation/cleanup. All three owners, profiles and the
owned Linux container were cleaned. Full serial Go run80902 exited0 with36
packages passed (CLI reran,35 unchanged packages cached),11 without tests, no
failure events. See `handoff-descendant-summary-20261119.md` and
`handoff-descendant-full-go-summary-20261119.json`. Detached descendants, official
Agent and signed candidate lifecycle are not implied by this fixture result.

Preceding checkpoint (2026-11-19): full serial Go run90369 exited0 with no JSON
failure events (CLI365.837s, noderun37.340s, Relay42.041s); raw events and package
summary are `root-cleanup-serial-full-go-20261119.jsonl` and
`root-cleanup-serial-full-go-summary-20261119.json`. MCP build/unit tests passed
74 with2 platform skips. Fresh Windows cross-layer run79397 passed18 scenarios,
including actual stdio MCP execution/nonzero exits, CLI/MCP owner reuse, Runtime
restart, cancellation/process-tree cleanup, Relay reconnect and schema13 metadata
guard. `cross-layer-windows-current-20261119.json` records complete cleanup and
native-owner-mechanics scope. Linux then passed the same18 named scenarios after
an explicit current MCP build, fresh Go fixture/tree builds and verified offline
uploads: `current-mcp-cross-layer-linux-20261119-002931/report.json`. Build inputs,
dist artifacts, selected source hashes and container cleanup were checked; no
official dependency-signature or hermetic-build claim. Signed/published-package
gates remain open. Historical failures below
remain evidence, not current pending handles.

The following table describes the pre-integration uncommitted WIP, not a release. It separates
completed local validation from remaining acceptance. Linux reports belong to
their specific source snapshots; they do not collectively certify one release.

| Area | Verified state | Evidence and boundary |
| --- | --- | --- |
| Independent Runtime approval, resume and foreground exec | Windows/Linux targeted passes, real runtime-serve processes and OS IPC; owner revoke/rotate reaps real parent/child tasks | [Process spec](../specs/2026-11-18-independent-runtime-e2e.md), `independent-exec-linux-20261118-204351/report.json`; nine owner launches and four task processes exited, profiles/container removed. Controlled provenance, loopback WS, occupancy sampled rather than continuous |
| Handoff consent expiry | Original red cases corrected; Windows package/full suites and seven Linux authority/session cases pass | `dist/verification/handoff-consent-expiry-summary-20261118.md`; original and current shorter consent deadlines bound post-I/O renewal. Module clock tests, not Runtime handoff E2E |
| Handoff cancellation gate | Original handler leak and subsequent registration failure reproduced; fixed handler passes Windows/Linux injection and full Go | `dist/verification/handoff-gate-summary-20261118.md`; temporary Go overlay, exact case event checking, CI execution/upload configured but remote CI not run |
| CLI handoff consent propagation | `submit/get --session-id` now forwards top-level consent separately from nested Cursor session; Windows targeted and four Linux top-level cases pass | `handoff-cli-session-linux-20261118-212642/report.json`; Cobra/RPC seam plus same-process Runtime objects, net.Pipe framing, real TLS Relay and SQL. Deliberate workspace preflight failure, no Agent execution |
| Windows/Linux independent-process handoff under shared grant | The same six scenario groups passed on both OSes: real CLI subprocess calls, durable receipt/dedupe, mode/output errors, deadline reaping, repository/attachment checks and normal stop/restart without replay | Windows: `dist/verification/independent-handoff-windows-final-20261118.json`; Linux: `independent-handoff-linux-20261118-220335/report.json`. Distinct Runtime PIDs, changed restart launch ID, equal image hash and cleanup complete; Linux selected source and embedded-file hashes match, build options fixed and container absent. Test-binary dispatcher, controlled verifier/Agent, loopback WS; not logical consent, crash recovery or signed-release acceptance |
| Windows/Linux logical-consent handoff invalidation | Independent Runtime owners, OS IPC approval/running query, revoke/rotate or real-clock expiry, durable unknown/authorization_lost, Agent kernel exit, job release and denied old receipt access | Windows revoke and rotate/expiry targeted passed; Linux final three-case matrix `independent-consent-matrix-linux-20261118-224155/report.json` passed with clean container removal and unchanged selected inputs. Expiry uses approved 45-second deadline, no test management/storage mutation; live code remains active. Controlled verifier/Agent, SQL seed and parent loopback Relay; no official Agent/TLS or descendant-tree claim |

Current-code full Go after consent matrix and head-readiness synchronization:
session 7597 exited 1, log `dist/verification/consent-matrix-head-ready-full-go-20261118.log`.
Uses the existing CI package budget (`-timeout 20m`), unchanged per-case deadlines.
CLI hit the cumulative 20m timeout, with prior native lifecycle/probe failures
and one receipt-read sharing violation. noderun and Relay also had timeout/cleanup
failures. The final CLI case had only been running 56s at alarm; this does not
prove that individual case deadlocked. No all-suite pass is claimed.
The next public-CLI consent test is implemented but not yet accepted: it launches
real command subprocesses for submit/get, verifies revoked refusal and same-owner
PID/launch/image reuse. Independent review found and corrected test-helper
startup cleanup and timeout-as-refusal risks. Original focused session17544
exited 1: CLI submission returned a receipt, but Agent readiness was not observed
within 15 seconds and the persisted receipt remained received. Both Runtime
owners were cleaned up. The stricter helper changed after that run began and
requires a subsequent run with preflight investigation. No public-CLI consent
pass follows from that first attempt. The stricter diagnostic rerun30050 later
exited 0 (74.755s): submit/get/refusal/status CLI processes all reaped, same owner
identity confirmed, Agent exit and profile cleanup passed. Initial readiness
failure remains unexplained. No new broad run has been launched to mask the failures.

Windows observation issue remains unresolved: holding the current os.Open reader
was reproduced to block atomicfile.Write replacement (Access denied). A proposed
test-only FILE_SHARE_DELETE reader also failed the same controlled regression,
so the unproven helper/test edit was withdrawn. Logs independent-snapshot-red and
independent-snapshot-final preserve both failures. Do not claim this read race
fixed or attribute all other timeouts to it. See snapshot-observation-investigation-20261118.md.
The replacement test design now has focused Windows/Linux validation:
handoff cases add a third independent owner Runtime used only for receipt get,
never code redemption/resume/submit. Active terminal polling goes through its
authorized IPC path; original controller denial remains required. Disk receipt
is audited only after target stop/Wait/endpoint absence. This removes the active
receipt-reader/writer interference without changing product file APIs. Static
review found no P0-P2. Windows expiry/public-CLI run33842 exited0 (158.728s),
log `dist/verification/independent-observer-ipc-windows-20261118.log`.
Linux four-case observer/revoke/rotate/expiry/public-CLI matrix passed, report
`independent-observer-ipc-linux-20261118-233623/report.json`: twelve owner launches
exited/reaped/IPC absent, four Agent exits, all roots removed, selected inputs
unchanged and container absent. Each handoff case now requires three owners;
earlier two-owner reports remain historical evidence only. Full Go run42855
exited1, log `dist/verification/observer-ipc-serial-full-go-20261118.log`;
all packages included with -p1, unchanged case deadlines. Only CLI failed
(778.088s): natural-expiry semantics and all three process exits passed, but
RemoveAll could not delete the observer activation exe (Access denied). Other
packages passed/cached, including noderun26.541s and Relay47.564s. The test root
xr-ipc-2668475368 is retained. Later read-only inspection found attributes32 and
no PIDs from NtQueryInformationFile class47, not proof of the original lock cause.
The exact retained image was later SHA-verified and deleted; only verified empty
directories were removed afterwards. The old failed test result is not rewritten.
Test-root cleanup now captures physical identity at creation and only after all
process/handle obligations settle retries explicit Windows access/sharing denial
for one second, rechecking root identity each time. Three native red/green cleanup
cases and the original expiry case pass; Linux four-case observer matrix passes
(`independent-observer-ipc-linux-20261119-000400/report.json`). Full serial run90369
exited0, with JSON events in `root-cleanup-serial-full-go-20261119.jsonl`.
All tested packages passed; no JSON failure events. Its CLI package passed365.837s.
See `dist/verification/independent-root-cleanup-summary-20261119.md` for limits.

Cross-layer acceptance guard maintenance: schema8/12-table assertion was stale;
current migrations and a fresh isolated real Relay prove schema13/18 metadata
tables. The updated exact allowlist still rejects unknown/missing tables and
old/future versions; existing key/grant checks remain. Report begin/completion
guards prevent stale success and reject non-object result rows. Four schema,
seven report and nine docs tests pass. The subsequent Windows full cross-layer/MCP
run passed18 scenarios on the final harness; Linux subsequently passed the same
18 named scenarios on explicitly rebuilt MCP/current Go artifacts.
Earlier independent-consent run 81698 exited 1 with the native-start exit72 below;
log `dist/verification/independent-consent-handoff-full-go-20261118.log`.
Preceding scope-matrix session 91106 exited 0 (noderun
21.437s, other packages cached), log
`dist/verification/handoff-scope-matrix-full-go-20261118.log`. Preceding full
diagnostic run 32118 also exited 0 (CLI 264.775s), log
`dist/verification/handoff-cli-session-full-go-diagnostic-20261118.log`.

Intermittent failure under investigation: full sessions 95793 and 81698 exited 1 because
TestNativeStart_should_admitSelectedChild_andReuseOwner returned exit 72 at
foreground shutdown. Original log: `dist/verification/handoff-cli-session-full-go-20261118.log`.
Three isolated repetitions passed (16.628s); they and the later full pass do not
diagnose or supersede that failure. Failure output now includes the captured
child log. The second recurrence reports the exact final owner-handshake absence
error. A test synchronization gap is reachable and consistent with that error:
external readiness did not prove the original head had completed its final
handshake before the test stopped the owner. The test now waits for an atomic,
original-head-PID-bound event emitted only after real ensure returns successfully.
Production cmdStart only extracts an ensure-injected constructor; its default
path, readiness requirements and errors are unchanged. Three native repetitions
pass (59.596s). This does not establish the unique cause of every historical exit72.
Native CLI build and both handoff help
commands passed; nine documentation checks passed; independent static review
found no P0-P2 in the CLI propagation batch.

The preceding post-gate full Go run exited 0 (CLI 255.308s, noderun 30.352s,
Relay 49.852s, storage 18.418s). The preceding post-expiry run also exited 0
(CLI 395.137s). These are historical batch evidence, not substitutes for the
current-code run above.

Immediate remaining work:

1. Ordinary handoff descendant revocation now passes Windows/Linux targeted
   acceptance: both live image-bound tasks exit, jobs release, three owners stop,
   roots/container are removed. See `handoff-descendant-summary-20261119.md`.
   Full Go regression80902 passed after this fixture change. Detached descendants
   and crash recovery remain separate.
   Public CLI consent submit/get and revoke/rotate/natural expiry are covered;
   official CLI acceptance is a separate gate.
2. Logical-consent MCP request/recipient approval/scoped info/read/native exec,
   denied write/no file, owner MCP revoke, revoked invocation/account-fallback
   refusal and same-owner reuse now pass targeted Windows/Linux stdio runs.
   Wrapper and CI gates reject skips and incomplete cleanup; see
   `independent-mcp-consent-summary-20261119.md`. Full Go run30205 passed with the
   actual MCP case enabled and executed, not skipped.
   The added real owner-restart/MCP-resume and running-task revoke/rotate cases
   now pass the four-case Windows driver and Linux native matrix76031, with all
   process/profile/container cleanup confirmed. Full Go32620 passed; all four
   MCP cases actually executed with explicit Node/entry inputs, no skip.
   They require proof rotation/refusal and independently observed task
   exits, not only error text. See `mcp-matrix-summary-20261119.md`.
   Published-artifact entrypoints remain separate.
   Shared-grant public CLI/stdio MCP cross-layer passes on Windows/Linux.
   Handoff submit/get scope separation
   and absent/wrong-consent submission passed Windows targeted tests (3.265s)
   and Linux two top-level cases, with no skips, identical source hashes and
   clean container removal (`handoff-scope-matrix-linux-20261118-214038/report.json`).
   This is same-process Runtime/TLS wire coverage. MCP currently exposes no handoff
   tool; this batch adds none.
3. Two clean owner-compatible protected-tag signed candidates A/B, then the complete
   Windows/Linux install/upgrade/rollback/failure-recovery gates below.
4. Production changes remain separate from local evidence and require the
   applicable deployment authorization; nothing above is a production upgrade.

Historical failures remain preserved: the first independent-exec Linux attempt
timed out before testing (`independent-exec-linux-20261118-202621/report.json`);
the same runner later passed without relaxed deadlines. Initial Windows fixture
failures and later hardening are recorded below. No failed report was replaced.

## Earlier implementation evidence

Latest receipt/input correction (2026-11-18):

- Rotation now returns the original child metadata from the authorized write
  transaction after confirmed commit. State, policy, generation and original
  timestamps survive receipt reuse, including consumed/revoked/further-rotated
  and naturally expired children. It is not a new execution authorization.
- Relay/Runtime use a private intent digest to bind the request. Runtime checks
  IDs, device/ownership, digest and metadata, then removes digest/secret from
  IPC output. Unknown commit never returns the temporary success metadata.
- CLI removed --new-secret; environment or explicit bounded stdin is required.
  Dual sources, empty/invalid/oversized input and invalid flags fail locally
  before the Runtime runner, without echoing values. Input handling is verified
  through real Cobra parsing and I/O, not a separate installed CLI process.
- Focused Windows, related vet/build, six documentation checks and independent
  read-only review pass. MCP build/tests passed (74 pass, 2 platform skips).
  Native Linux passed 25 top-level tests (CLI 3, Relay 17, noderun 5), no skips,
  matching source hashes, container_absent=true and cleanup_errors=[]; report
  `non-gui-release/rotation-secret-linux-20261118-163751/report.json`.
- Initial batch Windows `go test -p 2 ./...` exited 1: CLI launch Stop cleanup
  deadline, launcher inherited-output acceptance observation, and native owner
  journal Query SQLITE_BUSY failed. The same three cases passed in isolation
  (7.204s), which does not diagnose or supersede the failure. Logs:
  `dist/verification/rotation-secret-full-go-20261118.log` and
  `dist/verification/rotation-secret-runtime-repro-20261118.log`.
  No published package or independent CLI/MCP/Runtime E2E claim.

Verification-reliability progress (2026-11-18):

- A bounded `waitNativeObservation` now retries only explicit read-only
  `SQLITE_BUSY` under one caller deadline. A real `BEGIN EXCLUSIVE` fixture first
  failed this test; after release it observes the same record without replaying
  the operation. Cancellation and permanent errors remain terminal.
- Native fixture dispatch is now fail-closed: a copied test binary receiving a
  production-shaped non-`-test.*` command exits before `m.Run`; the regression
  proved the former unconfigured version command entered the whole test suite.
  NativeOwner version probing reuses the supervised bounded probe instead of
  bare `Output` with mixed fixture flags.
- Launcher observation tests now synchronize parent/child readiness and retain
  kernel exit watches before releasing the parent. Settlement timing starts
  after confirmed parent exit; the original context must remain live and the
  child must remain alive. Cancellation-before-child-start is distinct from
  inherited-output settlement. Focused launch/probe tests pass (6.197s).
- Stop diagnostics retain the first observation and independent process watch;
  they read buffered stderr only after foreground wait. No timeout was enlarged,
  lifecycle mutation replayed, or failure overwritten by deferred cleanup.

The latest full Windows suite from before these fixture changes remains failed
in the three original Runtime tests; rerun the full suite only after independent
review of this batch. Preserve original evidence and never treat isolated
focused green as a full-suite result.

Cleanup audit found a separate historical orphan chain: 115 copied test-binary
`version --json` probes descending from PID 20092, created between 14:03 and
14:26, with the original parent absent. Every executable path and command was
checked against the exact NativeOwner/NativeLaunch temporary fixture patterns
before terminating that owned process tree. Termination exited 0; none of those
probes remained. Evidence: `dist/verification/orphan-version-probes-20261118.json`
and `orphan-version-probes-cleanup-result-20261118.json`. No ordinary Runtime
service or test directories were removed. This proves a historical recursive
probe/cleanup defect, not the cause of today's later three failures or the old
UI freeze. Add fail-closed test-binary dispatch and bounded probe-tree cleanup
to the next fixture-hardening work; do not re-execute the historical bad binary.

The current WIP includes target/session lifecycle, code management and handoff
integration. Historical passing package tests below are not complete public
entrypoint acceptance. A new real TLS/SQLite/Runtime IPC test reproduced BOTH
code revoke and rotation committing successfully while IPC returned relay_error:
Relay omitted required device_context, and Runtime parsed code results as
ownership State. The current correction supplies the context and shares typed
code results; Runtime validates the action, requested IDs/secret and metadata.
Malformed ownership-shaped acknowledgements were also reproduced as false
success before this correction. Focused regressions, related vet/build and six
documentation checks pass. Independent read-only review found no new P0-P2 in
this correction. Native Linux passed 8 top-level tests with no skips, unchanged
Go source digest and verified container removal; report:
`non-gui-release/device-code-ipc-linux-20261118-151729/report.json`.
Full Windows `go test -p 2 ./...` exited 0 (CLI 289.842s, noderun 62.786s,
Relay 30.309s, storage 19.960s; unaffected packages cached). Log:
`dist/verification/device-code-ipc-full-go-20261118.log`.

The earlier full Windows suite passed (CLI 580.413s), but the preceding CLI
fixture timeout's causal diagnosis is not established: TestMain checks the
retained owner-fixture flag before the added version-probe flag. Do not call
the added flag a proven timeout fix. Original failure logs remain evidence.

Release verification was refreshed this afternoon: v0.2.6 online and offline
indexed-artifact verification passed, while the current signed-vendor launch
timed out and the signed lifecycle script stopped at source verification. See
`docs/attested-candidates.md`; there is still no current clean signed candidate.

Still open: independent-process running-task/owner-rotation/handoff acceptance,
public CLI/MCP service entrypoints, clean protected-tag/signed candidate lifecycle,
background receipt/cleanup E2E, management publication race with all external
clients, and production deployment. Basic independent owner/session/OS IPC
acceptance now has the controlled evidence linked above. GUI remains outside.

Transactional code authority correction (2026-11-18):

- Reproduced sixteen stale-actor/device cases before writes. Current eighteen
  cases reject revoked/deleted/expired credentials, disabled/re-enabled member
  versions, ownership transfer, device deletion/recreation and secret changes,
  preserving code/session/resume/receipt state after the external change.
- Six post-read SQLite interleavings use the independent side connection to
  commit revocation/version/secret changes. The production retry reads authority
  exactly twice and denies on the second read. This is not a simulated retry.
- Receipt reuse must reauthorize; real expiry during reads and complete actor
  identity comparison are covered. Inbound off/unused owned-grant changes do
  not block valid owners. Later credential revoke does not retroactively undo
  a management transaction that already committed.
- Real SQL read failure and injected reader failure remain unknown/quarantined,
  distinct from missing rows and definite denial. Authorization time means the
  final transactional check after reads, not continuous commit-time validity.
- Focused Windows, related vet/build, six documentation checks and independent
  read-only review pass. Native Linux passed 19 top-level tests (access 1, Relay
  16, noderun 2), no skips, identical source hashes and verified container removal;
  report `non-gui-release/code-authority-linux-20261118-160859/report.json`.
  Full Windows `go test -p 2 ./...` exited 0 (CLI 134.217s, noderun 27.211s,
  Relay 40.364s, storage 20.314s; unaffected packages cached); log
  `dist/verification/code-authority-full-go-20261118.log`.
  No independent Runtime/CLI/MCP package E2E claim.

Code-fence repair (2026-11-18, Windows/Linux component evidence):

- New `session_code_fence.go` fixes display-ID/UID target lookup and selects each
  route's original consent independently of the controller's current session.
  Revoke and rotate A/B after real resume on the same controller were red, then
  green: only the matching route is cancelled, and the other still renews.
- Target UID/epoch/generation mismatch and notification queue failure tests
  preserve occupancy and prohibit replacement-target cancellation or renewed
  authority after failed delivery. Controller-forged revocation reasons are
  stripped by Relay.
- Runtime installs the trusted revocation cause before cancelling approval;
  ordinary cancel stays ordinary. Both owner revoke/rotate native process tests
  first returned cancelled; after the fix they return authorization_lost with
  observed parent/child exit, intact startup output and zero job/transport
  occupancy. This is not merely a hand-injected target terminal.
- Focused noderun/Relay tests, related vet/build and six documentation checks
  pass. Independent read-only review found no new P0-P2 code issues. Native
  Linux passed 22 top-level tests (5 noderun, 17 Relay), no skips, source digest
  unchanged and container removed, cleanup_errors=[]; report:
  `non-gui-release/code-fence-linux-20261118-154534/report.json`.
  Full Windows `go test -p 2 ./...` exited 0 (CLI 210.392s, noderun 24.825s,
  Relay 39.753s, storage 18.719s; unaffected packages cached), log
  `dist/verification/code-fence-full-go-20261118.log`.
  No independent Runtime service/packaged E2E claim.

## Completed or currently evidenced

| Area | State | Evidence boundary |
| --- | --- | --- |
| Runtime ownership | Implemented WIP | Go owner/IPC tests and local matrices; not a published release |
| Windows IPC ACL | Implemented WIP | Real kernel DACL tests for normal/lifecycle pipes; client server-auth gap remains separate |
| Approval facts | Implemented WIP | Full immutable details, digest/exec binding, bounded writes, disconnect cancellation; Go tests/build pass |
| Source snapshots | Implemented WIP | Go/MCP private snapshot and replacement refusal tests; signed lifecycle still incomplete |
| Signed npm-installed entrypoints | Locally wired, genuine positive acceptance open | New authenticated-tgz install/byte-check/CLI/MCP/native-owner harness and B-stage CI gate; Windows/Linux controlled mechanics pass. Old v0.2.6 is rejected at owner identity/protocol preflight. Supervisor confirms ordinary descendants within Job/process-group scope; installed dependencies and global shim resolution are separate boundaries |
| CLI/IPC/MCP heads | Contract aligned | All heads use Runtime IPC; Windows/Linux controlled public CLI and actual stdio MCP matrices cover owner reuse, scoped operations, restart/resume and running-task revoke/rotate. MCP has no handoff tool; signed-release acceptance remains open |
| Usage/entitlement boundaries | Provider-neutral | Local usage/entitlement primitives only; no commercial provider or billing |
| Documentation governance | Current WIP | Status banners, roadmap and this ledger; issue evidence comments synchronized |
| Signed metadata primitive | Implemented WIP | Ed25519 root/targets/snapshot/timestamp verification, cross-signed root rotation, bundle chain and atomic version/root policy; not wired to install/update |
| Access-code domain state machine | Implemented WIP | Pure one-time/expiring/long-lived reservation, final approval, unknown, revoke and session checks; stable production semantics not published |
| Access-code SQLite/transaction core | Implemented WIP | Current schema 13 includes access-code tables, resume capabilities and code/session consent migrations; transactional reservation/finalization/unknown/revoke/session-end adapters are implemented. Stable published production redemption is not claimed |
| Access-code session digest | Implemented WIP | Canonical digest binds request/session/target/code generation/scope/session expiry and is used by protocol/Relay approval WIP |
| Access-code target binding | Implemented WIP | Session requests carry target UID and Relay checks code device/ownership binding before reservation; full external-client migration remains |
| Access-code session envelopes | Implemented WIP | Structured request/approval/result fields, validators and Hub routing; stable production contract not published |
| Access-code Relay session gate | Implemented WIP | Reserves and publishes digest-bound recipient approval, enforces approved scope and supports authenticated resume; controlled Windows/Linux independent Runtime/MCP acceptance exists. Genuine signed/published acceptance remains open |
| Access-code Relay wire acceptance | Implemented WIP | Real TLS WebSocket test covers client request, target approval, session result and scoped invoke against SQLite store; cross-process production Runtime acceptance remains |
| Access-code production composition | Implemented WIP | CLI Relay command injects SQLStore into Hub access-code handling. Controlled independent Runtime, public CLI and MCP lifecycle paths are evidenced on Windows/Linux; this is not production deployment or signed-release acceptance |
| Access-code disconnect safety | Implemented WIP | Target disconnect marks pending reservations unknown, persistently revokes active session authorizations and removes session clients. Controller reconnect uses authenticated one-use resume; it does not silently restore consent after target disconnect or epoch change |
| Access-code revoke propagation | Implemented WIP | Ownership invalidation persistently revokes session authorizations outside Hub mutex; full device/session revoke wire matrix remains |
| Access-code expiry settlement | Implemented WIP | First expired-operation observation persists session `ended`; no active row remains after in-memory denial |
| Runtime session approval head | Implemented WIP | Separate local IPC subscribe/list/respond hub with digest binding, timeout and last-head cancellation; Relay inbound adapter is wired WIP |
| Runtime Relay approval adapter | Implemented WIP | Device Runtime validates approval requests and returns digest-bound local-head decisions. Controlled Windows/Linux acceptance covers recipient approval, controller restart/resume and owner revoke/rotate with cleanup; signed candidate acceptance remains open |
| Runtime session request IPC | Implemented WIP | `session.request` waits for active/rejected/unknown and subsequent CLI/MCP/IPC invokes carry `session_id` into scope enforcement. `session.resume` is wired through Runtime IPC and public CLI/MCP; controlled restart, proof rotation/reuse refusal and scope preservation are evidenced |
| Runtime session unknown mapping | Implemented WIP | Client-link loss maps session waiters to `session_unknown`, preventing Relay errors from being mistaken for a definitive rejection or safe retry |
| Runtime↔Relay session acceptance | Verified controlled slice | Independent Runtime processes and real OS IPC cover approval, scoped invoke, controller restart/resume and owner revoke/rotate. Public CLI handoff and actual stdio MCP running-task cases pass on Windows/Linux. Parent loopback Relay, SQL-seeded codes and controlled verifier/Agent remain fixture boundaries |
| Session disconnect settlement | Implemented WIP | Client transport disconnect now persistently revokes its active session when no authenticated reconnect proof exists; pending/unknown and target disconnect paths remain conservative |
| Authenticated session reconnect | Verified controlled slice | Independent Runtime/MCP restart on Windows/Linux exercises wrong-secret and consumed/revoked proof refusal, rotated capability and preserved scope/expiry. Principal/authority binding and post-commit publication-race/unknown handling have focused regressions; this is not an exhaustive concurrency or arbitrary-crash matrix |
| Linux cross-build gate | Verified WIP | Go cross-compilation with `GOOS=linux GOARCH=amd64 CGO_ENABLED=0` produced native test binaries. This row is compile evidence only; native execution evidence is separate below and is no longer limited to cross-compilation |
| Linux runtime execution access | Verified controlled slice | Owned restricted containers executed independent Runtime, public CLI handoff, four-case MCP consent/lifecycle and 18-scenario cross-layer matrices, with recorded cleanup and container absence. Controlled fixtures only; no production host/service changes or signed-release claim |
| Resume capability domain rules | Implemented WIP | Pure active/consumed/revoked/unknown state transitions, binding checks, expiry and single-use consumption are covered. Resume tables introduced in schema 10 remain part of current schema 13 and persist atomic insert/consume/revoke/unknown transitions |
| MCP session request tool | Verified controlled slice | `remote.session_request` and `remote.session_resume` reuse Runtime IPC and environment secret boundaries. Four actual stdio MCP cases pass on Windows/Linux: request/approval/scoped invoke, Runtime restart/resume, running-task revoke and rotate. Stable published production acceptance remains open |
| Session-bound handoff | Verified controlled slice | Original/current consent expiry bounds renewal after permission I/O; Windows/Linux independent Runtime tests cover revoke/rotate/real-clock expiry, public CLI submit/get and ordinary-child exit with receipt/job cleanup. Controlled verifier/Agent, SQL seed and loopback Relay; detached descendants, official Agent and genuine signed lifecycle remain open. MCP exposes no handoff tool |
| Relay session-envelope budget | Implemented WIP | New session IDs, code IDs, digests, scopes and expiry metadata are included in outbox retained-byte accounting; Hub session routing is covered WIP |
| Storage wire-test timing | Stabilized WIP | Condition windows and expiry setup now start near the operation under test; full storage and access/relay/noderun focused matrices pass under package parallelism |

## S0 — release and trust governance (next implementation block)

Owner-only code management entry (2026-11-18): existing authenticated device
control now accepts `code.revoke` and `code.rotate`. It enforces account/owner,
device UID and ownership binding in the same storage transaction; rotation stores
only the new secret hash and returns public metadata through Runtime. Relay focused
tests cover non-owner/unauthenticated refusal and owner revoke/rotate. This is a
thin control-plane entry, not CLI/MCP secret custody or an independent management
service; existing connected-session publication fencing and background handoff
remain open.

Per-code in-memory fence (2026-11-18): successful owner revoke/rotate now
invalidates only sessions matching durable device UID + code ID + generation,
marks matching foreground routes non-renewable and sends target cancellation;
it preserves the route until a real terminal and leaves unrelated/new sessions
alone. A regression caught display device ID vs durable UID mixing in the first
helper; the corrected fence and related Go packages pass. This does not cover
background handoff or an instantaneous zero-window publication fence.

Management commit-uncertainty fence (2026-11-18): if owner code revoke/rotate
commits but its acknowledgement is lost, Relay quarantines the device route
before returning `relay_error`; a real wrapper test verifies durable row + cleared
route without replay. Reconnect reloads authority. This is conservative unknown
handling, not rollback or OS-cleanup proof.

Management input hardening (2026-11-18): shared access validation now rejects
oversized/invalid UTF-8 code IDs, oversized secrets and NUL bytes before hashing
or storage. CLI/MCP/Relay use this same validator; focused access/Relay/noderun
and MCP tests pass.

Session-bound handoff proof (2026-11-18): delegation claims now include the
original logical session ID, code ID/generation, target epoch and scope; Relay
renewal re-reads the durable session before issuing a lease. Explicit session
revoke/rotation or target epoch changes therefore deny old background proofs,
without changing receipt/OS cleanup semantics. Regression passed with real
SQLite-backed session revoke; relevant delegation/Relay/noderun/storage packages,
vet and build passed. See [handoff session design](../specs/2026-11-18-handoff-session-binding.md).
Handoff submit now also uses the target identity gate shared by code management
and settings, then rechecks the session before proof publication; relevant
delegation/relay/noderun packages still pass.

Final relevant package check after the entry: accesscode 3.045s, storage 30.654s,
relay 33.901s and noderun 25.941s passed; vet and CLI build passed. The broad
Windows suite's existing CLI native-owner 10-minute fixture timeout remains a
separate validation limitation.

Foreground session lease correction (2026-11-18): durable ended/revoked/expired
or narrowed sessions previously continued receiving full execution leases. The
Relay now revalidates each original route's consent and caps its remaining lease;
definite denial is irreversible for that route, while transient SQL faults do not
extend a lease or poison a recoverable route. Known expiry is detached/replied
before best-effort persistence. Native Windows component verification observed
real root/descendant exit, authorization_lost, and zero job/transport occupancy
after code revocation. See [evidence and limits](../specs/2026-11-18-session-execution-lease.md).
Targeted tests, vet/build and independent review passed; native Linux 68 top-level
tests passed (plus one ordinary-run helper skip), including the real process-tree
case. Report `non-gui-release/session-lease-linux-20261118-113005/report.json`
confirms source/binary hashes and container removal. The final focused Windows
rerun passed (accesscode 7.844s, storage 53.154s, relay 62.993s, noderun
46.774s). The broad Windows `go test -p 2 ./...` run failed at the existing CLI
`TestNativeOwnerTransactionSurvivesServingProcessReplacement` 10-minute
package timeout in `nativeOwnerBundle`; this is a validation limitation and is
not claimed as full-suite pass.
Per-code management/publication fencing, background handoff propagation,
Runtime/CLI management and independent signed-process acceptance remain open.

Typed code management transaction foundation (2026-11-18): explicit revoke now
cascades to the exact code/generation's consent and resume rows, preserving use
history; repeat revoke is idempotent. Rotation preserves policy/deadline, refuses
used or uncertain one-time eligibility, allocates device generations atomically
and persists stable intent receipts. Schema 13 preserves ordinary v12 consent
while retiring legacy children of already-revoked codes. See [contract and required next steps](../specs/2026-11-18-code-management.md).
Focused tests, native Linux 93 top-level cases, vet/build and independent review
passed; Linux report `non-gui-release/code-management-linux-20261118-095532/report.json`
confirms source/binary hashes and container removal. Windows `go test -p 2 ./...`
exited 0 (CLI 508.996s, unaffected packages cached), recorded in
`dist/verification/code-management-full-go-20261118.log`. This does not complete Relay management/per-code
fencing/lease propagation, Runtime/CLI custody or signed process acceptance.

CLI code-management head (2026-11-18, superseded input/output corrected):
`device code revoke --code-id` and `device code rotate --code-id --new-code-id`
call the existing Runtime bridge. Rotation input comes from the environment or
--secret-stdin, never --new-secret; public output is metadata only. MCP exposes
matching `remote.device_code_revoke` and `remote.device_code_rotate` tools and
reads `XALLOR_REMOTE_CODE_SECRET` only from environment. Focused CLI/Relay/accesscode/
storage/noderun and MCP tests passed, vet/build passed; independent signed-process
acceptance remains open.

Settings authorization correction (2026-11-18): inbound off/on on the same
target connection previously revived retained and pending consent. Settings and
existing session epoch now update atomically; effective changes revoke consent,
re-enable uses a new epoch, and no-op/owned unused-grant synchronization preserves
consent. The controller stays in logical-session mode and can seek fresh approval
on the same connection; no refusal/failure restores grant fallback. Final invoke
admission rechecks the pre-verification client snapshot and current session.
Controlled execution-stream tests preserve stdout/terminal and transport capacity
until settlement; they do not prove OS cleanup. See [settings evidence](../specs/2026-11-18-session-settings-epoch.md).
Focused tests, vet/build and independent review passed. Native Linux 84 top-level
tests passed with source/hash and container cleanup checks in
`non-gui-release/session-settings-linux-20261118-090355/report.json`; Windows
`go test -p 2 ./...` exited 0 (CLI 514.335s, unaffected packages cached), logged in
`dist/verification/session-settings-full-go-20261118.log`. Typed code revoke/rotation and durable unknown
recovery remain open.

Recipient connection lifecycle correction (2026-11-18): real SQLite regressions
proved that retained consent survived target disconnect/replacement and a fresh
Hub after controller disconnect. Schema 12 and target epochs now bind recipient
consent through reserve/finalize/resume and final routing admission, including
conditional retirement of old epochs and pre-rotation routing stop. Approval
publication matches the original pending/client/target state; late publications
cannot overwrite a new pending object. See [design and evidence](../specs/2026-11-18-session-target-epoch.md).
Focused storage/Relay/Runtime tests and independent read-only review passed;
Linux 69 top-level tests passed with source/hash and container-removal evidence
in `non-gui-release/session-target-linux-20261118-082731/report.json`.
Windows vet/build and `go test -p 2 ./...` passed (CLI 518.253s, unaffected packages
cached); log `dist/verification/session-target-full-go-20261118.log`. A failed Windows test's
temporary directory remains because execution policy rejected its deletion;
the exact path and boundary are recorded in the design. No production migration,
release or commit occurred. Inbound off/on generation, unknown recovery and
multi-active Relay fencing are not claimed complete by this change.

Runtime session correlation correction (2026-11-18): failing tests reproduced
same-session waiter overwrite, stale-request result delivery, duplicate terminal
delivery and lost unknown outcomes on transport failure. Both session issuance
and resume now share a request-ID registry that also matches session ID and
result type, retains one terminal, ignores progress, rejects duplicate/late
registrations and makes unsubscribe conditional on the original waiter. Failed
send attempts return session_unknown, except the local pre-write too_large gate.
Focused Windows tests include real IPC/WebSocket peer-simulator replies in reverse
order and injected write errors only after the peer decoded the entire request.
These are transport/correlation evidence, not proof of database commit or durable
unknown settlement. Native Linux ran 17 top-level session/client-link/transport
tests successfully; report `non-gui-release/session-replies-linux-20261118-074832/report.json`
confirms unchanged source hashes, matching uploaded binaries, owned container
absence and no cleanup errors. Independent read-only review found no actionable
P0-P2 issue within this change; it did not rerun tests. Windows focused tests,
vet and CLI build passed. Final Windows `go test -p 2 ./...` exited 0
(CLI 514.175s, noderun 30.606s; unaffected packages cached), with full log at
`dist/verification/session-replies-full-go-20261118.log`.

Verifier-settlement test correction (2026-11-18): a controlled six-second
descendant-readiness delay reproduced the fixture's premature five-second root
exit. The fixture now waits for the test release handshake; the 15-second overall,
10-second readiness and three-second settlement limits remain unchanged. Tests
require a live descendant before release, confirmed cleanup and joining verifier
completion before temporary-directory cleanup. Windows focused tests/vet and
the native Linux releasetrust package (15 top-level cases) passed. Linux report:
`non-gui-release/proof-settlement-linux-20261118-073758/report.json`, source hashes
unchanged during validation, owned container absent, no cleanup errors. This
does not establish the exact cause of the earlier intermittent failure and does
not change the pending clean signed-candidate requirement.
The final Windows `go test -p 2 ./...` exited 0 (releasetrust 17.280s; unaffected
packages cached), recorded in `dist/verification/proof-settlement-full-go-20261118.log`.

Resume publication correction (2026-11-18): confirmed commits could previously
publish active after client/target loss, target replacement or inbound-off, and
queue refusal could leave a bound session. Publication now orders route/context/
snapshot/deadline checks, nonblocking result enqueue and binding under one Hub
mutex. Eight regression cases cover these windows, cancelled connection contexts
and a real SQL-trigger retirement failure. Failure withholds the rotated secret,
preserves old consumption, attempts bounded revocation and reports unknown.
Retirement failure remains unconfirmed; queued does not mean delivered. No
unknown-commit or end-to-end delivery guarantee follows from these tests.

Native Linux publication validation:
`non-gui-release/session-consent-linux-20261118-071330/report.json` records 46
passing top-level tests, including all eight publication subcases. Source and
uploaded binary hashes match; the owned offline container is absent and cleanup
errors are empty. An independent read-only source review found no actionable
issues within this publication change; it did not rerun tests or validate
delivery/unknown-commit behavior.

Windows final publication validation passed `go test -p 2 ./...` (unaffected
packages cached; CLI 511.430s), relevant vet and CLI build. Full output is in
`dist/verification/resume-publication-final-go-suite.log`. Prior intermittent
expiry-test evidence remains retained separately and is not explained away.

Resume dynamic-authority correction (2026-11-18): eight stale account/share/
inbound cases reproduced successful resume under the old connection snapshot.
The consumption transaction now loads live authority and checks its canonical
binding; credential and share deadlines are checked again before consumption.
Eleven regression scenarios cover those eight cases, revocation committed after
the read via a second SQLite connection, and credential/share expiry between
read and consume. Positive identity and scoped Runtime reconnect tests remain
in the validation set. Post-commit publication and unknown-commit handling still
require acceptance; this does not certify the complete lifecycle.

Native Linux validation for dynamic authority is recorded in
`non-gui-release/session-consent-linux-20261118-065615/report.json`: 45 top-level
tests passed across five packages, including all eleven authority subcases.
Uploaded binary digests match; source-before/after agree; the owned offline
container is absent and cleanup errors are empty. This remains component-level
native evidence, not independent signed Runtime processes.

Windows final validation for this authority change passed `go test -p 2 ./...`
(unaffected packages cached; CLI rerun 504.184s), relevant vet and CLI build.
Log: `dist/verification/resume-authority-final-go-suite.log`. This success does
not resolve the earlier separately recorded intermittent expiry-test failure.

Recipient scope correction (2026-11-18): a negative fixture reproduced approval
of `info` creating persisted scope `[info,exec]`; wrong generation and out-of-code
scope also reached pending approval. Reservation now checks code binding,
generation and scope together in its transaction. Finalization requires an
explicit approved scope subset and the active response uses the persisted
authorization facts. Tests cover persistence, resume retaining the narrowed
scope, and a denied file write through Runtime handlers/live WebSockets with no
workspace side effect. This remains same-process integration evidence.

Schema 11 invalidates older WIP active sessions/resume proofs because their
original consent scope cannot be reconstructed, without refunding consumed
codes. Migration/reopen/failure rollback tests pass. Unknown outcomes and
concurrent authorization changes remain separate acceptance work.

Native Linux evidence: `non-gui-release/session-consent-linux-20261118-063629/report.json`
records 44 top-level passing tests across accesscode (12), protocol (4), storage
(12), relay (12) and noderun (4), using freshly cross-built binaries executed
on Linux x86_64. Uploaded hashes match, Go source digest is unchanged before/
after, and `container_absent=true`, `cleanup_errors=[]`. The container used
1 CPU, 1 GiB, 128 PID limit, read-only root, no host binds, no network and all
capabilities dropped. The report explicitly sets cryptographic acceptance and
independent Runtime processes to false. This supersedes the earlier assertion
that Linux access required new credentials; it does not close all Linux or
signed-candidate acceptance.

Windows validation for the consent fix: targeted accesscode/storage/relay/
noderun tests, vet and CLI build passed. The first full run failed the existing
`TestPrincipalWire_should_denyExpiredCaller_whenDatabaseWaitCrossesDeadline/hello`
case (unexpected `hello_ok`); log retained as
`dist/verification/session-consent-final-go-suite.log`. Three isolated repeats
passed; after adding an assertion that the fixture has reached wall-clock
expiry before releasing the pool, five further repeats passed. No production
authentication change or timeout relaxation was made. Full `go test -p 2 ./...`
recheck passed (unchanged packages cached; storage rerun) in
`dist/verification/session-consent-go-suite-recheck.log`. The original intermittent
expiry failure remains unexplained and must be investigated before claiming
release acceptance; the successful recheck does not erase it.

Resume identity correction (2026-11-18): real SQLite account/access fixtures
reproduced a different principal resuming the original caller's session under
the same ownership or grant. The resume digest now binds the full authenticated
identity and authorization snapshot, including tenant/membership version,
credential identity, device incarnation and share/permissions/expiry. Other
principals, replacement credentials and account-to-grant downgrade are refused;
the unchanged identity remains usable. Explicit session invokes after refused
resume are also denied rather than routed through ordinary transport rights.
Live revalidation and concurrent
revoke/rebind acceptance are still open.

Final validation of both identity binding and explicit-session refusal passed:
Windows `go test -p 2 ./...` (CLI 515.286s, unaffected packages reused Go test
cache), relay/noderun vet and CLI build. The final full-suite log is
`dist/verification/resume-identity-final-go-suite.log`; the earlier full-suite
run remains separately recorded in `resume-identity-go-suite.log`.

Session security correction (2026-11-18): the first resume handler did not
authenticate the supplied capability against its stored hash. Wrong-secret
and stored-hash-as-bearer tests reproduced unauthorized session rebinding.
`ConsumeResume` now requires a caller-derived SHA-256 and compares it before
mutation in the same transaction. Relay rejection and Runtime/WebSocket
negative cases pass; earlier positive tests were insufficient to prove this.
Remaining session work includes principal isolation, requested versus persisted
scope/generation, target/session binding, concurrent revoke/rebind and lost
commit response handling. Linux execution and a clean candidate are not the
only remaining gates.

Evidence clarification: `session_runtime_wire_test.go` creates Daemon objects
inside a single test process and calls IPC handlers over `net.Pipe`; it does not
launch two independent Runtime processes or validate production OS IPC.

Validation for this correction: Windows `go test -p 2 -count=1 ./...` passed
(CLI package 517.314s); focused accesscode/storage/relay/noderun vet and CLI
build also passed. The full test output is retained locally at
`dist/verification/non-gui-go-suite-latest.log`. These results do not close
the remaining session security acceptance items above.

Latest session-lifecycle checkpoint (2026-11-18):

- The internal WIP path now spans access-code state transitions, SQLite
  reservations/session authorization, Relay approval routing, Runtime approval
  IPC, CLI/MCP Runtime requests and real TLS Relay wire acceptance.
- Disconnect, expiry, ownership invalidation and unknown-result paths are
  conservative: they revoke/end/retain uncertainty and never silently replay
  or refund a code.
- This is still controlled WIP evidence. The public CLI/MCP entry is marked
  experimental, the stable reconnect policy is intentionally deferred, and no
  clean signed candidate or production authorization-code release exists.

### Current governance status

- `protected-release-tags` is active for `refs/tags/v*` and restricts creation,
  update, deletion and non-fast-forward changes; current API observation reports
  no bypass for the active account.
- GitHub `release` environment exists with `v*` deployment policy, one required
  reviewer (`xallor-agent`) and administrator bypass disabled.
- The workflow now requires `github.ref_protected` for both the bundles job and
  draft job. Manual branch runs cannot obtain attestation or create a draft.
- A clean protected-tag run with the new workflow has not yet been executed.

Latest verification on 2026-11-17 (current dirty WIP checkout):

- The existing draft `v0.2.6` was downloaded and verified online with the
  repository, tag, commit, exact certificate identity, signer digest, SLSA
  predicate and hosted-runner policy. The same release index was verified
  offline with its Sigstore bundle and a freshly obtained trusted root; zip,
  tgz, raw manifest and vendor hashes also passed local inspection.
- The current checkout's standalone source-gate and missing-verifier install
  refusals passed. These are negative gates only.
- The current MCP signed-vendor harness passed against the draft's authentic
  Windows bundle, including bootstrap snapshot identity and foreign-manifest,
  altered-manifest, unsigned-byte and rehashed-byte refusals.
- The draft's native signed upgrade/rollback could not run against this WIP
  Runtime: the old `v0.2.6` candidate reports an owner protocol that the
  current Runtime correctly rejects. This is evidence that a new clean
  protected-tag candidate is required; it is not a lifecycle pass.
- Current owner-mechanics regressions also pass: foreground authorization
  survives neither inbound off/on nor credential/member/share replacement,
  stops owned trees before capacity release, and does not replay after Relay
  reconnect; the restricted-PATH shell matrix reaches the explicit missing
  `pwsh` terminal without fallback. These are controlled mechanics evidence,
  not signed-release or PS5 control-host acceptance.
- During this verification, `scripts/verify_release.py` was fixed to avoid the
  GitHub CLI-invalid combination of `--signer-workflow` and `--cert-identity`.
  Regression tests cover the exact certificate-identity policy.

Verification update 2026-11-18:

- After repository configuration was supplied, GitHub API readback confirmed
  the active `protected-release-tags` ruleset and the `release` environment
  reviewer/branch policy. No tag, release, or production deployment was
  created by this verification.
- Existing draft `v0.2.6` (`8345804ca0afc218ac01f850df9a272c3899f49f`, workflow
  `42454269210`) passed the online indexed-artifact verifier and a second
  bundle-based offline attestation pass for all eight indexed artifacts.
- The current checkout passed the focused Go owner/releasetrust suites,
  MCP (74 passed, 2 platform skips), disposable-prefix npm installation, and
  the five-case signed-vendor matrix. These are current-checkout/draft evidence
  only; they do not create a clean protected-tag candidate.
- Historical PR check `42454200555` had successful Go/MCP jobs but both
  release-lifecycle jobs failed at the then-current `packaged_install.py`
  because `env` was undefined. The current worktree includes that fix and the
  real v0.2.6 tgz case passes locally; a new remote run is still required.
- Session authorization follow-through is now wired: Runtime `exec`,
  `read`, `write`, `processes` and `info` IPC requests accept `session_id`,
  the client envelope carries it into Relay `invoke`, CLI exposes
  `--session-id`, and MCP `remote.exec`, `remote.read`, `remote.write`,
  `remote.processes` and `remote.device_info` expose the same field. Focused
  Go and MCP suites pass after this change.
- A native cross-daemon acceptance test now passes: target and client
  `noderun.Daemon` instances connect to a live Relay WebSocket server, the
  target approval head accepts the request, and the client completes a scoped
  `info` invoke using the returned session ID. This is controlled evidence,
  not a clean signed-candidate or reconnect proof.
- The same cross-daemon test now also sends the consumed pre-rotation
  capability through a real Runtime IPC after reconnect; Relay rejects it.
- `session.result` now returns the approved generation, scope and expiry to
  both Runtime sides and the local `session.request` IPC result; clients can
  audit the exact authorization fact they received instead of reconstructing
  it from their request.

1. Retain the GitHub governance above and verify the reviewer identity/ownership
   before the first stable publication.
2. Produce two clean owner-compatible protected-tag candidates and preserve immutable run IDs, candidate
   manifest, all artifact digests, provenance bundles and reviewer decision.
3. Implement signed update metadata (TUF root/targets/snapshot/timestamp or an
   equivalent signed metadata contract) with monotonic version, expiry,
   revocation and rollback/freeze protection.
4. **Verified for the existing draft only:** online/offline provenance and
   raw Runtime, manifest, zip/tgz and npm vendor integrity passed for v0.2.6;
   rerun this exact matrix for the first clean protected-tag candidate.
5. Go owner/releasetrust and MCP snapshot-binding suites pass on this checkout;
   signed-vendor negative/positive wrapper checks pass against v0.2.6. The
   Windows/Linux signed install → upgrade → rollback → startup-failure chain
   remains open until two clean candidates built with the current owner protocol
   are produced and verified.
6. Wire the signed metadata primitive into the release/installer policy only
   after immutable root custody, offline root import and revocation publication
   are defined; do not treat unit metadata tests as production update trust.

**External dependency:** repository administrator access is required for
rulesets/environment settings and protected-tag configuration. No production
Relay deployment is part of this block.

### Signed lifecycle closeout sequence — 2026-11-19

- Completed: input/setup failures cannot leave old signed-upgrade success
  reports. The report starts before candidate input reads; created workspace
  identity is recorded before marker creation, unstarted setup can clean up,
  and any attempted command requires explicit lifecycle cleanup. Five regression
  tests pass and run in PR/release. No cryptographic acceptance follows from them.
- Completed locally: a read-only Windows process/image observer uses a retained process
  handle and kernel image-file mapping, not merely a reported path/hash. The signed
  script holds old/current process references across transitions and requires exit
  before success, releasing handles before root deletion. Native IPC ready peer,
  activation and launch are bound via the separately built runtime-observer helper;
  the missing-manifest negative phase only reuses a prior bound reference. Actual
  independent Runtime mechanics pass on Windows/Linux. Existing ProcessRef.close
  is not used because it actively kills tasks. Full Go95868 passed (37 packages,
  36 cached). See runtime-observer-summary-20261119.md; real signed cycle is pending.
- Completed locally: Linux startup fault is adapted to the real lifecycle worker. Correlate the
  unique operation notice, canonical intent and profile namespace; use lifecycle
  IPC peer credentials and the authenticated baseline worker image/entry to bind
  the candidate's actual parent. Keep candidate pidfd, pending activation and
  real database-lock failure checks. Do not invent a historical owner→worker
  PPID observation after reparenting; label which parent relation was observed.
- Recovery proof: query that same operation with settled=true/outcome=failed;
  require requested B start quiescent_failure, confirmed A restore start/commit/
  ready, unchanged identity/config/sentinel and actual restored A process/image.
  Do not match the obsolete `previous state restored` stderr phrase. Missing
  or uncertain operation/cleanup evidence must remain a failed acceptance.
  The controlled native end-to-end passes with a real DB lock, B/worker exits,
  restored A identity/config/files and complete profile/container cleanup:
  recovery-observer-linux-20261119-040220/report.json. It uses a native fixture
  and deliberately different B digest, not real signatures. Full Go40654 passed
  (37 packages,36 cached). See recovery-observer-summary-20261119.md.
  Windows fault observation is now implemented and passes actual controlled
  recovery, with Toolhelp discovery followed by handle-bound image/parent/argv.
  Shared candidate selection uses actual lifecycle observations, not intent or
  live state-file polling. Final Windows/Linux reports and retained initial
  Windows failure boundaries are in recovery-cross-platform-summary-20261119.md.
  Genuine signed-candidate failure recovery is still separate from this fixture.
- Completed locally: independent `signed-lifecycle.yml` wires native Windows/Linux
  A/B lifecycle acceptance, with explicit protected-B context, source verification
  before execution and complete native/effect evidence. Actionlint1.7.12 passes
  for all three touched workflows; 40 local policy/evidence/report/docs tests pass.
  The first aggregate test driver lacked the repository on sys.path; its failed
  import log is retained separately. Corrected driver has zero failures/errors/skips.
  Real protected-tag dispatch is still pending compatible signed candidates.
  First A may remain a baseline-only draft, as already
  authorized; a draft is not promotion. B promotion requires full lifecycle
  results and human approval, with immutable tag/commit/provenance references.
- External gates stay explicit: AGENTS forbids commits without explicit commit
  instruction. Default caller cannot bypass tags, but the existing xallor-agent
  account was independently verified to have repository admin and ruleset bypass;
  publisher credentials/configuration are therefore available. No rule was changed.
  Its current bypass also covers update/delete; do not claim publisher immutability.
  Human publication approval and genuine candidate acceptance remain independent.

## S1 — single-region operability

6. Add a separately authenticated management boundary (initially it may be a
   distinct listener/process in one deployment) using OIDC Authorization Code +
   PKCE, principal/membership mapping, device/share RBAC and structured audit.
7. Keep the data plane Runtime↔Relay only; management credentials never enter
   invoke payloads. Add a new profile-owned quarantine namespace and explicit
   operator reconciliation; retain historical objects without durable ownership.
8. Keep `native_user` as the only first-release execution backend. Do not label
   Job Object/process-group cleanup as sandboxing.

## S2 — commercial operation (deferred)

9. Versioned entitlement snapshot/PDP and atomic commercial lease admission.
10. Durable usage outbox/aggregator, separate append-only/WORM audit, retention,
    export/delete policy and explicit unknown billing treatment.

## S3/S4 — enterprise and high-risk execution (deferred)

11. Durable ownership epochs, fencing, replication and single-region failover
    drills before multi-region HA; post-dispatch work stays unknown.
12. Per-OS least-privilege backend (restricted Windows token/AppContainer and a
    Linux cgroup/Landlock/seccomp/container design) with independent cleanup,
    workspace, network and resource acceptance.

## Explicitly not implemented

- Automatic historical quarantine deletion without durable ownership proof.
- Automatic replay or force-release of uncertain jobs.
- Production issuance and complete acceptance of one-time/expiring/long-lived
  access codes; current protocol/schema remain WIP. Design is in
  `docs/superpowers/specs/2026-11-17-access-code-lifecycle.md`.
- Public management API, real entitlement provider, billing, cross-Relay HA,
  SCIM, or production OS sandbox.
- GUI features, interactive terminal UI, file browser/transfer UI.

## Evidence rules

- L1: static contracts and docs.
- L2: unit/integration tests against the current checkout.
- L3: native Windows/Linux or isolated external-host runs with fixed hashes.
- L4: clean protected-tag release, independent reviewer, and production-like
  lifecycle evidence.

L1/L2 never upgrade a claim to L3/L4. Dirty worktree, draft release, controlled
provenance fixture and historical report must be labeled as such.

Post-hardening full-suite evidence (2026-11-18): Windows `go test -p 2 ./...` exited 0, including the three earlier Runtime failures; CLI 114.055s and unaffected packages were cached. Log: `dist/verification/fixture-hardening-full-go-rerun-20261118.log`. Earlier failures remain historical evidence. No `version --json` orphan probes remain after owned-tree cleanup.

Final verification checkpoint (2026-11-18): after fixture-hardening review, Windows `go test -p 2 ./...` exited 0 (`dist/verification/non-gui-final-full-go-20261118.log`); affected CLI 114.055s, other packages passed/cached. `npm test --prefix packages/mcp` exited 0 (74 passed, 2 platform skips). `version --json` orphan probe count is 0. Independent Runtime service-process/IPC acceptance remains explicitly open; current session tests use two Daemon objects in one process with live TLS/net.Pipe.
