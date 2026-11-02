import { rpc, type Status } from "./bridge";
import { action, esc, feedback, form } from "./ui";

type Approval = { id: string; exec_id: string; preview: string; created: string };
export async function approvalPage(root: HTMLElement, signal: AbortSignal) {
  root.innerHTML = '<h2>审批</h2><p role="status"></p><div id="pending">正在读取…</div>';
  const panel = root.querySelector<HTMLElement>("#pending")!;
  let loading = false, previous = "";
  const refresh = async () => {
    if (loading || signal.aborted) return;
    loading = true;
    try {
      const { pending } = await rpc<{ pending: Approval[] }>("approval.list");
      if (signal.aborted) return;
      const current = JSON.stringify(pending);
      if (current === previous) return;
      previous = current;
      panel.innerHTML = pending.map((p, i) => `<section class="card" data-approval="${i}"><h3>本机操作等待确认</h3><p class="meta">${esc(p.created)}</p>
        <pre>${esc(p.preview)}</pre><div class="row"><button class="allow">允许执行</button><button class="deny">拒绝</button></div></section>`).join("") || '<p class="hint">当前没有等待确认的操作。</p>';
      pending.forEach((p, i) => {
        for (const [selector, allow] of [["allow", true], ["deny", false]] as const) {
          action(root, `[data-approval="${i}"] .${selector}`, async () => {
            await rpc("approval.respond", { approval_id: p.id, allow });
            feedback(root, allow ? "已允许执行。" : "已拒绝。", false);
            previous = ""; await refresh();
          });
        }
      });
    } catch (e) { if (!signal.aborted) { panel.textContent = "无法读取待审批操作。"; previous = ""; feedback(root, e); } }
    finally { loading = false; }
  };
  await refresh();
  if (!signal.aborted) {
    const timer = setInterval(() => { void refresh(); }, 1000);
    signal.addEventListener("abort", () => clearInterval(timer), { once: true });
  }
}

type Entry = { time: string; exec_id: string; device_id: string; direction: string; stage: string; op?: string; code?: string; exit_code?: number };
export async function auditPage(root: HTMLElement, signal: AbortSignal) {
  root.innerHTML = '<h2>审计</h2><p class="hint">保留最近 500 条本机操作记录。不保存命令正文。</p><p role="status"></p><button id="refresh">刷新记录</button><div id="history"></div>';
  const refresh = async () => {
    const { entries } = await rpc<{ entries: Entry[] }>("audit.list");
    if (signal.aborted) return;
    root.querySelector("#history")!.innerHTML = entries.slice().reverse().map(e => `<section class="card"><div class="card-head"><strong>${esc(e.direction === "inbound" ? "本机接收" : "发往对方")} · ${esc(e.device_id)}</strong><span>${esc(e.stage === "requested" ? "已请求" : e.stage === "finished" ? "已结束" : e.stage === "rejected" ? "已拒绝" : "结果待确认")}</span></div>
      <p class="meta">${esc(e.time)} · ${esc(e.op || e.code || "")}${e.exit_code === undefined ? "" : ` · 退出码 ${e.exit_code}`}</p><code>${esc(e.exec_id)}</code></section>`).join("") || '<p class="hint">尚无记录。</p>';
  };
  action(root, "#refresh", refresh); await refresh();
}

export async function settingsPage(root: HTMLElement, signal: AbortSignal) {
  const st = await rpc<Status>("status");
  if (signal.aborted) return;
  root.innerHTML = `<h2>设置</h2><p role="status"></p><section class="card"><h3>工作目录</h3>
    <p class="hint">文件操作限制在此目录；命令以此为默认工作目录。</p><form id="workspace" class="stack"><label>路径<input name="workspace" required value="${esc(st.workspace)}"></label><button type="submit">保存</button></form>
    </section><section class="card"><h3>本机版本</h3><p>${esc(st.version)}</p><p class="meta">${esc(st.device_id)}</p></section>`;
  form(root, "#workspace", async data => { await rpc("config.set", { workspace: String(data.get("workspace")).trim() }); feedback(root, "已保存。", false); });
}
