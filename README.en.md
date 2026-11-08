# XallorRemote

[中文](README.md)

**XallorRemote** runs one Node on each computer. A device ID and grant code let you send tasks through a Relay to a Windows or Linux computer and stream the results back. Either computer can control the other. No public IP or SSH is required on the target.

Governance gates: #57, #58, #59, #60, #73, and #74.

Start with the product documentation in [docs/README.md](docs/README.md). Contributor rules and PowerShell guidance are in [AGENTS.md](AGENTS.md). See [docs/dataplane.md](docs/dataplane.md) for the Relay, and [docs/heads.md](docs/heads.md) / [docs/stack.md](docs/stack.md) for clients and the technology stack.

The default hosted Relay is `wss://api.xallor.com/remote`. The preferred installation is **one npm package**, `xallor-remote-mcp`, which includes the Runtime; see [docs/mcp.md](docs/mcp.md) / [docs/stack.md](docs/stack.md).

First verify the release source as described below, then install the verified tgz. Do not use `npx -y xallor-remote-mcp` before the package is published to the npm registry:

```text
npm install -g ./xallor-remote-mcp-<version>.tgz
```

The shortest shared-grant setup after installation starts on the target computer. `grant issue` creates a new code and also enables inbound access; running it again replaces the code. Use `grant show` to view an existing code. Share the code only with the person or Agent that should control this device.

```text
xallor-remote grant issue
xallor-remote status --json
```

On the controller, enter the target's actual 9-digit device ID and grant code, replacing the angle-bracket placeholders:

```text
xallor-remote peer add --device <device-id> --grant <grant>
xallor-remote exec --device <device-id> --program whoami
```

These commands ensure that the local Runtime is running in the background; a separate `start` is unnecessary. `start` runs continuously in the foreground, whereas `ensure` idempotently ensures a background Runtime. Both computers must use the same Relay; for self-hosting, configure the Relay URL on each computer as described below.

Agents should use `--json` for queries and `--jsonl` for execution, checking the process exit code and the final exit/error event. For complex input, use a local UTF-8 `execution.json` file and argument arrays to preserve empty arguments and Unicode without nested PowerShell/Linux quoting. `--` does not prevent the local shell from expanding input first. For example:

```json
{"program":{"executable":"git","args":["status","--short"]},"timeout_ms":30000}
```

```text
xallor-remote exec --device <device-id> --request-file ./execution.json --jsonl
```

The program and working directory belong to the target; the request file is local. Request-file mode cannot be combined with `--shell`, `--cwd`, or `--timeout-ms`. With `exec --jsonl`, unknown flags, missing values, and invalid value types also produce exactly one `invalid_execution` error on stdout, exit 1, and retain stderr diagnostics without starting Runtime. JSONL can appear before or after the bad flag; literal arguments after `--` do not enable it, and `--jsonl=false` disables it. `--help` remains plain help. An Agent must not treat a missing terminal event as success. PowerShell 5's default `>` produces UTF-16, so do not use it to create UTF-8 request files. See [docs/mcp.md](docs/mcp.md) for MCP tools, configuration, and streaming, [docs/cli.md](docs/cli.md) for current CLI usage, and [docs/protocol.md](docs/protocol.md) for machine-readable fields.

Target capabilities come from the target Runtime's `info`; `remote.device_info` returns the target `shell`, `execution_modes`, and `execution_backend`. The current backend keeps the target user's privileges and reports a Job Object/process-group cleanup boundary on Windows/Linux with `sandbox: none`. The controller must use the target capability result instead of guessing from its own OS, and must not treat cleanup supervision as a least-privilege sandbox.

`xallor-remote policy --json` and the target `info.operation_policy` return the same operation matrix for CLI, MCP, and Agents; the minimal GUI can reuse the same field after its local IPC integration. It describes static boundaries; Relay still revalidates device access, member/tenant authorization, grants, inbound state, and versions on every call.

