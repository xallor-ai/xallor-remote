#!/usr/bin/env node
import { spawn } from "node:child_process";
import { McpServer } from "@modelcontextprotocol/server";
import { serveStdio } from "@modelcontextprotocol/server/stdio";
import * as z from "zod/v4";
import { findBinary, INSTALL_HINT } from "./ensure.js";
import { bindHostCancel, execToolText, newExecProgress, type ToolCtx } from "./exec-result.js";
import { readToolText } from "./read-result.js";
import { failText, RuntimeIpc, type Frame } from "./ipc.js";
import { MCP_VERSION, versionsMatch } from "./version.js";
import { packageBuild } from "./buildinfo.js";

let session: RuntimeIpc | undefined;

async function ensureRuntime(): Promise<RuntimeIpc> {
  if (session?.isConnected) {
    return session;
  }
  session?.close();
  session = undefined;
  const ipc = new RuntimeIpc();
  try {
    await ipc.connect(400);
  } catch {
    const bin = findBinary();
    if (!bin) {
      throw new Error(INSTALL_HINT);
    }
    await runEnsure(bin);
    await ipc.connect(8000);
  }
  const st = await ipc.call("status");
  if (st.ok === false) {
    throw new Error(failText(st));
  }
  const ver = String(st.result?.version ?? "");
  if (ver && !versionsMatch(ver, MCP_VERSION)) {
    throw new Error(`Runtime 版本 ${ver} 与 MCP ${MCP_VERSION} 主版本不一致。`);
  }
  const id = process.env.XALLOR_REMOTE_DEVICE_ID;
  const grant = process.env.XALLOR_REMOTE_DEVICE_GRANT;
  if (id && grant) {
    const add = await ipc.call("peer.add", { device_id: id, grant });
    if (add.ok === false) {
      throw new Error(failText(add));
    }
  }
  session = ipc;
  return ipc;
}

function runEnsure(bin: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const child = spawn(bin, ["ensure"], {
      windowsHide: true,
      stdio: ["ignore", "pipe", "pipe"],
      env: process.env,
    });
    let err = "";
    child.stderr?.on("data", (b: Buffer) => {
      err += b.toString();
    });
    const t = setTimeout(() => {
      child.kill();
      reject(new Error("ensure 超时。"));
    }, 12000);
    child.on("error", (e) => {
      clearTimeout(t);
      reject(e);
    });
    child.on("exit", (code) => {
      clearTimeout(t);
      if (code === 0) resolve();
      else reject(new Error(err.trim() || `ensure 退出码 ${code}`));
    });
  });
}

function textResult(text: string, isError = false) {
  return { content: [{ type: "text" as const, text }], isError };
}

function fromFrame(f: Frame, extra?: string) {
  if (f.ok === false) {
    return textResult(failText(f), true);
  }
  const body = extra ?? JSON.stringify(f.result ?? {}, null, 2);
  return textResult(body);
}

