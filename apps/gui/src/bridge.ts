export type Frame = {
  id: string; event?: string; data?: string; exec_id?: string;
  ok?: boolean; result?: unknown; code?: string; message?: string;
};
type Pending = { resolve: (f: Frame) => void; reject: (e: Error) => void; event?: (f: Frame) => void };
const native = "__TAURI_INTERNALS__" in window;
const CONNECT_HINT =
  "连接失败。请安装带 Runtime 的 xallor-remote-mcp，或运行 xallor-remote runtime install，或设置 XALLOR_REMOTE_BIN。";

function errMessage(e: unknown, fallback = CONNECT_HINT): string {
  if (typeof e === "string" && e.trim()) return e;
  if (e instanceof Error && e.message.trim()) return e.message;
  if (e && typeof e === "object") {
    const o = e as { message?: unknown; error?: unknown };
    if (typeof o.message === "string" && o.message.trim()) return o.message;
    if (typeof o.error === "string" && o.error.trim()) return o.error;
  }
  return fallback;
}

async function transport<T>(action: string, args: Record<string, unknown> = {}): Promise<T> {
  if (native) {
    const { invoke } = await import("@tauri-apps/api/core");
    try {
      return await invoke<T>(`session_${action}`, args);
    } catch (e) {
      throw new Error(errMessage(e));
    }
  }
  const res = await fetch(`/__ipc/${action}`, {
    method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(args),
    signal: AbortSignal.timeout(30_000),
  });
  const body = await res.json();
  if (!res.ok) throw new Error(body.message || CONNECT_HINT);
  return body.result as T;
}

class Connection {
  private pending = new Map<string, Pending>();
  private opening?: Promise<string>;
  private session?: string;
  private listeners = new Set<() => void>();
  error = "";
  onChange(fn: () => void) { this.listeners.add(fn); return () => { this.listeners.delete(fn); }; }
  private notify() { this.listeners.forEach(fn => fn()); }
  private async open(): Promise<string> {
    if (this.session) return this.session;
    if (this.opening) return this.opening;
    this.opening = transport<string>("open").then(id => {
      this.session = id; this.error = ""; void this.poll(id); this.notify(); return id;
    }).catch(e => { this.error = errMessage(e); this.notify(); throw e instanceof Error ? e : new Error(this.error); }).finally(() => { this.opening = undefined; });
    return this.opening;
  }
  private fail(id: string, error: Error) {
    if (this.session !== id) return;
    this.session = undefined; this.error = error.message;
    for (const p of this.pending.values()) p.reject(error);
    this.pending.clear(); this.notify();
    void transport("close", { session: id }).catch(() => {});
  }
  private async poll(id: string) {
    try {
      while (this.session === id) {
        const { frames, error } = await transport<{ frames: Frame[]; error?: string }>("poll", { session: id });
        for (const frame of frames) {
          if (frame.event === "approval") { this.notify(); continue; }
          const p = this.pending.get(frame.id);
          if (!p) continue;
          if (frame.event) p.event?.(frame);
          else { this.pending.delete(frame.id); p.resolve(frame); }
        }
        if (error) throw new Error(error);
        await new Promise(resolve => setTimeout(resolve, 80));
      }
    } catch (e) { this.fail(id, e instanceof Error ? e : new Error("连接已断开。")); }
  }
  async request(method: string, params: Record<string, unknown> = {}, event?: (f: Frame) => void): Promise<Frame> {
    // Failed sessions require explicit reconnect. Never replay a request.
    if (this.error) throw new Error(this.error);
    const session = await this.open(), id = crypto.randomUUID();
    return new Promise<Frame>((resolve, reject) => {
      const timer = setTimeout(() => this.fail(session, new Error("等待结果超时；任务结果待确认。")), method === "exec" ? 150_000 : 30_000);
      this.pending.set(id, { resolve: frame => { clearTimeout(timer); resolve(frame); }, reject: e => { clearTimeout(timer); reject(e); }, event });
      void transport("send", { session, frame: { id, method, params } }).catch(e => this.fail(session, e));
    });
  }
  async rpc<T = unknown>(method: string, params: Record<string, unknown> = {}): Promise<T> {
    const frame = await this.request(method, params);
    if (frame.ok !== true) throw new Error(frame.message || frame.code || "操作失败。");
    return frame.result as T;
  }
  async reconnect() { this.error = ""; await this.rpc("approval.subscribe"); this.notify(); }
  close() { if (this.session) this.fail(this.session, new Error("连接已关闭。")); }
}
export const connection = new Connection();
export const rpc = connection.rpc.bind(connection);
window.addEventListener("pagehide", () => connection.close());
export type Peer = { device_id: string; name?: string };
export type Status = { device_id: string; workspace: string; online: boolean; inbound: boolean; has_grant: boolean; version: string; peers: Peer[] };
