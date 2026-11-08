import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { RuntimeIpc } from "../../packages/mcp/dist/ipc.js";
import { verifyTree } from "./process-tree.mjs";

const root = process.env.XALLOR_TEST_CLUSTER;
assert(root && fs.readFileSync(path.join(root, ".owner"), "utf8") === "runtime-cluster");
const targetID = process.env.XALLOR_TEST_TARGET;
assert.match(targetID, /^\d{9}$/);
const connections = [];
const results = [];
const delay = ms => new Promise(r => setTimeout(r, ms));
async function bounded(promise, ms = 15000) {
  let timer;
  try { return await Promise.race([promise, new Promise((_, reject) => { timer = setTimeout(() => reject(Error("test deadline exceeded")), ms); })]); }
  finally { clearTimeout(timer); }
}
async function connect(name) {
  process.env.XALLOR_REMOTE_DATA_DIR = path.join(root, name);
  const client = new RuntimeIpc();
  await client.connect(); connections.push(client); return client;
}
function pass(name) { results.push({ name, status: "pass" }); console.log(name); }
function ok(frame) { assert.equal(frame.ok, true, frame.code); return frame.result; }
const sleep = process.platform === "win32" ? "Start-Sleep -Seconds 20" : "sleep 20";
try {
  const target = await connect("target"), controller = await connect("controller"), stranger = await connect("controller");
  const call = (client, method, params = {}) => bounded(client.call(method, params));
  const targetInfo = JSON.parse(ok(await call(controller, "info", { device_id: targetID })).content);
  assert.equal(targetInfo.operation_policy.version, 1);
  assert.equal(targetInfo.operation_policy.sandbox, "none");
  assert.deepEqual(targetInfo.operation_policy.operations.map(row => row.operation), ["info", "read", "write", "exec", "processes", "cancel", "handoff.submit", "handoff.get"]);
  assert.equal(targetInfo.device_lifecycle.version, 1);
  assert.deepEqual(targetInfo.device_lifecycle.steps.map(step => step.name), ["enroll", "grant_issue_or_rotate", "claim", "transfer", "revoke", "reset"]);
  assert.equal(targetInfo.entitlement_contract.version, 1);
  assert.deepEqual(targetInfo.entitlement_contract.features.map(feature => feature.name), ["exec.concurrent", "terminal.concurrent", "relay.bytes_per_second", "relay.monthly_egress_bytes", "file.max_transfer_bytes"]);
  assert.equal(targetInfo.usage_event_contract.version, 2);
  assert.deepEqual(targetInfo.usage_event_contract.outcomes, ["completed", "failed", "unknown"]);
  assert.equal(targetInfo.usage_event_contract.time.format, "RFC3339Nano");
  assert.equal(targetInfo.usage_event_contract.time.zone, "UTC Z");
  assert.equal(targetInfo.usage_event_contract.time.canonical, true);
  assert.equal(targetInfo.usage_event_contract.time.max_bytes, 30);
  assert.equal(targetInfo.usage_event_contract.retention.max_events, 5000);
  assert.equal(targetInfo.usage_event_contract.retention.max_retired_keys, 5000);
  assert.equal(targetInfo.usage_event_contract.retention.max_bytes, 4 * 1024 * 1024);
  assert.match(targetInfo.usage_event_contract.retention.retired_key, /all reuse conflicts/);
  assert.match(targetInfo.usage_event_contract.retention.when_full, /reject append/);
  assert.match(targetInfo.usage_event_contract.aggregation, /overflow returns error and zero/);
  assert.deepEqual(targetInfo.usage_event_contract, JSON.parse(process.env.XALLOR_TEST_USAGE_CONTRACT));
  pass("usage_contract_matches_cli_with_bounded_time_retention_and_overflow");
  const contents = "中文 🌍\nline two\n";
  ok(await call(controller, "write", { device_id: targetID, path: "roundtrip.txt", content: contents }));
  assert.equal(ok(await call(controller, "read", { device_id: targetID, path: "roundtrip.txt" })).content, contents);
  const limited = ok(await call(controller, "read", { device_id: targetID, path: "roundtrip.txt", offset: 8, limit: 2 }));
  assert.equal(limited.truncated, true);
  assert(!limited.content.includes("\ufffd"));
  assert.equal((await call(controller, "write", { device_id: targetID, path: "../escaped.txt", content: "no" })).code, "policy_deny");
  assert.equal((await call(controller, "write", { device_id: targetID, path: "large.txt", content: "x".repeat(1024*1024+1) })).code, "too_large");
  assert(!fs.existsSync(path.join(root, "workspaces/escaped.txt")));
  pass("utf8_file_roundtrip_and_workspace_boundary");
  const events = [];
  let firstObserved;
  const result = await bounded(controller.stream("exec", { device_id: targetID, script: { text: "echo first; " + (process.platform === "win32" ? "Start-Sleep -Milliseconds 500" : "sleep 0.5") + "; echo second" } }, f => {
    events.push(f);
    if (f.data?.includes("first") && firstObserved === undefined) firstObserved = Date.now();
  }));
  assert(firstObserved !== undefined && Date.now() - firstObserved >= 250, "output was buffered until completion");
  assert.equal(ok(result).exit_code, 0);
  const text = events.map(f => f.data || "").join("");
  assert(text.includes("first") && text.includes("second"));
  assert(events.filter(f => f.data).length >= 2);
  assert.equal(ok(await bounded(controller.stream("exec", { device_id: targetID, script: { text: "exit 7" } }, () => {}))).exit_code, 7);
  pass("streaming_and_nonzero_exit_are_preserved");
  let execID, readyResolve;
  const ready = new Promise(r => { readyResolve = r; });
  const running = controller.stream("exec", { device_id: targetID, script: { text: "echo ready; " + sleep } }, f => {
    if (f.exec_id) execID = f.exec_id;
    if (f.data?.includes("ready")) readyResolve();
  });
  await bounded(ready);
  assert.equal((await call(stranger, "exec.cancel", { exec_id: execID })).code, "unknown_exec");
  ok(await call(controller, "exec.cancel", { exec_id: execID }));
  assert.equal(ok(await bounded(running)).status, "cancelled");
  pass("cancel_requires_original_ipc_connection_and_terminates_task");
  let timeoutOutput = "";
  const timeoutMS = Number(process.env.XALLOR_TEST_TIMEOUT_MS || 5000);
  assert(timeoutMS >= 5000 && timeoutMS <= 30000);
  const longSleep = process.platform === "win32" ? "Start-Sleep -Seconds 60" : "sleep 60";
  const timed = await bounded(controller.stream("exec", { device_id: targetID, script: { text: "echo timeout-ready; " + longSleep }, timeout_ms: timeoutMS }, f => {
    timeoutOutput += f.data || "";
  }), timeoutMS + 10000);
  assert(timeoutOutput.includes("timeout-ready"), "timeout case did not reach a running child: " + JSON.stringify(timed));
  assert.equal(ok(timed).status, "timeout");
  pass("timeout_has_explicit_terminal_status");
  for (const mode of ["cancel", "timeout", "disconnect"]) {
    await verifyTree(root, targetID, mode);
    pass("descendant_processes_are_reclaimed_on_" + mode);
  }
  const flood = process.platform === "win32" ? "[Console]::Write(('x' * 9000000))" : "head -c 9000000 /dev/zero | tr '\\0' x";
  let bytes = 0;
  const flooded = await bounded(controller.stream("exec", { device_id: targetID, script: { text: flood } }, f => { bytes += Buffer.byteLength(f.data || ""); }), 30000);
  assert.equal(ok(flooded).truncated, true);
  assert(bytes > 0 && bytes <= 4 * 1024 * 1024);
  pass("output_flood_is_bounded_and_still_terminates");

  const newGrant = ok(await call(target, "grant.rotate")).grant;
  // Wait for the independent diagnostic connection to observe the rotation at Relay.
  for (let n = 0; ; n++) {
    const d = ok(await call(controller, "doctor", { device_id: targetID }));
    if (d.checks.some(c => c.code === "unauthorized")) break;
    assert(n < 40, "grant rotation never reached Relay"); await delay(50);
  }
  assert.equal((await call(controller, "info", { device_id: targetID })).code, "unauthorized");
  ok(await call(controller, "peer.add", { device_id: targetID, grant: newGrant }));
  ok(await call(controller, "info", { device_id: targetID }));
  pass("rotated_grant_revokes_cached_connection_and_new_pairing_recovers");

  let disconnectReady;
  const started = new Promise(r => { disconnectReady = r; });
  const interrupted = controller.stream("exec", { device_id: targetID, script: { text: "echo disconnect-ready; " + sleep } }, f => {
    if (f.data?.includes("disconnect-ready")) disconnectReady();
  });
  await bounded(Promise.race([
    started,
    interrupted.then(frame => { throw Error("Relay-loss execution ended before start: " + JSON.stringify(frame)); }),
  ]));
  // Only the fixture-owned Relay PID is provided by the parent test harness.
  const relayPID = Number(process.env.XALLOR_TEST_RELAY_PID);
  assert(Number.isSafeInteger(relayPID) && relayPID > 0);
  process.kill(relayPID);
  assert.equal((await bounded(interrupted)).code, "relay_error");
  pass("relay_loss_fails_inflight_without_reporting_success");
  fs.writeFileSync(path.join(root, "cross-layer-results.json"), JSON.stringify(results));
} finally { for (const client of connections) client.close(); }
