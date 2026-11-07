import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createHash } from "node:crypto";
import { managedRuntime } from "./managed-runtime.js";
import { dataDir, ipcPath } from "./ensure.js";
import { packageBuild } from "./buildinfo.js";

// Launch only verified installed bytes; fail before executing altered state.
test("managed selection rejects corruption, traversal and pending activation", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "xr-managed-"));
  try {
    const bytes = Buffer.from("test artifact");
    const sha256 = createHash("sha256").update(bytes).digest("hex");
    const filename = process.platform === "win32" ? "xallor-remote.exe" : "xallor-remote";
    const bin = path.join(dir, "runtime", "versions", sha256, filename);
    fs.mkdirSync(path.dirname(bin), { recursive: true });
    fs.writeFileSync(bin, bytes);
    const current = { schema: 1, build: { ...packageBuild(), os: process.platform.replace("win32", "windows"), arch: process.arch === "x64" ? "amd64" : process.arch },
      artifact: { filename, sha256, size: bytes.length } };
    const statePath = path.join(dir, "runtime", "state.json");
    const save = (state: object) => fs.writeFileSync(statePath, JSON.stringify(state));
    save({ current });
    assert.equal(managedRuntime(dir), bin);
    for (const profile_format of [undefined, 0, current.build.profile_format + 1]) {
      save({ current: { ...current, build: { ...current.build, profile_format } } });
      assert.throws(() => managedRuntime(dir), /无效/);
    }
    save({ current, pending: current });
    assert.throws(() => managedRuntime(dir), /rollback/);
    save({ current: { ...current, artifact: { ...current.artifact, filename: "../escape" } } });
    assert.throws(() => managedRuntime(dir), /无效/);
    save({ current });
    fs.writeFileSync(bin, "corrupted");
    assert.throws(() => managedRuntime(dir), /校验失败/);
  } finally {
    assert.ok(path.basename(dir).startsWith("xr-managed-"));
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

// Both runtimes must use the same normalized profile-to-endpoint algorithm.
test("profile isolates data and matches the Go IPC endpoint convention", () => {
  const dir = path.resolve(os.tmpdir(), "xr-profile");
  const env = { XALLOR_REMOTE_DATA_DIR: dir };
  assert.equal(dataDir(env), dir);
  const expected = process.platform === "win32"
    ? "\\\\.\\pipe\\XallorRemote-" + createHash("sha256").update(dir.toLowerCase()).digest("hex").slice(0, 16)
    : path.join(dir, "ipc.sock");
  assert.equal(ipcPath(env), expected);
  assert.throws(() => dataDir({ XALLOR_REMOTE_DATA_DIR: "relative" }));
});
