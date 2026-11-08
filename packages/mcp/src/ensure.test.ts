import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { bundledBinary, findBinary, vendorPlatformKey } from "./ensure.js";

test("XALLOR_REMOTE_BIN is rejected without a provenance sidecar", () => {
  const fake = path.join(os.tmpdir(), `xallor-bin-${process.pid}`);
  fs.writeFileSync(fake, "");
  try {
    const hit = findBinary({ ...process.env, XALLOR_REMOTE_BIN: fake, PATH: "" }, { packageRoot: path.join(os.tmpdir(), "empty-pkg") });
    assert.equal(hit, undefined);
  } finally {
    fs.unlinkSync(fake);
  }
});

test("missing env path falls through when no vendor", () => {
  const emptyRoot = fs.mkdtempSync(path.join(os.tmpdir(), "xr-mcp-"));
  const isolated = path.join(os.tmpdir(), `xr-iso-${process.pid}`);
  try {
    const hit = findBinary(
      {
        XALLOR_REMOTE_BIN: path.join(os.tmpdir(), "definitely-missing-xallor-remote"),
        PATH: "",
        Path: "",
        APPDATA: isolated,
        AppData: isolated,
        XDG_CONFIG_HOME: isolated,
        HOME: isolated,
        USERPROFILE: isolated,
      },
      { packageRoot: emptyRoot },
    );
    assert.equal(hit, undefined);
  } finally {
    fs.rmSync(emptyRoot, { recursive: true, force: true });
  }
});

test("bundled vendor path is found, but selection is rejected without a provenance proof", () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "xr-mcp-v-"));
  const plat = vendorPlatformKey();
  const name = process.platform === "win32" ? "xallor-remote.exe" : "xallor-remote";
  const target = path.join(root, "vendor", plat, name);
  fs.mkdirSync(path.dirname(target), { recursive: true });
  fs.writeFileSync(target, "");
  try {
    assert.equal(bundledBinary(root), target);
    const hit = findBinary(
      {
        ...process.env,
        XALLOR_REMOTE_BIN: "",
        PATH: "",
        APPDATA: path.join(os.tmpdir(), "no-xallor-appdata-2"),
      },
      { packageRoot: root },
    );
    assert.equal(hit, undefined);
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});
