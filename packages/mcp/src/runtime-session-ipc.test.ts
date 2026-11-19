import assert from "node:assert/strict";
import fs from "node:fs";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { ipcPath } from "./ensure.js";
import { RuntimeIpc } from "./ipc.js";
import { connectOwner, ensureOwner } from "./runtime-owner.js";
import { runtimeSession } from "./runtime-session.js";

// Real local sockets exercise transport and the full owner identity validator.
// Concurrent tools share one admitted connection; after disconnect they must
// validate a fresh owner and must not replay a process launch.
test("real IPC sessions share admission and revalidate after disconnect", { timeout: 10000 }, async () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "xr-session-ipc-"));
  const previous = process.env.XALLOR_REMOTE_DATA_DIR;
  process.env.XALLOR_REMOTE_DATA_DIR = root;
  const sockets = new Set<net.Socket>();
  const clients: RuntimeIpc[] = [];
  const requests: string[] = [];
  let connections = 0;
  let bootstraps = 0;
  const server = net.createServer(socket => {
    connections++;
    sockets.add(socket);
    socket.on("error", () => {});
    socket.on("close", () => sockets.delete(socket));
    socket.setEncoding("utf8");
    let buffer = "";
    socket.on("data", chunk => {
      buffer += chunk;
      while (buffer.includes("\n")) {
        const i = buffer.indexOf("\n");
        const request = JSON.parse(buffer.slice(0, i));
        buffer = buffer.slice(i + 1);
        requests.push(request.method);
        assert.equal(request.method, "runtime.ensure");
        assert.deepEqual(request.params, { required_profile_format: 1, required_protocol: 1 });
        socket.write(JSON.stringify({ id: request.id, ok: true, result: { runtime: {
          state: "ready", version: "0.2.6", revision: "a".repeat(40), protocol: 1,
          profile_format: 1, pid: 123, sha256: "b".repeat(64), activation_id: "c".repeat(32), launch_id: "d".repeat(32),
        } } })+"\n");
      }
    });
  });
  try {
    await new Promise<void>((resolve, reject) => { server.once("error", reject); server.listen(ipcPath(), resolve); });
    const get = runtimeSession(async bootstrap => {
      const ipc = new RuntimeIpc();
      clients.push(ipc);
      try {
        await connectOwner(ipc, bootstrap);
        await ensureOwner(ipc, { protocol: 1, profile_format: 1 });
        return ipc;
      } catch (error) { ipc.close(); throw error; }
    }, async () => { bootstraps++; throw new Error("unexpected bootstrap"); });
    const first = await Promise.all(Array.from({ length: 8 }, () => get()));
    assert.ok(first.every(c => c === first[0]));
    assert.equal(connections, 1);
    for (const socket of sockets) socket.destroy();
    const deadline = Date.now()+2000;
    while (first[0].isConnected && Date.now() < deadline) await new Promise(resolve => setTimeout(resolve, 5));
    assert.equal(first[0].isConnected, false);
    const second = await Promise.all([get(), get()]);
    assert.equal(second[0], second[1]);
    assert.notEqual(second[0], first[0]);
    assert.equal(connections, 2);
    assert.deepEqual(requests, ["runtime.ensure", "runtime.ensure"]);
    assert.equal(bootstraps, 0);
  } finally {
    for (const client of clients) client.close();
    for (const socket of sockets) socket.destroy();
    if (server.listening) await new Promise<void>(resolve => server.close(() => resolve()));
    if (previous === undefined) delete process.env.XALLOR_REMOTE_DATA_DIR;
    else process.env.XALLOR_REMOTE_DATA_DIR = previous;
    assert.equal(path.dirname(path.resolve(root)), path.resolve(os.tmpdir()));
    assert.ok(path.basename(root).startsWith("xr-session-ipc-"));
    fs.rmSync(root, { recursive: true, force: true });
  }
});
