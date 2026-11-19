#!/usr/bin/env node
/**
 * 把包装好的 Runtime 二进制转发出去，便于被控端只装一个 npm 包：
 *   xallor-remote start / grant issue / inbound on
 */
import { launchBootstrap } from "./bootstrap-launcher.js";
import { guardedArgs } from "./profile-format.js";

try {
  const result = await launchBootstrap(guardedArgs(process.argv.slice(2)), {
    env: process.env, stdio: "inherit", windowsHide: false,
  });
  process.exitCode = result.signal ? 1 : (result.code ?? 1);
} catch (error) {
  console.error(error instanceof Error ? error.message : String(error));
  process.exitCode = 1;
}
