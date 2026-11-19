import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { execFileSync, spawnSync } from "node:child_process";
import test from "node:test";
import { packageBuild } from "./buildinfo.js";

// A FIFO with no writer must be rejected without blocking the selecting process.
// Run in a bounded child so the regression itself cannot hang the test suite.
test("should_reject_fifo_candidates_without_waiting_for_a_writer", { skip: process.platform === "win32" }, () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "xr-artifact-fifo-"));
  const binary = path.join(dir, "xallor-remote");
  const manifest = path.join(dir, "manifest.json");
  const build = { ...packageBuild(), revision: "a".repeat(40), dirty: false, os: process.platform,
    arch: process.arch === "arm64" ? "arm64" : "amd64" };
  try {
    for (const subject of [binary, manifest]) {
      fs.writeFileSync(binary, "candidate");
      fs.writeFileSync(manifest, JSON.stringify({ schema: 1, build, artifact: {
        filename: "xallor-remote", size: 9, sha256: "0".repeat(64) } }));
      fs.unlinkSync(subject);
      execFileSync("mkfifo", [subject]);
      const code = `import { nativeArtifactObservation } from ${JSON.stringify(new URL("./artifact-manifest.js", import.meta.url).href)};
        try { nativeArtifactObservation(${JSON.stringify(binary)}, ${JSON.stringify(manifest)}, ${JSON.stringify(build)}); process.exitCode = 1; }
        catch { process.exitCode = 0; }`;
      const result = spawnSync(process.execPath, ["--input-type=module", "--eval", code], { timeout: 3000 });
      assert.equal(result.error, undefined, "candidate open blocked: " + subject);
      assert.equal(result.status, 0, result.stderr?.toString());
      fs.unlinkSync(subject);
    }
  } finally { fs.rmSync(dir, { recursive: true, force: true }); }
});

// Bootstrap discovery must not reintroduce a blocking pre-verification read;
// both source binary and manifest FIFOs are refused by the capture boundary.
test("should reject bootstrap FIFO inputs before verification or execution", { skip: process.platform === "win32" }, () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "xr-bootstrap-fifo-"));
  const binary = path.join(dir, "xallor-remote");
  const manifest = path.join(dir, "manifest.json");
  const build = { ...packageBuild(), revision: "a".repeat(40), dirty: false, os: process.platform,
    arch: process.arch === "arm64" ? "arm64" : "amd64" };
  try {
    for (const subject of [binary, manifest]) {
      fs.writeFileSync(binary, "candidate");
      fs.writeFileSync(manifest, JSON.stringify({ schema: 1, build, artifact: {
        filename: "xallor-remote", size: 9, sha256: "0".repeat(64) } }));
      fs.unlinkSync(subject);
      execFileSync("mkfifo", [subject]);
      const code = `import { captureBootstrap } from ${JSON.stringify(new URL("./bootstrap-snapshot.js", import.meta.url).href)};
        let verified = false;
        try { captureBootstrap(${JSON.stringify(binary)}, () => { verified = true; return true; }, ${JSON.stringify(dir)}); process.exitCode = 1; }
        catch { process.exitCode = verified ? 2 : 0; }`;
      const result = spawnSync(process.execPath, ["--input-type=module", "--eval", code], { timeout: 3000 });
      assert.equal(result.error, undefined, "bootstrap capture blocked: " + subject);
      assert.equal(result.status, 0, result.stderr?.toString());
      fs.unlinkSync(subject);
    }
  } finally {
    assert.equal(path.dirname(path.resolve(dir)), path.resolve(os.tmpdir()));
    assert.ok(path.basename(dir).startsWith("xr-bootstrap-fifo-"));
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

