import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import { PassThrough } from "node:stream";
import type { ChildProcess } from "node:child_process";
import test from "node:test";
import { runSnapshot } from "./bootstrap-process.js";
import type { BootstrapSnapshot } from "./bootstrap-snapshot.js";

function fixture() {
  let cleaned = 0;
  let imageClosed = 0;
  let killed = 0;
  const child = Object.assign(new EventEmitter(), { pid: 42, stderr: new PassThrough(), stdout: new PassThrough(), stdin: new PassThrough(),
    kill: () => { killed++; return false; }, unref() {} });
  const snapshot: BootstrapSnapshot = { directory: "owned-fixture", binary: "owned-fixture/runtime", sha256: "a".repeat(64), assertUnchanged() {}, cleanup() { cleaned++; } };
  return { child, snapshot, cleaned: () => cleaned, imageClosed: () => imageClosed, killed: () => killed, start: () => child as unknown as ChildProcess,
    prepare: () => ({ executable: snapshot.binary, fd: 3, close() { imageClosed++; } }) };
}

// exit does not mean stdio has closed. Retain the snapshot until close, then
// release it exactly once while preserving the actual exit code.
test("should retain bootstrap snapshot until close rather than exit", async () => {
  const f = fixture();
  const pending = runSnapshot(f.snapshot, [], { stdio: "pipe" }, f.start, f.prepare);
  let resolved = false;
  void pending.then(() => { resolved = true; });
  f.child.emit("exit", 7, null);
  await Promise.resolve();
  try {
    assert.equal(f.cleaned(), 0);
    assert.equal(f.imageClosed(), 0);
    assert.equal(resolved, false);
  } finally { f.child.emit("close", 7, null); }
  const result = await pending;
  assert.equal(result.code, 7);
  assert.equal(f.cleaned(), 1);
  assert.equal(f.imageClosed(), 1);
});

// Failed kill/no-close is an uncertain live process, not permission to remove
// its image. A later close may release it, but must never turn failure to success.
test("should retain snapshot when timeout does not confirm process exit", async () => {
  const f = fixture();
  const pending = runSnapshot(f.snapshot, [], { stdio: "pipe", timeoutMs: 5, stopTimeoutMs: 5 }, f.start, f.prepare);
  await assert.rejects(pending, error => (error as { code?: string }).code === "runtime_cleanup_uncertain");
  assert.equal(f.killed(), 1);
  assert.equal(f.cleaned(), 0);
  assert.equal(f.imageClosed(), 0);
  f.child.emit("close", null, "SIGTERM");
  assert.equal(f.cleaned(), 1);
  assert.equal(f.imageClosed(), 1);
});

// A process whose exit was already observed must not be signalled again while
// waiting for shared stdio closure; the PID could already have been reused.
test("should never kill an exited bootstrap while awaiting stdio closure", async () => {
  const f = fixture();
  const pending = runSnapshot(f.snapshot, [], { stdio: "pipe", timeoutMs: 5, stopTimeoutMs: 5 }, f.start, f.prepare);
  f.child.emit("exit", 0, null);
  await assert.rejects(pending);
  assert.equal(f.killed(), 0);
  assert.equal(f.cleaned(), 0);
  f.child.emit("close", 0, null);
});

// Zero exit cannot hide a retained snapshot. Cleanup uncertainty remains an
// error with a recoverable directory, rather than a successful bootstrap result.
test("should reject successful exit when snapshot cleanup fails", async () => {
  const f = fixture();
  f.snapshot.cleanup = () => { throw new Error("file locked"); };
  const pending = runSnapshot(f.snapshot, [], { stdio: "pipe" }, f.start, f.prepare);
  f.child.emit("exit", 0, null);
  f.child.emit("close", 0, null);
  await assert.rejects(pending, error => (error as { code?: string }).code === "runtime_cleanup_uncertain");
});

// Failure before a child exists still has a quarantine copy to dispose of.
test("should clean snapshot when spawn fails before creating a process", async () => {
  const f = fixture();
  f.child.pid = undefined as unknown as number;
  const pending = runSnapshot(f.snapshot, [], { stdio: "pipe" }, f.start, f.prepare);
  const failure = new Error("spawn refused");
  f.child.emit("error", failure);
  f.child.emit("close", -1, null);
  await assert.rejects(pending, error => error === failure);
  assert.equal(f.cleaned(), 1);
});

// A noisy bootstrap must not retain unbounded stderr while the pipe is drained.
test("should bound stderr while retaining the final diagnostic", async () => {
  const f = fixture();
  const pending = runSnapshot(f.snapshot, [], { stdio: "pipe" }, f.start, f.prepare);
  f.child.stderr.write(Buffer.from("x".repeat(128 * 1024)+"last diagnostic"));
  f.child.emit("exit", 9, null);
  f.child.emit("close", 9, null);
  const result = await pending;
  assert.equal(Buffer.byteLength(result.stderr) <= 64 * 1024, true);
  assert.equal(result.stderr.endsWith("last diagnostic"), true);
});
