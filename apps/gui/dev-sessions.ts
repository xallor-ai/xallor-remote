import net from "node:net";
import { randomUUID } from "node:crypto";
import { ipcPath } from "../../packages/mcp/src/ensure";
import { packageBuild } from "../../packages/mcp/src/buildinfo";
import { ensureOwner } from "../../packages/mcp/src/runtime-owner";
import type { Frame } from "../../packages/mcp/src/ipc";

const MAX_FRAME = 2 * 1024 * 1024;
export class Session {
  socket: net.Socket;
  private handshake?: { id: string; resolve: (frame: Frame) => void; reject: (error: Error) => void };
  frames: unknown[] = [];
  bytes = 0;
  error?: string;
  touched = Date.now();
  constructor(endpoint = ipcPath()) {
    // Development bridge only connects to an already started local owner. It
    // never discovers, verifies, selects, or executes a Runtime binary.
    this.socket = net.createConnection(endpoint);
    let buffer = "";
    this.socket.setEncoding("utf8");
    this.socket.on("data", (chunk: string) => {
      if (this.error) return;
      buffer += chunk;
      for (;;) {
        const i = buffer.indexOf("\n");
        if (i < 0) break;
        const raw = buffer.slice(0, i); buffer = buffer.slice(i + 1);
        if (Buffer.byteLength(raw) > MAX_FRAME) {
          this.fail("服务返回的数据过大。"); return;
        }
        let frame: Frame;
        try { frame = JSON.parse(raw); }
        catch { this.fail("服务返回了无效数据。"); return; }
        if (this.handshake) {
          if (!frame || frame.id !== this.handshake.id) { this.fail("服务就绪响应无效。"); return; }
          const waiter = this.handshake; this.handshake = undefined;
          waiter.resolve(frame); continue;
        }
        this.bytes += Buffer.byteLength(raw);
        if (this.bytes > 4 * MAX_FRAME || this.frames.length >= 4096) {
          this.fail("输出积压，连接已关闭；任务结果待确认。"); return;
        }
        this.frames.push(frame);
      }
      if (Buffer.byteLength(buffer) > MAX_FRAME) this.fail("服务返回的数据过大。");
    });
    this.socket.on("error", () => this.fail("无法连接本机服务，请先启动 Runtime。"));
    this.socket.on("close", () => this.fail("连接已断开；任务结果待确认。"));
  }
  async ready(expected?: Pick<ReturnType<typeof packageBuild>, "protocol" | "profile_format">, timeoutMs = 65_000) {
    try {
      await ensureOwner({ call: (method, params) => new Promise<Frame>((resolve, reject) => {
        if (this.error) { reject(new Error(this.error)); return; }
        const id = randomUUID();
        const timer = setTimeout(() => this.fail("Runtime 就绪超时，请检查运行状态。"), timeoutMs);
        this.handshake = { id, resolve: frame => { clearTimeout(timer); resolve(frame); },
          reject: error => { clearTimeout(timer); reject(error); } };
        this.socket.write(JSON.stringify({ id, method, params }) + "\n", error => {
          if (error) this.fail("连接已断开；任务结果待确认。");
        });
      }) }, expected ?? packageBuild());
      if (this.error) throw new Error(this.error);
    } catch (error) { this.fail(error instanceof Error ? error.message : "Runtime 就绪失败。"); throw error; }
  }
  fail(message: string) {
    this.error ??= message;
    const waiter = this.handshake; this.handshake = undefined;
    waiter?.reject(new Error(this.error)); this.socket.destroy();
  }
}
const active = new Map<string, Session>();
let opening = 0;
function get(id: string): Session {
  const s = active.get(id);
  if (!s) throw new Error("连接已关闭，请重新连接。");
  s.touched = Date.now(); return s;
}
export const sessions = {
  async open() {
    if (active.size + opening >= 8) throw new Error("打开的连接过多。");
    opening++;
    try {
      const session = new Session(); await session.ready();
      const id = randomUUID(); active.set(id, session); return id;
    } finally { opening--; }
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
    await new Promise<void>((resolve, reject) => s.socket.write(raw, e => e ? reject(e) : resolve()));
  },
  close(id: string) { const s = active.get(id); active.delete(id); s?.fail("连接已关闭。"); },
};
const timer = setInterval(() => {
  for (const [id, s] of active) if (Date.now() - s.touched > 90_000) sessions.close(id);
}, 15_000);
timer.unref();
process.once("exit", () => { for (const s of active.values()) s.socket.destroy(); });
