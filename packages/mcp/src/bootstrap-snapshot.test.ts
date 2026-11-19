import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createHash } from "node:crypto";
import test, { type TestContext } from "node:test";
import { packageBuild } from "./buildinfo.js";
import { captureBootstrap } from "./bootstrap-snapshot.js";

function fixture(t: TestContext) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "xr-snapshot-test-"));
  const source = path.join(root, "source");
  const temporary = path.join(root, "private");
  fs.mkdirSync(source); fs.mkdirSync(temporary);
  const filename = process.platform === "win32" ? "xallor-remote.exe" : "xallor-remote";
  const binary = path.join(source, filename);
  const bytes = Buffer.from("authenticated snapshot fixture");
  const build = { ...packageBuild(), dirty: false, revision: "a".repeat(40),
    os: process.platform === "win32" ? "windows" : process.platform,
    arch: process.platform === "win32" ? "amd64" : process.arch === "arm64" ? "arm64" : "amd64" };
  const manifestBytes = Buffer.from(JSON.stringify({ schema: 1, build, artifact: {
    filename, size: bytes.length, sha256: createHash("sha256").update(bytes).digest("hex") } }, null, 2)+"\n");
  fs.writeFileSync(binary, bytes);
  fs.writeFileSync(path.join(source, "manifest.json"), manifestBytes);
  t.after(() => {
    assert.equal(path.dirname(path.resolve(root)), path.resolve(os.tmpdir()));
    assert.ok(path.basename(root).startsWith("xr-snapshot-test-"));
    fs.rmSync(root, { recursive: true, force: true });
  });
  return { root, source, temporary, binary, bytes, manifestBytes };
}

// Replace the untrusted input while its private copy is being authenticated.
// Execution authority must refer only to preserved bytes and original manifest.
test("should retain private bootstrap bytes when original path changes during verification", t => {
  const f = fixture(t);
  const snapshot = captureBootstrap(f.binary, (_binary, _manifest, _build, excludedRoots) => {
    assert.ok(excludedRoots?.includes(fs.realpathSync(f.source)));
    fs.writeFileSync(f.binary, "replacement at original path");
    return true;
  }, f.temporary);
  assert.notEqual(snapshot.binary, f.binary);
  assert.deepEqual(fs.readFileSync(snapshot.binary), f.bytes);
  assert.deepEqual(fs.readFileSync(path.join(snapshot.directory, "manifest.json")), f.manifestBytes);
  snapshot.assertUnchanged();
  snapshot.cleanup();
  assert.equal(fs.existsSync(snapshot.directory), false);
  assert.equal(fs.readFileSync(f.binary, "utf8"), "replacement at original path");
});

// Refused proof cannot leave a runnable private candidate or change the source.
test("should discard private snapshot when bootstrap proof is rejected", t => {
  const f = fixture(t);
  assert.throws(() => captureBootstrap(f.binary, () => false, f.temporary));
  assert.deepEqual(fs.readdirSync(f.temporary), []);
  assert.deepEqual(fs.readFileSync(f.binary), f.bytes);
});

// The verifier decision does not waive byte/manifest binding. A controlled
// mutation of the private file itself must fail its post-verification check.
test("should reject changed private bytes after verifier returns", t => {
  const f = fixture(t);
  assert.throws(() => captureBootstrap(f.binary, privateBinary => {
    fs.chmodSync(privateBinary, 0o700);
    fs.writeFileSync(privateBinary, "changed private image");
    return true;
  }, f.temporary));
  assert.deepEqual(fs.readdirSync(f.temporary), []);
  assert.deepEqual(fs.readFileSync(f.binary), f.bytes);
});

// Cleanup may remove only the originally created directory. A replacement at
// the same pathname is another object and must survive a refused cleanup.
test("should retain a substituted directory rather than deleting unowned files", t => {
  const f = fixture(t);
  const snapshot = captureBootstrap(f.binary, () => true, f.temporary);
  try { fs.renameSync(snapshot.directory, snapshot.directory+".retained"); }
  catch (error) {
    // Windows refuses the directory rename while its child handles are held.
    // This proves prevention rather than the Linux post-substitution refusal.
    if (process.platform !== "win32" || !["EPERM", "EACCES", "EBUSY"].includes((error as NodeJS.ErrnoException).code ?? "")) throw error;
    snapshot.assertUnchanged();
    snapshot.cleanup();
    assert.equal(fs.existsSync(snapshot.directory), false);
    return;
  }
  fs.mkdirSync(snapshot.directory);
  const sentinel = path.join(snapshot.directory, "unowned.txt");
  fs.writeFileSync(sentinel, "keep");
  assert.throws(() => snapshot.cleanup());
  assert.equal(fs.readFileSync(sentinel, "utf8"), "keep");
});

