---
name: xallor-remote
description: Use the XallorRemote CLI through its shared local Runtime to inspect devices, execute commands, transfer bounded file content, observe processes, and handle explicit handoff results. Use when a user asks an Agent to connect to or operate an XallorRemote device.
---

# XallorRemote Agent workflow

This skill is a thin workflow layer over the installed `xallor-remote` CLI. The
CLI and MCP are heads of the same Go Runtime; do not create a second protocol,
connect to Relay directly, or treat GUI state as authority.

The wire and IPC shapes are defined in [`docs/protocol.md`](../../docs/protocol.md);
this skill only explains how an Agent should use the current heads.

## Safety rules

- Never print or persist real grants, device secrets, account credentials or
  tokens. Use placeholders in examples.
- Do not enable inbound, issue/rotate grants, revoke, reset, install, rollback,
  or change target configuration unless the user explicitly requested it.
- Do not retry an operation after an unknown or missing terminal. Query status or
  the original operation first; never replay a side effect by default.
- A successful `status` or `doctor` response is not proof that execution is
  authorized or that a target is ready. Check Runtime readiness and the final
  execution terminal.
- `native_user` is the only available backend and has `sandbox:none`; do not
  describe Job Objects/process groups as a security sandbox.

## First checks

```text
xallor-remote version --json
xallor-remote status --json
xallor-remote doctor --json
```

If Runtime is absent, use `xallor-remote ensure`. If it reports pending
recovery, source refusal, or uncertain cleanup, stop business operations and
report the state. Do not switch to another binary or candidate.

## Add and inspect a target

```text
xallor-remote peer add --device <9-digit-device-id> --grant <placeholder>
xallor-remote peer list
xallor-remote doctor --device <9-digit-device-id> --json
```

With multiple peers, always pass `--device`. A peer record is local saved
access, not proof of a human identity. The target Runtime remains the authority
for OS, shell capability, workspace and backend.

## Execute safely

First inspect target info through the CLI/MCP info operation and choose a shell
the target reports as available. Never infer the target shell from the control
machine OS.

For structured or complex input, write a local UTF-8 request file and use:

```text
xallor-remote exec --device <id> --request-file ./execution.json --jsonl
```

The request file preserves empty arguments, Unicode, quotes and argument
boundaries. It belongs to the control machine; `cwd` belongs to the target
workspace. Script and program modes are mutually exclusive.

For a short command:

```text
xallor-remote exec --device <id> --shell <target-shell> --jsonl -- "<complete script>"
```

For a program with exact arguments:

```text
xallor-remote exec --device <id> --jsonl --program <target-program> -- <arg1> <arg2>
```

Read stdout JSONL until the matching `exit` or `error` terminal. Distinguish
non-zero exit, policy refusal, cancellation, timeout, authorization loss,
resource uncertainty and result unknown. Only a completed terminal with the
required exit code is success. Stderr text is diagnostic, not a terminal.

## Files, processes and handoff

Use the existing `read`, `write`, `processes` and handoff commands/tools with
their bounded workspace rules. A truncated preview must never overwrite a full
file. Cancellation requests a stop; it does not prove the target process or
resource has been reclaimed. Handoff receipts describe submission/query state,
not an interactive terminal unless the target contract explicitly says so.

## Approval and access changes

High-risk commands require a subscribed local approval head and now expose a
complete immutable operation detail plus `request_sha256`. An approval response
must include the same digest; never approve from a shortened preview. If the
last approval subscriber disconnects, the pending request is cancelled.

The current CLI path still uses the v0 reusable bearer credential until
rotation, revoke or ownership invalidation. One-time, expiring and long-lived
code semantics have an internal WIP state machine and Relay/Runtime design, but
the supported CLI/production modes are not yet implemented; do not claim those
modes exist.

An experimental non-GUI `xallor-remote session request` entry now routes through
the local Runtime IPC. Use it only when explicitly requested, provide the code
through `XALLOR_REMOTE_ACCESS_CODE`, and treat rejected, pending, expired or
unknown results as non-success. It is WIP and does not establish a stable
production authorization-code contract.

The MCP `remote.session_request` tool follows the same boundary and reads the
same environment variable; it never opens a Relay connection itself.

## Evidence boundary

This skill describes the current CLI contract and its non-GUI WIP. It does not
claim a published signed Runtime, production Relay deployment, commercial
entitlements, multi-Relay HA, historical quarantine deletion, OS sandboxing or
complete TUF update trust. Check the repository's release and Runtime status
documents before treating a candidate as deployable.
