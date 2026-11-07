import test from "node:test";
import assert from "node:assert/strict";
import { packageBuild } from "./buildinfo.js";
import { checkRuntimeBuild, guardedArgs } from "./profile-format.js";

// IPC reuse must reject old metadata before peer.add or any other mutation.
test("runtime connection rejects missing or mismatched profile and protocol", () => {
  const build = packageBuild();
  checkRuntimeBuild({ version: build.version, build });
  for (const status of [undefined, {}, { version: build.version },
    { version: build.version, build: { ...build, profile_format: 0 } },
    { version: build.version, build: { ...build, protocol: build.protocol + 1 } }]) {
    assert.throws(() => checkRuntimeBuild(status), /不匹配/);
  }
  assert.deepEqual(guardedArgs(["ensure"]), ["--require-profile-format", String(build.profile_format), "ensure"]);
});
