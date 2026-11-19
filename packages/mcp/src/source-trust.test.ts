import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
import os from "node:os";
import path from "node:path";
import childProcess from "node:child_process";
import { syncBuiltinESMExports } from "node:module";
import { createHash } from "node:crypto";
import { packageBuild } from "./buildinfo.js";
import { verifySourceSync } from "./source-trust.js";

// Fake only the external verifier decision, not selection or filesystem behavior.
// This validates orchestration; it is not evidence of real cryptographic signing.
function proofFixture(t: import("node:test").TestContext, rejectedFile?: string, proofOutput?: (artifact: string) => unknown) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "xr-two-proofs-"));
  const candidate = path.join(dir, "candidate");
  const verifier = path.join(dir, "verifier");
  fs.mkdirSync(candidate); fs.mkdirSync(verifier);
  fs.writeFileSync(path.join(verifier, process.platform === "win32" ? "gh.exe" : "gh"), "fixture");
  const filename = process.platform === "win32" ? "xallor-remote.exe" : "xallor-remote";
  const binary = path.join(candidate, filename);
  const manifest = path.join(candidate, "manifest.json");
  const bytes = Buffer.from("binary fixture");
  const build = { ...packageBuild(), revision: "a".repeat(40), dirty: false,
    os: process.platform === "win32" ? "windows" : process.platform,
    arch: process.platform === "win32" ? "amd64" : ({ x64: "amd64", arm64: "arm64" } as Record<string, string>)[process.arch] };
  const metadata = { schema: 1, build, artifact: { filename, size: bytes.length,
    sha256: createHash("sha256").update(bytes).digest("hex") } };
  fs.writeFileSync(binary, bytes);
  fs.writeFileSync(manifest, JSON.stringify(metadata));
  const previous = process.env.PATH;
  process.env.PATH = verifier;
  const calls: string[][] = [];
  const stub = t.mock.method(childProcess, "spawnSync", (_command: unknown, argv: unknown) => {
    const args = argv as string[];
    calls.push([...args]);
    return { status: path.basename(args[2]) === (rejectedFile === "runtime" ? filename : rejectedFile) ? 1 : 0, signal: null,
      pid: 1, output: [], stdout: Buffer.from(JSON.stringify(proofOutput ? proofOutput(args[2]) : [{
        verificationResult: { statement: { subject: [{ digest: { sha256: createHash("sha256").update(fs.readFileSync(args[2])).digest("hex") } }] } },
      }])), stderr: Buffer.alloc(0) };
  });
  syncBuiltinESMExports();
  t.after(() => {
    stub.mock.restore(); syncBuiltinESMExports();
    if (previous === undefined) delete process.env.PATH;
    else process.env.PATH = previous;
    fs.rmSync(dir, { recursive: true, force: true });
  });
  return { binary, manifest, calls, build, metadata, verifier };
}

// Quarantining the binary must not make a verifier from the original untrusted
// tree look like a trusted system tool just because it is outside the copy.
test("should exclude original source tree when verifying a private bootstrap copy", t => {
  const f = proofFixture(t);
  assert.equal(verifySourceSync(f.binary, f.manifest, f.build, [f.verifier]), false);
  assert.equal(f.calls.length, 0);
});

// A successful verifier can have observed an authenticated replacement during
// an ABA swap. Unchanged before/after bytes are insufficient without the exact
// retained digest occurring in the verified statement, not its raw envelope.
test("should reject verifier success for a different subject digest", t => {
  const f = proofFixture(t, undefined, () => [{
    verificationResult: { statement: { subject: [{ digest: { sha256: "b".repeat(64) } }] } },
  }]);
  assert.equal(verifySourceSync(f.binary, f.manifest, f.build), false);
});

// Real release statements may contain duplicate basenames. Search verified
// subjects for exact SHA rather than accepting the first manifest.json.
test("should accept exact digest among multiple verified release subjects", t => {
  const f = proofFixture(t, undefined, artifact => [{
    verificationResult: { statement: { subject: [
      { name: "manifest.json", digest: { sha256: "b".repeat(64) } },
      { name: "manifest.json", digest: { sha256: createHash("sha256").update(fs.readFileSync(artifact)).digest("hex") } },
    ] } },
  }]);
  assert.equal(verifySourceSync(f.binary, f.manifest, f.build), true);
});

// The snapshot caller pins observations before entering the verifier. A
// different file at verifier entry must not replace that expected identity.
test("should refuse a changed caller-pinned subject before invoking gh", t => {
  const f = proofFixture(t);
  assert.equal(verifySourceSync(f.binary, f.manifest, f.build, [], {
    binarySHA256: "b".repeat(64),
    manifestSHA256: createHash("sha256").update(fs.readFileSync(f.manifest)).digest("hex"),
  }), false);
  assert.equal(f.calls.length, 0);
});

