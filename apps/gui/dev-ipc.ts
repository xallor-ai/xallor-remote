import type { IncomingMessage, ServerResponse } from "node:http";
import { sessions } from "./dev-sessions";

// Development only. Packaged GUI uses a child process, never an HTTP port.
export async function handleDevIpc(req: IncomingMessage, res: ServerResponse): Promise<boolean> {
  if (!req.url?.startsWith("/__ipc")) return false;
  res.setHeader("Content-Type", "application/json; charset=utf-8");
  res.setHeader("Cache-Control", "no-store");
  try {
    const host = req.headers.host || "";
    if (!/^(127\.0\.0\.1|localhost|\[::1\]):\d+$/.test(host) ||
        req.headers.origin !== `http://${host}` || req.method !== "POST" ||
        req.headers["content-type"] !== "application/json") {
      res.statusCode = 403; throw new Error("请求来源不受支持。");
    }
    const chunks: Buffer[] = []; let size = 0;
    for await (const chunk of req) {
      size += chunk.length;
      if (size > 2 * 1024 * 1024) throw new Error("请求过大。");
      chunks.push(chunk);
    }
    const value = JSON.parse(Buffer.concat(chunks).toString("utf8"));
    const action = req.url.slice("/__ipc/".length);
    let result: unknown;
    if (action === "open") result = sessions.open();
    else if (action === "poll") result = sessions.poll(value.session);
    else if (action === "send") result = await sessions.send(value.session, value.frame);
    else if (action === "close") result = sessions.close(value.session);
    else { res.statusCode = 404; throw new Error("未知请求。"); }
    res.end(JSON.stringify({ result }));
  } catch (e) {
    if (res.statusCode === 200) res.statusCode = 400;
    res.end(JSON.stringify({ message: e instanceof Error ? e.message : "操作失败。" }));
  }
  return true;
}
