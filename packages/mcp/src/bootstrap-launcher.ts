import { bootstrapCandidates } from "./bootstrap-discovery.js";
import { INSTALL_HINT } from "./ensure.js";
import { BootstrapCandidateMissing, captureBootstrap, type BootstrapSnapshot } from "./bootstrap-snapshot.js";
import { runSnapshot, type BootstrapExit, type BootstrapProcessOptions } from "./bootstrap-process.js";
import { verifySourceSync, type SourceVerifier } from "./source-trust.js";

export type BootstrapLaunchOptions = BootstrapProcessOptions & { packageRoot?: string; temporaryRoot?: string };

// The head supplies argv, never a selected executable path. Missing locations
// may be searched; a present but invalid/unverified source is a hard refusal.
// The verifier argument is dependency wiring for tests, not a CLI/env override.
export async function launchBootstrap(args: readonly string[], options: BootstrapLaunchOptions, verify: SourceVerifier = verifySourceSync): Promise<BootstrapExit> {
  let snapshot: BootstrapSnapshot | undefined;
  for (const candidate of bootstrapCandidates(options.env ?? process.env, options.packageRoot)) {
    try {
      snapshot = captureBootstrap(candidate, verify, options.temporaryRoot);
      break;
    } catch (error) {
      if (!(error instanceof BootstrapCandidateMissing)) throw error;
    }
  }
  if (!snapshot) throw new Error(INSTALL_HINT);
  // No discovery or fallback after the first dispatch, even on an unknown exit.
  return runSnapshot(snapshot, args, options);
}
