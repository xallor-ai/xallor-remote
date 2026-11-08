import assert from "node:assert/strict";
import { test } from "node:test";
import { execInputSchema } from "./exec-input.js";

// Agent inputs select one kind; neither obsolete strings nor mixed modes can
// silently dispatch. Program arrays retain empty and literal special arguments.
test("execution inputs preserve arguments and reject ambiguous modes", () => {
  const args = ["", "a b", "$HOME", "`", "中文🌍", 'a"b'];
  assert.deepEqual(execInputSchema.parse({ program: { executable: "git", args } }).program?.args, args);
  assert(execInputSchema.safeParse({ script: { text: "echo hello", shell: "powershell" } }).success);
  for (const backend of ["native_user", "low_privilege", "cgroup", "container", "unknown"]) {
    assert.equal(execInputSchema.parse({ script: { text: "echo hello" }, backend }).backend, backend);
  }
  for (const value of [{}, { command: "echo old" }, { script: { text: "x" }, program: { executable: "git" } },
    { program: { executable: "git", args: ["\0"] } }, { program: { executable: "git", args: ["\ud800"] } },
    { program: { executable: "git", args: [null] } }, { script: { text: "x", env: {} } },
    { script: { text: "中".repeat(65536/3+1) } }, { script: { text: "x" }, timeout_ms: -1 },
    { script: { text: "x" }, backend: null }, { script: { text: "x" }, backend: "x".repeat(65) },
    { script: { text: "x" }, backend: "中".repeat(22) },
    { script: { text: "x".repeat(65536) }, backend: "native_user" }]) {
    assert.equal(execInputSchema.safeParse(value).success, false, JSON.stringify(value).slice(0,100));
  }
});
