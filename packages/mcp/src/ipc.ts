import { randomUUID } from "node:crypto";
import net from "node:net";
import { ipcPath } from "./ensure.js";

// protocol.md: serialized JSON bytes, excluding the newline delimiter.
export const MAX_IPC_MESSAGE_BYTES = 6 * (1024 * 1024 + 4096) + 8192;

export type Frame = {
  id?: string;
  event?: string;
  data?: string;
  ok?: boolean;
  result?: Record<string, unknown>;
  code?: string;
  message?: string;
  exec_id?: string;
};

type Waiter = {
  resolve: (f: Frame) => void;
  reject: (e: Error) => void;
  onEvent?: (f: Frame) => void;
};

export class RuntimeIpc {
  private sock?: net.Socket;
  private buf = "";
  private readonly waiters = new Map<string, Waiter>();

  get isConnected(): boolean {
    return !!this.sock && !this.sock.destroyed;
  }

  async connect(timeoutMs = 2000): Promise<void> {
    const path = ipcPath();
    this.sock = await new Promise<net.Socket>((resolve, reject) => {
      const s = net.createConnection(path);
      const t = setTimeout(() => {
        s.destroy();
        reject(new Error("Runtime 未运行"));
      }, timeoutMs);
      s.once("connect", () => {
        clearTimeout(t);
        resolve(s);
      });
      s.once("error", (err) => {
        clearTimeout(t);
        reject(err);
      });
    });
    this.sock.setEncoding("utf8");
    this.sock.on("data", (chunk: string) => this.onData(chunk));
    this.sock.on("close", () => {
      this.buf = "";
      for (const [id, w] of this.waiters) {
        w.reject(new Error("IPC 已断开"));
        this.waiters.delete(id);
      }
    });
  }

  close(): void {
    this.sock?.destroy();
    this.sock = undefined;
    this.buf = "";
  }

  async call(method: string, params: Record<string, unknown> = {}): Promise<Frame> {
    return this.roundTrip(method, params);
  }

  async stream(
    method: string,
    params: Record<string, unknown>,
    onEvent: (f: Frame) => void,
  ): Promise<Frame> {
    return this.roundTrip(method, params, onEvent);
  }

  private roundTrip(
    method: string,
    params: Record<string, unknown>,
    onEvent?: (f: Frame) => void,
  ): Promise<Frame> {
    if (!this.sock) {
      return Promise.reject(new Error("IPC 未连接"));
    }
    const id = randomUUID();
    const line = JSON.stringify({ id, method, params }) + "\n";
    if (Buffer.byteLength(line, "utf8") - 1 > MAX_IPC_MESSAGE_BYTES) {
      return Promise.reject(new Error("IPC 参数超过上限"));
    }
    return new Promise<Frame>((resolve, reject) => {
      this.waiters.set(id, { resolve, reject, onEvent });
      this.sock!.write(line, (err) => {
        if (err) {
          this.waiters.delete(id);
          reject(err);
        }
      });
    });
  }

  private onData(chunk: string): void {
    this.buf += chunk;
    for (;;) {
      const i = this.buf.indexOf("\n");
      if (i < 0) break;
      const line = this.buf.slice(0, i);
      this.buf = this.buf.slice(i + 1);
      if (Buffer.byteLength(line, "utf8") > MAX_IPC_MESSAGE_BYTES) {
        this.failConnection(new Error("IPC 报文超过上限"));
        return;
      }
      if (!line.trim()) continue;
      let f: Frame;
      try {
        f = JSON.parse(line) as Frame;
      } catch {
        this.failConnection(new Error("IPC 报文格式无效"));
        return;
      }
      if (!f || typeof f !== "object" || Array.isArray(f)) {
        this.failConnection(new Error("IPC 报文格式无效"));
        return;
      }
      if (!f.id) continue;
      const w = this.waiters.get(f.id);
      if (!w) continue;
      if (f.event) {
        w.onEvent?.(f);
        continue;
      }
      this.waiters.delete(f.id);
      w.resolve(f);
    }
    if (Buffer.byteLength(this.buf, "utf8") > MAX_IPC_MESSAGE_BYTES) {
      this.failConnection(new Error("IPC 报文超过上限"));
    }
  }

  private failConnection(error: Error): void {
    for (const waiter of this.waiters.values()) waiter.reject(error);
    this.waiters.clear();
    this.close();
  }
}

export function failText(f: Frame): string {
  return f.message || f.code || "失败。";
}
