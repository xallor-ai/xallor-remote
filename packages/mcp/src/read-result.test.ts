import assert from "node:assert/strict";
import test from "node:test";
import { readToolText } from "./read-result.js";

test("should_keepContentOnly_whenReadIsComplete", () => {
  assert.equal(readToolText("hello", false), "hello");
  assert.equal(readToolText("hello", undefined), "hello");
});

test("should_appendHint_whenReadTruncated", () => {
  const got = readToolText("前缀", true);
  assert.match(got, /^前缀\n\n/);
  assert.match(got, /不是全文/);
  assert.match(got, /offset、limit 或 head\/tail/);
});
