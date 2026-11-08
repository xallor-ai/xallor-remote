import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
import os from "node:os";
import path from "node:path";
import { verifySourceSync } from "./source-trust.js";

// A missing proof/verifier fails closed; candidate-owned metadata cannot enable it.
test("source verification rejects unsigned candidate metadata", () => {
  assert.equal(verifySourceSync("/tmp/candidate", "/tmp/manifest.json", { version: "0.2.6", revision: "a".repeat(40), dirty: false }), false);
  assert.equal(verifySourceSync("/tmp/candidate", "/tmp/manifest.json", { version: "0.2.6", revision: "unknown", dirty: false }), false);
});

// A candidate-owned gh/gh.exe must not win command resolution on Windows or POSIX.
test("source verification rejects a verifier placed inside the candidate tree", () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "xr-source-gh-"));
  const name = process.platform === "win32" ? "gh.exe" : "gh";
  const verifier = path.join(root, name);
  fs.writeFileSync(verifier, "candidate verifier", { mode: 0o700 });
  const oldPath = process.env.PATH;
  process.env.PATH = root;
  try {
    assert.equal(verifySourceSync(path.join(root, "runtime"), path.join(root, "manifest.json"),
      { version: "0.2.6", revision: "a".repeat(40), dirty: false }), false);
  } finally {
    if (oldPath === undefined) delete process.env.PATH;
    else process.env.PATH = oldPath;
    fs.rmSync(root, { recursive: true, force: true });
  }
});
