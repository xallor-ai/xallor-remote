#!/usr/bin/env node
import { McpServer } from "@modelcontextprotocol/server";
import { serveStdio } from "@modelcontextprotocol/server/stdio";
import * as z from "zod/v4";
import { launchBootstrap } from "./bootstrap-launcher.js";
import { bindHostCancel, execFrameText, newExecProgress, type ToolCtx } from "./exec-result.js";
import { readToolText } from "./read-result.js";
import { failText, RuntimeIpc, type Frame } from "./ipc.js";
import { MCP_VERSION } from "./version.js";
import { guardedArgs, checkRuntimeBuild } from "./profile-format.js";
import { packageBuild } from "./buildinfo.js";
import { execInputSchema } from "./exec-input.js";
import { connectOwner, ensureOwner, OWNER_OBSERVATION_TIMEOUT_MS } from "./runtime-owner.js";
import { runtimeSession } from "./runtime-session.js";

const ensureRuntime = runtimeSession(async bootstrap => {
  const ipc = new RuntimeIpc();
  try {
    await connectOwner(ipc, bootstrap);
    await ensureOwner(ipc, packageBuild());
    const st = await ipc.call("status", {}, OWNER_OBSERVATION_TIMEOUT_MS);
    if (st.ok === false) {
      throw new Error(failText(st));
    }
    checkRuntimeBuild(st.result);
    const id = process.env.XALLOR_REMOTE_DEVICE_ID;
    const grant = process.env.XALLOR_REMOTE_DEVICE_GRANT;
    if (id && grant) {
      const add = await ipc.call("peer.add", { device_id: id, grant });
      if (add.ok === false) {
        throw new Error(failText(add));
      }
    }
    return ipc;
  } catch (error) {
    ipc.close();
    throw error;
  }
}, async () => {
  const result = await launchBootstrap(guardedArgs(["ensure"]), {
    stdio: "pipe", windowsHide: true, env: process.env, timeoutMs: 150_000,
  });
  if (result.code !== 0 || result.signal) {
    throw new Error(result.stderr.trim() || "Runtime 引导未成功，请检查运行状态。");
  }
});

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
      description: "查询一台目标设备的 os/arch/hostname/workspace、shell 能力、执行模式、execution_backend、execution_backends、operation_policy、device_lifecycle、entitlement_contract、usage_event_contract、relay_registry、relay_failover、plane_contract 和 oidc_mapping_contract。OIDC 映射只由本机管理控制面完成，MCP 不接 provider token；usage 只含隐私最小元数据；当前额度仍由管理面提供，授权仍由 Relay 对每次操作复验；只配一台时可省略 device_id。",
      inputSchema: z.object({
        device_id: z.string().optional().describe("目标设备 ID"),
        session_id: z.string().min(1).optional().describe("已批准的逻辑会话 ID"),
      }),
    },
    async ({ device_id, session_id }) => {
      const ipc = await ensureRuntime();
      const f = await ipc.call("info", { ...(device_id ? { device_id } : {}), ...(session_id ? { session_id } : {}) });
      return fromFrame(f, typeof f.result?.content === "string" ? f.result.content : undefined);
    },
  );

  server.registerTool(
    "remote.session_request",
    {
      description: "实验性 WIP：通过本机 Runtime 请求目标 Runtime 批准逻辑 session；授权码只从 XALLOR_REMOTE_ACCESS_CODE 环境变量读取。",
      inputSchema: z.object({
        device_id: z.string().optional(),
        code_id: z.string(),
        target_uid: z.string(),
        generation: z.number().int().positive(),
        scope: z.array(z.string()).min(1),
        session_expires_at: z.number().int().positive().optional(),
      }),
    },
    async ({ device_id, code_id, target_uid, generation, scope, session_expires_at }) => {
      const secret = process.env.XALLOR_REMOTE_ACCESS_CODE;
      if (!secret) {
        return textResult("缺少 XALLOR_REMOTE_ACCESS_CODE；不会通过工具参数或日志接收授权码。", true);
      }
      const ipc = await ensureRuntime();
      const f = await ipc.call("session.request", {
        ...(device_id ? { device_id } : {}),
        code_id,
        code_secret: secret,
        target_uid,
        generation,
        scope,
        ...(session_expires_at ? { session_expires_at } : {}),
      });
      return fromFrame(f);
    },
  );

  server.registerTool(
    "remote.device_code_revoke",
    {
      description: "通过本机 Runtime owner 管理通道撤销目标设备的逻辑授权码及其派生 session。Relay 执行 owner 校验；不会接收或输出 secret。",
      inputSchema: z.object({
        code_id: z.string().min(1),
      }),
    },
    async ({ code_id }) => {
      const ipc = await ensureRuntime();
      return fromFrame(await ipc.call("device.access", { action: "code.revoke", code_id }));
    },
  );

  server.registerTool(
    "remote.device_code_rotate",
    {
      description: "通过本机 Runtime owner 管理通道轮换本机设备的逻辑授权码。新 secret 只从 XALLOR_REMOTE_CODE_SECRET 环境变量读取，不进入工具参数；结果只返回原替代码的状态和策略元数据，不回显 secret，也不代表已获准执行。结果未确认时不得自动重试。",
      inputSchema: z.object({
        code_id: z.string().min(1),
        new_code_id: z.string().min(1),
      }),
    },
    async ({ code_id, new_code_id }) => {
      const secret = process.env.XALLOR_REMOTE_CODE_SECRET;
      if (!secret) {
        return textResult("缺少 XALLOR_REMOTE_CODE_SECRET；不会通过工具参数或日志接收新 secret。", true);
      }
      const ipc = await ensureRuntime();
      return fromFrame(await ipc.call("device.access", { action: "code.rotate", code_id, new_code_id, new_secret: secret }));
    },
  );

  server.registerTool(
    "remote.session_resume",
    {
      description: "实验性 WIP：通过本机 Runtime 使用一次性 resume capability 恢复已批准逻辑 session；capability 只从 XALLOR_REMOTE_RESUME_CAPABILITY 环境变量读取。",
      inputSchema: z.object({
        device_id: z.string(),
        session_id: z.string(),
        resume_id: z.string(),
        target_uid: z.string(),
      }),
    },
    async ({ device_id, session_id, resume_id, target_uid }) => {
      const capability = process.env.XALLOR_REMOTE_RESUME_CAPABILITY;
      if (!capability) {
        return textResult("缺少 XALLOR_REMOTE_RESUME_CAPABILITY；不会通过工具参数或日志接收 resume capability。", true);
      }
      const ipc = await ensureRuntime();
      const f = await ipc.call("session.resume", { device_id, session_id, resume_id, resume_capability: capability, target_uid });
      return fromFrame(f);
    },
  );

  server.registerTool(
    "remote.exec",
    {
      description: "在对方执行脚本或程序，script 与 program 二选一，流式回收 stdout/stderr。解释器和路径均属于对方。",
      inputSchema: execInputSchema,
    },
    async ({ script, program, device_id, session_id, cwd, timeout_ms, backend }, ctx) => {
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
      let observedExecID = "";
      const f = await ipc.stream(
        "exec",
        {
          ...(script ? { script } : { program }),
          ...(device_id ? { device_id } : {}),
          ...(session_id ? { session_id } : {}),
          ...(cwd ? { cwd } : {}),
          ...(backend !== undefined ? { backend } : {}),
          ...(timeout_ms ? { timeout_ms } : {}),
        },
        (ev) => {
          if (ev.exec_id && !observedExecID) {
            observedExecID = ev.exec_id;
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
      ).catch((): Frame => ({ ok: false, message: "连接已断开，执行结果未确认。" }));
      const got = execFrameText(f, stdout.join(""), stderr.join(""), observedExecID);
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
        session_id: z.string().min(1).optional().describe("已批准的逻辑会话 ID"),
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
        session_id: z.string().min(1).optional().describe("已批准的逻辑会话 ID"),
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
        session_id: z.string().min(1).optional().describe("已批准的逻辑会话 ID"),
      }),
    },
    async ({ device_id, session_id }) => {
      const ipc = await ensureRuntime();
      const f = await ipc.call("processes", { ...(device_id ? { device_id } : {}), ...(session_id ? { session_id } : {}) });
      return fromFrame(f, typeof f.result?.content === "string" ? f.result.content : undefined);
    },
  );

  return server;
}

void serveStdio(createServer);
