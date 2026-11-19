# Runtime Execution Ownership Implementation Plan

Goal: Move Runtime selection, provenance, activation, startup, observation and cleanup into the Go owner.
Architecture: Go owns the profile state machine and starts private activation snapshots. MCP, CLI and TUI call runtime.ensure over local IPC. GUI is excluded from this slice.
Tech Stack: Go 1.25, existing IPC/SQLite/profile state, TypeScript MCP, existing provenance verifier, Windows process supervision and Linux process groups.
Spec: docs/superpowers/specs/2026-11-12-runtime-execution-ownership.md

## Current ownership audit (2026-11-17; unfinished)

Selection retention now has an owner-internal schema 8 receipt prerequisite.
It binds the admitted transaction and requested confirmed commit/ready to exact
resource, activation, launch, source and image identities, with atomic operation
and resource revision checks. Persisted reads revalidate the historical pending
record digest; a later terminal outcome does not erase that linkage.

Review reproduced two completion/admission defects: a terminal operation with a
subsequently invalid receipt reported settled and allowed a new operation; closed
resource rows were filtered out before identity validation. Both have behavioral
RED/GREEN regressions and are corrected. Admission now checks all represented
resource operations one at a time under lifecycle.lock, with bounded per-operation
reads. This trades history scan time for a consistent fail-closed gate; no cache
or automatic repair has been introduced.

Normal tracked install/rollback now implements the production path in
`E/activation-native-integration-plan.md`: fresh candidate intent before allocation,
stable preallocated launch identity, prepared native ready evidence and durable
observer closure, then owner receipt transfer. Exact native kernel peer/image,
activation/launch, bounded raw manifest and stable selection checks precede ready
confirmation. Owner-private locked closure avoids recorder reentrancy during an
effect. Rejected candidates close only after receipt-based deletion; uncertainty
preserves files and debt. Reused rollback activations create no new allocation
obligation and leave earlier operation resources unchanged.

The final controller matrix passed on unchanged source: uncached Windows
`go test -p 1 -count=1 -timeout 20m ./...` (206.656s), Windows `go build ./...`,
and seven freshly compiled native Linux groups, with cleanup_errors empty.
Linux explicitly ran the native journal upgrade/rollback/restoration and ready
peer/image/manifest tests. `non-gui-release/native-activation-validation.json`
binds source, commands, logs and binary hashes. The earlier API-only
selection-transfer-validation.json is a separate historical checkpoint.

Native tests use controlled provenance with real processes/IPC/SQLite, not signed
release acceptance. Historical unknown-debt recovery, native R1–R7 collectors/actions
and signed Windows/Linux lifecycle acceptance remain unfinished. Bootstrap is still
guarded outside the admitted install transaction;
its fresh candidate now shares the preallocated nonce, but this batch does not
claim a new operation ledger for bootstrap. GitHub release inspection still shows
only the earlier draft v0.2.0/v0.2.5/v0.2.6 candidates.

Linux Exited observation also now retries EINTR. A real thread-targeted SIGURG
regression failed three times before the fix and passed three native package runs
after it; the final integrated Linux run passed too. This demonstrates the
observer defect without attributing every historical lifecycle failure to it.

Probe and source closure integration is now complete for normal tracked lifecycle
operations. Prepared probes record original activation/image/raw-source identity
before execution, reuse native process supervision, bound retained output, and
settle only exact cleanup receipts. Probe IDs are not service launch IDs. Source
acquisition closes original input handles and quarantine before settling its row;
snapshot-local member Close failures remain unknown independently of consumer errors.
Windows supervised IO closes adapter-owned files exactly once and preserves first
errors from copies, final drain, duplicated handles, Job preparation and primary
thread release. No caller-provided files are claimed or closed.

Final probe/source controller matrix: uncached Windows full Go tests (182.094s),
Windows build, and seven freshly compiled Linux groups (55.141s container run)
passed on unchanged source with cleanup_errors empty. Linux explicitly exercised
native journal lifecycle, calibrated intent denial, inherited-output descendant
exit, noisy output, incompatible/slow candidates, timeout and nonzero exit.
`non-gui-release/probe-source-validation.json` is the current proof; earlier
matrix reports remain historical. Source member-close faults use real files with
a synthetic error return; Windows input-handle and Linux descriptor ordering tests
are native. None of these fixtures establish signed release acceptance.

Next closure work is finite: durable interrupted quarantine ownership, grouped
worker/admission coverage, exact worker/child evidence, then supported
native reconciliation and bootstrap boundaries. Reuse completed StopObservation
and Start effect evidence; do not create duplicate per-handle/job registries.

Managed stop preparation now records its observer group before capture, with the
same preallocated AttemptID as StopIntent. Partial attempts return exact idempotent
cleanup receipts; missing/mismatched/duplicate receipts and image-capture uncertainty
retain debt. ApplyStop checks and settles the original group under its existing
mutex before confirming stop/not_applied. Existing StopObservation remains the
runtime/job release proof. No schema change or new reconciliation authority.

Bootstrap now applies the same historical resource gate as new operation admission:
terminal outcome cannot hide unknown, malformed or orphaned debt. Behavioral RED
demonstrated those three bypasses; positive settled-history and absent-journal
tests remain accepted without database creation. R4 resolved retries now require
the original CAS coordinates, strict resolution JSON and existing-only DB opening.
The precise delete-between-stat/open race was not reproduced; the new mode cannot
create the missing file. Unknown resolution fields were previously blocked by
resource checks, not observed as a successful false resolution.

Latest final evidence is `non-gui-release/stop-preparation-validation.json`:
uncached Windows full Go tests (183.641s), full build, seven freshly compiled Linux
groups (60.594s container run), unchanged source and no cleanup errors. Linux ran
bootstrap/R4 guards, preparation debt, real native journal lifecycle, lost-stop-ACK,
selection/capture failures and cancellation tests. Earlier fixture revision/count
failures during the internal contract migration are retained in diagnostic logs;
the complete final packages now pass. Signed release acceptance is still separate.

Normal admitted worker teardown now attributes admission proof and query endpoint
groups, joins query shutdown before terminal completion, and retains the first
listener/connection close error. Public Run still completes on return; the private
settlement path cannot finalize a duplicate invocation. Native launcher Attempt.Done
joins the exact child wait, ACK reader and cached image/pipe/log close results.
ACK cancellation stops observation only, with no worker kill or replay. Handoff
and acceptance share one nativepipe primitive while retaining separate EOF and
streaming-JSON semantics.

Review reproduced real Linux SIGPIPE from writing a lost stdout ACK after admission:
the old worker died with Pending. A scoped borrowed-fd Unix write now returns EPIPE
without changing global signal handling; native GREEN observes backend rejection,
settled receipt and both closed worker groups. A second native regression caught
negative syscall read counts leaking into io.Reader; shared nativepipe now returns
zero with the original error. Neither fix allocates a new acceptance descriptor.

Current final evidence: `non-gui-release/worker-settlement-validation.json`, uncached
Windows full Go tests (186.219s), build and nine freshly compiled Linux groups
(63.750s), unchanged source/empty cleanup errors. Linux ran actual lost-ACK worker,
inherited-output launcher, shared pipe error and native owner replacement tests.
This establishes normal worker/launcher settlement, not durable pre-authentication
quarantine ownership, original worker identity after crash, the journal's own
final handle closure, bootstrap-attempt accounting or signed lifecycle acceptance.

Provenance verifier processes now use the existing supervised process boundary,
preserving the fixed gh trust policy and redaction. Real RED observed a root-exited
verifier taking 7.99s despite a 2s context while a descendant held stdout. Native
settlement now joins the family/output resources; distinct verifier cleanup
uncertainty is attributed to the source group, independently of consumer errors.
Final provenance-settlement-validation.json passed unchanged-source Windows full
Go tests (191.860s)/build and ten freshly compiled Linux groups (65.360s), with
empty cleanup errors. Controlled fixture evidence is not cryptographic acceptance.

Native filesystem identity investigation is recorded externally in
quarantine-identity-native/findings.md and report.json. Windows replacement kept
the original creation timestamp despite a different 128-bit ID; tested Linux
tmpfs returned no BTime and different objects shared ctime/mtime. No historical
deletion authority follows from the 16 successful identity relationship checks.
The proposed NEW private-slot management boundary is in
docs/superpowers/specs/2026-11-17-quarantine-ownership-proposal.md and awaits a human
decision. It is not permission to enable historical deletion or a guarantee of
physical object continuity. Continue existing authorized verification separately.

Independent current acceptance audit (while that decision remains pending):
official Node 20.20.2 snapshot build and the entire MCP test script passed on
Windows (74 pass/2 platform skips of 76) and native Debian Linux (76/76, no skips).
Official archive hashes and Linux koffi lock integrity were checked. An Alpine
loader mismatch and 128-PID harness saturation are retained as environment
diagnostics; the final unchanged Linux script passed with 512 PID limit, with
offline/read-only/CPU/memory bounds retained and all owned containers cleaned.
The actual current Windows cross-layer CLI/Runtime/Relay/MCP 18-case suite also
passed with cleanup_complete. Go hashes still match the latest frozen matrix.
See external non-gui-acceptance-audit.md/json and mcp-node20-audit/summary.json.
These results do not constitute clean-install/remote CI or signed lifecycle proof,
and do not approve the new historical cleanup policy.

Further independent acceptance: the four Windows handoff CI contracts now pass
6/7/7/8 scenarios (28 total) with freshly compiled owner/Agent fixtures, complete
cleanup, unchanged sources and unchanged project HEAD. These are controlled Agent
contracts, not real Cursor AI approval/model execution. Current production CLI
readonly verification also accepts authentic old 0.2.6 raw artifacts natively on
Windows/Linux and rejects binary substitutions even after manifest rehash. No old
candidate was installed or executed; the Linux tool was direct official gh with
rechecked checksum. A Windows harness's mistaken cross-platform-positive expectation
is preserved separately; the product's platform refusal was correct and unchanged.
See external handoff-signed-verifier-audit.md and its referenced JSON evidence.
This closes those precise verification slices, not current signed release/lifecycle
or the still-unapproved historical management boundary.

Remaining Windows external integration gates now have current-source evidence:
18 scripts/109 scenario entries reached their required outcomes after a diagnostic
regression fix. The initial job-crash test preserved six correct behaviors but
failed the strict expected-refusal text check. Public stop now retains the existing
specific job-resource diagnosis and typed underlying cause; prior ACK uncertainty
also survives later wait/job failures. The harness was not relaxed. Intentional
crash debt remains retained with product_job_release_confirmed=false, even though
all test-owned processes were observed exited.

Final stop-diagnostic-validation.json passed unchanged-source Windows full tests
(184.922s)/build and ten fresh Linux groups (65.079s), cleanup_errors empty.
Isolated package V2 contains the exact six-file fix and passes both platform
builds, the original MCP pack script, release evidence/SBOM/index and npm layout.
It remains dirty local evidence, not a signed candidate or installation proof;
the npm native-build-script policy notice was not bypassed. All original project
outputs and HEAD were preserved during that package run. External
integration-package-audit.md records exact scope and retained owned evidence.

Native Linux external coverage is now complete for those existing mechanics
scripts: 23 scripts/150 scenario entries (104 integration, 28 handoff, 18 cross-layer)
reached expected outcomes on unchanged inputs. The five-case Windows difference
is available-shell coverage. Initial ENOSPC, 1 GiB OOM and audit-selected Node 20
versus CI integration Node 22 mismatch are preserved as harness diagnostics.
The final Node 22 run kept offline/read-only/1-CPU/2-GiB/512-PID bounds. All five
owned containers were removed and absence checked; uncertain job evidence was
archived without claiming product debt released.

