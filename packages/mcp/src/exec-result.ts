import type { Frame } from "./ipc.js";

export function execToolIsError(status: unknown, exitCode: unknown): boolean {
  return status !== "completed" || exitCode !== 0;
}

export function execToolText(
  status: unknown,
  exitCode: unknown,
  durationMs: unknown,
  truncated: unknown,
  execId: unknown,
  stdout: string,
  stderr: string,
): { text: string; isError: boolean } {
  const knownStatus = status === "completed" || status === "cancelled" || status === "timeout";
  const knownExit = typeof exitCode === "number" && Number.isSafeInteger(exitCode);
  const confirmed = knownStatus && (status !== "completed" || knownExit);
  const summary = !confirmed ? "执行结果未确认。" : status === "cancelled" ? "任务已取消。" : status === "timeout" ? "执行超时。" : "";
  const bits = [
    stdout,
    stderr ? `stderr:\n${stderr}` : "",
    summary,
    `status=${knownStatus ? status : "unknown"} exit_code=${knownExit ? exitCode : "unknown"} duration_ms=${typeof durationMs === "number" && Number.isSafeInteger(durationMs) && durationMs >= 0 ? durationMs : "unknown"} truncated=${typeof truncated === "boolean" ? truncated : "unknown"} exec_id=${typeof execId === "string" && execId ? execId : "unknown"}`,
  ].filter(Boolean);
  return { text: bits.join("\n"), isError: execToolIsError(status, exitCode) };
}

// The envelope must explicitly confirm a terminal before its result is trusted.
// Retain already-observed output even when the operation fails or disconnects.
export function execFrameText(frame: Frame, stdout: string, stderr: string, observedExecID = ""): { text: string; isError: boolean } {
  if (frame.ok !== true || frame.event || frame.code) {
    return {
      text: [stdout, stderr ? `stderr:\n${stderr}` : "", frame.ok === false ? frame.message || frame.code || "执行失败。" : "执行结果未确认。", observedExecID ? `exec_id=${observedExecID}` : ""].filter(Boolean).join("\n"),
      isError: true,
    };
  }
  const r = frame.result;
  return execToolText(r?.status, r?.exit_code, r?.duration_ms, r?.truncated, r?.exec_id, stdout, stderr);
}

export type ToolCtx = {
  mcpReq?: {
    signal?: AbortSignal;
    _meta?: { progressToken?: string | number };
    notify?: (msg: { method: string; params: Record<string, unknown> }) => Promise<void>;
  };
};

export function progressTokenOf(ctx: ToolCtx | undefined): string | number | undefined {
  return ctx?.mcpReq?._meta?.progressToken;
}

export async function notifyExecProgress(
  ctx: ToolCtx | undefined,
  progress: number,
  message: string,
): Promise<void> {
  const token = progressTokenOf(ctx);
  if (token === undefined || !ctx?.mcpReq?.notify) {
    return;
  }
  await ctx.mcpReq.notify({
    method: "notifications/progress",
    params: { progressToken: token, progress, message },
  });
}

export function newExecProgress(ctx: ToolCtx | undefined): (message: string) => Promise<void> {
  let n = 0;
  return async (message: string) => {
    n += 1;
    await notifyExecProgress(ctx, n, message);
  };
}

export function bindHostCancel(
  ctx: ToolCtx | undefined,
  cancel: (execId: string) => void,
): (execId: string) => void {
  const signal = ctx?.mcpReq?.signal;
  let execId = "";
  let aborted = Boolean(signal?.aborted);
  signal?.addEventListener("abort", () => {
    aborted = true;
    if (execId) {
      cancel(execId);
    }
  });
  return (id: string) => {
    execId = id;
    if (aborted) {
      cancel(id);
    }
  };
}
