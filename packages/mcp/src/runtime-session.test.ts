import assert from "node:assert/strict";
import test from "node:test";
import { runtimeSession } from "./runtime-session.js";
import { connectOwner } from "./runtime-owner.js";

// Concurrent tool calls share an uncompleted initialization, including the
// readiness gate. No caller gets a second connection/launch before that gate.
test("concurrent callers share one admitted owner connection", async () => {
  let release!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; });
  let initializations = 0;
  const connection = { isConnected: true, close() {} };
  const get = runtimeSession(async () => { initializations++; await gate; return connection; }, async () => {});
  const callers = Array.from({ length: 8 }, () => get());
  release();
  assert.ok((await Promise.all(callers)).every(c => c === connection));
  assert.equal(initializations, 1);
});

// Disconnecting discards only the old connection. Every successor is admitted
// by initialize, and concurrent reconnect callers still share one attempt.
test("disconnect revalidates a new connection without restarting the owner", async () => {
  const connections: { isConnected: boolean; closed: number; close(): void }[] = [];
  let bootstraps = 0;
  const get = runtimeSession(async () => {
    const c = { isConnected: true, closed: 0, close() { this.closed++; } };
    connections.push(c);
    return c;
  }, async () => { bootstraps++; });
  const first = await get();
  assert.equal(await get(), first);
  first.isConnected = false;
  const [second, concurrent] = await Promise.all([get(), get()]);
  assert.notEqual(second, first);
  assert.equal(second, concurrent);
  assert.equal(first.closed, 1);
  assert.equal(connections.length, 2);
  assert.equal(bootstraps, 0);
});

// Even a completed bootstrap must not become an automatic restart policy after
// later disconnection. Re-observation may fail; the process callback stays once.
test("successful bootstrap is never replayed after subsequent owner loss", async () => {
  let starts = 0;
  let available = false;
  const get = runtimeSession(async bootstrap => {
    await connectOwner({ async connect() {
      if (!available) throw Object.assign(new Error("absent"), { code: "ENOENT" });
    } }, bootstrap);
    return { isConnected: true as boolean, close() {} };
  }, async () => { starts++; available = true; });
  const first = await get();
  first.isConnected = false;
  available = false;
  await assert.rejects(get(), /absent/);
  assert.equal(starts, 1);
});

// A connection that closes at the readiness boundary is never cached as usable.
test("connection lost during initialization is closed and rejected", async () => {
  let closed = 0;
  const get = runtimeSession(async () => ({ isConnected: false, close() { closed++; } }), async () => {});
  await assert.rejects(get(), /已断开/);
  assert.equal(closed, 1);
});

// Admission refusal is shared by waiting tools, but is not cached as readiness.
// A later explicit tool call may re-observe an owner that has been repaired.
test("concurrent refusal closes the attempt and permits fresh observation", async () => {
  let attempts = 0;
  const failure = new Error("owner source rejected");
  const get = runtimeSession(async () => {
    if (++attempts === 1) throw failure;
    return { isConnected: true, close() {} };
  }, async () => { throw new Error("unexpected launch"); });
  const refused = await Promise.allSettled([get(), get(), get()]);
  assert.ok(refused.every(r => r.status === "rejected" && r.reason === failure));
  assert.equal(attempts, 1);
  assert.equal((await get()).isConnected, true);
  assert.equal(attempts, 2);
});

// An unknown bootstrap result must not be replayed by the next tool call.
// A later externally available owner may still be connected and admitted.
test("later calls observe an uncertain bootstrap without replaying it", async () => {
  let starts = 0;
  let available = false;
  const failure = new Error("bootstrap outcome unknown");
  const get = runtimeSession(async bootstrap => {
    await connectOwner({ async connect() {
      if (!available) throw Object.assign(new Error("absent"), { code: "ENOENT" });
    } }, bootstrap);
    return { isConnected: true, close() {} };
  }, async () => { starts++; throw failure; });
  await assert.rejects(get(), error => error === failure);
  await assert.rejects(get(), error => error === failure);
  assert.equal(starts, 1);
  available = true;
  assert.equal((await get()).isConnected, true);
  assert.equal(starts, 1);
});
