import assert from "node:assert/strict";
import test from "node:test";
import {
  bindHostCancel,
  execToolIsError,
  execToolText,
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

test("cancelled and timeout keep short user text", () => {
  assert.deepEqual(execToolText("cancelled", 1, 10, false, "ex", "out", ""), {
    text: "任务已取消。",
    isError: true,
  });
  assert.deepEqual(execToolText("timeout", 1, 10, false, "ex", "", ""), {
    text: "执行超时。",
    isError: true,
  });
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
