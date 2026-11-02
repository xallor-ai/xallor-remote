import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { RuntimeIpc } from "../../packages/mcp/dist/ipc.js";

const pause = ms => new Promise(r => setTimeout(r, ms));
function alive(pid) {
  try {
    process.kill(pid, 0);
    // A zombie is terminated and cannot execute; init may reap it later on Linux.
    if (process.platform === "linux" && /\) Z /.test(fs.readFileSync(`/proc/${pid}/stat`, "utf8"))) return false;
    return true;
  } catch (e) { if (e.code === "ESRCH" || e.code === "ENOENT") return false; throw e; }
}
export async function verifyTree(root, targetID, mode) {
  assert(["cancel", "timeout", "disconnect"].includes(mode));
  const dir = path.join(root, "tree-" + mode);
  fs.mkdirSync(dir);
  const binary = path.join(root, process.platform === "win32" ? "tree.exe" : "tree");
  const quote = text => process.platform === "win32" ? "'" + text.replaceAll("'", "''") + "'" : "'" + text.replaceAll("'", "'\\''") + "'";
  const command = (process.platform === "win32" ? "& " : "") + quote(binary) + " parent " + quote(dir);
  process.env.XALLOR_REMOTE_DATA_DIR = path.join(root, "controller");
  const ipc = new RuntimeIpc(); await ipc.connect();
  let id, pids, terminal, settled;
  let startupOutput="";
  try {
    terminal = ipc.stream("exec", { device_id: targetID, command, ...(mode === "timeout" ? { timeout_ms: 10000 } : {}) }, f => {
      if (f.exec_id) id = f.exec_id;
      if(f.data) startupOutput=(startupOutput+f.data).slice(-4096);
    });
    // Attach rejection immediately; disconnect is intentionally expected later.
    terminal = terminal.then(frame => (settled={ frame }), error => (settled={ error }));
    const started = Date.now() + 15000;
    while (!fs.existsSync(path.join(dir, "pids.json")) || !fs.existsSync(path.join(dir, "heartbeat"))) {
      assert(!settled, "tree exited before becoming active: "+JSON.stringify(settled)+" output="+startupOutput);
      assert(Date.now() < started, "process tree never became active; output="+startupOutput); await pause(30);
    }
    pids = JSON.parse(fs.readFileSync(path.join(dir, "pids.json"), "utf8"));
    assert(alive(pids.parent) && alive(pids.child), "fixture must prove both processes started");
    if (mode === "cancel") assert.equal((await ipc.call("exec.cancel", { exec_id: id })).ok, true);
    if (mode === "disconnect") ipc.close();
    let timer;
    const result = await Promise.race([terminal, new Promise((_, reject) => { timer = setTimeout(() => reject(Error("tree terminal timeout")), 15000); })]).finally(() => clearTimeout(timer));
    if (mode === "disconnect") assert(result.error);
    else assert.equal(result.frame?.result?.status, mode === "cancel" ? "cancelled" : "timeout");
    const deadline = Date.now() + 10000;
    while (alive(pids.parent) || alive(pids.child)) { assert(Date.now() < deadline, "descendant survived " + mode); await pause(50); }
    const heartbeat = fs.readFileSync(path.join(dir, "heartbeat"), "utf8");
    await pause(150);
    assert.equal(fs.readFileSync(path.join(dir, "heartbeat"), "utf8"), heartbeat);
  } finally {
    ipc.close();
    // Only PIDs written by this owned fixture are eligible for failure cleanup.
    if (pids) for (const pid of Object.values(pids)) { try { if (alive(pid)) process.kill(pid); } catch {} }
  }
}
