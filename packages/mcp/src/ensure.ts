import { createHash } from "node:crypto";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

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

export const INSTALL_HINT =
  "找不到 xallor-remote。请重装带 Runtime 的 xallor-remote-mcp，或设置 XALLOR_REMOTE_BIN。MCP 不会静默联网下载。";
