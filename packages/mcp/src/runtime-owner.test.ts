import assert from "node:assert/strict";
import test from "node:test";
import type { Frame } from "./ipc.js";
import { connectOwner, ensureOwner, OWNER_OBSERVATION_TIMEOUT_MS } from "./runtime-owner.js";

const expected = { protocol: 1, profile_format: 1 };
const ready = () => ({ state: "ready", version: "0.2.6", revision: "a".repeat(40),
  protocol: 1, profile_format: 1, sha256: "b".repeat(64), pid: 123, activation_id: "c".repeat(32), launch_id: "d".repeat(32) });

// Timeout, access denial, busy endpoints and untyped failures cannot establish
// owner absence. None may invoke the bootstrap callback or retry connection.
test("should not bootstrap when owner connection state is unknown", async () => {
  for (const code of ["ETIMEDOUT", "EACCES", "EPERM", "EBUSY", undefined]) {
    const error = Object.assign(new Error("connection uncertain"), code ? { code } : {});
    let connections = 0;
    let bootstraps = 0;
    await assert.rejects(connectOwner({ connect: async () => { connections++; throw error; } },
      async () => { bootstraps++; }), e => e === error);
    assert.equal(bootstraps, 0, `unexpected bootstrap for ${code}`);
    assert.equal(connections, 1);
  }
});

// Only explicit absence permits one bootstrap. Failure after dispatch cannot
// cause another bootstrap, even if the endpoint is still not visible afterward.
test("should bootstrap once for absent owner and never replay uncertainty", async () => {
  for (const code of ["ENOENT", "ECONNREFUSED"]) {
    let connections = 0;
    let bootstraps = 0;
    await connectOwner({ connect: async () => {
      if (++connections === 1) throw Object.assign(new Error("absent"), { code });
    } }, async () => { bootstraps++; });
    assert.equal(connections, 2);
    assert.equal(bootstraps, 1);
  }
  let bootstraps = 0;
  const uncertain = new Error("bootstrap outcome unknown");
  await assert.rejects(connectOwner({ connect: async () => { throw Object.assign(new Error("absent"), { code: "ENOENT" }); } },
    async () => { bootstraps++; throw uncertain; }), error => error === uncertain);
  assert.equal(bootstraps, 1);
});

// A connected owner must be reused without selecting or launching any binary.
test("should reuse connected owner without bootstrapping", async () => {
  let bootstraps = 0;
  await connectOwner({ connect: async () => {} }, async () => { bootstraps++; });
  assert.equal(bootstraps, 0);
});

// The head asks the owner once; a complete identity with matching requirements
// permits reuse without receiving any executable path from the owner.
test("should reuse a complete matching owner identity", async () => {
  const calls: unknown[] = [];
  await ensureOwner({ call: async (method, params, timeoutMs) => {
    calls.push([method, params, timeoutMs]);
    return { ok: true, result: { runtime: ready() } };
  } }, expected);
  assert.deepEqual(calls, [["runtime.ensure", { required_profile_format: 1, required_protocol: 1 }, OWNER_OBSERVATION_TIMEOUT_MS]]);
});

// Empty success, partial identity and a non-ready owner cannot authorize work.
test("should reject incomplete or mismatched ready replies without retry", async () => {
  const replies: Frame[] = [
    {}, { ok: true }, { ok: true, result: {} }, { ok: true, result: { runtime: null } },
    { ok: true, result: { runtime: { state: "ready" } } },
    ...[
      { state: "pending" }, { state: "unknown" }, { code: "runtime_pending_recovery" },
      { protocol: 2 }, { profile_format: 2 }, { pid: 0 }, { pid: 1.5 },
      { activation_id: "" }, { activation_id: "../other" }, { sha256: "" },
      { launch_id: undefined }, { launch_id: null }, { launch_id: "" }, { launch_id: "D".repeat(32) },
      { sha256: "x".repeat(64) }, { revision: "unknown" }, { version: "" },
      { version: "00.2.6" }, { version: "0.2.6-.." },
    ].map(change => ({ ok: true, result: { runtime: { ...ready(), ...change } } })),
    { ok: true, event: "stdout", result: { runtime: ready() } },
  ];
  for (const reply of replies) {
    let calls = 0;
    await assert.rejects(ensureOwner({ call: async () => { calls++; return reply; } }, expected),
      error => error instanceof Error, `accepted ${JSON.stringify(reply)}`);
    assert.equal(calls, 1);
  }
});

// Source/refusal and transport uncertainty remain visible to the caller; the
// head must not retry ensure or silently select another executable.
test("should preserve owner refusal and transport uncertainty", async () => {
  let calls = 0;
  await assert.rejects(ensureOwner({ call: async () => {
    calls++;
    return { ok: false, code: "runtime_source_unverified", message: "unverified owner" };
  } }, expected), /unverified owner/);
  assert.equal(calls, 1);
  const disconnected = new Error("connection lost after request");
  await assert.rejects(ensureOwner({ call: async () => { throw disconnected; } }, expected),
    error => error === disconnected);
});
