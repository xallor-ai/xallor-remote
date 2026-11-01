import { connection, rpc } from "./bridge";
import { append, finish, type Task } from "./task-state";
export const tasks: Task[] = [];
const listeners = new Set<() => void>();
const notify = () => listeners.forEach(fn => fn());
export function observeTasks(fn: () => void, signal: AbortSignal) { listeners.add(fn); signal.addEventListener("abort", () => listeners.delete(fn), { once: true }); }
export async function cancel(task: Task) {
  if (!task.live) return;
  task.cancelRequested = true; task.status = "正在取消…"; notify();
  if (!task.execID) return;
  try { await rpc("exec.cancel", { exec_id: task.execID }); }
  catch (e) { if (task.live) { task.error = e instanceof Error ? e.message : "取消失败。"; task.status = "取消未确认"; task.cancelRequested = false; notify(); } }
}
export function run(command: string, device: string) {
  if (tasks.filter(t => t.live).length >= 8) throw new Error("同时执行的任务过多，请等待完成。");
  while (tasks.length >= 20) { const i = tasks.findIndex(t => !t.live); if (i < 0) break; tasks.splice(i, 1); }
  const task: Task = { id: crypto.randomUUID(), command, device, output: "", status: "正在提交…", live: true, truncated: false, cancelRequested: false };
  tasks.push(task); notify();
  void connection.request("exec", { command, device_id: device, timeout_ms: 60_000 }, frame => {
    if (frame.exec_id && !task.execID) {
      task.execID = frame.exec_id; task.status = "等待执行结果…";
      if (task.cancelRequested) void cancel(task);
    }
    if (frame.data) { append(task, frame.data); if (!task.cancelRequested) task.status = "执行中"; }
    notify();
  }).then(frame => finish(task, frame)).catch(e => {
    task.live = false; task.status = "结果待确认"; task.error = e instanceof Error ? e.message : "连接中断。";
  }).finally(notify);
}
