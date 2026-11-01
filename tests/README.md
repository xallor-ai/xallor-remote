# Cross-layer verification

All process tests use owned temporary profiles and a loopback Relay. They never connect to the user's profile or official Relay. Cleanup stops only fixture processes, verifies the owned absolute directory and then removes it; failed cleanup retains evidence.

- `release_lifecycle.py`: manifest validation, install/upgrade/rollback and failed-start recovery, data preservation and actual MCP reconnection.
- `packaged_install.py`: npm tarball installation in a new prefix, packaged CLI version/install and MCP reconnection.
- `diagnostics_e2e.py`: CLI/IPC/Runtime/Relay/target info; offline, pairing, grant, inbound and read-only checks.
- `handoff_e2e.py --binary <runtime> [--go <go>]`: actual IPC/Relay/Runtime with a deterministic Cursor CLI fixture; durable dedupe, conflicting reuse, matching session/result, bounded Unicode, repository/attachment validation and restart without replay. Authenticated official Cursor CLI verification is a separate gate, not replaced by this fixture.
- `handoff_cursor_live.py --binary <runtime> --executable <official-cli> [--prefix-arg <entry>]`: opt-in after local account login; creates a new isolated Ask session, confirms a remembered random token after resuming that exact session, verifies duplicate receipt and clean repository. Evidence stays outside the checkout; credentials and raw login output are never recorded. Ordinary CI does not run this account-dependent test.
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

## GUI connection workflow

`npm ci`, `npm run build` and `npm test` in `apps/gui` validate frontend compilation and terminal-state/output-bound regressions. Install the test browser with `npx playwright install chromium`, then from the repository run:

```text
python tests/gui_e2e.py --binary <freshly-built-xallor-remote>
```

The harness owns a loopback Relay, two temporary Runtime profiles and a Vite process. Playwright performs actual alias persistence/XSS escaping, hidden credential reveal/hide, per-target doctor, stream-before-completion, navigation, cancellation (including five immediate cancels), nonzero exits, actual incoming approval allow/deny, audit and mobile-layout checks. No HTTP/Runtime mocks or production service are used. Screenshots and bounded failure audit metadata stay in a temporary evidence directory outside the checkout.

`--native` additionally runs the ignored Rust bridge integration test against that fixture: real IPC status, approval subscription, streaming and same-session cancellation. CI compiles the native application on Windows and Linux; browser interaction coverage does not claim native-window/WebView behavior. Native integration requires Rust/Tauri platform dependencies. The aggregate required gate includes both GUI jobs.