The exact existing CI 13-package race gate passed natively (789 pass events,
three platform skips, no races), as did six supplemental Runtime packages
(309 pass events, two documented skips, no races). Input hashes and all 19 executed
binary hashes are retained in linux-race-audit. Native toolchain/Git setup failures
were corrected only in private audit storage. After verification, PR check gained
a separate Linux-only Runtime ownership race step for those six exact packages;
the original race command remains unchanged. YAML/package mapping and unchanged
Go source were verified in ci-runtime-race-coverage.json. See external
linux-acceptance-and-race.md. No remote CI or signed lifecycle pass is implied.

### Historical implementation notes (2026-11-16)

R4 reconciliation is implemented as a Runtime-owner-only internal tranche. Strict
SQLite schema 6 adds bounded resolution and revision columns; schemas 1–5 refuse.
`Journal.Reconcile` acquires lifecycle admission, validates namespace and original
record digest/revision, refuses active workers and malformed/oversize rows, and
classifies only complete pre-dispatch stop/not_applied evidence with settled
jobs/runtime/observer resources. It appends a resolution with CAS while leaving
the original record/effects/outcome immutable; generated active derives from the
resolution. Repeated resolved requests are idempotent; missing evidence remains
unresolved and no Run/Install/Rollback/replay/force-clear path exists.

R4 has no public IPC/CLI surface yet. Its resource evidence is an owner-internal
bounded input under the lifecycle lock, not a user assertion or historical proof.
The independent review found and the implementation corrected no-journal creation
and oversize-row misclassification; review boundaries retain that R4 does not
settle source/probe debts, reconstruct lost native handles, or resolve old schema
rows. Windows full Go test/build passed in 99.578 seconds; Linux seven groups
passed with cleanup_errors:[]. reconciliation-r4-validation.json binds final source
and platform reports. Next R1–R3 observation cases, R5–R7 compensation/continuation,
and thin Runtime IPC/CLI remain required.

R1 present-state classification is now implemented as a pure Runtime-owner
predicate with typed evidence, but has no native observer, journal writer, IPC or
CLI authority. It accepts only a confirmed requested commit with missing/unknown
historical ready acknowledgement plus exact owner process/source/selection proof
and settled jobs/runtime/source/probe/observer debt. Baseline restoration, stale
namespace or record digest, identity mismatch, active worker/reconciler, and
unknown debt remain unresolved. Focused and package runtimeowner tests pass;
see `docs/superpowers/reconciliation-r1-implementation.md`.

Lifecycle intent binding is implemented and verified. Schema 5 writes a resolved
request binding before effects: original baseline selection, authenticated
candidate manifest/artifact identity, raw manifest byte digest and activation ID;
rollback clear-only intent is explicit. Effects carry requested, baseline_prepare,
restore or cancel purpose. Record validation now requires the first observation to
equal the binding baseline, each later observation to equal the prior observed
selection, and legal prepare/stop/start/commit/ready/restore/cancel phases. A
restored baseline commit/ready cannot satisfy requested success; failed fresh
cancellation is a failed empty-baseline result. Writes marked not_applied are
rejected; only independently proven stop refusal may use that state.

Unmanaged live baselines refuse before staging or Stop. Legacy Pending recovery
must stop first, create a fresh post-stop prepare/launch nonce and then start;
old pending identities cannot be reused. Bind is write-once and immutable, and
source binding uses raw verified bytes rather than a later bundle-path read.
Schemas 1-4 refuse without migration. Existing owner/native fixtures were
migrated to explicit bindings and contiguous evidence; native assertions retain
requested candidate identity when restoration returns to the baseline.

Initial Windows/Linux matrix had stale test fixtures: one Windows runtimehead
record and one Linux owner helper were missing schema 5 bindings. Both failures
are retained in the external evidence directory; the updated fixtures passed.
Final Windows full Go test/build passed in 194.844 seconds; Linux
release/runtimeowner/runtimehead/noderun/job/runtimeprocess and seven native
groups passed with cleanup_errors:[]. lifecycle-intent-validation.json binds the
unchanged source snapshot, both platform reports, and review artifacts. A stale
fixture failure and one transient Linux parent-observer interruption are retained;
focused and full reruns passed after the current fixture/source state. This
milestone still does not implement explicit reconciliation or resource-ledger
resolution for historical uncertain rows.

In progress: persist the resolved requested target and transition purpose before
any lifecycle effect. The bundle-path intent digest alone cannot identify the
original verified candidate after a crash; a final commit/ready may instead be a
restored baseline. Required recorder binding will retain the exact candidate/source
identity, original raw-manifest digest and baseline selection; effects distinguish
baseline preparation, requested transition, restoration and cancellation. This is
the first concrete stage in the reconciliation sequence below, not a substitute
for the planned real IPC/CLI endpoint or complete resource accounting.

Source snapshot cleanup no longer discards Close/Remove errors. Behavioral RED
reproduced deletion of unknown snapshot content, loss of cleanup uncertainty
alongside consumer/proof failure, and successful return with an actual Windows
non-delete-sharing handle still holding the snapshot. Cleanup now uses a retained
creation receipt for the directory and each exclusively created file, validates
the bounded full set, and removes only known members and an empty directory.
Unknown/replaced contents and failed cleanup remain evidence. Close/cleanup errors
join the original cause with ErrCleanupUncertain; failed calls return no Manifest.

The full Install callback may already have changed state before deferred cleanup
fails. The error does not mean not-applied and does not trigger another install or
restoration. Run waits for that complete backend result before Complete stores
Uncertain. This fixes truthful failure reporting; it does not reconstruct old
resource ownership or settle previously uncertain transactions.

Final Windows full Go test/build passed in 491.234 seconds (CLI 280.043 seconds),
temporary system activity request released. All seven Linux verification groups
passed, including native lifecycle/recovery, cleanup_errors:[]. Source hashes and
report/binary hashes match in source-cleanup-validation.json. Independent static
review found no actionable scoped defect. Descriptor-close failures were reviewed
statically rather than fault-injected; partial-write and directory-mismatch tests
are explicit mechanics simulations, not atomic hostile-writer proof.

The inspected reconciliation design now has a finite implementation sequence
(external reconciliation-design.md): bind requested candidate and transition
purpose; record resource obligations plus preflight_closed/effects_closed barriers;
append original-ID/revision-bound resolution evidence atomically with effective
active state; then expose shared Runtime IPC and thin CLI with an authenticated
no-owner role. First real endpoint cases are completed request with recoverable
cleanup debt, exact commit with freshly authenticated readiness, proved restored/
clear result with tracked debt, and a proven clean undispatched epoch. Subsequent
cases cover fenced preparation compensation, exact-live candidate continuation,
and resource-proved quiescent restoration. Each needs named crash-boundary native
tests and strict refusal when proof is missing; an assessment-only command is not
the endpoint deliverable. Original effects/outcome remain historical observations.

Schema 4 lacks complete ancillary resource/causal accounting: ready alone cannot
distinguish missing acknowledgement from source/probe/admission cleanup debt, nor
requested success from baseline restoration. Do not implement reconciliation as
Finish(Succeeded), a new Run/rollback, deleting the row or force-clearing active.
This next work is not implemented yet; no historical resolution is claimed here.

Prepared stop evidence is implemented and verified. Required PrepareStop replaces
the release Control stop contract. The single activation algorithm captures a
same-connection kernel peer, retained process/image observation, and process-latched
activation/launch before journal intent; dispatch runs once only after persistence.
The journal records typed dispatch, exit, locked job/resource and observer-release
observations plus independent post-selection. Missing/invalid ACK still retains
uncertainty, even when independent exit/resource observations succeed. Nil callback
success alone cannot confirm a stop. Close failures are included before recording.

Schema 4 refuses schemas 1/2/3. Durable stop intents reject public-observation and
image-only scope, unrelated target selection, nonempty fresh baseline and missing
legacy Pending. This closes a P2 validator gap found in static review: orchestration
checks alone did not protect received/persisted receipts. Six negative intent cases
now cover pending, unknown and terminal records. Some new tests followed inspected
gaps after implementation; their report explicitly does not claim behavioral RED.
The lost-ACK observer regression did fail behaviorally before correction.

Status carries actual permit-latched execution identity even while pending, without
claiming readiness. Public stop shares the engine and verifies the actual image;
it permits unmanaged image-only observation, which is excluded from durable effects.
No-peer fresh/guarded legacy recovery remain distinct from observed peer exit.
Recovery keeps its lifetime guard through launch revocation/new selection, separate
from observers closed before journal confirmation. The captured profile survives
ambient environment changes; selection is rechecked before dispatch. Same-path
host/file-object or deliberate same-user rewriting protection is not claimed.

Windows full Go test/build passed in 326.968 seconds (CLI 138.344 seconds), with
temporary activity request released. Linux release/runtimeowner/runtimehead/job/
runtimeprocess packages, focused daemon identity/status/startup and native prepared
stop/lifecycle/recovery checks passed; cleanup_errors:[]. The first Linux daemon
unit run failed because LoadFrom fixtures requested a default workspace under the
read-only /root. The node suite now has a dedicated profile in the container tmpfs;
the same seven binary hashes passed without relaxing read-only isolation. Both
reports are retained. prepared-stop-validation.json verifies unchanged Go sources,
binary/report hashes, implementation/test reports and final independent review.

No new automatic replay or historical operation reconciliation is provided. Next
work must use the original journal identity, authenticated current process/state
and these typed evidence boundaries to reconcile explicitly; old unbound records,
lost native observers and historical job debt must not be guessed clean. #63 job
recovery and #75 current signed-release acceptance remain open. No GUI, commit,
merge, publication or deployment occurred.

Stop observation now binds the kernel IPC peer and retains its process observer
before reading reported status. Native named-pipe regressions reproduced false
success while the connected server remained alive after reporting another exited
or live-helper PID. The corrected path refuses mismatched status before stop;
failure to capture the actual peer, including ErrProcessGone, cannot be converted
to success by a free lock. A third test independently establishes a truly absent
reported PID. Existing actual-daemon, delayed-IPC-close, job-ledger retention and
authenticated task-tree reclamation tests remain intact and pass.

Final Windows full Go test/build passed in 226.125 seconds, with process-local
package parallelism 1, GOGC=50 and GOMEMLIMIT=512MiB after observed host memory
pressure. Temporary system activity request was released. Linux release/owner/
head packages and native Stop plus lifecycle checks passed with cleanup_errors:[].
Source hashes are verified unchanged across final checks in stop-peer-validation.json.
Independent static review found no actionable defect in the scoped correction.

First Linux run failed TestNativeCLILifecycleUsesOwnerTransactions at install's
180-second context deadline without a recorded operation; other native lifecycle
and all Stop tests passed. The failed report and binary are retained. Failure-only
diagnostics now expose journal/log existence and a bounded transaction stderr tail,
never startup/identity/credentials. Diagnostic-only rerun passed; root cause is
not established and is not claimed fixed. Preserve this acceptance uncertainty
alongside the confirmed PID bug fix. No production logic/deadlines were changed
in response to that first Linux failure.

The prepared-stop design previously recorded here is implemented by the current
milestone above; stop-evidence-design.md remains the inspected design history.
Historical uncertain-operation reconciliation remains a separate required step.

