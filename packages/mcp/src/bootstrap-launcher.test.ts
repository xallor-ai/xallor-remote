import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createHash } from "node:crypto";
import test from "node:test";
import { launchBootstrap } from "./bootstrap-launcher.js";
import { packageBuild } from "./buildinfo.js";
import { vendorPlatformKey } from "./ensure.js";

// A present first binary without its manifest is unverified, not absent. It
// must not silently select a later vendor candidate, even before any dispatch.
test("should refuse present bootstrap with missing manifest without trying another candidate", async () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "xr-bootstrap-refusal-"));
  try {
    const filename = process.platform === "win32" ? "xallor-remote.exe" : "xallor-remote";
    const first = path.join(root, "first", filename);
    fs.mkdirSync(path.dirname(first), { recursive: true });
    fs.writeFileSync(first, "present but missing manifest");
    const second = path.join(root, "vendor", vendorPlatformKey(), filename);
    fs.mkdirSync(path.dirname(second), { recursive: true });
    const bytes = Buffer.from("second candidate");
    fs.writeFileSync(second, bytes);
    const build = { ...packageBuild(), dirty: false, revision: "a".repeat(40),
      os: process.platform === "win32" ? "windows" : process.platform,
      arch: process.platform === "win32" ? "amd64" : process.arch === "arm64" ? "arm64" : "amd64" };
    fs.writeFileSync(path.join(path.dirname(second), "manifest.json"), JSON.stringify({ schema: 1, build,
      artifact: { filename, size: bytes.length, sha256: createHash("sha256").update(bytes).digest("hex") } }));
    const calls: string[] = [];
    await assert.rejects(launchBootstrap([], {
      env: { XALLOR_REMOTE_BIN: first, XALLOR_REMOTE_DATA_DIR: path.join(root, "profile"), PATH: "" },
      packageRoot: root, temporaryRoot: root, stdio: "pipe",
    }, file => { calls.push(file); return false; }));
    assert.deepEqual(calls, [], "a later candidate reached verification after a present source was unverified");
  } finally {
    assert.equal(path.dirname(path.resolve(root)), path.resolve(os.tmpdir()));
    assert.ok(path.basename(root).startsWith("xr-bootstrap-refusal-"));
    fs.rmSync(root, { recursive: true, force: true });
  }
});

// Use a real native Node executable as a process fixture, with a private fake
// verifier only for wiring. This does not claim Node is a signed Runtime bundle.
test("should execute captured bytes after original replacement and remove only the closed snapshot", { timeout: 60000 }, async () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "xr-bootstrap-process-"));
  const source = path.join(root, "source");
  const temporary = path.join(root, "private");
  fs.mkdirSync(source); fs.mkdirSync(temporary);
  const filename = process.platform === "win32" ? "xallor-remote.exe" : "xallor-remote";
  const binary = path.join(source, filename);
  let captured = "";
  try {
    fs.copyFileSync(process.execPath, binary);
    const hash = createHash("sha256");
    const input = fs.openSync(binary, "r");
    try {
      const chunk = Buffer.alloc(64 * 1024);
      for (;;) {
        const count = fs.readSync(input, chunk, 0, chunk.length, null);
        if (!count) break;
        hash.update(chunk.subarray(0, count));
      }
    } finally { fs.closeSync(input); }
    const build = { ...packageBuild(), dirty: false, revision: "a".repeat(40),
      os: process.platform === "win32" ? "windows" : process.platform,
      arch: process.platform === "win32" ? "amd64" : process.arch === "arm64" ? "arm64" : "amd64" };
    const expectedDigest = hash.digest("hex");
    fs.writeFileSync(path.join(source, "manifest.json"), JSON.stringify({ schema: 1, build, artifact: {
      filename, size: fs.statSync(binary).size, sha256: expectedDigest } }));
    const result = await launchBootstrap(["-e", "process.stderr.write(JSON.stringify({executable:process.execPath,argv0:process.argv0,sha256:require('crypto').createHash('sha256').update(require('fs').readFileSync(process.platform==='linux'?'/proc/self/exe':process.execPath)).digest('hex')}));setTimeout(()=>process.exit(7),30)"],
      { env: { ...process.env, XALLOR_REMOTE_BIN: binary, XALLOR_REMOTE_DATA_DIR: path.join(root, "profile"), PATH: "" },
        packageRoot: root, temporaryRoot: temporary, stdio: "pipe", windowsHide: true, timeoutMs: 30000 },
      (copy, _manifest, _build, excludedRoots) => {
        assert.notEqual(copy, binary);
        assert.ok(excludedRoots?.includes(fs.realpathSync(source)));
        captured = path.dirname(copy);
        fs.writeFileSync(binary, "original path now contains a different program");
        return true;
      });
    assert.equal(result.code, 7);
    assert.equal(result.signal, null);
    const observed = JSON.parse(result.stderr);
    assert.equal(observed.sha256, expectedDigest);
    assert.equal(observed.argv0, path.join(captured, filename));
    if (process.platform === "linux") {
      const label = "/memfd:xr-runtime-" + createHash("sha256").update(path.join(captured, filename)).digest("hex");
      assert.ok(observed.executable === label || observed.executable === label + " (deleted)", observed.executable);
    } else assert.equal(path.dirname(observed.executable), captured);
    assert.equal(fs.existsSync(captured), false);
    assert.deepEqual(fs.readdirSync(temporary), []);
    assert.equal(fs.readFileSync(binary, "utf8"), "original path now contains a different program");
  } finally {
    assert.equal(path.dirname(path.resolve(root)), path.resolve(os.tmpdir()));
    assert.ok(path.basename(root).startsWith("xr-bootstrap-process-"));
    fs.rmSync(root, { recursive: true, force: true });
  }
});
