/**
 * 把已构建的 Runtime 打进 MCP 包再 npm pack，控制端 / 被控端只装一个 tgz。
 * 用法（仓库根）：node scripts/pack-mcp-with-runtime.mjs
 */
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync, execFileSync } from "node:child_process";
import { createHash } from "node:crypto";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const mcp = path.join(root, "packages", "mcp");
const release = path.join(root, "dist", "release");
const profileFormat = Number(fs.readFileSync(path.join(root, "internal/buildinfo/profile_format.txt"), "utf8").trim());
if (!Number.isSafeInteger(profileFormat) || profileFormat < 1) throw Error("Invalid source profile format");
const revision = execFileSync("git", ["rev-parse", "HEAD"], { cwd: root, encoding: "utf8" }).trim();
const dirty = !!execFileSync("git", ["status", "--porcelain", "--untracked-files=normal"], { cwd: root, encoding: "utf8" }).trim();

const copies = [
  {
    from: path.join(release, "windows-amd64", "xallor-remote.exe"),
    to: path.join(mcp, "vendor", "win32-x64", "xallor-remote.exe"),
    platform: "windows",
  },
  {
    from: path.join(release, "linux-amd64", "xallor-remote"),
    to: path.join(mcp, "vendor", "linux-x64", "xallor-remote"),
    platform: "linux",
  },
];

for (const { from, to, platform } of copies) {
  if (!fs.existsSync(from)) {
    console.error("缺少二进制，先构建:", from);
    process.exit(1);
  }
  const manifestPath = path.join(path.dirname(from), "manifest.json");
  const manifest = JSON.parse(fs.readFileSync(manifestPath, "utf8"));
  const pkg = JSON.parse(fs.readFileSync(path.join(mcp, "package.json"), "utf8"));
  const bytes = fs.readFileSync(from);
  if (manifest.schema !== 1 || manifest.build.version !== pkg.version ||
      manifest.build.revision !== revision || manifest.build.dirty !== dirty ||
      manifest.build.profile_format !== profileFormat ||
      manifest.build.os !== platform || manifest.build.arch !== "amd64" || manifest.build.protocol !== 1 ||
      manifest.artifact.filename !== path.basename(from) || manifest.artifact.size !== bytes.length ||
      manifest.artifact.sha256 !== createHash("sha256").update(bytes).digest("hex")) {
    throw new Error("Release manifest does not match artifact: " + from);
  }
  fs.mkdirSync(path.dirname(to), { recursive: true });
  fs.copyFileSync(from, to);
  fs.copyFileSync(manifestPath, path.join(path.dirname(to), "manifest.json"));
  if (process.platform !== "win32" || to.endsWith("xallor-remote")) {
    try {
      fs.chmodSync(to, 0o755);
    } catch {
      /* windows exe */
    }
  }
  console.log("vendor", path.relative(root, to), fs.statSync(to).size);
}

fs.writeFileSync(path.join(mcp, "build.json"), JSON.stringify({
  version: JSON.parse(fs.readFileSync(path.join(mcp, "package.json"), "utf8")).version,
  revision, dirty, built_at: new Date().toISOString(), protocol: 1, profile_format: profileFormat
}, null, 2));

const build = spawnSync("npm", ["run", "build"], { cwd: mcp, stdio: "inherit", shell: true });
if (build.status !== 0) process.exit(build.status ?? 1);
const test = spawnSync("npm", ["test"], { cwd: mcp, stdio: "inherit", shell: true });
if (test.status !== 0) process.exit(test.status ?? 1);

fs.mkdirSync(release, { recursive: true });
const pack = spawnSync("npm", ["pack", "--pack-destination", release], {
  cwd: mcp,
  stdio: "inherit",
  shell: true,
});
if (pack.status !== 0) process.exit(pack.status ?? 1);
console.log("packed into", release);