Durable effect selection binding is implemented and verified. Schema 3 stores
the exact decoded selection digest and current/previous/pending identity slots
before dispatch, a distinct intended selection for writes, and independently
observed selection afterward. Start targets bind artifact/activation/launch,
including restoration of Current under the active Pending nonce. Commit/ready
completion compares complete target identities and exact committed selection.
Missing/mismatched post-observation retains uncertainty; callback panic retains
the durable pending intent. Schema 1/2 refuse without migration or repair.

Regression RED demonstrated that SHA-only unbound completion was accepted. A
second RED caught the old SQLite 16 KiB limit at effect 7 when full evidence was
added; SQL/query/save now share the 128 KiB bound and 32 full snapshots pass.
Tests cover different launch/activation for the same SHA, owned snapshot copies,
post-read failure/mismatch, pre-capture refusal, interrupted dispatch and normal
restoration. Native assertions compare persisted records with actual Store state.

Windows full Go test and build passed in 834.703 seconds (CLI 745.066 seconds).
Linux release/runtimeowner/runtimehead packages and five native CLI checks passed,
including fresh install, upgrade, rollback, stop refusal and failed-candidate
restoration; container cleanup_errors:[]. effect-selection-validation.json binds
all Go source hashes to both platform results and the static review report.
Temporary thread-scoped system activity request was released. An intermediate
runtimehead run was interrupted during fixture edits; its result is not counted
as passing evidence. Final stable focused and full runs passed. Review identified
no actionable defect within this slice, not whole-branch or release certification.

That earlier milestone completed selection evidence only. The current prepared
stop layer additionally captures process and resource observations; explicit
uncertain-transaction reconciliation and historical job recovery remain incomplete.
No replay or automatic terminalization was introduced.

Earlier namespace milestone (now superseded by schema 4): schema 2 atomically stored
its configured absolute namespace digest and each record repeats that binding.
Open/query/save reject wrong, missing or corrupt binding and refuse schema 1
without rebinding. Native/head tests first reproduced accepted whole-database
and row transplants, legacy admission, and foreign successful receipts. Mutation
IPC now requires the caller's namespace and checks it before worker creation;
heads retain the original namespace across subsequent queries/environment changes.
The existing forged-observer and launch-nonce tests retain valid namespace inputs
so their original authentication failures remain exercised.

Windows full Go test/build and native wrong-profile/no-mutation plus normal
install/rollback checks passed. Linux ran runtimeowner/runtimehead packages and
native namespace, launch identity, normal lifecycle and legacy-pending recovery
checks successfully; cleanup_errors:[]. Evidence: journal-namespace-validation.json.
An earlier full Windows run failed during a rollback deadline spanning observed
Modern Standby (14:02 enter, 14:14 exit). That failed log remains separate. The
unchanged checks passed in 631.797 seconds with a temporary thread-scoped system
activity request, which was released afterward; no power-plan or test deadlines
were changed. Static review found no namespace-binding defect.

This remains exact configured-path correlation, not alias equivalence, a MAC,
host identity or protection from deliberate same-user store rewriting. It
provides no journal resume/replay. The latest effect binding above adds selection
and activation/launch observations; the current prepared stop layer adds typed
cleanup observations for new attempts, not retrospective proof for lost observers.

Recovery groundwork now binds runtime.ensure/status ready identity to the actual
serving launch. Launch ID is retained from the authenticated inherited permit;
stable selection must match it. Go/MCP heads reject missing or malformed IDs.
Native regression first showed absent identity, then proved direct rollback IPC
could bypass ready refusal after a same-artifact nonce change. The launcher now
compares OwnerHandoff.OwnerLaunchID with its process-bound value before worker
creation/journal admission; worker fresh-permit comparison also includes it.
Mismatch returns refusal without repairing the state or creating an operation.
Cold legacy recovery has no serving owner and retains its separate strict permit.

Final Windows full Go test/build passed (CLI package 548 seconds); MCP passed
74 with two platform skips. Windows/Linux cross-layer each passed 18 cases,
and native Linux launch-identity/state-tamper/direct-IPC regression passed with
cleanup_errors:[]. Source hashes, exact report scope and logs are recorded in
launch-identity-validation.json. Review found no new binding defect. This
supersedes the earlier process-observer build as the latest Go/MCP validation.

The next unresolved boundary is explicit reconciliation of the original operation.
Records now retain namespace, kind/intent, exact before/intended/observed selection,
target activation/launch IDs and typed prepared-stop outcomes. Same bytes, absent
PID or a free writer lock alone still cannot finish an uncertain operation. Lost
native observers cannot be recreated from these serializable fields. Further work
must validate the original operation and permissible fresh evidence without replay;
no historical terminalization path has been added by these slices.

