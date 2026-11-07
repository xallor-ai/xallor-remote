import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import test, { type TestContext } from "node:test";
import { ipcPath } from "./ensure.js";
import { MAX_IPC_MESSAGE_BYTES, RuntimeIpc } from "./ipc.js";

async function fixture(t: TestContext, handle: (socket: net.Socket, request: any) => void) {
  const root = await mkdtemp(path.join(os.tmpdir(), "xr-ipc-bound-"));
  const previous = process.env.XALLOR_REMOTE_DATA_DIR;
  process.env.XALLOR_REMOTE_DATA_DIR = root;
  const sockets = new Set<net.Socket>();
  const server = net.createServer((socket) => {
    sockets.add(socket);
    socket.on("error", () => {});
    socket.on("close", () => sockets.delete(socket));
    socket.setEncoding("utf8");
    let input = "";
    socket.on("data", (chunk: string) => {
      input += chunk;
      const end = input.indexOf("\n");
      if (end < 0) return;
      const request = JSON.parse(input.slice(0, end));
      input = input.slice(end + 1);
      handle(socket, request);
    });
  });
  const client = new RuntimeIpc();
  t.after(async () => {
    client.close();
    for (const socket of sockets) socket.destroy();
    await new Promise<void>((resolve) => server.close(() => resolve()));
    if (previous === undefined) delete process.env.XALLOR_REMOTE_DATA_DIR;
    else process.env.XALLOR_REMOTE_DATA_DIR = previous;
    assert.equal(path.dirname(root), path.resolve(os.tmpdir()));
    await rm(root, { recursive: true, force: true });
  });
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(ipcPath(), resolve);
  });
  await client.connect();
  return client;
}

// A peer cannot force unbounded buffering by omitting its NDJSON delimiter.
for (const delimiter of ["", "\n"]) {
  test(`should reject oversized response with delimiter=${Boolean(delimiter)}`, { timeout: 15000 }, async (t) => {
    const client = await fixture(t, (socket) => {
      socket.write("x".repeat(MAX_IPC_MESSAGE_BYTES + 1) + delimiter);
    });
    await assert.rejects(client.call("status"), /IPC 报文超过上限/);
    assert.equal(client.isConnected, false);
  });
}

// The wire budget must still accept worst-case escaping for a valid 1 MiB write.
test("should accept maximally escaped write content", { timeout: 15000 }, async (t) => {
  const content = "\0".repeat(1024 * 1024);
  const client = await fixture(t, (socket, request) => {
    assert.equal(request.params.content, content);
    socket.write(JSON.stringify({ id: request.id, ok: true, result: { accepted: true } }) + "\n");
  });
  const response = await client.call("write", { device_id: "123456789", path: "text", content });
  assert.equal(response.ok, true);
});

// Oversized requests are refused before handing bytes to the Runtime connection.
test("should reject oversized request without dispatch", { timeout: 15000 }, async (t) => {
  let requests = 0;
  const client = await fixture(t, () => { requests++; });
  await assert.rejects(client.call("write", { content: "x".repeat(MAX_IPC_MESSAGE_BYTES) }), /IPC 参数超过上限/);
  assert.equal(requests, 0);
});

// Coalesced stream records still reach the callback once and terminate normally;
// RuntimeIpc has no second, unused event history to retain for the task lifetime.
test("should deliver coalesced stream records and terminal", { timeout: 15000 }, async (t) => {
  const client = await fixture(t, (socket, request) => {
    let output = "";
    for (let index = 0; index < 1000; index++) {
      output += JSON.stringify({ id: request.id, event: "stdout", data: "中文🌍" }) + "\n";
    }
    output += JSON.stringify({ id: request.id, ok: true, result: { status: "completed" } }) + "\n";
    socket.write(output);
  });
  let count = 0;
  const terminal = await client.stream("exec", {}, (frame) => {
    assert.equal(frame.data, "中文🌍");
    count++;
  });
  assert.equal(count, 1000);
  assert.equal(terminal.ok, true);
});

// A malformed peer response fails the pending call instead of silently waiting.
test("should close and reject malformed response", { timeout: 15000 }, async (t) => {
  const client = await fixture(t, (socket) => { socket.write("null\n"); });
  await assert.rejects(client.call("status"), /IPC 报文格式无效/);
  assert.equal(client.isConnected, false);
});
