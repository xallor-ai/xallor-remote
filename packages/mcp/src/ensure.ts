import fs from "node:fs";
import { createHash } from "node:crypto";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { managedRuntime } from "./managed-runtime.js";
import { verifySourceSync } from "./source-trust.js";

export function dataDir(env: NodeJS.ProcessEnv = process.env): string {
	if (env.XALLOR_REMOTE_DATA_DIR) {
		if (!path.isAbsolute(env.XALLOR_REMOTE_DATA_DIR)) throw new Error("XALLOR_REMOTE_DATA_DIR must be absolute");
		return path.resolve(env.XALLOR_REMOTE_DATA_DIR);
	}
  if (process.platform === "win32") {
    const base = env.APPDATA || env.AppData || path.join(os.homedir(), "AppData", "Roaming");
    return path.join(base, "XallorRemote");
  }
  if (env.XDG_CONFIG_HOME) {
    return path.join(env.XDG_CONFIG_HOME, "xallor-remote");
  }
  return path.join(os.homedir(), ".config", "xallor-remote");
}

export function ipcPath(env: NodeJS.ProcessEnv = process.env): string {
	if (env.XALLOR_REMOTE_DATA_DIR) {
		const dir = dataDir(env);
		if (process.platform === "win32") {
			const sum = createHash("sha256").update(dir.toLowerCase()).digest("hex").slice(0, 16);
			return "\\\\.\\pipe\\XallorRemote-" + sum;
		}
		return path.join(dir, "ipc.sock");
	}
  if (process.platform === "win32") {
    return "\\\\.\\pipe\\XallorRemote";
  }
  if (env.XDG_RUNTIME_DIR) {
    return path.join(env.XDG_RUNTIME_DIR, "xallor-remote.sock");
  }
  return path.join(dataDir(env), "ipc.sock");
}

export function binaryNames(): string[] {
  return process.platform === "win32" ? ["xallor-remote.exe", "xallor-remote"] : ["xallor-remote"];
}

/** npm 包根目录（含 package.json / vendor）。 */
export function packageRoot(): string {
  return path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
}

/** 与 esbuild 类似的平台目录名。 */
export function vendorPlatformKey(platform = process.platform, arch = process.arch): string {
  if (platform === "win32" && (arch === "x64" || arch === "arm64")) return "win32-x64";
  if (platform === "linux" && arch === "x64") return "linux-x64";
  if (platform === "linux" && arch === "arm64") return "linux-arm64";
  if (platform === "darwin" && arch === "arm64") return "darwin-arm64";
  if (platform === "darwin" && arch === "x64") return "darwin-x64";
  return `${platform}-${arch}`;
}

export function bundledBinary(root = packageRoot()): string | undefined {
  const dir = path.join(root, "vendor", vendorPlatformKey());
  for (const n of binaryNames()) {
    const p = path.join(dir, n);
    try {
      fs.accessSync(p, fs.constants.F_OK);
      const manifest = path.join(dir, "manifest.json");
      if (!fs.existsSync(manifest)) continue;
      try {
        const parsed = JSON.parse(fs.readFileSync(manifest, "utf8"));
        if (verifySourceSync(p, manifest, parsed.build)) return p;
      } catch { /* reject unverified vendor */ }
    } catch {
      /* next */
    }
  }
  return undefined;
}

export type FindBinaryOpts = {
  packageRoot?: string;
  managed?: boolean;
};

export function findBinary(env = process.env, opts: FindBinaryOpts = {}): string | undefined {
  const names = binaryNames();
  const candidates: string[] = [];
  if (env.XALLOR_REMOTE_BIN) {
    candidates.push(env.XALLOR_REMOTE_BIN);
  }
  if (!env.XALLOR_REMOTE_BIN && opts.managed !== false) {
    const managed = managedRuntime(dataDir(env));
    if (managed) return managed;
  }
  const bundled = bundledBinary(opts.packageRoot ?? packageRoot());
  if (bundled) candidates.push(bundled);

  // PATH 显式为 "" 时不要回落到 Windows 的 Path
  const pathEnv = Object.prototype.hasOwnProperty.call(env, "PATH")
    ? (env.PATH ?? "")
    : (env.Path ?? env.PATH ?? "");
  const sep = process.platform === "win32" ? ";" : ":";
  for (const dir of pathEnv.split(sep)) {
    if (!dir) continue;
    for (const n of names) {
      candidates.push(path.join(dir, n));
    }
  }
  const binDir = path.join(dataDir(env), "bin");
  for (const n of names) {
    candidates.push(path.join(binDir, n));
  }
  for (const c of candidates) {
    try {
      fs.accessSync(c, fs.constants.F_OK);
      const manifest = path.join(path.dirname(c), "manifest.json");
      if (!fs.existsSync(manifest)) continue;
      try {
        const parsed = JSON.parse(fs.readFileSync(manifest, "utf8"));
        if (verifySourceSync(c, manifest, parsed.build)) return c;
      } catch { /* reject unverified candidate */ }
    } catch {
      /* try next */
    }
  }
  return undefined;
}

export const INSTALL_HINT =
  "找不到 xallor-remote。请重装带 Runtime 的 xallor-remote-mcp，或设置 XALLOR_REMOTE_BIN。MCP 不会静默联网下载。";
