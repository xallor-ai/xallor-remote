import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { createRequire } from "node:module";
import { spawnSync } from "node:child_process";

const root = fileURLToPath(new URL("../", import.meta.url));
const require = createRequire(import.meta.url);
const dist = path.join(root, "dist");
function checkOutputDirectory(allowMissing = false) {
  try {
    const info = fs.lstatSync(dist);
    if (!info.isDirectory() || info.isSymbolicLink()) throw new Error("Build output directory is redirected");
  } catch (error) {
    if (!allowMissing || error.code !== "ENOENT") throw error;
  }
}
checkOutputDirectory(true);
const result = spawnSync(process.execPath, [require.resolve("typescript/bin/tsc")], {
  cwd: root, stdio: "inherit", windowsHide: true,
});
if (result.error) throw result.error;
if (result.status !== 0) process.exit(result.status ?? 1);

// tsc does not delete artifacts for removed sources. Only unlink these known
// obsolete output files; never recursively clear a caller-selected directory.
checkOutputDirectory();
for (const module of ["managed-runtime", "managed-runtime.test", "ensure.test"]) {
  for (const suffix of [".js", ".js.map", ".d.ts", ".d.ts.map"]) {
    const target = path.join(dist, module + suffix);
    try {
      const file = fs.lstatSync(target);
      if (!file.isFile() || file.isSymbolicLink()) throw new Error("Obsolete build output is not an ordinary file");
      fs.unlinkSync(target);
    } catch (error) {
      if (error.code !== "ENOENT") throw error;
    }
  }
}
