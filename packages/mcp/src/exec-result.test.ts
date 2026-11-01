import assert from "node:assert/strict";
import test from "node:test";
import {
  bindHostCancel,
  execToolIsError,
  execToolText,
  notifyExecProgress,
  progressTokenOf,
} from "./exec-result.js";

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

test("progress is skipped without token", async () => {
  let sent = 0;
  await notifyExecProgress({ sendNotification: async () => { sent += 1; } }, "ex");
  assert.equal(sent, 0);
  assert.equal(progressTokenOf({ _meta: { progressToken: "t1" } }), "t1");
});

test("progress sends first message when token exists", async () => {
  const notes: Record<string, unknown>[] = [];
  await notifyExecProgress(
    {
      _meta: { progressToken: "tok" },
      sendNotification: async (msg) => {
        notes.push(msg.params);
      },
    },
    "ex_9",
  );
  assert.equal(notes.length, 1);
  assert.equal(notes[0]?.progressToken, "tok");
  assert.equal(notes[0]?.message, "ex_9");
});

test("host abort cancels after exec_id arrives", () => {
  const ac = new AbortController();
  const seen: string[] = [];
  const onExec = bindHostCancel({ signal: ac.signal }, (id) => seen.push(id));
  ac.abort();
  onExec("ex_late");
  assert.deepEqual(seen, ["ex_late"]);
});

test("host abort cancels live exec", () => {
  const ac = new AbortController();
  const seen: string[] = [];
  const onExec = bindHostCancel({ signal: ac.signal }, (id) => seen.push(id));
  onExec("ex_live");
  ac.abort();
  assert.deepEqual(seen, ["ex_live"]);
});
