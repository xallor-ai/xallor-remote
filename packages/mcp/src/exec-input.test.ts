import assert from "node:assert/strict";
import { test } from "node:test";
import { execInputSchema } from "./exec-input.js";

// Agent inputs select one kind; neither obsolete strings nor mixed modes can
// silently dispatch. Program arrays retain empty and literal special arguments.
test("execution inputs preserve arguments and reject ambiguous modes", () => {
  const args = ["", "a b", "$HOME", "`", "中文🌍", 'a"b'];
  assert.deepEqual(execInputSchema.parse({ program: { executable: "git", args } }).program?.args, args);
  assert(execInputSchema.safeParse({ script: { text: "echo hello", shell: "powershell" } }).success);
  for (const value of [{}, { command: "echo old" }, { script: { text: "x" }, program: { executable: "git" } },
    { program: { executable: "git", args: ["\0"] } }, { program: { executable: "git", args: ["\ud800"] } },
    { program: { executable: "git", args: [null] } }, { script: { text: "x", env: {} } },
    { script: { text: "中".repeat(65536/3+1) } }, { script: { text: "x" }, timeout_ms: -1 }]) {
    assert.equal(execInputSchema.safeParse(value).success, false, JSON.stringify(value).slice(0,100));
  }
});
