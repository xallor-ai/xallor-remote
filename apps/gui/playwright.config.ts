import { defineConfig } from "@playwright/test";
import os from "node:os";
import path from "node:path";
export default defineConfig({
  testDir: "./tests", testMatch: "gui.e2e.ts", timeout: 180_000, workers: 1,
  reporter: "line", outputDir: path.join(process.env.XR_GUI_EVIDENCE || os.tmpdir(), "playwright"),
  use: { baseURL: process.env.XR_GUI_URL, viewport: { width: 1100, height: 800 }, headless: true },
});
