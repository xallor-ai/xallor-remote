import { spawn } from "node:child_process";
import { randomUUID } from "node:crypto";
import { createInterface } from "node:readline";
import path from "node:path";
export function head(profile: string) {
  const child = spawn(process.env.XALLOR_REMOTE_BIN!, ["ipc-bridge"], {
    windowsHide: true, env: { ...process.env, XALLOR_REMOTE_DATA_DIR: path.join(process.env.XR_GUI_ROOT!, profile) }, stdio: "pipe",
  });
  const pending = new Map<string, { resolve: (f: any) => void; reject: (e: Error) => void; timer: ReturnType<typeof setTimeout> }>();
  const lines = createInterface({ input: child.stdout });
  child.stderr.resume();
  lines.on("line", line => {
    const frame = JSON.parse(line), p = pending.get(frame.id);
    if (p && !frame.event) { clearTimeout(p.timer); pending.delete(frame.id); p.resolve(frame); }
  });
  const fail = () => { for (const p of pending.values()) { clearTimeout(p.timer); p.reject(new Error("fixture head closed")); } pending.clear(); };
  child.on("error", fail); child.on("close", fail);
  return {
    call(method: string, params: object = {}) {
      const id = randomUUID();
      return new Promise<any>((resolve, reject) => {
        const timer = setTimeout(() => { pending.delete(id); reject(new Error(`fixture ${method} timed out`)); }, 60_000);
        pending.set(id, { resolve, reject, timer });
        child.stdin.write(JSON.stringify({ id, method, params }) + "\n");
      });
    },
    close() { fail(); lines.close(); child.stdin.end(); child.kill(); },
  };
}
