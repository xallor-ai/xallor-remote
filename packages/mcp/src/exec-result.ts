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

export type ProgressExtra = {
  signal?: AbortSignal;
  _meta?: { progressToken?: string | number };
  sendNotification?: (msg: { method: string; params: Record<string, unknown> }) => Promise<void>;
};

export function progressTokenOf(extra: ProgressExtra | undefined): string | number | undefined {
  return extra?._meta?.progressToken;
}

export async function notifyExecProgress(
  extra: ProgressExtra | undefined,
  message: string,
): Promise<void> {
  const token = progressTokenOf(extra);
  if (token === undefined || !extra?.sendNotification) {
    return;
  }
  await extra.sendNotification({
    method: "notifications/progress",
    params: { progressToken: token, progress: 0, message },
  });
}

export function bindHostCancel(
  extra: ProgressExtra | undefined,
  cancel: (execId: string) => void,
): (execId: string) => void {
  let execId = "";
  let aborted = false;
  extra?.signal?.addEventListener("abort", () => {
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
