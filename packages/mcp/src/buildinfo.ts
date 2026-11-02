import fs from "node:fs";
import { MCP_VERSION } from "./version.js";
export type PackageBuild = { version: string; revision: string; dirty: boolean; built_at?: string; protocol: number };
export function packageBuild(file: URL = new URL("../build.json", import.meta.url)): PackageBuild {
  let raw: string;
  try { raw = fs.readFileSync(file, "utf8"); }
  catch (e) {
    if ((e as NodeJS.ErrnoException).code === "ENOENT")
      return { version: MCP_VERSION, revision: "unknown", dirty: true, protocol: 1 };
    throw e;
  }
  const info = JSON.parse(raw);
  if (info.version !== MCP_VERSION || typeof info.revision !== "string" ||
      typeof info.dirty !== "boolean" || info.protocol !== 1) throw Error("MCP 构建记录无效。");
  return info;
}
