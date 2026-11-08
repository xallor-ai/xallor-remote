import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { randomUUID } from "node:crypto";
import { findBinary, INSTALL_HINT } from "../../packages/mcp/src/ensure";

const MAX_FRAME = 2 * 1024 * 1024;
class Session {
  child: ChildProcessWithoutNullStreams;
  frames: unknown[] = [];
  bytes = 0;
  error?: string;
  touched = Date.now();
  constructor() {
    const binary = findBinary(process.env, { allowUnverified: process.env.XR_GUI_TEST_FIXTURE === "1" });
    if (!binary) throw new Error(INSTALL_HINT);
    this.child = spawn(binary, ["ipc-bridge"], { windowsHide: true, stdio: "pipe" });
    let buffer = "";
    this.child.stdout.setEncoding("utf8");
    this.child.stdout.on("data", (chunk: string) => {
      if (this.error) return;
      buffer += chunk;
      for (;;) {
        const i = buffer.indexOf("\n");
        if (i < 0) break;
        const raw = buffer.slice(0, i); buffer = buffer.slice(i + 1);
        this.bytes += Buffer.byteLength(raw);
        if (Buffer.byteLength(raw) > MAX_FRAME || this.bytes > 4 * MAX_FRAME || this.frames.length >= 4096) {
          this.fail("输出积压，连接已关闭；任务结果待确认。"); return;
        }
        try { this.frames.push(JSON.parse(raw)); }
        catch { this.fail("服务返回了无效数据。"); return; }
      }
      if (Buffer.byteLength(buffer) > MAX_FRAME) this.fail("服务返回的数据过大。");
    });
    this.child.stderr.resume();
    this.child.on("error", () => this.fail("无法启动本机连接，请检查 Runtime 安装。"));
    this.child.stdin.on("error", () => this.fail("连接已断开；任务结果待确认。"));
    this.child.on("close", () => this.fail("连接已断开；任务结果待确认。"));
  }
  fail(message: string) { this.error ??= message; this.child.kill(); }
}
const active = new Map<string, Session>();
function get(id: string): Session {
  const s = active.get(id);
  if (!s) throw new Error("连接已关闭，请重新连接。");
  s.touched = Date.now(); return s;
}
export const sessions = {
  open() {
    if (active.size >= 8) throw new Error("打开的连接过多。");
    const id = randomUUID(); active.set(id, new Session()); return id;
  },
  poll(id: string) {
    const s = get(id), frames = s.frames;
    s.frames = []; s.bytes = 0;
    if (s.error) active.delete(id);
    return { frames, error: s.error };
  },
  async send(id: string, frame: unknown) {
    const s = get(id);
    if (s.error) throw new Error(s.error);
    const raw = JSON.stringify(frame) + "\n";
    if (Buffer.byteLength(raw) > MAX_FRAME) throw new Error("请求过大。");
    await new Promise<void>((resolve, reject) => s.child.stdin.write(raw, e => e ? reject(e) : resolve()));
  },
  close(id: string) { const s = active.get(id); active.delete(id); s?.fail("连接已关闭。"); },
};
const timer = setInterval(() => {
  for (const [id, s] of active) if (Date.now() - s.touched > 90_000) sessions.close(id);
}, 15_000);
timer.unref();
process.once("exit", () => { for (const s of active.values()) s.child.kill(); });
