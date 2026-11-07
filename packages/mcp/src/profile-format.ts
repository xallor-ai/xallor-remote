import { packageBuild } from "./buildinfo.js";
import { MCP_VERSION, versionsMatch } from "./version.js";

export function guardedArgs(args: string[]): string[] {
  return ["--require-profile-format", String(packageBuild().profile_format), ...args];
}

export function checkRuntimeBuild(status: Record<string, unknown> | undefined): void {
  const build = status?.build as { profile_format?: number; protocol?: number } | undefined;
  const version = typeof status?.version === "string" ? status.version : "";
  const expected = packageBuild();
  if (!version || !versionsMatch(version, MCP_VERSION) ||
      build?.profile_format !== expected.profile_format || build?.protocol !== expected.protocol) {
    throw Error("Runtime 与 MCP 的数据格式或版本不匹配，请检查配套安装与备份。");
  }
}
