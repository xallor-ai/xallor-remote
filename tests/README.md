# Cross-layer verification

## Independent Runtime process slice

`go test -count=1 -v -run '^TestIndependentRuntime' ./internal/cli` runs two
installed runtime-serve processes with separate short temporary profiles. It
checks real OS IPC kernel peers, owner readiness, approval/scoped info, controller
process stop/restart/resume, unapproved write rejection and owner code revocation.
The `TestIndependentRuntimeExec` cases additionally run a native parent/child
task under the target Runtime, revoke/rotate via owner IPC, and observe actual
`authorization_lost`, kernel task exits and job/transport counts at the terminal.
They sample occupancy before/after rather than proving every intermediate moment.
The existing native owner/gh fixture controls source proof; no signed candidate,
TLS, public CLI/MCP process or handoff acceptance follows from this.

Reports appear as `INDEPENDENT_RUNTIME_REPORT` log JSON; setting an absolute
`XR_INDEPENDENT_REPORT_DIR` also writes per-test JSON after cleanup. Normal tests
do not write reports into package-local directories. Profile roots are removed
only after confirmed stop, kernel exit, Wait and endpoint absence. Unknown startup
keeps the profile and fails cleanup. See the independent-runtime spec for remaining
coverage and the distinction from same-process noderun tests.

## Owner-start migration status

The following suites accept explicit `--owner-mechanics --go <go-executable>`
or `--owner-mechanics-binary <trusted-precompiled-test-file>`:

- cross-layer and diagnostics;
- backend selection, principal access, device ownership, IPC bounds and device settings;
- foreground authorization and process lifetime;
- job lifecycle, execution input/results and JSONL streaming;
- client authentication and peer flag migration;
- shell adapter selection and missing-interpreter handling;
- resource boundaries and identity transactions;
- handoff state/dedupe, scope, revocation and continuous authorization.

For example: `python tests/backend_selection_e2e.py --owner-mechanics`.
Build mode compiles `go test -c ./internal/cli` in an owned temporary root. It uses
the test-only dispatcher as native Runtime and controlled external verifier, and
executes actual CLI/IPC/Relay/MCP behavior. Prebuilt mode is an explicit trusted
test input; it does not infer source provenance from self-reported version data.
Reports record `cryptographic_acceptance:false` and fixture/binary SHA. Production
executables contain no test dispatcher. Omit these flags to retain real source
verification against a supplied owner-capable authenticated Runtime.

The latter twenty suites use `fixtures/mechanics_cli.py`: a report is first marked
incomplete before fixture construction, pins the harness SHA, and becomes passed
only after nonempty passing scenarios and owned-cluster cleanup. A failed setup,
scenario or cleanup cannot leave the prior run's success marked current. Backend
configuration-repair testing explicitly recovers retained pending state rather
than silently replaying ensure. Corresponding PR steps select mechanics explicitly;
signing/product-artifact acceptance remains separate.

Client authentication runs a deliberately unconfirmed Relay and requires zero
invocations from CLI/doctor/MCP in shared and account modes. Peer flag testing
requires the removed flag to fail before creating a profile and the current flag
to reach the owner. Both use strict Cluster cleanup; authentication additionally
checks its retained native Runtime reference before any fallback termination.
For offline Linux execution, authentication accepts an explicit `--relay-fixture`
for the deliberately unconfirmed test server and records its byte hash.
Argument parsing alone
(`cli_argument_errors_e2e.py`) still tests the supplied production CLI directly;
it does not start a Runtime and needs no controlled source verifier.

Shell adapter's Node helper writes intermediate results only inside its owned
cluster root. The public report is published after both normal and missing-pwsh
branches, configuration/restart checks and cleanup. The restricted target PATH
retains only the source verifier and platform shell; mechanics environment
overrides are restricted too. Controller/Node keep their normal PATH. This
preserves source verification while actually exercising interpreter absence.

Resource-boundary testing accepts `--program-fixture` for an explicit trusted
precompiled execution-result helper and records its hash. Identity transaction
testing verifies an empty job ledger before abrupt restart and terminates through
a retained native process reference; it does not signal a later PID lookup.
The committed identity, cache, Relay access and corrupt-identity refusal checks
remain required before the completed report is published.

