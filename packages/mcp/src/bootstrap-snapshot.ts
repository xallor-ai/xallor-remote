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

function copyBinary(source: string, target: string, created: (info: fs.BigIntStats) => void): void {
  const input = fs.openSync(source, READ_FLAGS);
  try {
    const info = fs.fstatSync(input);
    if (!info.isFile() || info.size <= 0 || info.size > MAX_BINARY) throw new Error("Invalid bootstrap binary size");
    const output = fs.openSync(target, "wx", 0o700);
    try {
      created(fs.fstatSync(output, { bigint: true }));
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
  // Linux permits removing empty directories while their descriptors are
  // open. Pin the directory too, otherwise its inode could also be reused.
  const directoryFd = process.platform === "linux" ? fs.openSync(directory, READ_FLAGS | fs.constants.O_DIRECTORY) : undefined;
  const owner = directoryFd === undefined ? fs.lstatSync(directory, { bigint: true }) : fs.fstatSync(directoryFd, { bigint: true });
  const binary = path.join(directory, filename);
  const manifest = path.join(directory, "manifest.json");
  const ownedFiles = new Map<string, { info: fs.BigIntStats; fd: number }>();
  // Keep read handles to the actual created objects for their full lifetime.
  // A stat tuple alone can be reused after unlink; a live inode cannot. Read
  // handles also avoid Linux ETXTBSY when the captured binary is executed.
  const ownFile = (file: string, info: fs.BigIntStats) => {
    const fd = fs.openSync(file, READ_FLAGS);
    const pinned = fs.fstatSync(fd, { bigint: true });
    if (!pinned.isFile() || pinned.ino !== info.ino || pinned.dev !== info.dev || pinned.nlink !== 1n) {
      fs.closeSync(fd);
      throw new Error("Bootstrap snapshot file ownership changed during capture");
    }
    ownedFiles.set(file, { info, fd });
  };
  let released = false;
  const releaseHandles = () => {
    if (released) return;
    released = true;
    for (const { fd } of ownedFiles.values()) fs.closeSync(fd);
    if (directoryFd !== undefined) fs.closeSync(directoryFd);
  };
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
    if (released) throw new BootstrapCleanupError(directory, new Error("Bootstrap ownership handles were released after failed cleanup"));
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
      // Pins prevent inode reuse; they do not make pathname unlink atomic with
      // this check. Concurrent name swaps remain outside the cleanup guarantee
      // documented in docs/install-trust-design.md.
      for (const file of [binary, manifest]) {
        try {
          const current = fs.lstatSync(file, { bigint: true });
          const original = ownedFiles.get(file)?.info;
          if (!original || !current.isFile() || current.isSymbolicLink() || current.ino !== original.ino || current.dev !== original.dev || current.nlink !== 1n) {
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
    finally { releaseHandles(); }
  };
  try {
    const manifestFd = fs.openSync(manifest, "wx", 0o600);
    try {
      ownFile(manifest, fs.fstatSync(manifestFd, { bigint: true }));
      fs.writeFileSync(manifestFd, raw);
    } finally { fs.closeSync(manifestFd); }
    copyBinary(source, binary, info => ownFile(binary, info));
    const before = nativeArtifactObservation(binary, manifest, build);
    fs.chmodSync(binary, 0o500);
    fs.chmodSync(manifest, 0o400);
    const excludedRoots = [...new Set([path.dirname(source), fs.realpathSync(path.dirname(candidate))])];
    if (!verify(binary, manifest, build, excludedRoots, {
      binarySHA256: before.binarySHA256,
      manifestSHA256: createHash("sha256").update(before.manifestBytes).digest("hex"),
    })) throw new Error("引导程序来源未通过验证。");
    const assertUnchanged = () => {
      if (removed || released) throw new Error("Bootstrap snapshot was already released");
      checkOwnedDirectory();
      for (const file of [binary, manifest]) {
        const current = fs.lstatSync(file, { bigint: true });
        const original = ownedFiles.get(file)!.info;
        if (!current.isFile() || current.isSymbolicLink() || current.ino !== original.ino || current.dev !== original.dev || current.nlink !== 1n) {
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
