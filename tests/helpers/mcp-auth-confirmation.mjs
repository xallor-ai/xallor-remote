import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import readline from "node:readline";
import path from "node:path";
import { fileURLToPath } from "node:url";
assert(process.env.XALLOR_REMOTE_DATA_DIR, "isolated profile required");
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const child = spawn(process.execPath, [path.join(root, "packages/mcp/dist/index.js")], {
  env: { ...process.env, XALLOR_REMOTE_DEVICE_ID: "", XALLOR_REMOTE_DEVICE_GRANT: "" }, windowsHide: true,
  stdio: ["pipe", "pipe", "pipe"],
});
const pending = new Map(); let id = 0;
const reader = readline.createInterface({ input: child.stdout });
reader.on("line", line => {
  const message = JSON.parse(line), waiter = pending.get(message.id);
  if (waiter) { clearTimeout(waiter.timer); pending.delete(message.id); message.error ? waiter.reject(Error("MCP RPC failed")) : waiter.resolve(message.result); }
});
function call(method, params) {
  const requestID = ++id;
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(Error("MCP response timed out")), 15000);
    pending.set(requestID, { resolve, reject, timer });
    child.stdin.write(JSON.stringify({ jsonrpc: "2.0", id: requestID, method, params }) + "\n");
  });
}
try {
  await call("initialize", { protocolVersion: "2025-11-25", capabilities: {}, clientInfo: { name: "auth-confirmation-test", version: "1" } });
  child.stdin.write(JSON.stringify({ jsonrpc: "2.0", method: "notifications/initialized" }) + "\n");
  const result = await call("tools/call", { name: "remote.exec", arguments: { device_id: "123456789", script: { text: "echo never-dispatch" } } });
  assert.equal(result.isError, true);
  const text = JSON.stringify(result);
  assert(!text.includes("unexpected-execution"));
  assert(!text.includes("exit_code=0"));
  console.log("MCP rejected unconfirmed client authentication");
} finally {
  for (const waiter of pending.values()) clearTimeout(waiter.timer);
  reader.close(); child.stdin.end(); child.kill();
}
