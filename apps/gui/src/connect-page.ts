import { rpc, type Status } from "./bridge";
import { action, esc, feedback, form } from "./ui";

const cleanups = new WeakMap<HTMLElement, () => void>();

export async function connectPage(root: HTMLElement, signal: AbortSignal): Promise<void> {
  cleanups.get(root)?.();
  const st = await rpc<Status>("status");
  if (signal.aborted) return;
  root.innerHTML = `<h2>连接</h2><p role="status"></p>
    <section class="card"><div class="card-head"><h3>本机</h3>
      <label class="switch"><input type="checkbox" id="inbound" ${st.inbound ? "checked" : ""}>允许入站</label></div>
      <p class="id-line">${esc(st.device_id)}</p><p class="meta">${st.online ? "中转已连接" : "中转未连接"}</p>
      <div class="row"><button id="reveal" ${st.has_grant ? "" : "disabled"}>显示授权码</button>
      <button id="copy" ${st.has_grant ? "" : "disabled"}>复制连接信息</button>
      <button id="issue">${st.has_grant ? "换新授权码" : "签发授权码"}</button></div>
      <pre id="grant">${st.has_grant ? "授权码已隐藏" : "尚未签发授权码"}</pre>
    </section><section class="card"><h3>对方设备</h3><p class="hint">添加后可在「任务」中执行命令。</p>
      <ul class="peer-list">${st.peers.map((peer, i) => `<li data-peer="${i}">
        <div class="peer-detail"><strong>${esc(peer.name || peer.device_id)}</strong><span class="meta">${esc(peer.device_id)}</span>
        <p class="peer-state hint">尚未检查</p><form class="rename add-row"><input aria-label="设备备注" name="name" maxlength="80" value="${esc(peer.name || "")}" placeholder="设备备注"><button type="submit">保存备注</button></form></div>
        <div class="row"><button class="check">检查连接</button><button class="remove">移除</button></div></li>`).join("") || '<li class="hint">尚未添加对方设备。</li>'}</ul>
      <form id="add" class="stack"><label>设备 ID<input name="id" required pattern="[0-9]{9}" inputmode="numeric" placeholder="对方的 9 位设备 ID"></label>
      <label>授权码<input type="password" name="grant" required autocomplete="off" placeholder="对方授权码"></label><button type="submit">添加设备</button></form>
    </section><button id="refresh">刷新状态</button>`;
  const refresh = () => connectPage(root, signal);
  const grant = root.querySelector<HTMLElement>("#grant")!;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const hide = () => { clearTimeout(timer); grant.textContent = "授权码已隐藏"; const button = root.querySelector("#reveal"); if (button) button.textContent = "显示授权码"; };
  const cleanup = () => { hide(); window.removeEventListener("blur", hide); signal.removeEventListener("abort", cleanup); };
  cleanups.set(root, cleanup);
  window.addEventListener("blur", hide);
  signal.addEventListener("abort", cleanup, { once: true });
  action(root, "#reveal", async () => {
    if (root.querySelector("#reveal")!.textContent === "隐藏授权码") { hide(); return; }
    const result = await rpc<{ grant: string }>("grant.show");
    if (signal.aborted) return;
    grant.textContent = result.grant || "尚未签发授权码";
    root.querySelector("#reveal")!.textContent = "隐藏授权码";
    timer = setTimeout(hide, 30_000);
  });
  action(root, "#copy", async () => {
    const result = await rpc<{ grant: string }>("grant.show");
    if (signal.aborted) return;
    await navigator.clipboard.writeText(`设备 ID: ${st.device_id}\n授权码: ${result.grant}`);
    feedback(root, "连接信息已复制。", false);
  });
  action(root, "#issue", async () => {
    if (st.has_grant && !confirm("换新授权码后旧码将失效。继续？")) return;
    await rpc(st.has_grant ? "grant.rotate" : "grant.issue"); await refresh();
  });
  root.querySelector<HTMLInputElement>("#inbound")!.onchange = async e => {
    const el = e.currentTarget as HTMLInputElement; el.disabled = true;
    try { await rpc("inbound.set", { enabled: el.checked }); await refresh(); }
    catch (e) { el.checked = st.inbound; feedback(root, e); }
    finally { el.disabled = false; }
  };
  form(root, "#add", async data => {
    await rpc("peer.add", { device_id: String(data.get("id")).trim(), grant: String(data.get("grant")).trim() });
    await refresh();
  });
  for (const [i, peer] of st.peers.entries()) {
    const row = root.querySelector<HTMLElement>(`[data-peer="${i}"]`)!;
    // Feedback belongs to the page; row controls retain their own device closure.
    form(root, `[data-peer="${i}"] .rename`, async data => {
      await rpc("peer.rename", { device_id: peer.device_id, name: String(data.get("name")) }); await refresh();
    });
    action(root, `[data-peer="${i}"] .remove`, async () => { await rpc("peer.remove", { device_id: peer.device_id }); await refresh(); });
    action(root, `[data-peer="${i}"] .check`, async () => {
      const state = row.querySelector<HTMLElement>(".peer-state")!; state.textContent = "正在检查…";
      try {
        const report = await rpc<{ ok: boolean; checks: { state: string; code?: string; action?: string }[] }>("doctor", { device_id: peer.device_id });
        state.textContent = report.ok ? "可连接" : report.checks.filter(c => c.state === "fail" || c.state === "unknown").map(c => c.action || c.code || "状态待确认").join("；");
      } catch (e) { state.textContent = "检查失败"; throw e; }
    });
  }
  action(root, "#refresh", refresh);
}
