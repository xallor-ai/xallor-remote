# Recipient epoch implementation plan

Implement [the fixed design](../specs/2026-11-18-session-target-epoch.md).
No GUI, production mutation or Git commit. Runtime remains the common head
authority; Relay/SQLite own recipient session authorization.

## Modules and steps

1. **Domain and SQLite authority** — add `internal/accesscode/target.go`,
   extend reservation/session target binding and Store contracts. Add
   `internal/storage/sqlstore_session_target.go` and its schema migration.
   Keep current epoch rotation, conditional retirement and scoped revocation in
   this module. Adapt ReserveCode/FinalizeReservation/ConsumeResume to require
   matching epoch in the transaction. Add storage tests for stale callbacks,
   delayed retirement, rollback, legacy unknown records and migration.
2. **Relay retirement and registration** — add
   `internal/relay/session_target.go` to own routing-stop capture and deferred
   bounded retirement; wire RegisterDevice, Drop, enqueue refusal, identity
   failure and deletion. Authenticate before stopping old routing. Preserve the
   expected target binding when async retirement runs. Existing device and
   authorization tests must retain their original meaning.
3. **Approval and routing admission** — bind reservation to captured epoch,
   add `internal/relay/session_publish.go` for approval publication with pending
   identity checks, augment resume and final invoke admission. Reproduce races
   using transaction barriers, not sleeps. Never compensate by restoring a code.
4. **Validation and delivery record** — first make the existing three red
   SQLite tests pass, then cover every acceptance item in the design. Run
   relevant Go tests/vet, CLI build, `go test -p 2 ./...`, isolated native Linux
   and independent read-only review. Update the canonical protocol/IPC boundary
   only where changed, and record exact evidence in the non-GUI TODO and #105.

Current state: implementation and focused regression tests complete; independent
review findings addressed and rechecked. Windows full suite, vet/build and native
Linux matrix passed; evidence boundaries and retained failed-test residue remain
explicit in the design. Red log: `dist/verification/session-target-lifecycle-red-20261118.log`;
review regression red: `dist/verification/session-target-review-red-20261118.log`.
Detailed coverage/limits are recorded in the linked design.