Runtime ownership implementation and validation remain WIP; this is not release
or deployment completion. Normal owner activation/start/install/rollback and the
authenticated legacy-pending recovery path are implemented. Explicit reconciliation
of unfinished/uncertain owner-journal transactions is still absent. Historical
uncertain jobs are retained; production job verify/release recovery (#63) is not
provided by the test-only expected-retention assertions.

All non-GUI Cluster-based E2E entry points now support explicit native owner
mechanics. The remaining direct Cluster calls in cross-layer/diagnostics already
select their fixture explicitly; the GUI harness was not changed. Other release
and full-workflow gates still need their own audit and acceptance evidence.

Latest handoff migration: state/dedupe (6), scope (7), revocation (7) and continuous
authorization (8) passed on both Windows and Linux. Windows executed the exact
serial CI entry pattern using one prebuilt owner fixture, in 655.219 seconds
including its build. The aggregate handoff job budget is now twenty minutes;
individual request, observation and cleanup deadlines are unchanged. Reports
record native owner and Go contract-Agent hashes, with explicit non-real-Cursor
and non-cryptographic scope. A retained process reference now verifies timeout
and Runtime-stop exit instead of a later PID lookup. Public reports are written
only after strict cleanup; no primary or cleanup failure is converted to passed.

Evidence: handoff-both-os.json, handoff-windows-ci.json and handoff-linux.json in
the existing non-gui-release evidence directory. Harness/helper hashes match the
current tree, and Linux cleanup_errors is empty. Two initial Windows concurrent
setup attempts failed before scenarios (probe deadline/source-verification
refusal). Their incomplete reports and handoff-windows-first-attempts.json are
retained; the root cause was not established and later serial success is not a
claim to have fixed them. Static review found no new weakened assertions. Python
discovery passed 49 tests with two platform skips.

The later launch-identity aggregate above is the current full Windows Go/MCP
validation. Signed
owner-capable production artifacts and end-to-end cryptographic installation
acceptance (#75), full GitHub CI on committed changes, and recovery requirements
remain open. No commits, merge, production upgrade or real Cursor experiment
were performed in this fixture slice.

## Earlier ownership and verification notes (historical)

The notes below record intermediate evidence and superseded pending states.
Use the current audit above for present completion claims.

Resource boundaries and identity transactions now use explicit owner mechanics,
shared strict cleanup and incomplete-before-construction report handling. Resource
tests retain transport/job capacity separation, observation/file access while
jobs are full, refusal of additional exec and cancellation/reuse assertions.
The execution helper can be supplied explicitly for offline Linux with its byte
hash recorded. Identity tests keep migration, write fault, contention, committed
restart, failed reset and corrupt identity refusal checks. Crash injection now
confirms jobs are empty, holds a native process object, then terminates that object
instead of signalling a later PID lookup; Linux gets an actual abrupt SIGKILL.

Windows and native isolated Linux each passed four resource and six identity
checks, with cleanup complete and Linux cleanup_errors:[]. Current harness/helper
hashes match resource-identity-both-os.json. Python discovery passed 49 with two
platform skips; static review found no weakened assertions or false publication.
CI has both explicit mechanics steps. The only remaining non-GUI direct legacy
Cluster consumers are the four handoff suites; release/crypto gates and recovery
work remain separate. This slice changes test/CI plumbing, not product Go/MCP.

Shell adapter now uses explicit owner mechanics and publishes the public report
only after all helper/configuration/restart checks and strict cleanup. The Node
helper writes intermediate results in the owned fixture. Its missing-pwsh branch
now restricts the target's explicit mechanics PATH as well as the harness PATH;
otherwise fixture_env restored the unrestricted path. Only the verifier binary
and platform shell are retained, with both environments restored afterward.
Windows passed ten checks and Linux five, including explicit missing-interpreter
refusal. shell-adapter-both-os.json pins the current harness/helper hashes. The
CI step is migrated; static review found no newly weakened assertions. Python
discovery passed 49 tests with two platform-specific skips.

The previously pending native Linux authorization (9) and process lifetime (3)
suites also passed with cleanup_errors:[] and current hashes matching the latest
Windows reports. Container preflight found no Git; two online bootstrap attempts
failed on permissions/network before any scenarios and cleaned up. The final run
instead used an official Debian Git 2.47.3 package verified against the published
SHA-256, extracted only under /xr in a readonly, network-disabled, limited owned
container. It changed no host packages or business container. Evidence is
lifetime-owner-linux.json plus test-git-source.json. These results cover these
suites, not Linux acceptance of the whole remaining repository or signed releases.
The subsequent resource/identity migration is described above; four handoff
suites remain direct legacy Cluster consumers. GUI remains outside this work.

Client authentication and peer flag tests now use explicit owner mechanics and
the shared strict cleanup/report boundary. The unconfirmed Relay still must see
zero invokes in shared/account CLI/doctor/MCP cases. Removed --id must fail before
creating a profile; --device must reach the owner. Review caught an independently
held process-exit assertion lost during cleanup migration; it is restored before
fallback native-ref termination, so a stop regression cannot be cleaned into a
passed report. Supplied production sources still retain normal verification.

Windows and isolated native Linux each passed these four scenarios; matching
current harness hashes were checked against auth-peer-windows.json and
auth-peer-linux.json, with Linux cleanup_errors:[]. The remote connection was
read-only checked before creating the owned limited/offline container. Plain
CLI parsing remained on a freshly built production binary and passed seventeen
cases without mechanics injection. Python discovery passed 49 with two platform
skips. Corresponding CI steps opt in explicitly, and peer report upload was added.
These auth/peer results are separate from the lifecycle evidence above;
no full workflow or signed-release claim is made. No product Go/MCP change here.

The initial input-suite uncertainty is now localized by its retained Runtime
startup.log: successful Windows PID query returned assigned=1/listed=0 and the
old capture path rejected it. Activity/ledger keys correlate that error with the
failed execution. A native probe found conhost.exe alongside the plain argv
fixture, ruling out a root-only workaround. Live observation now retains native
member handles before resume and during execution. A partial list is usable only
when lifetime cumulative population equals distinct retained handles; final
population stability and exit signals still apply. The observer joins before
settlement/close and bounds retained handles at 4096. Review found a late-overflow
error propagation gap; its native regression failed before adding the post-join
sticky-error check, then passed. Missing-child and live-handle cases remain
negative. Historical uncertain jobs are not rewritten or recovered.

Final-build Windows input (7), foreground authorization (9), process lifetime (3)
and intentional crash-retention (6) scenarios executed; input and crash-retention
also ran concurrently in independent profiles. The job outcome still retains its
ledger rather than claiming recovery. Final `go test -p 2 -timeout 20m ./...`
and `go build -p 2 ./...` exited zero; CLI package completed in 674 seconds.
The final report verifies the same fixture SHA across all four native suites:
3aae561d9202dbdf4ad909f4e5c011f48773b738d23e9e5f3c14eb763ddac6fb.
Evidence root-cause note: input-cleanup-root-cause.md; current source hashes,
test-log hash and native reports: process-observer-validation.json. Linux,
full GitHub CI and signed release gates remain separate.

Next fixture slice migrated job lifecycle, execution input/results and JSONL to
explicit mechanics mode, preserving the original assertions and strict cleanup.
Current Windows execution-owner-windows.json records 7 input, 4 result and 5
JSONL scenarios passed with cleanup and matching harness hashes. The TUI actual
RPC and MCP failure/timeout/truncation/disconnect paths executed. Input's literal
argv side report now invalidates previous success before setup too. A new JSONL
report variable shadowing failure was observed and corrected before its final run.

Job lifecycle initially twice reached its six behavior assertions then failed
strict stop because four uncertain jobs intentionally survive Runtime crash.
Its teardown now explicitly verifies this negative contract: only the precise
target job-release refusal is accepted, the replacement Runtime's retained native
reference must confirm exit, all tracked native child cleanups must finish, and
the uncertain ledger must remain byte-identical. A distinct outcome,
expected_refusal_verified, preserves cleanup_complete:false and
product_job_release_confirmed:false. The fixture profile is retained. No product
Stop gate, ledger release or recovery behavior was changed. Direct program launch
for the tree fixture avoids an unobserved shell wrapper while preserving capacity
and crash assertions. Final Windows job-retention-windows.json records six
scenarios and ten native test-process exit confirmations. Additional profile
errors, missing ACK, live owner and ledger changes have rejection tests; the
missing-ACK case was observed red before tightening the refusal match. Ordinary
IPC-bounds cleanup still passed. Python now passes 49 tests with two platform
skips. This closes the test teardown mismatch, not #63 production recovery.
The first input run also left one uncertain job; its original Node failure output
was unavailable. Explicit stdout/stderr capture was added, and three later runs
passed without a product change, including two bounded serial reproductions.
Those passing reruns alone did not explain the failure; the later retained-log
investigation and native regressions above localized and fixed its observation
gap. No matching executable was found in a read-only process snapshot for the
first two retained fixture roots;
that snapshot alone does not prove historic descendant cleanup. Python discovery
passed 43 with 2 platform skips. Linux execution of this slice remains pending.
Static review found no newly weakened assertions or suppression of primary
failures. CI executes the negative contract rather than skipping it; no full
workflow, final signed acceptance or production deployment is claimed.

Foreground authorization and process lifetime now also use explicit native-owner
mechanics selection and the shared incomplete-before-construction / passed-after-
cleanup evidence boundary. Existing process-tree, unique terminal, job release,
revocation and reconnect assertions are preserved. Trusted precompiled helper
inputs support offline testing and their byte hashes are recorded. The two PR
steps opt in explicitly; supplied production sources still use real verification.
Current Windows scripts passed all 9 authorization and 3 lifetime cases, including
actual MCP revocation and Runtime shutdown reclamation; cleanup completed and
harness hashes match lifetime-owner-windows.json. Python discovery passed 43 with
2 platform skips. The initial Linux connection attempts failed before container
creation and were not counted as tests. The subsequent offline-Git run described
above now verifies both Linux suites; the earlier five-suite report was never
used as a substitute for that evidence.

CI fixture migration now additionally covers backend selection, principal access,
device ownership, IPC bounds and device settings. These five scripts use explicit
mechanics flags; default supplied-runtime mode retains real source verification.
An actual unsigned current production CLI was refused with an incomplete report.
Shared evidence setup invalidates old success before fixture construction, records
harness/binary SHA, and publishes passed only after nonempty passing cases and
owned-cluster cleanup. Backend repair now explicitly recovers retained pending
state instead of replaying ensure; no production trust gate was weakened.

Final Windows and Linux five-suite runs each passed 27 scenarios, including actual
stdio MCP principal/ownership behavior, with cleanup complete. Reports are
ci-owner-batch-windows.json and ci-owner-batch-linux.json; current harness hashes
were checked against both. Python unit discovery passed 43 tests with two
platform-specific skips. Earlier Linux connection/upload failures did not enter
tests; a read-only check confirmed no residual labelled containers, and the final
chunked-upload run cleaned up without errors. PR integration now selects mechanics
for these five steps and runs selection/evidence/environment/cleanup unit checks.
The GitHub workflow itself has not run from this uncommitted tree. Remaining
standalone authentication, lifetime, handoff and other fixture consumers still
require migration; this is not a full-CI or cryptographic release acceptance claim.

Legacy pending rollback now uses a separately authenticated runtime-recovery
role rather than a CLI call into the rollback algorithm. It verifies the retained
Current source, or original Pending source for failed-first-bootstrap clearing;
no version-cache fallback is allowed. The launcher/role check absent IPC, free
Runtime/job resources and no unfinished journal, then the role owns the correlated
transaction. The bootstrap head waits on that exact child and only observes its
settled receipt afterward. Normal CLI production code no longer directly calls
Store.Install or Store.Rollback. Active/uncertain journal reconciliation remains
unimplemented and deliberately refuses automatic recovery.

Two adversarial defects were reproduced and corrected during this slice. First,
valid recovery input could wait forever for EOF: all internal inherited-input
entries now use bounded native nonblocking reads and a five-second input budget;
source/admission budgets remain separate. Second, an already authorized child
could arrive after clear-only recovery and initialize identity before noticing
revocation. Recovery now holds the service mutex through revocation and service
entry rechecks admission under that mutex before identity/IPC. Every new pending
launch has a fresh ID retained on commit, so recovery of the same activation also
revokes older permits without replacing the activation identity.

Windows scoped regressions passed, including a genuine authorized child paused
before the service lock, recovery completion, then release of the old child: it
exited without identity/workspace/IPC. The original parent remained alive, testing
revocation independently of parent death; this is not a complete host-crash test.
Native Linux legacy-recovery-linux.json passed the corresponding delayed-child,
same-activation, source-proof, occupied-resource and input-timeout cases with no
cleanup errors. These remain controlled-verifier mechanics, not signed releases.
The first Windows final aggregate hit Go's ten-minute package limit while the
last test had run only eleven seconds. It was not counted as a pass. Final
`go test -p 2 -timeout 20m ./...` and `go build -p 2 ./...` passed; CLI tests
completed in 717 seconds. Every individual startup/observation/cleanup deadline
was unchanged. PR check's aggregate Go test limit now also specifies twenty
minutes; that workflow edit has not been run on GitHub from this uncommitted tree.

Cold-start migration now covers normal no-owner operations. A fresh install uses
InitializeOwner, which requires completely empty references and authenticates one
requested bundle before guarded bootstrap. Existing/concurrent owner or selection
is refused instead of reporting another image as the requested installation.
For a complete stopped Current, CLI starts that original verified selection, then
submits upgrade/rollback through owner IPC. It never bootstraps the requested
upgrade as a substitute. Native Windows first-install -> stop -> cold-upgrade ->
stop -> cold-rollback passed, with settled owner receipts for both changes and
unchanged identity. The same Linux flow plus source-denial/race/unfinished-history
checks passed in cold-lifecycle-linux.json; cleanup_errors:[]. Source provenance
remains the controlled test verifier, not official release acceptance.
The final Windows `go test -p 2 ./...` and `go build -p 2 ./...` passed after
the cold-path migration. No packages were omitted; concurrency was bounded to two.

Latest running-owner CLI migration: runtime install/rollback now submit once on
the same admitted owner connection, expose the operation ID and only query that
ID across replacement. Unknown dial/handshake state never falls back to local
mutation. Terminal success waits for the lifecycle writer lock to be released
(settled), rather than interpreting a newly written terminal record as completed
worker cleanup. Offline runtime status --operation observes the same record.

Adversarial review reproduced a forged query endpoint returning a matching
rollback intent and successful receipt. Every newly opened query connection now
binds the kernel peer PID and retained exit observer to the actual process image
and original source proof of a stable Current/Previous/Pending activation. A
self-reported observer or settled flag alone cannot certify success. The forged
endpoint regression fails before the correction and passes afterward; static
review confirmed the scoped query-authentication gap closed.

Windows native public CLI upgrade, rollback and failed-candidate restoration
passed with original activation/device identity preserved. The test fault is now
bound to an isolated test-only profile marker: candidates inherit the owner, not
the invoking head's environment. Native Linux head-lifecycle-linux.json passes
the same CLI/worker/recovery tests, new query authentication, gate/cleanup and
settled observation cases; cleanup_errors is empty. Remote read-only inspection
also confirmed no labelled containers remained after the earlier disconnected
uploads. These are controlled-verifier mechanics, not official signed releases.

Windows unselected-candidate cleanup now retries only explicit access/sharing
denials for at most approximately one second. Each attempt rechecks the original
creation receipt under install.lock; changed identity, unknown errors or ongoing
denial retain uncertainty. There is no process/lifecycle replay. Real held-handle,
recheck-refusal and persistent-denial tests pass; the previously failing native
journal cycle passed three consecutive runs. The check/unlink race against writers
outside install.lock remains an explicit boundary. Final Windows full-suite
`go test -p 2 ./...` and `go build -p 2 ./...` both passed on 2026-11-15 after
this correction. The two-package limit bounds machine load; no packages or tests
were skipped by that setting. Earlier failed observations remain recorded below.

Legacy pending rollback without an unfinished journal now uses the trusted
recovery role above. Normal initialization and cold upgrade/rollback use the
bootstrap exception followed by owner IPC. Explicit uncertain-operation
reconciliation, remaining CI fixtures and signed-release acceptance are unfinished.
Do not mark the overall owner-takeover objective complete.

### Earlier worker integration observations

2026-11-15 worker integration: the serving Runtime now has install/rollback IPC
dispatch wired to an owner-launched protected `runtime-transaction` process. The
worker checks OS parent/kernel IPC peer/actual image before reading its inherited
envelope, then reauthenticates release proof and operation/profile/state binding.
It admits the journal, rechecks the retained parent observer under the lifecycle
lock, opens a fixed read-only lifecycle query endpoint and acknowledges the exact
operation ID, intent digest and worker PID. Accepted work runs independently of
the head's connection; the journal and replacement owner expose the same receipt.
One real Windows test completed serving-owner replacement and preserved identity.
The source verifier remains controlled test machinery, not release provenance.

Dispatch shutdown joins admission verification but does not wait for an accepted
worker that must outlive the serving process. Reply writes are bounded. Launcher
cleanup uncertainty retains its busy gate before/after worker start, and nonzero
worker exit cannot prove clean admission-resource release. A regression also
proved post-Admit refusal must close proof resources before recording Rejected;
cleanup failure now retains Uncertain. Read-only query shutdown closes and joins
idle clients before releasing the lifecycle lock. Static review found the scoped
gate/cleanup defects closed, without claiming the whole ownership migration done.

Remaining public CLI install/rollback routing and no-owner recovery are not yet
switched. Linux worker acceptance is UNVERIFIED: two upload attempts ended before
test execution with SSH EOF. A fresh observation after the first found no labelled
test containers. The second named xr-crossos-b0df670cb954 (owner b0df670cb954) had
unconfirmed automatic cleanup; subsequent SSH connect timed out. It was created
with --rm and a 300-second lifetime, but expiry is not observed cleanup evidence.
Record it for explicit read-only confirmation when connectivity returns; do not
touch other containers or claim this phase passed.

Latest final Windows full-suite run is NOT green: native journal Stop-refusal
cleanup hit Access Denied while deleting its unselected candidate. The operation
correctly stayed uncertain, so its optimistic Failed assertion did not pass.
The scoped FileProcessIdsUsingFileInformation snapshot reported holders 6120 and
24328 on that candidate, different from its completed probe PID 14324. A separate
read-only Win32 process/service lookup mapped these pre-existing svchost processes
to DiagTrack and InventorySvc. This is concrete external file-holder evidence,
not proof of their precise share flags or permission to disable either service.
All other packages in that run passed; do not hide this failing case, claim global
green or replay lifecycle effects to work around a retained uncertain receipt.

The coordinator now derives request identity from a canonical absolute Command,
claims execution once and completes from the entire release result. Cleanup
uncertainty before any journaled effect stays uncertain; invalid concurrent Run,
Complete or Finish cannot finalize a claimed runner. Native lifecycle tests now
use Run rather than manually supplying an expected terminal outcome. CLI ensure
and start refuse unfinished history, then hold the same lifecycle lock across
the final idle check and bootstrap (lifecycle -> install). Fresh source proof
precedes creating that lock directory. Offline runtime status includes the active
record, and the serving Runtime implements read-only runtime.operation.status
even while startup admission is pending. Unknown IDs never authorize replay.

Integration exposed a deterministic Windows selection-file race: an open reader
blocked os.Rename. State readers now share replacement and the writer uses Go
1.25 os.Root.Rename; held readers retain the old snapshot and new readers see the
new selection. Actual protected executable tests still reject rooted replacement;
their stricter sharing was not relaxed. Background version/provenance commands
now explicitly suppress Windows console creation, with prelaunch regressions.

Latest Windows full go test ./... and build passed; the final five native journal
cycles also passed. coordinator-linux.json records native coordinator/receipt,
bootstrap gate, snapshot replacement and pending-startup query passes with no
container cleanup errors. These are controlled-verifier mechanics, not signed
releases or an authenticated worker entry. Earlier Windows runs still observed
an unstarted candidate's file removal denied after Stop refusal, correctly making
the whole transaction uncertain despite a restored ready baseline. File-owner
diagnostics did not establish the holder's cause; an independent retained-process
observer experiment did not reproduce the lock. A separate foreground-job cleanup
observation was also once incomplete. Do not claim these intermittent observations
resolved solely from subsequent passes or attribute them to historical app hangs.

The remaining main task is the authenticated transaction process and its query
endpoint during serving-process replacement, then normal install/rollback routing
and explicit no-owner recovery. Public install/rollback still call the existing
backend directly; do not mark Runtime takeover complete or expose mutation IPC
before the handoff, query and admission path are connected.

The admitted journal can now instrument the existing release algorithm via
Store.WithEffects. It records selection writes and Stop/Start/Ready before
dispatch without introducing a second activation implementation. Journal or
commit uncertainty does not trigger a stop/replay/restore. A deterministic
failed Start is recorded as quiescent_failure (owned process/image released,
not "never executed"), allowing the original independently verified restoration
path to record its own start/commit/ready. Confirmed pre-dispatch Stop refusal
restores and acknowledges the original non-pending selection before a Failed
receipt can release admission; an uncertain acknowledgement stays unfinished.

Review found preparation cleanup errors could still appear ordinary. Native
image preparation now preserves an ErrCleanupUncertain marker across internal
descriptor cleanup, including Linux source capture; CLI Start/Probe map it to
the release marker and preserve it even on probe cancellation. Fault regressions
reproduced the discarded marker before the fix. Windows full Go tests/build and
Linux native tests passed, including real first installation, upgrade, rollback,
stop refusal followed by a new admitted request, and failed-candidate restoration
with durable receipts and preserved device identity. journal-effects-linux.json
has cleanup_errors:[]. These runs use the controlled native verifier, not signed
product releases. The public CLI still does not use this admitted-owner path.

Next integrate the authenticated transaction process and IPC routing. Its final
receipt must consider the entire release return error: even a failure before
recorded effects can carry ErrCleanupUncertain and must not become Rejected.
During the serving-process replacement gap, heads need correlated read-only
operation observation and bootstrap must refuse an unfinished lifecycle record.
Do not expose mutation endpoints until those ownership/admission guards are wired.

Handoff authentication primitives now bind the operation ID/intent, selected
activation, profile/state digests and worker image. Native IPC peer identity is
queried from Windows named-pipe server PID or Linux SO_PEERCRED. Review exposed a
P1: matching parent/peer PIDs could still accompany false owner image metadata.
A regression reproduced acceptance of this test process claiming a different
selected image. AuthorizeOwnerHandoff now independently verifies the parent via
runtimeprocess.WatchImage: Windows binds a read/execute file handle to the original
process image section using ProcessImageFileMapping, then hashes the same pinned
file; Linux hashes /proc/PID/exe. The original process observer is checked again
after hashing, and selection is rechecked before authorization returns.

Windows original-image positive, identical-bytes/different-file negative, spoofed
parent-image refusal, full Go tests/build all passed. Native Linux IPC peer,
handoff binding/refusal and process-image tests passed; owner-handoff-linux.json
records cleanup_errors:[]. Static re-review confirmed the P1 closed within this
primitive's scope. This does not attest mutable process memory. The future caller
must retain the initial parent kernel observer through journal admission and use
the same IPC connection; no worker command or install/rollback routing is wired
yet, so this is not a completed owner takeover or signed lifecycle acceptance.

Owner transaction foundation now exists in internal/runtimeowner, separate from
heads and release activation. Admission retains a profile OS lock and writes a
SQLite receipt before effects. Request IDs survive terminal outcomes; duplicates
cannot obtain an executable transaction handle, and mismatched intent is refused.
Pending/unknown operations survive writer exit and block new admission. Each
effect is bounded and durably marked before dispatch; uncertain results and
write failures prohibit subsequent dispatch. Success requires a matching,
confirmed commit/ready pair; an applied start cannot be relabelled rejected or
cleanly failed without independently confirmed restoration.

A regression reproduced divergent independently stored active/outcome fields.
The active marker is now a SQLite generated column derived from outcome, with a
unique active-transaction index; record updates also compare the original bytes.
Windows full go test ./... passed. Native Linux journal tests passed, including
a real child exiting inside an effect after its intent is persisted, and owned
container cleanup_errors was empty (owner-journal-linux.json). This proves
process-exit behavior, not power-loss persistence. Independent static review
found no new actionable issue. The module is not yet used by public install/rollback
routing or exposed through IPC; the authenticated transaction-process handoff and
explicit reconciliation remain required, so the ownership gap below is open.

The existing-owner business path is shared: CLI/TUI use runtimehead admission,
and MCP admits its profile IPC session through runtime.ensure. The actual stdio
MCP smoke now compares Runtime PID, loaded SHA, revision and formats with CLI
observation, checks two concurrent CLI ensures retain that process, then checks
both heads reconnect to the same replacement after an explicit stop/ensure.
The current Windows and Linux 18-case cross-layer runs both passed with those
assertions and completed cleanup. Both use a controlled verifier and are not
signed acceptance. Evidence files are shared-owner-cross-layer-windows.json and
owner-cross-layer-linux.json under the non-gui-release evidence directory.

There is still a process-ownership gap: internal/cli/runtime.go directly calls
Store.Install/Store.Rollback, and CLI runtimeControl owns candidate Start/Stop.
Moving code into release has centralized the algorithm but has not moved the
activation transaction into the running owner. Do not count Task 2 or the full
Runtime-takeover goal complete on that evidence. The next ownership slice must
route explicit lifecycle requests through the owner and preserve transaction
authority across replacement of the serving process. Simply invoking the
current blocking Stop/Start implementation inside that same process would wait
for its own exit. It needs an owner-controlled handoff, bounded observation and
an authoritative result that heads can query after disconnection, without
replaying an uncertain request. The existing no-owner bootstrap exception and
strict original-baseline proof refusal remain in force.

Independent read-only lifecycle review confirmed that gap. Before implementing
the handoff, specify local lifecycle request correlation, durable acceptance,
lock/endpoint ownership, terminal observation and trusted recovery admission in
the IPC and owner contracts. These are local installation transactions, not new
Relay execution states. Keep one release algorithm; an internal transaction
role must be launched by the authenticated owner and survive the serving
process's replacement, rather than transferring process-control authority back
to the CLI. Required behavior tests for that slice:

1. Competing heads admit one lifecycle transaction; the rejected request never
   probes, stops or launches a candidate.
2. Disconnect after durable acceptance does not cancel or replay the accepted
   change; reconnect queries its correlated outcome.
3. The old serving process really exits before the candidate becomes ready,
   while its owner-controlled transaction role survives to commit and acknowledge.
4. Transaction-role death after stop, launch or commit retains the exact known
   stage; later entry never repeats an uncertain effect. A committed selection
   with a lost acknowledgement is distinguishable from an uncommitted change.
5. Missing IPC with an occupied lifecycle/service lock or unreleased task
   resources refuses automatic bootstrap consistently for every head; recovery
   requires an explicit request bound to the recorded transaction.

Linux cross-layer validation completed in an owned offline Debian container
with official Node v22.14.0, whose archive matched the official HTTPS checksum
list. The report pins fixture/helper hashes and records 18 passed scenarios,
cleanup_complete:true and cleanup_errors:[]. Earlier interrupted SSH transfer
containers were separately reclaimed; the intervening Alpine/ Debian loader
mismatch was a fixture failure before business tests. Those failed attempts do
not count as passes. No business container or production service was modified.

## Global Constraints
- Runtime is the only execution owner; heads never spawn candidates.
- Official Relay and production are read-only; tests use isolated profiles.
- Missing baseline proof refuses before stopping the old process.
- No compatibility dual protocol.
- No candidate retry after unknown side effects.
- Do not commit or merge without explicit user request.
- A no-owner bootstrap may start exactly one source-verified private snapshot, then all operations go through the owner IPC contract.

## Task 1: Freeze IPC contract
Files: docs/ipc.md and existing IPC contract tests.
Add runtime.ensure request/result/errors and idempotency rules. Run docs tests.

## Task 2: Activation owner
Files: create internal/release/activation.go; modify store.go, install.go, activate.go; add activation tests.
Write failing tests for private snapshot, original manifest, byte recheck, cleanup ownership and readiness failure. Implement owner activation. Run go test ./internal/release and ./internal/releasetrust.

Progress (local worktree, not final acceptance): installation preflight, rollback,
failed-start recovery and ActivePath now share selected-activation verification.
Six regressions failed before the change and pass afterwards. Managed baselines
retain their original activation ID; neither damaged cache nor healthy cache can
replace the selected execution authority. Unmanaged signed baselines are copied
from the authenticated quarantine snapshot. Execution-handle protection, durable
process ownership, uncertain cleanup and activation retention are still pending;
Task 2 is not complete and old signed lifecycle evidence does not cover this diff.

Process-exit/recovery progress: runtimeprocess now observes Windows process
handles and Linux pidfds. Stop observes status and sends stop on the same IPC
connection, waits for OS exit, and refuses absent IPC with a held lifetime lock.
CLI stop uses this confirmation path before reporting success. Failed-start
termination is bounded, uses at most one signal to the owned child, skips a
known exited process and requires both Wait and image-release evidence.
ErrCleanupUncertain preserves pending and prohibits automatic restore. Explicit
recovery retains the prior pending record on pre-dispatch refusal and retains
the uncertain candidate's identity until its stop is confirmed.
These changes have regression tests plus real isolated-process and production
daemon stop tests. They prove root-process behavior, not all task-tree cleanup.
Current validation: Windows go test ./... and go build ./... passed; native
Linux process-watch, stop and recovery-state tests passed 12 cases in owned
limited containers, cleanup_errors empty (exit-runtimeprocess-linux.json,
exit-cli-linux.json, exit-release-linux.json). The CLI stop test exercises the
public command's confirmation text after IPC closes but before the child exits.
Recovery regressions also preserve pre-existing pending state when stop is
rejected and retain the uncertain candidate ID until termination is confirmed.
Independent scoped review of the same-connection Stop, kernel exit observation,
bounded child cleanup and pending recovery branches returned no actionable
findings. That reviewer did not run tests; the execution evidence above remains
separate, and neither proves cleanup of escaped descendants or host crashes.

Owner observer lifecycle progress: source observations are admitted under the
daemon context and counted by ownerObservationWG. Shutdown cancels the context,
closes unread IPC replies to avoid a blocked writer, rejects new observations,
and waits for the actual external verifier process to return. Linux container
evidence (`owner-noderun-process-linux.json`) covers the real subprocess case;
Windows unit/daemon tests cover the same lifecycle. This prevents Runtime exit
while an admitted verifier remains, but does not add crash recovery or turn a
verification result into a cacheable launch license.

Task-tree shutdown progress: `process.Child` already supervises each foreground
and handoff child with a Windows Job Object or Linux process group. Runtime Stop
now observes the main process exit, then checks `jobs.json` under the job lock;
occupied/uncertain/corrupt/locked records prevent a clean-stop claim. The real
authenticated local Relay integration starts a supervised parent/child tree,
then verifies both process handles and empty durable occupancy before the CLI
prints success. A crashed owner still leaves occupied work uncertain by design;
no replay or automatic recovery was added. This proves only the supervised
boundary; deliberate setsid/job escapes and crashed host recovery remain limits.

Task-tree/ledger validation progress: Stop now holds the Runtime lifetime lock
while job.CheckReleased observes the job ledger under its OS lock. It refuses
occupied/uncertain/corrupt/locked state without deleting or recovering records;
reserved proves no process was launched and remains unchanged. Startup and this
observer share a bounded, ordinary-file decoder; Linux FIFO input is nonblocking.
Production process-tree integration uses a real local Relay with authentication
and leases, a separate production Runtime daemon, a supervised parent and child,
and kernel process handles opened while both are alive. On Windows and Linux,
the public stop command confirms success only after both tree members exit and
the occupied ledger becomes empty. Tests use isolated profiles and no official
Relay. Linux evidence: tree-cli-linux.json, tree-job-linux.json,
tree-runtimeprocess-linux.json; all passed, cleanup_errors empty.

Windows full Go suite initially hit an unrelated 3-second external Git startup
assumption in a handoff dedupe test. Its standalone run passed; the receipt test
now uses deterministic missing-workspace preflight failure, preserving both
dedupe/conflict assertions and strengthening the expected error check. Full Go
suite passed after that fixture change. This is not proof of crash/escape cleanup
or signed lifecycle acceptance; those remaining boundaries are unchanged.

Windows execution-boundary progress: internal/runtimeimage now checks the
caller's expected digest before Control.Check/Start execute anything. The retained
Windows read-sharing handle blocks writes/deletion/replacement and is inherited
by the child so a short-lived launcher cannot drop protection. Actual file
mutation and inherited-descriptor subprocess tests pass; the prior Start path
was reproduced executing a marker despite a mismatched expected digest. This
is not end-to-end bootstrap proof.

Linux execution-boundary progress: candidates are captured into sealed memfds,
hashed after sealing and executed through an inherited descriptor. Native
container tests cover inode seals, writes/resizing/mapping refusal, replacement
of the disk source, child lifetime, preexisting ExtraFiles and the actual loaded
image digest. Native CLI probe/refusal tests pass as well; containers were
removed with no cleanup errors. buildinfo now hashes /proc/self/exe on Linux;
release baseline matching relates its kernel image name to the selected source.
Other platforms still require protection. Final signed lifecycle acceptance is
not established by these test binaries.

Owner observation now derives readiness and activation ID from stable selected
state, source authentication and the actual running identity. Missing/corrupt
state, pending, proof denial and state changes during verification cannot report
ready. Concurrent observations are coalesced without caching successful proof;
the verifier runs off the IPC reader. CLI/MCP validate the complete ready reply.
CLI connection timeout/access failure does not authorize bootstrap.

Validation for this observation slice: Windows go test ./... and go build ./...
passed; MCP tests passed 40 with one Windows-inapplicable FIFO skip, and its
build passed. Native Linux release observation, Runtime IPC refusal/status and
CLI owner-response tests passed in isolated offline containers, with empty
cleanup_errors (owner-release-linux.json, owner-noderun-linux.json,
owner-cli-linux.json). Runtime IPC test profiles explicitly isolate their
workspace after the read-only container exposed a default-workspace fixture
assumption. Release proof injection in unit tests verifies control flow, not
cryptographic authenticity; final signed acceptance is still required.

Remaining integration requirements: the signed lifecycle/startup-fault fixtures must inspect
the actual kernel image separately from the selected on-disk manifest. Old
signed artifacts that cannot report their loaded-image hash must not trigger an
unsealed fallback. These remain part of Tasks 3 and 6, not waived acceptance.

Go bootstrap progress: CLI now delegates absent-owner selection to
release.Store.EnsureRunning, which authenticates an initial bootstrap before
installation writes/probing, or uses the selected activation and its manifest
digest. No digest is refreshed from the mutable launch path. It records pending
before its one Start, preserves pending on failure, never stops an appearing
owner, and requires source/identity readiness before success. CLI then asks the
owner over IPC as well. The previous unsigned-self bootstrap executed a real
marker in a regression; it is now refused before execution. Native Linux
bootstrap/reuse/refusal tests pass (bootstrap-release-linux.json and
bootstrap-cli-linux.json), with no container cleanup errors. The release tests
use a private proof fixture and do not establish a signed first-install chain.

MCP connection admission now bootstraps only on ENOENT/ECONNREFUSED and invokes
bootstrap at most once; timeout/access/busy/unknown errors do not authorize it.
MCP and runtime-bin discover only a native bootstrap and no longer parse managed
selection state; Go owns that selection. Both heads now call a shared bootstrap
launcher without receiving executable paths. Discovery enumerates untrusted
locations only; capture reads bounded regular files into a unique private copy,
preserves original manifest bytes, authenticates the copy and rechecks identity
before dispatch. The verifier excludes both original and private source trees.
Only absence at the initial candidate lstat permits advancing discovery; a
present candidate with missing manifest or later disappearance is a hard refusal.

The Node launcher retains the copy until child close (not merely exit or kill),
retains uncertain cleanup, signals at most once, never signals an observed exited
PID, bounds stderr, and unlinks only known owned files rather than recursively
removing substituted contents. Its timeout budgets native source verification
plus owner observation and never retries a candidate. Independent read-only
review found a missing-manifest fallback bug; it was reproduced and fixed with
a regression, then re-reviewed. Additional cleanup replacement/link tests pass.

Validation: Windows MCP tests 60 passed with two POSIX-only FIFO skips; native
Linux Node 20.20.0 ran 32 related cases in an owned offline limited container,
all passed, cleanup_errors empty (bootstrap-node-linux.json). Actual system gh
verified official v0.2.6 Windows Runtime source; the current local wrapper ran
version from its private copy and rejected four mismatched/tampered inputs
(mcp-private-signed-windows.json, five passed cases, cleanup complete). This is
version-only acceptance, not owner bootstrap or signed upgrade/rollback.

Protected bootstrap progress: the final pathname-to-spawn mutation was reproduced
with a real native process loading a different digest. A minimal native binding
(pinned Koffi 3.3.7) now holds a Windows deny-write/delete handle converted through
Node's own libuv descriptor table, or creates and seals a Linux memfd. The final
hash uses these protected bytes and the digest retained before source verification.
The child inherits fd 3; the parent retains its handle through close/uncertainty.
Linux argv0 carries only the matching metadata location; Go authenticates that
bundle again before activation. No version selection/state machine was added
to the binding. Other platforms fail closed in this Node bootstrap path.

Review additionally found that verifier exit zero did not bind the retained
digest to its signed subject (ABA). Both TS and Go now require bounded gh JSON
verificationResult.statement.subject[].digest.sha256 to contain the pinned raw
manifest/binary SHA. Duplicate subject names are allowed; raw attestation fields
are ignored. Native mock-gh tests reproduced the original acceptance and an
embedded bytes.Buffer ReadFrom limit bypass; the latter now uses a named buffer.
These test fixtures do not replace actual cryptographic evidence.

Current Windows MCP tests: 67 passed, two POSIX FIFO skips; build passed. Native
Linux Node 20.20.0: 39 related cases passed, cleanup_errors empty in
bootstrap-protected-node-linux.json. Parent-close inheritance is tested with a
data file rather than relying on the child's executable mapping. The real gh
and official v0.2.6 Windows wrapper version case plus four rejection cases passed
(mcp-protected-signed-windows.json). Final Windows go test ./... and CLI build
passed after the named-buffer correction; six docs tests and git diff --check
passed. Native Linux Go verified-subject tests also passed, including oversized
matching JSON, with no cleanup errors (verified-subject-linux.json). The current
Go verifier accepted the official v0.2.6 Windows binary and original manifest
using real gh and its original provenance bundle at 2026-11-12T12:36:06Z.
Signed owner bootstrap, explicit start,
activation retention/cleanup, other platform acceptance and the final signed
upgrade/rollback lifecycle still need completion; Task 4 is not fully accepted.

Activation cleanup progress: fresh staging now returns an in-memory creation
receipt for its directory and files. Under install.lock, cleanup rejects
current/previous/pending references, incomplete state, missing receipts,
observed replacements and unknown entries. It uses rooted, non-recursive unlink
and an empty-directory removal; Stage failure no longer calls RemoveAll.
Installation, no-owner bootstrap and unmanaged-baseline adoption reclaim fresh
unpublished snapshots after deterministic rejection. Confirmed candidate failure
can reclaim its copy after restoration; ErrCleanupUncertain and persisted
recovery references retain it. No historical directory scan/GC was added.

The regressions first reproduced selected-image deletion, deletion of foreign
contents/replacements and leaked rejected candidates/adopted copies. Windows
go test ./... and go build ./... passed; native Linux cleanup/state regressions
passed with cleanup_errors empty (activation-cleanup-linux.json).
Scoped review found one still-unaccepted concurrency boundary: SameFile and
pathname unlink/rmdir are separate operations. os.Root prevents traversal but
does not stop a writer bypassing install.lock from replacing a child between
check and deletion. This batch does not establish identity-atomic deletion or
complete historical retention/cleanup acceptance; keep that work open.

## Task 3: CLI owner path

Files: internal/cli/ensure_runtime.go, root.go and IPC owner handler; CLI/IPC tests.
Make ensure send runtime.ensure. Refuse missing proof before Check/copy/Stop. Preserve profile guard and no replay. Run CLI/IPC/release tests.

Owner capability prerequisite: direct public start still enters the service loop
and remains unfinished. Before separating that entry, candidates and recovery
baselines now report runtime_owner_protocol=1 in version/runtime build identity,
independent of Relay protocol and profile format. Missing/mismatched capability
is rejected by protected native Check and shared release preflight before a new
pending, Stop or Start. Installation, rollback target/current baseline,
restoration and both selected/fresh bootstrap use the same gate. Existing-owner
observation also refuses incompatible capability. This is not a compatibility
fallback and does not make a signed legacy bundle suitable for the new owner.

Tests reproduced missing/999 owner capability being accepted by native Check,
and an incompatible observation being reported ready. The lifecycle matrix
checks unchanged state/process on rejection; restoration after candidate failure
also preserves pending instead of starting an incompatible baseline. A real gh
official-v0.2.6 Windows test verified source successfully then observed preflight
refusal, absent identity/jobs/current state, and clean temporary-profile removal
(owner-preflight-signed-windows.json). This is refusal evidence, not the required
successful signed install/upgrade/rollback cycle.

Validation for this prerequisite: final Windows full Go tests and build passed;
native Linux CLI capability/healthy-probe and release lifecycle/restore refusal
tests passed with cleanup_errors empty (owner-preflight-cli-linux.json and
owner-preflight-release-linux.json). Six docs tests and diff whitespace checks
passed. Read-only review found no blocking code issue and its restore-coverage
gap was filled with an explicit regression. At this prerequisite checkpoint,
public start/foreground handoff was not implemented by the capability marker;
its subsequent implementation is recorded below.

Explicit-start implementation progress: public start now uses the same verified
selection as ensure and waits for an owned foreground launch. runtimeControl
spawns the hidden service role using an inherited, bounded pipe context (never
argv/env/persisted retry data). The child verifies its actual parent, profile,
transition digest, activation, loaded image and source before identity/IPC/Relay.
Parent-exit observation cancels source admission; successful owner lifetime is
independent of the head. The foreground child remains in a detached/hidden
process group with displayed output, preventing a new Windows console.

The daemon exposes diagnostics while pending but gates Relay and business on the
parent's matching current commit. Review found target doctor was incorrectly
treated as local observation; a real HTTP endpoint regression reproduced the
pre-commit connection and the handler now gates target doctor too. Local doctor
and stop remain usable. Native fixture tests prove pipe/parent wiring and actual
foreground exit; they deliberately bypass cryptographic admission and cannot be
called signed-owner acceptance. Store-level admission tests use a private proof
fixture, including changed parent/profile/state/image/proof and stale contexts.

Windows full Go tests/build passed after the target-doctor correction, followed
by focused tests with isolated profile/workspace settings. Native Linux CLI
pipe/foreground/refusal/task-tree checks, launch authorization/commit checks and
daemon business/Relay/doctor gates passed; all three explicit-start-*-linux.json
reports have cleanup_errors empty. The initial Linux daemon fixture failure was
an unset profile env causing default workspace creation under read-only /root;
the fixture now sets its own XALLOR_REMOTE_DATA_DIR before loading identity.
Review rechecked the doctor correction and found no remaining confirmed issue
in this slice. Final signed owner bootstrap/upgrade/rollback acceptance, ownership
of every lifecycle entry, and concurrent/historical cleanup remain open.

Abandoned commit correction: a real native parent launched a fully admitted
diagnostics-only child and exited without committing current. The regression
initially observed that child surviving; runtime-serve now retains the held parent
observer through pending and limits commit waiting. Selection is read before
parent observation and again before rejecting loss/expiry, preserving a commit
that raced observation. Pending state remains byte-for-byte unchanged on exit.
Committed owners remain independent, as exercised by the native lifecycle suite.

Review found a second race: daemon shutdown can return while a startup gate call
still uses the parent observer. launchCommitGate now serializes Check/Close,
rejects late checks and closes once; its blocking-check shutdown regression
passes. Admission waiter is joined before the gate takes ownership. Review
rechecked this correction without finding a remaining confirmed issue.

Validation: final Windows go test ./... -count=1 and go build ./... passed;
native Linux parent-loss, commit/deadline/error races, synchronized shutdown,
foreground start and lifecycle tests passed (launch-parent-linux.json,
cleanup_errors empty). One earlier Windows full-suite run reported owner absent
immediately after rollback. Three focused lifecycle repetitions and the final
full suite passed, but the original cause is unproven. Only gate error lines
(not credentials/banners) were added to the test diagnostic; do not label that
intermittent failure fixed merely from successful reruns.

Intermittent-failure investigation: native test binaries now record only
service-enter/service-return, PID, parent PID and timestamps in their isolated
profile. On a readiness failure, the test retains its failing assertion and
adds a second read-only IPC observation plus lifetime/occupancy observation;
it does not retry the command or convert a later observation into success.
Startup output is still limited to gate errors, never banners/credentials.
Windows go-winio v0.6.2 was inspected: its listener retains firstHandle until
closure, so ordinary connection churn alone is not evidence for an absent pipe.
Two full Windows test repetitions (go test ./... -count=2) and build passed after
instrumentation. The original rollback absence remains unexplained and open;
no production behavior was changed on an unverified causal assumption.

Native integrated lifecycle evidence: new CLI _test subprocess fixtures run the
actual Execute/version/start/ensure/runtime-serve/install/rollback code, protected
image loading, inherited admission, source verifier invocation, selected commit
gate and real daemon. Only gh is a controlled native test process in a separate
tools directory; it checks policy arguments and emits matching subject JSON.
Distinct executable overlays provide different test image digests, not signed
product releases. These fixtures prove mechanics, never source authenticity.

Windows integration passed public foreground start, second-start PID reuse,
native upgrade, rollback and deterministic candidate startup failure/restore.
Identity/grant hash, config and user file are checked at each transition; rollback
and restore retain the original activation ID. A 35-second fixture deadline
initially interrupted cold ensure during full-suite contention; the harness now
budgets 150 seconds for the existing bounded product flow and records stage
duration/context expiry without changing product timeouts. Full Windows tests
and build then passed; focused clean-up-reviewed tests passed again.

Review found failure cleanup could orphan a detached child. The foreground head
is now supervised by the existing process boundary (Windows Job Object);
failure reaps it first and then performs bounded isolated-profile stop/absence
checks, surfacing cleanup uncertainty. Linux detached services rely on the
enclosing owned container PID namespace, not an asserted process-group guarantee.
No production trust bypass or environment-selected mock was added.

After the cleanup correction, final Windows full Go tests/build passed. Native
Linux reran both integrated tests using Node-independent compiled Go fixtures
inside an owned offline 1-CPU/1-GiB/128-PID container; all passed and cleanup_errors
is empty in native-owner-cycle-linux.json. The test image SHA is recorded there.
Six documentation checks and git diff --check passed. #74 remains open: these
controlled-verifier tests do not meet its real signed-version acceptance.

The prior temporary Go SDK disappeared between runs. Official go1.25.3
windows-amd64 was restored under the evidence directory's toolchains/go1.25.3;
archive SHA256 bc249a599c6fe9d0d4093c363856f6c6320dbbe05e5d5d8818b711fb4a14fc23
matched go.dev metadata before extraction. Do not depend on the deleted Temp
SDK path in future validation.

## Task 4: MCP owner path

Commit acknowledgement correction: deterministic regressions showed lifecycle
operations returning immediately after current was saved without a child ack,
and runtime.ensure returning ready without latching startup admission when the
background poll had not run. A subsequent pending transition can then be
misinterpreted by the previous child's startup gate. Control.Ready is now
mandatory after current save and before install.lock is released for install,
rollback, restore and fresh/selected bootstrap. Production Ready uses owner IPC;
the server latches startup admission before replying ready. Missing ack returns
an error and preserves the committed state/process, without replay or restore.

The new transition/lost-ack/delayed-poll regressions passed. Native Linux lifecycle,
foreground and acknowledgement tests passed with cleanup_errors empty in
commit-ack-{cli,release,noderun}-linux.json. Read-only review found no lock cycle
or confirmed new defect. Windows focused lifecycle repeated five times passed.
However a full-suite run still observed a missing endpoint after cold ensure;
the new ack gate correctly returned failure rather than success. Therefore the
deterministic ack gap is corrected but does not fully explain the older sporadic
owner disappearance. Test-only failure diagnostics now include process events
and gate errors at command failure as well as later observation failure.

Current verification limitation: the latest full Windows run did not pass.
After the cold-ensure absence above, another full run passed the native owner
cycle but failed TestStop_should_reclaimAuthenticatedTaskTree_beforeReportingSuccess:
an already-held descendant process watch was not signalled immediately after
public Stop reported success. Ten focused repetitions then passed. Build passed,
but neither reruns nor compilation explain the failure. Preserve this distinct
shutdown-observation failure for investigation; do not relax its immediate
post-success assertion or label the full suite green. The scoped ack regressions
and Linux reports remain valid only for their stated scope.

Shutdown failure subsequently reproduced at the Windows process boundary:
the pre-existing descendant test granted an extra 2s wait after Child.Wait
returned. Removing that grace made all 100 repetitions fail with WAIT_TIMEOUT.
The supervisor now snapshots cumulative Job population, captures read-only
member handles before termination (confirming membership), terminates the Job,
then requires accounting zero AND signalled captured handles within the existing
cleanup budget. Handles stay held until cleanup, preventing PID reuse. Failures
still attempt Job termination and propagate ErrCleanup; no PID-based kills added.

Review identified children born after enumeration as an uncaptured boundary.
The first cumulative TotalProcesses observation is retained across repeated
Stop and final reaping; any change prevents cleanup success. A gated native test
creates a descendant after that snapshot and verifies refusal, then confirms its
test process exits. That test and immediate descendant-exit test each passed
100 repetitions; public Runtime task-tree Stop also passed 50 focused repetitions.
This addresses the reproduced early-success mechanism. It does not prove
independent exit-handle observation of historical members already removed before
the first snapshot, nor explain the separate sporadic owner disappearance.

Final population-fenced full-suite run remains failing: public task-tree Stop
returned ErrCleanupUncertain because the task ledger retained unconfirmed
resources. It did not report false success. Process package tests and both
100-repeat regressions passed; reviewer found no confirmed flaw in the fence.
The reason that this integrated run retained uncertainty is not yet diagnosed.
Do not remove the fence, clear the ledger, or call the full suite passing.

Further diagnosis: local Runtime logging now records supervisor cleanup/release
errors without commands, output, credentials or scope; the task-tree test captures
only that diagnostic line on failure. A 100-repeat integrated run reproduced
population-fence failures; a 30-repeat follow-up with numeric counters showed
TotalProcesses changing from 3 to 4. Thus this failure is not the held-handle
wait timeout. The newly counted member/association has not yet been identified;
Windows cumulative job accounting can also include failed limit associations.
The fence remains fail-closed. No ledger clearing or assertion relaxation was
used, and the complete Stop path is still not accepted as passing.

Stable-tree fixture correction: a new opt-in completion-port diagnostic is
attached only to a test-owned Job before launch. It logs process IDs and image
basenames, no command/environment/credentials. With the same child creation
flags used by the CLI fixture, it observed two process.test.exe members and two
conhost.exe members (total 4). The original task-tree fixture announced readiness
immediately after child.Start, before the child's body ran. It now waits for
that child's existing heartbeat before announcing an established tree. The
post-Stop process-handle checks remain immediate; production population fencing
and the separate concurrent-birth refusal test are unchanged.

After this fixture correction, public Stop passed 100 repetitions and the final
Windows full Go test suite and build passed. Read-only review accepted the
fixture boundary. The separate job trace supports a late console-host explanation
for the prior 3->4 count, but does not directly identify the fourth member of the
original failed run. It also does not promise future OS helpers cannot appear:
those still produce conservative cleanup uncertainty. The earlier false-success
bug was fixed by held exit handles, not by this readiness handshake.

Files: packages/mcp/src/index.ts, runtime-bin.ts, ensure.ts, source-trust.ts and tests.
Remove direct candidate spawn. ensureRuntime calls fixed IPC and owner. Add replacement attack regression. Run npm test and npm run build.

## Task 5: TUI/shared head helper

MCP admission deadline correction: Go already bounded owner handshakes, but Node
could leave a connected silent owner's shared initialization pending forever.
A real IPC regression observed that missing deadline. RuntimeIpc.call now accepts
an optional validated deadline, rejects/clears pending waiters and closes on expiry,
and clears timers on response/disconnect/write failure. ensureOwner and the initial
status check use 65 seconds; streamed execution does not inherit that deadline.
Timeout is observation uncertainty, not permission to replay bootstrap/business.
Windows MCP: 76 cases, 74 passed/two POSIX skips, build passed. Scoped review
found no actionable defect; disconnect and late-write cleanup were inspected
statically rather than separately exercised by the new tests.
Offline Linux Node tests including the real IPC timeout/stream cases passed
58/58 with cleanup_errors empty (bootstrap-protected-node-linux.json).

MCP session admission progress: the shared session factory now coalesces
concurrent initial/reconnect callers, returns only an admitted connected IPC,
and retains a single bootstrap attempt's success or failure for the MCP process
lifetime. Later tools may re-observe a repaired IPC endpoint but never replay
bootstrap or business calls. Every new connection still runs owner identity
and status validation. Red tests reproduced eight independent initializations
and a second bootstrap after uncertainty; corrected tests include shared refusal,
external recovery, post-success owner loss and closed-at-admission connection.
An actual local IPC test validates one handshake for eight concurrent callers
and a fresh handshake on disconnect/reconnect without a launch.

Windows MCP validation: 72 cases, 70 passed and two POSIX FIFO skips; build,
six docs checks and diff check passed. Review found no actionable defect in
the scoped factory/index wiring. Its real IPC test covers transport plus owner
validation; index status validation and optional peer.add were reviewed but not
executed by that test. No new installation or process state machine was added.
Offline Linux Node 20.20.0 ran all 48 related cases successfully, including the
real Unix IPC admission/reconnect test; bootstrap-protected-node-linux.json
records cleanup_errors empty.

MCP obsolete-selector removal: usage search confirmed production index/runtime-bin
already use bootstrap-launcher and owner IPC, while ensure.findBinary,
bundledBinary and managed-runtime remained exported/shipped dead implementations.
Their allowUnverified path-return option and direct versions/state parsing have
now been removed, along with tests specific to the retired implementation.
Current source-proof, captured-bootstrap and Go owner selection tests remain.
ensure.ts contains endpoint/profile/layout helpers only.

Two boundary regressions first failed on the exported selector and stale emitted
managed-runtime.js. Build now runs tsc then unlinks only explicitly named obsolete
JS/declaration/map outputs; it checks dist is an ordinary directory before/after
compilation and does not recursively clear user-selected paths. Tests check both
exports and delivered output. Windows MCP: 65 cases, 63 passed/two POSIX skips;
build passed. Offline Linux Node: 41 related cases passed, cleanup_errors empty
(bootstrap-protected-node-linux.json). npm pack --dry-run --ignore-scripts listed
71 files and zero obsolete selector modules; this is a packaging inspection,
not a published or signed package. Documentation checks and diff check passed.

Files: TUI command/shared IPC client and tests.
Use shared owner client and structured status/errors. Run TUI tests.

Connection-boundary progress: internal/runtimehead now owns the Go head's
readiness handshake. CLI ensure, CLI/TUI business RPC and execution streams,
and CLI approval subscription use it. Every business connection confirms the
connected owner before dispatch; refused or malformed readiness closes that
connection, without retry/bootstrap. The same framed reader survives the
handshake and its deadline is cleared for streams. Status diagnostics and
explicit Stop/recovery remain available without ready. JSONL preserves owner
error codes at both the command's initial ensure and subsequent stream connection.

Regression evidence: real isolated IPC peers reproduced pre-readiness grant and
exec dispatch in both heads; a command-level test separately reproduced loss of
runtime_pending_recovery. Both are corrected. Windows focused tests and native
Linux CLI/TUI connection tests pass, including same-connection success, stream
output/terminal and diagnostic bypass. Linux evidence is shared-head-cli-linux.json
and shared-head-tui-linux.json in the non-gui-release evidence directory; both
report cleanup_errors empty. Shared malformed-response tests pass on Windows.
Scoped review found the command-level JSONL mapping defect above; it is fixed
and the reviewer verified the correction. Final Windows go test ./... and
go build ./... passed after the correction; six documentation checks passed.
These IPC-peer tests do not establish signed end-to-end lifecycle acceptance.

## Task 6: Signed lifecycle evidence
Files: tests/signed_upgrade_e2e.py, release workflow and release docs.
Run Windows/Linux signed install, upgrade, rollback, readiness failure restore and replacement attack. Keep online, bundle and dirty evidence separate.

Acceptance-harness alignment: signed_upgrade_e2e now resolves current activation
from validated state, checks its retained binary/raw original manifest separately
from the loaded image, and checks state stability across observation. On Linux,
runtime_image_observation holds a pidfd while checking /proc/PID/exe digest,
required seals, diagnostic memfd label and exact internal service arguments.
The missing-manifest negative case remains explicit and is not source success.
Windows reports retained-file/report checks without claiming independent pidfd
evidence. Stop failure now prevents recursive test-profile cleanup.

The Linux startup-failure injector now finds the pending activation, checks the
actual installer parent PID and sealed service image, then waits on the held
pidfd. It reads only new startup-log bytes, not historic SQLite errors; it no
longer reads process environment credentials or assumes a versions-cache path.
MCP signed-vendor version checks accept the real Linux memfd report but still
require its loaded SHA; old binaries that cannot report that identity fail.
The container signed-cycle runner includes the new helper module.

Windows Python suite: 32 cases, 30 passed and two Linux-specific skips. Native
offline Linux observer tests: six passed, one Windows-only skip, cleanup_errors
empty in image-observer-linux.json. Native tests exercise sealed/unsealed Python
images, actual descriptor lifetime, disk replacement, wrong-parent and service
role rejection. They validate the observer, not a signed Runtime upgrade.
Read-only review found no new confirmed defect in the harness slice. Script
import/--help/bytecode and whitespace checks passed. The full signed cycle and
database-lock injection have not been rerun against owner-capable signed
candidates, so Task 6 remains incomplete.

## Task 7: Completion audit

Linux diagnostics and cross-layer migration: owner mechanics can explicitly use
a supplied precompiled _test fixture for an offline container; self-reported
identity is checked only for fixture compatibility, not source authenticity.
The runner cross-compiled current cli tests, recorded the binary/script hashes,
and ran real Python Cluster/CLI/Runtime/Relay diagnostics in a dedicated offline
1-CPU/1-GiB/128-PID container. Seven scenarios passed; profiles, Relay and container
cleanup completed (owner-diagnostics-linux.json, cleanup_errors empty).

cross_layer_e2e now has an explicit --owner-mechanics mode using that same
builder. The current Windows run passed all 18 original scenarios including
real stdio MCP and Runtime restart, task-tree cleanup, grant rotation, Relay
loss/reconnection, stream terminals, workspace and file boundaries. The report
wraps scenarios in evidence with cryptographic_acceptance:false/fixture SHA and
is written only after successful scenarios and cleanup. Its PR CI step explicitly
selects mechanics; no other assertions or CI jobs were removed. Linux cross-layer
execution and remaining fixture consumers are still unverified/unmigrated.

First positive CI fixture migration: diagnostics_e2e accepts explicit
--owner-mechanics/--go, constructing a native Go _test executable and separately
located controlled gh under its owned root. Existing test-only dispatch runs
the actual CLI/owner/runtime/Relay; production binaries have no such flag/env
bypass. Manifest identity is fixture data, and the exported report records
cryptographic_acceptance:false, fixture SHA and cleanup_complete. Ordinary
Cluster mode strips inherited test-dispatch/profile/target credentials and only
explicit fixture wiring adds the test mode. Windows subprocesses remain hidden.

The current-source Windows diagnostics run passed seven cases: absent-profile
read-only observation, unpaired peer, denied auth, target info, inbound denial,
target offline and Relay transport loss. Cluster cleanup completed. Python
suite:39 cases,37 passed/two Linux-only skips; bytecode/diff checks passed.
pr-check's diagnostics step is now explicitly named native owner mechanics;
other integration/handoff (and excluded GUI) fixtures are not yet migrated.
Linux execution of this new Python builder/cluster path has not been performed;
earlier Linux native Go owner evidence is narrower. Do not claim full CI passed.

CI contract audit identified an unclosed integration dependency: pr-check's
integration/release-lifecycle/handoff jobs build unsigned local binaries, while
Cluster.add calls public ensure. Those starts now correctly require provenance,
so local Go/MCP greens do not imply the PR workflow can pass. GUI uses that same
fixture too but GUI implementation remains outside this work. Do not remove CI
steps or add a production trust bypass to conceal this mismatch.

The independent release_source_gate_e2e was unnecessarily starting an unsigned
cluster despite its standalone-verify scope. It now creates a static isolated
profile and asserts exact unchanged files/no new directories/no candidate
execution for all three refusal cases. Actual current-source Windows CLI run
passed all three; report explicitly says running_service_preservation not
exercised. --go selects the pinned SDK without altering verification PATH.

A new cleanup regression proved shared Cluster.stop ignored nonzero uncertainty
then accepted absent IPC; corrected stop fails and Cluster.close retains the
profile. Python suite: 33 cases,31 passed/two Linux-only skips; bytecode checks
passed. Broader positive CI cluster migration remains open. Also identified,
not yet fixed: release_install_source_e2e's separate TemporaryDirectory cleanup
can erase evidence when its stop/cleanup fails; it needs a retained owned-root
lifecycle before future signed positive runs. No CI or GUI checks were disabled.

Installer evidence-retention correction: release_install_source_e2e now uses an
explicit InstallWorkspace lifecycle instead of TemporaryDirectory's unconditional
exit deletion. Stop must succeed and subsequent status must report no owner;
unknown cleanup retains all profiles and attempts remaining owners. Primary
assertion failures also retain files with a cleanup report and original exception.
Successful deletion validates owned absolute temp root/marker/no redirects and
the success JSON is written only after cleanup. Regression first reproduced
uncertain stop followed by absent status deleting evidence, then passed along
with invalid-status, multiple-profile, primary-failure and clean-success cases.

Python suite: 38 cases,36 passed/two Linux-only skips; current-source Windows
production CLI passed the two installer negative checks (no signed candidate
supplied), with workspace cleanup complete. Scoped review found no confirmed
new issue; no production authentication bypass or positive signing claim added.

Run git diff --check, pinned Go tests, npm tests/build, Python docs/release tests and replacement probes. Record exact HEAD, dirty state and cleanup. Do not mark complete while GUI-independent acceptance is missing.
