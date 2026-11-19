import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createHash } from "node:crypto";
import { MAX_BINARY, MAX_MANIFEST, nativeArtifactObservation } from "./artifact-manifest.js";
import { verifySourceSync, type SourceVerifier } from "./source-trust.js";

export type BootstrapSnapshot = { directory: string; binary: string; sha256: string; assertUnchanged(): void; cleanup(): void };

export class BootstrapCandidateMissing extends Error {}

export class BootstrapCleanupError extends Error {
  readonly code = "runtime_cleanup_uncertain";
  constructor(readonly directory: string, cause: unknown) {
    super(`引导副本清理未确认，已保留：${directory}`, { cause });
  }
}

const READ_FLAGS = fs.constants.O_RDONLY | (process.platform === "win32" ? 0 : fs.constants.O_NONBLOCK | fs.constants.O_NOFOLLOW);

function readManifest(file: string): Buffer {
  const fd = fs.openSync(file, READ_FLAGS);
  try {
    const info = fs.fstatSync(fd);
    if (!info.isFile() || info.size < 1 || info.size > MAX_MANIFEST) throw new Error("Invalid bootstrap manifest size");
    const bytes = Buffer.alloc(MAX_MANIFEST + 1);
    let used = 0;
    while (used < bytes.length) {
      const count = fs.readSync(fd, bytes, used, bytes.length - used, null);
      if (!count) break;
      used += count;
    }
    if (used > MAX_MANIFEST) throw new Error("Bootstrap manifest exceeds size limit");
    return bytes.subarray(0, used);
  } finally { fs.closeSync(fd); }
}

function copyBinary(source: string, target: string): void {
  const input = fs.openSync(source, READ_FLAGS);
  try {
    const info = fs.fstatSync(input);
    if (!info.isFile() || info.size <= 0 || info.size > MAX_BINARY) throw new Error("Invalid bootstrap binary size");
    const output = fs.openSync(target, "wx", 0o700);
    try {
      const chunk = Buffer.alloc(64 * 1024);
      let copied = 0;
      for (;;) {
        const count = fs.readSync(input, chunk, 0, chunk.length, null);
        if (!count) break;
        copied += count;
        if (copied > info.size) throw new Error("Bootstrap binary grew during capture");
        let written = 0;
        while (written < count) {
          const n = fs.writeSync(output, chunk, written, count - written);
          if (n === 0) throw new Error("Bootstrap copy made no progress");
          written += n;
        }
      }
      if (copied !== info.size) throw new Error("Bootstrap binary changed size during capture");
      fs.fsyncSync(output);
    } finally { fs.closeSync(output); }
  } finally { fs.closeSync(input); }
}

