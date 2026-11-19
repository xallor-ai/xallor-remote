import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import * as endpoint from "./ensure.js";

// The GUI bridge retains a compatibility lookup, but it cannot select an
// unverified path or inspect the Runtime's managed activation state.
test("head lookup cannot authorize unverified executable paths", () => {
  assert.equal(typeof endpoint.findBinary, "function");
  assert.equal(typeof endpoint.bundledBinary, "function");
  assert.equal("managedRuntime" in endpoint, false);
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "xr-owner-boundary-"));
  const binary = path.join(root, "xallor-remote"), manifest = path.join(root, "manifest.json");
  fs.writeFileSync(binary, "unverified");
  fs.writeFileSync(manifest, JSON.stringify({ build: { version: "0.3.0-rc.1", revision: "a".repeat(40), dirty: false } }));
  const previous = process.env.XALLOR_REMOTE_BIN;
  process.env.XALLOR_REMOTE_BIN = binary;
  try { assert.equal(endpoint.findBinary(process.env), undefined); }
  finally {
    if (previous === undefined) delete process.env.XALLOR_REMOTE_BIN; else process.env.XALLOR_REMOTE_BIN = previous;
    fs.rmSync(root, { recursive: true, force: true });
  }
});

// Incremental tsc otherwise leaves removed modules in dist, and npm ships dist.
test("compiled package excludes the obsolete managed state implementation", () => {
  for (const name of ["managed-runtime", "managed-runtime.test", "ensure.test"]) {
    for (const suffix of [".js", ".js.map", ".d.ts", ".d.ts.map"]) {
      assert.equal(fs.existsSync(new URL("./"+name+suffix, import.meta.url)), false);
    }
  }
});
