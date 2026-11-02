import { rpc, type Status } from "./bridge";
import { tasks, run, cancel, observeTasks } from "./tasks";
import { esc, form } from "./ui";
export async function tasksPage(root: HTMLElement, signal: AbortSignal) {
  const st = await rpc<Status>("status");
  if (signal.aborted) return;
  root.innerHTML = `<h2>任务</h2><p role="status"></p><form id="run" class="stack wide">
    <label>对方设备<select name="device">${st.peers.map(p => `<option value="${esc(p.device_id)}">${esc(p.name || p.device_id)} · ${esc(p.device_id)}</option>`).join("")}</select></label>
    <label>命令<input name="command" required autocomplete="off" placeholder="例如 whoami"></label>
    <p class="hint">单次执行最长 60 秒；关闭窗口会取消当前任务。</p>
    <button type="submit" ${st.peers.length ? "" : "disabled"}>执行</button></form><div id="tasks"></div>`;
  const output = root.querySelector<HTMLElement>("#tasks")!;
  const paint = () => {
    output.innerHTML = tasks.slice().reverse().map(t => `<section class="card" data-task="${t.id}"><div class="card-head"><h3>${esc(t.device)}</h3>
      <span class="task-state">${esc(t.status)}</span><button class="cancel" ${!t.live || t.cancelRequested ? "disabled" : ""}>取消任务</button></div>
      <p class="task-command">${esc(t.command)}</p>${t.error ? `<p class="err">${esc(t.error)}</p>` : ""}
      ${t.truncated ? '<p class="hint">输出已截断，显示内容不完整。</p>' : ""}<pre>${esc(t.output)}</pre></section>`).join("") || '<p class="hint">尚无任务。</p>';
    output.querySelectorAll<HTMLElement>("[data-task]").forEach(row => {
      row.querySelector(".cancel")!.addEventListener("click", () => { void cancel(tasks.find(t => t.id === row.dataset.task)!); });
    });
  };
  form(root, "#run", async data => { run(String(data.get("command")).trim(), String(data.get("device"))); });
  observeTasks(paint, signal); paint();
}
