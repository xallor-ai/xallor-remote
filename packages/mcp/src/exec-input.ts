import { z } from "zod";

const text = z.string().refine(s => !s.includes("\0") && Buffer.from(s, "utf8").toString("utf8") === s, "需要有效 UTF-8 文本且不能包含 NUL");
export const execInputSchema = z.object({
  script: z.object({
    text: text.min(1),
    shell: z.enum(["auto", "powershell", "pwsh", "bash", "sh"]).optional(),
  }).strict().optional().describe("脚本文本与对方解释器；与 program 二选一"),
  program: z.object({
    executable: text.min(1),
    args: z.array(text).max(256).default([]),
  }).strict().optional().describe("直接启动对方程序，按数组保留参数，不经 Shell 展开"),
  device_id: z.string().optional(),
  backend: text.max(64).optional().describe("对方执行后端；省略使用目标配置，不支持时拒绝且不回退"),
  cwd: text.optional().describe("对方工作目录，默认 workspace"),
  timeout_ms: z.number().int().min(0).max(3_600_000).optional().describe("超时毫秒，0 不设期限，最长 1 小时"),
}).strict().refine(p => Boolean(p.script) !== Boolean(p.program), "script 与 program 必须且只能提供一个")
  .refine(p => [p.backend ?? "", p.cwd ?? "", p.script?.text ?? "", p.script?.shell ?? "", p.program?.executable ?? "", ...(p.program?.args ?? [])]
    .reduce((total, s) => total + Buffer.byteLength(s, "utf8"), 0) <= 64 * 1024, "执行输入最多 64 KiB");