// In-place changes retain physical ownership but must lose execution authority.
// Cleanup must still reclaim that original object without a hash-based bypass.
test("should reclaim the original inode after rejecting mutated execution bytes", t => {
  const f = fixture(t);
  const snapshot = captureBootstrap(f.binary, () => true, f.temporary);
  fs.chmodSync(snapshot.binary, 0o700);
  fs.appendFileSync(snapshot.binary, "changed");
  assert.throws(() => snapshot.assertUnchanged(), /Artifact size mismatch/);
  snapshot.cleanup();
  assert.equal(fs.existsSync(snapshot.directory), false);
});

// A refused cleanup releases its handles and must never reacquire ownership
// from path metadata on retry, even if a caller later removes foreign content.
test("should refuse cleanup retry after ownership evidence has been released", t => {
  const f = fixture(t);
  const snapshot = captureBootstrap(f.binary, () => true, f.temporary);
  const foreign = path.join(snapshot.directory, "foreign");
  fs.writeFileSync(foreign, "keep");
  assert.throws(() => snapshot.cleanup());
  fs.unlinkSync(foreign);
  assert.throws(() => snapshot.assertUnchanged(), /released/);
  assert.throws(() => snapshot.cleanup());
  assert.deepEqual(fs.readFileSync(snapshot.binary), f.bytes);
});

// Unexpected contents and replaced/linked files are not ownership evidence.
// Refusing cleanup must leave both their bytes and permissions untouched.
for (const kind of ["unexpected-entry", "replaced-file", "hard-linked-file"]) {
  test(`should refuse cleanup of ${kind} without touching unowned data`, t => {
    const f = fixture(t);
    const snapshot = captureBootstrap(f.binary, () => true, f.temporary);
    let unowned: string;
    if (kind === "unexpected-entry") {
      unowned = path.join(snapshot.directory, "foreign.txt");
      fs.writeFileSync(unowned, "keep");
    } else {
      fs.chmodSync(snapshot.binary, 0o700);
      fs.unlinkSync(snapshot.binary);
      if (kind === "hard-linked-file") {
        fs.linkSync(f.binary, snapshot.binary);
        unowned = f.binary;
      } else {
        fs.writeFileSync(snapshot.binary, "replacement");
        unowned = snapshot.binary;
      }
    }
    fs.chmodSync(unowned, 0o400);
    const before = fs.readFileSync(unowned);
    const mode = fs.statSync(unowned).mode;
    assert.throws(() => snapshot.cleanup());
    assert.deepEqual(fs.readFileSync(unowned), before);
    assert.equal(fs.statSync(unowned).mode, mode);
  });
}

// Failure while identifying a newly opened pin must close every acquired fd,
// while leaving unidentified filesystem objects for explicit investigation.
for (const target of ["file", "directory"]) {
  test(`should close ownership handles when ${target} identity lookup fails`, { skip: target === "directory" && process.platform !== "linux" }, t => {
    const f = fixture(t);
    const originalOpen = fs.openSync;
    const originalStat = fs.fstatSync;
    const originalClose = fs.closeSync;
    const held = new Set<number>();
    let failFd: number | undefined;
    let injected = false;
    t.mock.method(fs, "openSync", (...args: Parameters<typeof fs.openSync>) => {
      const fd = originalOpen(...args);
      held.add(fd);
      const [file, flags] = args;
      if (typeof file === "string" && file.startsWith(f.temporary) && typeof flags === "number") {
        if (target === "directory" ? (flags & fs.constants.O_DIRECTORY) !== 0 : path.basename(file) === "manifest.json") failFd = fd;
      }
      return fd;
    });
    t.mock.method(fs, "fstatSync", (...args: Parameters<typeof fs.fstatSync>) => {
      if (!injected && args[0] === failFd) { injected = true; throw new Error("injected identity lookup failure"); }
      return originalStat(...args);
    });
    t.mock.method(fs, "closeSync", (fd: number) => { originalClose(fd); held.delete(fd); });
    assert.throws(() => captureBootstrap(f.binary, () => true, f.temporary));
    assert.equal(injected, true);
    assert.equal(held.size, 0, "an acquired ownership handle leaked");
  });
}
