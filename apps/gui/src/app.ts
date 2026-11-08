import { connection, rpc } from "./bridge";
import { connectPage } from "./connect-page";
import { tasksPage } from "./tasks-page";
import { approvalPage, auditPage, settingsPage } from "./history-pages";
import { filesPage } from "./files-page";
import { pageError } from "./ui";

const pages = { connect: ["连接", connectPage], tasks: ["任务", tasksPage], files: ["文件", filesPage], approval: ["审批", approvalPage], audit: ["审计", auditPage], settings: ["设置", settingsPage] } as const;
type Page = keyof typeof pages;
export function mount(root: HTMLElement): void {
  root.innerHTML = `<aside class="nav"><h1>XallorRemote</h1>${Object.entries(pages).map(([key, [label]]) => `<button data-page="${key}">${label}<span class="badge"></span></button>`).join("")}</aside><main><div id="connection" role="status"></div><div id="page"></div></main>`;
  let controller = new AbortController(), current: Page = "connect";
  const outlet = root.querySelector<HTMLElement>("#page")!;
  const banner = root.querySelector<HTMLElement>("#connection")!;
  const go = (next: Page) => {
    controller.abort(); controller = new AbortController(); current = next;
    const content = document.createElement("div"); content.textContent = "加载中…";
    outlet.replaceChildren(content);
    root.querySelectorAll<HTMLButtonElement>("[data-page]").forEach(b => b.classList.toggle("on", b.dataset.page === next));
    void pages[next][1](content, controller.signal).catch(e => pageError(content, e));
  };
  root.querySelectorAll<HTMLButtonElement>("[data-page]").forEach(b => b.onclick = () => go(b.dataset.page as Page));
  let checking = false;
  const update = async () => {
    if (connection.error) {
      banner.replaceChildren(document.createTextNode(connection.error + " "));
      const retry = document.createElement("button"); retry.textContent = "重新连接";
      retry.onclick = async () => { retry.disabled = true; try { await connection.reconnect(); go(current); } catch (e) { banner.textContent = e instanceof Error ? e.message : "连接失败。"; } finally { retry.disabled = false; } };
      banner.append(retry); return;
    }
    banner.textContent = "";
    if (checking) return; checking = true;
    try {
      const result = await rpc<{ pending: unknown[] }>("approval.list");
      root.querySelector('[data-page="approval"] .badge')!.textContent = result.pending.length ? ` (${result.pending.length})` : "";
    } catch { /* Connection status is rendered above; never claim an empty queue. */ }
    finally { checking = false; }
  };
  connection.onChange(() => { void update(); });
  void connection.reconnect().then(() => go("connect")).catch(e => { pageError(outlet, e); void update(); });
  const timer = setInterval(() => { void update(); }, 3000);
  window.addEventListener("pagehide", () => { controller.abort(); clearInterval(timer); }, { once: true });
}
