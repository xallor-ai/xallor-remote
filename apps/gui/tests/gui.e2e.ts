import { test, expect } from "@playwright/test";
import path from "node:path";
import { readFile } from "node:fs/promises";
import { head } from "./fixture-head";

// Real state only: no route mocks, fake Runtime or prerecorded results.
test("connect, stream, cancel, approve and inspect actual history", async ({ page, request }) => {
  const target = process.env.XR_GUI_TARGET!, controller = process.env.XR_GUI_CONTROLLER!;
  const errors: string[] = [];
  const terminals: object[] = [];
  page.on("response", async response => {
    if (!response.url().endsWith("/__ipc/poll")) return;
    const batch = await response.json().catch(() => ({}));
    for (const f of batch.result?.frames || []) if (f.code || f.result?.exec_id) {
      terminals.push({ code: f.code, status: f.result?.status, exit_code: f.result?.exit_code });
    }
  });
  page.on("pageerror", e => errors.push(e.message));
  page.on("console", e => { if (e.type() === "error") errors.push(e.text()); });
  const incoming = head("target");
  try {
    await page.goto("/");
    await expect(page).toHaveTitle(/XallorRemote/);
    await expect(page.getByRole("heading", { name: "连接", exact: true })).toBeVisible();
    await expect(page.locator("vite-error-overlay")).toHaveCount(0);
    const grant = (await readFile(path.join(process.env.XR_GUI_ROOT!, "controller", "grant"), "utf8")).trim();
    expect(await page.locator("body").textContent()).not.toContain(grant);
    const alias = '<img src=x onerror="window.xss=1"> Laptop';
    await page.getByLabel("设备备注").fill(alias);
    await page.getByRole("button", { name: "保存备注" }).click();
    await expect(page.locator(".peer-detail strong")).toHaveText(alias);
    expect(await page.evaluate(() => (window as any).xss)).toBeUndefined();
    await page.getByRole("button", { name: "检查连接" }).click();
    await expect(page.locator(".peer-state")).toHaveText("可连接", { timeout: 15_000 });
    await page.getByRole("button", { name: "显示授权码" }).click();
    await expect(page.locator("#grant")).toHaveText(grant);
    await page.getByRole("button", { name: "隐藏授权码" }).click();
    expect(await page.locator("body").textContent()).not.toContain(grant);
    await page.screenshot({ path: path.join(process.env.XR_GUI_EVIDENCE!, "desktop.png"), fullPage: true });
    const rejected = await request.post("/__ipc/open", { headers: { Origin: "https://untrusted.example", "Content-Type": "application/json" }, data: {} });
    expect(rejected.status()).toBe(403);

    await page.getByRole("button", { name: "任务", exact: true }).click();
    const command = process.platform === "win32" ? "Write-Output gui-stream; Start-Sleep -Seconds 30" : "echo gui-stream; sleep 30";
    await page.getByLabel("命令", { exact: true }).fill(command);
    await page.getByRole("button", { name: "执行", exact: true }).click();
    await expect(page.locator("#tasks pre").first()).toContainText("gui-stream", { timeout: 20_000 });
    await expect(page.locator(".task-state").first()).toHaveText("执行中");
    await page.getByRole("button", { name: "连接", exact: true }).click();
    await page.getByRole("button", { name: "任务", exact: true }).click();
    await expect(page.locator("#tasks pre").first()).toContainText("gui-stream");
    await page.getByRole("button", { name: "取消任务" }).first().click();
    await expect(page.locator(".task-state").first()).toHaveText("已取消", { timeout: 15_000 });
    await page.getByLabel("命令", { exact: true }).fill("exit 7");
    await page.getByRole("button", { name: "执行", exact: true }).click();
    await expect(page.locator(".task-state").first()).toHaveText("执行失败（退出码 7）", { timeout: 20_000 });
    // Cancel immediately, before any output; must not execute the delayed side effect.
    const early = process.platform === "win32" ? "Start-Sleep -Seconds 30; Write-Output should-not-run" : "sleep 30; echo should-not-run";
    for (let attempt = 0; attempt < 5; attempt++) {
    await page.getByLabel("命令", { exact: true }).fill(early);
    await page.getByRole("button", { name: "执行", exact: true }).click();
    await page.getByRole("button", { name: "取消任务" }).first().click();
    await expect(page.locator(".task-state").first()).toHaveText("已取消", { timeout: 15_000 });
    expect(await page.locator("#tasks pre").first().textContent()).not.toContain("should-not-run");
    }

    await page.getByRole("button", { name: /^审批/ }).click();
    // The policy matches shutdown inside an inert echo. No power operation is run.
    const denied = incoming.call("exec", { device_id: controller, command: "echo shutdown", timeout_ms: 5000 });
    await expect(page.getByRole("button", { name: "拒绝", exact: true })).toBeVisible({ timeout: 15_000 });
    await page.getByRole("button", { name: "拒绝", exact: true }).click();
    expect((await denied).code).toBe("policy_deny");
    await expect(page.locator("#pending")).toContainText("当前没有等待确认", { timeout: 10_000 });
    const allowed = incoming.call("exec", { device_id: controller, command: "echo shutdown", timeout_ms: 5000 });
    await expect(page.getByRole("button", { name: "允许执行", exact: true })).toBeVisible({ timeout: 15_000 });
    await page.getByRole("button", { name: "允许执行", exact: true }).click();
    expect((await allowed).result.exit_code).toBe(0);
    await expect(page.locator("#pending")).toContainText("当前没有等待确认", { timeout: 10_000 });
    await page.getByRole("button", { name: "审计", exact: true }).click();
    await expect(page.locator("#history")).toContainText("本机接收");
    await expect(page.locator("#history")).toContainText("发往对方");
    expect(await page.locator("#history").textContent()).not.toContain("echo shutdown");
    await expect(page.locator('[data-page="approval"] .badge')).toHaveText("", { timeout: 5000 });
    await page.screenshot({ path: path.join(process.env.XR_GUI_EVIDENCE!, "audit.png"), fullPage: true });
    await page.setViewportSize({ width: 390, height: 844 });
    await page.getByRole("button", { name: "连接", exact: true }).click();
    await expect(page.locator(".peer-detail strong")).toHaveText(alias);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.screenshot({ path: path.join(process.env.XR_GUI_EVIDENCE!, "mobile.png"), fullPage: true });
    expect(errors).toEqual([]);
  } catch (e) { console.log("Terminal evidence:", JSON.stringify(terminals)); throw e; }
  finally { incoming.close(); }
});
