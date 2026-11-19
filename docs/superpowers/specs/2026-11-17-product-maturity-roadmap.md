# XallorRemote product maturity roadmap

Status: architecture review complete; conservative product defaults are selected
below. This is a staged plan, not a claim that future capabilities are already
implemented.

The current repository has a strong Runtime ownership and fail-closed execution
foundation, but it is not yet a generally available hosted product. The mature
path is staged so that release trust, single-region operation, commercial
services and high availability are not mixed into one unreviewable change.

## Current release decision

The product owner has approved:

- creating real protected tag/release candidates;
- allowing draft candidates before the full lifecycle is complete;
- requiring one human reviewer for normal publication, without a two-person
  requirement for routine releases;
- owner approval for trust-root revocation, rotation and offline recovery.

The release workflow must still enforce protected immutable `v*` tags and a
reviewed release environment. Manual branch runs are dry-run artifact checks;
they must not obtain official artifact provenance.

## Recommended stages

### S0 — close the current boundary

Keep `native_user` as the only execution backend, retain unknown jobs and
unowned quarantine objects, and freeze the Runtime/IPC/control-data contracts.
Finish release governance: protected tags, one reviewer, immutable candidate
manifest, fixed workflow/run IDs and reproducible evidence. Do not claim
production release while the worktree is dirty or the candidate is draft.

Jobs and source/quarantine cleanup are separate safety domains: `occupied` or
`uncertain` jobs are never released from absence alone, and historical source
snapshots without a durable ownership receipt are retained. A Runtime journal
can resolve an install/rollback request; it cannot prove that an old OS process
stopped or authorize deletion of an old quarantine directory.

### S1 — single-region operable product

Keep one Relay authority and a single-writer durable registry. Add an independent
management boundary (it may start as a separately authenticated listener in the
same deployment) with OIDC Authorization Code + PKCE, principal/membership
mapping, device ownership/share RBAC and structured management audit. Runtime
remains the only execution owner; the data plane does not persist command or
output payloads. Add a new profile-owned quarantine namespace and explicit
operator reconciliation, while retaining unresolved historical material.

### S2 — commercial operation

Add a versioned entitlement snapshot/PDP, atomic commercial leases and a durable
usage outbox/aggregator. Keep usage observations separate from append-only/WORM
audit. Define fail-closed versus a bounded grace policy, retention/export/delete
rules and the treatment of unknown outcomes before enabling billing.

### S3 — enterprise availability

Add durable ownership epochs, lease/fencing tokens, replication and a region
authority. Failover must not take over old in-flight side effects: post-dispatch
operations remain unknown; only pre-dispatch work with an explicit safe proof may
retry. Validate RPO/RTO and split-brain behavior before multi-region rollout.

### S4 — high-risk isolation

Implement least-privilege backends independently per OS (for example restricted
Windows tokens/AppContainer and Linux DynamicUser/cgroup/Landlock/seccomp or a
container runtime). Each backend needs its own workspace, network, resource and
cleanup contract. This can remain deferred while the initial trust boundary is
an explicitly authorized same-user machine.

## Selected product defaults

1. **Initial trust boundary:** the first release targets an explicitly
   authorized user controlling their own or explicitly shared devices. Hostile
   multi-tenant shared hosts are outside the first release and require S4
   isolation before support.
2. **Deployment promise:** the first release is single-region hosted or
   self-hosted, without commercial billing and without an HA SLA. Enterprise
   SSO, billing and multi-region failover are later stages.
3. **Historical quarantine:** new resources use a profile-owned private
   namespace and explicit reconciliation; historical objects without durable
   ownership proof remain retained. No historical deletion authority is enabled.
4. **Release metadata:** use TUF-style signed metadata (root/targets/snapshot/
   timestamp) or an equivalent design; stable publication requires one human
   reviewer, root rotation/emergency revocation uses a 2-of-3 offline root, and
   offline break-glass is diagnostic-only (never install/start).

The current WIP contains only an offline-verifiable metadata primitive in
`internal/releasetrust`: threshold Ed25519 signatures, root cross-signing,
expiry/revocation/rollback checks, and target digest binding. It is not wired to
install/bootstrap or a key store. The first production metadata rollout must
publish immutable metadata and evidence for all four roles, then run online and
offline verification from the clean protected-tag candidate.

PKCE, fencing, lease shape, database/message transport and OS-specific sandbox
mechanics are engineering decisions within these boundaries and do not need
separate product votes.

## Evidence boundary

Artifact provenance proves build origin only after a consumer verifies the
attestation against a policy. GitHub documents that artifact attestations do not
by themselves guarantee artifact safety and that offline verification requires a
bundle plus trusted roots; root freshness and revocation remain operational
responsibilities. A future update system should therefore use TUF-style signed
metadata (root/targets/snapshot/timestamp) or an equivalent design with expiry,
monotonic versions, revocation and rollback protection. This roadmap does not
claim those controls are implemented.
