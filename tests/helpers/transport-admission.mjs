import assert from "node:assert/strict";
import { createHash, randomBytes, randomInt } from "node:crypto";

// Real Relay sockets with a deliberately paused device isolate transport
// occupancy from Runtime job capacity. All connections belong to the fixture.
const url = process.env.XALLOR_TEST_RELAY;
assert(url?.startsWith("ws://127.0.0.1:"));
const links = [];
async function connect(hello) {
  const ws = new WebSocket(url), queue = [], waiters = [];
  const link = {
    send: m => ws.send(JSON.stringify(m)),
    next: () => queue.length ? Promise.resolve(queue.shift()) : new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(Error("Relay frame deadline")), 10000);
      waiters.push(m => { clearTimeout(timer); resolve(m); });
    }),
    close: () => ws.close(),
  };
  links.push(link);
  ws.addEventListener("message", event => {
    const msg = JSON.parse(event.data);
    if (msg.type === "heartbeat") return;
    if (waiters.length) waiters.shift()(msg); else queue.push(msg);
  });
  await new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(Error("Relay connect deadline")), 10000);
    ws.addEventListener("open", () => { clearTimeout(timer); resolve(); }, { once: true });
    ws.addEventListener("error", () => { clearTimeout(timer); reject(Error("Relay connect failed")); }, { once: true });
  });
  link.send(hello);
  assert.equal((await link.next()).type, "hello_ok");
  return link;
}
const id = String(randomInt(100000000, 1000000000));
const secret = randomBytes(32).toString("hex"), grant = randomBytes(32).toString("hex");
async function device() {
  const d = await connect({ type: "hello_device", device_id: id, secret });
  d.send({ type: "device.settings", request_id: "ready", inbound: true, grant_hash: createHash("sha256").update(grant).digest("hex") });
  const ack = await d.next();
  assert.equal(ack.type, "device.settings.result");
  assert(!ack.code);
  return d;
}
const invoke = (c, exec_id) => c.send({ type: "invoke", op: "info", exec_id });
try {
  let d = await device();
  const a = await connect({ type: "hello_client", target_device_id: id, grant });
  const b = await connect({ type: "hello_client", target_device_id: id, grant });
  const routes = [];
  for (let i = 0; i < 16; i++) {
    invoke(a, String(i));
    const routed = await d.next();
    assert.equal(routed.type, "invoke");
    routes.push(routed.exec_id);
  }
  invoke(b, "overflow");
  assert.equal((await b.next()).code, "quota_exceeded");
  a.send({ type: "invoke", op: "cancel", exec_id: "0" });
  const cancel = await d.next();
  assert.equal(cancel.op, "cancel"); assert.equal(cancel.exec_id, routes[0]);
  invoke(b, "cancel-pending");
  assert.equal((await b.next()).code, "quota_exceeded");
  d.send({ type: "exit", exec_id: routes[0], exit_code: 0, status: "completed" });
  assert.equal((await a.next()).exec_id, "0");
  invoke(b, "replacement");
  assert.equal((await d.next()).type, "invoke");
  d.send({ type: "exit", exec_id: routes[0], exit_code: 0, status: "completed" });
  invoke(b, "stale-release");
  assert.equal((await b.next()).code, "quota_exceeded");
  a.close();
  for (let i = 1; i < 16; i++) assert.equal((await d.next()).op, "cancel");
  for (let i = 0; i < 15; i++) {
    invoke(b, "after-drop-" + i);
    assert.equal((await d.next()).type, "invoke");
  }
  invoke(b, "full-again");
  assert.equal((await b.next()).code, "quota_exceeded");
  d.close();
  for (let i = 0; i < 16; i++) assert.equal((await b.next()).code, "device_offline");
  d = await device();
  invoke(b, "after-reconnect");
  const routed = await d.next();
  assert.equal(routed.type, "invoke");
  d.send({ type: "exit", exec_id: routed.exec_id, exit_code: 0, status: "completed" });
  assert.equal((await b.next()).exec_id, "after-reconnect");
  console.log("PASS transport saturation, cancel retention, terminal/drop/reconnect release, stale terminal isolation");
} finally {
  for (const link of links) link.close();
}