Handoff suites use `fixtures/handoff_cli.py` to build the repository's Go Cursor
contract stub, or accept an explicit `--agent-fixture`, recording its SHA. CI
builds one native owner test executable and explicitly reuses it for all four
suites. The base suite now publishes handoff.json and retains native process
references before checking timeout/stop completion. These are Agent protocol
and lifecycle checks; real Cursor vendor acceptance remains a separate gate.

Foreground authorization and process lifetime also accept `--tree-fixture` for
an explicitly supplied trusted process-tree test executable; process lifetime
accepts `--agent-fixture` for the Cursor test executable. Without these options,
the scripts build their helpers with `--go`. Helper byte hashes are included in
the report. These test helpers do not certify real agent or signed release behavior.

Job lifecycle deliberately retains uncertain occupancy after killing a Runtime.
Its negative contract has a separate `expected_refusal_verified` outcome: all six
scenario assertions must finish, the retained replacement Runtime reference must
confirm exit, every tracked test process must be reclaimed through its native
reference, and only the specific target job-release refusal is accepted. The
ledger must remain byte-identical and all its entries uncertain. Any additional
profile failure, missing stop ACK, live owner or changed ledger fails the test.
The owned profile remains as evidence; `cleanup_complete:false` and
`product_job_release_confirmed:false` must not be interpreted as normal cleanup
or production recovery. Ordinary Cluster cleanup remains strict. Production
evidence-based recovery is still tracked in #63. Input's CLI-literal-arguments
side report is reset before setup so a failed rerun cannot publish old success.

The current Runtime owner worktree requires authenticated activation for public
`start`/`ensure`. PR CI's locally built binaries have no tag provenance, so
ordinary unsigned scripts using `Cluster.add()` cannot be treated as positive
product startup acceptance. The non-GUI suites above now explicitly select their
mechanics input; migrating their entry points does not certify the full workflow.
Use owner-capable signed candidates for product acceptance. Native Go owner
fixture tests exercise mechanics with a controlled external verifier and must
remain explicitly separate from cryptographic evidence.

`release_source_gate_e2e.py` now truly runs standalone: no Runtime or Relay is
started, unavailable verification/dirty metadata/rehashed input are refused,
and an isolated static profile stays unchanged. It does not prove preservation
of a running service. Shared Cluster cleanup rejects a nonzero stop result even
if later IPC status appears absent, retaining the profile as evidence.

Installer-source acceptance uses an explicit owned workspace as well: assertion
failure or unconfirmed stop retains the directory and `cleanup-report.json`;
successful JSON is emitted only after confirmed cleanup. No signed-positive
claim follows from running that script without `--signed-bundle`.

`cli_argument_errors_e2e.py --binary <runtime>` runs the actual CLI without starting Relay/Runtime or creating a profile. It checks one JSONL `invalid_execution` terminal plus stderr and exit 1 for exec parsing errors, including unknown flag order, missing/type-invalid values, true/false precedence, inherited flags, `--`/literal-value boundaries, semantic errors, help, and non-exec commands. The same script runs in the Windows/Linux integration matrix; `internal/cli/exec_flag_error_test.go` also verifies untouched execution arguments and escaped error text.

`execution_input_e2e.py` additionally runs a real remote argv-report program with literal `--jsonl`, `--jsonl=false`, and `--unknown-option` after `--`, in both text and JSONL modes. `cli-literal-arguments.json` records the expected and observed arrays, including empty strings and Unicode; these payload arguments must not change the controller's output format.

`python -m unittest discover -s tests -p test_readme_docs.py` checks the Chinese/English onboarding entrypoints, identical executable examples, shared CLI/Relay/MCP/release spec links, current flags, and whitespace. It covers both human setup and Agent JSON/JSONL/request-file documentation; it does not replace native shell execution tests or close #73.

`release_source_gate_e2e.py` exercises standalone `runtime verify` refusals. `release_install_source_e2e.py` uses an isolated profile to prove install refuses before lock/profile/copy/Control.Check/stop, rejects rehashed bytes, and (when passed the real draft bundle) accepts the original verified bundle then preserves the running version on a rehashed retry. `XALLOR_TEST_REAL_GH=1 go test ./internal/releasetrust` uses the installed real gh with a malformed proof; this is a negative check. Rollback and MCP vendor enforcement remain outside #74 completion.

