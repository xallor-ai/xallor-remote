import test from "node:test";
import assert from "node:assert/strict";
import { append, finish, type Task } from "../src/task-state";
const task = (): Task => ({ id: "1", device: "123456789", command: "test", output: "", live: true, truncated: false, status: "", cancelRequested: false });
test("terminal certainty preserves nonzero, cancel, timeout and unknown results", () => {
  for (const [result, expected] of [
    [{ status: "completed", exit_code: 0 }, "已完成"],
    [{ status: "completed", exit_code: 7 }, "执行失败（退出码 7）"],
    [{ status: "completed" }, "结果待确认"],
    [{ status: "cancelled" }, "已取消"],
    [{ status: "timeout" }, "已超时"],
    [{ status: "authorization_lost", exit_code: 1 }, "授权失效，已停止"],
    [{}, "结果待确认"],
  ] as const) {
    const t = task(); finish(t, { id: "1", ok: true, result }); assert.equal(t.status, expected); assert.equal(t.live, false);
  }
  const t = task(); finish(t, { id: "1", ok: false, code: "relay_error" }); assert.equal(t.status, "结果待确认");
  const unknown = task(); finish(unknown, { id: "1", ok: false, code: "result_unknown" }); assert.equal(unknown.status, "结果待确认");
  const rejected = task(); finish(rejected, { id: "1", ok: false, code: "unauthorized" }); assert.equal(rejected.status, "执行被拒绝");
  const interrupted = task(); append(interrupted, "already ran\n"); finish(interrupted, { id: "1", ok: false, code: "unauthorized" }); assert.equal(interrupted.status, "结果待确认");
});

// Revocation is a confirmed stop after execution, retaining the output already shown.
test("authorization loss preserves partial output and confirmed stop", () => {
  const t = task(); append(t, "already executed\n");
  finish(t, { id: "1", ok: true, result: { status: "authorization_lost", exit_code: 1, truncated: true } });
  assert.equal(t.status, "授权失效，已停止");
  assert.equal(t.output, "already executed\n");
  assert.equal(t.truncated, true); assert.equal(t.live, false);
});
// A route error can precede cleanup after the command has produced output.
test("interrupted execution keeps its output, identity and uncertain outcome", () => {
  for (const code of ["unauthorized", "device_offline", "relay_error", "result_unknown"]) {
    const t = task(); t.execID = "submitted-exec"; append(t, "already executed\n");
    finish(t, { id: "1", ok: false, code, message: "route retired" });
    assert.equal(t.status, "结果待确认"); assert.equal(t.live, false);
    assert.equal(t.output, "already executed\n"); assert.equal(t.execID, "submitted-exec");
    assert.equal(t.error, "route retired"); assert.equal(t.cancelRequested, false);
  }
});
test("display output stays bounded and preserves truncation from either layer", () => {
  const t = task(); append(t, "x".repeat(300_000)); append(t, "tail");
  assert.equal(t.output.length, 256 * 1024); assert.equal(t.truncated, true);
  finish(t, { id: "1", ok: true, result: { status: "completed", exit_code: 0, truncated: false } });
  assert.equal(t.truncated, true);
});
