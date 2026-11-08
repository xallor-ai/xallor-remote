import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";

export const TRUST_REPOSITORY = "xallor-ai/xallor-remote";
export const TRUST_WORKFLOW = `${TRUST_REPOSITORY}/.github/workflows/release.yml`;
export const TRUST_ISSUER = "https://token.actions.githubusercontent.com";
export const TRUST_PREDICATE = "https://slsa.dev/provenance/v1";

export type BuildIdentity = { version?: unknown; revision?: unknown; dirty?: unknown };
export type SourceVerifier = (binary: string, manifestPath: string, build: BuildIdentity) => boolean;

function trustedGh(root: string): string | undefined {
  const names = process.platform === "win32" ? ["gh.exe", "gh"] : ["gh"];
  const pathValue = process.env.PATH ?? process.env.Path ?? "";
  const separator = process.platform === "win32" ? ";" : ":";
  for (const directory of pathValue.split(separator)) {
    if (!directory) continue;
    for (const name of names) {
      const candidate = path.join(directory, name);
      try {
        const resolved = fs.realpathSync(candidate);
        if (!resolved.startsWith(root + path.sep) && fs.statSync(resolved).isFile()) return resolved;
      } catch { /* continue searching outside the candidate tree */ }
    }
  }
  return undefined;
}

// Candidate keys, manifests, proof files and verifier binaries are never trust roots.
export function verifySourceSync(binary: string, manifestPath: string, build: BuildIdentity): boolean {
  if (typeof build.version !== "string" || typeof build.revision !== "string" || build.dirty !== false ||
      !/^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/.test(build.version) || !/^[a-f0-9]{40}$/.test(build.revision)) return false;
  let root: string;
  try { root = fs.realpathSync(path.dirname(manifestPath)); } catch { return false; }
  const gh = trustedGh(root);
  if (!gh) return false;
  const result = spawnSync(gh, ["attestation", "verify", binary, "--hostname", "github.com", "--repo", TRUST_REPOSITORY,
    "--signer-workflow", TRUST_WORKFLOW, "--cert-oidc-issuer", TRUST_ISSUER, "--predicate-type", TRUST_PREDICATE,
    "--source-ref", `refs/tags/v${build.version}`, "--source-digest", build.revision, "--deny-self-hosted-runners"],
    { cwd: process.cwd(), stdio: "ignore", timeout: 120_000 });
  if (result.error || result.status !== 0) return false;
  let manifest: string;
  try { manifest = fs.realpathSync(manifestPath); } catch { return false; }
  return manifest.startsWith(root + path.sep) && fs.existsSync(manifest);
}
