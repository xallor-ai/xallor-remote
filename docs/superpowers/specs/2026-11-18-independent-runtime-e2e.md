# Independent Runtime process acceptance (non-GUI)

Status: independent-process owner/OS IPC and session restart/revoke slice
implemented and exercised on Windows and Linux. This does not replace the
separate signing, TLS, public CLI/MCP or task-tree acceptance requirements.

## Scope

The black-box tests are under `internal/cli`, because the production owner start,
permit, profile selection and IPC path are owned there. Start two independent
`runtime-serve` processes from two isolated profiles and exercise:

1. owner permit and Runtime readiness over each profile's real named pipe/socket;
2. target/device and controller/client WebSocket connections to one test Relay;
3. `session.request` → target `session.approval.subscribe/respond` → scoped `info`;
4. client process stop and restart with the same profile;
5. `session.resume` with the saved capability, including wrong-capability refusal;
6. owner `device.access` revoke/rotate through the target Runtime IPC, followed by
   denied renewal/terminal cleanup where the test can observe it.

The test Relay uses an isolated `httptest` `ws://` listener. It does not touch the
official Relay, production data, existing Runtime, business containers or GUI.

## Process and profile boundary

Each child receives its own absolute `XALLOR_REMOTE_DATA_DIR`, Relay URL and
`XR_TEST_NATIVE_OWNER_FLOW=1` only for the test-binary dispatch. Parent selection
changes are serialized around start/stop/dial; existing children retain their
private inherited environment copies. No goroutine changes the parent's
environment. Each profile needs an installed baseline Runtime state and an
identity/config/peer set. IPC endpoints are derived from the profile path, so
the two named pipes/sockets remain distinct.

The child process is started with the same launch permit and owner protocol as
the production `runtimeControl.Start` path. Parent observes readiness and PID
through IPC plus an OS exit watch. IPC disconnect is not treated as process exit.

## Evidence rules

- Every approval and invoke goes through child IPC. No direct access to a child
  `Daemon` or in-process approval hub is allowed.
- Reconnect means stopping the client Runtime process, observing its exit, then
  starting the same profile again and consuming the saved resume capability.
  Dropping a `net.Pipe` alone is insufficient.
- Revocation success is a management acknowledgement. Process/job/transport
  cleanup is asserted only after an actual target terminal and OS observation.
- Unknown, timeout or cleanup failure is retained as failure/unknown; the test
  never kills an unowned PID or retries a side-effecting management request.
- Cleanup order is client, target, child exit watches, IPC endpoint absence,
  then Relay/store. A failed cleanup keeps evidence and fails the test.

## Acceptance artifact

The test logs `INDEPENDENT_RUNTIME_REPORT` JSON and optionally writes it to an
explicit absolute `XR_INDEPENDENT_REPORT_DIR` (normally `dist/verification`). It
records profile hashes, child PIDs/image/activation/launch identity, case names,
startup confirmation, exit-watch/Wait/endpoint-absence evidence and cleanup
errors. No capabilities or raw credentials are reported. Source provenance is
explicitly a controlled native gh fixture and the Relay is loopback WS; no
signed release or TLS acceptance is asserted. Task execution terminals are not
included in the current info-only slice.

## Implementation order

First add a two-profile owner/IPC readiness and clean-stop test. Then add target
and client Runtime WebSocket setup, approval/session, and finally restart/resume
and owner code management. Run focused tests after each slice, then the full Go
suite and the existing Linux container runner. Do not modify GUI or turn the
same-process noderun tests into a weaker substitute.

## Current evidence and remaining scope

`independent_runtime_fixture_test.go` owns installation, startup, profile selection,
kernel IPC peer validation and reverse-order stop/Wait/endpoint checks. Cleanup
is registered before launch; partial or unknown startup is retained rather than
promoted to success merely because IPC is absent. The negative cleanup test uses
a real empty profile with a constructed unknown obligation, not a real injected
startup timeout. The exact test-created short temporary root is removed only
after confirmed cleanup; reports state whether removal succeeded.