// Called only by the trusted bootstrap launcher. Neither the original input
// path nor discovery's prior observation is authority to execute these bytes.
export function captureBootstrap(candidate: string, verify: SourceVerifier = verifySourceSync, temporaryRoot = os.tmpdir()): BootstrapSnapshot {
  // Only absence of the candidate itself permits discovery to advance. Once
  // it is observed, missing metadata or later disappearance is a hard refusal.
  try { fs.lstatSync(candidate); }
  catch (error) {
    const code = (error as NodeJS.ErrnoException).code;
    if (code === "ENOENT" || code === "ENOTDIR") throw new BootstrapCandidateMissing("Bootstrap candidate is absent", { cause: error });
    throw error;
  }
  const source = fs.realpathSync(candidate);
  const originalManifest = fs.realpathSync(path.join(path.dirname(candidate), "manifest.json"));
  const filename = process.platform === "win32" ? "xallor-remote.exe" : "xallor-remote";
  if (path.basename(source) !== filename || path.dirname(source) !== path.dirname(originalManifest)) {
    throw new Error("Bootstrap binary and manifest must be in the same source directory");
  }
  const raw = readManifest(originalManifest);
  const build = JSON.parse(raw.toString("utf8"))?.build ?? {};
  let root: string;
  let directory: string;
  try {
    root = fs.realpathSync(temporaryRoot);
    directory = fs.mkdtempSync(path.join(root, "xr-bootstrap-"));
  } catch (error) {
    throw new Error("无法创建私有引导副本。", { cause: error });
  }
  const owner = fs.lstatSync(directory, { bigint: true });
  const binary = path.join(directory, filename);
  const manifest = path.join(directory, "manifest.json");
  type OwnedFile = { stats: fs.BigIntStats; sha256: string };
  const ownedFiles = new Map<string, OwnedFile>();
  let removed = false;
  const checkOwnedDirectory = () => {
    const current = fs.lstatSync(directory, { bigint: true });
    if (!current.isDirectory() || current.isSymbolicLink() || current.ino !== owner.ino || current.dev !== owner.dev ||
        fs.realpathSync(directory) !== directory || path.dirname(directory) !== root || !path.basename(directory).startsWith("xr-bootstrap-")) {
      throw new Error("Bootstrap snapshot directory ownership changed");
    }
  };
  const cleanup = () => {
    if (removed) return;
    try {
      try { checkOwnedDirectory(); }
      catch (error) {
        if ((error as NodeJS.ErrnoException).code === "ENOENT") { removed = true; return; }
        throw error;
      }
      const files: string[] = [];
      for (const entry of fs.readdirSync(directory)) {
        if (entry !== filename && entry !== "manifest.json") throw new Error("Unexpected file in bootstrap snapshot");
      }
      // Validate every file before touching any permission. Do not recursively
      // remove links or unknown files a process might have put in this directory.
      for (const file of [binary, manifest]) {
        try {
          const current = fs.lstatSync(file, { bigint: true });
          const original = ownedFiles.get(file);
          if (!original || !current.isFile() || current.isSymbolicLink() || current.ino !== original.stats.ino || current.dev !== original.stats.dev || current.nlink !== 1n || current.size !== original.stats.size || createHash("sha256").update(fs.readFileSync(file)).digest("hex") !== original.sha256) {
            throw new Error("Bootstrap snapshot file ownership changed");
          }
          files.push(file);
        }
        catch (error) { if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error; }
      }
      for (const file of files) { fs.chmodSync(file, 0o600); fs.unlinkSync(file); }
      fs.rmdirSync(directory);
      removed = true;
    } catch (error) { throw new BootstrapCleanupError(directory, error); }
  };
  try {
    const manifestFd = fs.openSync(manifest, "wx", 0o600);
    try {
      fs.writeFileSync(manifestFd, raw);
      ownedFiles.set(manifest, { stats: fs.fstatSync(manifestFd, { bigint: true }), sha256: createHash("sha256").update(raw).digest("hex") });
    } finally { fs.closeSync(manifestFd); }
    copyBinary(source, binary);
    ownedFiles.set(binary, { stats: fs.statSync(binary, { bigint: true }), sha256: createHash("sha256").update(fs.readFileSync(binary)).digest("hex") });
    const before = nativeArtifactObservation(binary, manifest, build);
    fs.chmodSync(binary, 0o500);
    fs.chmodSync(manifest, 0o400);
    const excludedRoots = [...new Set([path.dirname(source), fs.realpathSync(path.dirname(candidate))])];
    if (!verify(binary, manifest, build, excludedRoots, {
      binarySHA256: before.binarySHA256,
      manifestSHA256: createHash("sha256").update(before.manifestBytes).digest("hex"),
    })) throw new Error("引导程序来源未通过验证。");
    const assertUnchanged = () => {
      if (removed) throw new Error("Bootstrap snapshot was already released");
      checkOwnedDirectory();
      for (const file of [binary, manifest]) {
        const current = fs.lstatSync(file, { bigint: true });
        const original = ownedFiles.get(file)!;
        if (!current.isFile() || current.isSymbolicLink() || current.ino !== original.stats.ino || current.dev !== original.stats.dev || current.nlink !== 1n || current.size !== original.stats.size || createHash("sha256").update(fs.readFileSync(file)).digest("hex") !== original.sha256) {
          throw new Error("Bootstrap snapshot file ownership changed");
        }
      }
      const current = nativeArtifactObservation(binary, manifest, build);
      if (!current.manifestBytes.equals(before.manifestBytes) || current.binarySHA256 !== before.binarySHA256) {
        throw new Error("Bootstrap snapshot changed after authentication");
      }
    };
    assertUnchanged();
    return Object.freeze({ directory, binary, sha256: before.binarySHA256, assertUnchanged, cleanup });
  } catch (error) {
    try { cleanup(); }
    catch (cleanupError) { throw new BootstrapCleanupError(directory, new AggregateError([error, cleanupError], "Bootstrap verification and cleanup failed")); }
    throw new Error("引导副本准备或验证失败，请检查安装文件。", { cause: error });
  }
}
