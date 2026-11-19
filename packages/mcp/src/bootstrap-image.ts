import fs from "node:fs";
import { createHash } from "node:crypto";
import { MAX_BINARY } from "./artifact-manifest.js";
import { openWindowsImage } from "./bootstrap-image-windows.js";
import { openLinuxImage } from "./bootstrap-image-linux.js";

export type BootstrapImage = { executable: string; fd: number; close(): void };

// The expected digest comes from the authenticated snapshot, never from a
// fresh pathname observation. OS protection must exist before this final hash.
export function prepareBootstrapImage(binary: string, expected: string): BootstrapImage {
  if (!/^[a-f0-9]{64}$/.test(expected)) throw new Error("Invalid authenticated bootstrap digest");
  const fd = process.platform === "win32" ? openWindowsImage(binary)
    : process.platform === "linux" ? openLinuxImage(binary)
    : (() => { throw new Error("This platform does not support protected bootstrap"); })();
  try {
    const info = fs.fstatSync(fd);
    if (!info.isFile() || info.size < 1 || info.size > MAX_BINARY) throw new Error("Invalid bootstrap execution image");
    const digest = createHash("sha256");
    const chunk = Buffer.alloc(64 * 1024);
    let offset = 0;
    for (;;) {
      const count = fs.readSync(fd, chunk, 0, chunk.length, offset);
      if (!count) break;
      offset += count;
      if (offset > MAX_BINARY) throw new Error("Bootstrap execution image grew");
      digest.update(chunk.subarray(0, count));
    }
    if (offset !== info.size || digest.digest("hex") !== expected) throw new Error("Bootstrap execution image differs from authenticated bytes");
    let closed = false;
    return { executable: process.platform === "linux" ? "/proc/self/fd/3" : binary, fd,
      close() { if (!closed) { fs.closeSync(fd); closed = true; } } };
  } catch (error) { fs.closeSync(fd); throw error; }
}
