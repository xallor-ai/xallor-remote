#!/usr/bin/env node
/**
 * 把包装好的 Runtime 二进制转发出去，便于被控端只装一个 npm 包：
 *   xallor-remote start / grant issue / inbound on
 */
import { spawn } from "node:child_process";
import { findBinary, INSTALL_HINT } from "./ensure.js";
import { guardedArgs } from "./profile-format.js";
import { verifySourceSync } from "./source-trust.js";

const bin = findBinary(process.env, { managed: process.argv[2] !== "runtime", sourceVerifier: verifySourceSync });
if (!bin) {
  console.error(INSTALL_HINT);
  process.exit(1);
}

const child = spawn(bin, guardedArgs(process.argv.slice(2)), {
  stdio: "inherit",
  windowsHide: false,
});
child.on("error", (e) => {
  console.error(e.message || INSTALL_HINT);
  process.exit(1);
});
child.on("exit", (code, signal) => {
  if (signal) process.exit(1);
  process.exit(code ?? 1);
});
