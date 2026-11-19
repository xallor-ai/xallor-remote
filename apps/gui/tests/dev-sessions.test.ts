import { test } from "node:test";
import assert from "node:assert/strict";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import fs from "node:fs";
import { randomUUID } from "node:crypto";
import { Session } from "../dev-sessions";

const expected = { version: "0.3.0", revision: "a".repeat(40), dirty: false, protocol: 1, profile_format: 1 };
const ready = { state: "ready", protocol: 1, profile_format: 1, pid: 42, version: "0.3.0", revision: "a".repeat(40), sha256: "b".repeat(64), activation_id: "c".repeat(32), launch_id: "d".repeat(32) };
function endpoint() { return process.platform === "win32" ? `\\\\.\\pipe\\xr-gui-${randomUUID()}` : path.join(os.tmpdir(), `xr-gui-${randomUUID()}.sock`); }

// An absent owner must fail closed without discovering or spawning a binary.
test("should reject missing owner without bootstrap", async () => {
  const session = new Session(endpoint());
  await assert.rejects(session.ready(expected, 1000));
  assert.equal(session.socket.destroyed, true);
});

for (const response of [{}, { ...ready, activation_id: "invalid" }]) {
  // A transport connection alone must not authorize exposing a GUI session.
  test("should reject malformed owner readiness", async t => {
    const address = endpoint();
    const server = net.createServer(socket => socket.once("data", bytes => {
      const request = JSON.parse(bytes.toString());
      socket.write(JSON.stringify({ id: request.id, ok: true, result: { runtime: response } }) + "\n");
    }));
    await new Promise<void>(resolve => server.listen(address, resolve));
    t.after(() => { server.close(); if (process.platform !== "win32") fs.rmSync(address, { force: true }); });
    const session = new Session(address);
    await assert.rejects(session.ready(expected, 1000));
    assert.equal(session.socket.destroyed, true);
  });
}

// Readiness is consumed privately; application frames retain original IDs and
// a close tears down this connection, with no command replay or process spawn.
test("should preserve frames and close only the owned connection", async t => {
  const address = endpoint(); let closed!: () => void;
  const didClose = new Promise<void>(resolve => { closed = resolve; });
  const server = net.createServer(socket => {
    socket.once("close", closed);
    socket.once("data", bytes => {
      const request = JSON.parse(bytes.toString());
      assert.equal(request.method, "runtime.ensure");
      socket.write(JSON.stringify({ id: request.id, ok: true, result: { runtime: ready } }) + "\n");
      socket.once("data", bytes => socket.write(bytes));
    });
  });
  await new Promise<void>(resolve => server.listen(address, resolve));
  t.after(() => { server.close(); if (process.platform !== "win32") fs.rmSync(address, { force: true }); });
  const session = new Session(address); t.after(() => session.fail("test complete"));
  await session.ready(expected, 1000);
  assert.deepEqual(session.frames, []);
  const received = new Promise<void>(resolve => session.socket.once("data", () => resolve()));
  session.socket.write(JSON.stringify({ id: "original", event: "stdout", data: "hello" }) + "\n");
  await received;
  assert.deepEqual(session.frames, [{ id: "original", event: "stdout", data: "hello" }]);
  session.fail("closed"); await didClose;
});

// A silent connected owner has a finite observation deadline.
test("should close silent owner at readiness deadline", async t => {
  const address = endpoint();
  const server = net.createServer(socket => socket.resume());
  await new Promise<void>(resolve => server.listen(address, resolve));
  t.after(() => { server.close(); if (process.platform !== "win32") fs.rmSync(address, { force: true }); });
  const session = new Session(address);
  await assert.rejects(session.ready(expected, 30), /超时/);
  assert.equal(session.socket.destroyed, true);
});

// Queue exhaustion closes the connection rather than retaining unbounded
// output while the browser stops polling.
test("should close owner when application output exceeds queue bound", async t => {
  const address = endpoint();
  const server = net.createServer(socket => {
    socket.on("error", () => {});
    socket.once("data", bytes => {
      const request = JSON.parse(bytes.toString());
      socket.write(JSON.stringify({ id: request.id, ok: true, result: { runtime: ready } }) + "\n");
      socket.once("data", () => socket.write((JSON.stringify({ id: "task", event: "stdout", data: "hello" }) + "\n").repeat(4097)));
    });
  });
  await new Promise<void>(resolve => server.listen(address, resolve));
  t.after(() => { server.close(); if (process.platform !== "win32") fs.rmSync(address, { force: true }); });
  const session = new Session(address); t.after(() => session.fail("test complete"));
  await session.ready(expected, 1000);
  const closed = new Promise<void>(resolve => session.socket.once("close", () => resolve()));
  session.socket.write("start\n");
  await closed;
  assert.match(session.error!, /输出积压/);
  assert.equal(session.frames.length, 4096);
});
