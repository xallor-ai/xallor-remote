import fs from "node:fs";
import path from "node:path";
import { createHash } from "node:crypto";

// The Go installer owns this file. The launcher only verifies and selects an artifact.
export function managedRuntime(dir: string): string | undefined {
  const statePath = path.join(dir, "runtime", "state.json");
  if (!fs.existsSync(statePath)) return undefined;
  const state = JSON.parse(fs.readFileSync(statePath, "utf8"));
  if (state.pending) throw new Error("上次升级未完成，请运行 xallor-remote runtime rollback。");
  if (!state.current) return undefined;
  const m = state.current;
  const name = process.platform === "win32" ? "xallor-remote.exe" : "xallor-remote";
  if (m.schema !== 1 || m.build?.os !== process.platform.replace("win32", "windows") ||
      m.build?.arch !== ({ x64: "amd64", arm64: "arm64" } as Record<string, string>)[process.arch] ||
      m.artifact?.filename !== name || !/^[a-f0-9]{64}$/.test(m.artifact?.sha256 ?? "")) {
    throw new Error("安装记录无效，请修复安装。");
  }
  const bin = path.join(dir, "runtime", "versions", m.artifact.sha256, name);
  const st = fs.lstatSync(bin);
  if (!st.isFile() || st.isSymbolicLink() || st.size !== m.artifact.size ||
      createHash("sha256").update(fs.readFileSync(bin)).digest("hex") !== m.artifact.sha256) {
    throw new Error("已安装程序校验失败，请回滚或重新安装。");
  }
  return bin;
}
