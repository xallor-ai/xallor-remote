import fs from "node:fs";
import path from "node:path";
import { createHash } from "node:crypto";
import koffi from "koffi";
import { MAX_BINARY } from "./artifact-manifest.js";

// Sealing precedes the authenticated digest check. Never fall back to executing
// a disk pathname or an unsealed file if the kernel denies this operation.
export function openLinuxImage(binary: string): number {
  const libc = koffi.load(null);
  const create = libc.func("int memfd_create(const char *name, unsigned int flags)");
  const fcntl = libc.func("int fcntl(int fd, int command, ...)");
  const seek = libc.func("int64_t lseek(int fd, int64_t offset, int origin)");
  const name = "xr-runtime-" + createHash("sha256").update(path.resolve(binary)).digest("hex");
  let fd: number = create(name, 0x13);
  if (fd < 0 && koffi.errno() === 22) fd = create(name, 3); // pre-6.3 kernel
  if (fd < 0) throw new Error("Cannot create protected bootstrap image");
  try {
    const input = fs.openSync(binary, fs.constants.O_RDONLY | fs.constants.O_NONBLOCK | fs.constants.O_NOFOLLOW);
    try {
      const info = fs.fstatSync(input);
      if (!info.isFile() || info.size < 1 || info.size > MAX_BINARY) throw new Error("Invalid bootstrap source");
      const chunk = Buffer.alloc(64 * 1024);
      let total = 0;
      for (;;) {
        const count = fs.readSync(input, chunk, 0, chunk.length, null);
        if (!count) break;
        total += count;
        if (total > info.size) throw new Error("Bootstrap source changed size");
        let written = 0;
        while (written < count) {
          const n = fs.writeSync(fd, chunk, written, count - written);
          if (!n) throw new Error("Bootstrap capture made no progress");
          written += n;
        }
      }
      if (total !== info.size) throw new Error("Bootstrap source changed size");
    } finally { fs.closeSync(input); }
    fs.fchmodSync(fd, 0o500);
    let result: number = fcntl(fd, 1033, "int", 0x2f);
    if (result < 0 && koffi.errno() === 22) result = fcntl(fd, 1033, "int", 0xf);
    const seals: number = fcntl(fd, 1034, "int", 0);
    if (result < 0 || seals < 0 || (seals & 0xf) !== 0xf) throw new Error("Bootstrap execution image is not sealed");
    if (seek(fd, 0, 0) !== 0) throw new Error("Cannot rewind bootstrap execution image");
    return fd;
  } catch (error) { fs.closeSync(fd); throw error; }
}
