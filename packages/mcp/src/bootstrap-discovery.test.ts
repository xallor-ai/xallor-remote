import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { bootstrapCandidates } from "./bootstrap-discovery.js";
import { vendorPlatformKey } from "./ensure.js";

// State can contain activation-only installs, pending recovery, or corruption.
// Discovery must leave those decisions to Go, not parse another version store.
test("should discover only bootstrap without interpreting managed selection", () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "xr-bootstrap-discovery-"));
  try {
    const profile = path.join(root, "profile");
    fs.mkdirSync(path.join(profile, "runtime"), { recursive: true });
    const binary = path.join(root, "vendor", vendorPlatformKey(), process.platform === "win32" ? "xallor-remote.exe" : "xallor-remote");
    fs.mkdirSync(path.dirname(binary), { recursive: true });
    fs.writeFileSync(binary, "bootstrap fixture");
    fs.writeFileSync(path.join(path.dirname(binary), "manifest.json"), JSON.stringify({ build: { version: "0.2.6" } }));
    const env = { XALLOR_REMOTE_DATA_DIR: profile, PATH: "", XALLOR_REMOTE_BIN: "" };
    for (const state of ["{corrupt", JSON.stringify({ pending: {} }), JSON.stringify({ current: {}, current_activation: "a".repeat(32) })]) {
      fs.writeFileSync(path.join(profile, "runtime", "state.json"), state);
      const result = bootstrapCandidates(env, root);
      assert.equal(result[0], binary);
      assert.equal(result.some(candidate => candidate.includes(path.join("runtime", "versions"))), false);
      assert.equal(result.some(candidate => candidate.includes(path.join("runtime", "activations"))), false);
    }
    // Discovery has performed no authentication and grants no right to spawn.
    assert.equal(fs.readFileSync(binary, "utf8"), "bootstrap fixture");
  } finally {
    assert.equal(path.dirname(path.resolve(root)), path.resolve(os.tmpdir()));
    assert.ok(path.basename(root).startsWith("xr-bootstrap-discovery-"));
    fs.rmSync(root, { recursive: true, force: true });
  }
});
