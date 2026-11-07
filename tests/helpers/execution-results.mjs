import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import readline from "node:readline";
import { fileURLToPath } from "node:url";

const root = process.env.XALLOR_TEST_CLUSTER;
assert(root && fs.readFileSync(path.join(root, ".owner"), "utf8") === "runtime-cluster");
const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const child = spawn(process.execPath, [path.join(repo, "packages/mcp/dist/index.js")], {
  env: process.env, windowsHide: true, stdio: ["pipe", "pipe", "pipe"],
});
const pending = new Map();
let sequence = 0;
const lines = readline.createInterface({ input: child.stdout });
lines.on("line", line => {
  const message = JSON.parse(line), waiter = pending.get(message.id);
  if (waiter) {
    clearTimeout(waiter.timer); pending.delete(message.id);
    message.error ? waiter.reject(Error(JSON.stringify(message.error))) : waiter.resolve(message.result);
  }
});
child.stderr.resume();
function call(method, params) {
  const id = ++sequence;
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => { pending.delete(id); reject(Error("MCP response deadline")); }, 25000);
    pending.set(id, {resolve, reject, timer});
    child.stdin.write(JSON.stringify({jsonrpc:"2.0", id, method, params})+"\n");
  });
}
const execute = (args, timeout_ms=0) => call("tools/call", {name:"remote.exec", arguments:{
  device_id:process.env.XALLOR_TEST_TARGET, program:{executable:process.env.XALLOR_TEST_PROGRAM,args}, timeout_ms,
}});
const text = result => result.content.map(c => c.text || "").join("\n");
const controllerCrash = Boolean(process.env.XALLOR_TEST_CONTROLLER_PID);
try {
  await call("initialize", {protocolVersion:"2025-11-25",capabilities:{},clientInfo:{name:"result-e2e",version:"1"}});
  child.stdin.write(JSON.stringify({jsonrpc:"2.0",method:"notifications/initialized"})+"\n");
  if (!controllerCrash) {
    const success = await execute(["success"]);
    assert(!success.isError); assert(text(success).includes("status=completed exit_code=0"));
    for (const [mode, timeout, expected] of [["failure",0,"exit_code=7"],["wait",8000,"status=timeout"],["truncated",0,"truncated=true"]]) {
      const result = await execute([mode], timeout);
      assert.equal(result.isError, true);
      for (const value of ["result-observed-output",expected,"exec_id="]) assert(text(result).includes(value), value);
      if (mode !== "truncated") assert(text(result).includes("result-observed-error"));
    }
  }
  // Kill only the process owned and identified by the Python harness. Stream
  // notifications prove output reached MCP before Relay or controller loss.
  const started = path.join(root, controllerCrash ? "controller-result-ready" : "relay-result-ready");
  let observed = false;
  const observe = line => {
    const message = JSON.parse(line);
    if (message.method === "notifications/progress" && message.params?.message?.includes("result-observed-output")) observed = true;
  };
  lines.on("line", observe);
  const running = call("tools/call", {name:"remote.exec", arguments:{device_id:process.env.XALLOR_TEST_TARGET,
    program:{executable:process.env.XALLOR_TEST_PROGRAM,args:["wait",started]}}, _meta:{progressToken:"disconnect"}});
  running.catch(() => {});
  const deadline = Date.now()+15000;
  while (!observed || !fs.existsSync(started)) {
    assert(Date.now() < deadline, "process/output did not become observable");
    await new Promise(r => setTimeout(r,30));
  }
  const ownedPid = Number(controllerCrash ? process.env.XALLOR_TEST_CONTROLLER_PID : process.env.XALLOR_TEST_RELAY_PID);
  assert(Number.isInteger(ownedPid) && ownedPid > 0);
  process.kill(ownedPid);
  const disconnected = await running;
  assert.equal(disconnected.isError, true);
  assert(text(disconnected).includes("result-observed-output"));
  assert(text(disconnected).includes("exec_id="));
  assert(!text(disconnected).includes("status=completed exit_code=0"));
  if (controllerCrash) assert(text(disconnected).includes("执行结果未确认"));
  console.log(controllerCrash ? "PASS MCP output survives controller IPC loss" : "PASS MCP real success, failure, timeout, truncation and relay-loss output");
} finally {
  for (const waiter of pending.values()) clearTimeout(waiter.timer);
  lines.close(); child.stdin.end(); child.kill();
}