// Only the parsed verificationResult is trusted, never unverified raw envelope
// fields, an empty result, a primitive, or an unrelated JSON object.
test("should reject malformed or raw-envelope-only proof output", t => {
  let output: unknown;
  const f = proofFixture(t, undefined, artifact => output === "raw"
    ? [{ attestation: { subject: [{ digest: { sha256: createHash("sha256").update(fs.readFileSync(artifact)).digest("hex") } }] } }]
    : output);
  for (output of [null, [], {}, "invalid", "raw"]) assert.equal(verifySourceSync(f.binary, f.manifest, f.build), false);
});

// Two accepted proof subjects must still describe the same compatible artifact.
test("should_reject_when_individually_verified_files_are_not_a_matching_native_bundle", t => {
  const f = proofFixture(t);
  const original = JSON.stringify(f.metadata);
  for (const alter of [
    (m: any) => { m.artifact.sha256 = "0".repeat(64); },
    (m: any) => { m.artifact.size += 1; },
    (m: any) => { m.artifact.filename = "../runtime"; },
    (m: any) => { m.build.os = "wrong-os"; },
    (m: any) => { m.build.arch = "wrong-arch"; },
    (m: any) => { m.build.profile_format += 1; },
    (m: any) => { m.build.protocol += 1; },
    (m: any) => { m.build.revision = "b".repeat(40); },
    (m: any) => { m.schema = 999; },
    (m: any) => { m.build = null; },
    (m: any) => { m.artifact = null; },
    (m: any) => { m.artifact.size = 256 * 1024 * 1024 + 1; },
    (m: any) => { m.artifact.size = "14"; },
  ]) {
    const changed = JSON.parse(original);
    alter(changed);
    fs.writeFileSync(f.manifest, JSON.stringify(changed));
    assert.equal(verifySourceSync(f.binary, f.manifest, f.build), false, JSON.stringify(changed));
  }
  fs.writeFileSync(f.manifest, original);
  assert.equal(verifySourceSync(f.binary, f.manifest, f.build), true);
});

// Candidate metadata has a fixed bound, and malformed JSON must fail closed.
test("should_reject_malformed_or_oversized_manifest_before_source_verification", t => {
  const f = proofFixture(t);
  for (const raw of ["null", "{broken", JSON.stringify(f.metadata) + " ".repeat(65537)]) {
    fs.writeFileSync(f.manifest, raw);
    assert.equal(verifySourceSync(f.binary, f.manifest, f.build), false);
  }
  assert.equal(f.calls.length, 0);
});

// A valid binary proof must not make an unsigned or altered manifest trusted.
test("should_reject_when_binary_is_verified_but_manifest_proof_fails", t => {
  const f = proofFixture(t, "manifest.json");
  assert.equal(verifySourceSync(f.binary, f.manifest, f.build), false);
});

// A valid manifest proof cannot compensate for a rejected binary proof.
test("should_reject_when_manifest_is_verified_but_binary_proof_fails", t => {
  const f = proofFixture(t, "runtime");
  assert.equal(verifySourceSync(f.binary, f.manifest, f.build), false);
});

// Successful selection requires two subjects bound to the same source policy.
test("should_accept_only_after_both_files_verify_against_the_same_identity", t => {
  const f = proofFixture(t);
  assert.equal(verifySourceSync(f.binary, f.manifest, f.build), true);
  assert.deepEqual(f.calls.map(args => args[2]).sort(), [f.binary, f.manifest].sort());
  for (const args of f.calls) {
    assert.equal(args[args.indexOf("--repo") + 1], "xallor-ai/xallor-remote");
    assert.equal(args[args.indexOf("--hostname") + 1], "github.com");
    assert.equal(args[args.indexOf("--source-ref") + 1], `refs/tags/v${f.build.version}`);
    assert.equal(args[args.indexOf("--source-digest") + 1], f.build.revision);
    assert.equal(args[args.indexOf("--signer-workflow") + 1], "xallor-ai/xallor-remote/.github/workflows/release.yml");
    assert.equal(args[args.indexOf("--cert-oidc-issuer") + 1], "https://token.actions.githubusercontent.com");
    assert.equal(args[args.indexOf("--predicate-type") + 1], "https://slsa.dev/provenance/v1");
    assert.ok(args.includes("--deny-self-hosted-runners"));
  }
});

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
