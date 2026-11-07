import fs from "node:fs";
import { MCP_VERSION } from "./version.js";
export type PackageBuild = { version: string; revision: string; dirty: boolean; built_at?: string; protocol: number; profile_format: number };
export function packageBuild(file: URL = new URL("../build.json", import.meta.url)): PackageBuild {
  let raw: string;
  try { raw = fs.readFileSync(file, "utf8"); }
  catch (e) {
    if ((e as NodeJS.ErrnoException).code === "ENOENT") {
      // Only source checkouts have this file. Packaged installations must carry build.json.
      const profile_format = Number(fs.readFileSync(new URL("../../../internal/buildinfo/profile_format.txt", import.meta.url), "utf8").trim());
      if (!Number.isSafeInteger(profile_format) || profile_format < 1) throw Error("本机数据格式记录无效。");
      return { version: MCP_VERSION, revision: "unknown", dirty: true, protocol: 1, profile_format };
    }
    throw e;
  }
  const info = JSON.parse(raw);
  if (info.version !== MCP_VERSION || typeof info.revision !== "string" ||
      typeof info.dirty !== "boolean" || info.protocol !== 1 ||
      !Number.isSafeInteger(info.profile_format) || info.profile_format < 1) throw Error("MCP 构建记录无效。");
  return info;
}
