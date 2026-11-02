import assert from "node:assert/strict";
import test from "node:test";
import {
  bindHostCancel,
  execToolIsError,
  execToolText,
  execFrameText,
  newExecProgress,
  notifyExecProgress,
  progressTokenOf,
  type ToolCtx,
} from "./exec-result.js";

function ctxWith(partial: NonNullable<ToolCtx["mcpReq"]>): ToolCtx {
  return { mcpReq: partial };
}

test("nonzero exit or cancelled/timeout is error", () => {
  assert.equal(execToolIsError("completed", 0), false);
  assert.equal(execToolIsError("completed", 7), true);
  assert.equal(execToolIsError("cancelled", 0), true);
  assert.equal(execToolIsError("timeout", 0), true);
});

// Only an explicit completed status with a numeric zero confirms success.
test("missing or coerced exit values never confirm success", () => {
  for (const value of [null, undefined, false, "0", "", [], NaN, 0.5]) {
    assert.equal(execToolIsError("completed", value), true, String(value));
  }
  for (const status of ["", "unknown", "running"]) assert.equal(execToolIsError(status, 0), true);
});

// Cancellation still preserves the observed output, identity and truncation.
test("cancelled and timeout retain output and terminal metadata", () => {
  for (const status of ["cancelled", "timeout"]) {
    const got = execToolText(status, 1, 10, true, "ex", "out", "diagnostic");
    assert.equal(got.isError, true);
    for (const expected of ["out", "diagnostic", `status=${status}`, "exec_id=ex", "truncated=true"]) assert(got.text.includes(expected));
  }
});

// An untrusted envelope cannot bless even an otherwise successful-looking body.
test("terminal envelopes and malformed results preserve uncertainty", () => {
  const result = { status: "completed", exit_code: 0 };
  for (const frame of [{ result }, { ok: false, result }, { ok: true, result, code: "agent_error" },
    { ok: true, result, event: "stdout" }, { ok: true, result: { status: "completed", exit_code: null } },
    { ok: true, result: { exit_code: 0 } }]) {
    const got = execFrameText(frame, "observed-output", "observed-error", "observed-id");
    assert.equal(got.isError, true);
    assert(got.text.includes("observed-output") && got.text.includes("observed-error"));
  }
  assert.equal(execFrameText({ok:true,result}, "", "").isError, false);
  assert(execFrameText({ok:false,code:"relay_error"}, "", "", "observed-id").text.includes("exec_id=observed-id"));
});

test("completed nonzero still returns protocol exit fields", () => {
  const got = execToolText("completed", 2, 12, false, "ex_1", "hi", "boom");
  assert.equal(got.isError, true);
  assert.match(got.text, /exit_code=2/);
  assert.match(got.text, /exec_id=ex_1/);
  assert.match(got.text, /stderr:\nboom/);
});

test("progress is skipped without token on mcpReq", async () => {
  let sent = 0;
  await notifyExecProgress({ mcpReq: { notify: async () => { sent += 1; } } }, 1, "ex");
  assert.equal(sent, 0);
  assert.equal(progressTokenOf(ctxWith({ _meta: { progressToken: "t1" } })), "t1");
});

test("progress uses mcpReq.notify and increases", async () => {
  const notes: Record<string, unknown>[] = [];
  const report = newExecProgress(
    ctxWith({
      _meta: { progressToken: "tok" },
      notify: async (msg) => {
        notes.push(msg.params);
      },
    }),
  );
  await report("ex_9");
  await report("out");
  assert.equal(notes.length, 2);
  assert.equal(notes[0]?.progressToken, "tok");
  assert.equal(notes[0]?.progress, 1);
  assert.equal(notes[0]?.message, "ex_9");
  assert.equal(notes[1]?.progress, 2);
});

test("host abort on mcpReq.signal cancels after exec_id arrives", () => {
  const ac = new AbortController();
  const seen: string[] = [];
  const onExec = bindHostCancel(ctxWith({ signal: ac.signal }), (id) => seen.push(id));
  ac.abort();
  onExec("ex_late");
  assert.deepEqual(seen, ["ex_late"]);
});

test("host abort cancels live exec via mcpReq.signal", () => {
  const ac = new AbortController();
  const seen: string[] = [];
  const onExec = bindHostCancel(ctxWith({ signal: ac.signal }), (id) => seen.push(id));
  onExec("ex_live");
  ac.abort();
  assert.deepEqual(seen, ["ex_live"]);
});

test("already aborted mcpReq.signal still cancels when exec_id arrives", () => {
  const ac = new AbortController();
  ac.abort();
  const seen: string[] = [];
  const onExec = bindHostCancel(ctxWith({ signal: ac.signal }), (id) => seen.push(id));
  onExec("ex_early");
  assert.deepEqual(seen, ["ex_early"]);
});
