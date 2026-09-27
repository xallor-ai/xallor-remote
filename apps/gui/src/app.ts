import * as api from "./bridge";

type Page = "connect" | "session" | "approval" | "audit" | "settings";

const pages: { id: Page; label: string }[] = [
  { id: "connect", label: "连接" },
  { id: "session", label: "会话" },
  { id: "approval", label: "审批" },
  { id: "audit", label: "审计" },
  { id: "settings", label: "设置" },
];

export function mount(root: HTMLElement): void {
  root.innerHTML = `
    <aside class="nav">
      <h1>XallorRemote</h1>
      ${pages.map((p) => `<button type="button" data-page="${p.id}">${p.label}</button>`).join("")}
    </aside>
    <main id="main"></main>
  `;
  let page: Page = "connect";
  const main = root.querySelector("#main") as HTMLElement;
  const go = (next: Page) => {
    page = next;
    root.querySelectorAll(".nav button").forEach((b) => {
      b.classList.toggle("on", (b as HTMLElement).dataset.page === page);
    });
    void render(main, page);
  };
  root.querySelector(".nav")?.addEventListener("click", (e) => {
    const t = e.target as HTMLElement;
    if (t.dataset.page) go(t.dataset.page as Page);
  });
  go("connect");
}

async function render(main: HTMLElement, page: Page): Promise<void> {
  main.innerHTML = "<p class='hint'>加载中…</p>";
  try {
    if (page === "connect") return void (await connectPage(main));
    if (page === "session") return void (await session(main));
    if (page === "settings") return void (await settings(main));
    if (page === "approval") {
      main.innerHTML = "<h2>审批</h2><p class='hint'>当前没有等待确认的操作。</p>";
      return;
    }
    main.innerHTML = "<h2>审计</h2><p class='hint'>这里不展示命令全文。</p>";
  } catch (e) {
    main.innerHTML = `<p class="err">${esc(e instanceof Error ? e.message : "失败。")}</p>`;
  }
}

async function connectPage(main: HTMLElement): Promise<void> {
  const [st, grant, peers] = await Promise.all([
    api.status(),
    api.grantShow().catch(() => ""),
    api.peerList().catch(() => [] as string[]),
  ]);
  const inboundOn = !!(st.inbound && st.has_grant);
  main.innerHTML = `
    <h2>连接</h2>
    <section class="card">
      <div class="card-head">
        <h3>本机</h3>
        <label class="switch">
          <input type="checkbox" id="inbound" ${inboundOn ? "checked" : ""} />
          <span>允许入站</span>
        </label>
      </div>
      <p class="id-line">${esc(st.device_id || "")}</p>
      <p class="meta">${st.online ? "在线" : "离线"}${st.has_grant ? "" : " · 还没有授权码"}</p>
      <div class="row">
        <button type="button" id="issue" class="primary">签发授权码</button>
        <button type="button" id="rotate">换新授权码</button>
        <button type="button" id="copy" ${grant ? "" : "disabled"}>复制并分享</button>
      </div>
      <pre id="grant">${grant ? "授权码: " + esc(grant) + "\n把这一行给对方。" : "还没有授权码。签发后把授权码发给要控你的人。"}</pre>
    </section>
    <section class="card">
      <h3>对方设备</h3>
      <p class="hint">用对方的用户名和授权码添加后，到「会话」执行命令。</p>
      <ul class="peer-list" id="peers">${
        peers.length
          ? peers
              .map(
                (id) =>
                  `<li><span>${esc(id)}</span><button type="button" data-rm="${esc(id)}">移除</button></li>`,
              )
              .join("")
          : `<li class="hint">还没有对方设备。</li>`
      }</ul>
      <form id="add" class="add-row">
        <input name="id" placeholder="对方用户名" autocomplete="off" />
        <input name="grant" placeholder="对方授权码" autocomplete="off" />
        <button type="submit" class="primary">添加</button>
      </form>
    </section>
  `;

  const grantEl = main.querySelector("#grant") as HTMLElement;
  const showGrant = (g: string) => {
    grantEl.textContent = "授权码: " + g + "\n把这一行给对方。";
    (main.querySelector("#copy") as HTMLButtonElement).disabled = !g;
  };

  (main.querySelector("#inbound") as HTMLInputElement).onchange = async (e) => {
    const on = (e.target as HTMLInputElement).checked;
    try {
      await api.inboundSet(on);
      await connectPage(main);
    } catch (err) {
      alert(err instanceof Error ? err.message : "失败。");
      await connectPage(main);
    }
  };

  bind(main, "#issue", async () => showGrant(await api.grantIssue()));
  bind(main, "#rotate", async () => {
    if (!confirm("换新授权码后，旧码立刻失效。继续？")) return;
    showGrant(await api.grantRotate());
  });
  bind(main, "#copy", async () => {
    const g = await api.grantShow();
    if (!g) {
      alert("还没有授权码。");
      return;
    }
    const line = `用户名: ${st.device_id || ""}\n授权码: ${g}`;
    await navigator.clipboard.writeText(line);
    alert("已复制，发给对方即可。");
  });

  main.querySelector("#peers")?.addEventListener("click", (e) => {
    const t = e.target as HTMLElement;
    const id = t.dataset.rm;
    if (!id) return;
    void api
      .peerRemove(id)
      .then(() => connectPage(main))
      .catch((err: unknown) => alert(err instanceof Error ? err.message : "失败。"));
  });

  (main.querySelector("#add") as HTMLFormElement).onsubmit = async (e) => {
    e.preventDefault();
    const fd = new FormData(e.target as HTMLFormElement);
    const id = String(fd.get("id") || "").trim();
    const g = String(fd.get("grant") || "").trim();
    if (!id || !g) {
      alert("请填写用户名和授权码。");
      return;
    }
    try {
      await api.peerAdd(id, g);
      await connectPage(main);
    } catch (err) {
      alert(err instanceof Error ? err.message : "失败。");
    }
  };
}