function createServer(): McpServer {
  const server = new McpServer({ name: "xallor-remote", version: MCP_VERSION });

  // Tool 名用 snake_case：Cursor 代理层会把 catalog 里的点号展成下划线再去 call，
  // 若服务端仍注册 remote.exec，会出现「列表有、调用 not found」。
  server.registerTool(
    "remote.devices",
    {
      description: "列出本机已收编的对方设备（peers）以及本 Runtime 是否在线。",
      inputSchema: z.object({}),
    },
    async () => {
      const ipc = await ensureRuntime();
      const f = await ipc.call("status");
      if (f.ok === false) {
        return fromFrame(f);
      }
      const peers = Array.isArray(f.result?.peers)
        ? (f.result.peers as { device_id?: string }[]).map((p) => ({ device_id: p.device_id }))
        : [];
      return textResult(
        JSON.stringify(
          {
            device_id: f.result?.device_id,
            online: f.result?.online,
            version: f.result?.version,
            build: f.result?.build,
            mcp_build: packageBuild(),
            workspace: f.result?.workspace,
            peers,
          },
          null,
          2,
        ),
      );
    },
  );

  server.registerTool(
    "remote.device_info",
    {
      description: "查询一台设备的 os/arch/hostname/workspace。只配一台时可省略 device_id。",
      inputSchema: z.object({
        device_id: z.string().optional().describe("目标设备 ID"),
      }),
    },
    async ({ device_id }) => {
      const ipc = await ensureRuntime();
      const f = await ipc.call("info", device_id ? { device_id } : {});
      return fromFrame(f, typeof f.result?.content === "string" ? f.result.content : undefined);
    },
  );

  server.registerTool(
    "remote.exec",
    {
      description: "在对方设备执行命令，流式回收 stdout/stderr。cwd 默认对方 workspace。",
      inputSchema: z.object({
        command: z.string().describe("要执行的命令"),
        device_id: z.string().optional(),
        cwd: z.string().optional(),
        timeout_ms: z
          .number()
          .int()
          .positive()
          .max(3_600_000)
          .optional()
          .describe("超时毫秒，最长 1 小时"),
      }),
    },
    async ({ command, device_id, cwd, timeout_ms }, ctx) => {
      if (!process.env.XALLOR_REMOTE_DEVICE_GRANT && !process.env.XALLOR_REMOTE_DEVICE_ID) {
        const ipc = await ensureRuntime();
        const st = await ipc.call("status");
        const peers = st.result?.peers;
        if (!Array.isArray(peers) || peers.length === 0) {
          return textResult("缺少授权。请在 mcp.json 配置 XALLOR_REMOTE_DEVICE_ID 与 XALLOR_REMOTE_DEVICE_GRANT，或先 xallor-remote peer add。", true);
        }
      }
      const ipc = await ensureRuntime();
      const toolCtx = ctx as ToolCtx;
      const report = newExecProgress(toolCtx);
      const onExec = bindHostCancel(toolCtx, (id) => {
        void ipc.call("exec.cancel", { exec_id: id });
      });
      const stdout: string[] = [];
      const stderr: string[] = [];
      let announced = false;
      const f = await ipc.stream(
        "exec",
        {
          command,
          ...(device_id ? { device_id } : {}),
          ...(cwd ? { cwd } : {}),
          ...(timeout_ms ? { timeout_ms } : {}),
        },
        (ev) => {
          if (ev.exec_id && !announced) {
            announced = true;
            onExec(ev.exec_id);
            void report(ev.exec_id);
          }
          if (ev.event === "stdout" && ev.data) {
            stdout.push(ev.data);
            void report(ev.data);
          }
          if (ev.event === "stderr" && ev.data) {
            stderr.push(ev.data);
            void report(ev.data);
          }
        },
      );
      if (f.ok === false) {
        return textResult(failText(f), true);
      }
      const got = execToolText(
        String(f.result?.status ?? ""),
        f.result?.exit_code,
        f.result?.duration_ms,
        f.result?.truncated,
        f.result?.exec_id,
        stdout.join(""),
        stderr.join(""),
      );
      return textResult(got.text, got.isError);
    },
  );

  server.registerTool(
    "remote.exec_cancel",
    {
      description: "取消本 MCP 会话发起的 exec。对未知或别人的 exec_id 返回没有这条任务。",
      inputSchema: z.object({
        exec_id: z.string(),
      }),
    },
    async ({ exec_id }) => {
      const ipc = await ensureRuntime();
      const f = await ipc.call("exec.cancel", { exec_id });
      return fromFrame(f);
    },
  );

  server.registerTool(
    "remote.read",
    {
      description: "读取对方 workspace 内文件。路径原样传递。默认前 64KiB；截断时会说明不是全文。",
      inputSchema: z.object({
        path: z.string(),
        device_id: z.string().optional(),
        head: z.number().int().nonnegative().max(1_048_576).optional().describe("从头读的行数"),
        tail: z.number().int().nonnegative().max(1_048_576).optional().describe("从尾读的行数"),
        offset: z.number().int().nonnegative().max(1_073_741_824).optional().describe("按字节的起点，可超过 1MiB"),
        limit: z.number().int().positive().max(1_048_576).optional().describe("按字节的长度，最大 1MiB"),
      }),
    },
    async (args) => {
      const ipc = await ensureRuntime();
      const f = await ipc.call("read", args);
      if (f.ok === false) {
        return fromFrame(f);
      }
      return textResult(readToolText(f.result?.content, f.result?.truncated));
    },
  );

  server.registerTool(
    "remote.write",
    {
      description: "整文件覆盖写入对方 workspace，上限 1MiB，原子替换。",
      inputSchema: z.object({
        path: z.string(),
        content: z.string(),
        device_id: z.string().optional(),
      }),
    },
    async (args) => {
      const ipc = await ensureRuntime();
      const f = await ipc.call("write", args);
      return fromFrame(f);
    },
  );

  server.registerTool(
    "remote.processes",
    {
      description: "列出对方当前用户可见进程（只读）。",
      inputSchema: z.object({
        device_id: z.string().optional(),
      }),
    },
    async ({ device_id }) => {
      const ipc = await ensureRuntime();
      const f = await ipc.call("processes", device_id ? { device_id } : {});
      return fromFrame(f, typeof f.result?.content === "string" ? f.result.content : undefined);
    },
  );

  return server;
}

void serveStdio(createServer);
