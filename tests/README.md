# Cross-layer verification

All process tests use owned temporary profiles and a loopback Relay. They never connect to the user's profile or official Relay. Cleanup stops only fixture processes, verifies the owned absolute directory and then removes it; failed cleanup retains evidence.

- `release_lifecycle.py`: manifest validation, install/upgrade/rollback and failed-start recovery, data preservation and actual MCP reconnection.
- `packaged_install.py`: npm tarball installation in a new prefix, packaged CLI version/install and MCP reconnection.
- `diagnostics_e2e.py`: CLI/IPC/Runtime/Relay/target info; offline, pairing, grant, inbound and read-only checks.
- `cross_layer_e2e.py`: actual CLI nonzero exit; IPC file UTF-8/size/workspace boundaries; incremental output; cancellation ownership; timeout; flood truncation; grant rotation against an already connected client; updated pairing; Relay loss during execution and recovery; actual stdio MCP execution/nonzero/reconnection.

Build MCP and a Runtime before local tests:

```powershell
npm --prefix packages/mcp ci
npm --prefix packages/mcp run build
go build -o dist/integration/xallor-remote.exe ./cmd/xallor-remote
python tests/cross_layer_e2e.py --binary dist/integration/xallor-remote.exe
```

CI builds a clean manifest-tagged artifact on each OS, runs the integration script and uploads its result with that artifact's manifest. `pr-check` requires the integration matrix, release lifecycle matrix, Go, MCP and GUI checks. Linux also runs race detection. JSON artifacts describe completed scenarios; a nonzero script exit fails the gate even if earlier scenarios passed.

Unit-level adversarial checks additionally cover terminal EOF/missing fields/wrong IDs, fake terminal frames from another device, dropped client bindings, and grant rotation. No test result claims production deployment or authorizes merging a PR.
