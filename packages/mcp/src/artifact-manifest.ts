import fs from "node:fs";
import path from "node:path";
import { createHash } from "node:crypto";
import { packageBuild } from "./buildinfo.js";
import { versionsMatch } from "./version.js";
import type { BuildIdentity } from "./source-trust.js";

export const MAX_MANIFEST = 64 * 1024;
export const MAX_BINARY = 256 * 1024 * 1024;
// Opening a FIFO for a blocking read waits before fstat can reject its type.
const READ_FLAGS = fs.constants.O_RDONLY | (process.platform === "win32" ? 0 : fs.constants.O_NONBLOCK);

// Bounded reads avoid allocating a candidate-controlled size. These observations
// bind metadata to bytes; they do not provide an immutable execution snapshot.
export function nativeArtifactObservation(binary: string, manifest: string, identity: BuildIdentity) {
  const manifestFd = fs.openSync(manifest, READ_FLAGS);
  let bytes: Buffer;
  try {
    const info = fs.fstatSync(manifestFd);
    if (!info.isFile() || info.size < 1 || info.size > MAX_MANIFEST) throw Error("Invalid manifest size");
    const buffer = Buffer.alloc(MAX_MANIFEST + 1);
    let length = 0;
    while (length < buffer.length) {
      const count = fs.readSync(manifestFd, buffer, length, buffer.length - length, null);
      if (!count) break;
      length += count;
    }
    if (length > MAX_MANIFEST) throw Error("Manifest exceeds size limit");
    bytes = buffer.subarray(0, length);
  } finally { fs.closeSync(manifestFd); }

  const m = JSON.parse(bytes.toString("utf8"));
  const expected = packageBuild();
  const os = process.platform === "win32" ? "windows" : process.platform;
  // The existing Windows vendor maps ARM64 Node to the x64 executable.
  const arch = process.platform === "win32" && ["x64", "arm64"].includes(process.arch)
    ? "amd64" : ({ x64: "amd64", arm64: "arm64" } as Record<string, string>)[process.arch];
  const filename = process.platform === "win32" ? "xallor-remote.exe" : "xallor-remote";
  if (m?.schema !== 1 || !arch || m.build?.os !== os || m.build?.arch !== arch ||
      m.build?.version !== identity.version || m.build?.revision !== identity.revision ||
      m.build?.dirty !== false || identity.dirty !== false ||
      !versionsMatch(m.build.version) || m.build?.protocol !== expected.protocol ||
      m.build?.profile_format !== expected.profile_format ||
      m.artifact?.filename !== filename || path.basename(binary) !== filename ||
      !Number.isSafeInteger(m.artifact?.size) || m.artifact.size <= 0 || m.artifact.size > MAX_BINARY ||
      !/^[a-f0-9]{64}$/.test(m.artifact?.sha256 ?? "")) throw Error("Incompatible artifact manifest");

  const fd = fs.openSync(binary, READ_FLAGS);
  let digest: string;
  try {
    const info = fs.fstatSync(fd);
    if (!info.isFile() || info.size !== m.artifact.size) throw Error("Artifact size mismatch");
    const buffer = Buffer.alloc(64 * 1024);
    const hash = createHash("sha256");
    let length = 0;
    for (;;) {
      const count = fs.readSync(fd, buffer, 0, buffer.length, null);
      if (!count) break;
      length += count;
      if (length > m.artifact.size) throw Error("Artifact grew during read");
      hash.update(buffer.subarray(0, count));
    }
    digest = hash.digest("hex");
    if (length !== m.artifact.size || digest !== m.artifact.sha256) throw Error("Artifact digest mismatch");
  } finally { fs.closeSync(fd); }
  return { manifestBytes: bytes, binarySHA256: digest };
}

