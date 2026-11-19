import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
import * as endpoint from "./ensure.js";

// Delivered heads expose endpoint/layout information, never a second managed
// activation selector or a switch that authorizes unverified candidate paths.
test("head helpers cannot return executable selection authority", () => {
  for (const name of ["findBinary", "bundledBinary", "managedRuntime"]) {
    assert.equal(name in endpoint, false, "obsolete selector is still exported: " + name);
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