`independent_runtime_e2e_test.go` currently proves:

- two simultaneous runtime-serve PIDs and separate profile IPC endpoints;
- stopping one process leaves the other available;
- real owner claim, recipient approval and scoped remote info through OS IPC;
- actual controller stop/Wait and restart, with changed PID and launch ID;
- wrong capability refusal, same-consent resume with a fresh active durable proof,
  consumed proof reuse refusal and unapproved write denial with no target file;
- owner code revoke via target IPC, denial of old-session invoke and account
  fallback, plus durable session/resume revocation and revoked resume refusal.

Windows focused: `dist/verification/independent-runtime-final-focused-20261118.log`
(9.218s). Independent read-only review found two harness P2 issues (partial-start
cleanup and missing fresh-proof assertions); both were corrected and rechecked.
Linux: `non-gui-release/independent-runtime-linux-20261118-184332/report.json`,
3 top-level tests passed, no skips, equal source hashes, container absent and
cleanup_errors empty. Both embedded process reports confirm all five launches
exited, were reaped and lost their IPC endpoints; both profile roots were removed.
Windows full `go test -p 2 ./...` exited 0 (CLI 117.669s, unaffected packages
cached), log `dist/verification/independent-runtime-full-go-20261118.log`.
CLI vet and six documentation checks also passed.

The additional `independent_exec_test.go` cases now exercise owner revoke and
rotation during a real running exec in the independent target Runtime. The
test-only dispatcher starts root/leaf helpers lasting sixty seconds; the IPC
observation deadline is fifteen seconds. Both tasks are observed alive and
bound to their actual image digest with retained kernel handles before mutation.
The management request uses target IPC, and the real execution terminal must be
`authorization_lost` with an actual exit code, both tasks exited, an empty durable
job file and zero Relay transport occupancy. Both Runtime owners remain live;
the same execution IPC can subsequently return a correlated status response.
No test directly kills a task or injects a terminal. The report includes task
image/live/exit facts; cleanup checks tasks again after stopping the owners.

Windows focused exec cases passed in 7.830s; independent read-only review found
no new P0-P2. The first Linux attempt failed in cross-compilation after the
runner's 180-second limit (`TimeoutExpired`), before any container/test started;
report `non-gui-release/independent-exec-linux-20261118-202621/report.json`.
Do not reuse the preceding info-only Linux pass for these new exec cases.
The same Linux runner subsequently passed all five top-level cases without
changing build/test deadlines, after concurrent compilation ended. Report:
`non-gui-release/independent-exec-linux-20261118-204351/report.json`; identical
source hashes, no skips, nine owner launches exited/reaped/endpoints absent,
four task processes exited, four profile roots removed, container absent and
cleanup_errors empty. This is fresh exec evidence, not reuse of the info-only run.
CLI vet and the original full Go session 12208 exited 0 (CLI 387.140s), recorded
in `dist/verification/independent-exec-full-go-20261118.log`. The subsequent
handoff consent-expiry correction has a separate full-suite verification.
Occupancy is sampled while running and after the terminal, not continuously:
this test alone cannot prove no intermediate early release. The next status
response excludes an already queued duplicate terminal, not a duplicate arriving
at any arbitrarily later time. Existing Relay route-settlement tests stay separate.

