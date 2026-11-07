import type { Frame } from "./bridge";
export type Task = { id: string; device: string; command: string; output: string; status: string; live: boolean; truncated: boolean; execID?: string; cancelRequested: boolean; error?: string };
export function finish(task: Task, frame: Frame) {
  const hasObservedOutput = task.output.length > 0;
  task.live = false;
  if (frame.ok !== true) {
    // Retiring a submitted route does not prove that its command never ran.
    const uncertain = hasObservedOutput || ["relay_error", "agent_error", "result_unknown", "device_offline"].includes(frame.code || "");
    task.status = frame.code === "cancelled" ? "已取消" : frame.code === "exec_timeout" ? "已超时" : uncertain ? "结果待确认" : "执行被拒绝";
    task.error = frame.message || frame.code;
    return;
  }
  const r = frame.result as { status?: string; exit_code?: number; truncated?: boolean } | undefined;
  task.truncated ||= !!r?.truncated;
  if (r?.status === "cancelled") task.status = "已取消";
  else if (r?.status === "timeout") task.status = "已超时";
  else if (r?.status === "authorization_lost") task.status = "授权失效，已停止";
  else if (r?.status === "completed" && Number.isInteger(r.exit_code)) task.status = r.exit_code === 0 ? "已完成" : `执行失败（退出码 ${r.exit_code}）`;
  else task.status = "结果待确认";
}
export function append(task: Task, text: string) {
  const limit = 256 * 1024;
  const available = limit - task.output.length;
  task.output += text.slice(0, available);
  if (text.length > available) task.truncated = true;
}
