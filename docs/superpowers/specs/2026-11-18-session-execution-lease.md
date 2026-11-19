# Foreground execution session leases

Status: implemented WIP; targeted tests, native Linux, vet/build and independent
review passed. Full Windows verification is running. No GUI or public management API.

## Failure and behavior

Before-fix actual SQLite/Hub tests renewed a full 20 seconds after session end,
session/code revoke, session expiry and exec-scope removal. A session with only
three seconds remaining also received twenty seconds. Renewal previously checked
only transport/account/share authority.

`session_lease.go` now checks the original inflight route's immutable consent:
session ID, code ID/generation, target epoch, scope and expiry. It reads after the
base-authority check so a delayed base success cannot mask an intervening revoke.
The controller's later adopted session cannot lend authority to an old task.
Final routing-lock checks retain the same route/target/transport identity and
clamp duration to the provider budget, session deadline and millisecond floor.
Provider budget begins before its call; this is deliberately conservative.

Missing/invalid/revoked/ended/expired consent produces a definite denial and
permanently marks that invocation unrenewable. Only the matching old consent is
detached from its controller, retaining sticky session mode and allowing a fresh
explicit approval. A newer adopted session is untouched. SQL uncertainty grants
no new lease but does not permanently poison the route; Runtime may only retry
within its last confirmed deadline, without grace.

Known expiry is replied/marked/detached BEFORE best-effort durable EndSession.
The write rechecks origin and current expiry outside the routing lock. Failure
does not claim durable end and cannot downgrade the known denial into a transient
error or strand the controller. Output, route and transport capacity remain until
the target's terminal or actual routing loss; DB revocation is not OS cleanup.

Natural access-code expiry remains independent from an approved session. Legacy
routes without a logical session retain their existing authority validation.

## Evidence

- Initial red log: `dist/verification/session-execution-lease-red-20261118.log`.
- Tests cover all invalid-consent cases, TTL cap, natural code expiry, stale base
  success, late publication expiry, transient SQL recovery, permanent denial,
  new session isolation, failed/timeout expiry persistence and same-link approval.
- Fixed-clock final-budget tests cover provider/session equality boundaries and
  sub-millisecond values; scheduling tests alone are not claimed to force every
  final-lock branch. The persistence-timeout P2 was separately reproduced in
  `session-lease-cleanup-red-20261118.log` and fixed before validation.
- `TestSessionExecutionRuntime_shouldStopRealProcessTree_afterCodeRevocation`
  uses real TLS WebSockets/SQLite/IPC handlers and two Runtime objects in one Go
  process. A real native root and descendant report readiness; kernel handles
  prove both live before RevokeCode, then prove both exited before the client
  receives authorization_lost. Job ledger and transport occupancy are zero.
  Their 60-second watchdog exceeds readiness plus revoke observation deadlines,
  so natural fixture exit cannot pass the test. Successful assertions precede
  cleanup. Windows initial run passed in 5.30s.
- Independent read-only review confirmed the corrected P2 and found no remaining
  blocker in scope; it did not execute tests.

Native Linux report:
`non-gui-release/session-lease-linux-20261118-113005/report.json`: 68 top-level
tests passed (Relay 47, Runtime 21); one subprocess-only helper skipped in the
ordinary test runner. The actual process-tree integration passed in 5.03s and
reported confirmed exits, authorization_lost and zero occupancy. Source/binary
hashes matched, owned offline container absent, cleanup_errors empty. Windows
focused tests, relevant vet and CLI build passed.

Final post-fix rerun after the cleanup-timeout correction:
`non-gui-release/session-lease-final-linux-20261118-114432/report.json` passed;
Relay 47 and noderun 21 top-level tests completed with matching source/binary
hashes, owned container absent and no cleanup errors. Windows focused package
rerun passed: accesscode 7.844s, storage 53.154s, relay 62.993s and noderun
46.774s. The broad Windows `go test -p 2 ./...` run was not a valid success:
it hit the existing CLI `TestNativeOwnerTransactionSurvivesServingProcessReplacement`
fixture's 10-minute package timeout while starting `nativeOwnerBundle`;
the relevant package logs remained green. This is recorded as a validation
limitation, not hidden by a rerun or timeout change.

## Remaining boundaries

This is renewable foreground `exec` enforcement, not the management endpoint.
A revoke after the last session read but before publication can still race that
read; any previously observed grant remains bounded by its lease, not refreshed
by waits. Do not claim instantaneous zero-window revocation until the per-code
mutation/publication fence is implemented. Recipient-authorized management and
Runtime/CLI custody remain required. Background handoff proof/session propagation
is separate. Component/native-process evidence is not two independent Runtime
service processes, signed-package acceptance or a production deployment.
