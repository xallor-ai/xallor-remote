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
    [{}, "结果待确认"],
  ] as const) {
    const t = task(); finish(t, { id: "1", ok: true, result }); assert.equal(t.status, expected); assert.equal(t.live, false);
  }
  const t = task(); finish(t, { id: "1", ok: false, code: "relay_error" }); assert.equal(t.status, "结果待确认");
});
test("display output stays bounded and preserves truncation from either layer", () => {
  const t = task(); append(t, "x".repeat(300_000)); append(t, "tail");
  assert.equal(t.output.length, 256 * 1024); assert.equal(t.truncated, true);
  finish(t, { id: "1", ok: true, result: { status: "completed", exit_code: 0, truncated: false } });
  assert.equal(t.truncated, true);
});
