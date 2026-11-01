import fs from "node:fs";
export const MCP_VERSION: string = JSON.parse(fs.readFileSync(new URL("../package.json", import.meta.url), "utf8")).version;

export function versionsMatch(runtimeVersion: string, mcpVersion = MCP_VERSION): boolean {
  const a = runtimeVersion.replace(/-.*$/, "").split(".");
  const b = mcpVersion.replace(/-.*$/, "").split(".");
  if (a[0] !== b[0]) {
    return false;
  }
  if (a[0] === "0") {
    return a[1] === b[1];
  }
  return true;
}
