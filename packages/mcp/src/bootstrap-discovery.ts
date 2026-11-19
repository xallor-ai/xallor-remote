import path from "node:path";
import { binaryNames, dataDir, packageRoot, vendorPlatformKey } from "./ensure.js";

// These are untrusted locations, never execution authority. The launcher must
// capture and authenticate a private copy; discovery reads no candidate bytes
// or managed state. Go alone chooses the installed activation.
export function bootstrapCandidates(env: NodeJS.ProcessEnv = process.env, root = packageRoot()): string[] {
  const candidates: string[] = [];
  if (env.XALLOR_REMOTE_BIN) candidates.push(env.XALLOR_REMOTE_BIN);
  for (const name of binaryNames()) candidates.push(path.join(root, "vendor", vendorPlatformKey(), name));
  const value = Object.prototype.hasOwnProperty.call(env, "PATH") ? (env.PATH ?? "") : (env.Path ?? "");
  for (const directory of value.split(path.delimiter)) {
    if (directory) for (const name of binaryNames()) candidates.push(path.join(directory, name));
  }
  for (const name of binaryNames()) candidates.push(path.join(dataDir(env), "bin", name));
  return [...new Set(candidates)];
}
