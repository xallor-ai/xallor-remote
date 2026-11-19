import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { createHash } from "node:crypto";
import { nativeArtifactObservation } from "./artifact-manifest.js";

export const TRUST_REPOSITORY = "xallor-ai/xallor-remote";
export const TRUST_WORKFLOW = `${TRUST_REPOSITORY}/.github/workflows/release.yml`;
export const TRUST_ISSUER = "https://token.actions.githubusercontent.com";
export const TRUST_PREDICATE = "https://slsa.dev/provenance/v1";

export type BuildIdentity = { version?: unknown; revision?: unknown; dirty?: unknown };
export type ExpectedSubjects = { binarySHA256: string; manifestSHA256: string };
export type SourceVerifier = (binary: string, manifestPath: string, build: BuildIdentity, excludedRoots?: readonly string[], expected?: ExpectedSubjects) => boolean;

function trustedGh(excludedRoots: readonly string[]): string | undefined {
  const names = process.platform === "win32" ? ["gh.exe", "gh"] : ["gh"];
  const pathValue = process.env.PATH ?? process.env.Path ?? "";
  const separator = process.platform === "win32" ? ";" : ":";
  for (const directory of pathValue.split(separator)) {
    if (!directory) continue;
    for (const name of names) {
      const candidate = path.join(directory, name);
      try {
        const resolved = fs.realpathSync(candidate);
        const excluded = excludedRoots.some(root => {
          const relative = path.relative(root, resolved);
          return relative === "" || (relative !== ".." && !relative.startsWith(".." + path.sep) && !path.isAbsolute(relative));
        });
        if (!excluded && fs.statSync(resolved).isFile()) return resolved;
      } catch { /* continue searching outside the candidate tree */ }
    }
  }
  return undefined;
}

// Candidate keys, manifests, proof files and verifier binaries are never trust roots.
export function verifySourceSync(binary: string, manifestPath: string, build: BuildIdentity, excludedRoots: readonly string[] = [], expected?: ExpectedSubjects): boolean {
  if (typeof build.version !== "string" || typeof build.revision !== "string" || build.dirty !== false ||
      !/^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/.test(build.version) || !/^[a-f0-9]{40}$/.test(build.revision)) return false;
  let root: string;
  try { root = fs.realpathSync(path.dirname(manifestPath)); } catch { return false; }
  const gh = trustedGh([root, ...excludedRoots]);
  if (!gh) return false;
  let manifest: string;
  try { manifest = fs.realpathSync(manifestPath); } catch { return false; }
  if (!manifest.startsWith(root + path.sep)) return false;
  let candidate: string;
  let before: ReturnType<typeof nativeArtifactObservation>;
  try {
    candidate = fs.realpathSync(binary);
    if (path.dirname(candidate) !== root) return false;
    before = nativeArtifactObservation(candidate, manifest, build);
  } catch { return false; }
  const subjects = { binarySHA256: before.binarySHA256, manifestSHA256: createHash("sha256").update(before.manifestBytes).digest("hex") };
  if (expected && (expected.binarySHA256 !== subjects.binarySHA256 || expected.manifestSHA256 !== subjects.manifestSHA256)) return false;
  // A valid binary proof does not authenticate its adjacent metadata. Bind both
  // original subjects to the same issuer, workflow, tag and source commit.
  for (const [artifact, sha256] of [[manifest, subjects.manifestSHA256], [candidate, subjects.binarySHA256]]) {
    const result = spawnSync(gh, ["attestation", "verify", artifact, "--hostname", "github.com", "--repo", TRUST_REPOSITORY,
      "--signer-workflow", TRUST_WORKFLOW, "--cert-oidc-issuer", TRUST_ISSUER, "--predicate-type", TRUST_PREDICATE,
      "--source-ref", `refs/tags/v${build.version}`, "--source-digest", build.revision, "--deny-self-hosted-runners", "--format", "json"],
      { cwd: process.cwd(), stdio: ["ignore", "pipe", "ignore"], timeout: 120_000, maxBuffer: 2 * 1024 * 1024 });
    if (result.error || result.status !== 0) return false;
    try {
      const verified: unknown = JSON.parse(result.stdout.toString("utf8"));
      if (!Array.isArray(verified) || !verified.some(entry => {
        const subjects = entry?.verificationResult?.statement?.subject;
        return Array.isArray(subjects) && subjects.some(subject => subject?.digest?.sha256 === sha256);
      })) return false;
    } catch { return false; }
  }
  try {
    const after = nativeArtifactObservation(candidate, manifest, build);
    if (!after.manifestBytes.equals(before.manifestBytes) || after.binarySHA256 !== before.binarySHA256) return false;
  } catch { return false; }
  return true;
}