async function session(main: HTMLElement): Promise<void> {
  const peers = await api.peerList().catch(() => [] as string[]);
  const options =
    peers.length === 0
      ? `<option value="">（先在「连接」添加对方）</option>`
      : peers.map((id, i) => `<option value="${esc(id)}" ${i === 0 ? "selected" : ""}>${esc(id)}</option>`).join("");
  main.innerHTML = `
    <h2>会话</h2>
    <form id="run" class="stack wide">
      <label>对方设备
        <select name="device">${options}</select>
      </label>
      <input name="cmd" placeholder="要执行的命令" autocomplete="off" />
      <button type="submit" class="primary" ${peers.length ? "" : "disabled"}>执行</button>
    </form>
    <pre id="out"></pre>
  `;
  (main.querySelector("#run") as HTMLFormElement).onsubmit = async (e) => {
    e.preventDefault();
    const fd = new FormData(e.target as HTMLFormElement);
    const out = main.querySelector("#out") as HTMLElement;
    const cmd = String(fd.get("cmd") || "").trim();
    const device = String(fd.get("device") || "").trim();
    if (!cmd) {
      alert("请输入命令。");
      return;
    }
    if (!device) {
      alert("请先添加对方设备。");
      return;
    }
    out.textContent = "";
    try {
      await api.execCommand(cmd, device, (s) => {
        out.textContent += s;
        out.scrollTop = out.scrollHeight;
      });
    } catch (err) {
      out.textContent += (err instanceof Error ? err.message : "失败。") + "\n";
    }
  };
}

async function settings(main: HTMLElement): Promise<void> {
  const [st, cfg] = await Promise.all([api.status(), api.configGet().catch(() => ({} as { workspace?: string }))]);
  const ws = cfg.workspace || st.workspace || "";
  main.innerHTML = `
    <h2>设置</h2>
    <section class="card">
      <h3>Workspace</h3>
      <p class="hint">对本机执行与文件操作生效的工作目录。</p>
      <form id="ws" class="add-row">
        <input name="workspace" value="${esc(ws)}" placeholder="例如 C:\\Users\\you\\XallorRemote\\workspace" autocomplete="off" />
        <button type="submit" class="primary">保存</button>
      </form>
      <p id="ws-note" class="hint"></p>
    </section>
    <section class="card">
      <h3>关于</h3>
      <dl class="compact">
        <dt>版本</dt><dd>${esc(st.version || "")}</dd>
        <dt>用户名</dt><dd>${esc(st.device_id || "")}</dd>
      </dl>
    </section>
  `;
  (main.querySelector("#ws") as HTMLFormElement).onsubmit = async (e) => {
    e.preventDefault();
    const fd = new FormData(e.target as HTMLFormElement);
    const path = String(fd.get("workspace") || "").trim();
    const note = main.querySelector("#ws-note") as HTMLElement;
    if (!path) {
      alert("请填写路径。");
      return;
    }
    try {
      await api.configSetWorkspace(path);
      note.textContent = "已保存。";
    } catch (err) {
      note.textContent = "";
      alert(err instanceof Error ? err.message : "失败。");
    }
  };
}

function bind(root: HTMLElement, sel: string, fn: () => Promise<void>): void {
  root.querySelector(sel)?.addEventListener("click", () => {
    void fn().catch((e: unknown) => {
      alert(e instanceof Error ? e.message : "失败。");
    });
  });
}

function esc(s: string): string {
  return s.replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c] || c));
}