All process tests use owned temporary profiles and a loopback Relay. They never connect to the user's profile or official Relay. Cleanup stops only fixture processes, verifies the owned absolute directory and then removes it; failed cleanup retains evidence.

- `release_lifecycle.py`: legacy real-process install/upgrade/rollback harness; its unsigned local bundle is not a production install gate and is not run by CI after source authentication was enforced. Use Go `internal/release` mechanics tests plus `release_install_source_e2e.py` with authenticated raw bundles for current acceptance.
- `test_release_evidence.py`: zip/vendor/identity consistency, tampered payload/hash, signature-verifier rejection and pinned verification identity (offline tests; no claim of real OIDC signing).
- `test_signed_lifecycle_ci.py` and `test_signed_lifecycle_prepare.py`: exact protected B context, strict A/B identities, safe native extraction, preparation refusal before execution, six-case native evidence and same-operation restoration. Synthetic verifier fixtures cover orchestration only. The separate `signed-lifecycle.yml` runs genuine two-candidate Windows/Linux acceptance after draft creation; see [candidate workflow and evidence boundaries](../docs/attested-candidates.md#双候选验收工作流). It has no publish/sign/deploy authority and does not bypass human release approval.
- `signed_package_e2e.py --archive <signed-tgz> --bundle <signed-native-bundle> --tag <tag> --revision <full-commit> --observer <trusted-observer> --supervisor <trusted-package-supervisor> --output <report.json>` authenticates original inputs before standard npm installation. It compares installed application bytes with the tgz, runs its own CLI/MCP entrypoints, observes cold-start absence, shared owner reuse, explicit CLI restart/MCP reconnect and native cleanup. The npm helper uses existing Job/process-group supervision; deliberate Linux group escapes, dependency signatures and global shim resolution are not claimed. Any unconfirmed install-profile/resources retain the workspace. `package_heads_mechanics_e2e.py` separately tests this observation flow with copied local package files and a controlled verifier; it does not install npm or prove signatures. Input/output overlap, Windows archive aliases, protocol bounds and evidence rejection have regression tests.
- `release_attestation_ci.py`: real signed-release rejection of altered bytes and wrong signer workflow; runs only after attestations exist in release CI.
- Go `internal/relay` transport/request-policy tests cover PEM TLS, CA/hostname errors, health, public-listener refusal, forwarding spoof/quota/log attribution, trusted proxy chains and exact browser Origin (including IPv6 real upgrades). `internal/noderun/tls_execution_test.go` executes through the real Runtime and Relay using a test-only trusted CA.
- `packaged_install.py`: npm tarball installation in a new prefix, package/vendor manifest layout, and packaged CLI entrypoints. It intentionally does not call `runtime install` on an unsigned local tarball; `release_install_source_e2e.py` owns authenticated installer coverage.
- MCP discovers only untrusted bootstrap locations; Go owns managed activation selection. `source-trust.test.ts`, `bootstrap-snapshot.test.ts`, `bootstrap-launcher.test.ts` and `runtime-owner.test.ts` cover proof/identity rejection and owner admission. `owner-boundary.test.ts` checks that the obsolete managed selector is neither exported nor left in incremental build output. These tests do not replace signed-release acceptance.
- `diagnostics_e2e.py`: CLI/IPC/Runtime/Relay/target info; offline, pairing, grant, inbound and read-only checks.
- `handoff_e2e.py --binary <runtime> [--go <go>]`: actual IPC/Relay/Runtime with a deterministic Cursor CLI fixture; durable dedupe, conflicting reuse, matching session/result, bounded Unicode, repository/attachment validation and restart without replay. Authenticated official Cursor CLI verification is a separate gate, not replaced by this fixture.
- For current unsigned WIP, `handoff_e2e.py --owner-mechanics --go <go> --output <report.json>` uses independent native test-binary CLI/Runtime processes with a controlled verifier. It clones an existing source commit into the owned workspace without creating commits, records both Runtime identities and the target's changed launch after normal stop/restart, and explicitly compares returned Cursor session/request/workspace/revision. The six scenario groups use shared-grant authorization and loopback WS; this does not prove logical-session handoff, crash recovery, TLS, a published binary or official Cursor execution. Temporary cleanup success is required before a passed report is written.
- `internal/cli/independent_handoff_test.go` covers logical-consent revoke/rotate/natural expiry and public CLI subprocess submit/get. Three real Runtime processes include an independent same-owner observer that only queries receipts over IPC, never redeems the tested code. The original controller must lose access; the image-bound Agent must exit, jobs release, and IPC terminal must match the durable receipt read after target shutdown. Its descendant variant requires a real ordinary child, independent image/live/exit observations for both tasks, and no test-side task kill. It does not cover detached children or independently query kernel ancestry. Active receipt files are not polled, avoiding Windows observer/writer interference. Foreground exec shares the two-owner approval setup. Relay stays in the parent test, issuance is SQL seeded, provenance/Agent/CLI dispatcher are test fixtures. Linux needs real Git and a source checkout for the isolated clone; this is not signed/published-binary, official Agent or TLS acceptance.
- `mcp_consent_e2e.py --go <go> [--node <node>]`: builds the current MCP entry and runs four exact Go acceptance cases through real Node stdio JSON-RPC and independent Runtime owners. Covers session request/recipient IPC approval, scoped info/read/native exec, denied write/no file, owner revoke and no fallback; real controller Runtime replacement followed by MCP resume with rotated proof and wrong/reused/revoked-proof refusal; and running-task owner MCP revoke/rotate with correlated progress/output, independent parent/child image/live/exit watches and released jobs/transport. Checks every required case actually runs (a skip cannot pass), scenario coverage, process/profile cleanup and selected build/input hashes. Ordinary `go test` skips these Node-dependent cases unless both `XR_MCP_ACCEPTANCE_NODE` and `XR_MCP_ACCEPTANCE_ENTRY` are absolute files; CI uses the wrapper and uploads `mcp-consent.json`. Node teardown is supervised and accepts reaped signal termination on Linux, not a graceful-exit assertion. Task ancestry is fixture-reported; occupancy is sampled. TLS, official signing and published installation remain separate gates.
- Build the trusted read-only acceptance observer with `go build -o <observer> ./tests/fixtures/runtime-observer` (use an `.exe` name on Windows). `runtime_observer_e2e.py --owner-mechanics --observer <observer> --go <go>` checks its real native IPC peer, ready activation/launch, kernel image mapping and exit reference against an independent Runtime. It rejects a changed disk launch without repairing state; missing-manifest checks can reuse only a previously bound live reference. This controlled-verifier run is not signed lifecycle acceptance. CI uploads `runtime-observer.json`.
- `signed_upgrade_e2e.py --installer <trusted-installer> --observer <trusted-observer> --first <signed-A> --second <signed-B> --output <report.json>` requires two owner-compatible signed bundles. It retains native process references across upgrade/rollback and requires old exits, distinct launch identities, matching ready IPC identity and final exit/handle release before profile deletion. Windows uses original process image-file mapping; Linux uses sealed memfd/pidfd. The explicitly trusted observer is a test tool, not an artifact-provided trust root; its recorded SHA alone is not provenance. `--startup-fault` works on Windows/Linux: bind the active worker through lifecycle IPC/native peer, recheck after retaining a native process reference, bind candidate B's image/entry/parent to the original pending operation, then require structured same-operation failure/restoration evidence. Candidate selection comes from actual transaction observations, never Expected/intent or polling the actively replaced state file. Actual new signed A/B acceptance remains open.
- `runtime_recovery_observer_e2e.py --owner-mechanics --observer <observer> --go <go>` validates the Windows/Linux fault observer using actual worker/candidate processes and a real temporary identity-DB lock. It deliberately creates a different fixture image digest under the controlled verifier; it is not a genuine signed-candidate test. The same original operation must be failed/settled with B start quiescent_failure and A restore start/commit/ready confirmed; kernel exits and recovered identity/config/files are checked separately. Toolhelp/path are discovery hints on Windows, followed by retained-handle image mapping, actual parent and exact arguments. No candidate/worker kill manufactures success. Unconfirmed evidence retains the profile. CI uploads `runtime-recovery-observer.json` on both OSes.
- `handoff_identity_cancel_e2e.py [--go <go>]`: temporary Go overlay cancels immediately after the real identity gate is acquired; the real handoff handler must refuse dispatch, release the gate and admit a subsequent client for the same device. No product source is modified. This deterministic fault-injection check runs in both PR integration platforms; it is not a naturally scheduled race or independent Runtime handoff acceptance.
- `internal/cli/handoff_session_test.go` checks `submit/get --session-id` and keeps consent separate from the nested Cursor session and receipt ID. `internal/noderun/session_handoff_wire_test.go` uses real IPC framing, TLS Relay, approval and durable receipts; a deliberately absent workspace fails preflight before Agent execution. Missing/wrong consent is denied for submission/query, revoked consent cannot query, and submit/get cannot borrow each other's approved scope. The Runtime objects share the test process; this is not independent Runtime or Cursor execution acceptance.
- `handoff_cursor_live.py --binary <runtime> --executable <official-cli> [--prefix-arg <entry>]`: opt-in after local account login; creates a new isolated Ask session, confirms a remembered random token after resuming that exact session, verifies duplicate receipt and clean repository. Evidence stays outside the checkout; credentials and raw login output are never recorded. Ordinary CI does not run this account-dependent test.
- `cross_layer_e2e.py`: actual CLI nonzero exit; IPC file UTF-8/size/workspace boundaries; incremental output; cancellation ownership; timeout; flood truncation; grant rotation against an already connected client; updated pairing; Relay loss during execution and recovery; actual stdio MCP execution/nonzero/reconnection. The MCP smoke also compares the loaded Runtime PID, SHA, revision and formats with CLI observation, checks concurrent CLI ensure reuses that owner, and checks both heads observe the same replacement after restart.

Build MCP before local mechanics tests (the explicit fixture builds its own Go test executable):

```powershell
npm --prefix packages/mcp ci
npm --prefix packages/mcp run build
python tests/cross_layer_e2e.py --owner-mechanics
```

The cross-layer PR CI step explicitly uses native owner mechanics with a controlled test verifier; its report records `cryptographic_acceptance:false` and the fixture SHA. This proves interaction behavior, not signed-release acceptance. Ordinary `--binary` mode retains production source verification and requires a trusted installation. `pr-check` requires the integration matrix, release lifecycle matrix, Go, MCP and GUI checks. Linux also runs race detection. JSON artifacts describe completed scenarios; a nonzero script exit fails the gate even if earlier scenarios passed.

`cross_layer_e2e.py` checks a reviewed schema-13 metadata table inventory through
`fixtures/relay_schema_contract.py`, preserving signing-key and device/grant binding
assertions. This is schema/identity evidence, not a complete payload-privacy proof.
Reports start as `incomplete`; only nonempty object-shaped passing scenarios plus
completed cleanup can publish `passed`. Schema regressions reject unknown/missing
tables and old/future versions; mechanics regressions reject malformed result rows
and stale success after setup failure.

Unit-level adversarial checks additionally cover terminal EOF/missing fields/wrong IDs, fake terminal frames from another device, dropped client bindings, and grant rotation. No test result claims production deployment or authorizes merging a PR.

## GUI connection workflow

`npm ci`, `npm run build` and `npm test` in `apps/gui` validate frontend compilation and terminal-state/output-bound regressions. Install the test browser with `npx playwright install chromium`, then from the repository run:

```text
python tests/gui_e2e.py --binary <freshly-built-xallor-remote>
```

The harness owns a loopback Relay, two temporary Runtime profiles and a Vite process. Playwright performs actual alias persistence/XSS escaping, hidden credential reveal/hide, per-target doctor, stream-before-completion, navigation, cancellation (including five immediate cancels), nonzero exits, actual incoming approval allow/deny, audit and mobile-layout checks. No HTTP/Runtime mocks or production service are used. Screenshots and bounded failure audit metadata stay in a temporary evidence directory outside the checkout.

`--native` additionally runs the ignored Rust bridge integration test against that fixture: real IPC status, approval subscription, streaming and same-session cancellation. CI compiles the native application on Windows and Linux; browser interaction coverage does not claim native-window/WebView behavior. Native integration requires Rust/Tauri platform dependencies. The aggregate required gate includes both GUI jobs.
