import { spawn } from "node:child_process";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { readFileSync } from "node:fs";
import readline from "node:readline";
import path from "node:path";
import { fileURLToPath } from "node:url";
if (!process.env.XALLOR_REMOTE_DATA_DIR) throw new Error("isolated profile required");
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const expectedVersion = JSON.parse(readFileSync(path.join(root, "packages/mcp/package.json"), "utf8")).version;
const child = spawn(process.execPath, [process.env.XALLOR_TEST_MCP_ENTRY || path.join(root, "packages/mcp/dist/index.js")],
  { env: { ...process.env, XALLOR_REMOTE_DEVICE_ID: "", XALLOR_REMOTE_DEVICE_GRANT: "" }, windowsHide: true, stdio: ["pipe", "pipe", "pipe"] });
const pending = new Map();
let seq = 0;
const lines = readline.createInterface({ input: child.stdout });
lines.on("line", line => { const msg = JSON.parse(line); const p = pending.get(msg.id);
  if (p) { clearTimeout(p.timer); pending.delete(msg.id); msg.error ? p.reject(Error(JSON.stringify(msg.error))) : p.resolve(msg.result); } });
function call(method, params) {
  const id = ++seq;
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(Error("MCP timed out")), 10000);
    pending.set(id, { resolve, reject, timer });
    child.stdin.write(JSON.stringify({ jsonrpc: "2.0", id, method, params }) + "\n");
  });
}
try {
  await call("initialize", { protocolVersion: "2025-11-25", capabilities: {}, clientInfo: { name: "release-e2e", version: "1" } });
  child.stdin.write(JSON.stringify({ jsonrpc: "2.0", method: "notifications/initialized" }) + "\n");
  const result = await call("tools/call", { name: "remote.devices", arguments: {} });
  if (result.isError) throw new Error(JSON.stringify(result));
  const status = JSON.parse(result.content[0].text);
  if (!/^[0-9]{9}$/.test(status.device_id) || status.version !== expectedVersion) throw Error("incorrect runtime identity");
  if (process.env.XALLOR_TEST_TARGET) {
    if (process.env.XALLOR_TEST_USAGE_CONTRACT) {
      const device = await call("tools/call", { name: "remote.device_info", arguments: { device_id: process.env.XALLOR_TEST_TARGET } });
      if (device.isError) throw Error("MCP target usage contract lookup failed");
      const info = JSON.parse(device.content[0].text);
      if (JSON.stringify(info.usage_event_contract) !== JSON.stringify(JSON.parse(process.env.XALLOR_TEST_USAGE_CONTRACT)))
        throw Error("MCP usage contract diverged from CLI/Runtime");
    }
    const executed = await call("tools/call", { name: "remote.exec", arguments: { device_id: process.env.XALLOR_TEST_TARGET, script: { text: "echo mcp-e2e" } } });
    if (executed.isError || !executed.content.some(c => c.text?.includes("mcp-e2e") && c.text.includes("exit_code=0")))
      throw Error("MCP remote execution failed");
    const failed = await call("tools/call", { name: "remote.exec", arguments: { device_id: process.env.XALLOR_TEST_TARGET, script: { text: "exit 7" } } });
    if (!failed.isError) throw Error("MCP lost nonzero remote exit");
    if (process.env.XALLOR_TEST_PROGRAM) {
      const args = ["", "a b", "$HOME", "中文🌍"];
      const native = await call("tools/call", { name: "remote.exec", arguments: { device_id: process.env.XALLOR_TEST_TARGET, program: { executable: process.env.XALLOR_TEST_PROGRAM, args } } });
      if (native.isError || !native.content.some(c => c.text?.includes(JSON.stringify(args)))) throw Error("MCP native argv lost");
    }
  }
  if (process.env.XALLOR_TEST_CLI) {
    const run = promisify(execFile);
    const cli = (...args) => run(process.env.XALLOR_TEST_CLI, args, { env: process.env, windowsHide: true, timeout: 20000 });
    await cli("stop");
    const deadline = Date.now() + 10000;
    while (JSON.parse((await cli("runtime", "status")).stdout).running) {
      if (Date.now() > deadline) throw Error("Runtime did not stop");
      await new Promise(r => setTimeout(r, 50));
    }
    await cli("ensure");
    const after = await call("tools/call", { name: "remote.devices", arguments: {} });
    if (after.isError || JSON.parse(after.content[0].text).device_id !== status.device_id)
      throw Error("MCP did not reconnect after Runtime restart");
  }
  console.log("MCP profile and version verified");
} finally {
  for (const p of pending.values()) clearTimeout(p.timer);
  lines.close(); child.stdin.end(); child.kill();
}