Account and tenant boundaries are documented in the [OIDC/SSO boundary](docs/oidc.md). The CLI currently uses Relay-operator-issued local credentials and does not run browser login; an OIDC provider token cannot be used directly as a grant or tenant authorization.

Use `xallor-remote device lifecycle` to inspect enrollment, claim, transfer, revoke, and reset preconditions and uncertainty handling. It is a contract report and does not remotely mutate device ownership.

Use `xallor-remote entitlement contract` to inspect feature/value boundaries. It does not read balances, and Relay inflight/IP limits or local job capacity are not treated as membership entitlements.

Use `xallor-remote usage contract` to inspect privacy-minimized usage event and unknown/idempotency rules. Local activity history is not tamper-evident audit and does not directly create billing.

For local development, run a Relay in one terminal:

```text
go run ./cmd/xallor-remote relay --listen 127.0.0.1:8443
```

Open another PowerShell terminal for the local Runtime:

```powershell
$env:XALLOR_REMOTE_RELAY_URL="ws://127.0.0.1:8443"
go run ./cmd/xallor-remote start
```

The equivalent Runtime command for Linux sh/bash, also in a separate terminal:

```sh
XALLOR_REMOTE_RELAY_URL="ws://127.0.0.1:8443" go run ./cmd/xallor-remote start
```

A second Runtime on the same computer needs a different `XALLOR_REMOTE_DATA_DIR` and the same Relay URL. Across computers, first configure a secure self-hosted endpoint as described below, use the same reachable URL on both computers, then run `grant issue` / `peer add` / `exec --program whoami`. Current CLI usage is in [docs/cli.md](docs/cli.md).

The cleartext endpoint above is for local development only. For public self-hosting, use `relay --listen :8443 --tls-cert <PEM> --tls-key <PEM>` with `wss://`, or configure a trusted HTTPS reverse proxy. An unprotected non-loopback listener will fail to start. Proxy trust, the browser Origin allowlist (including IPv6), and health checks are documented in [docs/relay.md](docs/relay.md#5-运维).

For version checks and local upgrades, first follow [release source verification](docs/release-security.md) to verify the downloaded attachments against the expected tag and commit. `runtime install`, upgrades, and rollback reverify raw binary/manifest provenance before using an installed version; MCP vendor/PATH selection applies the same fixed policy to candidates. A real signed-vendor positive path still needs release-CI confirmation, so #74 remains in progress.

A separate read-only source verification command uses an independently trusted system GitHub CLI and a fixed signer policy. It does not execute candidates or install them, and rejects unsigned/development builds by default. Install, rollback, and MCP vendor selection reuse the same verifier, but its successful output is not an installation credential; later directory changes or bypassing the command remain unprotected. Offline trust-root rotation/revocation and a real signed-vendor positive path remain incomplete; #74 remains incomplete. See the [installation trust design](docs/install-trust-design.md) for trust anchors and recovery limits.

```text
xallor-remote runtime verify --bundle <bundle-dir>
```

```text
xallor-remote version --json
xallor-remote runtime install --bundle <bundle-dir>
xallor-remote runtime status
xallor-remote runtime rollback
```

Installation restarts the local Runtime while preserving the device ID, grant, connection settings, and inbound setting. Finish running tasks first. On failure, it automatically restores the previously managed version. When migrating from 0.1.x for the first time, stop the old service before installing the new package. Build and release instructions are in [docs/stack.md](docs/stack.md).

To investigate connection problems:

```text
xallor-remote status --json
xallor-remote doctor
xallor-remote doctor --device <device-id> --json
```

Diagnostics only read state: they do not start the service or change authorization or inbound settings. Follow the suggested recovery steps and retry. “Unconfirmed” means a complete response was not received.

`<bundle-dir>` is the directory extracted after verification; `<device-id>` is the target's 9-digit device ID. CLI help, parameter consistency, and native shell validation across platforms still need work; see the [#73 review and remaining work](docs/cli-ux-review.md). Aligning the bilingual READMEs does not complete all of #73.
