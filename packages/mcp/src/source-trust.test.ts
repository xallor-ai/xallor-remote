import assert from "node:assert/strict";
import test from "node:test";
import { verifySourceSync } from "./source-trust.js";

// A missing proof/verifier fails closed; candidate-owned metadata cannot enable it.
test("source verification rejects unsigned candidate metadata", () => {
  assert.equal(verifySourceSync("/tmp/candidate", "/tmp/manifest.json", { version: "0.2.6", revision: "a".repeat(40), dirty: false }), false);
  assert.equal(verifySourceSync("/tmp/candidate", "/tmp/manifest.json", { version: "0.2.6", revision: "unknown", dirty: false }), false);
});