Separate `tests/handoff_e2e.py --owner-mechanics` acceptance now passes on
Windows and Linux with independent CLI/Relay/Runtime processes under shared
grant authority: six receipt/dedupe/preflight/deadline/normal-restart groups,
controlled Agent/verifier, loopback WS, test-binary command dispatcher. See
`dist/verification/independent-handoff-summary-20261118.md` for exact scope.
Additionally, `independent_handoff_test.go` passes owner revocation of a logical
session's running handoff on Windows/Linux. Real OS IPC approves and queries
running before target-owner revoke; image-bound Agent exit, durable unknown /
execution_authorization_lost, job release and denied old receipt access are
observed. Reports and initial test-fixture failures are documented in
`dist/verification/independent-consent-handoff-summary-20261118.md`.
The same harness also passes rotation and real-clock expiry on Windows/Linux:
preparation precedes approval TTL, running is observed before the approved deadline,
and no direct database expiry mutation or task kill substitutes for lease expiry.
See `dist/verification/consent-matrix-head-ready-summary-20261118.md`.
The current handoff harness uses a third same-owner observer Runtime solely for
IPC receipt queries. This avoids concurrent file readers interfering with Windows
atomic replacement; original controller denial remains mandatory. Persistent
receipt audit happens after target stop/Wait, before root cleanup. Windows
expiry/public-CLI and Linux all four cases pass; see
`dist/verification/independent-observer-ipc-summary-20261118.md`. Earlier two-owner
handoff reports are historical and do not describe this observation mechanism.
The additional descendant case now passes on Windows and Linux: the controlled
Agent starts an ordinary inherited-group child and waits for its readiness before
publishing init. Both processes are independently bound to their image digests and
observed live before real owner IPC revocation. After the terminal is observed,
both kernel exit checks must pass before the test samples zero job/capacity usage;
cleanup checks them again after all three owners stop. No test-side task kill
produces a pass. See `dist/verification/handoff-descendant-summary-20261119.md`.
The fixture reports its own PPID and root checks the launched child PID; this is
not an independent kernel ancestry query. These results cover an ordinary child,
not detached descendants, official Agent behavior or continuous occupancy order.
Actual published CLI/MCP process entrypoints and clean signed-candidate
lifecycle/TLS acceptance remain separate.
Initial code issuance remains seeded through the test SQL store; it is not a
product issuance endpoint. Relay itself runs in the parent test process.

## MCP stdio acceptance extension — 2026-11-19

The consent fixture now exposes setup before any request, allowing the current
built MCP entry to initiate `remote.session_request` through real stdio JSON-RPC.
Two owned Node processes connect to the controller and target respectively;
recipient approval remains real target IPC. Tests check approved scope/expiry,
target info and UTF-8 read, native exec result, out-of-scope write refusal with no
file, owner MCP revoke, durable revocation, old-session and account-fallback denial,
and unchanged live Runtime PID/image identity. No tool callbacks are invoked
directly by the harness.

The Node-dependent case is explicit opt-in; `tests/mcp_consent_e2e.py` builds MCP,
supplies absolute Node/entry paths and rejects skips, partial scenario reports or
incomplete cleanup. Windows and owned-container Linux targeted runs pass; full
Go regression is recorded separately. Node heads are deliberately stopped during
teardown, before Runtime cleanup. A reaped Linux signal terminal counts as exit;
`process.ErrCleanup` still fails. These are controlled-source, loopback behavior
checks, not published MCP package or signed-install trust evidence. See
`dist/verification/independent-mcp-consent-summary-20261119.md`.

The expanded four-case matrix additionally replaces the actual controller
Runtime after confirmed stop/reap/endpoint absence. The existing MCP head admits
the replacement owner; an explicit old session is unauthorized on its fresh
unbound Relay connection. A correct one-use resume proof restores the same
scope/expiry and produces a new durable proof; wrong, consumed and revoked proofs
are refused through actual MCP calls. No unapproved write file is created.

Two running-task cases require actual stdio progress with the request's token,
then independently bind both native root/leaf images and observe both alive.
Owner MCP revoke or rotate must yield a correlated authorization_lost terminal
that retains prior stdout, both task exits and zero job/transport occupancy.
Task teardown never substitutes for these assertions; Node-head teardown is later.
The Runtime-reported duration is bounded, but not an independent latency metric.
Parent metadata is fixture-reported and occupancy is sampled, not continuously
observed. See `dist/verification/mcp-matrix-summary-20261119.md` for current results.

The first draft misread approval ID (it is Frame.ID) and failed without an early
cleanup registration, leaving two test Runtime processes. Both were subsequently
stopped through their own profile IPC and independently confirmed absent. The
fixed harness retained failed reports and proved cleanup on its later assertion
failure. A separate initial package-local report remains because its deletion
was rejected by execution policy; it is not current evidence.
