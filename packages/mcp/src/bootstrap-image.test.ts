import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createHash } from "node:crypto";
import { spawn } from "node:child_process";
import { once } from "node:events";
import test from "node:test";
import { packageBuild } from "./buildinfo.js";
import { captureBootstrap } from "./bootstrap-snapshot.js";
import { runSnapshot } from "./bootstrap-process.js";
import { prepareBootstrapImage } from "./bootstrap-image.js";

// A deterministic mutation in the final check -> OS spawn gap must either be
// denied or leave the loaded image unchanged. The native Node fixture uses a
// private test verifier; this does not establish release provenance.
test("should execute only authenticated bytes when snapshot is mutated immediately before spawn", { timeout: 60000 }, async () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "xr-bootstrap-image-"));
  const source = path.join(root, "source");
  fs.mkdirSync(source);
  const filename = process.platform === "win32" ? "xallor-remote.exe" : "xallor-remote";
  const binary = path.join(source, filename);
  try {
    fs.copyFileSync(process.execPath, binary);
    const expected = createHash("sha256").update(fs.readFileSync(binary)).digest("hex");
    const build = { ...packageBuild(), dirty: false, revision: "a".repeat(40),
      os: process.platform === "win32" ? "windows" : process.platform,
      arch: process.arch === "arm64" ? "arm64" : "amd64" };
    fs.writeFileSync(path.join(source, "manifest.json"), JSON.stringify({ schema: 1, build,
      artifact: { filename, size: fs.statSync(binary).size, sha256: expected } }));
    const snapshot = captureBootstrap(binary, () => true, root);
    let mutationDenied = false;
    const script = "const fs=require('fs'),c=require('crypto');process.stderr.write(c.createHash('sha256').update(fs.readFileSync(process.platform==='linux'?'/proc/self/exe':process.execPath)).digest('hex'))";
    const result = await runSnapshot(snapshot, ["-e", script], { stdio: "pipe", timeoutMs: 30000, windowsHide: true },
      (executable, args, options) => {
        try {
          fs.chmodSync(snapshot.binary, 0o700);
          fs.appendFileSync(snapshot.binary, "\nchanged-after-authentication");
        } catch (error) {
          if (!["EPERM", "EACCES", "EBUSY"].includes((error as NodeJS.ErrnoException).code ?? "")) throw error;
          mutationDenied = true;
        }
        return spawn(executable, args, options);
      });
    assert.equal(result.code, 0, result.stderr);
    assert.equal(result.stderr, expected, "OS started bytes different from the authenticated image");
    if (process.platform === "win32") assert.equal(mutationDenied, true);
    assert.equal(fs.existsSync(snapshot.directory), false);
  } finally {
    assert.equal(path.dirname(path.resolve(root)), path.resolve(os.tmpdir()));
    assert.ok(path.basename(root).startsWith("xr-bootstrap-image-"));
    fs.rmSync(root, { recursive: true, force: true });
  }
});

// Use a data file, not the child's executable, so Windows' executable section
// cannot accidentally supply the protection under test. The child owns fd 3
// after the parent releases its copy and reads the original bytes on demand.
test("should retain image protection in the child after the parent closes its handle", { timeout: 30000 }, async () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "xr-bootstrap-inherit-"));
  const binary = path.join(root, "image");
  const original = Buffer.from("owned authenticated bytes");
  const expected = createHash("sha256").update(original).digest("hex");
  fs.writeFileSync(binary, original);
  const image = prepareBootstrapImage(binary, expected);
  const script = "process.stdout.write('ready');process.stdin.once('data',()=>{process.stderr.write(require('crypto').createHash('sha256').update(require('fs').readFileSync(3)).digest('hex'));process.exit(0)})";
  const child = spawn(process.execPath, ["-e", script], { windowsHide: true, stdio: ["pipe", "pipe", "pipe", image.fd] });
  const closed = once(child, "close");
  let stderr = "";
  child.stderr!.on("data", data => { stderr += data; });
  try {
    await once(child.stdout!, "data");
    image.close();
    if (process.platform === "win32") {
      assert.throws(() => fs.appendFileSync(binary, "changed"));
      assert.throws(() => fs.renameSync(binary, binary + "-replacement"));
    } else fs.writeFileSync(binary, "changed");
    child.stdin!.end("continue");
    const [code] = await closed;
    assert.equal(code, 0, stderr);
    assert.equal(stderr, expected);
    fs.renameSync(binary, binary + "-released");
  } finally {
    image.close();
    if (child.exitCode === null && child.signalCode === null) { child.kill(); await closed; }
    assert.equal(path.dirname(path.resolve(root)), path.resolve(os.tmpdir()));
    assert.ok(path.basename(root).startsWith("xr-bootstrap-inherit-"));
    fs.rmSync(root, { recursive: true, force: true });
  }
});

// Expected SHA is pinned by source verification. Neither a modified file nor
// a missing expected digest can be admitted as an execution image.
test("should reject mismatched bytes before preparing a bootstrap launch", () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "xr-bootstrap-digest-"));
  const binary = path.join(root, "image");
  fs.writeFileSync(binary, "changed");
  try {
    assert.throws(() => prepareBootstrapImage(binary, "a".repeat(64)), /differs/);
    assert.throws(() => prepareBootstrapImage(binary, ""), /digest/);
    fs.renameSync(binary, binary + "-released");
  } finally {
    assert.equal(path.dirname(path.resolve(root)), path.resolve(os.tmpdir()));
    assert.ok(path.basename(root).startsWith("xr-bootstrap-digest-"));
    fs.rmSync(root, { recursive: true, force: true });
  }
});
