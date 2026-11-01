export function execToolIsError(status: string, exitCode: unknown): boolean {
  if (status === "cancelled" || status === "timeout") {
    return true;
  }
  return Number(exitCode) !== 0;
}

export function execToolText(
  status: string,
  exitCode: unknown,
  durationMs: unknown,
  truncated: unknown,
  execId: unknown,
  stdout: string,
  stderr: string,
): { text: string; isError: boolean } {
  if (status === "cancelled") {
    return { text: "任务已取消。", isError: true };
  }
  if (status === "timeout") {
    return { text: "执行超时。", isError: true };
  }
  const bits = [
    stdout,
    stderr ? `stderr:\n${stderr}` : "",
    `exit_code=${exitCode} duration_ms=${durationMs} truncated=${truncated} exec_id=${execId}`,
  ].filter(Boolean);
  return { text: bits.join("\n"), isError: execToolIsError(status, exitCode) };
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
